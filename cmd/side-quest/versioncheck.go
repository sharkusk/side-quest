package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// versionDrift returns a one-line warning when own and other are both known and
// differ, else "". It underlies the serve-time check that the running MCP server
// and the side-quest binary on PATH are the same build (SQ-0039): the plugin can
// run its own bundled binary for `serve` while git hooks and the human CLI invoke
// whatever `side-quest` is on PATH, and an auto-updated plugin can drift the two.
func versionDrift(own, other string) string {
	if own == "" || other == "" || own == other {
		return ""
	}
	return fmt.Sprintf("warning (side-quest): this MCP server is version %s but the side-quest on PATH is %s — "+
		"git hooks and the CLI use the PATH binary, so behavior may differ; align them by updating the plugin or the installed binary.", own, other)
}

// pathBinaryDrift looks up side-quest on PATH and compares its reported version to
// own. It returns "" (quiet) when there is nothing to warn about: no side-quest on
// PATH, the PATH entry is THIS same executable (the ordinary `side-quest serve`
// case), the version call fails, or the versions match. Best-effort by design — a
// version check must never break serve.
func pathBinaryDrift(own string) string {
	p, err := exec.LookPath("side-quest")
	if err != nil {
		return ""
	}
	if self, err := os.Executable(); err == nil && sameExecutable(self, p) {
		return ""
	}
	out, err := exec.Command(p, "version").Output()
	if err != nil {
		return ""
	}
	return versionDrift(own, strings.TrimSpace(string(out)))
}

// sameExecutable reports whether two paths resolve to the same on-disk file, after
// following symlinks — LookPath may hand back a symlink (e.g. a plugin shim) that
// points at the very binary now running.
func sameExecutable(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	if a == b {
		return true
	}
	fa, err1 := os.Stat(a)
	fb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(fa, fb)
}
