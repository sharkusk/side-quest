# Phase 3 CLI — Design

**Date:** 2026-07-02
**Status:** Approved (brainstorm)
**Scope:** The human-facing `side-quest <cmd>` command surface, built on the
existing `store`/`quest`/`config` libraries. Adds two small library setters the
CLI needs. MCP tools (Phase 4), DCC tone rendering (Phase 5), the importer
(Phase 6), and `$EDITOR` body editing are out of scope.

## Goal

Expose the quest store at the command line: create, list, show, change status,
reclassify (type/priority), and read/write config. This is the phase that
finally surfaces the Phase 3 schema work (`--type` / `--priority`) and the
`require_quest` knob to a user, and it defines the JSON output that the Phase 4
MCP layer will reuse.

## Motivation

Everything a user needs already exists in the `store` library — `Create`,
`List`, `Get`, `SetStatus`, `SetType`, `SetPriority`, `Config`,
`SetRequireQuest`, `SetStrategy`, and the current-quest/`Link` methods used by
the hooks. What is missing is a human entrypoint. The existing binary
(`cmd/side-quest`) only carries the hook-oriented subcommands (`link`,
`current`, `commit-msg`, `prepare-commit-msg`, `install-hooks`). Phase 3 adds
the human commands beside them.

## Design

### 1. Architecture and boundaries

The CLI is a **pure wiring layer**. Each command is a thin adapter that parses
flags, calls exactly one existing `store` method, and renders the result. No
business logic or validation lives in `cmd/` — validation stays at the store
write boundary, so an invalid `--type` surfaces as a store error and becomes a
non-zero exit. This keeps a single source of truth for what a valid quest is
(the store), consistent with how `Create`/`SetType`/`SetPriority` already work.

The new human commands join the existing `run()` switch in
`cmd/side-quest/main.go`. The hook commands are untouched. Flag parsing uses the
standard-library `flag` package with one `flag.FlagSet` per subcommand — zero
new dependencies, matching the project's minimal-deps stance (only `yaml.v3`
today) and the millisecond cold-start the hooks rely on.

**File layout** (`main.go` is already 161 lines; hooks already live in a
sibling `hooks.go`):

- `cmd/side-quest/cli.go` — the human command handlers (`cmdInit`, `cmdNew`,
  `cmdList`, `cmdShow`, `cmdStatus`, `cmdReclassify`, `cmdConfig`).
- `cmd/side-quest/render.go` — output rendering (human table/detail + JSON).
- `cmd/side-quest/main.go` — extend the `run()` switch and the `usage` string
  only.

### 2. Commands

| Command | Store call | Notes |
|---|---|---|
| `init` | `Init()` | Prints a confirmation. Surfaces the existing "already initialized" error as a normal error (exit 1). |
| `new <title>` | `Create(title, ctx, typ, prio, tags)` | Flags: `--type`, `--priority`, `--context`, `--tag k=v` (repeatable), `--current`, `--json`. Prints the new id to stdout (or the JSON quest with `--json`). |
| `list` | `List()` + in-memory filter | Flags: `--status`, `--type`, `--priority` (each single-value), `--json`. Human output = aligned columns `ID  STATUS  TYPE  PRIORITY  TITLE`. |
| `show <id>` | `Get(id)` | Human = frontmatter fields, a blank line, then the body. `--json` = the quest struct. |
| `status <id> <status>` | `SetStatus(id, status)` | The store validates the status enum; invalid → error → exit 1. |
| `reclassify <id>` | `SetType` and/or `SetPriority` | Flags: `--type`, `--priority`; at least one required (else usage error, exit 2). Calls only the setters whose flag was given. |
| `config get` | `Config()` | Prints the full effective config (human or `--json`). |
| `config set <key> <val>` | dispatch (below) | See key table. |

**`new` details.** The title is the single positional argument (require exactly
one; a multi-word title must be quoted). `--type`/`--priority` default to the
empty string, so omission flows through `Create`'s existing
required-with-defaults coercion (empty → `feature`/`low`; non-empty-invalid →
store error). `--tag k=v` is repeatable and builds the `tags` map; a value with
no `=` is a usage error. `--current` additionally calls `SetCurrent(newID)`
after a successful create — the create is non-derailing by default and only
moves the worktree pointer when explicitly asked (mirrors the `/sq` capture
philosophy). `--context` supplies the optional context string.

