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

func TestMarshalRoundTrip(t *testing.T) {
	created := time.Date(2026, 7, 2, 14, 3, 11, 0, time.UTC)
	q := &Quest{
		ID:      "SQ-0001", // must NOT appear in the serialized bytes
		Title:   "Crash stack-trace diagnostic",
		Status:  StatusOpen,
		Created: created,
		Commits: []string{"a62d4fa"},
		Context: "branch=main head=a62d4fa\nCaptured while debugging.",
		Tags:    map[string]string{"area": "engine"},
		Body:    "Full prose description.\nWith two lines.",
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
