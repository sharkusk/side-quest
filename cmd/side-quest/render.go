// Rendering helpers for the human CLI: JSON emission and (added in later tasks)
// human-readable tables and detail views.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/sharkusk/side-quest/internal/quest"
)

// emitJSON writes v as indented JSON followed by a newline. The value is a raw
// library struct (*quest.Quest, []*quest.Quest, config.Config) — the JSON shape
// is the struct shape, which the MCP layer will reuse.
func emitJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// renderList prints an aligned table of quests, or a friendly line when empty.
func renderList(w io.Writer, quests []*quest.Quest) {
	if len(quests) == 0 {
		fmt.Fprintln(w, "no quests")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSTATUS\tTYPE\tPRIORITY\tTITLE")
	for _, q := range quests {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", q.ID, q.Status, q.Type, q.Priority, q.Title)
	}
	tw.Flush()
}

// renderShow prints one quest's frontmatter fields, then a blank line and the
// body. Absent optional fields (completed, commits, context, tags, body) are
// omitted.
func renderShow(w io.Writer, q *quest.Quest) {
	fmt.Fprintf(w, "id:        %s\n", q.ID)
	fmt.Fprintf(w, "title:     %s\n", q.Title)
	fmt.Fprintf(w, "status:    %s\n", q.Status)
	fmt.Fprintf(w, "type:      %s\n", q.Type)
	fmt.Fprintf(w, "priority:  %s\n", q.Priority)
	fmt.Fprintf(w, "created:   %s\n", q.Created.Format(time.RFC3339))
	if q.Completed != nil {
		fmt.Fprintf(w, "completed: %s\n", q.Completed.Format(time.RFC3339))
	}
	if len(q.Commits) > 0 {
		fmt.Fprintf(w, "commits:   %s\n", strings.Join(q.Commits, ", "))
	}
	if q.Context != "" {
		fmt.Fprintf(w, "context:   %s\n", q.Context)
	}
	if len(q.Tags) > 0 {
		keys := make([]string, 0, len(q.Tags))
		for k := range q.Tags {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(w, "tag:       %s=%s\n", k, q.Tags[k])
		}
	}
	if q.Body != "" {
		fmt.Fprintf(w, "\n%s\n", strings.TrimRight(q.Body, "\n"))
	}
}
