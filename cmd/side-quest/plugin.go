package main

import (
	"os"
	"path/filepath"
	"strings"
)

// pluginActive reports whether this side-quest process is running as part of the
// Claude Code plugin. onboard uses it to skip writing a project .mcp.json — the
// plugin already registers the side-quest MCP server, so a second identically
// named one would be redundant (SQ-0064, D6).
func pluginActive() bool {
	exe, _ := os.Executable()
	home, _ := os.UserHomeDir()
	return pluginActiveFrom(os.Getenv("SIDE_QUEST_PLUGIN"), os.Getenv("CLAUDE_PLUGIN_DATA"), exe, home)
}

// pluginActiveFrom is the pure core of pluginActive, taking its inputs explicitly
// so the detection logic is testable without a real plugin install. Three
// independent signals, cheapest and most reliable first:
//   - SIDE_QUEST_PLUGIN is set — every side-quest launcher (the plugin's download
//     launcher and the terminal-CLI launcher) stamps it before exec, so the
//     binary is recognized wherever it is staged. This is the authoritative
//     signal: the download launcher caches the real binary under ~/.cache, OUTSIDE
//     the plugin tree, and CLAUDE_PLUGIN_DATA does not propagate to a shell/Bash
//     invocation, so the two signals below both miss that case (SQ-0072).
//   - CLAUDE_PLUGIN_DATA is set — the plugin's persistent data dir, exported into
//     the MCP server process by Claude Code.
//   - the running binary lives under <home>/.claude/plugins/ — covers a binary the
//     terminal launcher execs straight from the plugin data dir.
func pluginActiveFrom(marker, pluginData, exePath, home string) bool {
	if marker != "" {
		return true
	}
	if pluginData != "" {
		return true
	}
	if home == "" || exePath == "" {
		return false
	}
	pluginsDir := filepath.Join(home, ".claude", "plugins") + string(os.PathSeparator)
	return strings.HasPrefix(exePath, pluginsDir)
}
