package main

import (
	"path/filepath"
	"testing"
)

// pluginActiveFrom is the pure core of plugin detection: CLAUDE_PLUGIN_DATA being
// set, or the running binary residing under <home>/.claude/plugins/, means the
// Claude Code plugin is active (SQ-0064, D6).
func TestPluginActiveFrom(t *testing.T) {
	home := t.TempDir()
	underPlugins := filepath.Join(home, ".claude", "plugins", "data",
		"side-quest-side-quest", "bin", "side-quest-1.2.3")
	elsewhere := filepath.Join(t.TempDir(), "side-quest")

	cases := []struct {
		name       string
		pluginData string
		exePath    string
		home       string
		want       bool
	}{
		{"env set wins", "/somewhere/data", elsewhere, home, true},
		{"exe under plugins dir", "", underPlugins, home, true},
		{"exe elsewhere, no env", "", elsewhere, home, false},
		{"empty everything", "", "", "", false},
		{"exe set but home empty", "", underPlugins, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := pluginActiveFrom(c.pluginData, c.exePath, c.home); got != c.want {
				t.Errorf("pluginActiveFrom(%q,%q,%q) = %v, want %v",
					c.pluginData, c.exePath, c.home, got, c.want)
			}
		})
	}
}
