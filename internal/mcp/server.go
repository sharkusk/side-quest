// Package mcp is the stdio MCP frontend: it exposes the quest store as MCP
// tools for any MCP-capable agent. Each tool decodes typed params, calls one
// store method, and returns the result as JSON. Validation lives in the store;
// bad input becomes an MCP tool error (a returned error), not a protocol error.
// The first content block is always neutral JSON so parsers can rely on it; a
// mutation MAY append a SECOND text block carrying a tone-flavored line for a
// human reader, gated on the on-ref tone (silent for plain). Reads never voice.
package mcp

import (
	"os"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sharkusk/side-quest/internal/guidance"
	"github.com/sharkusk/side-quest/internal/store"
)

// NewServer builds an MCP server exposing the quest tools backed by s. version is
// the caller's build version (main.version), advertised to clients in the server
// implementation info — so the MCP-advertised version tracks `side-quest version`
// and never drifts from a hardcoded constant (SQ-0044).
func NewServer(s *store.Store, version string) *sdk.Server {
	srv := sdk.NewServer(
		&sdk.Implementation{Name: "side-quest", Version: version},
		&sdk.ServerOptions{Instructions: instructions()},
	)
	(&handlers{store: s}).register(srv)
	return srv
}

// instructions is the server's initialize-time guidance: the cross-agent Core
// brief, plus the plugin lifecycle block when running under the Claude Code plugin
// (CLAUDE_PLUGIN_DATA is set) — where the cli_* tools are relevant (SQ-0066).
func instructions() string {
	if os.Getenv("CLAUDE_PLUGIN_DATA") != "" {
		return guidance.Core + "\n\n" + guidance.Plugin
	}
	return guidance.Core
}

// handlers holds the store the tool handlers act on.
type handlers struct{ store *store.Store }
