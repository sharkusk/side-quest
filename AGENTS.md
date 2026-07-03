# side-quest for agents

side-quest is a git-native issue tracker. Quests live on a dedicated git ref
(`refs/side-quest/quests`), not in the working tree. Any MCP-capable agent can
drive it through the `side-quest serve` stdio MCP server. This file is
agent-agnostic; the Claude-plugin-flavored version of the same guidance is
`skills/side-quest/SKILL.md`.

## Capture reflex

When a new, unrelated idea surfaces mid-task, capture it instead of derailing:
call `quest_new` with a concise `title` and a one-sentence `context` note
explaining *why it came up now*. Do not set it current. Then keep working.

## Attributing commits (the trailer contract)

Commits link to quests through message trailers, read by a `post-commit` hook:

- `Quest: SQ-0001` — link this commit to SQ-0001 (no status change).
- `Completes: SQ-0001` — link it and mark SQ-0001 done.
- `Quest: none` — an explicit opt-out for a genuine chore.

Prefer explicit, per-commit trailers over implicit state — nothing is sticky, so
unrelated commits are never mis-attributed.

## The current quest

Each worktree can have one "current" quest (`quest_set_current`). When
`auto_trailer` is on (the default), the `prepare-commit-msg` hook injects that
quest's `Quest:` trailer automatically. Setting a current quest is optional and
mainly useful when teeing up a human's commits; agents should prefer writing the
trailer explicitly.

## Triage values

`type` is `bug` or `feature` (default `feature`); `priority` is `high` or `low`
(default `low`); `status` is `open`, `partial`, `done`, `deferred`, or
`discarded` (new quests start `open`). Tags are free-form annotations.
