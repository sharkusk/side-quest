#!/bin/sh
# Provision the native side-quest binary into the plugin's data dir, where the MCP
# server command (${CLAUDE_PLUGIN_DATA}/bin/side-quest.exe in plugin.json) spawns it.
# Run by the plugin's SessionStart hook on macOS/Linux; the Windows arm is provision.ps1.
#
# Why a hook and not a launcher: Claude spawns a plugin's MCP command with no shell, so
# it can only launch a real executable by absolute path — not `node`, a shell script, or
# a .cmd (a native-installer Windows box has no `node` at all). So the MCP command points
# straight at the provisioned binary, and getting that binary onto disk moves here, ahead
# of the spawn (SQ-0081/0089 supersede the node/launch.js launcher).
#
# Idempotent (a version marker skips re-download) and deliberately non-fatal: no `set -e`
# and an explicit `exit 0` at the end, so a failed download/extract can never block
# session start — the MCP spawn simply surfaces the missing binary and a reconnect after
# the next successful run recovers. `set -u` still catches our own unset-var bugs.
set -u

REPO="sharkusk/side-quest"

# This script is <root>/scripts/provision.sh; VERSION ships at the plugin root.
SELF_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
ROOT=$(CDPATH= cd -- "$SELF_DIR/.." && pwd -P)
VERSION=$(cat "$ROOT/VERSION" 2>/dev/null || echo dev)

# CLAUDE_PLUGIN_DATA is exported into hook processes; reconstruct the documented
# default if we're ever run outside Claude. Must equal the MCP command's path and the
# terminal launcher's path or a provisioned binary is invisible to the others (SQ-0079).
DATA="${CLAUDE_PLUGIN_DATA:-$HOME/.claude/plugins/data/side-quest-side-quest}"
BINDIR="$DATA/bin"
TARGET="$BINDIR/side-quest.exe" # fixed name the MCP command points at (.exe is cosmetic here)
MARKER="$BINDIR/.provisioned-version"

# Already provisioned for this version.
if [ -x "$TARGET" ] && [ "$(cat "$MARKER" 2>/dev/null || true)" = "$VERSION" ]; then
	exit 0
fi

# A dev checkout has no release to download from.
if [ "$VERSION" = dev ]; then
	exit 0
fi
command -v curl >/dev/null 2>&1 || exit 0

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
esac
asset="side-quest_${VERSION}_${os}_${arch}.tar.gz"
# SIDE_QUEST_RELEASE_BASE overrides the download host — for tests (a local fixture
# server) and air-gapped mirrors. Unset in normal use → the GitHub release (SQ-0084).
base="${SIDE_QUEST_RELEASE_BASE:-https://github.com/$REPO/releases/download/v$VERSION}"

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	else
		echo ""
	fi
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
if curl -fsSL "$base/$asset" -o "$tmp/$asset" &&
	curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"; then
	want=$(grep " $asset\$" "$tmp/checksums.txt" | awk '{print $1}')
	got=$(sha256 "$tmp/$asset")
	if [ -n "$want" ] && [ "$want" = "$got" ]; then
		mkdir -p "$BINDIR"
		tar -xzf "$tmp/$asset" -C "$tmp"
		mv "$tmp/side-quest" "$TARGET"
		chmod +x "$TARGET"
		printf '%s' "$VERSION" >"$MARKER"
	fi
fi
exit 0
