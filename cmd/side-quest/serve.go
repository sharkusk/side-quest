package main

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	questmcp "github.com/sharkusk/side-quest/internal/mcp"
)

// cmdServe runs the stdio MCP server until the client disconnects. It is a thin
// frontend: it opens the store for the cwd and hands it to the mcp package.
func cmdServe(args []string) error {
	if len(args) != 0 {
		return &usageErr{"serve takes no arguments"}
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	srv := questmcp.NewServer(s)
	return srv.Run(context.Background(), &sdk.StdioTransport{})
}
