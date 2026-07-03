# Phase 3 CLI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the human-facing `side-quest` subcommands (`init`, `new`, `list`, `show`, `status`, `reclassify`, `config get/set`) as a thin wiring layer over the existing `store` library.

**Architecture:** Each command is an adapter: parse flags with the stdlib `flag` package (one `flag.FlagSet` per subcommand), call exactly one existing `store` method, render the result (human text or `--json`). The new human handlers live in `cmd/side-quest/cli.go`; rendering in `cmd/side-quest/render.go`; the existing `run()` switch and `usage` string in `main.go` gain the new cases. Two small library setters the CLI needs are added first.

**Tech Stack:** Go standard library only (`flag`, `encoding/json`, `text/tabwriter`). No new third-party dependencies (project uses only `gopkg.in/yaml.v3`).

**Spec:** docs/superpowers/specs/2026-07-02-side-quest-cli-design.md

## Global Constraints

- **Zero new dependencies.** Stdlib only. The one project dependency stays `gopkg.in/yaml.v3`.
- **`cmd/` is a pure wiring layer.** No business logic; validation stays at the store write boundary. The *only* exception the spec allows: `list` validates its filter values against the enums before listing.
- **Only two changes outside `cmd/`:** `store.SetAutoTrailer(bool) error` and `config.Strategy.Valid() bool`. Nothing else in `internal/` changes — in particular do **not** add `json` tags to `quest.Quest` or `config.Config`.
- **`--json` marshals the raw struct.** `show`/`new` emit one `*quest.Quest`; `list` emits `[]*quest.Quest`; `config get` emits `config.Config`. No bespoke DTO. JSON keys are therefore the Go struct field names (e.g. `"ID"`, `"Title"`).
- **Exit codes:** usage errors (wrong positional count, malformed `--tag`, `reclassify` with no flag, unknown/`-help` flag) exit **2** via a `*usageErr`; all other errors (store/validation/not-found/bad bool/invalid filter) exit **1** via the existing `main()` path.
- **Flags precede positional args.** The stdlib `flag` package stops parsing at the first non-flag argument, so `new --type bug "title"` works but `new "title" --type bug` treats `--type` as junk. Usage strings and tests must put flags first.
- **Neutral tone.** Human output carries no DCC voice (that is Phase 5).
- **Living docs** (`docs/architecture.md` + `README.md`) are updated in the SAME change as the behavior — done in the final task, once the full command set exists.
- **Commit messages** end with these two footer lines:
  ```
  Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws
  ```

---

## File Structure

- `internal/store/store.go` — add `SetAutoTrailer` (Task 1).
- `internal/store/store_test.go` — add its test (Task 1).
- `internal/config/config.go` — add `Strategy.Valid()` (Task 1).
- `internal/config/config_test.go` — add its test (Task 1).
- `cmd/side-quest/cli.go` — **new**: `usageErr`, `tagFlag`, and the command handlers (`cmdInit`, `cmdNew` in Task 2; `cmdList`, `cmdShow` in Task 3; `cmdStatus`, `cmdReclassify` in Task 4; `cmdConfig`+`cmdConfigGet`+`cmdConfigSet`+`parseBool` in Task 5).
- `cmd/side-quest/render.go` — **new**: `emitJSON` (Task 2); `renderList`, `renderShow` (Task 3); `renderConfig` (Task 5).
- `cmd/side-quest/main.go` — extend `run()` switch, `usage` string, and add the `usageErr` exit-2 branch in `main()` (Task 2 adds the branch + init/new cases; later tasks add their cases + usage lines).
- `cmd/side-quest/cli_test.go` — **new**: e2e tests via the built binary, reusing the `buildBinary`/`newRepo`/`runBin` helpers from `main_test.go` (added to across Tasks 2–5).
- `docs/architecture.md`, `README.md` — updated in Task 5.

---

## Task 1: Library prerequisites (`SetAutoTrailer`, `Strategy.Valid`)

**Files:**
- Modify: `internal/store/store.go` (add after `SetRequireQuest`, ~line 508)
- Modify: `internal/config/config.go` (add after the `Strategy` consts, ~line 18)
- Test: `internal/store/store_test.go`, `internal/config/config_test.go`

**Interfaces:**
- Consumes: existing `store.mutate`, `config.Marshal`, `snap.Config`; `config.Sequential`, `config.Random`.
- Produces:
  - `func (s *Store) SetAutoTrailer(v bool) error`
  - `func (c Strategy) Valid() bool` (method on `config.Strategy`)

