package store

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sharkusk/side-quest/internal/quest"
	"github.com/sharkusk/side-quest/internal/trailer"
)

// LinkResult reports what Link did: which quest ids the commit was linked to
// (post-normalization) and which trailer ids named no existing quest. Skipped
// ids exist so callers can WARN — a silently dropped "Completes:" was the worst
// failure mode of the old void-returning Link (SQ-0119): the commit-msg hook had
// accepted the trailer, so the user believed it worked.
type LinkResult struct {
	Linked  []string
	Skipped []string
}

// Link applies a commit's side-quest trailers to the store: for every
// Quest:/Confirm:/Completes: trailer in the commit's message, it appends the
// commit's canonical hash to that quest and moves its status accordingly —
// Confirm: parks it in `confirm` for sign-off, Completes: closes it.
//
// This is the post-commit entry point where the chicken-and-egg is resolved:
// the commit already exists (its hash is known), and the quest update is a
// SEPARATE commit on the orphan ref whose own hash nobody has to record.
//
// Link is deliberately TOLERANT: a trailer naming a quest that does not exist
// (a typo, or an id from another clone) is reported in Skipped rather than
// failing — post-commit must never fail the user's already-made commit over a
// bad reference. Genuine errors (anything other than "not found") are surfaced.
func (s *Store) Link(sha string) (LinkResult, error) {
	var res LinkResult
	full, err := s.git.Run("rev-parse", sha)
	if err != nil {
		return res, err
	}
	msg, err := s.git.Run("show", "-s", "--format=%B", full)
	if err != nil {
		return res, err
	}
	refs, _ := trailer.Parse(msg)
	for _, r := range refs {
		kind := LinkTouch
		switch {
		case r.Completes:
			kind = LinkComplete
		case r.Confirms:
			kind = LinkConfirm
		}
		id, err := s.canonicalID(r.ID)
		if err != nil {
			return res, err
		}
		if err := s.AddCommit(id, full, kind); err != nil {
			if errors.Is(err, ErrNotFound) {
				res.Skipped = append(res.Skipped, r.ID)
				continue // unknown id — report, keep processing other refs
			}
			return res, err
		}
		res.Linked = append(res.Linked, id)
	}
	return res, nil
}

// ResolveCommit canonicalizes a sha or ref to its full commit hash via git, the
// same resolution Link applies — so a caller can normalize a user-supplied new
// sha to the stored form before a ReplaceCommit.
func (s *Store) ResolveCommit(sha string) (string, error) {
	return s.git.Run("rev-parse", sha)
}

// ReplaceCommit swaps a recorded commit for a new one — the corrective a rebase
// needs, since rewriting history leaves the old (now-dangling) sha recorded with
// no way to reach the new one. oldPrefix matches a stored hash by prefix, so it
// works even after the old commit is gone from the object store (no git resolve
// on it); newSha takes its slot, order preserved, deduped if already present.
// Errors if oldPrefix matches nothing or is ambiguous (SQ-0048).
func (s *Store) ReplaceCommit(id, oldPrefix, newSha string) error {
	q, err := s.Get(id)
	if err != nil {
		return err
	}
	full, err := matchCommit(q.Commits, oldPrefix)
	if err != nil {
		return err
	}
	return s.Update(id, func(q *quest.Quest) {
		seen := make(map[string]bool, len(q.Commits))
		out := q.Commits[:0]
		for _, c := range q.Commits {
			if c == full {
				c = newSha
			}
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
		q.Commits = out
	})
}

// RemoveCommit unlinks a recorded commit from a quest, matching shaPrefix against
// the stored hashes by prefix. Errors if nothing matches or the prefix is
// ambiguous (SQ-0048).
func (s *Store) RemoveCommit(id, shaPrefix string) error {
	q, err := s.Get(id)
	if err != nil {
		return err
	}
	full, err := matchCommit(q.Commits, shaPrefix)
	if err != nil {
		return err
	}
	return s.Update(id, func(q *quest.Quest) {
		out := q.Commits[:0]
		for _, c := range q.Commits {
			if c != full {
				out = append(out, c)
			}
		}
		q.Commits = out
	})
}

// matchCommit resolves a user-supplied sha prefix to the single stored commit it
// names, or an error when it matches none or more than one. A minimum length
// guards against a stray short string sweeping up an unintended commit.
func matchCommit(commits []string, prefix string) (string, error) {
	if len(prefix) < 4 {
		return "", fmt.Errorf("commit %q is too short (give at least 4 characters)", prefix)
	}
	var match string
	for _, c := range commits {
		if strings.HasPrefix(c, prefix) {
			if match != "" && match != c {
				return "", fmt.Errorf("commit %q is ambiguous — it matches several linked commits", prefix)
			}
			match = c
		}
	}
	if match == "" {
		return "", fmt.Errorf("no linked commit matches %q", prefix)
	}
	return match, nil
}
