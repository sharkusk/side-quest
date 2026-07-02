package store

import (
	"errors"

	"github.com/sharkusk/side-quest/internal/trailer"
)

// Link applies a commit's side-quest trailers to the store: for every
// Quest:/Completes: trailer in the commit's message, it appends the commit's
// canonical hash to that quest and, for Completes:, closes the quest.
//
// This is the post-commit entry point where the chicken-and-egg is resolved:
// the commit already exists (its hash is known), and the quest update is a
// SEPARATE commit on the orphan ref whose own hash nobody has to record.
//
// Link is deliberately TOLERANT: a trailer naming a quest that does not exist
// (a typo, or an id from another clone) is skipped — post-commit must never
// fail the user's already-made commit over a bad reference. Genuine errors
// (anything other than "not found") are surfaced.
func (s *Store) Link(sha string) error {
	full, err := s.git.Run("rev-parse", sha)
	if err != nil {
		return err
	}
	msg, err := s.git.Run("show", "-s", "--format=%B", full)
	if err != nil {
		return err
	}
	refs, _ := trailer.Parse(msg)
	for _, r := range refs {
		if err := s.AddCommit(r.ID, full, r.Completes); err != nil {
			if errors.Is(err, ErrNotFound) {
				continue // unknown id — skip, keep processing other refs
			}
			return err
		}
	}
	return nil
}
