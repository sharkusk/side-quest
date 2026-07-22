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
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/sharkusk/side-quest/internal/brief"
	"github.com/sharkusk/side-quest/internal/config"
	"github.com/sharkusk/side-quest/internal/quest"
	"github.com/sharkusk/side-quest/internal/store"
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
// When width > 0, long titles word-wrap to that terminal width with each
// continuation line hung under the TITLE column; width <= 0 (piped output or
// --no-wrap) prints one line per quest, keeping scripted output stable.
func renderList(w io.Writer, quests []*quest.Quest, v *voice.Voice, width int, showTags []string) {
	if len(quests) == 0 {
		fmt.Fprintln(w, v.EmptyList())
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	headers := listHeaders(showTags)
	fmt.Fprintln(tw, strings.Join(headers, "\t")+"\tTITLE")
	// Continuation lines are emitted as rows with empty leading cells, which
	// tabwriter pads to the TITLE column for us — so a wrapped title hangs under
	// the column instead of running off the terminal.
	titleCol := listTitleColumn(quests, showTags)
	blank := strings.Repeat("\t", len(headers)) // leading empty cells for a continuation row
	for _, q := range quests {
		lines := []string{q.Title}
		if width > 0 {
			lines = wrapText(q.Title, width-titleCol)
		}
		fmt.Fprintf(tw, "%s\t%s\n", strings.Join(listLeadingCells(q, showTags), "\t"), lines[0])
		for _, cont := range lines[1:] {
			fmt.Fprintf(tw, "%s%s\n", blank, cont)
		}
	}
	tw.Flush()
}

// listHeaders returns the header cells left of TITLE: the four fixed columns
// plus one per --show-tag key (uppercased to match the fixed labels).
func listHeaders(showTags []string) []string {
	h := []string{"ID", "STATUS", "TYPE", "PRIORITY"}
	for _, k := range showTags {
		h = append(h, strings.ToUpper(k))
	}
	return h
}

// listLeadingCells returns a quest's cell values left of TITLE, aligned with
// listHeaders: the four fixed fields plus each requested tag's value (an empty
// cell when the quest lacks that tag).
func listLeadingCells(q *quest.Quest, showTags []string) []string {
	cells := []string{q.ID, string(q.Status), string(q.Type), string(q.Priority)}
	for _, k := range showTags {
		cells = append(cells, q.Tags[k])
	}
	return cells
}

// listTitleColumn reports the terminal column where renderList's TITLE cell
// begins, matching tabwriter's layout (minwidth 0, padding 2): the sum over the
// leading columns (fixed + any --show-tag columns) of their widest cell plus the
// 2-space padding.
func listTitleColumn(quests []*quest.Quest, showTags []string) int {
	headers := listHeaders(showTags)
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, q := range quests {
		for i, cell := range listLeadingCells(q, showTags) {
			// Rune count, matching tabwriter's own cell measurement (it counts
			// runes too), so non-ASCII tag values don't skew the column (SQ-0123).
			if n := utf8.RuneCountInString(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}
	col := 0
	for _, wd := range widths {
		col += wd + 2 // tabwriter padding
	}
	return col
}

// showLabelPad left-justifies a field label (with its trailing colon) so every
// value starts at the same column (11). Kept as one constant so the label
// printer and the continuation-indent stay in lockstep.
const showLabelPad = 10

// showField prints one "label: value" line, word-wrapping value to width when
// width > 0. Continuation lines are indented to the value column so the field
// reads as a hanging block. A value carrying embedded newlines keeps them: each
// physical line is wrapped on its own, because handing the whole value to
// wrapText flattens its newlines into spaces (strings.Fields splits on them and
// the words rejoin with a space), running a captured branch/head/cwd block
// together into one line (SQ-0127). A width <= 0 (piped output, or --no-wrap)
// skips the word-wrapping, but still lays the value out as a hanging block.
func showField(w io.Writer, label, value string, width int) {
	prefix := fmt.Sprintf("%-*s ", showLabelPad, label+":") // value starts at col 11
	var lines []string
	for _, para := range strings.Split(value, "\n") {
		if para == "" {
			lines = append(lines, "") // a blank separator line, kept as-is
			continue
		}
		lines = append(lines, wrapText(para, width-len(prefix))...)
	}
	fmt.Fprintf(w, "%s%s\n", prefix, lines[0])
	indent := strings.Repeat(" ", len(prefix))
	for _, ln := range lines[1:] {
		if ln == "" {
			fmt.Fprintln(w) // no trailing indent on a blank line
			continue
		}
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

// renderHistory prints a quest's change log beneath its detail view: one line per
// commit, oldest first, with the date, short sha, author, and what changed (the
// changes within a commit joined by "; ").
func renderHistory(w io.Writer, entries []store.HistoryEntry) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "history (oldest first):")
	if len(entries) == 0 {
		fmt.Fprintln(w, "  (no recorded changes)")
		return
	}
	for _, e := range entries {
		fmt.Fprintf(w, "  %s  %s  %s — %s\n",
			e.When.Format("2006-01-02 15:04"),
			e.Commit,
			historyWho(e),
			strings.Join(e.Changes, "; "))
	}
}

// historyWho renders the author as "Name <email>", or just whichever part is
// present when the other is empty.
func historyWho(e store.HistoryEntry) string {
	switch {
	case e.Who != "" && e.Email != "":
		return e.Who + " <" + e.Email + ">"
	case e.Email != "":
		return "<" + e.Email + ">"
	default:
		return e.Who
	}
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
		// Measure in runes, not bytes: byte length over-counts every non-ASCII
		// character (é is 2 bytes, я is 2, 中 is 3) and wrapped such titles far
		// too early (SQ-0123). Rune count is still approximate for double-width
		// CJK glyphs (2 columns each), an accepted limitation — exact display
		// width needs a Unicode width table (a dependency) for a cosmetic gain.
		if utf8.RuneCountInString(cur)+1+utf8.RuneCountInString(w) <= width {
			cur += " " + w
		} else {
			lines = append(lines, cur)
			cur = w
		}
	}
	return append(lines, cur)
}

// renderBrief prints the human "resume" view: a header (branch + last activity),
// a one-line tally, then the CURRENT quest expanded, the OUTSTANDING backlog, and
// the RECENTLY CLOSED quests. It is tone-neutral — a data display, never routed
// through voice — and follows the list/show layout (borderless, tabwriter-aligned,
// wrapped to width; width <= 0 prints stable unwrapped rows for pipes/--no-wrap).
func renderBrief(w io.Writer, d brief.Data, branch string, commits []commitLine, width int) {
	parts := []string{"side-quest brief"}
	if branch != "" {
		parts = append(parts, branch)
	}
	if !d.LastActivity.IsZero() {
		parts = append(parts, "last activity "+brief.HumanizeSince(d.Now, d.LastActivity))
	}
	fmt.Fprintln(w, strings.Join(parts, " · "))
	curCount := 0
	if d.Current != nil {
		curCount = 1
	}
	fmt.Fprintf(w, "%d current · %d outstanding · %d recently closed\n",
		curCount, len(d.Outstanding), len(d.Closed))

	fmt.Fprintln(w)
	fmt.Fprintln(w, "CURRENT")
	if d.Current == nil {
		fmt.Fprintln(w, "  (none set — pick one with `side-quest current <id>`)")
	} else {
		renderBriefCurrent(w, d.Current, commits, width)
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "OUTSTANDING (%d)\n", len(d.Outstanding))
	if len(d.Outstanding) == 0 {
		fmt.Fprintln(w, "  (nothing outstanding)")
	} else {
		renderBriefRows(w, d.Outstanding, width, func(q *quest.Quest) []string {
			return []string{q.ID, string(q.Status), string(q.Type) + "/" + string(q.Priority)}
		})
	}

	if len(d.Closed) > 0 {
		fmt.Fprintln(w)
		if d.ClosedTotal > len(d.Closed) {
			fmt.Fprintf(w, "RECENTLY CLOSED (%d of %d)\n", len(d.Closed), d.ClosedTotal)
		} else {
			fmt.Fprintf(w, "RECENTLY CLOSED (%d)\n", len(d.Closed))
		}
		renderBriefRows(w, d.Closed, width, func(q *quest.Quest) []string {
			return []string{q.ID, string(q.Status), brief.HumanizeSince(d.Now, brief.ClosedTime(q))}
		})
	}
}

// renderBriefCurrent prints the featured current quest: id + type/priority/status,
// the title, the "why" narrative (mechanical capture lines stripped), any running
// notes, and the linked commits (subject only), each block wrapped to width.
func renderBriefCurrent(w io.Writer, q *quest.Quest, commits []commitLine, width int) {
	fmt.Fprintf(w, "  %s  %s/%s  %s\n", q.ID, q.Type, q.Priority, q.Status)
	for _, ln := range wrapText(q.Title, width-2) {
		fmt.Fprintf(w, "  %s\n", ln)
	}
	if why := brief.Narrative(q.Context); why != "" {
		writeBriefField(w, "why", why, width)
	}
	if body := strings.TrimRight(q.Body, "\n"); body != "" {
		writeBriefField(w, "notes", body, width)
	}
	if len(commits) > 0 {
		fmt.Fprintln(w, "  commits:")
		for _, c := range commits {
			subject, _, _ := strings.Cut(c.text, "\n")
			for _, ln := range wrapText(c.short+"  "+subject, width-4) {
				fmt.Fprintf(w, "    %s\n", ln)
			}
		}
	}
}

// writeBriefField prints a 2-space-indented "label: value" block, wrapping to
// width and hanging continuation lines under the value. Embedded newlines are
// preserved (each physical line wrapped on its own), so a multi-line notes body
// keeps its structure instead of flattening into one paragraph (cf. SQ-0127).
func writeBriefField(w io.Writer, label, value string, width int) {
	prefix := "  " + label + ": "
	indent := strings.Repeat(" ", len(prefix))
	first := true
	for _, para := range strings.Split(value, "\n") {
		if para == "" {
			fmt.Fprintln(w)
			continue
		}
		for _, ln := range wrapText(para, width-len(prefix)) {
			lead := indent
			if first {
				lead, first = prefix, false
			}
			fmt.Fprintf(w, "%s%s\n", lead, ln)
		}
	}
}

// renderBriefRows prints quests as aligned, 2-space-indented rows with the title
// last so a long title wraps under its column (the renderList continuation
// trick). meta returns the leading cells (id/status/…) for a quest.
func renderBriefRows(w io.Writer, quests []*quest.Quest, width int, meta func(*quest.Quest) []string) {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	titleCol := briefTitleColumn(quests, meta)
	for _, q := range quests {
		cells := meta(q)
		lines := []string{q.Title}
		if width > 0 {
			lines = wrapText(q.Title, width-titleCol)
		}
		fmt.Fprintf(tw, "  %s\t%s\n", strings.Join(cells, "\t"), lines[0])
		blank := "  " + strings.Repeat("\t", len(cells))
		for _, cont := range lines[1:] {
			fmt.Fprintf(tw, "%s%s\n", blank, cont)
		}
	}
	tw.Flush()
}

// briefTitleColumn reports the terminal column where the title cell begins under
// renderBriefRows' tabwriter layout (2-space indent + each leading cell's widest
// value + 2-space padding), so continuation lines hang under the title.
func briefTitleColumn(quests []*quest.Quest, meta func(*quest.Quest) []string) int {
	if len(quests) == 0 {
		return 0
	}
	widths := make([]int, len(meta(quests[0])))
	for _, q := range quests {
		for i, cell := range meta(q) {
			if n := utf8.RuneCountInString(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}
	col := 2 // the leading "  " indent, measured by tabwriter as part of cell 0
	for _, wd := range widths {
		col += wd + 2 // tabwriter padding
	}
	return col
}

// renderBriefMarkdown prints the same content as headed Markdown sections with
// absolute (RFC3339 / date) times — the shape pasted into or injected as an
// agent's context. Neutral, like the human render.
func renderBriefMarkdown(w io.Writer, d brief.Data, branch string, commits []commitLine) {
	fmt.Fprintln(w, "# side-quest brief")
	fmt.Fprintln(w)
	meta := []string{}
	if branch != "" {
		meta = append(meta, "branch `"+branch+"`")
	}
	if !d.LastActivity.IsZero() {
		meta = append(meta, "last activity "+d.LastActivity.Format(time.RFC3339))
	}
	curCount := 0
	if d.Current != nil {
		curCount = 1
	}
	meta = append(meta, fmt.Sprintf("%d current · %d outstanding · %d recently closed",
		curCount, len(d.Outstanding), len(d.Closed)))
	fmt.Fprintln(w, "_"+strings.Join(meta, " · ")+"_")

	fmt.Fprintln(w)
	fmt.Fprintln(w, "## Current")
	fmt.Fprintln(w)
	if d.Current == nil {
		fmt.Fprintln(w, "_None set._")
	} else {
		q := d.Current
		fmt.Fprintf(w, "**%s** — %s _(%s, %s/%s)_\n", q.ID, q.Title, q.Status, q.Type, q.Priority)
		if why := brief.Narrative(q.Context); why != "" {
			fmt.Fprintln(w)
			fmt.Fprintln(w, why)
		}
		if body := strings.TrimRight(q.Body, "\n"); body != "" {
			fmt.Fprintln(w)
			fmt.Fprintln(w, "Notes:")
			fmt.Fprintln(w)
			fmt.Fprintln(w, body)
		}
		if len(commits) > 0 {
			fmt.Fprintln(w)
			fmt.Fprintln(w, "Commits:")
			for _, c := range commits {
				subject, _, _ := strings.Cut(c.text, "\n")
				fmt.Fprintf(w, "- `%s` %s\n", c.short, subject)
			}
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "## Outstanding (%d)\n", len(d.Outstanding))
	fmt.Fprintln(w)
	if len(d.Outstanding) == 0 {
		fmt.Fprintln(w, "_Nothing outstanding._")
	} else {
		for _, q := range d.Outstanding {
			fmt.Fprintf(w, "- **%s** _(%s, %s/%s)_ — %s\n", q.ID, q.Status, q.Type, q.Priority, q.Title)
		}
	}

	if len(d.Closed) > 0 {
		fmt.Fprintln(w)
		if d.ClosedTotal > len(d.Closed) {
			fmt.Fprintf(w, "## Recently closed (%d of %d)\n", len(d.Closed), d.ClosedTotal)
		} else {
			fmt.Fprintf(w, "## Recently closed (%d)\n", len(d.Closed))
		}
		fmt.Fprintln(w)
		for _, q := range d.Closed {
			fmt.Fprintf(w, "- **%s** _(%s, closed %s)_ — %s\n",
				q.ID, q.Status, brief.ClosedTime(q).Format("2006-01-02"), q.Title)
		}
	}
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
	fmt.Fprintf(w, "local_only:    %t\n", c.LocalOnly)
}
