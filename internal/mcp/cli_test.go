package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

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
	t.Setenv("XDG_BIN_HOME", "")
	// dir leads PATH (so it's the chosen install dir) but the real PATH stays
	// appended after it, since newTestStore below still needs to exec git.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CLAUDE_PLUGIN_DATA", data)

	cs, ctx := dialTest(t, newTestStore(t))

	if st := callJSON(t, cs, ctx, "cli_status"); st["installed"] != false || st["offered"] != false {
		t.Fatalf("fresh status = %v, want installed/offered false", st)
	}

	in := callJSON(t, cs, ctx, "cli_install")
	if in["path"] == "" || in["path"] == nil {
		t.Fatalf("cli_install returned no path: %v", in)
	}
	if _, err := os.Stat(filepath.Join(dir, "side-quest")); err != nil {
		t.Fatalf("cli_install did not write the launcher: %v", err)
	}
	if _, err := os.Stat(filepath.Join(data, ".cli-offered")); err != nil {
		t.Fatalf("cli_install did not mark offered: %v", err)
	}

	if st := callJSON(t, cs, ctx, "cli_status"); st["installed"] != true || st["offered"] != true {
		t.Fatalf("post-install status = %v, want both true", st)
	}

	callJSON(t, cs, ctx, "cli_uninstall")
	if _, err := os.Stat(filepath.Join(dir, "side-quest")); !os.IsNotExist(err) {
		t.Fatalf("cli_uninstall did not remove the launcher (err=%v)", err)
	}
}

// cli_dismiss records a decline (writes the sentinel) so status reports offered.
func TestCliDismissMarksOffered(t *testing.T) {
	data := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_BIN_HOME", "")
	// Keep the real PATH appended so newTestStore below can still exec git.
	t.Setenv("PATH", t.TempDir()+string(os.PathListSeparator)+os.Getenv("PATH"))
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
