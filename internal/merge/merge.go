// Package merge is the pure, git-free three-way merge engine for the quest ref
// (design spec 2026-07-03-side-quest-sync §5). Every rule here is a deterministic
// function of its inputs so that two clones merging the same divergence converge
// on byte-identical results regardless of which side each calls "local".
package merge

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
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
	var pendingLosers []*quest.Quest

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
			if b == nil {
				// same id, two different quests (added independently) -> collision.
				keep, lose := collisionKeeper(l, r)
				res.Quests[id] = keep
				pendingLosers = append(pendingLosers, lose)
				break
			}
			res.Quests[id] = mergeConflict(id, b, l, r, local.Touch[id], remote.Touch[id])
			events = append(events, Event{Kind: Conflicted, ID: id,
				Detail: "both sides changed; scalars taken from the later edit"})
		}
	}

	// Resolve id collisions deterministically. taken starts as every id that
	// exists anywhere, so a reassigned id can never shadow a real quest.
	taken := map[string]bool{}
	for id := range res.Quests {
		taken[id] = true
	}
	for _, id := range unionIDs(base.Quests, local.Quests, remote.Quests) {
		taken[id] = true
	}
	sort.SliceStable(pendingLosers, func(i, j int) bool {
		return bytes.Compare(canonical(pendingLosers[i]), canonical(pendingLosers[j])) < 0
	})
	// The merged config must be decided BEFORE collision renames so the rename
	// prefix is side-independent: using the pre-merge local prefix would let two
	// clones merging the same divergence mint different loser ids when one side
	// had (unsynced) changed id_prefix, breaking the package's determinism
	// guarantee.
	res.Config = mergeConfig(base.Config, local.Config, remote.Config, local.ConfigTouch, remote.ConfigTouch)
	for _, lose := range pendingLosers {
		newID := collisionID(res.Config.IDPrefix, lose, taken)
		taken[newID] = true
		old := lose.ID
		reassigned := *lose // copy; do not mutate the input Side's quest
		reassigned.ID = newID
		reassigned.Body = appendRenameNote(reassigned.Body, old)
		res.Quests[newID] = &reassigned
		events = append(events, Event{Kind: Renamed, ID: newID,
			Detail: "renamed from " + old + " (id collision)"})
	}
	return res, events
}

// collisionKeeper returns (keeper, loser): the earlier-Created quest keeps the
// id; an exact Created tie is broken by larger canonical bytes, so the outcome
// does not depend on which side is "local".
func collisionKeeper(l, r *quest.Quest) (keep, lose *quest.Quest) {
	if l.Created.Equal(r.Created) {
		if bytes.Compare(canonical(l), canonical(r)) >= 0 {
			return l, r
		}
		return r, l
	}
	if l.Created.Before(r.Created) {
		return l, r
	}
	return r, l
}

// collisionID derives a stable new id for a reassigned quest: prefix + the first
// 6 hex chars of sha256(canonical), widening by 2 hex chars on the astronomically
// unlikely event the id is already taken. Deterministic across clones.
func collisionID(prefix string, q *quest.Quest, taken map[string]bool) string {
	sum := sha256.Sum256(canonical(q))
	full := hex.EncodeToString(sum[:])
	for n := 6; n <= len(full); n += 2 {
		id := fmt.Sprintf("%s-%s", prefix, full[:n])
		if !taken[id] {
			return id
		}
	}
	// Exhausted the hash (practically impossible); fall back to the full digest.
	return fmt.Sprintf("%s-%s", prefix, full)
}

// appendRenameNote records the reassignment as a note so the history is visible
// in the quest itself. It reuses the note header shape (no timestamp source in
// this pure layer, so the marker is fixed and sorts before real notes).
func appendRenameNote(body, oldID string) string {
	note := "--- note (sync) ---\n\nrenamed from " + oldID + " on sync: id collision"
	if strings.TrimSpace(body) == "" {
		return note
	}
	return strings.TrimRight(body, "\n") + "\n\n" + note
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

// unionCommits merges commit lists element-wise three-way: a base sha survives
// only while BOTH sides still carry it — a side that dropped it (unlink, or a
// relink after a rebase) made a deliberate removal, and re-seeding from base
// would silently resurrect it on the next conflicting sync. Shas new to either
// side are appended. Deterministic order: base survivors in base order, then
// local's additions, then remote's (deduped).
func unionCommits(b, l, r *quest.Quest) []string {
	has := func(q *quest.Quest, sha string) bool {
		if q == nil {
			return false
		}
		for _, c := range q.Commits {
			if c == sha {
				return true
			}
		}
		return false
	}
	seen := map[string]bool{}
	out := []string{}
	keep := func(sha string) {
		if !seen[sha] {
			seen[sha] = true
			out = append(out, sha)
		}
	}
	if b != nil {
		for _, sha := range b.Commits {
			if has(l, sha) && has(r, sha) {
				keep(sha)
			}
		}
	}
	addNew := func(q *quest.Quest) {
		if q == nil {
			return
		}
		for _, sha := range q.Commits {
			if !has(b, sha) {
				keep(sha)
			}
		}
	}
	addNew(l)
	addNew(r)
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

// mergeConfig merges the on-ref config: seq_next only ever moves forward (max of
// all three), and every other field is last-writer-wins by config touch time,
// with an exact tie broken by larger marshaled bytes for determinism.
func mergeConfig(base, local, remote config.Config, lTouch, rTouch time.Time) config.Config {
	out := local
	if rTouch.After(lTouch) {
		out = remote
	} else if lTouch.Equal(rTouch) {
		lb, _ := config.Marshal(local)
		rb, _ := config.Marshal(remote)
		if bytes.Compare(rb, lb) > 0 {
			out = remote
		}
	}
	out.SeqNext = maxInt(base.SeqNext, maxInt(local.SeqNext, remote.SeqNext))
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
