# side-quest `brief` — Design (SQ-0132)

**Goal:** Add `side-quest brief` — a read-only "resume" view that assembles the
project's current state from existing quest data so a fresh session, a new machine,
or a new agent can pick up where the last one left off *without re-reading everything*.
Human-formatted at the terminal by default; a structured `--markdown` form feeds the
plugin's `SessionStart` context injection and a `quest_brief` MCP tool.

**Why now:** A committed `CLAUDE.md` (SQ-0131) gave a fresh agent the project's
*durable* standing context — the invariants, the build loop, the conventions. But the
*volatile* layer — what's in flight right now, what was just closed and why — still
can't be handed over without reading many quests one at a time. `brief` is the mirror
image of side-quest's core promise: you **capture without derailing** on the way in;
you should **resume without re-reading** on the way back.

## Decisions (locked in brainstorming)

- **A computed view, never a stored artifact.** The brief is assembled on demand from
  the store at read time — exactly like `list`/`show`. Nothing is persisted. This is
  deliberate: a materialized brief would have to be regenerated on every quest change
  or it would lie. A read-time projection **cannot** be stale. The only thing ever
  *written* is a quest itself, through the normal capture flow.
- **No new storage or data model.** Everything the brief needs already exists: quest
  frontmatter (status/type/priority/created/completed/**context**/commits), body
  **notes**, the worktree **current-quest** pointer, and — because every mutation is a
  commit on the ref — per-quest and whole-store **timestamps**. The brief only *reads
  and arranges*. No "codex", no decision-log store: the rationale a fresh agent wants
  already lives in each quest's context/notes.
- **One data-assembly path, two renderers.** The same assembled model renders two ways,
  the split side-quest already uses for `list`/`show`:
  - **default → human render** (pretty, aligned, relative times),
  - **`--markdown` → machine/injection render** (headed sections, absolute ISO times).
- **House style, no borders.** Sectioned headers + `text/tabwriter`-aligned columns
  (matching `list`), `label`/hanging-indent values (matching `show`). No box-drawing.
- **Terminal-aware wrapping.** Context/notes wrap to terminal width with a hanging
  indent via the existing `wrapText`; `--no-wrap` and any non-TTY (piped/redirected)
  output print stable, unwrapped rows so scripts and the injection path see fixed text.
- **Neutral — never voiced.** The brief is a *data display*, so per the existing
  neutral-path rule it stays tone-free regardless of `plain`/`dcc`, like `list`/`show`.
  (A single flavored header line could be added later behind a flag; default neutral,
  and always neutral in the `--markdown`/injection form.)
- **"Recently closed" is a fixed count, not a time window.** Default **5**, overridable
  with `--closed N`. A count gives the brief a predictable length regardless of how busy
  the week was; a 7-day window could show zero entries or fifty.

## What the brief contains

A **freshness header** for time-orientation, then three sections:

1. **Freshness header** — branch · last-activity (the ref's most recent mutation commit
   date) · a one-line tally (`1 current · 3 outstanding · 5 recently closed`).
2. **CURRENT** — the worktree's current quest, expanded: title, type/priority/status,
   its context, and its linked commits (`<short-sha>  <subject>`, as `show` renders them).
   Omitted cleanly when no current quest is set (a fresh clone/worktree has none — the
   pointer is worktree-local and never travels).
3. **OUTSTANDING** — every open/partial/confirm quest *except* the current one, as
   aligned one-liners: `id · title · type · priority · status`. This is the standing
   "what's on the board" that the outstanding `list` view already defines.
4. **RECENTLY CLOSED (last N)** — the most recently completed/deferred/discarded quests,
   newest first, with a relative close time — so a resuming agent sees what just landed
   (and doesn't re-open settled or abandoned work).

## Surfaces & phasing

**v1 (this quest):**
- **`side-quest brief`** — the CLI command (human render default; `--markdown` machine
  render; `--no-wrap`; `--closed N`). A thin adapter over one new assembly function plus
  two renderers, living beside `list`/`show` in `cli.go`/`render.go`.
- **`quest_brief` MCP tool** — a read tool returning the same assembled content, so an
  MCP-only agent can request the brief on demand. Read-only, neutral (reads never voice).

**v2 (follow-up quest, not built here):**
- **`SessionStart` auto-injection.** The plugin hook already runs at session start to
  provision the binary; a follow-up shells `side-quest brief --markdown` and emits it as
  session context, so a fresh agent on a new machine simply *knows* the state. Gated by
  an opt-in on-ref config key (e.g. `session_brief`, default off) and by "only inject
  when there is a current quest or outstanding work," to bound token cost. Deferred so v1
  ships value immediately and the magic is added deliberately.

## Data sources (all existing)

| Needs | Source |
|---|---|
| every quest + frontmatter + notes + commits | `store.List()` (already one batched `cat-file --batch`) |
| the current quest | `store.Current()` (worktree-local pointer) |
| linked-commit subjects | the same resolution `show` uses |
| last-activity / close times | the ref's commit timestamps (`store.History` / the ref tip) |

Because `List()` is already batched (≈constant cost into the hundreds of quests, per
`docs/scale.md`), regenerating the brief on every call — and every session start — is cheap.

## Flags

| Flag | Effect |
|---|---|
| *(none)* | human render: aligned, wrapped to terminal width, relative times |
| `--markdown` | machine/injection render: headed sections, absolute ISO times |
| `--closed N` | number of recently-closed quests to show (default 5) |
| `--no-wrap` | one stable line per quest (implied for non-TTY output) |
| `--json` | *(optional, for CLI consistency)* the assembled struct as neutral JSON |

## Constraints / invariants (must hold)

- **Read-only.** `brief` performs no mutation — no ref write, no CAS, and (trivially) it
  never touches the working tree or real index. It only reads snapshots.
- **Neutral-path rule.** No `brief` output routes through `internal/voice`; a test asserts
  the render is byte-identical across `plain`/`dcc`/`dcc-superfan` (the `list`/`show`
  pattern — cf. `TestNewJSONNeutralAcrossTones`).
- **Non-TTY stability.** Piped/redirected output is unwrapped, one line per quest, so the
  injection path and scripts get deterministic text.
- **Reuse, don't duplicate.** Assemble from `store.List`/`Current`/`History` and reuse
  `wrapText` + the `tabwriter` setup; no new store method unless a genuine gap appears.
- **Docs are part of the change.** Add the `brief` command to the CLI surface in
  `docs/architecture.md` and to the README usage in the same change that ships it.

## Out of scope

- **Auto-injection** (the `SessionStart` wiring + `session_brief` config key) — its own
  follow-up quest (v2 above).
- **A decision-log / codex store.** v1 surfaces rationale straight from quest
  context/notes; a dedicated rationale mechanism is only justified if that proves too
  thin, and would be a separate design.
- **Any prose summarization.** Durable rules stay verbatim in `CLAUDE.md`; live state is
  rendered structurally here. No lossy summary in between.

## Success criteria

- `side-quest brief` prints a scannable, aligned, borderless digest — freshness header,
  expanded current quest, aligned outstanding list, last-5 closed — wrapped to the
  terminal and stable when piped.
- `side-quest brief --markdown` prints the same content as headed sections with absolute
  times, suitable for pasting/injecting into a fresh agent's context.
- `quest_brief` returns the equivalent content over MCP; reads never carry voice.
- Output is tone-neutral across all tones (guard test); `go test ./...` / `go vet` green;
  `docs/architecture.md` + README updated in the same change.
