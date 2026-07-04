package store

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/sharkusk/side-quest/internal/gitcmd"
)

// newOrigin returns (originDir) for a bare repo usable as a file remote.
func newOrigin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := gitcmd.New(dir).Run("init", "--bare", "-q"); err != nil {
		t.Fatal(err)
	}
	return dir
}

// clone makes a working repo with origin set to originDir and an identity.
func clone(t *testing.T, originDir string) *Store {
	t.Helper()
	dir := t.TempDir()
	g := gitcmd.New(dir)
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "Tester"},
		{"remote", "add", "origin", originDir},
	} {
		if _, err := g.Run(args...); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestReconcileFastForward(t *testing.T) {
	origin := newOrigin(t)
	a := clone(t, origin)
	if err := a.Init(); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, a) // SQ-0001
	if _, err := a.git.Run("push", origin, Ref); err != nil {
		t.Fatal(err)
	}

	b := clone(t, origin)
	// b has no quests yet; fetch origin into its tracking ref, then reconcile.
	if _, err := b.git.Run("fetch", origin, FetchRefspec); err != nil {
		t.Fatal(err)
	}
	res, err := b.reconcile(false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.UpToDate && res.Merged == 0 {
		t.Errorf("expected b to adopt remote quests, got %+v", res)
	}
	got, err := b.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("b should have 1 quest after reconcile, got %d", len(got))
	}
}

