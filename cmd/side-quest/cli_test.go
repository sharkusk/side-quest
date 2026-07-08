package main

import (
	"encoding/json"
	"fmt"
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

// Environment keys the fake editor (see TestMain) reads to decide what to do to
// the file it is handed.
const (
	fakeEditorAction = "SIDE_QUEST_TEST_EDITOR_ACTION"
	fakeEditorText   = "SIDE_QUEST_TEST_EDITOR_TEXT"
)

// TestMain lets this test binary re-exec itself as a fake $EDITOR (the
// helper-process idiom): when fakeEditorAction is set in the environment we act as
// the editor and exit, rather than running the suite. A real cross-platform
// executable avoids the shell-script fakes that Windows refused to exec ("%1 is not
// a valid Win32 application").
func TestMain(m *testing.M) {
	if action, ok := os.LookupEnv(fakeEditorAction); ok {
		runFakeEditor(action, os.Getenv(fakeEditorText), os.Args[len(os.Args)-1])
		return // unreached: runFakeEditor always exits
	}
	// Build the CLI once for the whole package; every buildBinary(t) shares it.
	bin, cleanup, err := buildSharedBinary()
	if err != nil {
		fmt.Fprintln(os.Stderr, "test setup:", err)
		os.Exit(1)
	}
	sharedBin = bin
	code := m.Run()
	cleanup()
	os.Exit(code)
}

// runFakeEditor performs one action on path, then exits: "append" adds text as a
// new line, "write" replaces the file with text, "noop" leaves it untouched.
func runFakeEditor(action, text, path string) {
	switch action {
	case "append":
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			os.Exit(1)
		}
		_, werr := f.WriteString(text + "\n")
		if cerr := f.Close(); werr != nil || cerr != nil {
			os.Exit(1)
		}
	case "write":
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			os.Exit(1)
		}
	case "noop":
		// leave the file untouched
	default:
		os.Exit(2)
	}
	os.Exit(0)
}

