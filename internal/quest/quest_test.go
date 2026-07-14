package quest

import (
	"strings"
	"testing"
	"time"
)

func TestStatusValid(t *testing.T) {
	for _, s := range []Status{StatusOpen, StatusPartial, StatusDone, StatusDeferred, StatusDiscarded} {
		if !s.Valid() {
			t.Errorf("%q should be valid", s)
		}
	}
	if Status("bogus").Valid() {
		t.Error("bogus should be invalid")
	}
}

func TestTypeValid(t *testing.T) {
	for _, ty := range []Type{TypeBug, TypeFeature} {
		if !ty.Valid() {
			t.Errorf("%q should be valid", ty)
		}
	}
	if Type("bogus").Valid() {
		t.Error("bogus type should be invalid")
	}
	if Type("").Valid() {
		t.Error("empty type should be invalid")
	}
}

func TestPriorityValid(t *testing.T) {
	for _, p := range []Priority{PriorityHigh, PriorityLow} {
		if !p.Valid() {
			t.Errorf("%q should be valid", p)
		}
	}
	if Priority("bogus").Valid() {
		t.Error("bogus priority should be invalid")
	}
	if Priority("").Valid() {
		t.Error("empty priority should be invalid")
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	created := time.Date(2026, 7, 2, 14, 3, 11, 0, time.UTC)
	q := &Quest{
		ID:       "SQ-0001", // must NOT appear in the serialized bytes
		Title:    "Crash stack-trace diagnostic",
		Status:   StatusOpen,
		Type:     TypeBug,
		Priority: PriorityHigh,
		Created:  created,
		Commits:  []string{"a62d4fa"},
		Context:  "branch=main head=a62d4fa\nCaptured while debugging.",
		Tags:     map[string]string{"area": "engine"},
		Body:     "Full prose description.\nWith two lines.",
	}
	data, err := Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "SQ-0001") {
		t.Fatal("id must not be serialized into the file (filename is the id)")
	}
	if !strings.HasPrefix(string(data), "---\n") {
		t.Fatalf("expected leading frontmatter fence, got:\n%s", data)
	}

	got, err := Unmarshal("SQ-0001", data)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "SQ-0001" {
		t.Errorf("id: got %q want SQ-0001 (from filename)", got.ID)
	}
	if got.Title != q.Title || got.Status != q.Status {
		t.Errorf("title/status mismatch: %+v", got)
	}
	if got.Type != TypeBug {
		t.Errorf("type: got %q want bug", got.Type)
	}
	if got.Priority != PriorityHigh {
		t.Errorf("priority: got %q want high", got.Priority)
	}
	if !strings.Contains(string(data), "type: bug") || !strings.Contains(string(data), "priority: high") {
		t.Fatalf("type/priority not serialized into frontmatter:\n%s", data)
	}
	if !got.Created.Equal(created) {
		t.Errorf("created: got %v want %v", got.Created, created)
	}
	if len(got.Commits) != 1 || got.Commits[0] != "a62d4fa" {
		t.Errorf("commits mismatch: %v", got.Commits)
	}
	if got.Tags["area"] != "engine" {
		t.Errorf("tags mismatch: %v", got.Tags)
	}
	if got.Body != q.Body {
		t.Errorf("body: got %q want %q", got.Body, q.Body)
	}
}

func TestUnmarshalRejectsMissingFence(t *testing.T) {
	_, err := Unmarshal("SQ-0001", []byte("no frontmatter here"))
	if err == nil {
		t.Fatal("expected error for missing frontmatter fence")
	}
}

func TestNormalizeID(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
		width  int
		raw    string
		want   string
	}{
		{"bare number", "SQ", 4, "11", "SQ-0011"},
		{"zero-padded number", "SQ", 4, "0011", "SQ-0011"},
		{"over-padded number", "SQ", 4, "00011", "SQ-0011"},
		{"already canonical", "SQ", 4, "SQ-0011", "SQ-0011"},
		{"number wider than width", "SQ", 4, "12345", "SQ-12345"},
		{"surrounding whitespace", "SQ", 4, "  11 ", "SQ-0011"},
		{"custom prefix and width", "Q", 3, "7", "Q-007"},
		{"non-numeric bare left alone", "SQ", 4, "abc", "abc"},
		{"random hex id left alone", "SQ", 4, "SQ-a1b2c3", "SQ-a1b2c3"},
		{"empty left alone", "SQ", 4, "", ""},
		// SQ-0119: the unpadded prefixed form used to pass through verbatim and
		// silently link nothing; the lowercase prefix likewise.
		{"unpadded prefixed number", "SQ", 4, "SQ-12", "SQ-0012"},
		{"lowercase prefix folded", "SQ", 4, "sq-0012", "SQ-0012"},
		{"lowercase unpadded", "SQ", 4, "sq-12", "SQ-0012"},
		// An all-digit random id (leading zero, at/beyond width) must never be
		// integer-mangled when given in full prefixed form.
		{"all-digit random id verbatim", "SQ", 4, "SQ-012345", "SQ-012345"},
		{"prefixed at-width digits verbatim", "SQ", 4, "SQ-0011", "SQ-0011"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NormalizeID(c.prefix, c.width, c.raw); got != c.want {
				t.Errorf("NormalizeID(%q, %d, %q) = %q, want %q", c.prefix, c.width, c.raw, got, c.want)
			}
		})
	}
}

func TestMatchTags(t *testing.T) {
	have := map[string]string{"area": "cli", "phase": "5"}
	cases := []struct {
		name string
		want map[string]string
		ok   bool
	}{
		{"empty filter matches all", nil, true},
		{"single pair present", map[string]string{"area": "cli"}, true},
		{"all pairs present", map[string]string{"area": "cli", "phase": "5"}, true},
		{"value mismatch", map[string]string{"area": "map"}, false},
		{"missing key", map[string]string{"owner": "me"}, false},
		{"one of two missing", map[string]string{"area": "cli", "owner": "me"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MatchTags(have, c.want); got != c.ok {
				t.Errorf("MatchTags(%v, %v) = %v, want %v", have, c.want, got, c.ok)
			}
		})
	}
	// A nil have with a non-empty filter never matches.
	if MatchTags(nil, map[string]string{"area": "cli"}) {
		t.Error("nil tags should not match a non-empty filter")
	}
}
