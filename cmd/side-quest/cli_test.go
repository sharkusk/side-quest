package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sharkusk/side-quest/internal/config"
	"github.com/sharkusk/side-quest/internal/quest"
)

// idFromCreated extracts the quest id from the plain-tone `new` confirmation
// ("created SQ-0001"), for tests that create a quest via the human path and
// then act on the returned id.
func idFromCreated(t *testing.T, out string) string {
	t.Helper()
	id := strings.TrimPrefix(strings.TrimSpace(out), "created ")
	if !strings.HasPrefix(id, "SQ-") {
		t.Fatalf("expected an SQ- id, got %q", out)
	}
	return id
}

func TestNewCreatesQuestAndPrintsID(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)
	t.Setenv("SIDE_QUEST_TONE", "plain")

	out, code := runBin(t, bin, dir, "new", "Fix the parser")
	if code != 0 {
		t.Fatalf("new exit=%d out=%s", code, out)
	}
	id := idFromCreated(t, out)
	q, err := s.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if q.Title != "Fix the parser" || q.Type != quest.TypeFeature || q.Priority != quest.PriorityLow {
		t.Fatalf("unexpected quest: %+v", q)
	}
}

func TestNoteAppendsToQuestBody(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)
	t.Setenv("SIDE_QUEST_TONE", "plain")

	out, _ := runBin(t, bin, dir, "new", "Track a thing")
	id := idFromCreated(t, out)

	nout, code := runBin(t, bin, dir, "note", id, "learned something useful")
	if code != 0 {
		t.Fatalf("note exit=%d out=%s", code, nout)
	}
	q, err := s.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q.Body, "learned something useful") {
		t.Fatalf("note not appended: body=%q", q.Body)
	}
}

func TestNoteMissingTextExitsTwo(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)

	out, code := runBin(t, bin, dir, "note", "SQ-0001")
	if code != 2 {
		t.Fatalf("expected usage exit 2, got %d out=%s", code, out)
	}
}

// The usage output is the only place enum values are discoverable (subcommand
// -h is silenced), so it must list the valid type/priority/status values.
func TestUsageListsEnumValues(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()

	out, code := runBin(t, bin, dir) // no args -> usage on stderr, exit 2
	if code != 2 {
		t.Fatalf("bare invocation should exit 2, got %d\n%s", code, out)
	}
	for _, want := range []string{
		"bug|feature",
		"high|low",
		"open|partial|done|deferred|discarded",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("usage missing enum values %q\n%s", want, out)
		}
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
	t.Setenv("SIDE_QUEST_TONE", "plain")

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
	t.Setenv("SIDE_QUEST_TONE", "plain")

	out, _ := runBin(t, bin, dir, "new", "Show me")
	id := idFromCreated(t, out)

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
	t.Setenv("SIDE_QUEST_TONE", "plain")

	out, _ := runBin(t, bin, dir, "new", "Do a thing")
	id := idFromCreated(t, out)

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
	t.Setenv("SIDE_QUEST_TONE", "plain")

	out, _ := runBin(t, bin, dir, "new", "Reclassify me")
	id := idFromCreated(t, out)

	if _, code := runBin(t, bin, dir, "reclassify", "--type", "bug", "--priority", "high", id); code != 0 {
		t.Fatalf("reclassify exit=%d", code)
	}
	q, _ := s.Get(id)
	if q.Type != quest.TypeBug || q.Priority != quest.PriorityHigh {
		t.Fatalf("reclassify wrong: %+v", q)
	}
}

// Flags may appear after the positional title/id as well as before it. Go's
// stdlib flag package stops at the first positional, so these cases only work
// because the CLI re-parses interspersed args (see parseInterspersed).
func TestNewFlagsAfterTitle(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)

	out, code := runBin(t, bin, dir,
		"new", "Broken flag", "--type", "bug", "--priority", "high", "--tag", "area=cli", "--json")
	if code != 0 {
		t.Fatalf("new (flags after title) exit=%d out=%s", code, out)
	}
	var q quest.Quest
	if err := json.Unmarshal([]byte(out), &q); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if q.Title != "Broken flag" || q.Type != quest.TypeBug || q.Priority != quest.PriorityHigh {
		t.Fatalf("flags after title not applied: %+v", q)
	}
	if q.Tags["area"] != "cli" {
		t.Fatalf("tag after title not recorded: %+v", q.Tags)
	}
	_ = s
}

