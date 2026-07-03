package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sharkusk/side-quest/internal/quest"
)

func TestNewCreatesQuestAndPrintsID(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)

	out, code := runBin(t, bin, dir, "new", "Fix the parser")
	if code != 0 {
		t.Fatalf("new exit=%d out=%s", code, out)
	}
	id := strings.TrimSpace(out)
	if !strings.HasPrefix(id, "SQ-") {
		t.Fatalf("expected an SQ- id, got %q", id)
	}
	q, err := s.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if q.Title != "Fix the parser" || q.Type != quest.TypeFeature || q.Priority != quest.PriorityLow {
		t.Fatalf("unexpected quest: %+v", q)
	}
}

func TestNewFlagsTypePriorityTagCurrentJSON(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)

	out, code := runBin(t, bin, dir,
		"new", "--type", "bug", "--priority", "high",
		"--tag", "area=cli", "--current", "--json", "Broken flag")
	if code != 0 {
		t.Fatalf("new exit=%d out=%s", code, out)
	}
	var q quest.Quest
	if err := json.Unmarshal([]byte(out), &q); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if q.Type != quest.TypeBug || q.Priority != quest.PriorityHigh {
		t.Fatalf("flags not applied: %+v", q)
	}
	if q.Tags["area"] != "cli" {
		t.Fatalf("tag not recorded: %+v", q.Tags)
	}
	cur, _ := s.Current()
	if cur != q.ID {
		t.Fatalf("--current did not set pointer: cur=%q id=%q", cur, q.ID)
	}
}

func TestNewInvalidTypeExitsNonZeroEmptyRef(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)

	_, code := runBin(t, bin, dir, "new", "--type", "buggg", "Nope")
	if code != 1 {
		t.Fatalf("expected exit 1 for invalid type, got %d", code)
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("invalid create wrote a quest: %+v", list)
	}
}

func TestNewBadTagExitsTwo(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)

	_, code := runBin(t, bin, dir, "new", "--tag", "noequals", "Title")
	if code != 2 {
		t.Fatalf("expected exit 2 for malformed --tag, got %d", code)
	}
}

func TestInitThenReinitErrors(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)

	if out, code := runBin(t, bin, dir, "init"); code != 0 {
		t.Fatalf("init exit=%d out=%s", code, out)
	}
	if _, code := runBin(t, bin, dir, "init"); code != 1 {
		t.Fatalf("re-init should exit 1, got %d", code)
	}
}
