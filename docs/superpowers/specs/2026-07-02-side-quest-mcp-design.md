# Phase 4 MCP Server — Design

**Date:** 2026-07-02
**Status:** Approved (brainstorm)
**Scope:** A stdio MCP frontend (`side-quest serve`) exposing the quest store to
any MCP-capable agent, built on the existing `store`/`quest`/`config`/`gitcmd`
libraries. Adds a handful of store mutators the update tools need and a small
context-capture helper. Voice/tone (Phase 5), the importer (Phase 6), full
Claude-plugin packaging / `AGENTS.md` / the `/sq` command (Phase 7), a `sync`
command, and config-write tools are out of scope.

## Goal

Let an agent capture, read, and drive quests programmatically over MCP, so the
quest tracker works hands-free inside an agent session: capture a side quest
without derailing, record newly-learned findings onto a quest, reclassify and
close quests, and point the worktree at an active quest so commits auto-link.
The tool responses reuse the CLI's `quest.Quest` JSON shape, so this phase also
locks the structured surface agents parse.

## Motivation

The store already carries everything the CLI drives. MCP is the second frontend
the original design (§14) always intended (`side-quest serve` over the one core
lib). The Phase 3 CLI proved the wiring-layer pattern; this phase applies the
same pattern to a stdio JSON-RPC server. The one genuinely new capability agents
need beyond the CLI is an **update path** — appending learned notes and editing
metadata on an existing quest — which the CLI deliberately deferred and which
this phase adds at the store level.

## Design

### 1. Architecture and boundaries

A new **stdio MCP frontend** beside the CLI/hook frontends, over the same store.

- `cmd/side-quest/main.go` gains a `serve` case in `run()` → a thin
  `cmdServe` that opens the store for the cwd, builds the server, and runs it on
  stdio until EOF.
- **`internal/mcp`** (new package) owns tool registration and the handlers, so
  the server is testable without a real stdio pipe (via the SDK's in-memory
  transport). Each handler is a thin adapter: decode typed params → call one
  store method → return structured JSON. Same discipline as the CLI: no business
  logic in the frontend; validation stays at the store write boundary.
- Invalid input (bad enum, missing id) comes back as an **MCP tool error**
  (an error *result*, so the agent sees it and the server keeps running), never
  a JSON-RPC protocol error.

**Dependency:** this phase adds `github.com/modelcontextprotocol/go-sdk` — the
project's first dependency beyond `gopkg.in/yaml.v3`. The implementation plan
pins the exact version, and bumps the `go 1.22` directive in `go.mod` if the SDK
requires a newer floor. The SDK generates each tool's input JSON-Schema from a
Go param struct, so tool schemas stay in sync with the code.

**stdout is the protocol channel.** The server writes only JSON-RPC to stdout;
any diagnostics go to stderr. Nothing in a handler may print to stdout.

### 2. Tools

Ten tools, each returning neutral structured JSON (no voice flavor). Mutating
tools return the updated `quest.Quest`; `quest_set_current`/`quest_link_commit`
return a small ack; `quest_get_current` returns the id (or empty).

| Tool | Params | Store call | Returns |
|---|---|---|---|
| `quest_new` | `title`, `context?`, `type?`, `priority?`, `tags?`, `set_current?` | `Create` (+ capture helper §4) | created quest |
| `quest_list` | `status?`, `type?`, `priority?` | `List` + validated filter | quest array |
| `quest_show` | `id` | `Get` | one quest |
| `quest_set_status` | `id`, `status` | `SetStatus` | updated quest |
| `quest_reclassify` | `id`, `type?`, `priority?` | `SetType` / `SetPriority` | updated quest |
| `quest_update` | `id`, `title?`, `tags?` | `SetTitle` / `MergeTags` (§3) | updated quest |
| `quest_note` | `id`, `text` | `AppendNote` (§3) | updated quest |
| `quest_set_current` | `id` \| `clear:true` | `SetCurrent` / `ClearCurrent` | ack |
| `quest_get_current` | — | `Current` | id or empty |
| `quest_link_commit` | `sha` | `Link` | ack |

Rules mirroring the CLI:

- `quest_new`: `type`/`priority` empty → store defaults; non-empty-invalid →
  tool error (nothing written). `tags` is a string→string map. **`set_current`
  defaults false** — capture must not hijack the worktree's active quest (see
  §5); when true, the tool also calls `SetCurrent(newID)` after a successful
  create.
- `quest_list`: filter values validated against the enums before listing
  (unknown value → tool error); filters combine with AND (the one sanctioned
  bit of frontend validation, same as the CLI).
- `quest_reclassify`: at least one of `type`/`priority` required (else tool
  error); calls only the setter whose param was given.
- `quest_update`: at least one of `title`/`tags` required (else tool error).
  A non-empty `title` replaces the title; `tags` merges (§3). An empty `title`
  is ignored, not applied (a quest keeps a title).
- `quest_note`: `text` required and non-empty.

Config tools (read or write) are intentionally **excluded** — `require_quest`,
`auto_trailer`, and `id_strategy` change how the repo behaves for the human and
are project-owner decisions, not agent decisions (YAGNI; add a read-only
`config_get` later only if a real need appears). Title/body edits beyond
`quest_update`/`quest_note` (e.g. full-body replace) are also excluded.

### 3. New store mutators

The update tools need three mutators the store does not have yet. Each is a thin
`Update`-based method with a test in the `store` package, following the existing
`SetStatus`/`SetType` pattern (mutation logic lives in the store, not the
frontend):

