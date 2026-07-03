# Phase 5 Voice / Tone System — Design

**Date:** 2026-07-02
**Status:** Approved (brainstorm)
**Scope:** A `voice` layer that routes the CLI's human-facing confirmation and
warning strings through a tone-selected, randomized line pool, so the tool can
speak in a sardonic *Dungeon Crawler Carl*–style "System" register without ever
obscuring information. Realizes the main design's §12. The MCP frontend, all
`--json`/machine output, data displays, and error messages stay neutral.

Out of scope: achievements/milestones (need persistent counters — a later feature),
the `sync` command's flavor (that command doesn't exist yet), and actually wiring
`dcc-superfan` verbatim lines into messages (this phase builds only the recognition +
fallback plumbing). Voice/tone is never applied to MCP tool payloads (Phase 4 keeps
those neutral by design).

## Goal

Make the CLI fun without ever costing the user clarity. A quest creation, a status
change, or a missing-trailer nudge should read like a heckling game-show System —
but the id is still in the line, `--json` is still pure data, and real errors are
still plain. This phase delivers the whole tone *infrastructure* wired into the
confirmation/warning sites that already emit today.

## Background: what already exists

Verified against the code while designing:

- **`config.Tone`** already exists (`internal/config/config.go`): a `Tone` string
  type with constants `TonePlain` = `plain`, `ToneDCC` = `dcc`, `ToneDCCSuperfan`
  = `dcc-superfan`; `Config.Tone` defaults to `ToneDCC`. But there is **no
  `Tone.Valid()`** method yet, and **`config set` does not accept `tone`** (only
  `require_quest`, `auto_trailer`, `id_strategy`). Nothing consumes `Tone` today.
- **Human-facing output is scattered and small**: `cmdInit` prints
  `"side-quest: initialized"`; `cmdNew` prints the bare quest id; `cmdStatus` is
  silent; `renderList` prints `"no quests"` when empty; the `commit-msg` path in
  `main.go` prints missing-trailer warnings to stderr; `hooks.go` prints
  `"side-quest: hooks installed in <dir>"`. Detail views (`renderShow`, the
  populated `list` table, `renderConfig`) are structured data displays.
- **`emitJSON`** is the single machine-output path; it marshals raw library structs.

## Design

### 1. The `internal/voice` package

A new pure package depending only on the standard library, `config.Tone`, and
`quest.Status` (for the `StatusSet` arg type) — no dependency on `store` or the
CLI.

- **Line pools** as Go literals: `plain` and `dcc`, each a map from an internal
  **message-key enum** to a `[]string` of candidate lines. A line may carry a
  `fmt`-style verb for its interpolated data (e.g. `"%s enters the dungeon."`).
  The `plain` pool has exactly one neutral line per key; `dcc` has several per key
  so output varies, with `discarded` and `deferred` getting the most character.
- **`Voice`** — a value carrying the resolved `Tone` and an **injectable random
  source** (an unexported single-method interface, e.g. `intn(n int) int`).
  Production constructs it with a time-seeded source; tests inject a deterministic
  one. All randomness flows through this seam so tests are deterministic.
- **Typed per-message methods** — the entire human surface for this phase:
  - `QuestCreated(id string) string`
  - `StatusSet(id string, status quest.Status) string`
  - `MissingTrailer() string`
  - `EmptyList() string`
  - `Initialized() string`
  - `HooksInstalled(dir string) string`

  Each looks up its `(key, tone)` pool, picks a line via the random source, and
  interpolates its typed args. The compiler guarantees each call site passes the
  right data; call sites read as plain method calls.

The package exposes nothing stringly-typed to callers — no exported `Say(key,
...any)`. The message-key enum is an internal implementation detail of the pools.

### 2. Tone resolution

Precedence, highest first: **`SIDE_QUEST_TONE` env → on-ref `config.Tone` →
default `dcc`.**

- An invalid `SIDE_QUEST_TONE` value is **ignored** (best-effort; never blocks a
  command) and resolution falls through to the config value.
- `Voice` is built once per CLI invocation from the resolved tone.
- Commands that run before a config exists (notably `init`) resolve tone from the
  env-or-default only (`dcc`), since there is no on-ref config to read yet.

### 3. Message points wired (this phase)

