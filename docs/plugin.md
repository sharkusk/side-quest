# Claude Code plugin

The one-command path for Claude Code. It bundles the agent wiring the
[manual setup](manual-setup.md) does by hand — but you still do the per-repo
setup yourself (see below).

```
/plugin marketplace add sharkusk/side-quest
/plugin install side-quest
```

This registers the `side-quest` MCP server and the `/sq` capture command, and
loads the guidance skill — nothing to add to your `AGENTS.md`. On first use it
**auto-provisions** the matching `side-quest` binary (downloaded from the release
and checksum-verified) into a per-plugin cache. If a download isn't possible
(offline, or before the project is public), install the binary yourself
([installation](install.md)) and the plugin will use it from your `PATH`.

**Still set up each repo.** The plugin wires your agent, not your repository. In
every repo you want to track, run `side-quest init` and `side-quest install-hooks`
to create the quest ref and install the git hooks — see
[Per-project setup](manual-setup.md#per-project-setup).

**Restart the Claude Code session** after installing so the MCP server, the `/sq`
command, and the skill load.

To move quests between machines, see
[Sharing quests across machines](manual-setup.md#sharing-quests-across-machines).
