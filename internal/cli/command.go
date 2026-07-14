package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sharkusk/side-quest/commands"
	"github.com/sharkusk/side-quest/internal/gitcmd"
)

// Outcomes reported by InstallCommand.
const (
	CmdInstalled     = "installed"       // written fresh (was absent)
	CmdRefreshed     = "refreshed"       // our marked copy, updated to the current version
	CmdUpToDate      = "up_to_date"      // our marked copy already matches
	CmdLeftCustom    = "left_custom"     // present without our marker — a user's own command
	CmdSkippedNoRepo = "skipped_no_repo" // not inside a git repo
)

// CommandResult reports what InstallCommand did.
type CommandResult struct {
	Outcome string
	Path    string // the .claude/commands/sq.md path (empty when skipped)
}

// InstallCommand writes the project-level /sq slash command into the git repo
// containing dir (<root>/.claude/commands/sq.md) so a plugin user gets a bare
// `/sq` — Claude Code namespaces the plugin's own copy as /side-quest:sq
// (SQ-0107/0108). It is shared by the `install-cli` subcommand and the MCP
// `cli_install` tool. Marker-based: written when absent, refreshed when the
// existing copy carries the managed marker (so updates propagate), and left
// untouched when it does not (a user's own command). Best-effort: outside a git
// repo it reports CmdSkippedNoRepo with no error.
func InstallCommand(dir string) (CommandResult, error) {
	root, err := gitcmd.New(dir).Run("rev-parse", "--show-toplevel")
	if err != nil {
		return CommandResult{Outcome: CmdSkippedNoRepo}, nil
	}
	path := filepath.Join(root, ".claude", "commands", "sq.md")
	existing, rerr := os.ReadFile(path)
	if rerr != nil {
		if !os.IsNotExist(rerr) {
			// Any read failure other than not-exist means the marker cannot be
			// checked — writing anyway would clobber a file we can't prove is
			// ours (SQ-0122). Mirrors cli.Install's D8 refusal.
			return CommandResult{Path: path}, fmt.Errorf("cannot verify %s is our command (not overwriting): %w", path, rerr)
		}
		if err := writeCommandFile(path); err != nil {
			return CommandResult{Path: path}, err
		}
		return CommandResult{Outcome: CmdInstalled, Path: path}, nil
	}
	switch {
	case !strings.Contains(string(existing), commands.ManagedMarker):
		return CommandResult{Outcome: CmdLeftCustom, Path: path}, nil
	case string(existing) == commands.Sq:
		return CommandResult{Outcome: CmdUpToDate, Path: path}, nil
	}
	if err := writeCommandFile(path); err != nil {
		return CommandResult{Path: path}, err
	}
	return CommandResult{Outcome: CmdRefreshed, Path: path}, nil
}

func writeCommandFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(commands.Sq), 0o644)
}
