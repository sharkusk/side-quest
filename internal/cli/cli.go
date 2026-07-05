// Package cli is the shared core of side-quest's terminal-CLI launcher: choosing
// a PATH dir, and writing / removing / detecting the marked launcher that resolves
// the binary the Claude Code plugin provisions into its data dir. Both the
// install-cli/uninstall-cli subcommands (cmd/side-quest) and the MCP cli_* tools
// (internal/mcp, SQ-0066) call this one place, so enabling the CLI is a single
// implementation reachable two ways.
package cli

import (
	"path/filepath"
)

// Marker identifies a launcher this package wrote. Uninstall removes only files
// carrying it, and Install refuses to overwrite a side-quest that lacks it (a
// user's own build) — spec D8. It is distinct from the plugin shim's comment
// "side-quest plugin launcher" (no hyphen there), so the two never collide.
const Marker = "side-quest-cli-launcher"

// InstallDirCandidates lists the conventional user bin dirs Install prefers,
// most-preferred first (spec D7).
func InstallDirCandidates(home, xdgBinHome string) []string {
	return []string{
		xdgBinHome,
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, "bin"),
		filepath.Join(home, "go", "bin"),
	}
}

// ChooseInstallDir returns the first candidate already on PATH, or "" when none
// is — pure, so it is testable without touching the real environment.
func ChooseInstallDir(candidates, pathDirs []string) string {
	on := make(map[string]bool, len(pathDirs))
	for _, p := range pathDirs {
		if p != "" {
			on[p] = true
		}
	}
	for _, c := range candidates {
		if c != "" && on[c] {
			return c
		}
	}
	return ""
}
