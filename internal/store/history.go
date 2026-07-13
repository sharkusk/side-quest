package store

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/sharkusk/side-quest/internal/quest"
)

// HistoryEntry is one recorded change to a quest, reconstructed from the ref's
// commit history: who changed it, when, and what changed. The mutation commit
// messages are intentionally generic ("side-quest: update SQ-0001"), so Changes
// is derived by diffing the quest file against the commit's first parent, not by
// parsing the message.
type HistoryEntry struct {
	Commit  string    `json:"commit"`          // abbreviated sha of the quest-ref commit
	When    time.Time `json:"when"`            // author date
	Who     string    `json:"who"`             // author name (may be empty)
	Email   string    `json:"email,omitempty"` // author email
	Changes []string  `json:"changes"`         // human descriptions, e.g. "created", "status: open → done"
}

// History returns a quest's change history, oldest first. Each entry is one
// commit on refs/side-quest/quests that touched the quest's file; Changes is a
// field-level diff against that commit's first parent (the merge convention —
// sync can introduce merge commits). Errors with ErrNotFound if the quest does
// not exist.
func (s *Store) History(id string) ([]HistoryEntry, error) {
	id, err := s.canonicalID(id)
	if err != nil {
		return nil, err
	}
	tip, err := s.tip()
	if err != nil {
		return nil, err
	}
	if tip == "" {
		return nil, ErrNotFound
	}
	path := questPath(id)
	if _, err := s.readFile(tip, path); err != nil {
		return nil, ErrNotFound
	}

	// One line per commit touching path, newest first. Fields are \x1f-separated
	// (no field — sha, name, ISO date, parents — can contain that byte or \n).
	const fieldSep = "\x1f"
	format := "%H" + fieldSep + "%h" + fieldSep + "%aN" + fieldSep + "%aE" + fieldSep + "%aI" + fieldSep + "%P"
	out, err := s.git.Run("log", Ref, "--format="+format, "--", path)
	if err != nil {
		return nil, err
	}

	type commit struct {
		full, short, who, email, iso, parent string
	}
	var commits []commit
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, fieldSep)
		if len(f) != 6 {
			return nil, fmt.Errorf("history: unexpected log line %q", line)
		}
		parent := ""
		if ps := strings.Fields(f[5]); len(ps) > 0 {
			parent = ps[0] // first parent
		}
		commits = append(commits, commit{full: f[0], short: f[1], who: f[2], email: f[3], iso: f[4], parent: parent})
	}

	// Read every quest blob we need — each commit's version and its first
	// parent's — in one cat-file batch, tolerating the missing parent at creation.
	specSet := map[string]bool{}
	for _, c := range commits {
		specSet[c.full+":"+path] = true
		if c.parent != "" {
			specSet[c.parent+":"+path] = true
		}
	}
	blobs, err := s.readOptionalBlobs(specSet)
	if err != nil {
		return nil, err
	}

	// Emit oldest first: git log is newest-first, so walk it in reverse.
	entries := make([]HistoryEntry, 0, len(commits))
	for i := len(commits) - 1; i >= 0; i-- {
		c := commits[i]
		newQ := parseQuestBlob(id, blobs[c.full+":"+path])
		var oldQ *quest.Quest
		if c.parent != "" {
			oldQ = parseQuestBlob(id, blobs[c.parent+":"+path])
		}
		when, err := time.Parse(time.RFC3339, c.iso)
		if err != nil {
			return nil, fmt.Errorf("history: bad date %q: %w", c.iso, err)
		}
		entries = append(entries, HistoryEntry{
			Commit:  c.short,
			When:    when,
			Who:     c.who,
			Email:   c.email,
			Changes: diffQuest(oldQ, newQ),
		})
	}
	return entries, nil
}

// readOptionalBlobs reads the given object specs (`<sha>:<path>`) in a single
// cat-file batch, returning a map from spec to contents; a missing object maps to
// a nil slice rather than erroring.
func (s *Store) readOptionalBlobs(specSet map[string]bool) (map[string][]byte, error) {
	specs := make([]string, 0, len(specSet))
	for sp := range specSet {
		specs = append(specs, sp)
	}
	m := make(map[string][]byte, len(specs))
	if len(specs) == 0 {
		return m, nil
	}
	var in bytes.Buffer
	for _, sp := range specs {
		in.WriteString(sp + "\n")
	}
	out, err := s.git.RunRawInput(in.Bytes(), "cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	blobs, err := parseBatch(out, len(specs), true)
	if err != nil {
		return nil, err
	}
	for i, sp := range specs {
		m[sp] = blobs[i]
	}
	return m, nil
}

// parseQuestBlob parses a quest blob, or returns nil if the blob is absent (the
// file did not exist at that commit) or unparseable (a corrupt historical state
// should degrade to "no old version", not fail the whole history).
func parseQuestBlob(id string, blob []byte) *quest.Quest {
	if blob == nil {
		return nil
	}
	q, err := quest.Unmarshal(id, blob)
	if err != nil {
		return nil
	}
	return q
}

// diffQuest describes how a quest changed from old to new. A nil old means the
// quest was created in this commit. When nothing recognizable changed (e.g. a
// no-op rewrite), it reports a generic "updated" so the commit is never silent.
func diffQuest(old, new *quest.Quest) []string {
	if old == nil {
		return []string{"created"}
	}
	var ch []string
	if old.Title != new.Title {
		ch = append(ch, "title changed")
	}
	if old.Status != new.Status {
		ch = append(ch, fmt.Sprintf("status: %s → %s", old.Status, new.Status))
	}
	if old.Type != new.Type {
		ch = append(ch, fmt.Sprintf("type: %s → %s", old.Type, new.Type))
	}
	if old.Priority != new.Priority {
		ch = append(ch, fmt.Sprintf("priority: %s → %s", old.Priority, new.Priority))
	}
	for _, sha := range added(old.Commits, new.Commits) {
		ch = append(ch, "linked commit "+shortSHA(sha))
	}
	for _, sha := range added(new.Commits, old.Commits) {
		ch = append(ch, "unlinked commit "+shortSHA(sha))
	}
	if !sameTags(old.Tags, new.Tags) {
		ch = append(ch, "tags updated")
	}
	if old.Body != new.Body {
		if noteCount(new.Body) > noteCount(old.Body) {
			ch = append(ch, "note added")
		} else {
			ch = append(ch, "body edited")
		}
	}
	if len(ch) == 0 {
		ch = append(ch, "updated")
	}
	return ch
}

// added returns the entries present in b but not in a (order preserved).
func added(a, b []string) []string {
	seen := make(map[string]bool, len(a))
	for _, x := range a {
		seen[x] = true
	}
	var out []string
	for _, x := range b {
		if !seen[x] {
			out = append(out, x)
		}
	}
	return out
}

func sameTags(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// noteCount counts AppendNote-style entries in a body (each opens with the
// "--- note " marker) so a body change that adds one reads as "note added".
func noteCount(body string) int { return strings.Count(body, "--- note ") }

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