// useFakeEditor points $EDITOR at this test binary re-invoked as the fake editor
// (see TestMain), configured to perform action ("append"/"write"/"noop") with text
// when the editor "saves".
func useFakeEditor(t *testing.T, action, text string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", self)
	t.Setenv(fakeEditorAction, action)
	t.Setenv(fakeEditorText, text)
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

	// A fake editor that appends a body line to the temp file.
	useFakeEditor(t, "append", "a note from the editor")
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
	useFakeEditor(t, "noop", "")
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
	useFakeEditor(t, "write", "this is not a quest file")
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

// SQ-0070: `list --show-tag KEY` renders a column of that tag's values.
func TestListShowTagColumn(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)
	runBin(t, bin, dir, "new", "--tag", "launch=alpha", "One")
	runBin(t, bin, dir, "new", "Two")

	out, code := runBin(t, bin, dir, "list", "--show-tag", "launch")
	if code != 0 {
		t.Fatalf("list --show-tag exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, "LAUNCH") {
		t.Errorf("expected a LAUNCH column header:\n%s", out)
	}
	if !strings.Contains(out, "alpha") {
		t.Errorf("expected the launch tag value in the column:\n%s", out)
	}
	if !strings.Contains(out, "Two") {
		t.Errorf("untagged quest should still be listed:\n%s", out)
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

// TestListDefaultsToOpenAndPartial (SQ-0043): a bare `list` is the "what's
// outstanding?" view, so it shows only open and partial quests; --all restores
// every status, while an explicit --status still selects exactly that status.
func TestListDefaultsToOpenAndPartial(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)

	runBin(t, bin, dir, "new", "Open one")     // SQ-0001
	runBin(t, bin, dir, "new", "Partial one")  // SQ-0002
	runBin(t, bin, dir, "new", "Done one")     // SQ-0003
	runBin(t, bin, dir, "new", "Deferred one") // SQ-0004
	runBin(t, bin, dir, "status", "SQ-0002", "partial")
	runBin(t, bin, dir, "status", "SQ-0003", "done")
	runBin(t, bin, dir, "status", "SQ-0004", "deferred")

	ids := func(args ...string) map[string]bool {
		out, code := runBin(t, bin, dir, append(args, "--json")...)
		if code != 0 {
			t.Fatalf("list %v exit=%d out=%s", args, code, out)
		}
		var got []quest.Quest
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("json: %v\n%s", err, out)
		}
		m := make(map[string]bool, len(got))
		for _, q := range got {
			m[q.ID] = true
		}
		return m
	}

	// Bare list: only open + partial.
	if got := ids("list"); !got["SQ-0001"] || !got["SQ-0002"] || got["SQ-0003"] || got["SQ-0004"] {
		t.Fatalf("default list should show only open+partial, got %v", got)
	}
	// --all: every status.
	if got := ids("list", "--all"); len(got) != 4 {
		t.Fatalf("--all should show all four, got %v", got)
	}
	// Explicit --status wins over the default narrowing.
	if got := ids("list", "--status", "done"); !got["SQ-0003"] || len(got) != 1 {
		t.Fatalf("--status done should show only the done quest, got %v", got)
	}
}

// TestListFilterExpression (SQ-0038): --filter takes a boolean expression over
// bare enum values and key=value tags; an explicit expression suppresses the
// open+partial default, a bad expression exits 1, and --filter cannot be
// combined with the simple filter flags.
func TestListFilterExpression(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)

	runBin(t, bin, dir, "new", "--type", "bug", "Bug one")      // SQ-0001
	runBin(t, bin, dir, "new", "--type", "feature", "Feat one") // SQ-0002
	runBin(t, bin, dir, "status", "SQ-0001", "done")            // the bug is now done

	ids := func(args ...string) map[string]bool {
		out, code := runBin(t, bin, dir, append(args, "--json")...)
		if code != 0 {
			t.Fatalf("list %v exit=%d out=%s", args, code, out)
		}
		var got []quest.Quest
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("json: %v\n%s", err, out)
		}
		m := make(map[string]bool, len(got))
		for _, q := range got {
			m[q.ID] = true
		}
		return m
	}

	// "not done" -> only the open feature (SQ-0001 is done).
	if got := ids("list", "--filter", "not done"); !got["SQ-0002"] || got["SQ-0001"] {
		t.Fatalf(`--filter "not done" got %v`, got)
	}
	// An explicit expression suppresses the open+partial default, so the done
	// bug reappears: "bug or feature" matches both quests.
	if got := ids("list", "--filter", "bug or feature"); !got["SQ-0001"] || !got["SQ-0002"] {
		t.Fatalf(`--filter "bug or feature" got %v`, got)
	}
	// A malformed expression is a value error (exit 1).
	if _, code := runBin(t, bin, dir, "list", "--filter", "banana"); code != 1 {
		t.Fatalf("bad --filter should exit 1, got %d", code)
	}
	// Combining --filter with the simple flags is a usage error (exit 2).
	if _, code := runBin(t, bin, dir, "list", "--filter", "bug", "--status", "open"); code != 2 {
		t.Fatalf("--filter with --status should exit 2, got %d", code)
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

// TestExportWritesNativeFilePerQuest: `export <dir>` writes one round-trippable
// SQ-*.md per quest, across every status, creating the dir if missing (SQ-0101).
func TestExportWritesNativeFilePerQuest(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)
	runBin(t, bin, dir, "init")

	q1, err := s.Create("first quest", "some context", quest.TypeFeature, quest.PriorityLow, nil)
	if err != nil {
		t.Fatal(err)
	}
	q2, err := s.Create("second quest", "", quest.TypeBug, quest.PriorityHigh, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(q2.ID, quest.StatusDiscarded); err != nil { // a non-active status must still export
		t.Fatal(err)
	}
	q2.Status = quest.StatusDiscarded // reflect the transition in our expectation

	out := filepath.Join(t.TempDir(), "sub", "export-out") // nested + missing: MkdirAll must create it
	if o, code := runBin(t, bin, dir, "export", out); code != 0 {
		t.Fatalf("export exit=%d out=%s", code, o)
	}

	for _, want := range []*quest.Quest{q1, q2} {
		data, err := os.ReadFile(filepath.Join(out, want.ID+".md"))
		if err != nil {
			t.Fatalf("missing export file for %s: %v", want.ID, err)
		}
		got, err := quest.Unmarshal(want.ID, data)
		if err != nil {
			t.Fatalf("export for %s does not parse: %v", want.ID, err)
		}
		if got.Title != want.Title || got.Status != want.Status || got.Type != want.Type {
			t.Errorf("%s round-trip mismatch: got title=%q status=%q type=%q", want.ID, got.Title, got.Status, got.Type)
		}
	}
}

// TestConfigSetLocalOnlyAndSyncSkips: local_only persists, shows in `config get`,
// and makes `sync` a themed no-op that publishes nothing to the remote (SQ-0100).
func TestConfigSetLocalOnlyAndSyncSkips(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)
	runBin(t, bin, dir, "init")

	// A bare origin so sync has a remote it must decline to push to.
	origin := t.TempDir()
	if _, err := gitcmd.New(origin).Run("init", "--bare", "-q"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitcmd.New(dir).Run("remote", "add", "origin", origin); err != nil {
		t.Fatal(err)
	}

	if _, code := runBin(t, bin, dir, "config", "set", "local_only", "true"); code != 0 {
		t.Fatal("set local_only")
	}
	if cfg, _ := s.Config(); !cfg.LocalOnly {
		t.Fatalf("local_only not persisted: %+v", cfg)
	}
	if out, code := runBin(t, bin, dir, "config", "get"); code != 0 || !strings.Contains(out, "local_only") {
		t.Fatalf("config get missing local_only: %s", out)
	}

	runBin(t, bin, dir, "new", "a quest") // local is now ahead of the empty remote
	t.Setenv("SIDE_QUEST_TONE", "plain")
	out, code := runBin(t, bin, dir, "sync")
	if code != 0 {
		t.Fatalf("local-only sync exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, "local-only") {
		t.Errorf("sync did not announce local-only: %q", out)
	}
	if _, err := gitcmd.New(origin).Run("rev-parse", "--verify", "-q", "refs/side-quest/quests"); err == nil {
		t.Error("local-only sync pushed a quest ref to origin")
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

func TestShowRendersCommitMessages(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)
	t.Setenv("SIDE_QUEST_TONE", "plain")

	out, _ := runBin(t, bin, dir, "new", "Commit render")
	id := idFromCreated(t, out)
	if _, code := runBin(t, bin, dir, "current", id); code != 0 {
		t.Fatalf("set current exit=%d", code)
	}

	// A real commit carrying the quest trailer, then link it.
	g := gitcmd.New(dir)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run("add", "f.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run("commit", "-m", "feat: a thing\n\nbody detail here\n\nQuest: "+id); err != nil {
		t.Fatal(err)
	}
	if _, code := runBin(t, bin, dir, "link", "HEAD"); code != 0 {
		t.Fatalf("link exit=%d", code)
	}

	// Default: subject line, no full body.
	out, code := runBin(t, bin, dir, "show", id)
	if code != 0 || !strings.Contains(out, "feat: a thing") {
		t.Fatalf("show default: exit=%d out=%s", code, out)
	}
	if strings.Contains(out, "body detail here") {
		t.Errorf("default show must not print the commit body:\n%s", out)
	}

	// --full: includes the body.
	out, code = runBin(t, bin, dir, "show", "--full", id)
	if code != 0 || !strings.Contains(out, "body detail here") {
		t.Fatalf("show --full must print the body: exit=%d out=%s", code, out)
	}
}
