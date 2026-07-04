// Package capture builds the "mechanical context" recorded on a quest at
// creation: the git branch, short HEAD, cwd, and the worktree's current quest.
// It is best-effort — any piece that can't be read is simply omitted, and it
// never returns an error — so a create is never blocked by a missing git state.
package capture

import (
	"fmt"
	"strings"

	"github.com/sharkusk/side-quest/internal/gitcmd"
)

// Mechanical returns a few greppable labeled lines describing the worktree at
// dir. currentQuest, when non-empty, is included as the active-quest line.
func Mechanical(dir, currentQuest string) string {
	g := gitcmd.New(dir)
	var b strings.Builder
	if branch, err := g.Run("rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		fmt.Fprintf(&b, "branch: %s\n", strings.TrimSpace(branch))
	}
	if head, err := g.Run("rev-parse", "--short", "HEAD"); err == nil {
		fmt.Fprintf(&b, "head: %s\n", strings.TrimSpace(head))
	}
	fmt.Fprintf(&b, "cwd: %s\n", dir)
	if currentQuest != "" {
		fmt.Fprintf(&b, "current: %s\n", currentQuest)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Body composes the context stored on a new quest: the mechanical capture for
// the worktree at dir, followed by the user's own context note. Either may be
// empty (the mechanical part is empty only outside a readable cwd). It is the
// single shape shared by the CLI and MCP create paths, so both record the same
// provenance.
func Body(dir, currentQuest, userContext string) string {
	var parts []string
	if mech := Mechanical(dir, currentQuest); mech != "" {
		parts = append(parts, mech)
	}
	if userContext != "" {
		parts = append(parts, userContext)
	}
	return strings.Join(parts, "\n\n")
}
