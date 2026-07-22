---
name: using-side-quest
description: Use when a new, unrelated idea, follow-up, bug, or TODO surfaces while you're working on something else — capture it as a side quest via the side-quest MCP tools instead of derailing. Also covers tracking, attributing commits to, closing, and triaging quests.
---

# Using side-quest

side-quest is a git-backed quest tracker. Its MCP server exposes tools for
capturing, tracking, and closing quests (plus a few lifecycle helpers). This skill
is about *when* to reach for them — most importantly, the reflex to capture a
stray idea without derailing your current work.

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
- Finished a change the user should judge — not one tests or an obvious check
  settle? Set it to confirm (quest_set_status, or a "Confirm: SQ-1234" trailer);
  it stays outstanding till they accept or reopen it. Else complete it.
- Outstanding = open, partial, confirm. quest_list lists them, quest_show reads one,
  quest_brief snapshots the state — call it first when resuming.
- Relay the flavor: a tool may append a flavored line beside its JSON — show it
  verbatim; it's the tracker's voice.

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
- `Confirm: SQ-xxxx` — this commit finishes the work but the user should confirm
  it (links **and** parks the quest in `confirm` for their sign-off).
- `Completes: SQ-xxxx` — this commit finishes the quest (links **and** closes it).
- `Quest: none` — a genuine chore, deliberately linked to nothing.

Multiple commits per quest is normal: each in-progress commit carries
`Quest: SQ-xxxx`; the final one carries `Completes: SQ-xxxx` (or
`Confirm: SQ-xxxx` when the user should judge it first).

## The current quest

`quest_set_current` points the worktree at a quest so that commits **without an
explicit trailer** auto-link to it (when auto-trailer is on, the default). Per the
core guidance: work one at a time and make the quest you're on current, so the
hooks attribute your commits without you touching hashes. An explicit trailer in
a commit message always takes precedence over the pointer, so writing
`Completes:`/`Confirm:` yourself composes fine with it.

It is **sticky** until you change it. Clear it
(`quest_set_current { "clear": true }`) or switch it the moment focus changes, and
use `Quest: none` on any one-off unrelated commit made while it's set. Use
`quest_get_current` to see what the worktree currently points at.

**In a git worktree, the pointer starts empty.** It's worktree-local (kept in the
worktree's git dir, never on the ref), so a fresh `git worktree` or clone doesn't
inherit the main tree's current quest — `quest_get_current` returns empty until
you set one there. The quests themselves are shared across worktrees (they live on
the ref); only this pointer is local. Re-run `quest_set_current` after switching
into the worktree (or write explicit trailers, which need no pointer).

## Closing a quest

- **Preferred:** put `Completes: SQ-xxxx` in the finishing commit — one move links
  the commit and closes the quest.
- **Fallback — `quest_set_status`** — when there's no commit to carry a trailer:
  `done` (finished, uncommitted), `confirm` (finished, awaiting the user's
  sign-off), `discarded` (rejected idea), `deferred` (postponed), or `partial`
  (advanced but not done).
- **After closing, clear the current quest** if it pointed there
  (`quest_set_current { "clear": true }`) or switch it to the next one — otherwise a
  later commit auto-links to the quest you just finished.

## Triage and metadata

Allowed values (these are the complete sets):

- `type`: `bug` | `feature` — defaults to `feature`.
- `priority`: `high` | `low` — defaults to `low`.
- `status`: `open` | `partial` | `confirm` | `done` | `deferred` | `discarded` —
  new quests start `open`.

Set `type`/`priority` at capture when you know them; otherwise reclassify later with
`quest_reclassify { id, type?, priority? }` (supply at least one). Append findings
you learn with `quest_note { id, text }`. Edit the title with
`quest_update { id, title? }`.

**Tags** are free-form `key → value` pairs, not a fixed set. `quest_update`'s `tags`
**merges** into the existing tags (an empty value deletes a key). Tags **are
queryable**: `quest_list`'s `tags` filter matches quests carrying every given
key=value pair (AND), which makes a shared tag a good way to group work — e.g.
tagging a release's scope. Reach for `type`/`priority` first for the built-in
dimensions, and tags for everything else.

## Reviewing quests

`quest_list { status?, type?, priority?, tags? }` — filters combine with AND; an
unknown filter value returns a tool error. `quest_show { id }` — the full quest,
including its tags, notes, and linked commits. `quest_history { id }` — the
quest's change log (who changed what, when), for historical questions.
`quest_brief { closed_shown? }` — a one-shot resume snapshot (current quest in
full, the outstanding backlog, the most-recently-closed quests); call it at the
start of a session to orient without paging through quests one by one.

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
| Make the quest you're working on current (hooks link your commits) | `quest_set_current` |
| See the current quest | `quest_get_current` |
| Browse / filter quests | `quest_list` |
| Read one quest fully | `quest_show` |
| Ask when/who/what changed a quest | `quest_history` |
| Orient at session start (current + backlog + recently closed) | `quest_brief` |
| Apply a commit's trailers after the fact | `quest_link_commit` |

## This skill does not

- **Change configuration** (`require_quest`, `auto_trailer`, `id_strategy`, `tone`)
  — those are the project owner's decisions; no tool exposes them.
- **Start captured work.** Capturing a quest is not permission to begin it.
- **Suppress the tracker's voice.** A mutation's first content block is neutral
  JSON, but a second, tone-flavored line may ride beside it — relay that line to
  the user verbatim (see the core guidance's "Relay the flavor").

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
- **Stay current after an update.** Your guidance and tools come from the MCP
  server's binary, loaded at session start, so after the user updates side-quest a
  still-running server serves the old build until it restarts. If the user mentions
  updating/reinstalling (or a tool or enum looks stale), call `server_info` and
  compare its version to the build they installed; if it's behind, tell them to
  restart the MCP server (`/mcp`) or start a fresh session before relying on this
  guidance — a reconnect reloads tools, not the session-start brief.
