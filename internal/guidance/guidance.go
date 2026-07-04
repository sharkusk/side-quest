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

//go:embed agents.md
var agentsRaw string

// Agents is the agent-agnostic guidance block that `onboard --agents-md` and
// `agents-md` emit — the UNWRAPPED template. The refresh markers are added at
// runtime by the emitter (cmd/side-quest/onboard.go), never stored here.
var Agents = agentsRaw
