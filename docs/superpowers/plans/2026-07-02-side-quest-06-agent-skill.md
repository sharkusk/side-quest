# Agent Skill (`SKILL.md`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Write `skills/side-quest/SKILL.md` — a workflow skill teaching an MCP-capable agent when and how to use side-quest's ten MCP tools across the quest lifecycle.

**Architecture:** A single prose (Markdown) file with YAML frontmatter. No code, no automated tests. The deliverable is grounded against the *live* Phase 4 MCP surface (in-memory harness + source) before and after writing, and verified by an accuracy review. This is intentionally a **single-task plan**: a skill file cannot be split into independently-committable pieces (half a skill is not a valid deliverable), so the usual TDD task template is replaced by *ground → write → verify claims → commit*.

**Tech Stack:** Markdown + YAML frontmatter. Reference surfaces (read-only, not modified): `internal/mcp/tools.go`, `internal/trailer/trailer.go`, `internal/store/*.go`, `internal/quest/quest.go`, and the `internal/mcp` in-memory test harness (`internal/mcp/server_test.go`).

## Global Constraints

- **Exactly one file is created:** `skills/side-quest/SKILL.md`. No other file is created or modified — no plugin scaffold, no `README.md`/`docs/architecture.md` changes, no code, no test files. (Spec §1, §2; Decisions: living docs untouched this phase.)
- **No changes to the MCP tools, store mutators, trailer parser, or hooks** — the skill describes them, it does not alter them. (Spec: Out of scope.)
- **Every tool name, parameter, trailer keyword, and enum value the skill states MUST match the code.** The verified ground-truth is in the Reference Facts block below; the skill must not drift from it. (Spec: Testing/verification.)
- **Frontmatter:** `name: using-side-quest`; `description` phrased as a capture-reflex-primary activation trigger (fires on "a new, unrelated idea surfaces mid-task"), also naming the rest of the loop. (Spec §1.)
- **Attribution leads with explicit per-commit trailers**; `quest_set_current` is demoted (retained, not led with). Completion is trailer-first with `quest_set_status` as fallback. (Spec §5, §6, §7; Decisions.)
- **Prose is agent-directed:** concise, imperative, unambiguous. The reader is an AI agent following instructions, not a human learner.
- **Commit message** ends with the two required footer lines:
  `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`
  and `Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws`.
- **Do not merge or push.** Per-task commits land on the feature branch only; merging/pushing waits for an explicit human request.

---

## Reference Facts (verified ground-truth — the skill must match these exactly)

Confirmed from source on 2026-07-02. Task 1 re-confirms them live before writing.

**Ten MCP tools** (`internal/mcp/tools.go`) and their parameters (JSON field names):

| Tool | Params (json names) |
|---|---|
| `quest_new` | `title`, `context?`, `type?`, `priority?`, `tags?` (map), `set_current?` (bool) |
| `quest_list` | `status?`, `type?`, `priority?` |
| `quest_show` | `id` |
| `quest_get_current` | — (returns `{current}`) |
| `quest_set_status` | `id`, `status` |
| `quest_reclassify` | `id`, `type?`, `priority?` |
| `quest_update` | `id`, `title?`, `tags?` (map) |
| `quest_note` | `id`, `text` |
| `quest_set_current` | `id?`, `clear?` (bool) |
| `quest_link_commit` | `sha` |

**Trailers** (`internal/trailer/trailer.go`, applied by `store.Link`/`AddCommit`):
- `Quest: SQ-xxxx` → links the commit hash to the quest's commit log; status unchanged.
- `Completes: SQ-xxxx` → links **and** sets status to `done` (stamps completion time).
- `Quest: none` → explicit escape hatch; commit deliberately unlinked.
- The `prepare-commit-msg` hook auto-injects `Quest: <current>` **only when no trailer is already present**, so an explicit trailer overrides it (no double-stamp).

**Current-quest pointer:** per-worktree file `<git-dir>/side-quest-current`; sole runtime consumer is the `prepare-commit-msg` hook; never on the ref, never pushed. Sticky until cleared/switched.

**Enums** (`internal/quest/quest.go`):
- `type`: `bug` | `feature` — default `feature` (`DefaultType`).
- `priority`: `high` | `low` — default `low` (`DefaultPriority`).
- `status`: `open` | `partial` | `done` | `deferred` | `discarded` — new quests start `open`.