**`list` filter details.** Each filter takes a single value and is validated
against its enum before listing (`--type`/`--priority`/`--status` reject an
unknown value with an error), so a typo like `--type bugg` fails loudly rather
than silently matching nothing. Omitted filters match everything. Multiple
filters combine with AND.

**`config set` key dispatch:**

| Key | Value | Store call |
|---|---|---|
| `require_quest` | `true` \| `false` | `SetRequireQuest(bool)` |
| `auto_trailer` | `true` \| `false` | `SetAutoTrailer(bool)` *(new — see §3)* |
| `id_strategy` | `sequential` \| `random` | `SetStrategy(Strategy)`, gated by `Strategy.Valid()` *(new — see §3)* |

Any other key is an error (exit 1) listing the accepted keys. A malformed bool
or an invalid strategy is an error before any write. `tone` is deliberately
excluded until Phase 5.

### 3. Small library additions

These are the only changes outside `cmd/`. Each ships with tests in the same
phase and follows an existing pattern exactly.

- **`store.SetAutoTrailer(v bool) error`** — mirrors `SetRequireQuest`: a
  `mutate` that reads the snapshot config, sets `AutoTrailer = v`, and writes
  `_config.yaml` back. Needed because `config set auto_trailer` has no setter
  today.

- **`config.Strategy.Valid() bool`** — mirrors `quest.Status.Valid()`: returns
  true for `Sequential`/`Random`, false otherwise. `SetStrategy` does not
  validate its argument, so the CLI calls `Valid()` first and rejects an
  unknown strategy before writing. Placing `Valid()` on the type (not in the
  CLI) keeps the validity rule with the enum, consistent with `Status`, `Type`,
  and `Priority`.

### 4. Output contract

- **Human output** is neutral in tone (the DCC voice layer is Phase 5). `list`
  with no matches prints a friendly "no quests" line, not an empty result.
  Timestamps render RFC3339.
- **`--json`** marshals the `quest.Quest` value(s) directly: one object for
  `show` and `new`, an array for `list`, the `config.Config` for `config get`.
  This is the stable machine surface the Phase 4 MCP layer reuses, so the JSON
  shape is the struct shape — no bespoke CLI DTO.
- **Errors** go to stderr as `side-quest: <err>` with exit 1 (the existing
  `main()` contract). A usage error (missing/extra positional arg, `--tag`
  without `=`, `reclassify` with no flag) exits 2, matching the existing
  arg-count checks.

### 5. Documentation (living docs)

In the **same change** as the behavior:

- `docs/architecture.md` — add a "CLI" subsection listing the human commands
  and the JSON-output contract; note the two new library setters
  (`SetAutoTrailer`, `Strategy.Valid()`).
- `README.md` — add a usage section showing the common commands
  (`init`, `new`, `list`, `show`, `status`, `reclassify`, `config`).

The dated spec/plan files under `docs/superpowers/` are frozen history and are
not edited to match later code.

## Out of Scope (deferred)

- DCC / tone rendering of human output (Phase 5) — output is neutral now.
- `tone` as a `config set` key (Phase 5).
- MCP tool surface (Phase 4) — but the `--json` shape is designed to be reused.
- `$EDITOR`-based editing of title/context/body (later; no `edit` command).
- The importer's default-classification policy for babelmap items (Phase 6).

## Testing

- **`cmd` package** (table-driven against a temp git repo, following the
  existing `main_test.go` pattern):
  - `new` persists a quest and prints its id; `new --json` emits a parseable
    quest; `new --current` sets the worktree pointer; `new --tag k=v` records
    the tag; `new` with a bad `--tag` (no `=`) exits 2.
  - `list` filters narrow correctly for `--status`/`--type`/`--priority`;
    `--json` yields a parseable array; empty result prints the "no quests" line;
    an invalid filter value (e.g. `--type bugg`) errors (exit 1).
  - `show <id>` renders fields and body; `show` of a missing id errors (exit 1);
    `--json` round-trips.
  - `status` and `reclassify` reject an invalid enum (exit 1); `reclassify`
    with no flag is a usage error (exit 2).
  - `config get` prints the effective config; `config set` for each of the
    three keys persists; `config set badkey x` and an invalid strategy/bool
    error.
  - **Carry-forward e2e:** `new --type buggg` exits non-zero and leaves the ref
    empty (no partial write).
- **`store` package:** `SetAutoTrailer` toggles and persists the flag.
- **`config` package:** `Strategy.Valid()` truth table (each known value true;
  an unknown value false).
