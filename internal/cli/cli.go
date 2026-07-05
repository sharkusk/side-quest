// Package cli is the shared core of side-quest's terminal-CLI launcher: choosing
// a PATH dir, and writing / removing / detecting the marked launcher that resolves
// the binary the Claude Code plugin provisions into its data dir. Both the
// install-cli/uninstall-cli subcommands (cmd/side-quest) and the MCP cli_* tools
// (internal/mcp, SQ-0066) call this one place, so enabling the CLI is a single
// implementation reachable two ways.
package cli

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

//go:embed launcher.sh
var launcherSh []byte

//go:embed launcher.cmd
var launcherCmd []byte

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

// LauncherName is the launcher filename for this OS: the native batch file on
// Windows, the extensionless POSIX script elsewhere.
func LauncherName() string {
	if runtime.GOOS == "windows" {
		return "side-quest.cmd"
	}
	return "side-quest"
}

// LauncherBody is the embedded launcher asset for this OS.
func LauncherBody() []byte {
	if runtime.GOOS == "windows" {
		return launcherCmd
	}
	return launcherSh
}

// InstallResult reports where Install placed the launcher.
type InstallResult struct {
	Path   string // absolute path of the written launcher
	Dir    string // the dir it was written into
	OnPath bool   // whether Dir is already on PATH
}

// Install writes the read-only launcher onto the user's PATH (spec D7). It reads
// $HOME, $XDG_BIN_HOME and $PATH from the environment, never clobbers a side-quest
// lacking Marker (D8), and reports where it landed and whether that dir is on PATH.
func Install() (InstallResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return InstallResult{}, err
	}
	candidates := InstallDirCandidates(home, os.Getenv("XDG_BIN_HOME"))
	dir := ChooseInstallDir(candidates, filepath.SplitList(os.Getenv("PATH")))
	onPath := dir != ""
	if dir == "" {
		dir = filepath.Join(home, ".local", "bin") // conventional fallback (D7)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return InstallResult{}, err
	}
	target := filepath.Join(dir, LauncherName())
	if b, err := os.ReadFile(target); err == nil && !bytes.Contains(b, []byte(Marker)) {
		return InstallResult{}, fmt.Errorf("refusing to overwrite %s — not a launcher we installed (no %q marker)", target, Marker)
	}
	if err := os.WriteFile(target, LauncherBody(), 0o755); err != nil {
		return InstallResult{}, err
	}
	return InstallResult{Path: target, Dir: dir, OnPath: onPath}, nil
}
