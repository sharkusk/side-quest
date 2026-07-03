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
explicit trailer** auto-link to it (when auto-trailer is on, the default). You author your own commit messages, so you
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
searchable** — `quest_list` filters by `status`/`type`/`priority` only; a quest's
tags show in its details (`quest_show`) but you can't query by them. So treat a tag
as a note-to-self on one quest, reach for `type`/`priority` first, and use tags
sparingly.

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
