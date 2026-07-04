// Package guidance holds side-quest's canonical agent guidance. Core is the
// single source of truth for the compact behavioral brief: the MCP server sends
// it verbatim as its initialize-time instructions (internal/mcp), and the
// reinforcement surfaces (AGENTS.md, skills/side-quest/SKILL.md) must contain it,
// drift-guarded by a test in internal/packaging.
package guidance

import (
	_ "embed"
	"strings"
)

//go:embed core.md
var coreRaw string

// Core is the canonical core guidance brief, trimmed of surrounding whitespace.
var Core = strings.TrimSpace(coreRaw)
