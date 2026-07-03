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
