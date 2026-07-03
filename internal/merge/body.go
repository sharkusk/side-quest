package merge

import (
	"sort"
	"strings"

	"github.com/sharkusk/side-quest/internal/quest"
)

// noteHeader marks the start of a timestamped note block, matching the format
// store.AppendNote writes: "--- note <RFC3339> ---".
const noteHeaderPrefix = "--- note "
const noteHeaderSuffix = " ---"

// note is one parsed note block: its timestamp (for ordering) and full text
// (header + body, trimmed) for dedup.
type note struct {
	ts   string
	text string
}

// splitBody separates the preamble (prose before the first note header) from the
// ordered list of note blocks. A body with no note headers is all preamble.
func splitBody(body string) (preamble string, notes []note) {
	lines := strings.Split(body, "\n")
	// find first note header
	start := -1
	for i, ln := range lines {
		if isNoteHeader(ln) {
			start = i
			break
		}
	}
	if start < 0 {
		return strings.TrimRight(body, "\n"), nil
	}
	preamble = strings.TrimRight(strings.Join(lines[:start], "\n"), "\n")
	// walk note blocks
	var cur []string
	var curTS string
	flush := func() {
		if len(cur) == 0 {
			return
		}
		notes = append(notes, note{ts: curTS, text: strings.TrimRight(strings.Join(cur, "\n"), "\n")})
		cur = nil
	}
	for _, ln := range lines[start:] {
		if isNoteHeader(ln) {
			flush()
			curTS = noteTimestamp(ln)
		}
		cur = append(cur, ln)
	}
	flush()
	return preamble, notes
}

func isNoteHeader(line string) bool {
	line = strings.TrimSpace(line)
	return strings.HasPrefix(line, noteHeaderPrefix) && strings.HasSuffix(line, noteHeaderSuffix)
}

func noteTimestamp(line string) string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, noteHeaderPrefix)
	line = strings.TrimSuffix(line, noteHeaderSuffix)
	return strings.TrimSpace(line)
}

// mergeBody keeps the winner's preamble and unions both sides' notes, deduped by
// full text and ordered by timestamp (stable for equal timestamps).
func mergeBody(winner, l, r *quest.Quest) string {
	preamble, _ := splitBody(winner.Body)
	_, ln := splitBody(l.Body)
	_, rn := splitBody(r.Body)

	seen := map[string]bool{}
	var all []note
	for _, n := range append(append([]note{}, ln...), rn...) {
		if seen[n.text] {
			continue
		}
		seen[n.text] = true
		all = append(all, n)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].ts < all[j].ts })

	var b strings.Builder
	if preamble != "" {
		b.WriteString(preamble)
	}
	for _, n := range all {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(n.text)
	}
	return b.String()
}
