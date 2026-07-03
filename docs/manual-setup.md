# Manual setup (`init` + `install-hooks`)

The general path: it works with any MCP-capable agent, and it's what to use with
Claude Code before the [plugin](plugin.md) is available. Setup has two halves —
prepare the repo (quest store + git hooks), then tell your agent how to reach
side-quest.

**Prerequisite:** the `side-quest` binary on your `PATH`
([installation](install.md)).

## Per-project setup

Inside the repo you want to track, the one-shot way:

```
side-quest onboard         # init + install-hooks + write .mcp.json (if absent)
```

`onboard` does everything below in one command and prints the AGENTS.md guidance
to paste; it is safe to re-run. To do it by hand instead:

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
uses, appending its own marker-guarded block without clobbering yours — and it
tells you when it does. It **warns** if `core.hooksPath` is set (that dir usually
belongs to another framework) and whenever it appends to a hook that already had
content, and it **skips** — leaving untouched — any hook whose shebang names a
non-sh interpreter (a Python or Node hook), since appending shell lines would
corrupt it. Heed those warnings: retire or migrate any *conflicting* bookkeeping
first, unset a stale `core.hooksPath` if you want the shims in the default
`.git/hooks`, and for a skipped hook, fold a `side-quest <hook>` call into it by
hand.

## Wire up your agent

side-quest drives quests through an MCP server — `side-quest serve`, a stdio
subprocess your agent launches. Register it with a project `.mcp.json`:

```json
{ "mcpServers": { "side-quest": { "command": "side-quest", "args": ["serve"] } } }
```

Then add side-quest's guidance to your agent instructions. Run `side-quest
agents-md` to print the canonical block (or copy this repo's
[`AGENTS.md`](../AGENTS.md)). If the project already has an `AGENTS.md`, append
the block as a new section — **merge, don't overwrite**. Optionally add a `/sq`
capture command at `.claude/commands/sq.md`.

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
`side-quest install-hooks` configures a fetch refspec on `origin` (and migrates
any pre-sync install off its old refspecs), so once it has run:

```
# Fetch the remote quest ref into a local tracking ref (never clobbers your
# live quests — sync merges from it):
git config --add remote.origin.fetch 'refs/side-quest/quests:refs/side-quest-remote/quests'

# Keep pushing your current branch. Do NOT add a quest push refspec — the
# side-quest pre-push hook publishes refs/side-quest/quests for you.
git config --add remote.origin.push HEAD
```

- `git fetch` / `git pull` retrieve quest updates into the tracking ref
  `refs/side-quest-remote/quests` (your normal fetch is unchanged).
- `git push` publishes the quest ref via the side-quest pre-push hook, alongside
  your current branch — there is no quest push refspec.

A fresh `git clone` does **not** include the quest ref (Git skips custom refs on
clone) — run `side-quest install-hooks` in the clone, then `git fetch`, to pull
existing quests. To publish before the hooks are configured, push the ref
explicitly:

```
git push origin refs/side-quest/quests
```

A dedicated `sync` command that automates this is **planned** (see the
[README roadmap](../README.md#roadmap)).
