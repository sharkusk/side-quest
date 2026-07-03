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

func TestListFilterAndJSON(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)

	runBin(t, bin, dir, "new", "--type", "bug", "--priority", "high", "A bug")
	runBin(t, bin, dir, "new", "--type", "feature", "B feature")

	// Human list shows both.
	out, code := runBin(t, bin, dir, "list")
	if code != 0 || !strings.Contains(out, "A bug") || !strings.Contains(out, "B feature") {
		t.Fatalf("list exit=%d out=%s", code, out)
	}

	// Filter by type=bug returns only the bug, as JSON.
	out, code = runBin(t, bin, dir, "list", "--type", "bug", "--json")
	if code != 0 {
		t.Fatalf("list --json exit=%d out=%s", code, out)
	}
	var got []quest.Quest
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if len(got) != 1 || got[0].Type != quest.TypeBug {
		t.Fatalf("filter wrong: %+v", got)
	}
}

func TestListEmptyPrintsNoQuests(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)

	out, code := runBin(t, bin, dir, "list")
	if code != 0 || !strings.Contains(out, "no quests") {
		t.Fatalf("empty list exit=%d out=%q", code, out)
	}
}

func TestListInvalidFilterExitsOne(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)

	_, code := runBin(t, bin, dir, "list", "--type", "bugg")
	if code != 1 {
		t.Fatalf("invalid filter should exit 1, got %d", code)
	}
}

func TestShowRendersAndJSON(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)

	out, _ := runBin(t, bin, dir, "new", "Show me")
	id := strings.TrimSpace(out)

	out, code := runBin(t, bin, dir, "show", id)
	if code != 0 || !strings.Contains(out, "Show me") || !strings.Contains(out, id) {
		t.Fatalf("show exit=%d out=%s", code, out)
	}

	out, code = runBin(t, bin, dir, "show", "--json", id)
	if code != 0 {
		t.Fatalf("show --json exit=%d out=%s", code, out)
	}
	var q quest.Quest
	if err := json.Unmarshal([]byte(out), &q); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if q.Title != "Show me" {
		t.Fatalf("wrong quest: %+v", q)
	}
}

func TestShowMissingExitsOne(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)

	_, code := runBin(t, bin, dir, "show", "SQ-9999")
	if code != 1 {
		t.Fatalf("missing show should exit 1, got %d", code)
	}
}

func TestStatusSetsAndRejects(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)

	out, _ := runBin(t, bin, dir, "new", "Do a thing")
	id := strings.TrimSpace(out)

	if _, code := runBin(t, bin, dir, "status", id, "done"); code != 0 {
		t.Fatalf("status done exit=%d", code)
	}
	q, _ := s.Get(id)
	if q.Status != quest.StatusDone {
		t.Fatalf("status not set: %+v", q)
	}

	if _, code := runBin(t, bin, dir, "status", id, "nope"); code != 1 {
		t.Fatalf("invalid status should exit 1, got %d", code)
	}
}

func TestReclassifyBothFields(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)

	out, _ := runBin(t, bin, dir, "new", "Reclassify me")
	id := strings.TrimSpace(out)

	if _, code := runBin(t, bin, dir, "reclassify", "--type", "bug", "--priority", "high", id); code != 0 {
		t.Fatalf("reclassify exit=%d", code)
	}
	q, _ := s.Get(id)
	if q.Type != quest.TypeBug || q.Priority != quest.PriorityHigh {
		t.Fatalf("reclassify wrong: %+v", q)
	}
}

func TestReclassifyNoFlagIsUsageError(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)

	out, _ := runBin(t, bin, dir, "new", "x")
	id := strings.TrimSpace(out)

	if _, code := runBin(t, bin, dir, "reclassify", id); code != 2 {
		t.Fatalf("reclassify with no flag should exit 2, got %d", code)
	}
}

func TestReclassifyInvalidExitsOne(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)

	out, _ := runBin(t, bin, dir, "new", "x")
	id := strings.TrimSpace(out)

	if _, code := runBin(t, bin, dir, "reclassify", "--type", "bugg", id); code != 1 {
		t.Fatalf("reclassify invalid type should exit 1, got %d", code)
	}
}
