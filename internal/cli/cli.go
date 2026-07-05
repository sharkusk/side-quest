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
	"io"
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

// markerScanLimit bounds how far into a file we look for Marker. A launcher this
// package writes is a short script carrying Marker in its first comment line; the
// compiled side-quest binary embeds launcher.sh/.cmd — marker and all — via
// //go:embed, but that copy lives deep in the binary's data section, far past this
// limit. Scanning only the prefix tells the two apart, so a user's own go-installed
// side-quest is never mistaken for our launcher and clobbered (SQ-0074).
const markerScanLimit = 512

// isMarkedLauncher reports whether the file at p is a launcher this package wrote:
// a file carrying Marker within its first markerScanLimit bytes. It reads only that
// prefix, so a multi-megabyte binary named side-quest is neither slurped whole nor
// matched on a marker buried in its embedded copy of the launcher. The returned
// error is the open/read error (e.g. not-exist, permission), letting callers tell
// "absent" from "cannot verify".
func isMarkedLauncher(p string) (bool, error) {
	f, err := os.Open(p)
	if err != nil {
		return false, err
	}
	defer f.Close()
	buf := make([]byte, markerScanLimit)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return false, err
	}
	return bytes.Contains(buf[:n], []byte(Marker)), nil
}

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
	switch marked, err := isMarkedLauncher(target); {
	case err == nil && !marked:
		return InstallResult{}, fmt.Errorf("refusing to overwrite %s — not a launcher we installed (no %q marker near the top)", target, Marker)
	case err != nil && !os.IsNotExist(err):
		// A read error other than not-exist means we cannot confirm this is our
		// launcher, so we must not clobber it (D8).
		return InstallResult{}, fmt.Errorf("refusing to overwrite %s — cannot verify it is our launcher: %w", target, err)
	}
	if err := os.WriteFile(target, LauncherBody(), 0o755); err != nil {
		return InstallResult{}, err
	}
	return InstallResult{Path: target, Dir: dir, OnPath: onPath}, nil
}

// launcherDirs is the deduped set of dirs a launcher might live in: everything on
// $PATH plus the conventional install candidates (D7). Scanning the union makes
// Status/Uninstall robust even when the caller's $PATH is the GUI PATH rather than
// the user's login shell (the MCP server sees that — spec D7).
func launcherDirs() []string {
	home, _ := os.UserHomeDir()
	all := append(filepath.SplitList(os.Getenv("PATH")), InstallDirCandidates(home, os.Getenv("XDG_BIN_HOME"))...)
	seen := make(map[string]bool, len(all))
	out := make([]string, 0, len(all))
	for _, d := range all {
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

// UninstallResult reports what Uninstall did.
type UninstallResult struct {
	Removed []string // launcher paths removed
	Refused []string // paths left in place (present but unmarked)
}

// Uninstall removes the marked launcher(s) while the plugin is still installed
// (the plugin-gone case is the launcher's own self-removal — spec D4.3/D8). It
// never removes a side-quest lacking Marker.
func Uninstall() (UninstallResult, error) {
	var res UninstallResult
	for _, dir := range launcherDirs() {
		for _, name := range []string{"side-quest", "side-quest.cmd"} {
			p := filepath.Join(dir, name)
			marked, err := isMarkedLauncher(p)
			if err != nil {
				continue
			}
			if !marked {
				res.Refused = append(res.Refused, p)
				continue
			}
			if err := os.Remove(p); err != nil {
				return res, err
			}
			res.Removed = append(res.Removed, p)
		}
	}
	return res, nil
}

// StatusResult reports whether a marked launcher is present.
type StatusResult struct {
	Installed bool
	Path      string // the first marked launcher found, if any
}

// Status scans for a marked launcher (spec D5's cli_status). Like Uninstall it
// looks in the union of $PATH and the candidate dirs.
func Status() StatusResult {
	for _, dir := range launcherDirs() {
		for _, name := range []string{"side-quest", "side-quest.cmd"} {
			p := filepath.Join(dir, name)
			if marked, err := isMarkedLauncher(p); err == nil && marked {
				return StatusResult{Installed: true, Path: p}
			}
		}
	}
	return StatusResult{}
}
