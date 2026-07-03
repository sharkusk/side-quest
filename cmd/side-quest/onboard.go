package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	sidequest "github.com/sharkusk/side-quest"
	"github.com/sharkusk/side-quest/internal/gitcmd"
	"github.com/sharkusk/side-quest/internal/store"
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

// mcpJSON is the project MCP registration onboard writes when none exists. It
// uses the bare `side-quest` command, not an absolute path: .mcp.json is a
// committed, shared file, so a machine-specific path would break on clone.
const mcpJSON = `{
  "mcpServers": {
    "side-quest": {
      "command": "side-quest",
      "args": ["serve"]
    }
  }
}
`

// cmdOnboard runs the whole per-repo setup in one shot: create the quest ref,
// install the git hooks, write a project .mcp.json if absent, then print the
// guidance to paste into AGENTS.md. It is safe to re-run — an existing ref,
// existing hooks, and an existing .mcp.json are each left as they are.
func cmdOnboard(args []string) error {
	if len(args) != 0 {
		return &usageErr{"onboard takes no arguments"}
	}
	s, err := openStore()
	if err != nil {
		return err
	}

	// 1. quest ref
	switch err := s.Init(); {
	case err == nil:
		fmt.Println(voiceFor(s).Initialized())
		noticeRandomIDs(s) // clone→onboard usually has a remote, so ids default to random (SQ-0030)
	case errors.Is(err, store.ErrAlreadyInitialized):
		fmt.Println("side-quest: quest ref already present — leaving it as is.")
	default:
		return err
	}

	// 2. git hooks (idempotent)
	if err := cmdInstallHooks(nil); err != nil {
		return err
	}

	// 3. project .mcp.json, at the repo root, only if absent
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	top, err := gitcmd.New(cwd).Run("rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	mcpPath := filepath.Join(top, ".mcp.json")
	switch _, err := os.Stat(mcpPath); {
	case err == nil:
		fmt.Println("side-quest: .mcp.json already exists — leaving it as is.")
	case os.IsNotExist(err):
		if err := os.WriteFile(mcpPath, []byte(mcpJSON), 0o644); err != nil {
			return err
		}
		fmt.Println("side-quest: wrote .mcp.json (registers the side-quest MCP server).")
	default:
		return err
	}

	// 4. guidance to paste + restart reminder
	fmt.Print("\nAdd this to your AGENTS.md (merge into an existing one, don't overwrite):\n\n")
	fmt.Println(sidequest.AgentsGuidance)
	fmt.Println("Then restart your agent session so the MCP server and AGENTS.md load.")
	return nil
}
