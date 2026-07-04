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

`onboard` does everything below in one command; it is safe to re-run. Guidance
rides the MCP server, so `onboard` does not touch your `AGENTS.md` by default — add
`--agents-md` to also merge the guidance block in place (see
[Wire up your agent](#wire-up-your-agent)). To do it by hand instead:

```
side-quest init            # create the quest ref
side-quest install-hooks   # install git hooks + the refs/side-quest/quests fetch refspec
```

`init` creates the orphan ref (`refs/side-quest/quests`) that stores quests, off
your main history and never checked out. `install-hooks` installs the
`post-commit`, `prepare-commit-msg`, and `pre-push` shims, and configures the
fetch refspec so quests travel into a local tracking ref with `git fetch` — the
`pre-push` shim is what publishes them on `git push` (see
[Sharing quests across machines](#sharing-quests-across-machines)). The shims
call `side-quest` via your `PATH` (not by an absolute path), so the installed
block is identical on every machine — you can commit it into a shared hooks
dir — and it skips with a warning if `side-quest` is not on the `PATH` git runs
under. Both are one-time, per-repo.

### Existing git hooks

Already have a git hook framework? (Husky, pre-commit, or a custom setup via
`core.hooksPath`.) `install-hooks` composes into whatever hooks directory git
uses, appending its own marker-guarded block without clobbering yours — and it
tells you when it does. Because the appended block invokes `side-quest` via
`PATH`, it is safe to **commit** into a shared hook — it carries no
machine-local path. The trade-off: the hook needs `side-quest` on the `PATH`
that git runs under. A terminal or agent that has side-quest on `PATH` works; a
GUI client or cron job launched without it simply skips the side-quest step
(with a warning) rather than failing. See the `PATH` note under [Wire up your
agent](#wire-up-your-agent) — the hooks share that dependency. It **warns** if
`core.hooksPath` is set (that dir usually
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

That's enough on its own: **the server carries side-quest's core guidance**, sent
on connect as the MCP `instructions` field and reinforced by the tool descriptions,
so any MCP client is primed without a file in your repo.

Want the guidance reinforced in your own agent instructions too? Run `side-quest
agents-md` to print the canonical block (or `side-quest onboard --agents-md` to
merge it in place). The block is wrapped in `<!-- >>> side-quest >>> -->` markers
and version-stamped; if the project already has an `AGENTS.md`, it's appended as a
new section — **merge, don't overwrite** — and a later `onboard --agents-md`
refreshes it in place. On Claude Code, the plugin's skill and `/sq` command are the
equivalent reinforcement.

**Restart the agent session** so the MCP server (and any guidance files) load —
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

## Customizing guidance for your project

side-quest ships tag *mechanics* — `quest_new`/`quest_update` take key/value
`tags`, and `list --tag k=v` / `list --filter "…"` query them — but deliberately no
tag *taxonomy*: there is no universal one, so the core guidance stays taxonomy-free.
To have your agent tag quests consistently, declare the convention where you already
keep project instructions — your own `AGENTS.md`/`CLAUDE.md`, or a custom skill. For
example:

> Tag every side quest with `area=<subsystem>` (e.g. `area=parser`); tag bugs with
> `platform=<os>` when the issue is OS-specific.

The agent then passes those tags to `quest_new` on capture, and you slice the
backlog with them:

```
side-quest list --filter "area=parser and bug and not done"
```

This is the reinforcement layer doing its job: the MCP server carries the universal
core, and *your* instructions carry the project-specific conventions on top.

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

`side-quest sync` automates all of the above — fetch, merge, and push — and is
also what the `pre-push` hook runs on every `git push`. See
[`docs/sync.md`](sync.md) for how the merge works.
