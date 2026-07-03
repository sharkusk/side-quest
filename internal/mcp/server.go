// Package mcp is the stdio MCP frontend: it exposes the quest store as MCP
// tools for any MCP-capable agent. Each tool decodes typed params, calls one
// store method, and returns the result as JSON. Validation lives in the store;
// bad input becomes an MCP tool error (a returned error), not a protocol error.
// Tool responses are neutral JSON — no voice/tone.
package mcp

import (
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sharkusk/side-quest/internal/store"
)

// Version is advertised to clients in the server implementation info.
const Version = "0.1.0"

// NewServer builds an MCP server exposing the quest tools backed by s.
func NewServer(s *store.Store) *sdk.Server {
	srv := sdk.NewServer(&sdk.Implementation{Name: "side-quest", Version: Version}, nil)
	(&handlers{store: s}).register(srv)
	return srv
}

// handlers holds the store the tool handlers act on.
type handlers struct{ store *store.Store }