- [ ] **Step 1: Write the failing config test**

Add to `internal/config/config_test.go`:

```go
func TestStrategyValid(t *testing.T) {
	for _, s := range []Strategy{Sequential, Random} {
		if !s.Valid() {
			t.Errorf("Strategy %q should be valid", s)
		}
	}
	for _, s := range []Strategy{"", "seq", "rand", "Sequential"} {
		if Strategy(s).Valid() {
			t.Errorf("Strategy %q should be invalid", s)
		}
	}
}
```

- [ ] **Step 2: Write the failing store test**

Add to `internal/store/store_test.go` (mirror `TestSetRequireQuestPersists`):

```go
func TestSetAutoTrailerPersists(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.Config()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AutoTrailer {
		t.Fatal("fresh store should have auto_trailer=true (Default)")
	}
	if err := s.SetAutoTrailer(false); err != nil {
		t.Fatal(err)
	}
	cfg, err = s.Config()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AutoTrailer {
		t.Fatal("SetAutoTrailer(false) did not persist")
	}
}
```

- [ ] **Step 3: Run both tests to verify they fail**

Run: `go test ./internal/config/ ./internal/store/ -run 'TestStrategyValid|TestSetAutoTrailerPersists' -v`
Expected: FAIL — `s.Valid undefined` and `s.SetAutoTrailer undefined` (build errors).

- [ ] **Step 4: Implement `Strategy.Valid()`**

In `internal/config/config.go`, immediately after the `Sequential`/`Random` const block (line 18):

```go
// Valid reports whether s is one of the known id strategies.
func (s Strategy) Valid() bool {
	switch s {
	case Sequential, Random:
		return true
	}
	return false
}
```

- [ ] **Step 5: Implement `SetAutoTrailer`**

In `internal/store/store.go`, immediately after the `SetRequireQuest` method:

```go
// SetAutoTrailer flips the auto_trailer flag on the ref (controls whether the
// prepare-commit-msg hook injects the current-quest trailer).
func (s *Store) SetAutoTrailer(v bool) error {
	return s.mutate("side-quest: set auto_trailer", func(snap *Snapshot, tx *txn) error {
		cfg := snap.Config
		cfg.AutoTrailer = v
		data, err := config.Marshal(cfg)
		if err != nil {
			return err
		}
		tx.put(configPath, data)
		return nil
	})
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/config/ ./internal/store/ -run 'TestStrategyValid|TestSetAutoTrailerPersists' -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/store/store.go internal/store/store_test.go
git commit -m "feat(store,config): SetAutoTrailer + Strategy.Valid for CLI config set" -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws"
```

---

## Task 2: `init` and `new` commands (+ CLI scaffolding)

**Files:**
- Create: `cmd/side-quest/cli.go`
- Create: `cmd/side-quest/render.go`
- Create: `cmd/side-quest/cli_test.go`
- Modify: `cmd/side-quest/main.go` (add `errors` import; `usageErr` branch in `main()`; `init`/`new` cases in `run()`; two `usage` lines)

**Interfaces:**
- Consumes: `openStore()` (exists in `main.go`), `store.Init()`, `store.Create(title, context string, typ quest.Type, prio quest.Priority, tags map[string]string) (*quest.Quest, error)`, `store.SetCurrent(id string) error`, `quest.Quest`.
- Produces (used by later tasks):
  - `type usageErr struct{ msg string }` with `func (e *usageErr) Error() string`
  - `type tagFlag struct{ m map[string]string }` implementing `flag.Value`
  - `func emitJSON(w io.Writer, v any) error`
  - handler signature convention: `func cmdX(args []string) error`

- [ ] **Step 1: Write the failing e2e tests**

Create `cmd/side-quest/cli_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./cmd/side-quest/ -run 'TestNew|TestInit' -v`
Expected: FAIL — build error (`cmdNew`/`cmdInit`/`emitJSON` undefined; `new`/`init` unknown commands).

- [ ] **Step 3: Create `render.go` with `emitJSON`**

Create `cmd/side-quest/render.go`:

```go
// Rendering helpers for the human CLI: JSON emission and (added in later tasks)
// human-readable tables and detail views.
package main

import (
	"encoding/json"
	"io"
)

// emitJSON writes v as indented JSON followed by a newline. The value is a raw
// library struct (*quest.Quest, []*quest.Quest, config.Config) — the JSON shape
// is the struct shape, which the MCP layer will reuse.
func emitJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
```

