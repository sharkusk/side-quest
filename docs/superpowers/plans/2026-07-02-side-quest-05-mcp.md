# Phase 4 MCP Server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a stdio MCP frontend (`side-quest serve`) exposing the quest store as ten MCP tools to any MCP-capable agent, plus the store mutators and context helper the update/capture tools need.

**Architecture:** A new `serve` subcommand in `main.go` builds a server from a new `internal/mcp` package (thin handlers over `*store.Store`, using the official Go SDK) and runs it on stdio. Two update mutators land in `store`; a small `internal/capture` helper feeds mechanical git context into `quest_new`. Validation stays in the store; bad input becomes an MCP tool error, not a protocol error.

**Tech Stack:** Go, `github.com/modelcontextprotocol/go-sdk` v1.6.1 (stdio JSON-RPC + struct-inferred tool schemas), existing `store`/`quest`/`config`/`gitcmd` packages.

**Spec:** docs/superpowers/specs/2026-07-02-side-quest-mcp-design.md

## Global Constraints

- **New dependency:** `github.com/modelcontextprotocol/go-sdk v1.6.1` — the project's first dep beyond `gopkg.in/yaml.v3`. Adding it bumps the `go` directive in `go.mod` from `1.22` to `1.25.0` (the SDK's floor; `go get` does this automatically). No hand-picked extra deps beyond the SDK's transitive set.
- **Pure wiring layer:** `cmd/` and `internal/mcp` hold no business logic; validation stays at the store write boundary. The one sanctioned frontend exception: `quest_list` validates its filter values against the enums before listing.
- **Tool errors, not protocol errors:** a handler returns a plain `error` for bad input (invalid enum, missing id, nothing-to-update); the SDK packs it into the tool result with `IsError` set. A handler must never `os.Exit`, panic, or return a transport-level failure for user input.
- **stdout is the protocol channel:** handlers and `serve` write nothing but JSON-RPC to stdout. Any diagnostics go to stderr.
- **Neutral responses:** tool results are plain JSON of the `quest.Quest`/ack shape via `json.MarshalIndent` — no voice/tone.
- **SDK import alias:** inside `internal/mcp` (itself `package mcp`), import the SDK as `sdk "github.com/modelcontextprotocol/go-sdk/mcp"` to avoid the name clash. In `cmd/side-quest`, import our package as `questmcp` and the SDK as `sdk`.
- **Exactly ten tools, names verbatim:** `quest_new`, `quest_list`, `quest_show`, `quest_set_status`, `quest_reclassify`, `quest_update`, `quest_note`, `quest_set_current`, `quest_get_current`, `quest_link_commit`.
- **`quest_new`:** `set_current` defaults false (capture must not move the worktree pointer); mechanical context is prepended to the agent's narrative `context`.
- **Only three new store mutators:** `AppendNote`, `SetTitle`, `MergeTags`. No other `store` changes; no `json` tags added to `quest.Quest`/`config.Config`.
- **Living docs** (`docs/architecture.md` + `README.md`) updated in the same change as behavior (Task 5). Dated `docs/superpowers/` files are frozen — never edited.
- **Committed repo-root `.mcp.json`** uses the `go run ./cmd/side-quest serve` dogfood form (not the `side-quest` PATH form, which is only shown in the README for end users).
- **Commit messages** end with:
  ```
  Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws
  ```

---

## File Structure

- `internal/store/store.go` — add `AppendNote`, `SetTitle`, `MergeTags` (Task 1).
- `internal/store/store_test.go` — their tests (Task 1).
- `internal/capture/capture.go` — **new**: `Mechanical(dir, currentQuest string) string` (Task 2).
- `internal/capture/capture_test.go` — **new** (Task 2).
- `go.mod` / `go.sum` — add the SDK (Task 3).
- `internal/mcp/server.go` — **new**: `NewServer`, `handlers`, `register` (Task 3; extended in Task 4).
- `internal/mcp/tools.go` — **new**: input structs + handlers + JSON helpers (Task 3 for capture/read tools; Task 4 for mutation tools).
- `internal/mcp/server_test.go` — **new**: in-memory client/server integration tests (Task 3; extended in Task 4).
- `cmd/side-quest/serve.go` — **new**: `cmdServe` (Task 3).
- `cmd/side-quest/main.go` — add `serve` case + usage line (Task 3).
- `docs/architecture.md`, `README.md`, `.mcp.json` — Task 5.

---

## Task 1: Store update mutators (`AppendNote`, `SetTitle`, `MergeTags`)

**Files:**
- Modify: `internal/store/store.go` (add after `SetPriority`, ~line 457)
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: existing `s.Update(id, func(*quest.Quest))`, `quest.Quest` fields `Title`/`Body`/`Tags`.
- Produces:
  - `func (s *Store) AppendNote(id, text string) error`
  - `func (s *Store) SetTitle(id, title string) error`
  - `func (s *Store) MergeTags(id string, tags map[string]string) error`

- [ ] **Step 1: Write the failing tests**

Add to `internal/store/store_test.go` (the file already imports `strings`, `testing`, `quest`):

