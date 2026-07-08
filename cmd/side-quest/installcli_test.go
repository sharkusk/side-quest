package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharkusk/side-quest/commands"
	"github.com/sharkusk/side-quest/internal/cli"
)

// install-cli is wired into run() and writes a marked launcher end-to-end.
func TestInstallCliCommand(t *testing.T) {
	bin := buildBinary(t)
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir() reads USERPROFILE on Windows (SQ-0086)
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("PATH", dir)

	out, code := runBin(t, bin, home, "install-cli")
	if code != 0 {
		t.Fatalf("install-cli exit=%d out=%q", code, out)
	}
	b, err := os.ReadFile(filepath.Join(dir, cli.LauncherName()))
	if err != nil {
		t.Fatalf("launcher not written: %v", err)
	}
	if !strings.Contains(string(b), cli.Marker) {
		t.Error("written launcher is missing the marker")
	}
}

// installCliEnv points HOME/PATH at a throwaway prefix so the launcher install
// side of install-cli never touches the real machine.
func installCliEnv(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".local", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir() reads USERPROFILE on Windows (SQ-0086)
	t.Setenv("XDG_BIN_HOME", "")
	// Prepend the launcher dir but KEEP the real PATH so `git` stays resolvable —
	// installSqCommand shells out to `git rev-parse` to find the repo root.
	t.Setenv("PATH", filepath.Join(home, ".local", "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestInstallCliInstallsSqCommand (SQ-0107): install-cli also drops the /sq slash
// command into the current repo's .claude/commands so plugin users get a bare
// /sq (Claude Code namespaces the plugin's own copy as /side-quest:sq).
func TestInstallCliInstallsSqCommand(t *testing.T) {
	bin := buildBinary(t)
	repo, _ := newRepo(t) // a git repo; install-cli runs from here
	installCliEnv(t)

	out, code := runBin(t, bin, repo, "install-cli")
	if code != 0 {
		t.Fatalf("install-cli exit=%d out=%q", code, out)
	}
	root, err := filepath.EvalSymlinks(repo) // rev-parse resolves symlinks (macOS /var -> /private/var)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, ".claude", "commands", "sq.md"))
	if err != nil {
		t.Fatalf("/sq command not written: %v", err)
	}
	if !strings.Contains(string(b), "quest_new") || !strings.Contains(string(b), "$ARGUMENTS") {
		t.Errorf("installed sq.md missing expected content: %q", b)
	}
	if !strings.Contains(out, "/sq") {
		t.Errorf("output didn't mention the /sq command: %q", out)
	}
}

// TestInstallCliNeverClobbersExistingCommand (SQ-0107): an existing
// .claude/commands/sq.md (a user's own, or the make-dev symlink) is left intact.
func TestInstallCliNeverClobbersExistingCommand(t *testing.T) {
	bin := buildBinary(t)
	repo, _ := newRepo(t)
	installCliEnv(t)

	root, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".claude", "commands")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const sentinel = "MY OWN /sq COMMAND\n"
	if err := os.WriteFile(filepath.Join(dir, "sq.md"), []byte(sentinel), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, code := runBin(t, bin, repo, "install-cli"); code != 0 {
		t.Fatal("install-cli failed")
	}
	b, _ := os.ReadFile(filepath.Join(dir, "sq.md"))
	if string(b) != sentinel {
		t.Errorf("install-cli clobbered an existing sq.md: %q", b)
	}
}

// TestInstallCliRefreshesMarkedCommand (SQ-0107): an older copy that carries our
// managed marker is refreshed to the current version on re-run.
func TestInstallCliRefreshesMarkedCommand(t *testing.T) {
	bin := buildBinary(t)
	repo, _ := newRepo(t)
	installCliEnv(t)

	root, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".claude", "commands")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A stale but marked copy — install-cli should overwrite it.
	stale := "---\nname: side-quest\n---\n<!-- " + commands.ManagedMarker + " -->\nOLD STALE BODY\n"
	if err := os.WriteFile(filepath.Join(dir, "sq.md"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, code := runBin(t, bin, repo, "install-cli"); code != 0 {
		t.Fatal("install-cli failed")
	}
	b, _ := os.ReadFile(filepath.Join(dir, "sq.md"))
	if string(b) != commands.Sq {
		t.Errorf("marked command not refreshed to current version; got:\n%s", b)
	}
}

// uninstall-cli is wired into run() and removes a marked launcher end-to-end.
func TestUninstallCliCommand(t *testing.T) {
	bin := buildBinary(t)
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(dir, "side-quest")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\n# "+cli.Marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir() reads USERPROFILE on Windows (SQ-0086)
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("PATH", dir)

	out, code := runBin(t, bin, home, "uninstall-cli")
	if code != 0 {
		t.Fatalf("uninstall-cli exit=%d out=%q", code, out)
	}
	if _, err := os.Stat(launcher); !os.IsNotExist(err) {
		t.Errorf("uninstall-cli did not remove the marked launcher (err=%v)", err)
	}
}
