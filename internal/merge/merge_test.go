package merge

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sharkusk/side-quest/internal/config"
	"github.com/sharkusk/side-quest/internal/quest"
)

// q is a terse quest builder for tests.
func q(id, title string, st quest.Status) *quest.Quest {
	return &quest.Quest{
		ID: id, Title: title, Status: st,
		Type: quest.TypeFeature, Priority: quest.PriorityLow,
		Created: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Commits: []string{},
	}
}

func side(qs ...*quest.Quest) Side {
	m := map[string]*quest.Quest{}
	for _, x := range qs {
		m[x.ID] = x
	}
	return Side{Config: config.Default(), Quests: m, Touch: map[string]time.Time{}}
}

func TestMergeStructuralCases(t *testing.T) {
	base := side(q("SQ-0001", "shared", quest.StatusOpen))
	// SQ-0002 added only locally; SQ-0003 added only remotely;
	// SQ-0001 unchanged locally, edited remotely -> remote wins.
	remoteEdited := q("SQ-0001", "shared edited", quest.StatusPartial)
	local := side(q("SQ-0001", "shared", quest.StatusOpen), q("SQ-0002", "local only", quest.StatusOpen))
	remote := side(remoteEdited, q("SQ-0003", "remote only", quest.StatusOpen))

	res, events := Merge(base, local, remote)

	if len(events) != 0 {
		t.Fatalf("expected no events, got %v", events)
	}
	if got := res.Quests["SQ-0001"].Title; got != "shared edited" {
		t.Errorf("SQ-0001 title = %q, want remote's %q", got, "shared edited")
	}
	if _, ok := res.Quests["SQ-0002"]; !ok {
		t.Error("SQ-0002 (local add) missing from result")
	}
	if _, ok := res.Quests["SQ-0003"]; !ok {
		t.Error("SQ-0003 (remote add) missing from result")
	}
}

func TestMergeEmptyBaseTakesBothAdds(t *testing.T) {
	// No common ancestor: base is the zero Side.
	local := side(q("SQ-0001", "a", quest.StatusOpen))
	remote := side(q("SQ-0002", "b", quest.StatusOpen))
	res, events := Merge(Side{}, local, remote)
	if len(events) != 0 {
		t.Fatalf("events: %v", events)
	}
	if len(res.Quests) != 2 {
		t.Errorf("want 2 quests, got %d", len(res.Quests))
	}
}

func TestMergeBothChangedScalarLWW(t *testing.T) {
	base := side(q("SQ-0001", "orig", quest.StatusOpen))
	l := q("SQ-0001", "local title", quest.StatusDone)
	l.Priority = quest.PriorityHigh
	l.Commits = []string{"aaa"}
	r := q("SQ-0001", "remote title", quest.StatusPartial)
	r.Commits = []string{"bbb"}

	local := side(l)
	local.Touch["SQ-0001"] = time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC) // earlier
	remote := side(r)
	remote.Touch["SQ-0001"] = time.Date(2026, 1, 2, 11, 0, 0, 0, time.UTC) // later -> wins

	res, events := Merge(base, local, remote)
	got := res.Quests["SQ-0001"]
	if got.Title != "remote title" || got.Status != quest.StatusPartial {
		t.Errorf("scalars = (%q,%q), want remote's (remote title, partial)", got.Title, got.Status)
	}
	// commits union regardless of winner:
	if len(got.Commits) != 2 || got.Commits[0] != "aaa" || got.Commits[1] != "bbb" {
		t.Errorf("commits = %v, want [aaa bbb]", got.Commits)
	}
	if len(events) != 1 || events[0].Kind != Conflicted {
		t.Errorf("events = %v, want one Conflicted", events)
	}
}

func TestMergeEqualTouchTiebreakByBytes(t *testing.T) {
	base := side(q("SQ-0001", "orig", quest.StatusOpen))
	l := q("SQ-0001", "aaa", quest.StatusOpen)
	r := q("SQ-0001", "zzz", quest.StatusOpen)
	ts := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	local := side(l)
	local.Touch["SQ-0001"] = ts
	remote := side(r)
	remote.Touch["SQ-0001"] = ts
	// Same result whichever side is "local": larger canonical bytes win.
	res1, _ := Merge(base, local, remote)
	res2, _ := Merge(base, remote, local)
	if res1.Quests["SQ-0001"].Title != res2.Quests["SQ-0001"].Title {
		t.Fatalf("tiebreak not symmetric: %q vs %q",
			res1.Quests["SQ-0001"].Title, res2.Quests["SQ-0001"].Title)
	}
}

