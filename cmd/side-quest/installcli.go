package main

import (
	"fmt"

	"github.com/sharkusk/side-quest/internal/cli"
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
// the plugin's own copy as /side-quest:sq (SQ-0107). The mechanism is shared with
// the MCP cli_install tool (cli.InstallCommand); this formats the outcome for the
// terminal. Best-effort: it never fails install-cli.
func installSqCommand() {
	res, err := cli.InstallCommand(".")
	switch {
	case err != nil:
		fmt.Printf("  /sq command: couldn't install it (%v).\n", err)
	case res.Outcome == cli.CmdSkippedNoRepo:
		fmt.Println("  /sq command: skipped — run install-cli from inside your repo to install it project-level.")
	case res.Outcome == cli.CmdLeftCustom:
		fmt.Printf("  /sq command: left your customized %s as is.\n", res.Path)
	case res.Outcome == cli.CmdUpToDate:
		fmt.Printf("  /sq command already up to date at %s.\n", res.Path)
	default: // installed or refreshed
		fmt.Printf("  %s the /sq command at %s — usable as /sq (the plugin's own is /side-quest:sq).\n", res.Outcome, res.Path)
	}
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
