package store

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sharkusk/side-quest/internal/config"
	"github.com/sharkusk/side-quest/internal/gitcmd"
	"github.com/sharkusk/side-quest/internal/quest"
)

// newStore creates a throwaway git repo with a committer identity and returns
// an opened Store. Integration tests run against real git plumbing.
func newStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	g := gitcmd.New(dir)
	if _, err := g.Run("init", "-q"); err != nil {
		t.Fatal(err)
	}
	// commit-tree needs an identity; set it locally in the temp repo.
	if _, err := g.Run("config", "user.email", "t@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run("config", "user.name", "Tester"); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestParseBatch checks the `cat-file --batch` stream parser (SQ-0053): entries
// are length-delimited, so content that itself contains a newline must survive.
func TestParseBatch(t *testing.T) {
	// Two entries; the second's content contains a newline to prove size-based
	// (not line-based) parsing.
	buf := []byte("a1 blob 5\nhello\nb2 blob 3\nx\ny\n")
	got, err := parseBatch(buf, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || string(got[0]) != "hello" || string(got[1]) != "x\ny" {
		t.Fatalf("parseBatch = %q, %q", got[0], got[1])
	}
}

func TestParseBatchMissingObject(t *testing.T) {
	if _, err := parseBatch([]byte("deadbeef missing\n"), 1, false); err == nil {
		t.Fatal("a missing object must be an error")
	}
	// With allowMissing, a missing object yields a nil entry instead of an error.
	got, err := parseBatch([]byte("deadbeef missing\n"), 1, true)
	if err != nil {
		t.Fatalf("allowMissing should not error: %v", err)
	}
	if len(got) != 1 || got[0] != nil {
		t.Fatalf("allowMissing = %v, want [nil]", got)
	}
}

// mustCreate writes one throwaway open quest and returns it, failing the test
// on error. Handy when a test needs a real id to point at (e.g. SetCurrent).
func mustCreate(t *testing.T, s *Store) *quest.Quest {
	t.Helper()
	q, err := s.Create("a task", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func TestBareIDResolvesThroughStore(t *testing.T) {
	s := newStore(t)
	q := mustCreate(t, s) // SQ-0001 under the default sequential strategy

	// Get accepts a bare and a zero-padded number, returning the canonical id.
	for _, raw := range []string{"1", "0001", "SQ-0001"} {
		got, err := s.Get(raw)
		if err != nil {
			t.Fatalf("Get(%q): %v", raw, err)
		}
		if got.ID != q.ID {
			t.Errorf("Get(%q).ID = %q, want %q", raw, got.ID, q.ID)
		}
	}

	// Update-based mutations accept the bare form too.
	if err := s.SetStatus("1", quest.StatusDone); err != nil {
		t.Fatalf("SetStatus(bare): %v", err)
	}
	if got, _ := s.Get(q.ID); got.Status != quest.StatusDone {
		t.Errorf("status after SetStatus(bare) = %q, want done", got.Status)
	}

	// SetCurrent normalizes before writing the pointer, so the stored value is
	// canonical (a bare id in the pointer would inject a dangling trailer).
	if err := s.SetCurrent("1"); err != nil {
		t.Fatalf("SetCurrent(bare): %v", err)
	}
	cur, err := s.Current()
	if err != nil {
		t.Fatal(err)
	}
	if cur != q.ID {
		t.Errorf("current pointer = %q, want canonical %q", cur, q.ID)
	}

	// An unknown number still fails, not silently resolving to something real.
	if _, err := s.Get("999"); err == nil {
		t.Error("Get(unknown bare id) should fail")
	}
}

func TestSnapshotEmptyBeforeInit(t *testing.T) {
	s := newStore(t)
	snap, err := s.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Tip != "" {
		t.Errorf("expected empty tip before init, got %q", snap.Tip)
	}
	if len(snap.IDs) != 0 {
		t.Errorf("expected no ids, got %v", snap.IDs)
	}
	// Defaults apply when no config exists yet.
	if snap.Config.IDStrategy != config.Sequential {
		t.Errorf("expected default strategy, got %v", snap.Config.IDStrategy)
	}
}

func TestInitCreatesRefAndConfig(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	snap, err := s.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Tip == "" {
		t.Fatal("ref not created by Init")
	}
	if snap.Config.SeqNext != 1 || snap.Config.IDPrefix != "SQ" {
		t.Errorf("unexpected initialized config: %+v", snap.Config)
	}
}

// TestInitDefaultsToRandomWithRemote (SQ-0030): a configured remote signals a
// team/shared workflow, where sequential ids collide across offline clones, so
// Init should default to the random strategy. Without a remote it stays sequential.
func TestInitDefaultsToRandomWithRemote(t *testing.T) {
	dir := t.TempDir()
	g := gitcmd.New(dir)
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "Tester"},
		{"remote", "add", "origin", t.TempDir()},
	} {
		if _, err := g.Run(args...); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.Config()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IDStrategy != config.Random {
		t.Errorf("Init with a remote should default to random ids, got %v", cfg.IDStrategy)
	}
}

func TestInitStaysSequentialWithoutRemote(t *testing.T) {
	s := newStore(t) // no remote configured
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.Config()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IDStrategy != config.Sequential {
		t.Errorf("Init without a remote should stay sequential, got %v", cfg.IDStrategy)
	}
}

func TestInitTwiceFails(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.Init(); err == nil {
		t.Fatal("second Init should fail (already initialized)")
	}
}

// TestInitConcurrentOnlyOneWins launches several Init() calls concurrently and
// asserts exactly one succeeds and the rest report "already initialized". The
// guard must live inside the mutate closure for this to hold.
func TestInitConcurrentOnlyOneWins(t *testing.T) {
	s := newStore(t)
	const n = 6
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() { errs <- s.Init() }()
	}
	success := 0
	for i := 0; i < n; i++ {
		if err := <-errs; err == nil {
			success++
		} else if !strings.Contains(err.Error(), "already initialized") {
			t.Errorf("unexpected error: %v", err)
		}
	}
	if success != 1 {
		t.Fatalf("expected exactly 1 successful Init, got %d", success)
	}
}

func TestCreateSequential(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	a, err := s.Create("first", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Create("second", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != "SQ-0001" || b.ID != "SQ-0002" {
		t.Fatalf("sequential ids wrong: %q, %q", a.ID, b.ID)
	}
	// Counter must have advanced on the ref.
	snap, _ := s.snapshot()
	if snap.Config.SeqNext != 3 {
		t.Errorf("seq_next: got %d want 3", snap.Config.SeqNext)
	}
}

func TestCreateRandom(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStrategy(config.Random); err != nil {
		t.Fatal(err)
	}
	q, err := s.Create("rand", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// SQ- + 6 hex chars.
	if len(q.ID) != len("SQ-")+6 {
		t.Fatalf("random id wrong shape: %q", q.ID)
	}
}

func TestCreatePersistsAndReloads(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	created, err := s.Create("persist me", "ctx", "", "", map[string]string{"area": "engine"})
	if err != nil {
		t.Fatal(err)
	}
	// Re-open the same repo to prove it was persisted on the ref.
	s2 := s // same Store; snapshot re-reads from the ref via git, proving on-disk persistence
	snap, err := s2.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.IDs) != 1 || snap.IDs[0] != created.ID {
		t.Fatalf("persisted ids wrong: %v", snap.IDs)
	}
	raw, err := s2.readFile(snap.Tip, questPath(created.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "title: persist me") {
		t.Fatalf("quest content not persisted: %s", raw)
	}
}

// TestCreateConcurrentNoDuplicateIDs launches several creates concurrently and
// asserts every id is unique — exercising the CAS retry loop under contention.
func TestCreateConcurrentNoDuplicateIDs(t *testing.T) {
	// Previously skipped on Windows: 8-way concurrent `git hash-object -w`
	// contends on loose-object file locking ("Permission denied"). commitTx now
	// retries that transient write (retryTransient), so the test runs on every
	// platform and the Windows CI job proves the fix end-to-end (SQ-0088).
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	const n = 8
	ids := make(chan string, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() { // goroutine: a lightweight concurrent function (like a green thread)
			q, err := s.Create("concurrent", "", "", "", nil)
			if err != nil {
				errs <- err
				return
			}
			ids <- q.ID
		}()
	}
	seen := map[string]bool{}
	for i := 0; i < n; i++ {
		select {
		case err := <-errs:
			t.Fatal(err)
		case id := <-ids:
			if seen[id] {
				t.Fatalf("duplicate id allocated: %q", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != n {
		t.Fatalf("expected %d unique ids, got %d", n, len(seen))
	}
}

// stubTransientSleep replaces the retry backoff with a no-op for the duration of
// a test and returns a restore func.
func stubTransientSleep(t *testing.T) {
	t.Helper()
	orig := transientSleep
	transientSleep = func(time.Duration) {}
	t.Cleanup(func() { transientSleep = orig })
}

func TestIsTransientGitWrite(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"hash-object: Permission denied", true},
		{"error: unable to create '.../objects/...': Permission denied", true},
		{"fatal: Unable to create '.../index.lock': File exists", true},
		{"cannot lock ref 'refs/side-quest/quests'", false}, // ref race, handled by CAS
		{"cannot update ref: nonexistent object", false},    // genuine fault
		{"", false},
	}
	for _, c := range cases {
		var err error
		if c.msg != "" {
			err = errors.New(c.msg)
		}
		if got := isTransientGitWrite(err); got != c.want {
			t.Errorf("isTransientGitWrite(%q) = %v, want %v", c.msg, got, c.want)
		}
	}
}

func TestRetryTransientSucceedsAfterContention(t *testing.T) {
	stubTransientSleep(t)
	calls := 0
	err := retryTransient(func() error {
		calls++
		if calls < 3 {
			return errors.New("hash-object: Permission denied")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("want success after transient retries, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("want 3 attempts, got %d", calls)
	}
}

func TestRetryTransientSurfacesNonTransientImmediately(t *testing.T) {
	stubTransientSleep(t)
	calls := 0
	sentinel := errors.New("cannot update ref: nonexistent object")
	err := retryTransient(func() error { calls++; return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel surfaced, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("a non-transient error must not be retried, got %d attempts", calls)
	}
}

func TestRetryTransientGivesUpAfterCap(t *testing.T) {
	stubTransientSleep(t)
	calls := 0
	err := retryTransient(func() error { calls++; return errors.New("Permission denied") })
	if err == nil {
		t.Fatal("want the last transient error surfaced after exhausting retries")
	}
	if calls != transientMaxTries {
		t.Fatalf("want %d attempts, got %d", transientMaxTries, calls)
	}
}

func TestReplaceOverwritesContent(t *testing.T) {
	s := newStore(t)
	q := mustCreate(t, s) // open/feature/low, title "a task"

	edited := *q
	edited.Title = "a much better title"
	edited.Status = quest.StatusPartial
	edited.Type = quest.TypeBug
	edited.Body = "a fresh body written in the editor"

	if err := s.Replace(q.ID, &edited); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	got, err := s.Get(q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "a much better title" || got.Status != quest.StatusPartial ||
		got.Type != quest.TypeBug || got.Body != "a fresh body written in the editor" {
		t.Fatalf("Replace did not overwrite content: %+v", got)
	}
}

func TestReplaceValidatesAtWriteBoundary(t *testing.T) {
	s := newStore(t)
	q := mustCreate(t, s)

	bad := *q
	bad.Status = quest.Status("bogus")
	if err := s.Replace(q.ID, &bad); err == nil {
		t.Error("Replace should reject an invalid status")
	}

	blank := *q
	blank.Title = "   "
	if err := s.Replace(q.ID, &blank); err == nil {
		t.Error("Replace should reject a blank title")
	}

	// The rejected writes must not have altered the stored quest.
	got, _ := s.Get(q.ID)
	if got.Title != q.Title || got.Status != q.Status {
		t.Fatalf("rejected Replace mutated the quest: %+v", got)
	}
}

func TestReplaceUnknownID(t *testing.T) {
	s := newStore(t)
	mustCreate(t, s)
	ghost := &quest.Quest{Title: "x", Status: quest.StatusOpen, Type: quest.TypeFeature, Priority: quest.PriorityLow}
	if err := s.Replace("SQ-9999", ghost); err != ErrNotFound {
		t.Errorf("Replace(unknown) = %v, want ErrNotFound", err)
	}
}

func TestGetAndList(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	a, _ := s.Create("alpha", "", "", "", nil)
	b, _ := s.Create("bravo", "", "", "", nil)

	got, err := s.Get(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "alpha" {
		t.Errorf("Get returned wrong quest: %+v", got)
	}
	if _, err := s.Get("SQ-9999"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != a.ID || list[1].ID != b.ID {
		t.Fatalf("List wrong: %v", list)
	}
}

func TestSetStatusSetsCompletedOnDone(t *testing.T) {
	s := newStore(t)
	_ = s.Init()
	q, _ := s.Create("finish me", "", "", "", nil)

	if err := s.SetStatus(q.ID, quest.StatusDone); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(q.ID)
	if got.Status != quest.StatusDone {
		t.Errorf("status not set: %v", got.Status)
	}
	if got.Completed == nil {
		t.Error("completed timestamp should be set when moving to done")
	}

	if err := s.SetStatus(q.ID, quest.Status("bogus")); err == nil {
		t.Error("invalid status should error")
	}
}

func TestAddCommitAppendsAndDedupes(t *testing.T) {
	s := newStore(t)
	_ = s.Init()
	q, _ := s.Create("linkme", "", "", "", nil)

	if err := s.AddCommit(q.ID, "abc123", LinkTouch); err != nil {
		t.Fatal(err)
	}
	if err := s.AddCommit(q.ID, "abc123", LinkTouch); err != nil { // duplicate
		t.Fatal(err)
	}
	if err := s.AddCommit(q.ID, "def456", LinkComplete); err != nil { // completing link
		t.Fatal(err)
	}
	got, _ := s.Get(q.ID)
	if len(got.Commits) != 2 {
		t.Fatalf("commits not deduped: %v", got.Commits)
	}
	if got.Status != quest.StatusDone || got.Completed == nil {
		t.Errorf("completing link should mark done: %+v", got)
	}
}

// TestAddCommitPromotesOpenToPartial (SQ-0094): a non-closing linked commit advances
// an untouched open quest to partial ("work has started"), mirroring how a closing
// commit sets done — but it only promotes from open, never churning or resurrecting a
// partial/deferred/done quest.
func TestAddCommitPromotesOpenToPartial(t *testing.T) {
	s := newStore(t)
	_ = s.Init()
	q, _ := s.Create("wip", "", "", "", nil)
	if q.Status != quest.StatusOpen {
		t.Fatalf("new quest status = %q, want open", q.Status)
	}

	// A non-closing link promotes open -> partial.
	if err := s.AddCommit(q.ID, "aaa111", LinkTouch); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(q.ID); got.Status != quest.StatusPartial {
		t.Fatalf("after a linked commit, status = %q, want partial", got.Status)
	}
	// A second non-closing link leaves it partial (no churn).
	if err := s.AddCommit(q.ID, "bbb222", LinkTouch); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(q.ID); got.Status != quest.StatusPartial {
		t.Errorf("status churned off partial: %q", got.Status)
	}

	// A non-closing link must NOT resurrect a deferred quest.
	d, _ := s.Create("later", "", "", "", nil)
	if err := s.SetStatus(d.ID, quest.StatusDeferred); err != nil {
		t.Fatal(err)
	}
	if err := s.AddCommit(d.ID, "ccc333", LinkTouch); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(d.ID); got.Status != quest.StatusDeferred {
		t.Errorf("non-closing commit changed a deferred quest to %q, want deferred", got.Status)
	}
}

// TestAddCommitConfirm (SQ-0110): a Confirm: link parks the quest in `confirm` for
// user sign-off — outstanding, not done, and never timestamped as completed. It moves
// any non-done quest (matching Completes:' explicit-override intent) but leaves an
// already-done quest untouched.
func TestAddCommitConfirm(t *testing.T) {
	s := newStore(t)
	_ = s.Init()

	// open -> confirm, with no completion stamp.
	q, _ := s.Create("signme", "", "", "", nil)
	if err := s.AddCommit(q.ID, "aaa111", LinkConfirm); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(q.ID)
	if got.Status != quest.StatusConfirm {
		t.Fatalf("after Confirm link, status = %q, want confirm", got.Status)
	}
	if got.Completed != nil {
		t.Errorf("confirm must not stamp Completed: %v", got.Completed)
	}

	// A done quest is not downgraded to confirm.
	d, _ := s.Create("finished", "", "", "", nil)
	if err := s.SetStatus(d.ID, quest.StatusDone); err != nil {
		t.Fatal(err)
	}
	if err := s.AddCommit(d.ID, "bbb222", LinkConfirm); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(d.ID); got.Status != quest.StatusDone {
		t.Errorf("Confirm link downgraded a done quest to %q, want done", got.Status)
	}
}

// TestReplaceCommit (SQ-0048): a rebase rewrites a linked commit's sha, leaving
// the old (now-dangling) sha recorded. ReplaceCommit swaps it for the new one by
// prefix — without asking git to resolve the dead old sha — preserving order and
// deduping if the new sha was already present.
func TestReplaceCommit(t *testing.T) {
	s := newStore(t)
	_ = s.Init()
	q, _ := s.Create("rebased", "", "", "", nil)
	old := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	keep := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	fresh := "cccccccccccccccccccccccccccccccccccccccc"
	_ = s.AddCommit(q.ID, old, LinkTouch)
	_ = s.AddCommit(q.ID, keep, LinkTouch)

	// prefix match on the dead old sha, replaced in place with the fresh one
	if err := s.ReplaceCommit(q.ID, "aaaaaaa", fresh); err != nil {
		t.Fatalf("ReplaceCommit: %v", err)
	}
	got, _ := s.Get(q.ID)
	if len(got.Commits) != 2 || got.Commits[0] != fresh || got.Commits[1] != keep {
		t.Fatalf("replace wrong/position not preserved: %v", got.Commits)
	}

	// no match surfaces an error, doesn't silently no-op
	if err := s.ReplaceCommit(q.ID, "deadbeef", fresh); err == nil {
		t.Error("ReplaceCommit with no matching old sha should error")
	}
}

// TestRemoveCommit (SQ-0048): unlink a recorded commit by prefix, erroring when
// nothing matches.
func TestRemoveCommit(t *testing.T) {
	s := newStore(t)
	_ = s.Init()
	q, _ := s.Create("unlinkme", "", "", "", nil)
	a := "1111111111111111111111111111111111111111"
	b := "2222222222222222222222222222222222222222"
	_ = s.AddCommit(q.ID, a, LinkTouch)
	_ = s.AddCommit(q.ID, b, LinkTouch)

	if err := s.RemoveCommit(q.ID, "1111111"); err != nil {
		t.Fatalf("RemoveCommit: %v", err)
	}
	got, _ := s.Get(q.ID)
	if len(got.Commits) != 1 || got.Commits[0] != b {
		t.Fatalf("remove wrong: %v", got.Commits)
	}
	if err := s.RemoveCommit(q.ID, "9999999"); err == nil {
		t.Error("RemoveCommit with no match should error")
	}
}

func TestSetStrategyPreservesSeqNext(t *testing.T) {
	s := newStore(t)
	_ = s.Init()
	_, _ = s.Create("one", "", "", "", nil) // seq_next -> 2

	if err := s.SetStrategy(config.Random); err != nil {
		t.Fatal(err)
	}
	snap, _ := s.snapshot()
	if snap.Config.IDStrategy != config.Random {
		t.Errorf("strategy not switched: %v", snap.Config.IDStrategy)
	}
	if snap.Config.SeqNext != 2 {
		t.Errorf("seq_next must be preserved across switch, got %d", snap.Config.SeqNext)
	}
}

func TestSetTone(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTone(config.TonePlain); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.Config()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tone != config.TonePlain {
		t.Errorf("Tone = %q, want plain", cfg.Tone)
	}
}

// TestCreateDoesNotTouchWorkingTree proves the store's whole reason to exist:
// writes go only to refs/side-quest/quests via a scratch index, never the user's
// working tree or real index. After Create, `git status --porcelain` must be empty.
func TestCreateDoesNotTouchWorkingTree(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("no side effects", "", "", "", nil); err != nil {
		t.Fatal(err)
	}
	out, err := s.git.Run("status", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Fatalf("working tree/index was modified by the store: %q", out)
	}
	// And the quest really was persisted to the ref.
	snap, err := s.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Tip == "" || len(snap.IDs) != 1 {
		t.Fatalf("quest not persisted to ref: tip=%q ids=%v", snap.Tip, snap.IDs)
	}
}

// TestConcurrentUpdateSameQuest fires N concurrent AddCommit calls at ONE quest
// with distinct shas and asserts none are lost — the classic lost-update case the
// CAS re-read loop must handle.
func TestConcurrentUpdateSameQuest(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	q, err := s.Create("target", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	const n = 8
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- s.AddCommit(q.ID, fmt.Sprintf("sha%02d", i), LinkTouch)
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Get(q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Commits) != n {
		t.Fatalf("lost updates: got %d commits, want %d (%v)", len(got.Commits), n, got.Commits)
	}
}

func TestSetRequireQuestPersists(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.Config()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RequireQuest {
		t.Fatal("fresh store should have require_quest=false")
	}
	if err := s.SetRequireQuest(true); err != nil {
		t.Fatal(err)
	}
	cfg, err = s.Config()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.RequireQuest {
		t.Fatal("SetRequireQuest(true) did not persist")
	}
}

func TestSetAutoTrailerPersists(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.Config()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AutoTrailer {
		t.Fatal("fresh store should have auto_trailer=true (Default)")
	}
	if err := s.SetAutoTrailer(false); err != nil {
		t.Fatal(err)
	}
	cfg, err = s.Config()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AutoTrailer {
		t.Fatal("SetAutoTrailer(false) did not persist")
	}
}

func TestConfigEmptyStoreIsDefault(t *testing.T) {
	s := newStore(t) // not initialized
	cfg, err := s.Config()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IDPrefix != "SQ" || cfg.RequireQuest {
		t.Fatalf("empty-store Config should be Default(): %+v", cfg)
	}
}

// TestStrategySwitchRoundTripResumesCounter verifies the spec §7 promise: after
// switching sequential -> random -> sequential, sequential ids resume from the
// preserved counter rather than restarting.
func TestStrategySwitchRoundTripResumesCounter(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	a, _ := s.Create("one", "", "", "", nil) // SQ-0001, seq_next -> 2
	if a.ID != "SQ-0001" {
		t.Fatalf("first id: got %q want SQ-0001", a.ID)
	}
	if err := s.SetStrategy(config.Random); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("two", "", "", "", nil); err != nil { // random id
		t.Fatal(err)
	}
	if err := s.SetStrategy(config.Sequential); err != nil {
		t.Fatal(err)
	}
	c, err := s.Create("three", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != "SQ-0002" {
		t.Fatalf("counter not resumed after round-trip: got %q want SQ-0002", c.ID)
	}
}

func TestCreateAppliesTypePriorityDefaults(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	q, err := s.Create("defaulted", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if q.Type != quest.TypeFeature {
		t.Errorf("default type: got %q want feature", q.Type)
	}
	if q.Priority != quest.PriorityLow {
		t.Errorf("default priority: got %q want low", q.Priority)
	}
}

func TestCreatePersistsExplicitTypePriority(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	q, err := s.Create("explicit", "", quest.TypeBug, quest.PriorityHigh, nil)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := s.Get(q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Type != quest.TypeBug || reloaded.Priority != quest.PriorityHigh {
		t.Errorf("persisted type/priority wrong: %q/%q", reloaded.Type, reloaded.Priority)
	}
}

func TestCreateRejectsInvalidTypePriority(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("bad type", "", quest.Type("chore"), "", nil); err == nil {
		t.Error("expected error for invalid type")
	}
	if _, err := s.Create("bad prio", "", "", quest.Priority("urgent"), nil); err == nil {
		t.Error("expected error for invalid priority")
	}
	// Nothing should have been written by the rejected creates.
	snap, err := s.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.IDs) != 0 {
		t.Errorf("rejected creates wrote quests: %v", snap.IDs)
	}
}

// commitCount returns how many commits the ref advanced from tip old to the
// current tip — used to prove a multi-field change lands as ONE commit.
func commitCount(t *testing.T, s *Store, old string) int {
	t.Helper()
	now, err := s.tip()
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.git.Run("rev-list", "--count", old+".."+now)
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestReclassifySetsBothInOneCommit(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	q, err := s.Create("reclassify me", "", "", "", nil) // defaults: feature/low
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.tip()
	if err := s.Reclassify(q.ID, quest.TypeBug, quest.PriorityHigh); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != quest.TypeBug || got.Priority != quest.PriorityHigh {
		t.Errorf("after reclassify: got %q/%q want bug/high", got.Type, got.Priority)
	}
	if n := commitCount(t, s, before); n != 1 {
		t.Errorf("reclassify of two fields must be one commit, got %d", n)
	}
}

// Reclassify must be atomic: a valid type paired with an invalid priority is
// rejected whole, leaving BOTH fields untouched — never landing the type change
// and then failing on the priority.
func TestReclassifyRejectsInvalidAtomically(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	q, err := s.Create("keep me", "", "", "", nil) // defaults: feature/low
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.tip()
	if err := s.Reclassify(q.ID, quest.TypeBug, quest.Priority("urgent")); err == nil {
		t.Fatal("expected error for invalid priority")
	}
	got, err := s.Get(q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != quest.TypeFeature || got.Priority != quest.PriorityLow {
		t.Errorf("rejected reclassify mutated the quest: %q/%q", got.Type, got.Priority)
	}
	if n := commitCount(t, s, before); n != 0 {
		t.Errorf("rejected reclassify must write nothing, got %d commits", n)
	}
}

// Reclassify treats an empty field as "leave unchanged", so a single-field
// change touches only that field.
func TestReclassifyEmptyFieldLeavesUnchanged(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	q, err := s.Create("half", "", quest.TypeBug, quest.PriorityHigh, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Reclassify(q.ID, "", quest.PriorityLow); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(q.ID)
	if got.Type != quest.TypeBug || got.Priority != quest.PriorityLow {
		t.Errorf("empty type should be left unchanged: got %q/%q want bug/low", got.Type, got.Priority)
	}
}

func TestAppendNoteAccumulates(t *testing.T) {
	s := newStore(t)
	q, err := s.Create("noted", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendNote(q.ID, "first finding"); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendNote(q.ID, "second finding"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Body, "first finding") || !strings.Contains(got.Body, "second finding") {
		t.Fatalf("both notes should survive, body=%q", got.Body)
	}
	if err := s.AppendNote(q.ID, "  "); err == nil {
		t.Fatal("empty note text should error")
	}
}

func TestModifyTitleAndTagsInOneCommit(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	q, _ := s.Create("old", "", "", "", map[string]string{"area": "cli", "keep": "yes"})
	before, _ := s.tip()
	err := s.Modify(q.ID, "new title", map[string]string{"area": "mcp", "new": "x", "keep": ""})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(q.ID)
	if got.Title != "new title" {
		t.Fatalf("title not replaced: %q", got.Title)
	}
	if got.Tags["area"] != "mcp" || got.Tags["new"] != "x" {
		t.Fatalf("merge/overwrite wrong: %+v", got.Tags)
	}
	if _, ok := got.Tags["keep"]; ok {
		t.Fatalf("empty value should delete key: %+v", got.Tags)
	}
	if n := commitCount(t, s, before); n != 1 {
		t.Errorf("title+tags change must be one commit, got %d", n)
	}
}

// Modify must be atomic: a whitespace-only title is rejected whole, leaving the
// title AND the tags untouched.
func TestModifyRejectsBlankTitleAtomically(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	q, _ := s.Create("keep", "", "", "", map[string]string{"area": "cli"})
	before, _ := s.tip()
	if err := s.Modify(q.ID, "   ", map[string]string{"area": "mcp"}); err == nil {
		t.Fatal("blank title should error")
	}
	got, _ := s.Get(q.ID)
	if got.Title != "keep" || got.Tags["area"] != "cli" {
		t.Errorf("rejected modify mutated the quest: %q %+v", got.Title, got.Tags)
	}
	if n := commitCount(t, s, before); n != 0 {
		t.Errorf("rejected modify must write nothing, got %d commits", n)
	}
}

// An empty title means "leave the title unchanged" — only the tags move.
func TestModifyEmptyTitleKeepsTitle(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	q, _ := s.Create("stays", "", "", "", nil)
	if err := s.Modify(q.ID, "", map[string]string{"area": "mcp"}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(q.ID)
	if got.Title != "stays" || got.Tags["area"] != "mcp" {
		t.Errorf("empty title should keep title, tags should merge: %q %+v", got.Title, got.Tags)
	}
}
