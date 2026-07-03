package merge

import (
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
