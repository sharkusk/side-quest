package store

import (
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