**Query surface:** `quest_list` filters by `status`/`type`/`priority` only (AND'd); **no tag filter** exists. Tags are set via `quest_new`/`quest_update` (merge; empty value deletes a key) and are visible only via `quest_show`.

**Errors:** invalid input → MCP **tool error** result (agent-readable), never a crash; tools are CAS-safe (concurrent CLI/hook/agent use is safe).

---

## Task 1: Author `skills/side-quest/SKILL.md`

**Files:**
- Create: `skills/side-quest/SKILL.md`
- (No test file — prose deliverable; verification is the live check in Step 2 + the accuracy review gate.)

**Interfaces:**
- Consumes: the Reference Facts block above (the live MCP tool surface). No code interfaces.
- Produces: the skill file. Nothing downstream in this plan consumes it (Phase 7 packaging will later reference it).

- [ ] **Step 1: Create the feature branch**

```bash
cd /Volumes/Videos/Source/side-quest
git checkout -b agent-skill
```

Expected: `Switched to a new branch 'agent-skill'`.

- [ ] **Step 2: Live-check the tool surface (ground the prose in reality)**

Reuse the Phase 4 in-memory harness and confirm the surface is real and green, and that the registered tool names + param tags match the Reference Facts:

```bash
go test ./internal/mcp/... -v 2>&1 | tail -30
grep -nE 'jsonschema:|json:"' internal/mcp/tools.go
grep -nE 'StatusOpen|StatusPartial|StatusDone|StatusDeferred|StatusDiscarded|TypeBug|TypeFeature|PriorityHigh|PriorityLow|DefaultType|DefaultPriority' internal/quest/quest.go
grep -nE 'Quest:|Completes:|none' internal/trailer/trailer.go
```

Expected: `internal/mcp` tests PASS (the `TestListToolsExposesTen` test and the round-trip tests confirm all ten tools and their behavior); the grep output matches the Reference Facts (ten tools with the listed json param names; the five statuses, two types, two priorities and their defaults; the three trailer forms). If anything differs from the Reference Facts, STOP and reconcile the draft in Step 3 to what the code actually does — the code is the source of truth.

- [ ] **Step 3: Write `skills/side-quest/SKILL.md`**

Write the file with exactly this content (reconcile against Step 2 if the live surface differs from the Reference Facts):

````markdown
---
name: using-side-quest
description: Use when a new, unrelated idea, follow-up, bug, or TODO surfaces while you're working on something else — capture it as a side quest via the side-quest MCP tools instead of derailing. Also covers tracking, attributing commits to, closing, and triaging quests.
---

# Using side-quest

side-quest is a git-backed quest tracker. Its MCP server exposes ten tools for
capturing, tracking, and closing quests. This skill is about *when* to reach for
them — most importantly, the reflex to capture a stray idea without derailing your
current work.

## The capture reflex (the main thing)

When a new idea, follow-up, bug, or TODO occurs to you **while you're working on
something else**, capture it and immediately return to what you were doing.

- Call `quest_new` with a `title` and a one-sentence `context` — where `context`
  is *why this came up*, written from the live session (the tool records the
  branch/commit/cwd itself; you supply the human "why").
- **Do not** set `set_current` — a capture must never change what the worktree's
  commits attribute to.
- Give one short acknowledgement, then keep working.

**Hard rule: capture is create-and-return-only.** Do not start the captured work,
do not plan it, do not derail the session.

Example — mid-way through fixing a parser test, you notice a separate issue:

> `quest_new { "title": "Tokenizer re-scans whole buffer each call",`
> `"context": "Spotted while fixing the parser flake — SplitLine restarts from`
> `offset 0 every call, likely O(n^2) on big files.", "type": "bug" }`
>
> "Captured as SQ-0007. Back to the flake fix."

## Writing a good context

The one-sentence `context` is the part only an in-session agent can write, and the
part that makes a quest legible days later. Make it the *why*, not a restatement of
the title. One sentence.

## Attributing commits — write the trailer per commit

When you author a commit that works on a quest, put the trailer in the commit
message yourself. This is the default: it is always intentional, and nothing is
sticky, so no unrelated commit can ever be attributed by accident.

- `Quest: SQ-xxxx` — this commit did work on the quest (links it; does not close).
- `Completes: SQ-xxxx` — this commit finishes the quest (links **and** closes it).
- `Quest: none` — a genuine chore, deliberately linked to nothing.

Multiple commits per quest is normal: each in-progress commit carries
`Quest: SQ-xxxx`; the final one carries `Completes: SQ-xxxx`.

## The current quest (optional convenience)

`quest_set_current` points the worktree at a quest so that commits **without an
explicit trailer** auto-link to it. You author your own commit messages, so you
usually don't need it — prefer explicit trailers. Reach for it only to tee up a
*human's* (or a non-message-authoring agent's) upcoming commits.

If you do use it: it is **sticky** until you change it. Clear it
(`quest_set_current { "clear": true }`) or switch it the moment focus changes, and
use `Quest: none` on any one-off unrelated commit made while it's set. Use
`quest_get_current` to see what the worktree currently points at.

## Closing a quest

- **Preferred:** put `Completes: SQ-xxxx` in the finishing commit — one move links
  the commit and closes the quest.
- **Fallback — `quest_set_status`** — when there's no commit to carry a trailer:
  `done` (finished, uncommitted), `discarded` (rejected idea), `deferred`
  (postponed), or `partial` (advanced but not done).

## Triage and metadata

Allowed values (these are the complete sets):

- `type`: `bug` | `feature` — defaults to `feature`.
- `priority`: `high` | `low` — defaults to `low`.
- `status`: `open` | `partial` | `done` | `deferred` | `discarded` — new quests
  start `open`.

