package store

import (
	"fmt"
	"strings"
	"sync"
	"testing"

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

	if err := s.AddCommit(q.ID, "abc123", false); err != nil {
		t.Fatal(err)
	}
	if err := s.AddCommit(q.ID, "abc123", false); err != nil { // duplicate
		t.Fatal(err)
	}
	if err := s.AddCommit(q.ID, "def456", true); err != nil { // completing link
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
			errs <- s.AddCommit(q.ID, fmt.Sprintf("sha%02d", i), false)
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

func TestSetTypeAndPriority(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	q, err := s.Create("reclassify me", "", "", "", nil) // defaults: feature/low
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetType(q.ID, quest.TypeBug); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPriority(q.ID, quest.PriorityHigh); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != quest.TypeBug || got.Priority != quest.PriorityHigh {
		t.Errorf("after set: got %q/%q want bug/high", got.Type, got.Priority)
	}
}

func TestSetTypeAndPriorityRejectInvalid(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	q, err := s.Create("keep me", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetType(q.ID, quest.Type("chore")); err == nil {
		t.Error("expected error for invalid type")
	}
	if err := s.SetPriority(q.ID, quest.Priority("urgent")); err == nil {
		t.Error("expected error for invalid priority")
	}
	// The quest keeps its original defaulted values.
	got, err := s.Get(q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != quest.TypeFeature || got.Priority != quest.PriorityLow {
		t.Errorf("rejected sets mutated the quest: %q/%q", got.Type, got.Priority)
	}
}
