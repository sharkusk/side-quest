# MCP compact return shapes — design (SQ-0052)

**Status:** approved, ready for implementation planning
**Quest:** SQ-0052
**Area:** `internal/mcp`

## Problem

The MCP tool surface feeds an LLM agent more than it needs. Every `quest.Quest`
carries two heavy fields — `Context` (the auto-captured git block) and `Body`
(every appended note, growing without bound) — plus `Commits` (full 40-char
SHAs). Several tools return whole `Quest` objects where the agent needs only
confirmation or a triage line:

- **`quest_list`** returns `[]*quest.Quest` — the full body of *every* match. An
  agent listing a backlog pulls all bodies into context at once. This is the
  primary clutter vector, and it is asymmetric with the compact one-line list
  the CLI already prints.
- **`quest_note`** re-echoes the entire accumulated body on *every* append.
- **`quest_set_status` / `quest_reclassify` / `quest_update` /
  `quest_relink_commit` / `quest_unlink_commit`** each echo a full `Quest` where
  an acknowledgement of the change would do.

The other seven tools (`quest_get_current`, `quest_set_current`,
`quest_link_commit`, `cli_status`, `cli_install`, `cli_uninstall`,
`cli_dismiss`) already return minimal ad-hoc acks and are fine.

## Goal

Standardise a compact-vs-full convention across the whole MCP surface: a **read
detail path** (`quest_show`), a **compact summary** for list/triage, and
**minimal acks** for mutations — so an agent is never force-fed unbounded bodies
on a call that doesn't need them.

## Scope

`internal/mcp` only.

- **No store change.** The summary is a presentation projection; it lives in the
  MCP layer.
- **No CLI change.** The CLI `list` already prints compact one-liners; this quest
  is scoped to the MCP tools.
- **No back-compat concern.** These outputs are consumed by an agent, not a
  versioned API, so shapes may change freely; no version field or dual path.

## Design

### The summary projection

A presentation type in `internal/mcp` and a pure mapper from a stored quest:

```go
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

func summarize(q *quest.Quest) questSummary
```

`summarize` copies the light fields, drops `Context` and `Body` entirely, and
collapses `Commits` to its length. `Completed` stays a `*time.Time` so it is
absent (not a zero timestamp) for unfinished quests; `Tags` is `omitempty`.
`commit_count` is always present (an explicit `0` reads clearly).

### Per-tool return shapes

| Tool | Return | Notes |
|---|---|---|
| `quest_list` | `[]questSummary` | Detail is fetched per quest via `quest_show`. |
| `quest_new` | `questSummary` of the created quest | Confirms the assigned id, applied type/priority defaults, `status: open`, and any tags — without echoing the auto-captured context. |
| `quest_show` | full `Quest` | Unchanged. The one full-detail read. |
| `quest_set_status` | `{ok, id, status}` | Ack. Voiced (unchanged). |
| `quest_reclassify` | `{ok, id, type?, priority?}` | Ack; echoes only the field(s) that were set (`omitempty`). |
| `quest_update` | `{ok, id, title?, tags}` | Ack; `tags` is the **merged resulting** set (see below). |
| `quest_note` | `{ok, id}` | Ack. Voiced (unchanged). |
| `quest_relink_commit` | `{ok, id, old_sha, new_sha}` | `new_sha` is the **resolved canonical** hash the store recorded. |
| `quest_unlink_commit` | `{ok, id, sha}` | Ack. |
| `quest_get_current` | `{current}` | Unchanged. |
| `quest_set_current` | `{ok}` / `{ok, current}` | Unchanged. |
| `quest_link_commit` | `{ok, sha}` | Unchanged (a commit's trailers may name several quests, so there is no single id). |
| `cli_status` | `{installed, path?, offered}` | Unchanged. |
| `cli_install` | `{path, dir, on_path}` | Unchanged. |
| `cli_uninstall` | `{removed, refused}` | Unchanged. |
| `cli_dismiss` | `{offered}` | Unchanged. |

Each ack is a small anonymous struct built inline, matching the style the
already-minimal tools use today.

### Two mutations that cannot be answered from inputs alone

- **`quest_update`** merges tags (an empty value deletes a key), so the resulting
  tag set differs from what the caller passed. `Store.Modify` returns only
  `error`, so the handler re-reads the quest after the write and reports the
  merged `Tags` in the ack. This is the only added re-read.
- **`quest_relink_commit`** already resolves `new_sha` to its canonical hash via
  `Store.ResolveCommit` before recording it; the ack echoes that resolved value
  (no extra read).

The remaining acked mutations (`set_status`, `reclassify`, `note`, `unlink`)
report values they already hold from their inputs — no re-read.

### Voicing

The `voiced()` wrapper (SQ-0028) appends a tone-flavoured human line as a second
content block, independent of the JSON in `content[0]`. It is preserved exactly
where it applies today — `quest_new`, `quest_set_status`, `quest_note` — now
wrapping their ack/summary results instead of full-quest results. The other
three mutations remain unvoiced, as they are today.

### Helper cleanup

`result(id)` and `resultVoiced(id, line)` re-read a quest and return the full
object. After this change every caller switches to a summary or an ack, leaving
both helpers unused; they are removed. `voiced()` and `jsonResult()` stay.

## Testing

TDD, in `internal/mcp/server_test.go` (and `tools`-level tests as needed):

1. **`quest_list` returns summaries** — a quest with a non-empty `Body`/`Context`
   and ≥1 commit; assert the JSON contains `commit_count` and the title, and does
   **not** contain the body text or the `context` field. Assert it is an array.
2. **`quest_new` returns a summary** — assert the assigned `id`, defaulted
   `type`/`priority`, `status: "open"`, and absence of a `context`/`body` field.
3. **Each acked mutation returns its documented shape** — assert `ok:true`, the
   `id`, and the changed field(s); assert the body text is absent.
4. **`quest_update` ack shows merged tags** — seed tags `{a:1,b:2}`, update with
   `{b:"",c:3}`, assert the ack's `tags` is `{a:1,c:3}`.
5. **`quest_relink_commit` ack echoes the resolved sha** — pass an abbreviated
   `new_sha`, assert the ack's `new_sha` is the full canonical hash.
6. **`quest_show` still returns the full quest** — a body/context regression guard.
7. Existing tests that asserted full-quest returns from the changed tools are
   updated to the new shapes (not deleted — retargeted).

## Out of scope

- Pagination / archival / list performance at large quest counts (**SQ-0053**).
- Any change to the CLI output or the store API.
