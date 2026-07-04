package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sharkusk/side-quest/internal/config"
	"github.com/sharkusk/side-quest/internal/quest"
	"github.com/sharkusk/side-quest/internal/voice"
)

func TestRenderShowCommitBlock(t *testing.T) {
	// Only the commits block is under test, so a minimal quest suffices (renderShow
	// prints empty strings for the unset status/type/priority fields — fine here).
	q := &quest.Quest{ID: "SQ-0001", Title: "t", Created: time.Now()}
	commits := []commitLine{
		{short: "b510826", text: "refactor: move the thing"},
		{short: "d5eb4b2", text: "docs: reword it\n\nbody line here\n\nToken: xyz"},
		{short: "cafef00dcafef00d", text: "(message unavailable)"},
	}
	var buf bytes.Buffer
	renderShow(&buf, q, 0, commits)
	out := buf.String()

	for _, want := range []string{
		"commits:",
		"b510826  refactor: move the thing",
		"d5eb4b2  docs: reword it",
		"body line here", // --full body is printed
		"cafef00dcafef00d  (message unavailable)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q:\n%s", want, out)
		}
	}
	// The subject must NOT be duplicated inside the body block.
	if strings.Count(out, "docs: reword it") != 1 {
		t.Errorf("subject duplicated in body:\n%s", out)
	}
}

// The first commit shares the commits: line (like any label: value field); each
// later commit is inset to the same column as the other field values, so the
// block reads as one aligned column.
func TestRenderShowCommitsAlignWithContext(t *testing.T) {
	q := &quest.Quest{ID: "SQ-0001", Title: "t", Created: time.Now()}
	commits := []commitLine{
		{short: "b510826", text: "refactor: move the thing"},
		{short: "d5eb4b2", text: "docs: reword it"},
	}
	var buf bytes.Buffer
	renderShow(&buf, q, 0, commits)

	label := fmt.Sprintf("%-*s ", showLabelPad, "commits:")
	indent := strings.Repeat(" ", len(label)) // column where context: values begin
	wantFirst := label + "b510826  refactor: move the thing"
	wantRest := indent + "d5eb4b2  docs: reword it"

	hasFirst, hasRest := false, false
	for _, ln := range strings.Split(buf.String(), "\n") {
		if ln == wantFirst {
			hasFirst = true
		}
		if ln == wantRest {
			hasRest = true
		}
	}
	if !hasFirst {
		t.Errorf("first commit not on the commits: line; want %q in:\n%s", wantFirst, buf.String())
	}
	if !hasRest {
		t.Errorf("later commit not inset to the value column; want %q in:\n%s", wantRest, buf.String())
	}
}

// A long commit subject wraps at the terminal width, and every continuation
// line hangs at the value column — the same behavior as a wrapped context:.
func TestRenderShowCommitSubjectWraps(t *testing.T) {
	const width = 50
	long := "refactor: a deliberately long commit subject line that must wrap across two lines"
	q := &quest.Quest{ID: "SQ-0001", Title: "t", Created: time.Now()}
	commits := []commitLine{{short: "b510826", text: long}}
	var buf bytes.Buffer
	renderShow(&buf, q, width, commits)

	indent := strings.Repeat(" ", showLabelPad+1)
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	sawContinuation := false
	for _, ln := range lines {
		if len(ln) > width {
			t.Errorf("line exceeds width %d: %q", width, ln)
		}
		if strings.HasPrefix(ln, indent) && strings.TrimSpace(ln) != "" {
			sawContinuation = true
		}
	}
	if !sawContinuation {
		t.Fatalf("expected a continuation line hung at the value column at width %d:\n%s", width, buf.String())
	}
	flat := strings.Join(strings.Fields(buf.String()), " ")
	for _, wd := range strings.Fields(long) {
		if !strings.Contains(flat, wd) {
			t.Errorf("wrapped output dropped word %q", wd)
		}
	}
}

// renderShow should print the tag: block ABOVE context:, so that context and
// the appended notes (in Body) read as one contiguous block at the bottom —
// tags must not split context from its notes.
func TestRenderShowTagsAboveContext(t *testing.T) {
	q := &quest.Quest{
		ID:       "SQ-0001",
		Title:    "T",
		Status:   quest.StatusOpen,
		Type:     quest.TypeFeature,
		Priority: quest.PriorityLow,
		Created:  time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC),
		Context:  "why it came up",
		Tags:     map[string]string{"area": "cli"},
		Body:     "--- note 2026-07-03T00:00:00Z ---\n\nan appended note",
	}
	var b bytes.Buffer
	renderShow(&b, q, 0, nil) // width 0 = no wrapping, current plain layout
	out := b.String()

	tagIdx := strings.Index(out, "tag:")
	ctxIdx := strings.Index(out, "context:")
	noteIdx := strings.Index(out, "an appended note")
	if tagIdx < 0 || ctxIdx < 0 || noteIdx < 0 {
		t.Fatalf("missing a section (tag=%d context=%d note=%d):\n%s", tagIdx, ctxIdx, noteIdx, out)
	}
	if !(tagIdx < ctxIdx && ctxIdx < noteIdx) {
		t.Fatalf("want tag: before context: before notes; got tag=%d context=%d note=%d\n%s",
			tagIdx, ctxIdx, noteIdx, out)
	}
}

