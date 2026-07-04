package main

import (
	"context"
	"fmt"
	"os"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	questmcp "github.com/sharkusk/side-quest/internal/mcp"
)

// cmdServe runs the stdio MCP server until the client disconnects. It is a thin
// frontend: it opens the store for the cwd and hands it to the mcp package.
func cmdServe(args []string) error {
	if len(args) != 0 {
		return &usageErr{"serve takes no arguments"}
	}
	// Warn (on stderr, never fatally) if the side-quest on PATH — used by git
	// hooks and the human CLI — is a different build than this server (SQ-0039).
	if msg := pathBinaryDrift(version); msg != "" {
		fmt.Fprintln(os.Stderr, msg)
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	srv := questmcp.NewServer(s)
	return srv.Run(context.Background(), &sdk.StdioTransport{})
}
