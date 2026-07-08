package mcp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/sharkusk/side-quest/internal/cli"
)

// sandboxPath builds a PATH with only dir (the intended install dir) and the
// directory holding git, so newTestStore can still exec git while
// launcherDirs()'s PATH-side scan stays inside the sandbox. Appending the real
// ambient PATH (as an earlier draft did) would let Status/Uninstall reach — and
// cli_uninstall DELETE — a real marked launcher on the developer's own PATH.
func sandboxPath(t *testing.T, dir string) string {
	t.Helper()
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	return dir + string(os.PathListSeparator) + filepath.Dir(gitBin)
}

// callJSON calls a no-arg tool and unmarshals its neutral JSON content block.
func callJSON(t *testing.T, cs *sdk.ClientSession, ctx context.Context, name string) map[string]any {
	t.Helper()
	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("%s error: %s", name, contentText(t, res))
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(contentText(t, res)), &m); err != nil {
		t.Fatalf("%s: bad JSON: %v\n%s", name, err, contentText(t, res))
	}
	return m
}

// The lifecycle: fresh -> cli_install writes a launcher + marks offered -> status
// reflects both -> cli_uninstall removes it.
func TestCliToolsLifecycle(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir() reads USERPROFILE on Windows (SQ-0086)
	t.Setenv("XDG_BIN_HOME", "")
	// dir leads PATH (so it's the chosen install dir); only git's dir follows, so
	// the launcherDirs() scan can't reach a real launcher on the ambient PATH.
	t.Setenv("PATH", sandboxPath(t, dir))
	t.Setenv("CLAUDE_PLUGIN_DATA", data)

	// cli_install now also writes a project-level /sq command; run inside a
	// throwaway git repo so it never touches the real working tree (SQ-0108).
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	t.Chdir(repo)

	cs, ctx := dialTest(t, newTestStore(t))

	if st := callJSON(t, cs, ctx, "cli_status"); st["installed"] != false || st["offered"] != false {
		t.Fatalf("fresh status = %v, want installed/offered false", st)
	}

	in := callJSON(t, cs, ctx, "cli_install")
	if in["path"] == "" || in["path"] == nil {
		t.Fatalf("cli_install returned no path: %v", in)
	}
	if _, err := os.Stat(filepath.Join(dir, cli.LauncherName())); err != nil {
		t.Fatalf("cli_install did not write the launcher: %v", err)
	}
	if _, err := os.Stat(filepath.Join(data, ".cli-offered")); err != nil {
		t.Fatalf("cli_install did not mark offered: %v", err)
	}
	if in["sq_command"] != "installed" {
		t.Errorf("cli_install sq_command = %v, want installed", in["sq_command"])
	}
	if p, _ := in["sq_command_path"].(string); p == "" {
		t.Error("cli_install returned no sq_command_path")
	} else if _, err := os.Stat(p); err != nil {
		t.Errorf("cli_install did not write the /sq command: %v", err)
	}

	if st := callJSON(t, cs, ctx, "cli_status"); st["installed"] != true || st["offered"] != true {
		t.Fatalf("post-install status = %v, want both true", st)
	}

	callJSON(t, cs, ctx, "cli_uninstall")
	if _, err := os.Stat(filepath.Join(dir, cli.LauncherName())); !os.IsNotExist(err) {
		t.Fatalf("cli_uninstall did not remove the launcher (err=%v)", err)
	}
}

// cli_dismiss records a decline (writes the sentinel) so status reports offered.
func TestCliDismissMarksOffered(t *testing.T) {
	data := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir() reads USERPROFILE on Windows (SQ-0086)
	t.Setenv("XDG_BIN_HOME", "")
	// Only git's dir follows the sandbox dir on PATH — never the ambient PATH.
	t.Setenv("PATH", sandboxPath(t, t.TempDir()))
	t.Setenv("CLAUDE_PLUGIN_DATA", data)

	cs, ctx := dialTest(t, newTestStore(t))
	callJSON(t, cs, ctx, "cli_dismiss")
	if _, err := os.Stat(filepath.Join(data, ".cli-offered")); err != nil {
		t.Fatalf("cli_dismiss did not write the sentinel: %v", err)
	}
	if st := callJSON(t, cs, ctx, "cli_status"); st["offered"] != true {
		t.Fatalf("after dismiss, offered = %v, want true", st["offered"])
	}
}
