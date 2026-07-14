package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sharkusk/side-quest/internal/cli"
)

// offerSentinel is the path of the "CLI offer already made" marker, or "" when not
// running under the plugin (no CLAUDE_PLUGIN_DATA). It lives in the data dir so
// Claude deletes it on uninstall — a reinstall then re-offers (spec D5).
func offerSentinel() string {
	d := os.Getenv("CLAUDE_PLUGIN_DATA")
	if d == "" {
		return ""
	}
	return filepath.Join(d, ".cli-offered")
}

func offerMade() bool {
	p := offerSentinel()
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

func markOffered() {
	if p := offerSentinel(); p != "" {
		_ = os.WriteFile(p, nil, 0o644) // best-effort; the offer never blocks
	}
}

// cliStatus reports whether the terminal launcher is present and whether the
// one-time enable offer has already been made.
func (h *handlers) cliStatus(ctx context.Context, req *sdk.CallToolRequest, in emptyIn) (*sdk.CallToolResult, any, error) {
	st := cli.Status()
	return jsonResult(struct {
		Installed bool   `json:"installed"`
		Path      string `json:"path,omitempty"`
		Offered   bool   `json:"offered"`
	}{st.Installed, st.Path, offerMade()})
}

// cliInstall writes the launcher in-process, installs the project-level /sq
// command in the current repo (so a plugin user gets a bare /sq — the plugin's
// own is /side-quest:sq), and records that the offer was made (SQ-0108).
func (h *handlers) cliInstall(ctx context.Context, req *sdk.CallToolRequest, in emptyIn) (*sdk.CallToolResult, any, error) {
	r, err := cli.Install()
	if err != nil {
		return nil, nil, err
	}
	markOffered()
	// Best-effort: a /sq-command hiccup must not fail enabling the CLI. "." is the
	// server's cwd, i.e. the user's project.
	cmd, cerr := cli.InstallCommand(".")
	sqCommand := cmd.Outcome
	if cerr != nil {
		sqCommand = "error"
	}
	return jsonResult(struct {
		Path          string `json:"path"`
		Dir           string `json:"dir"`
		OnPath        bool   `json:"on_path"`
		SqCommand     string `json:"sq_command"`
		SqCommandPath string `json:"sq_command_path,omitempty"`
	}{r.Path, r.Dir, r.OnPath, sqCommand, cmd.Path})
}

// cliUninstall removes the marked launcher(s) this tool installed. A partial
// failure still reports what WAS removed — discarding that list would tell the
// agent nothing happened when some launchers are already gone (SQ-0122).
func (h *handlers) cliUninstall(ctx context.Context, req *sdk.CallToolRequest, in emptyIn) (*sdk.CallToolResult, any, error) {
	r, err := cli.Uninstall()
	if err != nil {
		if len(r.Removed) > 0 {
			return nil, nil, fmt.Errorf("%v (already removed: %s)", err, strings.Join(r.Removed, ", "))
		}
		return nil, nil, err
	}
	return jsonResult(struct {
		Removed []string `json:"removed"`
		Refused []string `json:"refused"`
	}{r.Removed, r.Refused})
}

// cliDismiss records that the user declined the offer, so it is not repeated.
func (h *handlers) cliDismiss(ctx context.Context, req *sdk.CallToolRequest, in emptyIn) (*sdk.CallToolResult, any, error) {
	markOffered()
	return jsonResult(struct {
		Offered bool `json:"offered"`
	}{true})
}
