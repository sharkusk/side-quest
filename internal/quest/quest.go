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
	StatusDone      Status = "done"
	StatusDeferred  Status = "deferred"
	StatusDiscarded Status = "discarded"
)

// Valid reports whether s is one of the five known statuses.
func (s Status) Valid() bool {
	switch s {
	case StatusOpen, StatusPartial, StatusDone, StatusDeferred, StatusDiscarded:
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
