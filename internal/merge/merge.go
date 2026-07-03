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
			res.Quests[id] = mergeConflict(id, b, l, r, local.Touch[id], remote.Touch[id])
			events = append(events, Event{Kind: Conflicted, ID: id,
				Detail: "both sides changed; scalars taken from the later edit"})
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

// mergeConflict resolves a quest changed on both sides. The later-touched side
// wins the scalar fields (equal times break on larger canonical bytes, a
// side-independent tiebreak); Created is the earliest seen; commits and tags
// union. Body keeps the winner's preamble and unions both sides' notes.
func mergeConflict(id string, b, l, r *quest.Quest, lTouch, rTouch time.Time) *quest.Quest {
	winner := laterWins(l, r, lTouch, rTouch)
	out := *winner // copy the winning scalars (Title, Status, Type, Priority, Context, Body, Completed)
	out.Body = mergeBody(winner, l, r)
	out.ID = id
	out.Created = earliest(b, l, r)
	out.Commits = unionCommits(b, l, r)
	out.Tags = unionTags(winner, l, r)
	return &out
}

// laterWins returns whichever of l, r was touched later; on an exact tie the one
// with lexicographically larger canonical bytes wins, so the choice never
// depends on which side is "local".
func laterWins(l, r *quest.Quest, lTouch, rTouch time.Time) *quest.Quest {
	if lTouch.Equal(rTouch) {
		if bytes.Compare(canonical(r), canonical(l)) > 0 {
			return r
		}
		return l
	}
	if rTouch.After(lTouch) {
		return r
	}
	return l
}

// earliest returns the earliest non-zero Created among the present quests.
func earliest(qs ...*quest.Quest) time.Time {
	var out time.Time
	for _, x := range qs {
		if x == nil || x.Created.IsZero() {
			continue
		}
		if out.IsZero() || x.Created.Before(out) {
			out = x.Created
		}
	}
	return out
}

// unionCommits merges commit lists preserving base order, then appends shas new
// to either side (deduped, deterministic order: local's new before remote's).
func unionCommits(b, l, r *quest.Quest) []string {
	seen := map[string]bool{}
	out := []string{}
	add := func(q *quest.Quest) {
		if q == nil {
			return
		}
		for _, sha := range q.Commits {
			if !seen[sha] {
				seen[sha] = true
				out = append(out, sha)
			}
		}
	}
	add(b)
	add(l)
	add(r)
	return out
}

// unionTags unions the tag keys of l and r; a key set on both takes the winner's
// value. Nil result when no tags exist anywhere.
func unionTags(winner, l, r *quest.Quest) map[string]string {
	out := map[string]string{}
	for _, q := range []*quest.Quest{l, r} {
		for k, v := range q.Tags {
			out[k] = v
		}
	}
	for k, v := range winner.Tags {
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
