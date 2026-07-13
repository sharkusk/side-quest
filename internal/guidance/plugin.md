This session runs under the side-quest Claude Code plugin, so the terminal
`side-quest` command may not be on the user's PATH yet.

Enable it once: early in the session, call `cli_status`. If it reports
`installed: false` and `offered: false`, offer to put `side-quest` on the user's
PATH so they can run it — and have their own git commits link — from the terminal.
On yes, call `cli_install` and report the path it returns. On no, call
`cli_dismiss` so you don't ask again. (A reinstall re-offers.)

Re-enable or remove anytime: if the launcher was deleted, `cli_install` restores
it; `cli_uninstall` removes it.

Stay current after an update: your guidance and tools come from the MCP server's
binary, which Claude Code loads at session start — so after the user updates
side-quest, a still-running server keeps serving the old build until it restarts.
If the user says they updated or reinstalled side-quest (or a tool or enum looks out
of date), call `server_info` and compare its version to the build they installed. If
it's behind, tell them to restart the MCP server (`/mcp`) or start a fresh Claude
Code session before relying on this guidance — a reconnect reloads tools, but the
brief is read only at session start.
