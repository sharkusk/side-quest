package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sharkusk/side-quest/commands"
	"github.com/sharkusk/side-quest/internal/cli"
	"github.com/sharkusk/side-quest/internal/gitcmd"
)

// cmdInstallCli writes the read-only launcher onto the user's PATH so a plugin
// user can run side-quest from the terminal (SQ-0065). The mechanism lives in
// internal/cli (shared with the MCP cli_install tool); this wrapper just formats
// the result for the terminal.
func cmdInstallCli(args []string) error {
	if len(args) != 0 {
		return &usageErr{"install-cli takes no arguments"}
	}
	r, err := cli.Install()
	if err != nil {
		return err
	}
	fmt.Printf("side-quest: installed the CLI launcher at %s\n", r.Path)
	if !r.OnPath {
		fmt.Printf("  Add %s to your PATH to run `side-quest` from the terminal.\n", r.Dir)
	}
	installSqCommand()
	return nil
}

// installSqCommand drops the /sq slash command into the current repo's
// .claude/commands so a plugin user gets a bare `/sq` — Claude Code namespaces
// the plugin's own copy as /side-quest:sq (SQ-0107). Project-level and
// best-effort: it never fails install-cli, never clobbers an existing file, and
// skips with a note when not run inside a git repo.
func installSqCommand() {
	root, err := gitcmd.New(".").Run("rev-parse", "--show-toplevel")
	if err != nil {
		fmt.Println("  /sq command: skipped — run install-cli from inside your repo to install it project-level.")
		return
	}
	path := filepath.Join(root, ".claude", "commands", "sq.md")
	action := "installed"
	if existing, rerr := os.ReadFile(path); rerr == nil {
		switch {
		case !strings.Contains(string(existing), commands.ManagedMarker):
			fmt.Printf("  /sq command: left your customized %s as is.\n", path)
			return
		case string(existing) == commands.Sq:
			fmt.Printf("  /sq command already up to date at %s.\n", path)
			return
		default:
			action = "refreshed" // our marked copy, but an older version
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Printf("  /sq command: couldn't create .claude/commands (%v).\n", err)
		return
	}
	if err := os.WriteFile(path, []byte(commands.Sq), 0o644); err != nil {
		fmt.Printf("  /sq command: couldn't write it (%v).\n", err)
		return
	}
	fmt.Printf("  %s the /sq command at %s — usable as /sq (the plugin's own is /side-quest:sq).\n", action, path)
}

// cmdUninstallCli removes the marked launcher(s) while the plugin is still
// installed (the plugin-gone case is handled by the launcher's own self-removal
// — SQ-0065, D8). The mechanism is internal/cli.Uninstall; this wrapper formats.
func cmdUninstallCli(args []string) error {
	if len(args) != 0 {
		return &usageErr{"uninstall-cli takes no arguments"}
	}
	r, err := cli.Uninstall()
	if err != nil {
		return err
	}
	for _, p := range r.Removed {
		fmt.Printf("side-quest: removed the CLI launcher at %s\n", p)
	}
	for _, p := range r.Refused {
		fmt.Printf("side-quest: left %s in place — not one of our launchers (no %q marker).\n", p, cli.Marker)
	}
	if len(r.Removed) == 0 && len(r.Refused) == 0 {
		fmt.Println("side-quest: no CLI launcher found on PATH — nothing to remove.")
	}
	return nil
}
