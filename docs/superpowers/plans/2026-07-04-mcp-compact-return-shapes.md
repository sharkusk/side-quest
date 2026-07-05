# MCP compact return shapes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Slim the MCP tool return shapes so an agent is never force-fed unbounded quest bodies — `quest_list`/`quest_new` return a compact summary, the six mutating tools return minimal acks, and `quest_show` stays the one full-detail read.

**Architecture:** Add a presentation-only `questSummary` projection in `internal/mcp`. `quest_list` maps its filtered results through it; `quest_new` returns the summary of the created quest; the six mutation handlers return small inline ack structs and the now-orphaned `result`/`resultVoiced` helpers are removed. No store or CLI change.

**Tech Stack:** Go, `github.com/modelcontextprotocol/go-sdk/mcp`, `encoding/json`, standard `testing`.

## Global Constraints

- **Scope is `internal/mcp` only.** No change to the store (`internal/store`, `internal/quest`) or the CLI.
- **No back-compat / versioning.** These outputs feed an agent, not a versioned API.
- **`questSummary` uses lowercase JSON keys** (`id`, `title`, `commit_count`, …), matching the existing ack style (`{ok, current}`, `{installed, …}`).
- **`quest.Quest` is unchanged and still serialises with CAPITALISED Go-field JSON keys** (`ID`, `Title`, `Context`, `Body`, …) because it carries only `yaml` tags. `quest_show` therefore still emits capitalised keys — do not "fix" this.
- **⚠️ Go's `encoding/json` matches keys case-insensitively.** Unmarshaling a summary or ack into a `quest.Quest` will *silently succeed* (e.g. `"status"` → `Status`), masking a shape regression. In tests, unmarshal into `questSummary` or an explicit ack struct, and assert that dropped fields (body/context text) are **absent** from the raw JSON.
- **Voicing is preserved exactly where it is today** — `quest_new`, `quest_set_status`, `quest_note` append a second tone block via `h.voiced(...)`; the other three mutations do not.
- **`commit_count` is always present** in the summary (an explicit `0` is intended).
- **Commit convention:** subject ends `(SQ-0052)`; body; then a `Quest: SQ-0052` trailer (link-only — do NOT use `Completes:`, the quest stays open until both tasks land), then the repo's `Co-Authored-By:` / `Claude-Session:` trailer lines as on recent commits.

---

### Task 1: Summary projection + `quest_list`/`quest_new` return summaries

**Files:**
- Modify: `internal/mcp/tools.go` (add `time` import; add `questSummary` + `summarize`; change `questList` and `questNew`)
- Test: `internal/mcp/server_test.go`

**Interfaces:**
- Produces (used by Task 2 and tests):
  - `type questSummary struct { ID, Title, Status, Type, Priority string; Completed *time.Time; Tags map[string]string; CommitCount int }` with JSON keys `id, title, status, type, priority, completed (omitempty), tags (omitempty), commit_count`.
  - `func summarize(q *quest.Quest) questSummary`
- Consumes (already present): `jsonResult(v any)`, `h.voiced(res, line)`, `quest.Quest` fields (`ID, Title, Status, Type, Priority, Completed, Tags, Commits`).

- [ ] **Step 1: Write the failing test — `quest_list` returns summaries, not bodies**

Add to `internal/mcp/server_test.go`:

```go
func TestQuestListReturnsSummaries(t *testing.T) {
	cs, ctx := dialTest(t, newTestStore(t))
	res, _ := cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_new",
		Arguments: map[string]any{"title": "with body", "context": "SECRETCONTEXT"}})
	var created questSummary
	if err := json.Unmarshal([]byte(contentText(t, res)), &created); err != nil {
		t.Fatalf("quest_new should return a summary: %v\n%s", err, contentText(t, res))
	}
	cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_note",
		Arguments: map[string]any{"id": created.ID, "text": "SECRETNOTE"}})

	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_list", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	text := contentText(t, res)
	if strings.Contains(text, "SECRETNOTE") || strings.Contains(text, "SECRETCONTEXT") {
		t.Fatalf("quest_list leaked body/context into the summary:\n%s", text)
	}
	if !strings.Contains(text, "commit_count") {
		t.Fatalf("summary missing commit_count:\n%s", text)
	}
	var got []questSummary
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("list is not an array of summaries: %v", err)
	}
	if len(got) != 1 || got[0].Title != "with body" {
		t.Fatalf("unexpected summaries: %+v", got)
	}
}
```

- [ ] **Step 2: Run it — verify it fails to compile**

