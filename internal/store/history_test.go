package store

import (
	"errors"
	"testing"

	"github.com/sharkusk/side-quest/internal/quest"
)

// TestHistory walks a quest through a few mutations and checks the reconstructed
// change log: oldest first, one entry per commit, each carrying the right diff.
func TestHistory(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	q, err := s.Create("hist me", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(q.ID, quest.StatusConfirm); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendNote(q.ID, "a note"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddCommit(q.ID, "abcdef1234567890", LinkComplete); err != nil {
		t.Fatal(err)
	}

	h, err := s.History(q.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"created"},
		{"status: open → confirm"},
		{"note added"},
		{"status: confirm → done", "linked commit abcdef1"},
	}
	if len(h) != len(want) {
		t.Fatalf("history has %d entries, want %d: %+v", len(h), len(want), h)
	}
	for i, w := range want {
		if !equalStrings(h[i].Changes, w) {
			t.Errorf("entry %d changes = %v, want %v", i, h[i].Changes, w)
		}
		if h[i].Commit == "" || h[i].Who == "" || h[i].When.IsZero() {
			t.Errorf("entry %d missing commit/who/when: %+v", i, h[i])
		}
	}
}

func TestHistoryNotFound(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.History("SQ-9999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("History on a missing quest = %v, want ErrNotFound", err)
	}
}

// TestDiffQuest exercises the pure field-diff that powers each history entry.
func TestDiffQuest(t *testing.T) {
	base := &quest.Quest{Title: "t", Status: quest.StatusOpen, Type: quest.TypeFeature, Priority: quest.PriorityLow}

	cases := []struct {
		name     string
		mutate   func(*quest.Quest)
		wantSome []string // substrings that must appear among the changes
	}{
		{"status", func(q *quest.Quest) { q.Status = quest.StatusDone }, []string{"status: open → done"}},
		{"type", func(q *quest.Quest) { q.Type = quest.TypeBug }, []string{"type: feature → bug"}},
		{"priority", func(q *quest.Quest) { q.Priority = quest.PriorityHigh }, []string{"priority: low → high"}},
		{"title", func(q *quest.Quest) { q.Title = "new" }, []string{"title changed"}},
		{"link", func(q *quest.Quest) { q.Commits = []string{"1234567890"} }, []string{"linked commit 1234567"}},
		{"tags", func(q *quest.Quest) { q.Tags = map[string]string{"area": "cli"} }, []string{"tags updated"}},
		{"note", func(q *quest.Quest) { q.Body = "--- note 2026 ---\nhi" }, []string{"note added"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			old := *base
			nu := *base
			c.mutate(&nu)
			got := diffQuest(&old, &nu)
			for _, sub := range c.wantSome {
				if !contains(got, sub) {
					t.Errorf("diffQuest = %v, want to contain %q", got, sub)
				}
			}
		})
	}

	// A nil old is a creation.
	if got := diffQuest(nil, base); !equalStrings(got, []string{"created"}) {
		t.Errorf("diffQuest(nil, q) = %v, want [created]", got)
	}
	// Unlinking: old has a commit the new one lacks.
	oldLinked := *base
	oldLinked.Commits = []string{"deadbeef00"}
	if got := diffQuest(&oldLinked, base); !contains(got, "unlinked commit deadbee") {
		t.Errorf("diffQuest unlink = %v, want an unlinked entry", got)
	}
}

func equalStrings(a, b []string) bool {
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