```go
func TestAppendNoteAccumulates(t *testing.T) {
	s := newStore(t)
	q, err := s.Create("noted", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendNote(q.ID, "first finding"); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendNote(q.ID, "second finding"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Body, "first finding") || !strings.Contains(got.Body, "second finding") {
		t.Fatalf("both notes should survive, body=%q", got.Body)
	}
	if err := s.AppendNote(q.ID, "  "); err == nil {
		t.Fatal("empty note text should error")
	}
}

func TestSetTitleReplacesAndRejectsEmpty(t *testing.T) {
	s := newStore(t)
	q, _ := s.Create("old", "", "", "", nil)
	if err := s.SetTitle(q.ID, "new title"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(q.ID)
	if got.Title != "new title" {
		t.Fatalf("title not replaced: %q", got.Title)
	}
	if err := s.SetTitle(q.ID, "   "); err == nil {
		t.Fatal("empty title should error")
	}
}

func TestMergeTagsAddsOverwritesDeletes(t *testing.T) {
	s := newStore(t)
	q, _ := s.Create("tagged", "", "", "", map[string]string{"area": "cli", "keep": "yes"})
	if err := s.MergeTags(q.ID, map[string]string{"area": "mcp", "new": "x", "keep": ""}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(q.ID)
	if got.Tags["area"] != "mcp" || got.Tags["new"] != "x" {
		t.Fatalf("merge/overwrite wrong: %+v", got.Tags)
	}
	if _, ok := got.Tags["keep"]; ok {
		t.Fatalf("empty value should delete key: %+v", got.Tags)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/store/ -run 'TestAppendNote|TestSetTitle|TestMergeTags' -v`
Expected: FAIL — `AppendNote`/`SetTitle`/`MergeTags` undefined (build error).

- [ ] **Step 3: Implement the three mutators**

In `internal/store/store.go`, immediately after `SetPriority` (confirm `strings` is imported — the file already uses `fmt` and `time`; add `strings` to the import block if absent):

```go
// AppendNote appends text to a quest's body as a new, UTC-timestamped entry,
// leaving any existing body intact. The Update closure may run more than once
// under CAS, so it recomputes from the freshly-read body each time.
func (s *Store) AppendNote(id, text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("note text is empty")
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	return s.Update(id, func(q *quest.Quest) {
		var b strings.Builder
		b.WriteString(q.Body)
		if q.Body != "" {
			if !strings.HasSuffix(q.Body, "\n") {
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "--- note %s ---\n\n%s\n", ts, strings.TrimRight(text, "\n"))
		q.Body = b.String()
	})
}

// SetTitle replaces a quest's title. An empty title is rejected — a quest must
// keep a title.
func (s *Store) SetTitle(id, title string) error {
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("title is empty")
	}
	return s.Update(id, func(q *quest.Quest) { q.Title = title })
}

// MergeTags merges tags into a quest's tag map: a non-empty value sets/overwrites
// the key; an empty value deletes it.
func (s *Store) MergeTags(id string, tags map[string]string) error {
	return s.Update(id, func(q *quest.Quest) {
		for k, v := range tags {
			if v == "" {
				delete(q.Tags, k)
				continue
			}
			if q.Tags == nil {
				q.Tags = map[string]string{}
			}
			q.Tags[k] = v
		}
	})
}
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/store/ -run 'TestAppendNote|TestSetTitle|TestMergeTags' -v`
Expected: PASS.

- [ ] **Step 5: Run the full suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go
git commit -m "feat(store): AppendNote, SetTitle, MergeTags mutators for MCP updates" -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws"
```

---

## Task 2: Mechanical context helper (`internal/capture`)

**Files:**
- Create: `internal/capture/capture.go`
- Test: `internal/capture/capture_test.go`

**Interfaces:**
- Consumes: `gitcmd.New(dir).Run(args...) (string, error)`.
- Produces: `func Mechanical(dir, currentQuest string) string`

- [ ] **Step 1: Write the failing test**

Create `internal/capture/capture_test.go`:

```go
package capture

import (
	"strings"
	"testing"

	"github.com/sharkusk/side-quest/internal/gitcmd"
)