Run: `go test ./internal/mcp/ -run TestQuestListReturnsSummaries`
Expected: FAIL — `undefined: questSummary` (the type does not exist yet).

- [ ] **Step 3: Add the `time` import and the summary type + mapper**

In `internal/mcp/tools.go`, add `"time"` to the import block (with the stdlib imports). Then add, just above `// --- input types ---`:

```go
// questSummary is the compact, list/triage projection of a quest for the MCP
// surface (SQ-0052): the light identifying fields only, with Context and Body
// dropped and Commits collapsed to a count, so an agent listing a backlog is
// not force-fed every quest's full body. Lowercase JSON keys match the ack
// style used elsewhere in this package (quest.Quest itself, carrying only yaml
// tags, still marshals with capitalised keys via quest_show).
type questSummary struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Status      string            `json:"status"`
	Type        string            `json:"type"`
	Priority    string            `json:"priority"`
	Completed   *time.Time        `json:"completed,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	CommitCount int               `json:"commit_count"`
}

func summarize(q *quest.Quest) questSummary {
	return questSummary{
		ID:          q.ID,
		Title:       q.Title,
		Status:      string(q.Status),
		Type:        string(q.Type),
		Priority:    string(q.Priority),
		Completed:   q.Completed,
		Tags:        q.Tags,
		CommitCount: len(q.Commits),
	}
}
```

- [ ] **Step 4: Map `quest_list` results through `summarize`**

In `internal/mcp/tools.go`, in `questList`, replace the accumulation + return (currently building `filtered []*quest.Quest` and `return jsonResult(filtered)`) so the loop appends summaries. The filter conditions are unchanged; only the collected type and the return change:

```go
	summaries := make([]questSummary, 0, len(all))
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
		if !quest.MatchTags(q.Tags, in.Tags) {
			continue
		}
		summaries = append(summaries, summarize(q))
	}
	return jsonResult(summaries)
```

- [ ] **Step 5: Return a summary from `quest_new`**

In `questNew`, change the result line from `res, meta, err := jsonResult(q)` to:

```go
	res, meta, err := jsonResult(summarize(q))
```

Leave the following `h.voiced(res, func(v *voice.Voice) string { return v.QuestCreated(q.ID) })` line untouched (`q` is still in scope).

- [ ] **Step 6: Run the new test — verify it passes**

Run: `go test ./internal/mcp/ -run TestQuestListReturnsSummaries`
Expected: PASS.

- [ ] **Step 7: Retarget the existing list/new tests to the summary shape**

Three existing tests assert against the old full-quest list/new output. Update them:

**(a)** `TestQuestNewThenShow` — the summary has no `Context` field, so verify context via `quest_show` instead. Replace the body of the test from the first `var created` through the context check with:

```go
	var created questSummary
	if err := json.Unmarshal([]byte(contentText(t, res)), &created); err != nil {
		t.Fatalf("json: %v\n%s", err, contentText(t, res))
	}
	if created.Title != "Fix parser" || created.Type != "bug" {
		t.Fatalf("bad created quest: %+v", created)
	}
```

Then, in the `quest_show` section lower in the same test, after unmarshaling `shown quest.Quest` and checking `shown.ID`, add:

```go
	if shown.Context == "" {
		t.Fatal("quest_show should still carry the recorded context")
	}
