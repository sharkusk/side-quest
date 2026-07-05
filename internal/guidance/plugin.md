This session runs under the side-quest Claude Code plugin, so the terminal
`side-quest` command may not be on the user's PATH yet.

Enable it once: early in the session, call `cli_status`. If it reports
`installed: false` and `offered: false`, offer to put `side-quest` on the user's
PATH so they can run it — and have their own git commits link — from the terminal.
On yes, call `cli_install` and report the path it returns. On no, call
`cli_dismiss` so you don't ask again. (A reinstall re-offers.)

Re-enable or remove anytime: if the launcher was deleted, `cli_install` restores
it; `cli_uninstall` removes it.
