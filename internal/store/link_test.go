package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sharkusk/side-quest/internal/quest"
)

// commitInWorktree makes a real commit on the working branch (not the orphan
// ref) with the given message, and returns its full sha. It writes a unique
// file so each commit has content.
func commitInWorktree(t *testing.T, s *Store, filename, message string) string {
	t.Helper()
	top, err := s.git.Run("rev-parse", "--show-toplevel")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(top, filename), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.git.Run("add", filename); err != nil {
		t.Fatal(err)
	}
	if _, err := s.git.Run("commit", "-m", message); err != nil {
		t.Fatal(err)
	}
	sha, err := s.git.Run("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return sha
}

func TestLinkCompletesClosesQuest(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	q, err := s.Create("close me", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	sha := commitInWorktree(t, s, "a.txt", "work\n\nCompletes: "+q.ID+"\n")

	if err := s.Link("HEAD"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != quest.StatusDone || got.Completed == nil {
		t.Fatalf("Completes: should close the quest: %+v", got)
	}
	if len(got.Commits) != 1 || got.Commits[0] != sha {
		t.Fatalf("commit hash not linked: %v (want %s)", got.Commits, sha)
	}
}

func TestLinkQuestAppendsWithoutClosing(t *testing.T) {
	s := newStore(t)
	_ = s.Init()
	q, _ := s.Create("ongoing", "", nil)
	commitInWorktree(t, s, "b.txt", "progress\n\nQuest: "+q.ID+"\n")

	if err := s.Link("HEAD"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(q.ID)
	if got.Status != quest.StatusOpen {
		t.Fatalf("Quest: (not Completes) must not close: %+v", got)
	}
	if len(got.Commits) != 1 {
		t.Fatalf("expected 1 linked commit, got %v", got.Commits)
	}
}

func TestLinkUnknownIDIsTolerant(t *testing.T) {
	s := newStore(t)
	_ = s.Init()
	commitInWorktree(t, s, "c.txt", "typo\n\nCompletes: SQ-9999\n")
	// Referenced quest does not exist; Link must not error (commit already made).
	if err := s.Link("HEAD"); err != nil {
		t.Fatalf("Link should tolerate unknown ids, got %v", err)
	}
}

func TestLinkNoTrailerIsNoop(t *testing.T) {
	s := newStore(t)
	_ = s.Init()
	commitInWorktree(t, s, "d.txt", "no trailer here\n")
	if err := s.Link("HEAD"); err != nil {
		t.Fatalf("no-trailer commit should be a no-op, got %v", err)
	}
}

// TestLinkIgnoresInheritedIndexFile proves the Task 1 hardening in the real
// hook scenario: even if GIT_INDEX_FILE is set in the environment (as git does
// inside hooks), Link's mutation uses its own scratch index and succeeds
// without touching that index.
func TestLinkIgnoresInheritedIndexFile(t *testing.T) {
	s := newStore(t)
	_ = s.Init()
	q, _ := s.Create("hooked", "", nil)
	// Make the commit BEFORE setting the bogus index (git add/commit need the
	// real index).
	commitInWorktree(t, s, "e.txt", "work\n\nCompletes: "+q.ID+"\n")

	bogus := filepath.Join(t.TempDir(), "nonexistent-index")
	os.Setenv("GIT_INDEX_FILE", bogus)
	err := s.Link("HEAD")
	os.Unsetenv("GIT_INDEX_FILE")
	if err != nil {
		t.Fatalf("Link failed under inherited GIT_INDEX_FILE: %v", err)
	}
	got, _ := s.Get(q.ID)
	if got.Status != quest.StatusDone {
		t.Fatalf("link did not apply under inherited index: %+v", got)
	}
}