func TestShowFlagAfterID(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)
	t.Setenv("SIDE_QUEST_TONE", "plain")

	out, _ := runBin(t, bin, dir, "new", "Show me")
	id := idFromCreated(t, out)

	out, code := runBin(t, bin, dir, "show", id, "--json")
	if code != 0 {
		t.Fatalf("show <id> --json exit=%d out=%s", code, out)
	}
	var q quest.Quest
	if err := json.Unmarshal([]byte(out), &q); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if q.Title != "Show me" {
		t.Fatalf("wrong quest: %+v", q)
	}
}

func TestReclassifyFlagsAfterID(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)
	t.Setenv("SIDE_QUEST_TONE", "plain")

	out, _ := runBin(t, bin, dir, "new", "Reclassify me")
	id := idFromCreated(t, out)

	if _, code := runBin(t, bin, dir, "reclassify", id, "--type", "bug", "--priority", "high"); code != 0 {
		t.Fatalf("reclassify <id> --type ... exit nonzero")
	}
	q, _ := s.Get(id)
	if q.Type != quest.TypeBug || q.Priority != quest.PriorityHigh {
		t.Fatalf("reclassify (flags after id) wrong: %+v", q)
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

func TestConfigGetShowsDefaults(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)
	runBin(t, bin, dir, "init")

	out, code := runBin(t, bin, dir, "config", "get")
	if code != 0 || !strings.Contains(out, "require_quest") || !strings.Contains(out, "auto_trailer") {
		t.Fatalf("config get exit=%d out=%s", code, out)
	}
}

func TestConfigSetEachKey(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)
	runBin(t, bin, dir, "init")

	if _, code := runBin(t, bin, dir, "config", "set", "require_quest", "true"); code != 0 {
		t.Fatal("set require_quest")
	}
	if _, code := runBin(t, bin, dir, "config", "set", "auto_trailer", "false"); code != 0 {
		t.Fatal("set auto_trailer")
	}
	if _, code := runBin(t, bin, dir, "config", "set", "id_strategy", "random"); code != 0 {
		t.Fatal("set id_strategy")
	}
	cfg, _ := s.Config()
	if !cfg.RequireQuest || cfg.AutoTrailer || cfg.IDStrategy != config.Random {
		t.Fatalf("config not persisted: %+v", cfg)
	}
}

func TestConfigSetTone(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)
	runBin(t, bin, dir, "init")

	if _, code := runBin(t, bin, dir, "config", "set", "tone", "plain"); code != 0 {
		t.Fatal("set tone plain")
	}
	cfg, _ := s.Config()
	if cfg.Tone != config.TonePlain {
		t.Fatalf("tone not persisted: %+v", cfg)
	}

	if _, code := runBin(t, bin, dir, "config", "set", "tone", "loud"); code != 1 {
		t.Fatal("set tone loud: want exit 1")
	}
}

// TestNewJSONNeutralAcrossTones locks in the invariant that --json output
// never carries tone flavor, regardless of SIDE_QUEST_TONE.
func TestNewJSONNeutralAcrossTones(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)

	for _, tone := range []string{"plain", "dcc", ""} {
		t.Setenv("SIDE_QUEST_TONE", tone)
		out, code := runBin(t, bin, dir, "new", "--json", "A title")
		if code != 0 {
			t.Fatalf("new --json tone=%q exit=%d out=%s", tone, code, out)
		}
		for _, word := range []string{"System", "crawler", "dungeon"} {
			if strings.Contains(out, word) {
				t.Errorf("--json under tone %q leaked flavor word %q: %s", tone, word, out)
			}
		}
	}
}

// TestNewHumanFlavoredContainsID locks in that the human `new` confirmation
// still surfaces the created quest's id, even under a non-default tone.
func TestNewHumanFlavoredContainsID(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)

	t.Setenv("SIDE_QUEST_TONE", "plain")
	out, code := runBin(t, bin, dir, "new", "A title")
	if code != 0 {
		t.Fatalf("new exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, "SQ-") {
		t.Errorf("human new output missing id: %q", out)
	}
}

func TestConfigSetRejectsBadKeyValueStrategy(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)
	runBin(t, bin, dir, "init")

	if _, code := runBin(t, bin, dir, "config", "set", "bogus", "x"); code != 1 {
		t.Fatal("bad key should exit 1")
	}
	if _, code := runBin(t, bin, dir, "config", "set", "require_quest", "maybe"); code != 1 {
		t.Fatal("bad bool should exit 1")
	}
	if _, code := runBin(t, bin, dir, "config", "set", "id_strategy", "hash"); code != 1 {
		t.Fatal("bad strategy should exit 1")
	}
	if _, code := runBin(t, bin, dir, "config", "set", "require_quest"); code != 2 {
		t.Fatal("missing value should exit 2")
	}
}
