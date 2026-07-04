// Rendering helpers for the human CLI: JSON emission, human-readable tables,
// and detail views.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"golang.org/x/term"

	"github.com/sharkusk/side-quest/internal/config"
	"github.com/sharkusk/side-quest/internal/quest"
	"github.com/sharkusk/side-quest/internal/voice"
)

// terminalWidth returns the column count of f's terminal, or 0 when f is not a
// terminal (piped or redirected output) or the size cannot be determined. A 0
// return is the signal callers use to skip word-wrapping.
func terminalWidth(f *os.File) int {
	w, _, err := term.GetSize(int(f.Fd()))
	if err != nil {
		return 0
	}
	return w
}

// emitJSON writes v as indented JSON followed by a newline. The value is a raw
// library struct (*quest.Quest, []*quest.Quest, config.Config) — the JSON shape
// is the struct shape, which the MCP layer will reuse.
func emitJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// renderList prints an aligned table of quests, or a friendly line when empty.
func renderList(w io.Writer, quests []*quest.Quest, v *voice.Voice) {
	if len(quests) == 0 {
		fmt.Fprintln(w, v.EmptyList())
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSTATUS\tTYPE\tPRIORITY\tTITLE")
	for _, q := range quests {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", q.ID, q.Status, q.Type, q.Priority, q.Title)
	}
	tw.Flush()
}

// showLabelPad left-justifies a field label (with its trailing colon) so every
// value starts at the same column (11). Kept as one constant so the label
// printer and the continuation-indent stay in lockstep.
const showLabelPad = 10

// showField prints one "label: value" line, word-wrapping value to width when
// width > 0. Continuation lines are indented to the value column so the field
// reads as a hanging block. A width <= 0 (piped output, or --no-wrap) prints the
// value verbatim, preserving the original single-line layout.
func showField(w io.Writer, label, value string, width int) {
	prefix := fmt.Sprintf("%-*s ", showLabelPad, label+":") // value starts at col 11
	lines := wrapText(value, width-len(prefix))
	fmt.Fprintf(w, "%s%s\n", prefix, lines[0])
	indent := strings.Repeat(" ", len(prefix))
	for _, ln := range lines[1:] {
		fmt.Fprintf(w, "%s%s\n", indent, ln)
	}
}

// renderShow prints one quest's frontmatter fields, then a blank line and the
// body. Absent optional fields (completed, commits, context, tags, body) are
// omitted. When width > 0, long values and body lines are word-wrapped to that
// terminal width; width <= 0 prints everything unwrapped.
// commitLine is one linked commit resolved for display: short is the abbreviated
// sha (or the stored sha when the commit is gone), and text is the subject line
// (default) or the complete message (--full). A missing commit has
// text == "(message unavailable)".
type commitLine struct {
	short, text string
}

func renderShow(w io.Writer, q *quest.Quest, width int, commits []commitLine) {
	showField(w, "id", q.ID, width)
	showField(w, "title", q.Title, width)
	showField(w, "status", string(q.Status), width)
	showField(w, "type", string(q.Type), width)
	showField(w, "priority", string(q.Priority), width)
	showField(w, "created", q.Created.Format(time.RFC3339), width)
	if q.Completed != nil {
		showField(w, "completed", q.Completed.Format(time.RFC3339), width)
	}
	if len(commits) > 0 {
		// Render like any label: value field — the first commit shares the
		// "commits:" line and every later line hangs at the value column, with
		// long subjects/body lines word-wrapped to that column just like context.
		label := fmt.Sprintf("%-*s ", showLabelPad, "commits:")
		indent := strings.Repeat(" ", len(label))
		first := true
		emit := func(pad, text string) {
			for _, ln := range wrapText(text, width-len(pad)) {
				lead := pad
				if first {
					lead, first = label, false
				}
				fmt.Fprintf(w, "%s%s\n", lead, ln)
			}
		}
		for _, c := range commits {
			subject, rest, _ := strings.Cut(c.text, "\n")
			emit(indent, c.short+"  "+subject)
			if body := strings.Trim(rest, "\n"); body != "" {
				fmt.Fprintln(w)
				for _, bl := range strings.Split(body, "\n") {
					if bl == "" {
						fmt.Fprintln(w)
						continue
					}
					emit(indent+"  ", bl)
				}
				fmt.Fprintln(w)
			}
		}
	}
	if len(q.Tags) > 0 {
		keys := make([]string, 0, len(q.Tags))
		for k := range q.Tags {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			showField(w, "tag", k+"="+q.Tags[k], width)
		}
	}
	if q.Context != "" {
		showField(w, "context", q.Context, width)
	}
	if q.Body != "" {
		fmt.Fprintln(w)
		for _, line := range strings.Split(strings.TrimRight(q.Body, "\n"), "\n") {
			// Wrap each body line independently so note markers and blank lines
			// stay put while long prose lines fold to the terminal width.
			for _, wl := range wrapText(line, width) {
				fmt.Fprintln(w, wl)
			}
		}
	}
}

// wrapText greedily wraps text to at most width columns, breaking only at
// spaces so a long token (a commit SHA, a URL) overflows its line intact rather
// than being split mid-word. A width <= 0 disables wrapping and returns the
// text unchanged as a single line.
func wrapText(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{text}
	}
	var lines []string
	cur := words[0]
	for _, w := range words[1:] {
		if len(cur)+1+len(w) <= width {
			cur += " " + w
		} else {
			lines = append(lines, cur)
			cur = w
		}
	}
	return append(lines, cur)
}

// renderConfig prints the effective on-ref configuration as aligned key: value
// lines.
func renderConfig(w io.Writer, c config.Config) {
	fmt.Fprintf(w, "id_prefix:     %s\n", c.IDPrefix)
	fmt.Fprintf(w, "id_strategy:   %s\n", c.IDStrategy)
	fmt.Fprintf(w, "seq_next:      %d\n", c.SeqNext)
	fmt.Fprintf(w, "seq_width:     %d\n", c.SeqWidth)
	fmt.Fprintf(w, "tone:          %s\n", c.Tone)
	fmt.Fprintf(w, "auto_trailer:  %t\n", c.AutoTrailer)
	fmt.Fprintf(w, "require_quest: %t\n", c.RequireQuest)
}
