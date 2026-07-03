# Agent Skill (`SKILL.md`) — Design

**Date:** 2026-07-02
**Status:** Approved (brainstorm)
**Scope:** A single workflow skill file, `skills/side-quest/SKILL.md`, that teaches
an MCP-capable agent *when* and *how* to use side-quest's ten MCP tools across the
quest lifecycle: capture a stray idea without derailing, attribute commits to a
quest, note findings, close a quest, and triage by type/priority. Built against the
tool surface delivered in Phase 4.

Out of scope: the rest of the Claude-plugin packaging (`.claude-plugin/plugin.json`,
`marketplace.json`, `commands/` incl. `/sq`, the agent-agnostic `AGENTS.md`) — all
Phase 7. No changes to the MCP tools, the store, the trailer parser, or the hooks:
this phase writes prose only.

## Goal

Close the last gap between "the tools exist" and "an agent reliably uses them." The
Phase 4 MCP server exposes the quest store, but an agent left to its own devices
will forget to capture the follow-up it just thought of, or will attribute a commit
to the wrong quest, or will never close anything. A skill encodes the *reflex* and
the *discipline* so the workflow happens without the human prompting it each time.

## Background: what the skill is teaching against

These facts are load-bearing and were verified against the code while designing.
The skill's every instruction must stay true to them.

- **Ten MCP tools** (Phase 4): `quest_new`, `quest_list`, `quest_show`,
  `quest_get_current`, `quest_set_status`, `quest_reclassify`, `quest_update`,
  `quest_note`, `quest_set_current`, `quest_link_commit`. Bad input returns a
  **tool error** the agent can read and correct — not a crash.
- **Enum value discovery.** The tools' `jsonschema` tags describe the allowed
  `type`/`priority`/`status` values in free text (e.g. "bug or feature; defaults to
  feature"), surfaced to the agent via `tools/list`. These are *descriptions*, not
  JSON-Schema `enum` constraints — the store is the actual validator (invalid value
  → tool error, nothing written). The skill therefore enumerates the values itself
  (§8) rather than relying on the agent introspecting the schema.
- **Trailers** (parsed by `internal/trailer`, applied by `store.Link` via the
  `post-commit` hook):
  - `Quest: SQ-xxxx` — links the commit's hash to the quest's commit log; **does
    not** change status.
  - `Completes: SQ-xxxx` — links **and** sets the quest's status to `done` (stamps
    a completion time).
  - `Quest: none` — explicit escape hatch: a genuine chore, deliberately unlinked.
- **Current-quest pointer** — per-worktree state at
  `<git-dir>/side-quest-current`, never on the ref, never pushed. Its sole runtime
  consumer is the `prepare-commit-msg` hook, which auto-injects `Quest: <current>`
  into a commit **only when no trailer is already present**. Writing an explicit
  `Quest:`/`Completes:`/`Quest: none` therefore overrides the auto-inject — no
  double-stamping.
- **`quest_new` auto-captures mechanical context** (branch, short HEAD, cwd,
  current quest) itself. The agent supplies only the narrative one-sentence `context`.

## Design

### 1. Identity and activation

- **File:** `skills/side-quest/SKILL.md` (its permanent home; the surrounding
  plugin that will load it is Phase 7).
- **`name`:** `using-side-quest`.
- **`description` (the activation trigger):** capture-reflex-primary. It fires on
  the high-precision moment an agent actually forgets — *a new, unrelated idea,
  follow-up, bug, or TODO surfaces while you are working on something else* — and
  it also names the rest of the loop (tracking, closing, and triaging quests via
  the side-quest MCP tools) so that once active the agent has the whole workflow.
  The reflex is the trigger; the lifecycle is the payload.

### 2. Structure

A single `SKILL.md`. No `references/` or `scripts/` subfiles — the content is small
enough to hold in one focused file (YAGNI). Body sections, in order:

1. The capture reflex (the headline behavior + its hard rule).
2. Capturing well (the narrative one-sentence context).
3. Attributing commits (explicit per-commit trailers — the lead pattern).
4. The current-quest pointer (the optional convenience + its hygiene rules).
5. Closing a quest.
6. Triage (type/priority) and metadata edits.
7. A compact intent → tool cheat-sheet.
8. What this skill does *not* do (boundaries).

### 3. The capture reflex (headline)

When a new, unrelated idea surfaces mid-task, the agent calls
`quest_new { title, context, type?, priority? }` where **`context` is a
one-sentence "what were you doing, and why did this occur to you"** — then gives a
single terse acknowledgement and **returns to the original task**.

