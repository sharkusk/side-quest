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
