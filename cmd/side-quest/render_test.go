package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/sharkusk/side-quest/internal/quest"
)

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
	renderShow(&b, q, 0) // width 0 = no wrapping, current plain layout
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
	renderShow(&b, q, width)
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
