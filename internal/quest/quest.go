// Package quest defines the Quest model and its on-disk representation: a YAML
// frontmatter block (delimited by `---` fences) followed by a Markdown body.
//
// The quest id is intentionally NOT a field in the serialized file — it is the
// filename (quests/SQ-0001.md -> "SQ-0001"), the single source of truth
// (spec §5.5). Unmarshal takes the id from the caller (derived from the path).
package quest

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Status is the lifecycle state of a quest. Defining a named string type (akin
// to a C typedef) lets us attach methods like Valid and use it distinctly in
// signatures.
type Status string

const (
	StatusOpen      Status = "open"
	StatusPartial   Status = "partial"
	StatusConfirm   Status = "confirm"
	StatusDone      Status = "done"
	StatusDeferred  Status = "deferred"
	StatusDiscarded Status = "discarded"
)

// Valid reports whether s is one of the six known statuses.
func (s Status) Valid() bool {
	switch s {
	case StatusOpen, StatusPartial, StatusConfirm, StatusDone, StatusDeferred, StatusDiscarded:
		return true
	}
	return false
}

// Type is the kind of work a quest represents. Like Status, it is a named
// string type with a Valid method and a small set of allowed values.
type Type string

const (
	TypeBug     Type = "bug"
	TypeFeature Type = "feature"
)

// Valid reports whether t is one of the known types.
func (t Type) Valid() bool {
	switch t {
	case TypeBug, TypeFeature:
		return true
	}
	return false
}

// DefaultType is applied when a quest is created without an explicit type.
const DefaultType = TypeFeature

// Priority is how urgent a quest is.
type Priority string

const (
	PriorityHigh Priority = "high"
	PriorityLow  Priority = "low"
)

// Valid reports whether p is one of the known priorities.
func (p Priority) Valid() bool {
	switch p {
	case PriorityHigh, PriorityLow:
		return true
	}
	return false
}

// DefaultPriority is applied when a quest is created without an explicit priority.
const DefaultPriority = PriorityLow

// MatchTags reports whether have satisfies every key/value pair in want (an AND
// over the filter). An empty want matches anything; a want pair whose key is
// absent from have, or present with a different value, fails the match. Used by
// the CLI and MCP list filters.
func MatchTags(have, want map[string]string) bool {
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}

// Quest is one tracked unit of work.
//
// Struct tags (the `yaml:"..."` strings) are metadata read by the YAML library
// via reflection — similar in spirit to Python type annotations, but they
// direct (de)serialization. `yaml:"-"` means "never serialize this field":
// ID and Body live outside the YAML frontmatter block.
type Quest struct {
	ID string `yaml:"-"` // from filename, set by Unmarshal; never written to the file

	Title     string            `yaml:"title"`
	Status    Status            `yaml:"status"`
	Type      Type              `yaml:"type"`
	Priority  Priority          `yaml:"priority"`
	Created   time.Time         `yaml:"created"`
	Completed *time.Time        `yaml:"completed,omitempty"` // pointer => can be absent/null
	Commits   []string          `yaml:"commits"`
	Context   string            `yaml:"context,omitempty"`
	Tags      map[string]string `yaml:"tags,omitempty"`

	Body string `yaml:"-"` // Markdown after the frontmatter block
}

// NormalizeID canonicalizes a user-supplied quest id so the frontends can accept
// shorthand. A bare number (11, 0011, 00011) becomes prefix + "-" + the number
// zero-padded to width (SQ-0011) — bare-digit shorthand is a sequential-id
// convenience, so leading zeros are integer-normalized. An id carrying the
// "prefix-" head — matched case-insensitively, so a hand-typed "sq-11" works —
// has its prefix case fixed, and a SHORT all-digit suffix padded to width
// (SQ-12 -> SQ-0012, fixing the trap where the unpadded form passed the hook but
// linked nothing, SQ-0119); a suffix already at or beyond width is kept VERBATIM
// so a random-strategy id that is all digits with a leading zero (SQ-012345) is
// never mangled into SQ-12345. Non-digit suffixes (random hex ids, typos) pass
// through unchanged. Normalization is idempotent and never fails: an id that
// resolves to nothing real is caught later by the store's existence check, not
// here.
func NormalizeID(prefix string, width int, raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) > len(prefix) && strings.EqualFold(raw[:len(prefix)], prefix) && raw[len(prefix)] == '-' {
		suffix := raw[len(prefix)+1:]
		if isDigits(suffix) && len(suffix) < width {
			return prefix + "-" + strings.Repeat("0", width-len(suffix)) + suffix
		}
		return prefix + "-" + suffix
	}
	if n, err := strconv.Atoi(raw); err == nil && n >= 0 && isDigits(raw) {
		return fmt.Sprintf("%s-%0*d", prefix, width, n)
	}
	return raw
}

// isDigits reports whether s is non-empty and all ASCII digits.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

const fence = "---"

// Marshal renders q to file bytes: `---`-fenced YAML frontmatter then the body.
func Marshal(q *Quest) ([]byte, error) {
	var fm bytes.Buffer
	enc := yaml.NewEncoder(&fm)
	enc.SetIndent(2)
	if err := enc.Encode(q); err != nil {
		return nil, fmt.Errorf("encode frontmatter: %w", err)
	}
	enc.Close()

	var out bytes.Buffer
	out.WriteString(fence + "\n")
	out.Write(fm.Bytes())
	out.WriteString(fence + "\n")
	if q.Body != "" {
		out.WriteString("\n")
		out.WriteString(q.Body)
		if !strings.HasSuffix(q.Body, "\n") {
			out.WriteString("\n")
		}
	}
	return out.Bytes(), nil
}

// Unmarshal parses file bytes into a Quest, assigning id from the filename.
func Unmarshal(id string, data []byte) (*Quest, error) {
	s := string(data)
	if !strings.HasPrefix(s, fence+"\n") && s != fence {
		return nil, fmt.Errorf("quest %s: missing leading frontmatter fence", id)
	}
	rest := strings.TrimPrefix(s, fence+"\n")
	idx := strings.Index(rest, "\n"+fence)
	if idx < 0 {
		return nil, fmt.Errorf("quest %s: unterminated frontmatter", id)
	}
	fmBlock := rest[:idx]
	body := rest[idx+len("\n"+fence):]
	body = strings.TrimLeft(body, "\n") // drop blank line(s) after closing fence

	var q Quest
	if err := yaml.Unmarshal([]byte(fmBlock), &q); err != nil {
		return nil, fmt.Errorf("quest %s: parse frontmatter: %w", id, err)
	}
	q.ID = id
	q.Body = strings.TrimRight(body, "\n")
	return &q, nil
}