Set `type`/`priority` at capture when you know them; otherwise reclassify later with
`quest_reclassify { id, type?, priority? }` (supply at least one). Append findings
you learn with `quest_note { id, text }`. Edit the title with
`quest_update { id, title? }`.

**Tags** are free-form `key → value` pairs, not a fixed set. `quest_update`'s `tags`
**merges** into the existing tags (an empty value deletes a key). Tags are **not
searchable** — `quest_list` filters by `status`/`type`/`priority` only, and tags
show only in `quest_show`. So treat a tag as a note-to-self on one quest, reach for
`type`/`priority` first, and use tags sparingly.

## Reviewing quests

`quest_list { status?, type?, priority? }` — filters combine with AND; an unknown
filter value returns a tool error. `quest_show { id }` — the full quest, including
its tags, notes, and linked commits.

## Intent → tool

| You want to… | Use |
|---|---|
| Capture a stray idea, keep working | `quest_new` (no `set_current`) |
| Record something you learned on a quest | `quest_note` |
| Change a quest's title/tags | `quest_update` |
| Change type/priority | `quest_reclassify` |
| Mark a status directly (no commit) | `quest_set_status` |
| Close via a commit | `Completes: SQ-xxxx` trailer |
| Link a commit without closing | `Quest: SQ-xxxx` trailer |
| Tee up a human's commits | `quest_set_current` |
| See the current quest | `quest_get_current` |
| Browse / filter quests | `quest_list` |
| Read one quest fully | `quest_show` |
| Apply a commit's trailers after the fact | `quest_link_commit` |

## This skill does not

- **Change configuration** (`require_quest`, `auto_trailer`, `id_strategy`, `tone`)
  — those are the project owner's decisions; no tool exposes them.
- **Start captured work.** Capturing a quest is not permission to begin it.
- **Apply any voice/tone.** Tool responses are neutral JSON by design.
````

- [ ] **Step 4: Verify every claim against the ground-truth**

Re-read the finished file line by line and confirm against Step 2's output and the Reference Facts:
- Every tool name appears exactly as registered (ten names, spelled correctly).
- Every parameter named (`title`, `context`, `type`, `priority`, `tags`, `set_current`, `id`, `status`, `text`, `sha`, `clear`) matches a real json field.
- Trailer descriptions match: `Quest:` links-only, `Completes:` links+closes, `Quest: none` unlinks.
- Enum sets and defaults match (bug/feature→feature; high/low→low; the five statuses; start `open`).
- The "tags not searchable / `quest_list` filters status/type/priority only" claim holds.
Fix any drift in the file. The code wins every disagreement.

- [ ] **Step 5: Validate the frontmatter**

Confirm the file begins with a YAML frontmatter block delimited by `---` lines, containing exactly `name:` and `description:` keys, `name` is `using-side-quest`, and `description` opens with the capture-reflex trigger ("Use when a new, unrelated idea… surfaces while you're working on something else").

```bash
head -5 skills/side-quest/SKILL.md
```

Expected: the frontmatter block with `name: using-side-quest` and the reflex-first `description`.

- [ ] **Step 6: Commit**

```bash
git add skills/side-quest/SKILL.md
git commit -m "$(cat <<'EOF'
feat(skill): add using-side-quest agent workflow skill

A capture-reflex-primary workflow skill teaching an MCP agent when and how
to use side-quest's ten tools: capture without derailing, attribute commits
with explicit Quest:/Completes: trailers, close, and triage.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws
EOF
)"
```

Expected: one commit on branch `agent-skill` adding one file.

---

## Verification (whole-deliverable)

Because there is no automated test, "done" = both of these pass:

1. **Live tool check** (Step 2): `go test ./internal/mcp/... ` is green and the tool names/params/enums/trailers in the file match source.
2. **Accuracy review** (the final whole-branch review under subagent-driven-development, dispatched on the most capable model): a reviewer cross-checks every tool name/parameter against `internal/mcp/tools.go`, every trailer/current-quest/status claim against `internal/trailer` + `internal/store` + `internal/quest`, confirms the frontmatter is valid and reflex-first, confirms only the one file changed, and confirms the skill states nothing the code does not do.

## Self-Review (planner)

- **Spec coverage:** §1 identity/activation → frontmatter (Steps 3, 5). §2 structure → the eight body sections. §3 capture reflex + hard rule + example → "The capture reflex". §4 narrative context → "Writing a good context". §5 explicit-per-commit attribution → "Attributing commits". §6 current-quest demoted + hygiene → "The current quest". §7 trailer-first close + fallback → "Closing a quest". §8 enums enumerated + defaults + tags mechanics/discipline + not-a-query-axis → "Triage and metadata" + "Reviewing quests". §9 boundaries → "This skill does not". §10 error handling → covered inline (tool errors; CAS note folded into the reference, not forced into prose). Verification → Steps 2, 4 + Accuracy review. All covered.
- **Placeholder scan:** none — the full file content is inline.
- **Consistency:** tool names and json params match the Reference Facts, which were taken from source; enums match `internal/quest/quest.go`. Single task, so no cross-task signature drift.
