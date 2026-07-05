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
	return nil
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