- **Hard rule:** capture is create-and-return-only. Never start the captured work
  now, never plan it, never derail the session. (Mirrors the design's §11 capture
  contract.)
- **`set_current` stays false** on a capture (its default). Casually capturing a
  stray idea must never change what the worktree's commits attribute to. This is
  the backstop against mis-attribution.

### 4. Capturing well — the narrative context

The one genuinely agent-only value-add. `quest_new` already records the mechanical
context (branch/HEAD/cwd/current); the agent's job is the **narrative** sentence —
the part that makes a quest legible days later ("Noticed while fixing the parser
flake that the tokenizer re-scans the whole buffer on every call"). The skill
instructs: always supply `context`, keep it to one sentence, make it the *why*, not
a restatement of the title.

### 5. Attributing commits — explicit per-commit trailers (the lead pattern)

The skill leads with the safest attribution model for an agent:

- **Write the trailer into each commit message the agent authors:** `Quest: SQ-xxxx`
  on an in-progress commit, `Completes: SQ-xxxx` on the commit that finishes the
  quest. Because the trailer is written per-commit by an agent that already knows
  what it is committing, attribution is always intentional and **nothing is sticky**
  — a later unrelated commit cannot inherit a quest by accident.
- **Multiple commits per quest** is the normal case: every intermediate commit
  carries `Quest: SQ-xxxx` (each hash lands in the quest's commit log); the final
  commit carries `Completes: SQ-xxxx`.
- A genuine chore unrelated to any quest → `Quest: none`.

No new trailer keyword is introduced (a status-auto-advancing `Partial:` trailer was
considered and deferred — it would be a code change, and explicit `quest_set_status`
covers the need). To signal in-progress without closing, the agent may call
`quest_set_status SQ-xxxx partial` when meaningful progress lands; status otherwise
stays `open` until `Completes:` closes it.

### 6. The current-quest pointer — optional convenience, with hygiene

`quest_set_current` is **demoted, not removed** — it stays in the tool surface (it
serves the human's terminal auto-link workflow and agents that don't author commit
messages), but the skill does not lead with it. The skill teaches:

- **When it's the right tool:** teeing up a quest so a *human's* (or a
  non-message-authoring agent's) subsequent commits auto-link — e.g. handing work
  back to the human on SQ-0001. Not the agent's default path for its own commits.
- **Hygiene, if used:** current is sticky per-worktree. Clear it
  (`quest_set_current { clear: true }`) or switch it the moment focus changes;
  use `Quest: none` for a one-off unrelated commit made while current is set.
- **`quest_get_current`** is orientation only — read what the worktree points at.

### 7. Closing a quest — trailer-first, tool fallback

- **Preferred:** `Completes: SQ-xxxx` in the finishing commit — links the commit
  and closes the quest in one move, keeping the commit↔quest link that is
  side-quest's whole reason for existing.
- **Fallback — `quest_set_status`** — when there is no commit to carry a trailer:
  `done` for a completed-but-uncommitted or non-committing flow, `discarded` for a
  rejected idea, `deferred` for a postponed one, `partial` for advanced-not-done.

### 8. Triage and metadata

The skill **enumerates the allowed enum values inline** so an agent never has to
guess or introspect the schema to find them. The complete sets (from
`internal/quest`):

- **`type`** — `bug` | `feature`. Default `feature`.
- **`priority`** — `high` | `low`. Default `low`.
- **`status`** — `open` | `partial` | `done` | `deferred` | `discarded`.
  New quests start `open`.

Guidance:

- Set `type` and `priority` at capture when known; they default to feature/low.
  Reclassify later with `quest_reclassify { id, type?, priority? }` (at least one
  required).
- Edit title/tags with `quest_update { id, title?, tags? }`. Append durable
  findings with `quest_note { id, text }`.

**Tags** are intentionally *unconstrained* — arbitrary `key → value` string pairs,
not a fixed enum (the design's "minimal schema + flexible tags"). The skill teaches
their mechanics and their discipline, not a vocabulary:

- **Mechanics:** `quest_new` sets the initial map; `quest_update`'s `tags`
  **merges** into the existing map (does not replace it), and a key whose **value
  is the empty string deletes** that key. No validation — any key, any value.
- **Not a query axis (yet).** `quest_list` filters by `status`/`type`/`priority`
  only — there is **no tag filter** in either frontend. Tags are per-quest
  annotations, visible in `quest_show`, for recall — not something the agent can
  search by. The skill must not imply tags are filterable.
- **Discipline:** reach for `type`/`priority` first; use a tag only for a
  genuine cross-cutting attribute those enums can't express (e.g. `area=parser`
  as a note-to-self on the quest). Use them sparingly — free-form tags drift into
  inconsistent noise across sessions if over-applied, and can't be filtered to
  make sense of later.
- The skill deliberately does **not** prescribe an official tag vocabulary; that
  would invent schema the store does not enforce, and belongs to a later phase if
  ever wanted.
- Review with `quest_list { status?, type?, priority? }` (filters AND together;
  unknown filter values return a tool error).

### 9. Boundaries — what the skill does *not* do

- **No config changes.** `require_quest`, `auto_trailer`, `id_strategy`, and `tone`
  are project-owner decisions; no MCP tool exposes them and the skill must not try.
- **No derailing.** Capturing a quest is not permission to start it.
- **No voice/tone concern.** Tool payloads are neutral JSON by design; tone
  (Phase 5) never touches them.

### 10. Error handling the skill should convey

Bad input (unknown id, invalid enum, nothing-to-update) comes back as a tool-error
result the agent reads and corrects — it does not stop the server. Tools are
CAS-safe, so agent, human CLI, and hooks can operate on the same repo concurrently.

## Decisions taken during brainstorming (for the record)

- **Deliverable = the skill file only** (option A); plugin packaging stays Phase 7.
- **Activation = capture-reflex-primary** (not broad "working in a repo", not an
  enumerated multi-trigger).
- **Completion = trailer-first, `quest_set_status` fallback.**
- **Attribution = explicit per-commit trailers lead;** `quest_set_current` demoted
  but retained.
- **Multi-commit status = skill-only, no new `Partial:` trailer;** explicit
  `quest_set_status partial` covers in-progress signalling.
- **Living docs:** README/architecture are *not* touched this phase — the skill
  isn't wired into any loadable packaging yet, so there is no behavior for them to
  describe. The skill's existence is recorded in the SDD progress ledger and project
  memory, and the README/AGENTS mention is folded into Phase 7 packaging.
- **A short worked example is included** in the skill body (one realistic
  capture: idea → `quest_new` call shape → terse ack), because a concrete example
  lands a prose skill better than instructions alone.
- **Tags = mechanics + discipline, no prescribed vocabulary.** The skill enumerates
  the constrained enums (type/priority/status) but teaches tags as the free-form
  escape valve, used sparingly; it does not invent an "official" tag key set.

## Testing / verification

This is a prose deliverable; "done" is a correctness review, not a passing binary.

- **Accuracy review (most-capable model):** cross-check every tool name and
  parameter the skill mentions against `internal/mcp/tools.go`, and every trailer /
  current-quest / status claim against `internal/trailer`, `internal/store`, and
  the hook behavior. The skill must not reference a tool or parameter that does not
  exist, nor misdescribe what `Completes:`/`Quest:`/`Quest: none` or the
  current-quest pointer do.
- **Frontmatter validity:** `name` and `description` present; `description` phrased
  as an activating trigger (the capture-reflex moment), not a title.
- **Live tool check:** the documented calls are exercised against a running
  `side-quest serve` (in-memory transport, reusing the Phase 4 test harness) to
  confirm the tool names and parameter shapes behave exactly as the skill claims.
- No automated unit test is added for the markdown file itself.

## Out of scope (deferred)

- Plugin packaging: `.claude-plugin/plugin.json`, `marketplace.json`, `commands/`
  (incl. `/sq`), `bin/` bundling (Phase 7).
- The agent-agnostic `AGENTS.md` (Phase 7) — it will share this skill's conceptual
  content in a non-Claude-specific form; writing it now would duplicate prose that
  is about to be packaged.
- Any change to the MCP tools, store mutators, trailer parser, or hooks — including
  a `Partial:` trailer.
- **Follow-up (not this phase):** promote `type`/`priority`/`status` to real
  JSON-Schema `enum` constraints in the tool definitions so MCP clients hard-validate
  before the call reaches the store. A Phase 4 code change; recorded for later.
- **Follow-up (not this phase):** add a `tag` filter to `quest_list` (and the CLI
  `list`) so quests can be queried by tag. Frontend-only — `store.List()` already
  returns every quest and filtering is done frontend-side — but still a code change
  outside this skill. Until it lands, tags are annotation-only, which is why the
  skill teaches them as recall notes, not a query axis.
- Voice/tone (Phase 5) and the babelmap importer (Phase 6).
