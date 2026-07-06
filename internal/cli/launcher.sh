#!/bin/sh
# side-quest-cli-launcher — read-only resolver for the side-quest binary the Claude
# Code plugin provisions into its data dir. Installed by `side-quest install-cli`;
# removed by `side-quest uninstall-cli` or, once the plugin is gone, by this script
# itself. It NEVER downloads: if the plugin is installed, its MCP server has already
# placed the binary. Resolution:
#   1. <data>/bin/side-quest.exe present       -> exec it
#   2. data dir present, no binary yet         -> ask the user to open a session
#   3. data dir absent (plugin uninstalled)    -> inert; offer/announce removal
set -eu

# Mark the binary we exec as plugin-launched, so `onboard` skips the redundant
# project .mcp.json even when run straight from this launcher (SQ-0072).
export SIDE_QUEST_PLUGIN=1

DATA="${CLAUDE_PLUGIN_DATA:-$HOME/.claude/plugins/data/side-quest-side-quest}"
BINDIR="$DATA/bin"

SELF_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
SELF="$SELF_DIR/$(basename -- "$0")"

# 1. the provisioned binary (fixed name — the plugin's SessionStart hook writes exactly
# this path, the same one the MCP server command spawns; SQ-0079/0089).
BIN="$BINDIR/side-quest.exe"
if [ -x "$BIN" ]; then
	exec "$BIN" "$@"
fi

# 2. data dir exists but nothing provisioned yet.
if [ -d "$DATA" ]; then
	echo "side-quest: binary not found — open a Claude Code session to finish setup." >&2
	exit 1
fi

# 3. data dir absent => the plugin is gone. This launcher is inert.
if [ -t 0 ] && [ -t 1 ]; then
	printf 'side-quest: the plugin is no longer installed; this launcher (%s) is inert.\n' "$SELF" >&2
	printf 'Remove it now? [y/N] ' >&2
	read -r ans
	case "$ans" in
	[yY]*)
		rm -f "$SELF" "$SELF.cmd" && echo "side-quest: removed $SELF" >&2
		;;
	*)
		echo "side-quest: left in place — safe to remove: rm $SELF" >&2
		;;
	esac
else
	echo "side-quest: the plugin is gone; this launcher is inert — safe to remove: rm $SELF" >&2
fi
exit 1
