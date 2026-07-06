# Claude Code plugin

The one-command path for Claude Code. It bundles the agent wiring the
[manual setup](manual-setup.md) does by hand — but you still do the per-repo
setup yourself (see below).

```
/plugin marketplace add sharkusk/side-quest
/plugin install side-quest
```

This registers the `side-quest` MCP server — which carries side-quest's core
guidance itself — plus the `/sq` capture command and the guidance skill that
reinforces it. Nothing to add to your `AGENTS.md`. On first use it
**auto-provisions** the matching `side-quest` binary (downloaded from the release
and checksum-verified) into a per-plugin cache. If a download isn't possible
(offline, or before the project is public), install the binary yourself
([installation](install.md)) and the plugin will use it from your `PATH`.

**Still set up each repo.** The plugin wires your agent, not your repository.
Every repo you want to track needs `side-quest onboard` run once — it creates the
quest ref and installs the git hooks. Under the plugin it **skips** writing a
project `.mcp.json` (the plugin already registers the MCP server). The plugin puts
`side-quest` on the *agent's* `PATH`, so the simplest way is to ask the agent to
run `onboard`; it is safe to re-run, and a later `onboard` refreshes the hooks
after a plugin update. See
[Per-project setup](manual-setup.md#per-project-setup) for what it does.

## Run `side-quest` from your own terminal

The plugin provisions the `side-quest` binary for its MCP server, but it isn't on
your command `PATH` — so out of the box you can't run `side-quest` in a terminal,
and a `git commit` (yours *or* the agent's) won't record the quest link (the hook
skips cleanly when it can't find `side-quest`). Enabling the terminal CLI puts a
launcher on your `PATH` that your shell **and** the agent's Bash tool both use. Two
equally good ways to do it; pick either:

- **Let the agent enable it.** Under the plugin the agent will offer, once, to
  enable the terminal CLI — or just ask it any time ("enable the side-quest CLI").
  It runs the `cli_install` MCP tool, which writes a small, read-only launcher
  named `side-quest` into the first of `$XDG_BIN_HOME`, `~/.local/bin`, `~/bin`, or
  `~/go/bin` that is already on your `PATH` (falling back to `~/.local/bin`, with a
  note to add it). The launcher only resolves and runs the binary the plugin
  already provisioned — **nothing is downloaded** — and it self-heals across plugin
  updates. To remove it later, ask the agent to run `cli_uninstall`, or run
  `side-quest uninstall-cli` yourself.

- **Install the binary yourself.** Follow [installation](install.md) to put
  `side-quest` on your `PATH` the usual way — a prebuilt binary, `go install`, or a
  source build. Prefer this if you'd rather manage the binary directly, or on
  Windows.

Either route gives your terminal a working `side-quest`, and from then on a
terminal `git commit` records the quest link exactly as an agent-run commit does.

**Restart the Claude Code session** after installing the plugin so the MCP server,
the `/sq` command, and the skill load.

To move quests between machines, see
[Sharing quests across machines](manual-setup.md#sharing-quests-across-machines).

## Uninstalling

Remove the plugin from Claude Code the usual way (`/plugin`). What's left afterward
depends on your platform, because Claude Code manages its plugin **data** directory
(where side-quest provisions its binary) differently per OS:

- **macOS / Linux** — Claude Code removes the plugin's data directory on uninstall,
  so the provisioned `side-quest` binary goes with it. If you enabled the terminal
  CLI, its launcher notices the binary is gone the next time you run `side-quest`,
  reports that it's inert, and offers to remove itself.

- **Windows** — Claude Code leaves the data directory in place, so the provisioned
  `side-quest.exe` — and a terminal launcher, if you enabled one — keep working after
  the plugin is gone. To finish cleaning up: run `side-quest uninstall-cli` to remove
  the terminal launcher, then delete the data directory yourself if you want the
  binary gone:

  ```
  %USERPROFILE%\.claude\plugins\data\side-quest-side-quest
  ```

Either way, your quests are safe: they live in the repository's git refs
(`refs/side-quest/*`), not in the plugin, so uninstalling never touches them.
