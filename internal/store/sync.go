// Package store — sync.go adds the git plumbing for the cross-clone three-way
// merge (design spec 2026-07-03-side-quest-sync). It reads snapshots at arbitrary
// commits, drives internal/merge, records the result as a two-parent merge
// commit, and publishes it with a fetch-merge-retry loop.
package store

import (
	"fmt"
	"strings"
	"time"

	"github.com/sharkusk/side-quest/internal/config"
	"github.com/sharkusk/side-quest/internal/merge"
	"github.com/sharkusk/side-quest/internal/quest"
)

// TrackingRef is where a fetch lands the remote quest ref, kept separate from the
// live Ref so the network never clobbers local quests.
const TrackingRef = "refs/side-quest-remote/quests"

// FetchRefspec maps the remote quest ref into the local tracking ref.
const FetchRefspec = Ref + ":" + TrackingRef

// sideAt reads the config and every quest present in the tree at commit into a
// merge.Side. An empty commit yields a zero Side (used as the no-common-ancestor
// base). Touch/ConfigTouch are left empty; callers fill them for the conflict set.
func (s *Store) sideAt(commit string) (merge.Side, error) {
	side := merge.Side{Config: config.Default(), Quests: map[string]*quest.Quest{}, Touch: map[string]time.Time{}}
	if commit == "" {
		return side, nil
	}
	if raw, err := s.readFile(commit, configPath); err == nil {
		cfg, err := config.Unmarshal(raw)
		if err != nil {
			return side, err
		}
		side.Config = cfg
	}
	ids, err := s.listIDs(commit)
	if err != nil {
		return side, err
	}
	for _, id := range ids {
		raw, err := s.readFile(commit, questPath(id))
		if err != nil {
			return side, err
		}
		q, err := quest.Unmarshal(id, raw)
		if err != nil {
			return side, err
		}
		side.Quests[id] = q
	}
	return side, nil
}

// fillTouch populates side.Touch for the given ids and side.ConfigTouch, reading
// the committer time of the last commit that changed each path at/under commit.
func (s *Store) fillTouch(side *merge.Side, commit string, ids []string) error {
	for _, id := range ids {
		t, err := s.lastTouch(commit, questPath(id))
		if err != nil {
			return err
		}
		side.Touch[id] = t
	}
	if t, err := s.lastTouch(commit, configPath); err == nil {
		side.ConfigTouch = t
	}
	return nil
}

// lastTouch returns the committer time (RFC3339, parsed) of the most recent
// commit reachable from commit that modified path.
func (s *Store) lastTouch(commit, path string) (time.Time, error) {
	out, err := s.git.Run("log", "-1", "--format=%cI", commit, "--", path)
	if err != nil {
		return time.Time{}, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return time.Time{}, fmt.Errorf("no commit touched %s", path)
	}
	return time.Parse(time.RFC3339, out)
}