- [ ] **Step 4: Create `cli.go` with scaffolding + `cmdInit` + `cmdNew`**

Create `cmd/side-quest/cli.go`:

```go
// Human-facing CLI subcommands (init, new, list, show, status, reclassify,
// config). Each handler parses its own flags with the stdlib flag package and
// calls exactly one store method — validation lives in the store, not here.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sharkusk/side-quest/internal/quest"
)

// usageErr marks a wrong-usage problem (missing arg, malformed flag). main()
// maps it to exit code 2; every other error exits 1.
type usageErr struct{ msg string }

func (e *usageErr) Error() string { return e.msg }

// tagFlag collects repeated --tag key=value flags into a map (a flag.Value).
type tagFlag struct{ m map[string]string }

func (t *tagFlag) String() string { return "" }

func (t *tagFlag) Set(v string) error {
	i := strings.IndexByte(v, '=')
	if i <= 0 {
		return fmt.Errorf("tag must be key=value, got %q", v)
	}
	if t.m == nil {
		t.m = map[string]string{}
	}
	t.m[v[:i]] = v[i+1:]
	return nil
}

// newFlagSet returns a FlagSet that stays silent on error (we surface parse
// failures ourselves as usageErr) so output is not double-printed.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func cmdInit(args []string) error {
	if len(args) != 0 {
		return &usageErr{"init takes no arguments"}
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	if err := s.Init(); err != nil {
		return err
	}
	fmt.Println("side-quest: initialized")
	return nil
}

func cmdNew(args []string) error {
	fs := newFlagSet("new")
	var typ, prio, context string
	var setCurrent, asJSON bool
	var tags tagFlag
	fs.StringVar(&typ, "type", "", "quest type (bug|feature)")
	fs.StringVar(&prio, "priority", "", "quest priority (high|low)")
	fs.StringVar(&context, "context", "", "context note")
	fs.Var(&tags, "tag", "tag as key=value (repeatable)")
	fs.BoolVar(&setCurrent, "current", false, "also set as this worktree's current quest")
	fs.BoolVar(&asJSON, "json", false, "emit the created quest as JSON")
	if err := fs.Parse(args); err != nil {
		return &usageErr{err.Error()}
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return &usageErr{"new needs exactly one <title> (quote multi-word titles; put flags before it)"}
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	q, err := s.Create(rest[0], context, quest.Type(typ), quest.Priority(prio), tags.m)
	if err != nil {
		return err
	}
	if setCurrent {
		if err := s.SetCurrent(q.ID); err != nil {
			return err
		}
	}
	if asJSON {
		return emitJSON(os.Stdout, q)
	}
	fmt.Println(q.ID)
	return nil
}
```

- [ ] **Step 5: Wire `main.go` — `usageErr` branch, `run()` cases, usage lines**

In `cmd/side-quest/main.go`, add `"errors"` to the imports. Replace the `main()` error handling:

```go
func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2:]); err != nil {
		var ue *usageErr
		if errors.As(err, &ue) {
			fmt.Fprintln(os.Stderr, "side-quest:", ue.msg)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "side-quest:", err)
		os.Exit(1)
	}
}
```

Add these cases to the `run()` switch (above `default`):

```go
	case "init":
		return cmdInit(args)
	case "new":
		return cmdNew(args)
```

Add these two lines to the `usage` const, in the command list (before `link`, since they are the primary human commands):

```
  init                            create the quest ref (_config.yaml)
  new <title> [--type --priority --context --tag k=v --current --json]
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./cmd/side-quest/ -run 'TestNew|TestInit' -v`
Expected: PASS (5 tests).

- [ ] **Step 7: Run the full suite (no regressions in existing hook tests)**

Run: `go test ./...`
Expected: PASS across all packages.

- [ ] **Step 8: Commit**

```bash
git add cmd/side-quest/cli.go cmd/side-quest/render.go cmd/side-quest/cli_test.go cmd/side-quest/main.go
git commit -m "feat(cmd): init and new subcommands" -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws"
```

---

## Task 3: `list` and `show` commands

**Files:**
- Modify: `cmd/side-quest/cli.go` (add `cmdList`, `cmdShow`)
- Modify: `cmd/side-quest/render.go` (add `renderList`, `renderShow`)
- Modify: `cmd/side-quest/main.go` (`list`/`show` cases + usage lines)
- Test: `cmd/side-quest/cli_test.go`