func TestMechanicalInRepo(t *testing.T) {
	dir := t.TempDir()
	g := gitcmd.New(dir)
	for _, a := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "Tester"},
		{"commit", "--allow-empty", "-q", "-m", "root"},
	} {
		if _, err := g.Run(a...); err != nil {
			t.Fatal(err)
		}
	}
	out := Mechanical(dir, "SQ-0007")
	for _, want := range []string{"branch:", "head:", "cwd:", "current: SQ-0007"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestMechanicalBestEffortNoCurrent(t *testing.T) {
	dir := t.TempDir() // not a git repo
	out := Mechanical(dir, "")
	if strings.Contains(out, "current:") {
		t.Fatalf("no current expected, got:\n%s", out)
	}
	if !strings.Contains(out, "cwd:") {
		t.Fatalf("cwd should always be present, got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/capture/ -v`
Expected: FAIL — no package/`Mechanical` yet (build error).

- [ ] **Step 3: Implement `Mechanical`**

Create `internal/capture/capture.go`:

```go
// Package capture builds the "mechanical context" recorded on a quest at
// creation: the git branch, short HEAD, cwd, and the worktree's current quest.
// It is best-effort — any piece that can't be read is simply omitted, and it
// never returns an error — so a create is never blocked by a missing git state.
package capture

import (
	"fmt"
	"strings"

	"github.com/sharkusk/side-quest/internal/gitcmd"
)

// Mechanical returns a few greppable labeled lines describing the worktree at
// dir. currentQuest, when non-empty, is included as the active-quest line.
func Mechanical(dir, currentQuest string) string {
	g := gitcmd.New(dir)
	var b strings.Builder
	if branch, err := g.Run("rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		fmt.Fprintf(&b, "branch: %s\n", strings.TrimSpace(branch))
	}
	if head, err := g.Run("rev-parse", "--short", "HEAD"); err == nil {
		fmt.Fprintf(&b, "head: %s\n", strings.TrimSpace(head))
	}
	fmt.Fprintf(&b, "cwd: %s\n", dir)
	if currentQuest != "" {
		fmt.Fprintf(&b, "current: %s\n", currentQuest)
	}
	return strings.TrimRight(b.String(), "\n")
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/capture/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/capture/capture.go internal/capture/capture_test.go
git commit -m "feat(capture): mechanical git-context helper for quest capture" -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws"
```

---

## Task 3: MCP server + capture/read tools + `serve` command

**Files:**
- Modify: `go.mod`, `go.sum` (add the SDK)
- Create: `internal/mcp/server.go`, `internal/mcp/tools.go`, `internal/mcp/server_test.go`
- Create: `cmd/side-quest/serve.go`
- Modify: `cmd/side-quest/main.go` (add `serve` case + usage line)

**Interfaces:**
- Consumes: `store.Open`, `store.Create`, `store.List`, `store.Get`, `store.Current`, `store.SetCurrent`, `capture.Mechanical` (Task 2), `openStore()` (exists in main.go), the SDK.
- Produces (used by Task 4):
  - `func NewServer(s *store.Store) *sdk.Server`
  - `type handlers struct{ store *store.Store }` with `func (h *handlers) register(s *sdk.Server)`
  - `func jsonResult(v any) (*sdk.CallToolResult, any, error)` and `func (h *handlers) result(id string) (*sdk.CallToolResult, any, error)`

> **Implementer note on the SDK:** `sdk.AddTool[In,Out](srv, &sdk.Tool{Name,Description}, handler)` infers the input JSON-Schema from the `In` struct (field descriptions from `jsonschema:"..."` tags; a field is optional when its json tag has `,omitempty`). Handlers use `Out = any` and build `CallToolResult.Content` themselves via `jsonResult`. A handler returning a non-nil `error` is turned into a tool-error result automatically — so return the store's error directly for bad input. The integration test in Step 3 is the source of truth: if the SDK's validation treats a field's required-ness differently than expected, adjust the `In` struct tags to match — that is the intended way to pin this detail.

- [ ] **Step 1: Add the SDK dependency**

Run:
```bash
go get github.com/modelcontextprotocol/go-sdk/mcp@v1.6.1
go mod tidy
```
Expected: `go.mod` now requires `github.com/modelcontextprotocol/go-sdk v1.6.1` and the `go` directive reads `go 1.25.0`; `go.sum` is populated. `go build ./...` still succeeds (existing packages unaffected).

- [ ] **Step 2: Write the failing integration test**

Create `internal/mcp/server_test.go`:

```go
package mcp

import (
	"context"
	"encoding/json"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sharkusk/side-quest/internal/gitcmd"
	"github.com/sharkusk/side-quest/internal/quest"
	"github.com/sharkusk/side-quest/internal/store"
)

// newTestStore makes a throwaway git repo with an identity and an opened store.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	g := gitcmd.New(dir)
	for _, a := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "Tester"},
	} {
		if _, err := g.Run(a...); err != nil {
			t.Fatal(err)
		}
	}
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// dialTest wires an in-memory client to a server backed by s.
func dialTest(t *testing.T, s *store.Store) (*sdk.ClientSession, context.Context) {
	t.Helper()
	ctx := context.Background()
	srv := NewServer(s)
	serverT, clientT := sdk.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverT, nil) // server connects first
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs, ctx
}

func contentText(t *testing.T, res *sdk.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("no content in result")
	}
	tc, ok := res.Content[0].(*sdk.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T, want *sdk.TextContent", res.Content[0])
	}
	return tc.Text
}

func TestListToolsExposesTen(t *testing.T) {
	cs, ctx := dialTest(t, newTestStore(t))
	lt, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(lt.Tools) != 10 {
		names := make([]string, len(lt.Tools))
		for i, tl := range lt.Tools {
			names[i] = tl.Name
		}
		t.Fatalf("want 10 tools, got %d: %v", len(lt.Tools), names)
	}
}

func TestQuestNewThenShow(t *testing.T) {
	cs, ctx := dialTest(t, newTestStore(t))

	res, err := cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "quest_new",
		Arguments: map[string]any{"title": "Fix parser", "type": "bug", "context": "saw a stack trace"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("quest_new tool error: %s", contentText(t, res))
	}
	var created quest.Quest
	if err := json.Unmarshal([]byte(contentText(t, res)), &created); err != nil {
		t.Fatalf("json: %v\n%s", err, contentText(t, res))
	}
	if created.Title != "Fix parser" || created.Type != quest.TypeBug {
		t.Fatalf("bad created quest: %+v", created)
	}
	if created.Context == "" {
		t.Fatal("expected mechanical+narrative context to be recorded")
	}

	res, err = cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "quest_show",
		Arguments: map[string]any{"id": created.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	var shown quest.Quest
	if err := json.Unmarshal([]byte(contentText(t, res)), &shown); err != nil {
		t.Fatal(err)
	}
	if shown.ID != created.ID {
		t.Fatalf("show returned %q, want %q", shown.ID, created.ID)
	}
}

func TestQuestListFilterAndInvalid(t *testing.T) {
	cs, ctx := dialTest(t, newTestStore(t))
	cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_new", Arguments: map[string]any{"title": "a", "type": "bug"}})
	cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_new", Arguments: map[string]any{"title": "b", "type": "feature"}})

	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_list", Arguments: map[string]any{"type": "bug"}})
	if err != nil {
		t.Fatal(err)
	}
	var bugs []quest.Quest
	if err := json.Unmarshal([]byte(contentText(t, res)), &bugs); err != nil {
		t.Fatal(err)
	}
	if len(bugs) != 1 || bugs[0].Type != quest.TypeBug {
		t.Fatalf("filter wrong: %+v", bugs)
	}

	res, err = cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_list", Arguments: map[string]any{"type": "bugg"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("invalid filter should be a tool error")
	}
}

func TestGetCurrentEmpty(t *testing.T) {
	cs, ctx := dialTest(t, newTestStore(t))
	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_get_current", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("get_current errored: %s", contentText(t, res))
	}
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./internal/mcp/ -v`
Expected: FAIL — `NewServer` and the tools don't exist yet (build error).

- [ ] **Step 4: Create `internal/mcp/server.go`**

```go
// Package mcp is the stdio MCP frontend: it exposes the quest store as MCP
// tools for any MCP-capable agent. Each tool decodes typed params, calls one
// store method, and returns the result as JSON. Validation lives in the store;
// bad input becomes an MCP tool error (a returned error), not a protocol error.
// Tool responses are neutral JSON — no voice/tone.
package mcp

import (
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sharkusk/side-quest/internal/store"
)

// Version is advertised to clients in the server implementation info.
const Version = "0.1.0"

// NewServer builds an MCP server exposing the quest tools backed by s.
func NewServer(s *store.Store) *sdk.Server {
	srv := sdk.NewServer(&sdk.Implementation{Name: "side-quest", Version: Version}, nil)
	(&handlers{store: s}).register(srv)
	return srv
}

// handlers holds the store the tool handlers act on.
type handlers struct{ store *store.Store }
```

- [ ] **Step 5: Create `internal/mcp/tools.go` with the capture/read tools**

```go
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sharkusk/side-quest/internal/capture"
	"github.com/sharkusk/side-quest/internal/quest"
)

// register wires every tool onto the server. Task 4 appends the mutation tools.
func (h *handlers) register(s *sdk.Server) {
	sdk.AddTool(s, &sdk.Tool{Name: "quest_new", Description: "Capture a new quest. Mechanical git context (branch/head/cwd/current) is recorded automatically; pass a one-sentence narrative in context."}, h.questNew)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_list", Description: "List quests, optionally filtered by status/type/priority (AND)."}, h.questList)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_show", Description: "Show one quest by id."}, h.questShow)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_get_current", Description: "Return this worktree's current quest id (empty if none)."}, h.questGetCurrent)
}

// --- input types ---

type newIn struct {
	Title      string            `json:"title" jsonschema:"the quest title"`
	Context    string            `json:"context,omitempty" jsonschema:"a one-sentence note on why this quest was captured"`
	Type       string            `json:"type,omitempty" jsonschema:"bug or feature; defaults to feature"`
	Priority   string            `json:"priority,omitempty" jsonschema:"high or low; defaults to low"`
	Tags       map[string]string `json:"tags,omitempty" jsonschema:"optional key/value tags"`
	SetCurrent bool              `json:"set_current,omitempty" jsonschema:"also set this quest as the worktree's current quest"`
}

type listIn struct {
	Status   string `json:"status,omitempty" jsonschema:"filter by status"`
	Type     string `json:"type,omitempty" jsonschema:"filter by type (bug|feature)"`
	Priority string `json:"priority,omitempty" jsonschema:"filter by priority (high|low)"`
}

type idIn struct {
	ID string `json:"id" jsonschema:"the quest id, e.g. SQ-0001"`
}

type emptyIn struct{}

// --- shared helpers ---

// jsonResult renders v as indented JSON text content — the neutral tool payload.
func jsonResult(v any) (*sdk.CallToolResult, any, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(b)}}}, nil, nil
}

// result re-reads a quest by id and returns it — used by the mutating tools so
// the agent sees the post-mutation state.
func (h *handlers) result(id string) (*sdk.CallToolResult, any, error) {
	q, err := h.store.Get(id)
	if err != nil {
		return nil, nil, err
	}
	return jsonResult(q)
}

// --- handlers ---

func (h *handlers) questNew(ctx context.Context, req *sdk.CallToolRequest, in newIn) (*sdk.CallToolResult, any, error) {
	cur, _ := h.store.Current()
	dir, _ := os.Getwd()
	var parts []string
	if mech := capture.Mechanical(dir, cur); mech != "" {
		parts = append(parts, mech)
	}
	if in.Context != "" {
		parts = append(parts, in.Context)
	}
	q, err := h.store.Create(in.Title, strings.Join(parts, "\n\n"), quest.Type(in.Type), quest.Priority(in.Priority), in.Tags)
	if err != nil {
		return nil, nil, err
	}
	if in.SetCurrent {
		if err := h.store.SetCurrent(q.ID); err != nil {
			return nil, nil, err
		}
	}
	return jsonResult(q)
}

func (h *handlers) questList(ctx context.Context, req *sdk.CallToolRequest, in listIn) (*sdk.CallToolResult, any, error) {
	if in.Status != "" && !quest.Status(in.Status).Valid() {
		return nil, nil, fmt.Errorf("invalid status %q", in.Status)
	}
	if in.Type != "" && !quest.Type(in.Type).Valid() {
		return nil, nil, fmt.Errorf("invalid type %q", in.Type)
	}
	if in.Priority != "" && !quest.Priority(in.Priority).Valid() {
		return nil, nil, fmt.Errorf("invalid priority %q", in.Priority)
	}
	all, err := h.store.List()
	if err != nil {
		return nil, nil, err
	}
	filtered := make([]*quest.Quest, 0, len(all))
	for _, q := range all {
		if in.Status != "" && string(q.Status) != in.Status {
			continue
		}
		if in.Type != "" && string(q.Type) != in.Type {
			continue
		}
		if in.Priority != "" && string(q.Priority) != in.Priority {
			continue
		}
		filtered = append(filtered, q)
	}
	return jsonResult(filtered)
}

func (h *handlers) questShow(ctx context.Context, req *sdk.CallToolRequest, in idIn) (*sdk.CallToolResult, any, error) {
	q, err := h.store.Get(in.ID)
	if err != nil {
		return nil, nil, err
	}
	return jsonResult(q)
}

func (h *handlers) questGetCurrent(ctx context.Context, req *sdk.CallToolRequest, in emptyIn) (*sdk.CallToolResult, any, error) {
	cur, err := h.store.Current()
	if err != nil {
		return nil, nil, err
	}
	return jsonResult(struct {
		Current string `json:"current"`
	}{cur})
}
```

- [ ] **Step 6: Create `cmd/side-quest/serve.go`**

```go
package main

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	questmcp "github.com/sharkusk/side-quest/internal/mcp"
)

// cmdServe runs the stdio MCP server until the client disconnects. It is a thin
// frontend: it opens the store for the cwd and hands it to the mcp package.
func cmdServe(args []string) error {
	if len(args) != 0 {
		return &usageErr{"serve takes no arguments"}
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	srv := questmcp.NewServer(s)
	return srv.Run(context.Background(), &sdk.StdioTransport{})
}
```

- [ ] **Step 7: Wire `main.go`**

Add to the `run()` switch:

```go
	case "serve":
		return cmdServe(args)
```

Add to the `usage` const command list (near the other frontends):

```
  serve                           run the stdio MCP server
```

- [ ] **Step 8: Run the integration test to verify it passes**

Run: `go test ./internal/mcp/ -v`
Expected: PASS (`TestListToolsExposesTen` sees 10 tools even though only 4 are registered now — the four capture/read tools plus the six mutation tools are all registered by the end of Task 4; **for this task, temporarily assert the four you have**).

> **Task-3 test scope:** Until Task 4 registers the other six tools, `TestListToolsExposesTen` cannot pass with `10`. For Task 3, write that assertion as `4` and leave a `// TODO(task4): becomes 10` note; Task 4's first step updates it to `10`. All other Task-3 tests (`TestQuestNewThenShow`, `TestQuestListFilterAndInvalid`, `TestGetCurrentEmpty`) pass as written.

- [ ] **Step 9: Run the full suite + build the binary**

Run: `go build ./... && go test ./...`
Expected: PASS across all packages (the new `internal/mcp`, `internal/capture`, and existing ones).

- [ ] **Step 10: Commit**

```bash
git add go.mod go.sum internal/mcp/ cmd/side-quest/serve.go cmd/side-quest/main.go
git commit -m "feat(mcp): stdio server with capture/read tools and serve command" -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws"
```

---

## Task 4: Mutation tools (`set_status`, `reclassify`, `update`, `note`, `set_current`, `link_commit`)

**Files:**
- Modify: `internal/mcp/tools.go` (register + handlers + input types)
- Modify: `internal/mcp/server_test.go` (bump the tool count, add mutation tests)

**Interfaces:**
- Consumes: `store.SetStatus`, `store.SetType`, `store.SetPriority`, `store.SetTitle`/`MergeTags`/`AppendNote` (Task 1), `store.SetCurrent`/`ClearCurrent`, `store.Link`; `h.result(id)`, `jsonResult`, `idIn` (Task 3).
- Produces: the remaining six tool handlers, bringing the server to ten tools.

- [ ] **Step 1: Update the tool-count assertion and add mutation tests**

In `internal/mcp/server_test.go`, change the `TestListToolsExposesTen` assertion from `4` back to `10`. Then add:

```go
func TestSetStatusAndReclassify(t *testing.T) {
	cs, ctx := dialTest(t, newTestStore(t))
	res, _ := cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_new", Arguments: map[string]any{"title": "x"}})
	var q quest.Quest
	json.Unmarshal([]byte(contentText(t, res)), &q)

	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_set_status", Arguments: map[string]any{"id": q.ID, "status": "done"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("set_status error: %s", contentText(t, res))
	}
	var done quest.Quest
	json.Unmarshal([]byte(contentText(t, res)), &done)
	if done.Status != quest.StatusDone {
		t.Fatalf("status not set: %+v", done)
	}

	res, _ = cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_reclassify", Arguments: map[string]any{"id": q.ID, "priority": "high"}})
	var re quest.Quest
	json.Unmarshal([]byte(contentText(t, res)), &re)
	if re.Priority != quest.PriorityHigh {
		t.Fatalf("reclassify failed: %+v", re)
	}

	res, _ = cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_set_status", Arguments: map[string]any{"id": q.ID, "status": "nope"}})
	if !res.IsError {
		t.Fatal("invalid status should be a tool error")
	}
	res, _ = cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_reclassify", Arguments: map[string]any{"id": q.ID}})
	if !res.IsError {
		t.Fatal("reclassify with no field should be a tool error")
	}
}

func TestUpdateAndNote(t *testing.T) {
	cs, ctx := dialTest(t, newTestStore(t))
	res, _ := cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_new", Arguments: map[string]any{"title": "orig", "tags": map[string]any{"keep": "yes"}}})
	var q quest.Quest
	json.Unmarshal([]byte(contentText(t, res)), &q)

	res, _ = cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_update", Arguments: map[string]any{"id": q.ID, "title": "renamed", "tags": map[string]any{"area": "mcp", "keep": ""}}})
	var up quest.Quest
	json.Unmarshal([]byte(contentText(t, res)), &up)
	if up.Title != "renamed" || up.Tags["area"] != "mcp" {
		t.Fatalf("update wrong: %+v", up)
	}
	if _, ok := up.Tags["keep"]; ok {
		t.Fatalf("empty tag value should delete: %+v", up.Tags)
	}

	res, _ = cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_note", Arguments: map[string]any{"id": q.ID, "text": "learned something"}})
	var noted quest.Quest
	json.Unmarshal([]byte(contentText(t, res)), &noted)
	if !strings.Contains(noted.Body, "learned something") {
		t.Fatalf("note not appended: body=%q", noted.Body)
	}

	res, _ = cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_update", Arguments: map[string]any{"id": q.ID}})
	if !res.IsError {
		t.Fatal("update with nothing to change should be a tool error")
	}
}

func TestSetCurrentAndLink(t *testing.T) {
	cs, ctx := dialTest(t, newTestStore(t))
	res, _ := cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_new", Arguments: map[string]any{"title": "cur"}})
	var q quest.Quest
	json.Unmarshal([]byte(contentText(t, res)), &q)

	if _, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_set_current", Arguments: map[string]any{"id": q.ID}}); err != nil {
		t.Fatal(err)
	}
	res, _ = cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_get_current", Arguments: map[string]any{}})
	if !strings.Contains(contentText(t, res), q.ID) {
		t.Fatalf("current not set: %s", contentText(t, res))
	}
	// clear
	cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_set_current", Arguments: map[string]any{"clear": true}})
	res, _ = cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_get_current", Arguments: map[string]any{}})
	if strings.Contains(contentText(t, res), q.ID) {
		t.Fatalf("current not cleared: %s", contentText(t, res))
	}
	// link_commit tolerates an unknown/most-any sha argument shape (Link is tolerant of unknown ids)
	if _, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_link_commit", Arguments: map[string]any{"sha": "HEAD"}}); err != nil {
		t.Fatal(err)
	}
}
```

Add `"strings"` to the test file's imports if not already present.

- [ ] **Step 2: Run to verify the new tests fail**

Run: `go test ./internal/mcp/ -run 'TestSetStatus|TestUpdateAndNote|TestSetCurrentAndLink|TestListToolsExposesTen' -v`
Expected: FAIL — the six tools aren't registered (unknown tool / count is 4).

- [ ] **Step 3: Register the six tools**

In `internal/mcp/tools.go`, extend `register` with:

```go
	sdk.AddTool(s, &sdk.Tool{Name: "quest_set_status", Description: "Set a quest's lifecycle status (open|partial|done|deferred|discarded)."}, h.questSetStatus)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_reclassify", Description: "Change a quest's type and/or priority."}, h.questReclassify)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_update", Description: "Update a quest's title and/or tags (a tag with an empty value is deleted)."}, h.questUpdate)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_note", Description: "Append a timestamped note to a quest's body (non-destructive)."}, h.questNote)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_set_current", Description: "Set this worktree's current quest by id, or clear it with clear:true."}, h.questSetCurrent)
	sdk.AddTool(s, &sdk.Tool{Name: "quest_link_commit", Description: "Apply a commit's Quest:/Completes: trailers to the referenced quests."}, h.questLinkCommit)
```

- [ ] **Step 4: Add the six handlers + input types**

Append to `internal/mcp/tools.go`:

```go
type statusIn struct {
	ID     string `json:"id" jsonschema:"the quest id"`
	Status string `json:"status" jsonschema:"open|partial|done|deferred|discarded"`
}

type reclassifyIn struct {
	ID       string `json:"id" jsonschema:"the quest id"`
	Type     string `json:"type,omitempty" jsonschema:"new type (bug|feature)"`
	Priority string `json:"priority,omitempty" jsonschema:"new priority (high|low)"`
}

type updateIn struct {
	ID    string            `json:"id" jsonschema:"the quest id"`
	Title string            `json:"title,omitempty" jsonschema:"new title"`
	Tags  map[string]string `json:"tags,omitempty" jsonschema:"tags to merge; empty value deletes a key"`
}

type noteIn struct {
	ID   string `json:"id" jsonschema:"the quest id"`
	Text string `json:"text" jsonschema:"the note text to append"`
}

type setCurrentIn struct {
	ID    string `json:"id,omitempty" jsonschema:"the quest id to make current"`
	Clear bool   `json:"clear,omitempty" jsonschema:"clear the current quest instead of setting it"`
}

type shaIn struct {
	SHA string `json:"sha" jsonschema:"the commit sha whose trailers to apply"`
}

func (h *handlers) questSetStatus(ctx context.Context, req *sdk.CallToolRequest, in statusIn) (*sdk.CallToolResult, any, error) {
	if err := h.store.SetStatus(in.ID, quest.Status(in.Status)); err != nil {
		return nil, nil, err
	}
	return h.result(in.ID)
}

func (h *handlers) questReclassify(ctx context.Context, req *sdk.CallToolRequest, in reclassifyIn) (*sdk.CallToolResult, any, error) {
	if in.Type == "" && in.Priority == "" {
		return nil, nil, fmt.Errorf("reclassify needs type and/or priority")
	}
	if in.Type != "" {
		if err := h.store.SetType(in.ID, quest.Type(in.Type)); err != nil {
			return nil, nil, err
		}
	}
	if in.Priority != "" {
		if err := h.store.SetPriority(in.ID, quest.Priority(in.Priority)); err != nil {
			return nil, nil, err
		}
	}
	return h.result(in.ID)
}

func (h *handlers) questUpdate(ctx context.Context, req *sdk.CallToolRequest, in updateIn) (*sdk.CallToolResult, any, error) {
	if in.Title == "" && in.Tags == nil {
		return nil, nil, fmt.Errorf("update needs title and/or tags")
	}
	if in.Title != "" {
		if err := h.store.SetTitle(in.ID, in.Title); err != nil {
			return nil, nil, err
		}
	}
	if in.Tags != nil {
		if err := h.store.MergeTags(in.ID, in.Tags); err != nil {
			return nil, nil, err
		}
	}
	return h.result(in.ID)
}

func (h *handlers) questNote(ctx context.Context, req *sdk.CallToolRequest, in noteIn) (*sdk.CallToolResult, any, error) {
	if err := h.store.AppendNote(in.ID, in.Text); err != nil {
		return nil, nil, err
	}
	return h.result(in.ID)
}

func (h *handlers) questSetCurrent(ctx context.Context, req *sdk.CallToolRequest, in setCurrentIn) (*sdk.CallToolResult, any, error) {
	if in.Clear {
		if err := h.store.ClearCurrent(); err != nil {
			return nil, nil, err
		}
		return jsonResult(struct {
			OK bool `json:"ok"`
		}{true})
	}
	if in.ID == "" {
		return nil, nil, fmt.Errorf("set_current needs an id (or clear:true)")
	}
	if err := h.store.SetCurrent(in.ID); err != nil {
		return nil, nil, err
	}
	return jsonResult(struct {
		OK      bool   `json:"ok"`
		Current string `json:"current"`
	}{true, in.ID})
}

func (h *handlers) questLinkCommit(ctx context.Context, req *sdk.CallToolRequest, in shaIn) (*sdk.CallToolResult, any, error) {
	if err := h.store.Link(in.SHA); err != nil {
		return nil, nil, err
	}
	return jsonResult(struct {
		OK  bool   `json:"ok"`
		SHA string `json:"sha"`
	}{true, in.SHA})
}
```

- [ ] **Step 5: Run the mcp tests to verify they pass**

Run: `go test ./internal/mcp/ -v`
Expected: PASS — all tests, `TestListToolsExposesTen` now sees 10.

- [ ] **Step 6: Run the full suite**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/mcp/tools.go internal/mcp/server_test.go
git commit -m "feat(mcp): status/reclassify/update/note/current/link tools" -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws"
```

---

## Task 5: Living docs + dogfooding `.mcp.json`

**Files:**
- Modify: `docs/architecture.md`, `README.md`
- Create: `.mcp.json`

**Interfaces:** none (docs + config only).

- [ ] **Step 1: Create the repo-root `.mcp.json`**

Create `.mcp.json` (the `go run` dogfood form — no installed binary assumed):

```json
{
  "mcpServers": {
    "side-quest": {
      "command": "go",
      "args": ["run", "./cmd/side-quest", "serve"]
    }
  }
}
```

- [ ] **Step 2: Update `docs/architecture.md`**

Add an "MCP frontend" subsection (H2 to match sibling depth — check the file's headings first, as the CLI subsection did). Content to include:

```markdown
## MCP frontend (`internal/mcp` + `side-quest serve`)

`side-quest serve` runs a stdio MCP server (JSON-RPC over stdin/stdout) built on
`github.com/modelcontextprotocol/go-sdk`. `cmd/side-quest/serve.go` is a thin
frontend: it opens the store for the cwd and hands it to `internal/mcp.NewServer`,
which registers ten tools:

- `quest_new`, `quest_list`, `quest_show`, `quest_get_current` (capture/read)
- `quest_set_status`, `quest_reclassify`, `quest_update`, `quest_note`,
  `quest_set_current`, `quest_link_commit` (mutation)

Each handler decodes typed params (the SDK infers each tool's JSON-Schema from a
Go struct), calls one store method, and returns neutral JSON of the
`quest.Quest`/ack shape. Validation stays in the store; invalid input is returned
as an MCP **tool error** (not a protocol error). The one frontend-side check is
`quest_list` validating its filter values. `quest_new` auto-records mechanical
context (branch/HEAD/cwd/current-quest, via `internal/capture.Mechanical`) ahead
of the agent's narrative note, and only moves the current-quest pointer when
`set_current:true`. stdout carries only JSON-RPC; diagnostics go to stderr.

The three store mutators the update tools use — `AppendNote` (append a dated note
to the body), `SetTitle`, and `MergeTags` (empty value deletes a key) — live in
`store` beside the other setters.
```

- [ ] **Step 3: Update `README.md`**

Add an MCP section (match the README's heading style). Include the end-user PATH form and the dogfooding note:

```markdown
## MCP server

`side-quest serve` runs a stdio MCP server so any MCP-capable agent can capture,
read, and drive quests. Register it with your agent (end-user form, assumes
`side-quest` is on PATH):

```json
{ "mcpServers": { "side-quest": { "command": "side-quest", "args": ["serve"] } } }
```

Tools: `quest_new`, `quest_list`, `quest_show`, `quest_set_status`,
`quest_reclassify`, `quest_update`, `quest_note`, `quest_set_current`,
`quest_get_current`, `quest_link_commit`. Responses are neutral JSON.

**Developing side-quest with side-quest (dogfooding):** this repo's `.mcp.json`
uses `go run ./cmd/side-quest serve`, which recompiles from source on each
launch, so every new session runs your latest code — no install step, and it
won't disturb a `side-quest` you use elsewhere. Restart the server to pick up
code or tool-schema changes. Quest data lives on the git ref and is
binary-version-independent (the on-ref parser is default-tolerant), so switching
binaries mid-session is safe.
```

- [ ] **Step 4: Verify the server starts (smoke) and the full suite is green**

Run:
```bash
go build ./...
go test ./...
```
Expected: PASS. (The MCP behavior is covered by the `internal/mcp` integration tests; no separate stdio smoke needed.)

- [ ] **Step 5: Commit**

```bash
git add .mcp.json docs/architecture.md README.md
git commit -m "docs(mcp): document the MCP server; add dogfooding .mcp.json" -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>" -m "Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws"
```

---

## Self-Review

**1. Spec coverage:**
- §1 architecture (thin `cmdServe` + `internal/mcp`, official SDK, tool-errors, stdout-protocol, dep + go bump) → Tasks 3 (+ Global Constraints). ✓
- §2 ten tools with the exact rules (new defaults set_current false + capture; list validated AND; reclassify/update need ≥1 field; note text required) → Tasks 3 (4 tools) + 4 (6 tools). ✓
- §2 config excluded, full-body-replace excluded → nothing in the plan builds them. ✓
- §3 three store mutators (AppendNote/SetTitle/MergeTags, empty-title reject, empty-tag delete) → Task 1. ✓
- §4 `internal/capture.Mechanical(dir, currentQuest)`, best-effort, fed into quest_new → Task 2 + Task 3 questNew. ✓
- §5 current-quest semantics (set/get/clear; new leaves pointer unless set_current) → Task 3/4 handlers. ✓
- §6 error handling (tool errors, EOF shutdown via Run, stdout discipline) → handler pattern + serve. ✓
- §7 living docs + dogfooding `.mcp.json` (go run form; README both forms) → Task 5. ✓
- §Testing (store mutators, capture best-effort, in-memory integration incl. tools/list=10, round-trips, tool-error on bad enum) → Tasks 1/2/3/4 tests. ✓

**2. Placeholder scan:** No TBD/TODO except the one intentional, fully-specified `// TODO(task4)` count bump, whose resolution is Task 4 Step 1. Every code step shows complete code; every run step shows the command + expected result. ✓

**3. Type consistency:** SDK usage matches the pinned v1.6.1 API (`AddTool[In,Out]`, `ToolHandlerFor` shape `(ctx, *CallToolRequest, In) (*CallToolResult, Out, error)`, `NewServer`/`Run`/`StdioTransport`, `NewInMemoryTransports`/`Server.Connect`/`Client.Connect`/`ClientSession.CallTool`/`ListTools`, `CallToolResult.{Content,IsError}`, `*TextContent`). Store signatures match Task 1 (`AppendNote(id,text)`, `SetTitle(id,title)`, `MergeTags(id,map)`) and existing methods. `handlers`, `jsonResult`, `result`, `idIn`/`emptyIn` defined in Task 3 and reused in Task 4. SDK aliased `sdk` in `internal/mcp`, `questmcp`+`sdk` in `cmd`. ✓
