# side-quest for agents

side-quest is a git-native issue tracker. Quests live on a dedicated git ref
(`refs/side-quest/quests`), not in the working tree. Any MCP-capable agent can
drive it through the `side-quest serve` stdio MCP server. This file is
agent-agnostic; the Claude-plugin-flavored version of the same guidance is
`skills/side-quest/SKILL.md`.

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
- Relay the flavor. A tool may append a flavored line beside its JSON; show it to
  the user verbatim — it's the tracker's voice.

## First-run setup (if the repo isn't tracked yet)

If a quest tool reports the repo isn't initialized — or you're the first to use
side-quest here — the quest ref and git hooks are missing. There's no MCP tool
for setup, so run it once in the shell (`side-quest` is on your `PATH`), then retry:

```
side-quest onboard   # create the quest ref, install the commit-linking hooks,
                     # and pull any existing quests if a remote is configured
```

This is a one-time, per-repo step so the user need not do it by hand; skip it
once the ref exists.

## Capture reflex

When a new, unrelated idea surfaces mid-task, capture it instead of derailing:
call `quest_new` with a concise `title` and a one-sentence `context` note
explaining *why it came up now*. Do not set it current. Then keep working.

## Attributing commits (the trailer contract)

Commits link to quests through message trailers, read by a `post-commit` hook:

- `Quest: SQ-0001` — link this commit to SQ-0001 (advances an open quest to
  partial; never closes it).
- `Completes: SQ-0001` — link it and mark SQ-0001 done.
- `Quest: none` — an explicit opt-out for a genuine chore.

Prefer explicit, per-commit trailers over implicit state — nothing is sticky, so
unrelated commits are never mis-attributed.

## The current quest

Each worktree can have one "current" quest (`quest_set_current`). When
`auto_trailer` is on (the default), the `prepare-commit-msg` hook injects that
quest's `Quest:` trailer automatically. Setting a current quest is optional and
mainly useful when teeing up a human's commits; agents should prefer writing the
trailer explicitly. If you do set one, clear it (`quest_set_current { "clear": true }`)
or switch it once that quest is done, so a later commit doesn't auto-link to a
finished quest.

**Worktrees start with no current quest.** The pointer is worktree-local — it
lives in that worktree's git dir, not on the ref — so a new `git worktree` (or a
fresh clone) does *not* inherit the main tree's current quest. The quest data
itself is shared (it rides the ref, so every worktree sees the same quests), but
the "current" pointer does not travel. If you move into a worktree and rely on
auto-linking, re-run `quest_set_current` there; otherwise commits won't
auto-link until you do. As an agent, the robust habit is to write the `Quest:`/
`Completes:` trailer explicitly, which works identically in every worktree with
no pointer to set.

## Triage values

`type` is `bug` or `feature` (default `feature`); `priority` is `high` or `low`
(default `low`); `status` is `open`, `partial`, `done`, `deferred`, or
`discarded` (new quests start `open`). Tags are free-form annotations.
