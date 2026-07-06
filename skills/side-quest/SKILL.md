---
name: using-side-quest
description: Use when a new, unrelated idea, follow-up, bug, or TODO surfaces while you're working on something else — capture it as a side quest via the side-quest MCP tools instead of derailing. Also covers tracking, attributing commits to, closing, and triaging quests.
---

# Using side-quest

side-quest is a git-backed quest tracker. Its MCP server exposes ten tools for
capturing, tracking, and closing quests. This skill is about *when* to reach for
them — most importantly, the reflex to capture a stray idea without derailing your
current work.

## Core

side-quest is a git-native tracker; a "quest" is just an issue, task, or
follow-up you manage through these tools, not by editing files.

- Capture without derailing. An idea surfaces mid-task? File it with quest_new
  (one-line restatement + why it came up) and resume. Set type/priority only
  when the request makes it obvious — a crash or regression is a bug;
  "urgent"/"critical"/"blocking" is high — else keep defaults.
- Work one at a time. Make the quest you're on current (quest_set_current) so the
  git hooks link your commits to it (you never touch hashes); clear it — or switch
  it — once that quest is done, so a later commit doesn't attach to a finished quest.
- Linking a commit (Quest: SQ-1234, or the current-quest auto-link) advances an
  open quest to partial — work has started; "Completes: SQ-1234" closes it, and
  quest_set_status sets any state directly.
- Active work is both open and partial — treat them alike as outstanding. List work
  with quest_list (shows open + partial by default); read one with quest_show.

## First-run setup (if the repo isn't tracked yet)

If a quest tool reports the repo isn't initialized — or you're the first to use
side-quest in this repo — the quest ref and git hooks are missing. There's no MCP
tool for setup, so run it once in the shell (`side-quest` is on your `PATH`), then
retry:

```
side-quest onboard   # create the quest ref, install the commit-linking hooks,
                     # and pull any existing quests if a remote is configured
```

This is a one-time, per-repo step, done so the user need not run it by hand; skip
it once the ref exists.

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
- **After closing, clear the current quest** if it pointed there
  (`quest_set_current { "clear": true }`) or switch it to the next one — otherwise a
  later commit auto-links to the quest you just finished.

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

## Plugin lifecycle (Claude Code)

When side-quest runs as the Claude Code plugin, the MCP server's instructions guide
you through its lifecycle — this section is the human-readable version.

- **Enable the terminal CLI (once).** Early in a session, call `cli_status`. If the
  terminal CLI isn't enabled and hasn't been offered, offer to put `side-quest` on
  the user's PATH; on yes call `cli_install` (report the path it returns), on no
  call `cli_dismiss`. Re-run `cli_install` anytime to re-enable if the launcher was
  removed; `cli_uninstall` removes it.
- **Set up or refresh a repo.** To track a repo (or refresh it after a plugin
  update), run `side-quest onboard` — it creates the quest ref, installs the git
  hooks, and (outside the plugin) writes `.mcp.json`. Safe to re-run.