// With a width set, renderShow wraps a long context value and hangs the
// continuation lines under the value column (col 11); no rendered line exceeds
// the width, and every word survives.
func TestRenderShowWrapsLongFields(t *testing.T) {
	const width = 40
	longCtx := "this is a deliberately long context value that must wrap across several lines when a terminal width is supplied to renderShow"
	q := &quest.Quest{
		ID:       "SQ-0001",
		Title:    "T",
		Status:   quest.StatusOpen,
		Type:     quest.TypeFeature,
		Priority: quest.PriorityLow,
		Created:  time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC),
		Context:  longCtx,
	}
	var b bytes.Buffer
	renderShow(&b, q, width, nil)
	out := b.String()

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	sawContinuation := false
	for _, ln := range lines {
		if len(ln) > width {
			t.Errorf("line exceeds width %d: %q", width, ln)
		}
		// A wrapped continuation of context: is indented to the value column.
		if strings.HasPrefix(ln, "           ") && strings.TrimSpace(ln) != "" {
			sawContinuation = true
		}
	}
	if !sawContinuation {
		t.Errorf("expected an indented continuation line; got:\n%s", out)
	}
	// Every word of the context must still be present once unwrapped.
	flat := strings.Join(strings.Fields(out), " ")
	for _, word := range strings.Fields(longCtx) {
		if !strings.Contains(flat, word) {
			t.Errorf("wrapped output dropped word %q", word)
		}
	}
}

// A long title in `list` wraps to the terminal width, and every continuation
// line hangs under the TITLE column (where tabwriter aligns the header) rather
// than running off the edge or falling to the left margin.
func TestRenderListWrapsLongTitles(t *testing.T) {
	const width = 60
	quests := []*quest.Quest{{
		ID:       "SQ-0001",
		Status:   quest.StatusOpen,
		Type:     quest.TypeFeature,
		Priority: quest.PriorityLow,
		Title:    "a deliberately long quest title that needs to wrap across more than one line in a narrow terminal",
	}}
	var buf bytes.Buffer
	renderList(&buf, quests, voice.New(config.TonePlain), width)

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	titleCol := -1
	for _, ln := range lines {
		if i := strings.Index(ln, "TITLE"); i >= 0 {
			titleCol = i
		}
	}
	if titleCol < 0 {
		t.Fatalf("no header row with TITLE:\n%s", buf.String())
	}
	indent := strings.Repeat(" ", titleCol)
	sawContinuation := false
	for _, ln := range lines {
		if len(ln) > width {
			t.Errorf("line exceeds width %d: %q", width, ln)
		}
		// A continuation row is padded with spaces up to the title column.
		if ln != "" && strings.HasPrefix(ln, indent) {
			sawContinuation = true
		}
	}
	if !sawContinuation {
		t.Fatalf("expected a title continuation hung at the title column (%d) at width %d:\n%s",
			titleCol, width, buf.String())
	}
	flat := strings.Join(strings.Fields(buf.String()), " ")
	for _, wd := range strings.Fields(quests[0].Title) {
		if !strings.Contains(flat, wd) {
			t.Errorf("wrapped list dropped title word %q", wd)
		}
	}
}

func TestWrapText(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		width int
		want  []string
	}{
		{"short fits", "one two", 20, []string{"one two"}},
		{"greedy wrap", "aa bb cc dd", 5, []string{"aa bb", "cc dd"}},
		{"long token overflows intact", "short verylongtokenhere x", 6, []string{"short", "verylongtokenhere", "x"}},
		{"width zero disables", "aa bb cc", 0, []string{"aa bb cc"}},
		{"empty stays empty", "", 10, []string{""}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := wrapText(c.text, c.width)
			if len(got) != len(c.want) {
				t.Fatalf("wrapText(%q,%d) = %q, want %q", c.text, c.width, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("wrapText(%q,%d) = %q, want %q", c.text, c.width, got, c.want)
				}
			}
		})
	}
}
