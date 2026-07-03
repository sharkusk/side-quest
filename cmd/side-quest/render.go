// Rendering helpers for the human CLI: JSON emission and (added in later tasks)
// human-readable tables and detail views.
package main

import (
	"encoding/json"
	"io"
)

// emitJSON writes v as indented JSON followed by a newline. The value is a raw
// library struct (*quest.Quest, []*quest.Quest, config.Config) — the JSON shape
// is the struct shape, which the MCP layer will reuse.
func emitJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
