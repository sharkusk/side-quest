package store

import (
	"os"
	"path/filepath"
	"strings"
)

// The current-quest pointer is WORKTREE-LOCAL state, not ref state: it records
// which quest this worktree is "on" so prepare-commit-msg can auto-fill the
// Quest: trailer. It lives in the worktree's git dir (NOT on the orphan ref and
// NOT in the working tree), so each worktree/lane has its own and it never
// travels with a push. s.gitDir is already the per-worktree git dir
// (rev-parse --absolute-git-dir), so this is worktree-scoped for free.
func (s *Store) currentPath() string {
	return filepath.Join(s.gitDir, "side-quest-current")
}

// SetCurrent records id as this worktree's active quest. It normalizes the id to
// canonical form (so "11" is stored as "SQ-0011", not injected verbatim as a
// dangling trailer) and verifies the quest exists on the ref: pointing at a
// typo'd or stale id would silently arm prepare-commit-msg to inject a bad
// Quest: trailer, so a missing id is rejected with ErrNotFound and the pointer
// is left untouched.
func (s *Store) SetCurrent(id string) error {
	id, err := s.canonicalID(id)
	if err != nil {
		return err
	}
	if _, err := s.Get(id); err != nil {
		return err
	}
	return os.WriteFile(s.currentPath(), []byte(id+"\n"), 0o644)
}

// Current returns the worktree's active quest id, or "" if none is set.
func (s *Store) Current() (string, error) {
	b, err := os.ReadFile(s.currentPath())
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// ClearCurrent removes the pointer; it is not an error if none was set.
func (s *Store) ClearCurrent() error {
	if err := os.Remove(s.currentPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