**Interfaces:**
- Consumes: `store.List() ([]*quest.Quest, error)`, `store.Get(id string) (*quest.Quest, error)` (returns `store.ErrNotFound`), `quest.Status/Type/Priority` with `.Valid()`, `emitJSON`.
- Produces: `func renderList(w io.Writer, quests []*quest.Quest)`, `func renderShow(w io.Writer, q *quest.Quest)`.

- [ ] **Step 1: Write the failing tests**

Add to `cmd/side-quest/cli_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./cmd/side-quest/ -run 'TestList|TestShow' -v`
Expected: FAIL — build error (`cmdList`/`cmdShow`/`renderList`/`renderShow` undefined; `list`/`show` unknown commands).

- [ ] **Step 3: Add `renderList` and `renderShow` to `render.go`**

Add these imports to `render.go`: `"fmt"`, `"sort"`, `"strings"`, `"text/tabwriter"`, `"time"`, and `"github.com/sharkusk/side-quest/internal/quest"`. Append:

```go
// renderList prints an aligned table of quests, or a friendly line when empty.
func renderList(w io.Writer, quests []*quest.Quest) {
	if len(quests) == 0 {
		fmt.Fprintln(w, "no quests")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSTATUS\tTYPE\tPRIORITY\tTITLE")
	for _, q := range quests {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", q.ID, q.Status, q.Type, q.Priority, q.Title)
	}
	tw.Flush()
}

// renderShow prints one quest's frontmatter fields, then a blank line and the
// body. Absent optional fields (completed, commits, context, tags, body) are
// omitted.
func renderShow(w io.Writer, q *quest.Quest) {
	fmt.Fprintf(w, "id:        %s\n", q.ID)
	fmt.Fprintf(w, "title:     %s\n", q.Title)
	fmt.Fprintf(w, "status:    %s\n", q.Status)
	fmt.Fprintf(w, "type:      %s\n", q.Type)
	fmt.Fprintf(w, "priority:  %s\n", q.Priority)
	fmt.Fprintf(w, "created:   %s\n", q.Created.Format(time.RFC3339))
	if q.Completed != nil {
		fmt.Fprintf(w, "completed: %s\n", q.Completed.Format(time.RFC3339))
	}
	if len(q.Commits) > 0 {
		fmt.Fprintf(w, "commits:   %s\n", strings.Join(q.Commits, ", "))
	}
	if q.Context != "" {
		fmt.Fprintf(w, "context:   %s\n", q.Context)
	}
	if len(q.Tags) > 0 {
		keys := make([]string, 0, len(q.Tags))
		for k := range q.Tags {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(w, "tag:       %s=%s\n", k, q.Tags[k])
		}
	}
	if q.Body != "" {
		fmt.Fprintf(w, "\n%s\n", strings.TrimRight(q.Body, "\n"))
	}
}
```

- [ ] **Step 4: Add `cmdList` and `cmdShow` to `cli.go`**

Append to `cli.go`:

```go
func cmdList(args []string) error {
	fs := newFlagSet("list")
	var status, typ, prio string
	var asJSON bool
	fs.StringVar(&status, "status", "", "filter by status")
	fs.StringVar(&typ, "type", "", "filter by type (bug|feature)")
	fs.StringVar(&prio, "priority", "", "filter by priority (high|low)")
	fs.BoolVar(&asJSON, "json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return &usageErr{err.Error()}
	}
	if status != "" && !quest.Status(status).Valid() {
		return fmt.Errorf("invalid status %q", status)
	}
	if typ != "" && !quest.Type(typ).Valid() {
		return fmt.Errorf("invalid type %q", typ)
	}
	if prio != "" && !quest.Priority(prio).Valid() {
		return fmt.Errorf("invalid priority %q", prio)
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	all, err := s.List()
	if err != nil {
		return err
	}
	filtered := make([]*quest.Quest, 0, len(all))
	for _, q := range all {
		if status != "" && string(q.Status) != status {
			continue
		}
		if typ != "" && string(q.Type) != typ {
			continue
		}
		if prio != "" && string(q.Priority) != prio {
			continue
		}
		filtered = append(filtered, q)
	}
	if asJSON {
		return emitJSON(os.Stdout, filtered)
	}
	renderList(os.Stdout, filtered)
	return nil
}

func cmdShow(args []string) error {
	fs := newFlagSet("show")
	var asJSON bool
	fs.BoolVar(&asJSON, "json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return &usageErr{err.Error()}
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return &usageErr{"show needs exactly one <id>"}
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	q, err := s.Get(rest[0])
	if err != nil {
		return err
	}
	if asJSON {
		return emitJSON(os.Stdout, q)
	}
	renderShow(os.Stdout, q)
	return nil
}
```

