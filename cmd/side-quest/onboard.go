package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sharkusk/side-quest/internal/gitcmd"
	"github.com/sharkusk/side-quest/internal/guidance"
	"github.com/sharkusk/side-quest/internal/store"
)

const (
	// The emitted guidance is wrapped in these HTML-comment markers (invisible in
	// rendered Markdown) so a merged copy can be found and refreshed in place —
	// the same drift-defeating pattern as the git-hook blocks (SQ-0045). The
	// start/end markers are version-FREE so a block written by ANY version is
	// still located and replaced; the version lives on its own stamped line.
	agentsMarker        = "<!-- >>> side-quest >>> -->"
	agentsEndMarker     = "<!-- <<< side-quest <<< -->"
	agentsVersionPrefix = "<!-- side-quest-version: "
	agentsVersionSuffix = " -->"
)

// agentsBlock renders the marker-guarded, version-stamped guidance block.
func agentsBlock(version string) string {
	return agentsMarker + "\n" +
		agentsVersionPrefix + version + agentsVersionSuffix + "\n" +
		strings.TrimRight(guidance.Agents, "\n") + "\n" +
		agentsEndMarker + "\n"
}

// parseAgentsVersion returns the version stamped inside a guidance block, or ""
// when the block predates version stamping.
func parseAgentsVersion(block string) string {
	for _, line := range strings.Split(block, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), agentsVersionPrefix); ok {
			return strings.TrimSpace(strings.TrimSuffix(v, agentsVersionSuffix))
		}
	}
	return ""
}

// blockOutcome reports what installAgentsGuidance did to AGENTS.md.
type blockOutcome int

const (
	blockCreated   blockOutcome = iota // created AGENTS.md with our block
	blockAppended                      // appended our block to an existing AGENTS.md
	blockRefreshed                     // replaced our own (changed) block in place
	blockUnchanged                     // our block was already byte-identical
)

// installAgentsGuidance writes the guidance block into AGENTS.md at path,
// managing only our own marked block: it creates the file when absent, refreshes
// our block in place when present (never duplicating it), and otherwise appends
// to a user's existing file without disturbing their content. When it replaced
// an existing block it also reports the version that block was stamped with
// ("" if it predated stamping) (SQ-0047).
func installAgentsGuidance(path, version string) (blockOutcome, string, error) {
	block := agentsBlock(version)

	existing, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return 0, "", err
		}
		return blockCreated, "", os.WriteFile(path, []byte(block), 0o644)
	}

	text := string(existing)
	if i := strings.Index(text, agentsMarker); i >= 0 {
		if j := strings.Index(text, agentsEndMarker); j >= 0 {
			end := j + len(agentsEndMarker)
			if end < len(text) && text[end] == '\n' {
				end++
			}
			existingBlock := text[i:end]
			prev := parseAgentsVersion(existingBlock)
			if existingBlock == block {
				return blockUnchanged, prev, nil
			}
			return blockRefreshed, prev, os.WriteFile(path, []byte(text[:i]+block+text[end:]), 0o644)
		}
	}
	// No block yet: append after the user's content, separated by a blank line.
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return blockAppended, "", os.WriteFile(path, []byte(text+"\n"+block), 0o644)
}

// agentsGuidanceNote describes what installAgentsGuidance did, so onboard can
// tell the user whether it wrote, appended, or refreshed the block.
func agentsGuidanceNote(o blockOutcome, prev, version string) string {
	switch o {
	case blockCreated:
		return fmt.Sprintf("side-quest: wrote the agent guidance to AGENTS.md (v%s).", version)
	case blockAppended:
		return fmt.Sprintf("side-quest: appended the side-quest guidance block to your existing AGENTS.md (v%s).", version)
	case blockRefreshed:
		switch {
		case prev == "":
			return fmt.Sprintf("side-quest: refreshed the AGENTS.md guidance to v%s (it predated version stamping).", version)
		case prev != version:
			return fmt.Sprintf("side-quest: refreshed the AGENTS.md guidance (v%s → v%s).", prev, version)
		default:
			return fmt.Sprintf("side-quest: refreshed the AGENTS.md guidance (v%s; content changed).", version)
		}
	default:
		return fmt.Sprintf("side-quest: AGENTS.md guidance already current (v%s).", version)
	}
}

// cmdAgentsMd prints the canonical agent-guidance block — the repo's AGENTS.md,
// embedded in the binary, wrapped in refresh markers and version-stamped — to
// stdout, ready to paste into a project's own AGENTS.md. It needs no repo and no
// init.
func cmdAgentsMd(args []string) error {
	if len(args) != 0 {
		return &usageErr{"agents-md takes no arguments"}
	}
	fmt.Print(agentsBlock(version))
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
// install the git hooks, write a project .mcp.json if absent (skipped when the
// plugin is active), then print the
// guidance to paste into AGENTS.md. It is safe to re-run — an existing ref,
// existing hooks, and an existing .mcp.json are each left as they are.
func cmdOnboard(args []string) error {
	fs := newFlagSet("onboard")
	var withAgents bool
	fs.BoolVar(&withAgents, "agents-md", false, "also merge the side-quest guidance block into the project's AGENTS.md")
	setUsage(fs, "usage: side-quest onboard [--agents-md]\nper-repo setup: create the quest ref, install hooks, write .mcp.json (add --agents-md to also merge AGENTS.md guidance)")
	rest, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return &usageErr{"onboard takes no positional arguments"}
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

	// 3. project .mcp.json, at the repo root, only if absent — but never when the
	// plugin is active, since it already registers the side-quest MCP server; a
	// second identically-named one would be redundant. We skip silently: .mcp.json
	// is internal plumbing an end user need not hear about (SQ-0064, D6).
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	top, err := gitcmd.New(cwd).Run("rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	if !pluginActive() {
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
	}

	// 4. AGENTS.md guidance is opt-in reinforcement now — only with --agents-md
	// (SQ-0051). Guidance rides the MCP server by default; when requested, the
	// marked block still refreshes in place so a directive change can be pulled in
	// on re-run rather than left silently stale in a hand-merged copy (SQ-0047).
	if withAgents {
		agentsPath := filepath.Join(top, "AGENTS.md")
		outcome, prev, err := installAgentsGuidance(agentsPath, version)
		if err != nil {
			return err
		}
		fmt.Println(agentsGuidanceNote(outcome, prev, version))
	}
	fmt.Println("Then restart your agent session so the MCP server loads its guidance.")
	return nil
}
