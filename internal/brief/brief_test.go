package brief

import (
	"testing"
	"time"

	"github.com/sharkusk/side-quest/internal/quest"
)

func mkQuest(id string, st quest.Status, created time.Time, completed *time.Time) *quest.Quest {
	return &quest.Quest{
		ID:        id,
		Title:     id + " title",
		Status:    st,
		Type:      quest.TypeFeature,
		Priority:  quest.PriorityLow,
		Created:   created,
		Completed: completed,
	}
}

func tp(t time.Time) *time.Time { return &t }

func TestBuildPartitions(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	day := func(n int) time.Time { return base.AddDate(0, 0, n) }

	open := mkQuest("SQ-0001", quest.StatusOpen, day(0), nil)
	partial := mkQuest("SQ-0002", quest.StatusPartial, day(1), nil)
	confirm := mkQuest("SQ-0003", quest.StatusConfirm, day(2), nil)
	doneOld := mkQuest("SQ-0004", quest.StatusDone, day(1), tp(day(3)))
	doneNew := mkQuest("SQ-0005", quest.StatusDone, day(2), tp(day(6)))
	deferred := mkQuest("SQ-0006", quest.StatusDeferred, day(4), nil) // no completed → sorts by created
	quests := []*quest.Quest{open, partial, confirm, doneOld, doneNew, deferred}

	d := Build(quests, "SQ-0002", day(10), 2)

	if d.Current != partial {
		t.Fatalf("Current = %v, want SQ-0002", d.Current)
	}
	if want := []string{"SQ-0001", "SQ-0003"}; !equal(ids(d.Outstanding), want) {
		t.Errorf("Outstanding = %v, want %v (current excluded, id order)", ids(d.Outstanding), want)
	}
	if d.ClosedTotal != 3 {
		t.Errorf("ClosedTotal = %d, want 3", d.ClosedTotal)
	}
	// Newest-first by close time, capped to 2: doneNew(day6), deferred(day4).
	if want := []string{"SQ-0005", "SQ-0006"}; !equal(ids(d.Closed), want) {
		t.Errorf("Closed = %v, want %v", ids(d.Closed), want)
	}
	if !d.LastActivity.Equal(day(6)) {
		t.Errorf("LastActivity = %v, want %v", d.LastActivity, day(6))
	}
}

func TestBuildCurrentClosedIsFeaturedNotListed(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	done := mkQuest("SQ-0009", quest.StatusDone, base, tp(base.AddDate(0, 0, 1)))
	other := mkQuest("SQ-0010", quest.StatusDone, base, tp(base))
	d := Build([]*quest.Quest{done, other}, "SQ-0009", base.AddDate(0, 0, 2), 5)

	if d.Current != done {
		t.Fatalf("Current = %v, want SQ-0009 (featured even though done)", d.Current)
	}
	if contains(ids(d.Closed), "SQ-0009") {
		t.Errorf("Closed = %v, must not contain the current quest", ids(d.Closed))
	}
	if want := []string{"SQ-0010"}; !equal(ids(d.Closed), want) {
		t.Errorf("Closed = %v, want %v", ids(d.Closed), want)
	}
}

func TestBuildNoCurrentAndNegativeMeansNoCap(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	var quests []*quest.Quest
	for i := 0; i < 4; i++ {
		quests = append(quests, mkQuest("SQ-000"+string(rune('1'+i)), quest.StatusDone, base, tp(base.AddDate(0, 0, i))))
	}
	d := Build(quests, "", base, -1)
	if d.Current != nil {
		t.Errorf("Current = %v, want nil", d.Current)
	}
	if len(d.Closed) != 4 {
		t.Errorf("len(Closed) = %d, want 4 (negative closedN = no cap)", len(d.Closed))
	}
}

func TestNarrativeStripsMechanicalBlock(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"mechanical then narrative", "branch: main\nhead: abc1234\ncwd: /repo\ncurrent: SQ-0001\n\nWhy this came up.", "Why this came up."},
		{"mechanical only", "branch: main\nhead: abc1234\ncwd: /repo", ""},
		{"narrative only", "Just a plain note.", "Just a plain note."},
		{"empty", "", ""},
		{"multi-line narrative preserved", "branch: x\ncwd: /r\n\nline one\nline two", "line one\nline two"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Narrative(c.in); got != c.want {
				t.Errorf("Narrative(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestHumanizeSince(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{10 * time.Second, "just now"},
		{5 * time.Minute, "5m ago"},
		{3 * time.Hour, "3h ago"},
		{2 * 24 * time.Hour, "2d ago"},
		{3 * 7 * 24 * time.Hour, "3w ago"},
		{-time.Hour, "just now"}, // a future stamp never reads as negative
	}
	for _, c := range cases {
		if got := HumanizeSince(now, now.Add(-c.ago)); got != c.want {
			t.Errorf("HumanizeSince(-%v) = %q, want %q", c.ago, got, c.want)
		}
	}
}

func ids(qs []*quest.Quest) []string {
	out := make([]string, len(qs))
	for i, q := range qs {
		out[i] = q.ID
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(a []string, s string) bool {
	for _, x := range a {
		if x == s {
			return true
		}
	}
	return false
}
