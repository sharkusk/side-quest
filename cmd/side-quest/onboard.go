package main

import (
	"fmt"

	sidequest "github.com/sharkusk/side-quest"
)

// cmdAgentsMd prints the canonical agent-guidance block — the repo's AGENTS.md,
// embedded in the binary — to stdout, ready to paste into a project's own
// AGENTS.md. It needs no repo and no init.
func cmdAgentsMd(args []string) error {
	if len(args) != 0 {
		return &usageErr{"agents-md takes no arguments"}
	}
	fmt.Print(sidequest.AgentsGuidance)
	return nil
}
