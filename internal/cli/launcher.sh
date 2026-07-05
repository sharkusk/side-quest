#!/bin/sh
# side-quest-cli-launcher — read-only resolver for the side-quest binary the Claude
# Code plugin provisions into its data dir. Installed by `side-quest install-cli`;
# removed by `side-quest uninstall-cli` or, once the plugin is gone, by this script
# itself. It NEVER downloads: if the plugin is installed, its MCP server has already
# placed the binary. Resolution:
#   1. newest <data>/bin/side-quest-* present  -> exec it
#   2. data dir present, no binary yet         -> ask the user to open a session
#   3. data dir absent (plugin uninstalled)    -> inert; offer/announce removal
set -eu

DATA="${CLAUDE_PLUGIN_DATA:-$HOME/.claude/plugins/data/side-quest-side-quest}"
BINDIR="$DATA/bin"

SELF_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
SELF="$SELF_DIR/$(basename -- "$0")"

# 1. newest provisioned binary wins.
if [ -d "$BINDIR" ]; then
	newest=
	for f in "$BINDIR"/side-quest-*; do
		[ -x "$f" ] || continue
		if [ -z "$newest" ] || [ "$f" -nt "$newest" ]; then
			newest=$f
		fi
	done
	if [ -n "$newest" ]; then
		exec "$newest" "$@"
	fi
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