- **`AppendNote(id, text string) error`** — appends `text` to the quest `Body`
  as a distinct, UTC-timestamped entry (a separator + timestamp line + the
  text), non-destructive so incremental notes never clobber earlier ones. The
  exact separator format is fixed in the plan; intent: each note is a
  greppable, dated block.
- **`SetTitle(id, title string) error`** — replaces `Title`. Rejects an empty
  title (a quest must keep a title).
- **`MergeTags(id string, tags map[string]string) error`** — merges `tags` into
  the quest's `Tags` (creating the map if nil); a key whose value is the empty
  string deletes that key.

These are store-level and reusable; a later CLI `edit`/`note` command can adopt
them, but this phase wires them only to MCP (surgical).

### 4. Mechanical context helper

New small unit **`internal/capture`** (named to avoid colliding with Go's
`context` package):

- **`Mechanical(dir, currentQuest string) string`** — reads the worktree's git
  state (branch and short HEAD via `gitcmd`, cwd) plus the passed
  `currentQuest`, and formats a few greppable labeled lines. Best-effort: any
  piece that can't be read is omitted; it never errors and never blocks a
  create. Depends only on `gitcmd` (the current-quest id is passed in, keeping
  this decoupled from `store`).

`quest_new` composes the stored context as the mechanical block, then a blank
line, then the agent's narrative `context` (if any), and passes the result to
`store.Create`. This delivers the design's "why did I want this?" recall on the
agent capture path. The CLI's `new` may adopt the same helper later; this phase
does not wire it into the CLI.

### 5. current-quest and data flow

The current-quest pointer is per-worktree state at
`<worktree-git-dir>/side-quest-current` (not on the ref, never pushed). Its sole
consumer is the `prepare-commit-msg` hook, which — when a current quest is set
and `auto_trailer` is on and no trailer is already present — injects
`Quest: <current>` so the commit auto-links to that quest via
`post-commit`→`Link`.

The MCP server shares the worktree's git dir with the hooks, so
`quest_set_current(id)` over MCP makes the *next* commit (human or agent)
auto-stamp that quest — MCP and the hooks cooperate through this shared file.
`quest_get_current` lets an agent orient. Because capture must not context-switch
the worktree, `quest_new` leaves the pointer untouched unless `set_current:true`.

Read tools snapshot the ref; write tools go through the store's CAS mutation
loop exactly as the CLI and hooks do, so CLI, hooks, and MCP are safe running
concurrently against the same repo.

### 6. Error handling

- Bad enum / not-found / missing-required-param / nothing-to-update → tool-error
  result carrying the store's (or a clear frontend) message; the server keeps
  serving.
- Transport EOF / client disconnect → clean shutdown, exit 0.
- The server never writes non-protocol bytes to stdout.

### 7. Documentation (living docs) and dogfooding

In the same change as the behavior:

- `docs/architecture.md` — an "MCP frontend" subsection: the ten tools, the
  `internal/mcp` boundary, the `capture` helper, tool-errors, and the
  stdout-is-protocol rule.
- `README.md` — an MCP setup snippet (the example `.mcp.json` uses
  `"command": "side-quest"`, i.e. a PATH install) plus a **dogfooding note**:
  point a repo-local `.mcp.json` at `go run ./cmd/side-quest serve` to always
  run latest source; restart the server to pick up code/tool changes; quest data
  on the ref is binary-version-independent (`Unmarshal` is a pure, default-
  tolerant parser), so switching binaries mid-dogfood is safe.
- A repo-root **`.mcp.json`** so the project can dogfood immediately. Because
  this file is for side-quest's *own* development (no installed binary assumed),
  it uses the `go run ./cmd/side-quest serve` form — the same variant the
  README's dogfooding note documents — not the `side-quest` PATH form shown to
  end users. (The two forms are intentionally different: PATH install for users,
  `go run` for this repo's dogfood loop.)

The full Claude-plugin packaging (`.claude-plugin/`, `marketplace.json`),
`AGENTS.md`, and the `/sq` command/skill remain in their later phases.

## Out of Scope (deferred)

- Voice/tone rendering (Phase 5); tone is never applied to tool payloads anyway.
- The babelmap importer (Phase 6).
- Full plugin packaging, `AGENTS.md`, and the `/sq` command/skill (Phase 7 / the
  agent-skill item).
- A `sync` push/pull command.
- Config tools over MCP (read or write).
- Full-body replace; a CLI `edit`/`note` command (the new store mutators are
  wired only to MCP this phase).

## Testing

- **`store` package:** `AppendNote` appends a dated entry without clobbering an
  existing body; `SetTitle` replaces and rejects empty; `MergeTags` adds,
  overwrites, and deletes (empty-value) keys — each against a temp-repo store,
  following the existing `newStore` test pattern.
- **`internal/capture`:** `Mechanical` returns the branch/head/cwd/current
  fields for a temp git repo and degrades gracefully (omits unreadable pieces)
  outside one or with no current quest.
- **`internal/mcp` integration:** using the SDK's in-memory transport, wire a
  client to the server and assert `tools/list` returns the ten tools, then
  round-trip representative `tools/call`s: `quest_new` then `quest_show`;
  `quest_note` then `quest_show` shows the appended entry; `quest_update` merges
  and deletes tags; `quest_set_current`/`quest_get_current`; an invalid enum
  (e.g. `quest_set_status` with a bad status) returns a tool error, not a
  transport failure.
- The exact SDK API (server construction, tool registration, in-memory
  transport helpers) is pinned in the implementation plan against the installed
  SDK version.
