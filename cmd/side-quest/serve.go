package main

import (
	"context"
	"fmt"
	"os"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sharkusk/side-quest/internal/cli"
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
	// Self-heal a stale terminal launcher an older side-quest left on PATH, so an
	// upgrade doesn't strand the CLI (SQ-0091). Best-effort; stderr only, so the MCP
	// stdout protocol is untouched.
	for _, p := range cli.Refresh() {
		fmt.Fprintf(os.Stderr, "side-quest: refreshed stale CLI launcher at %s\n", p)
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	srv := questmcp.NewServer(s, version)
	return srv.Run(context.Background(), &sdk.StdioTransport{})
}