```

**(b)** `TestQuestListFilterAndInvalid` — change `var bugs []quest.Quest` to `var bugs []questSummary`, and the check `if len(bugs) != 1 || bugs[0].Type != quest.TypeBug {` to `if len(bugs) != 1 || bugs[0].Type != "bug" {`.

**(c)** `TestQuestListTagFilter` — change `var got []quest.Quest` to `var got []questSummary` (the `got[0].Tags["area"]` check is unchanged; `questSummary` has a `Tags` field).

- [ ] **Step 8: Run the whole package — verify green**

Run: `go test ./internal/mcp/`
Expected: PASS (all tests). `TestMutationVoiceBlock` still passes: it unmarshals `quest_new`'s content[0] into `quest.Quest` but only reads `.ID`, which case-insensitively matches the summary's `id` — leave that test unchanged.

- [ ] **Step 9: Commit**

```bash
git add internal/mcp/tools.go internal/mcp/server_test.go
git commit -m "feat: quest_list/quest_new return a compact summary (SQ-0052)" \
  -m "Add a questSummary projection (no Context/Body, Commits->count) and return it from quest_list and quest_new, so an agent listing a backlog is not fed every quest's full body." \
  -m "Quest: SQ-0052"
# append the repo's Co-Authored-By / Claude-Session trailer lines as on recent commits
```

---

### Task 2: Mutation tools return minimal acks; remove orphaned helpers

**Files:**
- Modify: `internal/mcp/tools.go` (rewrite six handlers; delete `result` + `resultVoiced`)
- Test: `internal/mcp/server_test.go`

**Interfaces:**
- Consumes: `jsonResult`, `h.voiced`, `h.store.Get`, `h.store.ResolveCommit`, `h.store.ReplaceCommit`, `h.store.RemoveCommit`, `h.store.SetStatus`, `h.store.Reclassify`, `h.store.Modify`, `h.store.AppendNote`.
- Produces (ack shapes, all lowercase JSON keys):
  - `quest_set_status` → `{ok:true, id, status}` (voiced)
  - `quest_reclassify` → `{ok:true, id, type?, priority?}` (`omitempty` on each)
  - `quest_update` → `{ok:true, id, title?, tags}` where `tags` is the merged post-write set (one re-read)
  - `quest_note` → `{ok:true, id}` (voiced)
  - `quest_relink_commit` → `{ok:true, id, old_sha, new_sha}` (`new_sha` = resolved canonical hash)
  - `quest_unlink_commit` → `{ok:true, id, sha}`

- [ ] **Step 1: Write the failing tests — acks, no bodies**

Replace `TestSetStatusAndReclassify` and `TestUpdateAndNote` in `internal/mcp/server_test.go` with ack-shape assertions, and retarget `TestUnlinkAndRelinkCommitTools`.

Replace `TestSetStatusAndReclassify` entirely with:

```go
func TestSetStatusAndReclassify(t *testing.T) {
	cs, ctx := dialTest(t, newTestStore(t))
	res, _ := cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_new", Arguments: map[string]any{"title": "x"}})
	var q questSummary
	json.Unmarshal([]byte(contentText(t, res)), &q)

	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_set_status", Arguments: map[string]any{"id": q.ID, "status": "done"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("set_status error: %s", contentText(t, res))
	}
	var st struct {
		OK     bool   `json:"ok"`
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(contentText(t, res)), &st); err != nil {
		t.Fatal(err)
	}
	if !st.OK || st.ID != q.ID || st.Status != "done" {
		t.Fatalf("set_status ack wrong: %+v", st)
	}

	res, _ = cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_reclassify", Arguments: map[string]any{"id": q.ID, "priority": "high"}})
	var rc struct {
		OK       bool   `json:"ok"`
		ID       string `json:"id"`
		Type     string `json:"type"`
		Priority string `json:"priority"`
	}
	json.Unmarshal([]byte(contentText(t, res)), &rc)
	if !rc.OK || rc.Priority != "high" || rc.Type != "" {
		t.Fatalf("reclassify ack wrong (type should be omitted): %+v", rc)
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
```

Replace `TestUpdateAndNote` entirely with:

```go
func TestUpdateAndNote(t *testing.T) {
	cs, ctx := dialTest(t, newTestStore(t))
	res, _ := cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_new", Arguments: map[string]any{"title": "orig", "tags": map[string]any{"keep": "yes"}}})
	var q questSummary
	json.Unmarshal([]byte(contentText(t, res)), &q)

	res, _ = cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_update", Arguments: map[string]any{"id": q.ID, "title": "renamed", "tags": map[string]any{"area": "mcp", "keep": ""}}})
	var up struct {
		OK    bool              `json:"ok"`
		ID    string            `json:"id"`
		Title string            `json:"title"`
		Tags  map[string]string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(contentText(t, res)), &up); err != nil {
		t.Fatal(err)
	}
	if !up.OK || up.Title != "renamed" || up.Tags["area"] != "mcp" {
		t.Fatalf("update ack wrong: %+v", up)
	}
	if _, ok := up.Tags["keep"]; ok {
		t.Fatalf("empty tag value should delete in the merged result: %+v", up.Tags)
	}

	res, _ = cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_note", Arguments: map[string]any{"id": q.ID, "text": "learned something"}})
	txt := contentText(t, res)
	if strings.Contains(txt, "learned something") {
		t.Fatalf("note ack must not echo the body: %s", txt)
	}
	var nk struct {
		OK bool   `json:"ok"`
		ID string `json:"id"`
	}
	json.Unmarshal([]byte(txt), &nk)
	if !nk.OK || nk.ID != q.ID {
		t.Fatalf("note ack wrong: %+v", nk)
	}

	res, _ = cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_update", Arguments: map[string]any{"id": q.ID}})
	if !res.IsError {
		t.Fatal("update with nothing to change should be a tool error")
	}
	res, _ = cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_update", Arguments: map[string]any{"id": q.ID, "tags": map[string]any{}}})
	if !res.IsError {
		t.Fatal("update with empty tags object and no title should be a tool error")
	}
}
```

In `TestUnlinkAndRelinkCommitTools`, replace the two blocks that unmarshal into `quest.Quest` and check `.Commits`. For the relink call, replace the `var relinked quest.Quest ...` block with:

```go
	var rl struct {
		OK     bool   `json:"ok"`
		ID     string `json:"id"`
		OldSHA string `json:"old_sha"`
		NewSHA string `json:"new_sha"`
	}
	if err := json.Unmarshal([]byte(contentText(t, res)), &rl); err != nil {
		t.Fatal(err)
	}
	if !rl.OK || rl.NewSHA != c2 {
		t.Fatalf("relink ack should echo the resolved new sha %s: %+v", c2, rl)
	}
```

For the unlink call, replace the `var unlinked quest.Quest ...` block with:

```go
	var ul struct {
		OK  bool   `json:"ok"`
		ID  string `json:"id"`
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal([]byte(contentText(t, res)), &ul); err != nil {
		t.Fatal(err)
	}
	if !ul.OK {
		t.Fatalf("unlink ack wrong: %+v", ul)
	}
	// Confirm the effect via a full read.
	shown, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "quest_show", Arguments: map[string]any{"id": q.ID}})
	if err != nil {
		t.Fatal(err)
	}
	var after quest.Quest
	json.Unmarshal([]byte(contentText(t, shown)), &after)
	if len(after.Commits) != 0 {
		t.Fatalf("unlink did not remove the sha: %v", after.Commits)
	}
```

- [ ] **Step 2: Run the tests — verify they fail**

Run: `go test ./internal/mcp/ -run 'TestSetStatusAndReclassify|TestUpdateAndNote|TestUnlinkAndRelinkCommitTools'`
Expected: FAIL — the mutations still return full quests, so e.g. the note ack contains `"learned something"` and `rc.Type` is populated / `rl.NewSHA` is empty.

- [ ] **Step 3: Rewrite the six mutation handlers**

In `internal/mcp/tools.go`, replace the six handler bodies.

`questSetStatus`:

```go
func (h *handlers) questSetStatus(ctx context.Context, req *sdk.CallToolRequest, in statusIn) (*sdk.CallToolResult, any, error) {
	if err := h.store.SetStatus(in.ID, quest.Status(in.Status)); err != nil {
		return nil, nil, err
	}
	res, meta, err := jsonResult(struct {
		OK     bool   `json:"ok"`
		ID     string `json:"id"`
		Status string `json:"status"`
	}{true, in.ID, in.Status})
	if err != nil {
		return res, meta, err
	}
	return h.voiced(res, func(v *voice.Voice) string { return v.StatusSet(in.ID, quest.Status(in.Status)) }), meta, nil
}
```

`questReclassify`:

```go
func (h *handlers) questReclassify(ctx context.Context, req *sdk.CallToolRequest, in reclassifyIn) (*sdk.CallToolResult, any, error) {
	if in.Type == "" && in.Priority == "" {
		return nil, nil, fmt.Errorf("reclassify needs type and/or priority")
	}
	if err := h.store.Reclassify(in.ID, quest.Type(in.Type), quest.Priority(in.Priority)); err != nil {
		return nil, nil, err
	}
	return jsonResult(struct {
		OK       bool   `json:"ok"`
		ID       string `json:"id"`
		Type     string `json:"type,omitempty"`
		Priority string `json:"priority,omitempty"`
	}{true, in.ID, in.Type, in.Priority})
}
```

`questUpdate` (re-reads to report the merged tag set):

```go
func (h *handlers) questUpdate(ctx context.Context, req *sdk.CallToolRequest, in updateIn) (*sdk.CallToolResult, any, error) {
	if in.Title == "" && len(in.Tags) == 0 {
		return nil, nil, fmt.Errorf("update needs title and/or tags")
	}
	if err := h.store.Modify(in.ID, in.Title, in.Tags); err != nil {
		return nil, nil, err
	}
	q, err := h.store.Get(in.ID) // re-read: tags merge, so the result isn't knowable from inputs
	if err != nil {
		return nil, nil, err
	}
	return jsonResult(struct {
		OK    bool              `json:"ok"`
		ID    string            `json:"id"`
		Title string            `json:"title,omitempty"`
		Tags  map[string]string `json:"tags,omitempty"`
	}{true, in.ID, in.Title, q.Tags})
}
```

`questNote`:

```go
func (h *handlers) questNote(ctx context.Context, req *sdk.CallToolRequest, in noteIn) (*sdk.CallToolResult, any, error) {
	if err := h.store.AppendNote(in.ID, in.Text); err != nil {
		return nil, nil, err
	}
	res, meta, err := jsonResult(struct {
		OK bool   `json:"ok"`
		ID string `json:"id"`
	}{true, in.ID})
	if err != nil {
		return res, meta, err
	}
	return h.voiced(res, func(v *voice.Voice) string { return v.NoteAdded(in.ID) }), meta, nil
}
```

`questRelinkCommit`:

```go
func (h *handlers) questRelinkCommit(ctx context.Context, req *sdk.CallToolRequest, in relinkIn) (*sdk.CallToolResult, any, error) {
	newSHA, err := h.store.ResolveCommit(in.NewSHA)
	if err != nil {
		return nil, nil, fmt.Errorf("new commit %q not found: %w", in.NewSHA, err)
	}
	if err := h.store.ReplaceCommit(in.ID, in.OldSHA, newSHA); err != nil {
		return nil, nil, err
	}
	return jsonResult(struct {
		OK     bool   `json:"ok"`
		ID     string `json:"id"`
		OldSHA string `json:"old_sha"`
		NewSHA string `json:"new_sha"`
	}{true, in.ID, in.OldSHA, newSHA})
}
```

`questUnlinkCommit`:

```go
func (h *handlers) questUnlinkCommit(ctx context.Context, req *sdk.CallToolRequest, in unlinkIn) (*sdk.CallToolResult, any, error) {
	if err := h.store.RemoveCommit(in.ID, in.SHA); err != nil {
		return nil, nil, err
	}
	return jsonResult(struct {
		OK  bool   `json:"ok"`
		ID  string `json:"id"`
		SHA string `json:"sha"`
	}{true, in.ID, in.SHA})
}
```

- [ ] **Step 4: Delete the now-orphaned helpers**

In `internal/mcp/tools.go`, delete the `result` method (the block starting `// result re-reads a quest by id ...`) and the `resultVoiced` method (the block starting `// resultVoiced re-reads a quest by id ...`). Keep `jsonResult` and `voiced` — they are still used. After deletion, `result` and `resultVoiced` must have no remaining callers.

- [ ] **Step 5: Build — verify no unused-symbol or reference errors**

Run: `go build ./...`
Expected: builds clean. (Go fails the build on an unused import or an undefined reference; a leftover call to `result`/`resultVoiced` would show here.)

- [ ] **Step 6: Run the retargeted tests — verify they pass**

Run: `go test ./internal/mcp/ -run 'TestSetStatusAndReclassify|TestUpdateAndNote|TestUnlinkAndRelinkCommitTools'`
Expected: PASS.

- [ ] **Step 7: Run the whole package + vet — verify green**

Run: `go test ./internal/mcp/ && go vet ./internal/mcp/`
Expected: PASS, no vet complaints.

- [ ] **Step 8: Commit**

```bash
git add internal/mcp/tools.go internal/mcp/server_test.go
git commit -m "feat: MCP mutation tools return minimal acks (SQ-0052)" \
  -m "set_status/reclassify/update/note/relink/unlink now return {ok,id,...changed} acks instead of the full quest (quest_note no longer re-echoes the whole body; quest_update reports the merged tag set; relink echoes the resolved sha). Remove the orphaned result/resultVoiced helpers." \
  -m "Completes: SQ-0052"
# append the repo's Co-Authored-By / Claude-Session trailer lines as on recent commits
```

---

## Notes for the executor

- After Task 2, `git grep -n 'resultVoiced\|\.result(' internal/mcp` should return nothing (both helpers gone, no callers).
- The full suite `go test ./...` should stay green — no non-`internal/mcp` package depends on these return shapes.
- `gofmt -l internal/mcp/` should list nothing after each task. (Note: `internal/filter/filter_test.go` is a pre-existing `gofmt` flag unrelated to this work — leave it.)
- Task 2's commit uses `Completes: SQ-0052` to close the quest; Task 1 uses link-only `Quest: SQ-0052`. If executing out of order or stopping after Task 1, the quest stays open, which is correct.