- [ ] **Step 5: Wire `main.go`**

Add to the `run()` switch:

```go
	case "list":
		return cmdList(args)
	case "show":
		return cmdShow(args)
```

Add to the `usage` const command list:

```
  list [--status --type --priority --json]   list quests (filters combine)
  show <id> [--json]              show one quest
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./cmd/side-quest/ -run 'TestList|TestShow' -v`
Expected: PASS.

- [ ] **Step 7: Run the full suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add cmd/side-quest/cli.go cmd/side-quest/render.go cmd/side-quest/cli_test.go cmd/side-quest/main.go
git commit -m "feat(cmd): list and show subcommands with filters and --json" -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws"
```

---

## Task 4: `status` and `reclassify` commands

**Files:**
- Modify: `cmd/side-quest/cli.go` (add `cmdStatus`, `cmdReclassify`)
- Modify: `cmd/side-quest/main.go` (`status`/`reclassify` cases + usage lines)
- Test: `cmd/side-quest/cli_test.go`

**Interfaces:**
- Consumes: `store.SetStatus(id string, st quest.Status) error`, `store.SetType(id string, t quest.Type) error`, `store.SetPriority(id string, p quest.Priority) error` (each validates and returns an error on an invalid value).
- Produces: `func cmdStatus(args []string) error`, `func cmdReclassify(args []string) error`.

- [ ] **Step 1: Write the failing tests**

Add to `cmd/side-quest/cli_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./cmd/side-quest/ -run 'TestStatus|TestReclassify' -v`
Expected: FAIL — build error (`cmdStatus`/`cmdReclassify` undefined; unknown commands).

- [ ] **Step 3: Add `cmdStatus` and `cmdReclassify` to `cli.go`**

Append to `cli.go`:

```go
func cmdStatus(args []string) error {
	if len(args) != 2 {
		return &usageErr{"status needs <id> <status>"}
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	return s.SetStatus(args[0], quest.Status(args[1]))
}

func cmdReclassify(args []string) error {
	fs := newFlagSet("reclassify")
	var typ, prio string
	fs.StringVar(&typ, "type", "", "new type (bug|feature)")
	fs.StringVar(&prio, "priority", "", "new priority (high|low)")
	if err := fs.Parse(args); err != nil {
		return &usageErr{err.Error()}
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return &usageErr{"reclassify needs exactly one <id>"}
	}
	if typ == "" && prio == "" {
		return &usageErr{"reclassify needs --type and/or --priority"}
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	id := rest[0]
	if typ != "" {
		if err := s.SetType(id, quest.Type(typ)); err != nil {
			return err
		}
	}
	if prio != "" {
		if err := s.SetPriority(id, quest.Priority(prio)); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Wire `main.go`**

Add to the `run()` switch:

```go
	case "status":
		return cmdStatus(args)
	case "reclassify":
		return cmdReclassify(args)
```

Add to the `usage` const command list:

```
  status <id> <status>            set a quest's status
  reclassify <id> [--type --priority]  change a quest's type/priority
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./cmd/side-quest/ -run 'TestStatus|TestReclassify' -v`
Expected: PASS.

- [ ] **Step 6: Run the full suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/side-quest/cli.go cmd/side-quest/main.go cmd/side-quest/cli_test.go
git commit -m "feat(cmd): status and reclassify subcommands" -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws"
```

---

## Task 5: `config get`/`config set` commands + living docs

**Files:**
- Modify: `cmd/side-quest/cli.go` (add `cmdConfig`, `cmdConfigGet`, `cmdConfigSet`, `parseBool`)
- Modify: `cmd/side-quest/render.go` (add `renderConfig`)
- Modify: `cmd/side-quest/main.go` (`config` case + usage line)
- Modify: `docs/architecture.md`, `README.md`
- Test: `cmd/side-quest/cli_test.go`

**Interfaces:**
- Consumes: `store.Config() (config.Config, error)`, `store.SetRequireQuest(bool) error`, `store.SetAutoTrailer(bool) error` (Task 1), `store.SetStrategy(config.Strategy) error`, `config.Strategy.Valid()` (Task 1), `config.Config`.
- Produces: `func renderConfig(w io.Writer, c config.Config)` and the config command handlers.

- [ ] **Step 1: Write the failing tests**

Add to `cmd/side-quest/cli_test.go` (add `"github.com/sharkusk/side-quest/internal/config"` to its imports):

```go
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
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./cmd/side-quest/ -run 'TestConfig' -v`
Expected: FAIL — build error (`cmdConfig`/`renderConfig` undefined; `config` unknown command).

- [ ] **Step 3: Add `renderConfig` to `render.go`**

Add `"github.com/sharkusk/side-quest/internal/config"` to `render.go` imports. Append:

```go
// renderConfig prints the effective on-ref configuration as aligned key: value
// lines.
func renderConfig(w io.Writer, c config.Config) {
	fmt.Fprintf(w, "id_prefix:     %s\n", c.IDPrefix)
	fmt.Fprintf(w, "id_strategy:   %s\n", c.IDStrategy)
	fmt.Fprintf(w, "seq_next:      %d\n", c.SeqNext)
	fmt.Fprintf(w, "seq_width:     %d\n", c.SeqWidth)
	fmt.Fprintf(w, "tone:          %s\n", c.Tone)
	fmt.Fprintf(w, "auto_trailer:  %t\n", c.AutoTrailer)
	fmt.Fprintf(w, "require_quest: %t\n", c.RequireQuest)
}
```

- [ ] **Step 4: Add the config handlers to `cli.go`**

Add `"github.com/sharkusk/side-quest/internal/config"` to `cli.go` imports. Append:

```go
func cmdConfig(args []string) error {
	if len(args) < 1 {
		return &usageErr{"config needs a subcommand: get | set"}
	}
	switch args[0] {
	case "get":
		return cmdConfigGet(args[1:])
	case "set":
		return cmdConfigSet(args[1:])
	default:
		return &usageErr{fmt.Sprintf("unknown config subcommand %q (want get|set)", args[0])}
	}
}

func cmdConfigGet(args []string) error {
	fs := newFlagSet("config get")
	var asJSON bool
	fs.BoolVar(&asJSON, "json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return &usageErr{err.Error()}
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	cfg, err := s.Config()
	if err != nil {
		return err
	}
	if asJSON {
		return emitJSON(os.Stdout, cfg)
	}
	renderConfig(os.Stdout, cfg)
	return nil
}

func cmdConfigSet(args []string) error {
	if len(args) != 2 {
		return &usageErr{"config set needs <key> <value>"}
	}
	key, val := args[0], args[1]
	s, err := openStore()
	if err != nil {
		return err
	}
	switch key {
	case "require_quest":
		b, err := parseBool(val)
		if err != nil {
			return err
		}
		return s.SetRequireQuest(b)
	case "auto_trailer":
		b, err := parseBool(val)
		if err != nil {
			return err
		}
		return s.SetAutoTrailer(b)
	case "id_strategy":
		st := config.Strategy(val)
		if !st.Valid() {
			return fmt.Errorf("invalid id_strategy %q (want sequential|random)", val)
		}
		return s.SetStrategy(st)
	default:
		return fmt.Errorf("unknown config key %q (settable: require_quest, auto_trailer, id_strategy)", key)
	}
}

// parseBool accepts only "true" or "false" (stricter than strconv.ParseBool).
func parseBool(v string) (bool, error) {
	switch v {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("want true or false, got %q", v)
}
```

- [ ] **Step 5: Wire `main.go`**

Add to the `run()` switch:

```go
	case "config":
		return cmdConfig(args)
```

Add to the `usage` const command list:

```
  config get [--json]             show effective config
  config set <key> <value>        set require_quest | auto_trailer | id_strategy
```

- [ ] **Step 6: Run the config tests to verify they pass**

Run: `go test ./cmd/side-quest/ -run 'TestConfig' -v`
Expected: PASS.

- [ ] **Step 7: Update `docs/architecture.md`**

Add a "CLI" subsection (place it after the CRUD/store description, before or near the hooks section — match the document's existing heading style). Content to include:

```markdown
### Command-line interface (`cmd/side-quest`)

Beside the git-hook subcommands (`link`, `current`, `commit-msg`,
`prepare-commit-msg`, `install-hooks`), the binary exposes the human commands:

- `init` — create the quest ref.
- `new <title>` — create a quest; flags `--type`, `--priority`, `--context`,
  `--tag k=v` (repeatable), `--current` (also set the worktree's current quest),
  `--json`.
- `list` — list quests; filters `--status`/`--type`/`--priority` (validated,
  combined with AND) and `--json`.
- `show <id>` — show one quest; `--json`.
- `status <id> <status>` — set the lifecycle status.
- `reclassify <id> [--type --priority]` — change type and/or priority.
- `config get` / `config set <key> <value>` — read config; set `require_quest`,
  `auto_trailer`, or `id_strategy`.

Handlers live in `cli.go`; rendering in `render.go`. Each command is a thin
adapter over one `store` method — validation stays at the store write boundary
(the sole exception: `list` validates its filter values). `--json` marshals the
raw `quest.Quest`/`config.Config` structs, so the JSON keys are the Go field
names; this is the stable machine surface the MCP layer reuses. Flags must
precede positional arguments (stdlib `flag` behavior). Usage errors exit 2; all
other errors exit 1.

The CLI relies on two store/config additions: `store.SetAutoTrailer` and
`config.Strategy.Valid()`.
```

- [ ] **Step 8: Update `README.md`**

Add a "Usage" section (or extend the existing commands section — match the README's style) showing the common commands:

```markdown
## Usage

```
side-quest init
side-quest new "Fix the flaky parser test" --type bug --priority high
side-quest list --status open --type bug
side-quest show SQ-0001
side-quest status SQ-0001 done
side-quest reclassify SQ-0001 --priority low
side-quest config set require_quest true
side-quest config get
```

Add `--json` to `new`, `list`, `show`, or `config get` for machine-readable
output. Flags come before the title/id positional argument.
```

- [ ] **Step 9: Run the full suite**

Run: `go test ./...`
Expected: PASS across all packages.

- [ ] **Step 10: Commit**

```bash
git add cmd/side-quest/cli.go cmd/side-quest/render.go cmd/side-quest/main.go cmd/side-quest/cli_test.go docs/architecture.md README.md
git commit -m "feat(cmd): config get/set subcommands; document the CLI" -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws"
```

---

## Self-Review

**1. Spec coverage:**
- §1 architecture / pure wiring / file layout (`cli.go`, `render.go`) → Tasks 2–5. ✓
- §2 command table: `init`, `new` (all flags incl. `--current`, `--tag`, `--json`) → Task 2; `list` (+filters), `show` → Task 3; `status`, `reclassify` → Task 4; `config get`/`set` (3 keys) → Task 5. ✓
- §2 `list` filter validation (loud typo failure, AND combine) → Task 3 `cmdList` + `TestListInvalidFilterExitsOne`. ✓
- §2 `config set` key dispatch table (require_quest/auto_trailer/id_strategy; unknown key errors; bad bool/strategy errors) → Task 5. ✓
- §3 library additions `SetAutoTrailer`, `Strategy.Valid()` → Task 1. ✓
- §4 output contract: neutral tone, empty "no quests", RFC3339, `--json` raw struct, error exit codes (2 usage / 1 runtime) → `usageErr` (Task 2), renderers (Tasks 3, 5), tests throughout. ✓
- §5 living docs (architecture.md + README, same change) → Task 5. ✓
- §6 out of scope (tone, `$EDITOR`, MCP, importer) → nothing in the plan builds these. ✓
- Testing bullets incl. the carry-forward e2e (`new --type buggg` → non-zero + empty ref) → `TestNewInvalidTypeExitsNonZeroEmptyRef`. ✓

**2. Placeholder scan:** No TBD/TODO/"handle errors"/"similar to". Every code step shows full code; every run step shows the command and expected result. ✓

**3. Type consistency:** `usageErr`, `tagFlag`, `newFlagSet`, `emitJSON`, `renderList`/`renderShow`/`renderConfig`, and the `cmdX(args []string) error` convention are defined in the task that first uses them and referenced consistently after. Store/quest/config signatures match the code read from `store.go`/`quest.go`/`config.go` (`Create(title, context string, typ quest.Type, prio quest.Priority, tags map[string]string)`, `Get`/`List`/`SetStatus`/`SetType`/`SetPriority`/`SetStrategy`/`Config`/`SetCurrent`, `ErrNotFound`). ✓
