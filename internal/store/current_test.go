package store

import "testing"

func TestCurrentRoundTrip(t *testing.T) {
	s := newStore(t)
	cur, err := s.Current()
	if err != nil {
		t.Fatal(err)
	}
	if cur != "" {
		t.Fatalf("fresh worktree should have no current quest, got %q", cur)
	}
	if err := s.SetCurrent("SQ-0007"); err != nil {
		t.Fatal(err)
	}
	cur, err = s.Current()
	if err != nil {
		t.Fatal(err)
	}
	if cur != "SQ-0007" {
		t.Fatalf("current not persisted: %q", cur)
	}
}

func TestClearCurrent(t *testing.T) {
	s := newStore(t)
	if err := s.SetCurrent("SQ-0001"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearCurrent(); err != nil {
		t.Fatal(err)
	}
	cur, _ := s.Current()
	if cur != "" {
		t.Fatalf("expected cleared, got %q", cur)
	}
	// Clearing again is not an error.
	if err := s.ClearCurrent(); err != nil {
		t.Fatalf("double clear should be a no-op, got %v", err)
	}
}

// The pointer must NOT touch the orphan ref or the working tree.
func TestSetCurrentDoesNotTouchRefOrTree(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	before, _ := s.tip()
	if err := s.SetCurrent("SQ-0003"); err != nil {
		t.Fatal(err)
	}
	after, _ := s.tip()
	if before != after {
		t.Fatal("SetCurrent must not move the orphan ref")
	}
	out, _ := s.git.Run("status", "--porcelain")
	if out != "" {
		t.Fatalf("SetCurrent modified the working tree/index: %q", out)
	}
}