func TestReconcileDivergedConverges(t *testing.T) {
	origin := newOrigin(t)
	a := clone(t, origin)
	if err := a.Init(); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, a) // SQ-0001 (shared base)
	if _, err := a.git.Run("push", origin, Ref); err != nil {
		t.Fatal(err)
	}
	b := clone(t, origin)
	if _, err := b.git.Run("fetch", origin, FetchRefspec); err != nil {
		t.Fatal(err)
	}
	if _, err := b.reconcile(false); err != nil { // b adopts SQ-0001
		t.Fatal(err)
	}

	// Diverge: a adds SQ-0002, b adds SQ-0003 (random strategy avoids id clash;
	// both are sequential here but different content, so ids differ by counter).
	if _, err := a.Create("a work", "", "", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.git.Run("push", origin, Ref); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Create("b work", "", "", "", nil); err != nil {
		t.Fatal(err)
	}
	// b fetches a's advance and reconciles -> merge commit containing all three.
	if _, err := b.git.Run("fetch", origin, FetchRefspec); err != nil {
		t.Fatal(err)
	}
	res, err := b.reconcile(false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Merged == 0 {
		t.Errorf("expected a merge, got %+v", res)
	}
	got, _ := b.List()
	if len(got) != 3 {
		t.Fatalf("b should have 3 quests after merge, got %d", len(got))
	}
	// the merge commit has two parents
	tip, _ := b.tip()
	parents, _ := b.git.Run("rev-list", "--parents", "-n", "1", tip)
	if len(strings.Fields(parents)) != 3 {
		t.Errorf("merge tip should have 2 parents: %q", parents)
	}
}

func TestBuildMergeCommitHasTwoParents(t *testing.T) {
	s := newStore(t)
	// two independent root commits on the quest ref namespace to use as parents
	tx1 := newTxn()
	tx1.put("quests/SQ-0001.md", []byte("one"))
	p1, err := s.buildCommit("", "p1", tx1)
	if err != nil {
		t.Fatal(err)
	}
	tx2 := newTxn()
	tx2.put("quests/SQ-0002.md", []byte("two"))
	p2, err := s.buildCommit("", "p2", tx2)
	if err != nil {
		t.Fatal(err)
	}

	tx := newTxn()
	tx.put("quests/SQ-0003.md", []byte("merged"))
	m, err := s.buildMergeCommit([]string{p1, p2}, "merge", tx)
	if err != nil {
		t.Fatal(err)
	}

	// exactly two parents, in order
	out, err := s.git.Run("rev-list", "--parents", "-n", "1", m)
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(out)
	if len(fields) != 3 || fields[1] != p1 || fields[2] != p2 {
		t.Fatalf("parents = %v, want [%s %s]", fields, p1, p2)
	}
	// tree is exactly tx (only SQ-0003), not a union with the parents' trees
	names, err := s.git.Run("ls-tree", "--name-only", "-r", m)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(names) != "quests/SQ-0003.md" {
		t.Fatalf("tree = %q, want only quests/SQ-0003.md", names)
	}
}

func TestSideAtReadsQuestsAndTouch(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	a := mustCreate(t, s) // SQ-0001
	tip, _ := s.tip()

	side, err := s.sideAt(tip)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := side.Quests[a.ID]; !ok {
		t.Fatalf("sideAt missing %s: %v", a.ID, side.Quests)
	}

	if err := s.fillTouch(&side, tip, []string{a.ID}); err != nil {
		t.Fatal(err)
	}
	if side.Touch[a.ID].IsZero() {
		t.Errorf("touch time for %s not populated", a.ID)
	}
}

func TestSyncConvergesAndInherits(t *testing.T) {
	origin := newOrigin(t)
	a := clone(t, origin)
	if err := a.Init(); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, a)
	if _, err := a.Sync("origin", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	b := clone(t, origin)
	if _, err := b.Sync("origin", SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	// diverge
	if _, err := a.Create("a work", "", "", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Sync("origin", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Create("b work", "", "", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Sync("origin", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	// a syncs again and must converge to b's merged tree
	if _, err := a.Sync("origin", SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	al, _ := a.List()
	bl, _ := b.List()
	if len(al) != len(bl) || len(al) != 3 {
		t.Fatalf("did not converge: a=%d b=%d (want 3 each)", len(al), len(bl))
	}

	// inheritance: a second sync on a settled clone does nothing (no new commit)
	before, _ := a.tip()
	res, err := a.Sync("origin", SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := a.tip()
	if before != after || !res.UpToDate {
		t.Errorf("settled clone re-merged: before=%s after=%s res=%+v", before, after, res)
	}
}

func TestSideAtEmptyCommit(t *testing.T) {
	s := newStore(t)
	side, err := s.sideAt("")
	if err != nil {
		t.Fatal(err)
	}
	if len(side.Quests) != 0 {
		t.Errorf("empty commit should yield no quests, got %d", len(side.Quests))
	}
}

// userWork is a fingerprint of everything side-quest must never touch: the
// user's branches, their staged index, and their working tree. Every field is
// content-addressed, so any change — a moved ref, a restaged file, an edited or
// deleted worktree file — shows up as an inequality.
type userWork struct {
	heads     string // "refs/heads/<name> <sha>" lines for every local branch
	indexHash string // sha256 over the raw .git/index bytes
	tree      string // sorted "relpath sha256" lines for every file except .git
}

// captureUserWork fingerprints the three protected surfaces of s's clone.
func captureUserWork(t *testing.T, s *Store) userWork {
	t.Helper()
	top, err := s.git.Run("rev-parse", "--show-toplevel")
	if err != nil {
		t.Fatal(err)
	}
	heads, err := s.git.Run("for-each-ref", "--format=%(refname) %(objectname)", "refs/heads")
	if err != nil {
		t.Fatal(err)
	}
	idx, err := os.ReadFile(filepath.Join(s.gitDir, "index"))
	if err != nil {
		t.Fatal(err)
	}
	return userWork{
		heads:     heads,
		indexHash: fmt.Sprintf("%x", sha256.Sum256(idx)),
		tree:      hashTree(t, top),
	}
}

// hashTree returns a stable "relpath sha256" listing of every file under root
// except the .git directory, so any content edit, addition, or deletion in the
// working tree changes the result.
func hashTree(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		lines = append(lines, fmt.Sprintf("%s %x", rel, sha256.Sum256(b)))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// TestSyncNeverTouchesUserWork is the executable form of the branch-safety
// invariant: a full diverge -> fetch -> merge -> push cycle on the quest ref
// must leave the user's branches, index, and working tree byte-for-byte
// unchanged. side-quest may only ever write refs/side-quest/*.
func TestSyncNeverTouchesUserWork(t *testing.T) {
	origin := newOrigin(t)

	// a is the "other" clone that diverges the shared quest ref.
	a := clone(t, origin)
	if err := a.Init(); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, a) // SQ-0001 shared base
	if _, err := a.Sync("origin", SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	// b is the clone whose branches/index/worktree we guard.
	b := clone(t, origin)
	if _, err := b.Sync("origin", SyncOptions{}); err != nil { // adopt the base
		t.Fatal(err)
	}

	// Give b real, dirty user work that spans all three protected surfaces:
	// a committed branch, a staged-but-uncommitted change (index != HEAD), an
	// unstaged edit (worktree != index), and an untracked file. A careless sync
	// that ran plain git against the real index/worktree would disturb one of
	// these.
	top, err := b.git.Run("rev-parse", "--show-toplevel")
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(top, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("README.md", "committed v1\n")
	if _, err := b.git.Run("add", "README.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.git.Run("commit", "-qm", "user work"); err != nil {
		t.Fatal(err)
	}
	write("README.md", "staged v2\n")
	if _, err := b.git.Run("add", "README.md"); err != nil { // index = v2, HEAD = v1
		t.Fatal(err)
	}
	write("README.md", "worktree v3\n") // worktree = v3 (unstaged)
	write("scratch.txt", "untracked\n") // untracked file

	before := captureUserWork(t, b)
	beforeTip, err := b.tip()
	if err != nil {
		t.Fatal(err)
	}

	// Full cycle: a advances origin, b advances locally, then b.Sync must
	// fetch a's advance, three-way merge, and push the result.
	if _, err := a.Create("a work", "", "", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Sync("origin", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Create("b work", "", "", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Sync("origin", SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	// Positive control: the quest ref must actually have advanced, or the
	// "user work unchanged" assertions below would be vacuously true. (Merged
	// is documented as an unreliable signal, so we check the tip directly.)
	afterTip, err := b.tip()
	if err != nil {
		t.Fatal(err)
	}
	if afterTip == beforeTip {
		t.Fatalf("quest ref did not advance (%s); the sync cycle was a no-op", beforeTip)
	}

	// The guarantee: not one byte of the user's branches, index, or worktree moved.
	after := captureUserWork(t, b)
	if after.heads != before.heads {
		t.Errorf("refs/heads changed:\n before=%q\n after =%q", before.heads, after.heads)
	}
	if after.indexHash != before.indexHash {
		t.Errorf("index bytes changed: before=%s after=%s", before.indexHash, after.indexHash)
	}
	if after.tree != before.tree {
		t.Errorf("worktree changed:\n before=%q\n after =%q", before.tree, after.tree)
	}
}

// TestAdoptRefRefusesToClobberConcurrentCreate reproduces the read-then-write
// race on the fresh-adopt path: a caller observes the live ref as absent, but a
// concurrent process creates a quest before the adopt writes. The guarded create
// must refuse rather than overwrite that quest.
func TestAdoptRefRefusesToClobberConcurrentCreate(t *testing.T) {
	s := newStore(t)

	// Concurrent create: a real quest now sits on the live ref.
	created := mustCreate(t, s)
	existing, err := s.tip()
	if err != nil {
		t.Fatal(err)
	}
	if existing == "" {
		t.Fatal("precondition: live ref should exist after Create")
	}

	// A different commit the adopt would try to install (e.g. the tracking tip).
	tx := newTxn()
	tx.put("quests/SQ-9999.md", []byte("other"))
	other, err := s.buildCommit("", "other", tx)
	if err != nil {
		t.Fatal(err)
	}

	// The guard must reject the adopt, leaving the concurrently-created quest intact.
	if err := s.adoptRef(other); err == nil {
		t.Fatal("adoptRef overwrote an existing live ref; CAS guard missing")
	}
	if tip, _ := s.tip(); tip != existing {
		t.Errorf("live ref changed from %s to %s despite guard", existing, tip)
	}
	if _, err := s.Get(created.ID); err != nil {
		t.Errorf("concurrently-created quest %s lost: %v", created.ID, err)
	}
}

func TestBootstrapAdoptsTrackingWhenLiveAbsent(t *testing.T) {
	origin := newOrigin(t)
	a := clone(t, origin)
	if err := a.Init(); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, a)
	if _, err := a.Sync("origin", SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	b := clone(t, origin)
	// simulate a fetch having populated only the tracking ref (no live ref yet)
	if _, err := b.git.Run("fetch", origin, FetchRefspec); err != nil {
		t.Fatal(err)
	}
	if tip, _ := b.tip(); tip != "" {
		t.Fatalf("precondition: live ref should be absent, got %s", tip)
	}
	if err := b.BootstrapFromTracking(); err != nil {
		t.Fatal(err)
	}
	got, _ := b.List()
	if len(got) != 1 {
		t.Fatalf("bootstrap should adopt 1 quest, got %d", len(got))
	}
}
