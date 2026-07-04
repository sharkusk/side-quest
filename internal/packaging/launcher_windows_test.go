//go:build windows

// Windows coverage for the .cmd launcher (bin/side-quest.cmd), the sibling of the
// POSIX bin/side-quest tested in launcher_test.go. A batch file can't be exec'd
// directly, so we invoke it through `cmd /c`; the stand-in "binaries" are real
// compiled .exe markers (a shell-script fake won't run here), and the environment
// is inherited-then-overridden so cmd.exe/where.exe keep the system dirs they need.
package packaging

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// cmdLauncherPath returns the absolute path to the Windows .cmd launcher.
func cmdLauncherPath(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "bin", "side-quest.cmd"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("windows launcher missing: %v", err)
	}
	return p
}

// buildMarkerExe compiles a tiny program to path that prints "<marker> <args…>", so
// a launcher test can tell which resolution branch actually ran the binary.
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

// runCmdLauncher invokes launcher through cmd.exe (a .cmd can't be exec'd directly)
// with the given environment, returning combined output and the run error.
func runCmdLauncher(t *testing.T, launcher string, env []string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("cmd", append([]string{"/c", launcher}, args...)...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// stageLauncher copies the .cmd launcher into a fresh <root>/bin and writes
// <root>/VERSION, so a test can drive it at a chosen version (e.g. "dev" to reach
// the hint branch without triggering a real download).
func stageLauncher(t *testing.T, version string) string {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(cmdLauncherPath(t))
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(bin, "side-quest.cmd")
	if err := os.WriteFile(dst, src, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte(version+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

// Step 1: a cached binary for this VERSION is run.
func TestWindowsLauncherRunsCachedBinary(t *testing.T) {
	ver := strings.TrimSpace(string(repoFile(t, "VERSION")))
	data := t.TempDir()
	cacheBin := filepath.Join(data, "bin")
	if err := os.MkdirAll(cacheBin, 0o755); err != nil {
		t.Fatal(err)
	}
	buildMarkerExe(t, filepath.Join(cacheBin, "side-quest-"+ver+".exe"), "CACHED")

	out, err := runCmdLauncher(t, cmdLauncherPath(t),
		withEnv(os.Environ(), map[string]string{"CLAUDE_PLUGIN_DATA": data}),
		"serve")
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "CACHED serve") {
		t.Errorf("got %q, want the cached binary", out)
	}
}

// Step 2: a side-quest.exe already on PATH (dev build) is run.
func TestWindowsLauncherRunsPathBinary(t *testing.T) {
	pathDir := t.TempDir()
	buildMarkerExe(t, filepath.Join(pathDir, "side-quest.exe"), "PATHBIN")

	out, err := runCmdLauncher(t, cmdLauncherPath(t),
		withEnv(os.Environ(), map[string]string{
			"CLAUDE_PLUGIN_DATA": t.TempDir(), // empty -> no cache hit
			"PATH":               pathDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		}),
		"serve")
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "PATHBIN serve") {
		t.Errorf("got %q, want the PATH binary", out)
	}
}

// Step 4: nothing resolves and download is disabled (VERSION=dev), so the launcher
// prints the install hint and exits non-zero.
func TestWindowsLauncherFailsWithHint(t *testing.T) {
	launcher := stageLauncher(t, "dev")

	out, err := runCmdLauncher(t, launcher,
		withEnv(os.Environ(), map[string]string{"CLAUDE_PLUGIN_DATA": t.TempDir()}),
		"serve")
	if err == nil {
		t.Fatalf("expected non-zero exit, got success: %s", out)
	}
	if !strings.Contains(out, "go install github.com/sharkusk/side-quest/cmd/side-quest@latest") {
		t.Errorf("missing install hint: %s", out)
	}
}
