package store

import (
	"strings"
	"testing"

	"github.com/sharkusk/side-quest/internal/config"
	"github.com/sharkusk/side-quest/internal/gitcmd"
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
	a, err := s.Create("first", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Create("second", "", nil)
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
	if err := s.SetStrategyForTest(t); err != nil { // helper flips to random
		t.Fatal(err)
	}
	q, err := s.Create("rand", "", nil)
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
	created, err := s.Create("persist me", "ctx", map[string]string{"area": "engine"})
	if err != nil {
		t.Fatal(err)
	}
	// Re-open the same repo to prove it was persisted on the ref.
	s2 := s // same dir; snapshot reads from git, not memory
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
			q, err := s.Create("concurrent", "", nil)
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

// SetStrategyForTest flips the on-ref strategy to random by rewriting config.
// (A public SetStrategy lands in Task 7; this keeps Task 6's test self-contained.)
func (s *Store) SetStrategyForTest(t *testing.T) error {
	t.Helper()
	return s.mutate("test: strategy random", func(snap *Snapshot, tx *txn) error {
		cfg := snap.Config
		cfg.IDStrategy = config.Random
		b, err := config.Marshal(cfg)
		if err != nil {
			return err
		}
		tx.put(configPath, b)
		return nil
	})
}
