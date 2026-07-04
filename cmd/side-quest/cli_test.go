package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharkusk/side-quest/internal/config"
	"github.com/sharkusk/side-quest/internal/gitcmd"
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

// writeEditor writes an executable fake-$EDITOR script and returns its path.
func writeEditor(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fake-editor.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestEditRoundTrip: `edit <id>` opens the quest in $EDITOR and writes the saved
// buffer back to the ref.
func TestEditRoundTrip(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)
	q, err := s.Create("original title", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	// A fake editor that appends a body line to the temp file (arg $1).
	t.Setenv("EDITOR", writeEditor(t, `printf 'a note from the editor\n' >> "$1"`))
	out, code := runBin(t, bin, dir, "edit", q.ID)
	if code != 0 {
		t.Fatalf("edit exit=%d out=%s", code, out)
	}
	got, err := s.Get(q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Body, "a note from the editor") {
		t.Fatalf("edit did not persist the body: %q", got.Body)
	}

	// Shorthand id resolves, and a no-op edit writes nothing.
	t.Setenv("EDITOR", writeEditor(t, `exit 0`))
	out, code = runBin(t, bin, dir, "edit", "1")
	if code != 0 {
		t.Fatalf("no-op edit exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, "no changes") {
		t.Errorf("expected a no-changes message, got %q", out)
	}
}

// TestEditRejectsInvalidBuffer: a saved buffer that no longer parses is refused,
// the stored quest is left untouched, and the message points at the kept file.
func TestEditRejectsInvalidBuffer(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)
	q, err := s.Create("keep me", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", writeEditor(t, `printf 'this is not a quest file' > "$1"`))
	out, code := runBin(t, bin, dir, "edit", q.ID)
	if code == 0 {
		t.Fatalf("edit of an unparseable buffer should fail; out=%s", out)
	}
	got, _ := s.Get(q.ID)
	if got.Title != "keep me" {
		t.Errorf("rejected edit mutated the quest: %+v", got)
	}
}

// TestPerCommandHelp: `<cmd> -h` / `--help` prints a clean, command-specific
// help screen (synopsis + each flag with its description) and exits 0, rather
// than the raw "flag: help requested" usage error.
func TestPerCommandHelp(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)

	cases := []struct {
		args     []string
		contains []string
	}{
		{[]string{"new", "-h"}, []string{"side-quest new", "-type", "bug|feature", "-tag", "-current"}},
		{[]string{"show", "--help"}, []string{"side-quest show", "-no-wrap", "-json"}},
		{[]string{"list", "-h"}, []string{"side-quest list", "-status", "-priority"}},
		{[]string{"reclassify", "-h"}, []string{"side-quest reclassify", "-type", "-priority"}},
	}
	for _, c := range cases {
		out, code := runBin(t, bin, dir, c.args...)
		if code != 0 {
			t.Errorf("%v: exit=%d, want 0\n%s", c.args, code, out)
		}
		for _, want := range c.contains {
			if !strings.Contains(out, want) {
				t.Errorf("%v: help missing %q\n%s", c.args, want, out)
			}
		}
	}
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

func TestNewCapturesMechanicalContext(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)
	// A commit so branch/head resolve for the capture.
	if _, err := gitcmd.New(dir).Run("commit", "--allow-empty", "-q", "-m", "root"); err != nil {
		t.Fatal(err)
	}

	out, code := runBin(t, bin, dir, "new", "--context", "why now", "--json", "Do a thing")
	if code != 0 {
		t.Fatalf("new exit=%d out=%s", code, out)
	}
	var q quest.Quest
	if err := json.Unmarshal([]byte(out), &q); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	for _, want := range []string{"branch:", "head:", "cwd:", "why now"} {
		if !strings.Contains(q.Context, want) {
			t.Errorf("quest context missing %q:\n%s", want, q.Context)
		}
	}
	// Mechanical capture precedes the user's own context.
	if strings.Index(q.Context, "branch:") > strings.Index(q.Context, "why now") {
		t.Errorf("user context should follow mechanical capture:\n%s", q.Context)
	}
}

func TestListTagFilter(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)
	runBin(t, bin, dir, "new", "--tag", "area=cli", "One")
	runBin(t, bin, dir, "new", "--tag", "area=map", "Two")

	out, code := runBin(t, bin, dir, "list", "--tag", "area=cli", "--json")
	if code != 0 {
		t.Fatalf("list exit=%d out=%s", code, out)
	}
	var got []quest.Quest
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if len(got) != 1 || got[0].Tags["area"] != "cli" {
		t.Fatalf("tag filter wrong, want one area=cli quest: %+v", got)
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

// TestNoteConfirmationRoutesThroughVoice (SQ-0027): the note confirmation now
// renders through the voice layer like its sibling mutations, so under dcc it is
// flavored rather than the bland "noted <id>" it printed before.
func TestNoteConfirmationRoutesThroughVoice(t *testing.T) {
	t.Setenv("SIDE_QUEST_TONE", "dcc")
	bin := buildBinary(t)
	dir, s := newRepo(t)
	q, err := s.Create("a task", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	out, code := runBin(t, bin, dir, "note", q.ID, "a note")
	if code != 0 {
		t.Fatalf("note exit=%d out=%s", code, out)
	}
	got := strings.TrimSpace(out)
	if !strings.Contains(got, q.ID) {
		t.Errorf("note confirmation missing id: %q", got)
	}
	if got == "noted "+q.ID {
		t.Errorf("note confirmation not routed through voice (still bland): %q", got)
	}
}

// TestInitWithRemoteChoosesRandomAndSaysSo (SQ-0030): initializing a repo that
// already has a remote defaults to random ids and tells the user.
func TestInitWithRemoteChoosesRandomAndSaysSo(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)
	g := gitcmd.New(dir)
	if _, err := g.Run("remote", "add", "origin", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	out, code := runBin(t, bin, dir, "init")
	if code != 0 {
		t.Fatalf("init exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, "random") {
		t.Errorf("expected a random-ids notice, got:\n%s", out)
	}
	// The created quest carries a random (hex) id, not SQ-0001.
	cout, _ := runBin(t, bin, dir, "new", "a task", "--json")
	if strings.Contains(cout, "SQ-0001") {
		t.Errorf("expected a random id, got sequential:\n%s", cout)
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