| Method | Current call site | Change |
|---|---|---|
| `QuestCreated(id)` | `cmdNew` — prints bare id | human (non-`--json`) path prints a flavored line **containing** the id; `--json` output unchanged |
| `StatusSet(id, status)` | `cmdStatus` — silent | prints a flavored confirmation; `discarded`/`deferred` get the most character |
| `MissingTrailer()` | `main.go` commit-msg **warn** (assisted mode) | the warn line is flavored |
| `EmptyList()` | `renderList` — `"no quests"` | flavored |
| `Initialized()` | `cmdInit` | flavored |
| `HooksInstalled(dir)` | `hooks.go` | flavored |

Two of these change existing behavior deliberately (both are the point of the
phase): `new`'s human output moves from a bare `SQ-0007` to a flavored line (the id
is still present; `--json` is unaffected), and `status` gains a confirmation line
where it is silent today.

### 4. Hard rules — the neutral paths (never routed through voice)

1. **All `--json`/machine output** (`emitJSON`) is untouched and byte-identical
   regardless of `SIDE_QUEST_TONE`. The System heckles humans, never scripts.
2. **Data displays stay neutral** — `renderShow`, the *populated* `list` table,
   and `config get`. Only the *empty*-list line is a flavor moment.
3. **Errors stay clear** — `main.go`'s error/usage printing and the `require_quest`
   **block** (an intentional rejection) are neutral. Flavor is confined to
   confirmations and the assisted-mode warn.

### 5. `dcc-superfan` — recognition + fallback plumbing only

`config set tone dcc-superfan` is accepted and persisted. When the resolved tone is
`dcc-superfan`, `Voice`:

- checks for the superfan file (default path `~/.config/side-quest/superfan-lines.txt`);
- if the file is **absent**, behaves exactly as `dcc` and emits a
  **one-time-per-process** hint to stderr naming the expected path;
- if the file is **present**, also behaves as `dcc` (the verbatim lines are
  reserved for a later phase — this phase does not feed them into messages).

Ships alongside: a clearly-marked, empty `superfan-lines.example.txt` and a README
**"Credits & permissions"** note explaining that verbatim *Dungeon Crawler Carl*
phrases are never shipped, load only from the user's own file, and require Matt
Dinniman's permission before any public/committed use. No verbatim content enters
the repo.

### 6. Config changes

- Add **`func (t Tone) Valid() bool`** to `internal/config` (mirrors the existing
  `Strategy.Valid`): true for `plain`/`dcc`/`dcc-superfan`.
- Add a **`config set tone <value>`** case to `cmdConfig`: validates via
  `Tone.Valid()` (invalid → error, nothing written), then persists. Closes the
  Phase-3 gap where `tone` was not settable from the CLI.

### 7. Wiring the CLI

`run()` resolves the tone (env + config, best-effort) and constructs one `Voice`,
threaded to the handlers that emit confirmations/warnings (`cmdNew`, `cmdStatus`,
`cmdInit`, the commit-msg path, `renderList`, `hooks.go`). Handlers call the typed
method and print its result on the human path; the `--json` branches never touch
`Voice`.

## Testing

- **`internal/voice`:** with an injected deterministic source, assert an exact
  chosen line per method; assert `plain` returns its single neutral line; assert
  every `dcc` key has at least one non-empty line and that the interpolated
  `id`/`status` appears in the result; assert construction with each tone.
- **Tone resolution:** env > config > default; an invalid `SIDE_QUEST_TONE` is
  ignored and the config value wins; `plain` env override forces neutral.
- **`dcc-superfan`:** absent file → falls back to `dcc` and the hint fires exactly
  once per process; present file → `dcc` behavior, no hint.
- **config:** `Tone.Valid()` truth table; `config set tone` accepts the three
  values and rejects an invalid one (writing nothing).
- **CLI machine-neutrality:** a command's `--json` output is byte-identical under
  `SIDE_QUEST_TONE=plain`, `=dcc`, and unset — the load-bearing guarantee.

## Living docs (same change as behavior)

- `docs/architecture.md` — a "voice layer" subsection: the `internal/voice`
  boundary, tone resolution/precedence, the neutral-path rules, and the
  superfan-plumbing status.
- `README.md` — a tone paragraph (the three tones, `SIDE_QUEST_TONE`, the
  `--json`-always-neutral guarantee) and the "Credits & permissions" note.

## Out of scope (deferred)

- Achievements/milestones (first quest, N done, N discarded) — need persistent
  counters; a later phase.
- Flavor for `sync` (command not yet built) and any MCP payload flavor (never).
- Wiring `dcc-superfan` verbatim lines into messages (only recognition + fallback
  this phase).
- Per-key superfan override formats (the file stays a flat, unwired `.txt` for now).
