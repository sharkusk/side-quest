package packaging

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func launcherPath(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs("../../bin/side-quest")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("launcher missing: %v", err)
	}
	return p
}

// writeExec writes an executable shell script.
func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// cleanEnv isolates the launcher from the developer's real PATH/HOME so that a
// real `side-quest` on the machine cannot leak into the resolution.
func cleanEnv(pathDir, pluginData string) []string {
	return []string{
		"PATH=" + pathDir + ":/usr/bin:/bin",
		"CLAUDE_PLUGIN_DATA=" + pluginData,
		"HOME=" + pluginData,
	}
}

// Step 1 of the chain: a cached binary for this VERSION is exec'd.
func TestLauncherExecsCachedBinary(t *testing.T) {
	ver := strings.TrimSpace(string(repoFile(t, "VERSION")))
	data := t.TempDir()
	cache := filepath.Join(data, "bin")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExec(t, filepath.Join(cache, "side-quest-"+ver), "#!/bin/sh\necho CACHED \"$@\"\n")

	cmd := exec.Command(launcherPath(t), "serve")
	cmd.Env = cleanEnv(t.TempDir(), data)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.HasPrefix(string(out), "CACHED serve") {
		t.Errorf("got %q, want the cached binary", out)
	}
}

// Step 2 of the chain: a side-quest already on PATH (dev build) is exec'd.
func TestLauncherExecsPathBinary(t *testing.T) {
	shim := t.TempDir()
	writeExec(t, filepath.Join(shim, "side-quest"), "#!/bin/sh\necho PATHBIN \"$@\"\n")

	cmd := exec.Command(launcherPath(t), "serve")
	cmd.Env = cleanEnv(shim, t.TempDir()) // empty plugin-data dir => no cache hit
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.HasPrefix(string(out), "PATHBIN serve") {
		t.Errorf("got %q, want the PATH binary", out)
	}
}

// Step 4 of the chain: nothing resolves and download is disabled (VERSION=dev),
// so the launcher prints the install hint and exits non-zero.
func TestLauncherFailsWithHint(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte("dev\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(launcherPath(t))
	if err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(root, "bin", "side-quest")
	if err := os.WriteFile(fake, src, 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(fake, "serve")
	cmd.Env = cleanEnv(t.TempDir(), t.TempDir())
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit, got success: %s", out)
	}
	if !strings.Contains(string(out), "go install github.com/sharkusk/side-quest/cmd/side-quest@latest") {
		t.Errorf("missing install hint: %s", out)
	}
}

// Regression test: a relative $0 (the real shipped deployment, e.g. `./side-quest`
// with the plugin's own bin/ on PATH) must resolve the step-2 self-check correctly
// (comparing an absolute SELF against found_abs) and terminate promptly at the
// install hint, rather than mis-exec'ing the wrong thing. The context timeout is
// a safety net, not a reproduction of an actual hang: POSIX shebang exec resolves
// $0 to an absolute path on each hop, so even the uncanonicalized pre-fix code
// self-heals after a couple of hops and terminates — it never hangs in practice.
func TestLauncherRelativeInvocationResolvesSelf(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte("dev\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(launcherPath(t))
	if err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(binDir, "side-quest")
	if err := os.WriteFile(fake, src, 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "./side-quest", "serve")
	cmd.Dir = binDir
	cmd.Env = []string{
		"PATH=" + binDir + ":/usr/bin:/bin",
		"CLAUDE_PLUGIN_DATA=" + t.TempDir(),
		"HOME=" + t.TempDir(),
	}
	out, err := cmd.CombinedOutput()

	if ctx.Err() != nil {
		t.Fatalf("launcher hit context deadline (did not terminate): %s", out)
	}
	if err == nil {
		t.Fatalf("expected non-zero exit, got success: %s", out)
	}
	if !strings.Contains(string(out), "go install github.com/sharkusk/side-quest/cmd/side-quest@latest") {
		t.Errorf("missing install hint: %s", out)
	}
}
