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
