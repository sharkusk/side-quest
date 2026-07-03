# Manual setup (`init` + `install-hooks`)

The general path: it works with any MCP-capable agent, and it's what to use with
Claude Code before the [plugin](plugin.md) is available. Setup has two halves —
prepare the repo (quest store + git hooks), then tell your agent how to reach
side-quest.

**Prerequisite:** the `side-quest` binary on your `PATH`
([installation](install.md)).

## Per-project setup

Inside the repo you want to track:

```
side-quest init            # create the quest ref
side-quest install-hooks   # install git hooks + the refs/side-quest/* refspec
```

`init` creates the orphan ref (`refs/side-quest/quests`) that stores quests, off
your main history and never checked out. `install-hooks` installs the
`post-commit` and `prepare-commit-msg` shims and configures the fetch/push
refspecs so quests travel with `git fetch` / `git push` (see
[Sharing quests across machines](#sharing-quests-across-machines)). Both are
one-time, per-repo.

### Existing git hooks

Already have a git hook framework? (Husky, pre-commit, or a custom setup via
`core.hooksPath`.) `install-hooks` composes into whatever hooks directory git
uses, appending its own marker-guarded block without clobbering yours. Retire or
migrate any *conflicting* bookkeeping first, and unset a stale `core.hooksPath`
if you want the shims in the default `.git/hooks`.

## Wire up your agent

side-quest drives quests through an MCP server — `side-quest serve`, a stdio
subprocess your agent launches. Register it with a project `.mcp.json`:

```json
{ "mcpServers": { "side-quest": { "command": "side-quest", "args": ["serve"] } } }
```

Then add side-quest's guidance to your agent instructions. If the project already
has an `AGENTS.md`, append side-quest's block as a new section — **merge, don't
overwrite** (the block to copy is this repo's [`AGENTS.md`](../AGENTS.md)).
Optionally add a `/sq` capture command at `.claude/commands/sq.md`.

**Restart the agent session** so the MCP server, commands, and `AGENTS.md` load —
you'll be prompted once to approve a new project MCP server.

**`PATH` note:** a bare `side-quest serve` in `.mcp.json` needs `side-quest` on the
launching shell's `PATH` (e.g. `~/go/bin`). A GUI-launched agent may not inherit
your shell `PATH` — use an absolute path to the binary if the server fails to
start.

### MCP tools

The server exposes: `quest_new`, `quest_list`, `quest_show`, `quest_set_status`,
`quest_reclassify`, `quest_update`, `quest_note`, `quest_set_current`,
`quest_get_current`, `quest_link_commit`. Responses are neutral JSON. For
agent-facing guidance see [`AGENTS.md`](../AGENTS.md) and
[`skills/side-quest/SKILL.md`](../skills/side-quest/SKILL.md).

## Sharing quests across machines

Custom refs like `refs/side-quest/quests` are not fetched or pushed by default.
`side-quest install-hooks` configures both refspecs on `origin`, so once it has
run:

- `git fetch` / `git pull` also retrieve quest updates (the fetch refspec is
  additive — your normal fetch is unchanged).
- `git push` sends your current branch **and** the quest ref together (the push
  refspec keeps pushing your branch; it does not replace it).

A fresh `git clone` does **not** include the quest ref (Git skips custom refs on
clone) — run `side-quest install-hooks` in the clone, then `git fetch`, to pull
existing quests. To publish before the hooks are configured, push the ref
explicitly:

```
git push origin refs/side-quest/quests
```

A dedicated `sync` command that automates this is **planned** (see the
[README roadmap](../README.md#roadmap)).
