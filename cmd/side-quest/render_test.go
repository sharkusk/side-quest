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
	renderShow(&b, q)
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
