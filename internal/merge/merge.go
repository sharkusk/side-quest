// Package merge is the pure, git-free three-way merge engine for the quest ref
// (design spec 2026-07-03-side-quest-sync §5). Every rule here is a deterministic
// function of its inputs so that two clones merging the same divergence converge
// on byte-identical results regardless of which side each calls "local".
package merge

import (
	"bytes"
	"sort"
	"time"

	"github.com/sharkusk/side-quest/internal/config"
	"github.com/sharkusk/side-quest/internal/quest"
)

// Side is one snapshot of the store the merge needs from a single commit.
// Touch holds, per quest id, the commit time of the last commit that modified
// quests/<id>.md on this side; it is consulted only for the both-sides-changed
// case, so the plumbing layer may leave it empty for non-conflicting quests.
type Side struct {
	Config      config.Config
	Quests      map[string]*quest.Quest
	Touch       map[string]time.Time
	ConfigTouch time.Time
}

// Result is the merged snapshot: the tree the plumbing layer will commit.
type Result struct {
	Config config.Config
	Quests map[string]*quest.Quest
}

// EventKind classifies a reportable merge outcome.
type EventKind int

const (
	Renamed    EventKind = iota // a quest was re-keyed to resolve an id collision
	Conflicted                  // both sides changed a quest; one side's scalars won
)

// Event is a human-reportable note about how the merge resolved something.
type Event struct {
	Kind   EventKind
	ID     string // the resulting id (post-rename for Renamed)
	Detail string
}

// canonical returns q's marshaled bytes (id excluded, per the file format), the
// stable basis for equality, tiebreaks, and the collision id. A marshal error is
// impossible for a well-formed quest; nil bytes simply read as "not equal".
func canonical(q *quest.Quest) []byte {
	b, err := quest.Marshal(q)
	if err != nil {
		return nil
	}
	return b
}

// equalQuest reports whether a and b are the same quest content (id aside).
func equalQuest(a, b *quest.Quest) bool {
	if a == nil || b == nil {
		return a == b
	}
	return bytes.Equal(canonical(a), canonical(b))
}

// Merge performs the three-way merge. base may be the zero Side (no common
// ancestor), in which case every quest is an add on one or both sides.
func Merge(base, local, remote Side) (Result, []Event) {
	res := Result{Config: local.Config, Quests: map[string]*quest.Quest{}}
	var events []Event

	for _, id := range unionIDs(base.Quests, local.Quests, remote.Quests) {
		b, l, r := base.Quests[id], local.Quests[id], remote.Quests[id]
		switch {
		case l == nil && r == nil:
			// deleted on both (no delete API exists; harmless if hand-edited).
		case l == nil:
			res.Quests[id] = r // add on remote only
		case r == nil:
			res.Quests[id] = l // add on local only
		case equalQuest(l, r):
			res.Quests[id] = l // same content (both made the same change, or neither)
		case b != nil && equalQuest(l, b):
			res.Quests[id] = r // unchanged locally -> take remote
		case b != nil && equalQuest(r, b):
			res.Quests[id] = l // unchanged remotely -> take local
		default:
			// both changed since base (or added independently) -> Task 2 / Task 4.
			res.Quests[id] = l // placeholder until Task 2
		}
	}
	return res, events
}

// unionIDs returns the sorted union of keys across the given maps.
func unionIDs(maps ...map[string]*quest.Quest) []string {
	seen := map[string]bool{}
	for _, m := range maps {
		for id := range m {
			seen[id] = true
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
