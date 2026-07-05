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
	return pluginActiveFrom(os.Getenv("CLAUDE_PLUGIN_DATA"), exe, home)
}

// pluginActiveFrom is the pure core of pluginActive, taking its three inputs
// explicitly so the detection logic is testable without a real plugin install.
// Two independent signals, per the design's established plugin facts:
//   - CLAUDE_PLUGIN_DATA is set — the plugin's persistent data dir, exported into
//     every Claude-spawned process (e.g. the MCP server).
//   - the running binary lives under <home>/.claude/plugins/ — the terminal
//     launcher execs the data-dir binary, so even outside a Claude process the
//     executable path betrays the plugin origin.
func pluginActiveFrom(pluginData, exePath, home string) bool {
	if pluginData != "" {
		return true
	}
	if home == "" || exePath == "" {
		return false
	}
	pluginsDir := filepath.Join(home, ".claude", "plugins") + string(os.PathSeparator)
	return strings.HasPrefix(exePath, pluginsDir)
}