func TestMergeTagsUnionWinnerWinsKey(t *testing.T) {
	base := side(q("SQ-0001", "orig", quest.StatusOpen))
	l := q("SQ-0001", "l", quest.StatusOpen)
	l.Tags = map[string]string{"area": "app", "only-l": "1"}
	r := q("SQ-0001", "r", quest.StatusOpen)
	r.Tags = map[string]string{"area": "map", "only-r": "2"}
	local := side(l)
	local.Touch["SQ-0001"] = time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	remote := side(r)
	remote.Touch["SQ-0001"] = time.Date(2026, 1, 2, 11, 0, 0, 0, time.UTC) // remote wins
	res, _ := Merge(base, local, remote)
	tags := res.Quests["SQ-0001"].Tags
	if tags["area"] != "map" || tags["only-l"] != "1" || tags["only-r"] != "2" {
		t.Errorf("tags = %v, want area=map + both only-* keys", tags)
	}
}

func TestMergeIDCollisionReassignsLoser(t *testing.T) {
	// SQ-0007 minted independently by two clones (no base) for different quests.
	early := q("SQ-0007", "fix parser", quest.StatusOpen)
	early.Created = time.Date(2026, 1, 2, 14, 2, 0, 0, time.UTC)
	late := q("SQ-0007", "add dark mode", quest.StatusOpen)
	late.Created = time.Date(2026, 1, 2, 15, 30, 0, 0, time.UTC)

	local := side(early)
	remote := side(late)
	res, events := Merge(Side{}, local, remote)

	if res.Quests["SQ-0007"].Title != "fix parser" {
		t.Errorf("earlier-Created should keep SQ-0007, got %q", res.Quests["SQ-0007"].Title)
	}
	// the loser exists under a new prefix-hex id with a rename note:
	var renamed *quest.Quest
	for id, x := range res.Quests {
		if id != "SQ-0007" {
			renamed = x
		}
	}
	if renamed == nil || renamed.Title != "add dark mode" {
		t.Fatalf("loser not reassigned: %v", res.Quests)
	}
	if !strings.Contains(renamed.Body, "renamed from SQ-0007") {
		t.Errorf("rename note missing:\n%s", renamed.Body)
	}
	if len(events) != 1 || events[0].Kind != Renamed || events[0].ID != renamed.ID {
		t.Errorf("events = %v, want one Renamed for %s", events, renamed.ID)
	}
}

func TestMergeIDCollisionDeterministic(t *testing.T) {
	early := q("SQ-0007", "fix parser", quest.StatusOpen)
	early.Created = time.Date(2026, 1, 2, 14, 2, 0, 0, time.UTC)
	late := q("SQ-0007", "add dark mode", quest.StatusOpen)
	late.Created = time.Date(2026, 1, 2, 15, 30, 0, 0, time.UTC)
	// Swapping which side is "local" must yield the same reassigned id.
	res1, _ := Merge(Side{}, side(early), side(late))
	res2, _ := Merge(Side{}, side(late), side(early))
	ids1, ids2 := idsOf(res1), idsOf(res2)
	if ids1 != ids2 {
		t.Errorf("collision resolution not deterministic: %s vs %s", ids1, ids2)
	}
}

func idsOf(r Result) string {
	var ids []string
	for id := range r.Quests {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

func TestMergeConfigSeqNextMaxAndLWW(t *testing.T) {
	base := side(q("SQ-0001", "x", quest.StatusOpen))
	base.Config = config.Default() // seq_next 1

	local := side(q("SQ-0001", "x", quest.StatusOpen))
	local.Config = config.Default()
	local.Config.SeqNext = 9
	local.Config.Tone = config.TonePlain
	local.ConfigTouch = time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)

	remote := side(q("SQ-0001", "x", quest.StatusOpen))
	remote.Config = config.Default()
	remote.Config.SeqNext = 5
	remote.Config.Tone = config.ToneDCC
	remote.ConfigTouch = time.Date(2026, 1, 2, 11, 0, 0, 0, time.UTC) // later -> tone wins

	res, _ := Merge(base, local, remote)
	if res.Config.SeqNext != 9 {
		t.Errorf("seq_next = %d, want max 9", res.Config.SeqNext)
	}
	if res.Config.Tone != config.ToneDCC {
		t.Errorf("tone = %q, want later writer's dcc", res.Config.Tone)
	}
}
