package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// launcherSrc is the POSIX launcher asset, exec'd directly to test its resolution.
func launcherSrc(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs("launcher.sh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("launcher.sh missing: %v", err)
	}
	return p
}

// runLauncher execs launcher.sh with a controlled HOME (and no CLAUDE_PLUGIN_DATA,
// as in a real terminal), returning combined output and the run error.
func runLauncher(t *testing.T, home string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(launcherSrc(t), args...)
	cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func writeExecFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func dataDir(home string) string {
	return filepath.Join(home, ".claude", "plugins", "data", "side-quest-side-quest")
}

// Case 1: newest provisioned binary in the data dir is exec'd.
func TestLauncherExecsProvisionedBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX launcher")
	}
	home := t.TempDir()
	bin := filepath.Join(dataDir(home), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecFile(t, filepath.Join(bin, "side-quest-9.9.9"), "#!/bin/sh\necho PROVISIONED \"$@\"\n")

	out, err := runLauncher(t, home, "serve")
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.HasPrefix(out, "PROVISIONED serve") {
		t.Errorf("got %q, want the provisioned binary", out)
	}
}

// Case 2: data dir present but no binary -> "open a Claude Code session", exit != 0.
func TestLauncherAsksToFinishSetup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX launcher")
	}
	home := t.TempDir()
	if err := os.MkdirAll(dataDir(home), 0o755); err != nil { // dir, but no bin/
		t.Fatal(err)
	}
	out, err := runLauncher(t, home, "list")
	if err == nil {
		t.Fatalf("expected non-zero exit, got success: %s", out)
	}
	if !strings.Contains(out, "open a Claude Code session") {
		t.Errorf("missing finish-setup notice: %s", out)
	}
}

// Case 3: data dir absent (plugin gone), non-interactive -> announce safe-to-remove.
func TestLauncherSelfRemovalNoticeWhenPluginGone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX launcher")
	}
	home := t.TempDir() // no .claude/... at all
	out, err := runLauncher(t, home, "list")
	if err == nil {
		t.Fatalf("expected non-zero exit, got success: %s", out)
	}
	if !strings.Contains(out, "safe to remove") {
		t.Errorf("missing self-removal notice: %s", out)
	}
}

// The Windows launcher asset carries the marker and the same two notices.
func TestWindowsLauncherAssetContent(t *testing.T) {
	b, err := os.ReadFile("launcher.cmd")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{Marker, "open a Claude Code session", "safe to remove"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("launcher.cmd missing %q", want)
		}
	}
}
