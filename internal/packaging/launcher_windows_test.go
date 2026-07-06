//go:build windows

// Windows coverage for the provisioning hook (scripts/provision.ps1), the sibling of the
// POSIX scripts/provision.sh tested in launcher_test.go. It's run through PowerShell (as
// the plugin's SessionStart hook does); the stand-in "binary" inside the release zip is a
// real compiled .exe marker (a shell-script fake won't run here), and the environment is
// inherited-then-overridden so powershell.exe keeps the system dirs it needs.
package packaging

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// makeZip builds a .zip holding srcFile under the given archive name — the shape of a
// Windows side-quest release archive, whose side-quest.exe provision.ps1 extracts.
func makeZip(t *testing.T, name, srcFile string) []byte {
	t.Helper()
	data, err := os.ReadFile(srcFile)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// buildMarkerExe compiles a tiny program to path that prints "<marker> <args…>", so a
// test can tell the provisioned binary actually ran.
func buildMarkerExe(t *testing.T, path, marker string) {
	t.Helper()
	src := filepath.Join(t.TempDir(), "main.go")
	prog := "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\t\"strings\"\n)\n\n" +
		"func main() { fmt.Println(\"" + marker + " \" + strings.Join(os.Args[1:], \" \")) }\n"
	if err := os.WriteFile(src, []byte(prog), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("go", "build", "-o", path, src).CombinedOutput(); err != nil {
		t.Fatalf("build marker exe: %v\n%s", err, out)
	}
}

// withEnv returns base with each key in kv replaced (case-insensitively, as Windows
// treats env keys) or appended — so overriding PATH/CLAUDE_PLUGIN_DATA can't leave a
// stale duplicate the child would resolve ambiguously.
func withEnv(base []string, kv map[string]string) []string {
	drop := func(line string) bool {
		i := strings.IndexByte(line, '=')
		if i < 0 {
			return false
		}
		for k := range kv {
			if strings.EqualFold(line[:i], k) {
				return true
			}
		}
		return false
	}
	out := make([]string, 0, len(base)+len(kv))
	for _, line := range base {
		if !drop(line) {
			out = append(out, line)
		}
	}
	for k, v := range kv {
		out = append(out, k+"="+v)
	}
	return out
}

// stageProvisionPS copies scripts/provision.ps1 into a fresh plugin root stamped with the
// given VERSION (provision.ps1 reads VERSION from <root>/VERSION), returning its path.
func stageProvisionPS(t *testing.T, version string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte(version+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scripts := filepath.Join(root, "scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join("..", "..", "scripts", "provision.ps1"))
	if err != nil {
		t.Fatalf("read provision.ps1: %v", err)
	}
	p := filepath.Join(scripts, "provision.ps1")
	if err := os.WriteFile(p, src, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// runProvisionPS runs the hook through PowerShell exactly as plugin.json declares it.
func runProvisionPS(t *testing.T, script string, env []string) (string, error) {
	t.Helper()
	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// End to end: the hook downloads the windows_amd64.zip from SIDE_QUEST_RELEASE_BASE,
// verifies its SHA-256 against checksums.txt (pure .NET — this runner's PowerShell may
// lack New-TemporaryFile/Get-FileHash, SQ-0085), extracts side-quest.exe to the fixed
// data-dir path the MCP command spawns, and writes a version marker (SQ-0084/0089).
func TestWindowsProvisionDownloadsBinary(t *testing.T) {
	const ver = "9.9.9"
	asset := fmt.Sprintf("side-quest_%s_windows_amd64.zip", ver)
	exePath := filepath.Join(t.TempDir(), "side-quest.exe")
	buildMarkerExe(t, exePath, "DOWNLOADED")
	base := serveRelease(t, asset, makeZip(t, "side-quest.exe", exePath))

	data := t.TempDir()
	env := withEnv(os.Environ(), map[string]string{
		"CLAUDE_PLUGIN_DATA":      data,
		"SIDE_QUEST_RELEASE_BASE": base,
	})
	if out, err := runProvisionPS(t, stageProvisionPS(t, ver), env); err != nil {
		t.Fatalf("provision: %v\n%s", err, out)
	}

	target := filepath.Join(data, "bin", "side-quest.exe")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("binary not provisioned at %s: %v", target, err)
	}
	out, err := exec.Command(target, "serve").CombinedOutput()
	if err != nil {
		t.Fatalf("run provisioned binary: %v\n%s", err, out)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(out)), "DOWNLOADED serve") {
		t.Errorf("provisioned binary output = %q, want the archived binary", out)
	}
	marker, _ := os.ReadFile(filepath.Join(data, "bin", ".provisioned-version"))
	if strings.TrimSpace(string(marker)) != ver {
		t.Errorf("version marker = %q, want %q", marker, ver)
	}
}

// Idempotent: with the marker already at this VERSION the hook returns before any
// download. Proven by pointing at a dead base and asserting the pre-planted sentinel
// binary survives untouched.
func TestWindowsProvisionIdempotent(t *testing.T) {
	const ver = "9.9.9"
	data := t.TempDir()
	bin := filepath.Join(data, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(bin, "side-quest.exe")
	buildMarkerExe(t, target, "SENTINEL")
	if err := os.WriteFile(filepath.Join(bin, ".provisioned-version"), []byte(ver), 0o644); err != nil {
		t.Fatal(err)
	}

	env := withEnv(os.Environ(), map[string]string{
		"CLAUDE_PLUGIN_DATA":      data,
		"SIDE_QUEST_RELEASE_BASE": "http://127.0.0.1:1", // any download would fail
	})
	if out, err := runProvisionPS(t, stageProvisionPS(t, ver), env); err != nil {
		t.Fatalf("provision (idempotent): %v\n%s", err, out)
	}
	out, err := exec.Command(target, "x").CombinedOutput()
	if err != nil || !strings.HasPrefix(strings.TrimSpace(string(out)), "SENTINEL") {
		t.Errorf("provision overwrote an already-current binary; got %q (err %v)", out, err)
	}
}

// A dev checkout (VERSION=dev) has no release to download and must no-op cleanly — exit 0,
// no binary.
func TestWindowsProvisionDevIsNoop(t *testing.T) {
	data := t.TempDir()
	env := withEnv(os.Environ(), map[string]string{"CLAUDE_PLUGIN_DATA": data})
	if out, err := runProvisionPS(t, stageProvisionPS(t, "dev"), env); err != nil {
		t.Fatalf("dev provision should exit 0: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(data, "bin", "side-quest.exe")); !os.IsNotExist(err) {
		t.Errorf("dev provision unexpectedly created a binary (err=%v)", err)
	}
}

// A failed download must be non-fatal (exit 0) and leave no partial binary, so a bad
// network or missing asset never blocks session start (SQ-0089).
func TestWindowsProvisionDownloadFailureIsNonFatal(t *testing.T) {
	data := t.TempDir()
	env := withEnv(os.Environ(), map[string]string{
		"CLAUDE_PLUGIN_DATA":      data,
		"SIDE_QUEST_RELEASE_BASE": "http://127.0.0.1:1", // nothing listening
	})
	if out, err := runProvisionPS(t, stageProvisionPS(t, "9.9.9"), env); err != nil {
		t.Fatalf("failed provision must still exit 0 (non-fatal): %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(data, "bin", "side-quest.exe")); !os.IsNotExist(err) {
		t.Errorf("failed provision left a binary behind (err=%v)", err)
	}
}
