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
Every repo you want to track needs `side-quest init` and `side-quest
install-hooks` run once, to create the quest ref and install the git hooks (see
[Per-project setup](manual-setup.md#per-project-setup)). The plugin puts
`side-quest` on the *agent's* `PATH`, so the simplest way is to ask the agent to
run them. Your own shell's `PATH` does **not** include the plugin's binary — to
run these yourself from a terminal, [install side-quest](install.md) first.

The git hooks rely on that same `PATH`: an agent-run `git commit` finds
`side-quest` and records the link, while a commit from your own terminal only
does so if you've [installed side-quest](install.md) there — otherwise the
hook skips cleanly.

**Restart the Claude Code session** after installing so the MCP server, the `/sq`
command, and the skill load.

To move quests between machines, see
[Sharing quests across machines](manual-setup.md#sharing-quests-across-machines).
