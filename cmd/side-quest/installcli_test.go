package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
