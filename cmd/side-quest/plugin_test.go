package main

import (
	"path/filepath"
	"testing"
)

// pluginActiveFrom is the pure core of plugin detection: the SIDE_QUEST_PLUGIN
// marker a launcher stamps, CLAUDE_PLUGIN_DATA being set, or the running binary
// residing under <home>/.claude/plugins/, each means the Claude Code plugin is
// active (SQ-0064/SQ-0072, D6).
func TestPluginActiveFrom(t *testing.T) {
	home := t.TempDir()
	underPlugins := filepath.Join(home, ".claude", "plugins", "data",
		"side-quest-side-quest", "bin", "side-quest-1.2.3")
	// The download launcher stages the real binary here, outside the plugin tree,
	// so the exe-path signal alone misses it — the marker is what saves it (SQ-0072).
	inCache := filepath.Join(home, ".cache", "side-quest", "bin", "side-quest-1.2.3")
	elsewhere := filepath.Join(t.TempDir(), "side-quest")

	cases := []struct {
		name       string
		marker     string
		pluginData string
		exePath    string
		home       string
		want       bool
	}{
		{"marker wins over everything else missing", "1", "", inCache, home, true},
		{"env set wins", "", "/somewhere/data", elsewhere, home, true},
		{"exe under plugins dir", "", "", underPlugins, home, true},
		{"cache-staged binary, no marker/env", "", "", inCache, home, false},
		{"exe elsewhere, no signals", "", "", elsewhere, home, false},
		{"empty everything", "", "", "", "", false},
		{"exe set but home empty", "", "", underPlugins, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := pluginActiveFrom(c.marker, c.pluginData, c.exePath, c.home); got != c.want {
				t.Errorf("pluginActiveFrom(%q,%q,%q,%q) = %v, want %v",
					c.marker, c.pluginData, c.exePath, c.home, got, c.want)
			}
		})
	}
}
