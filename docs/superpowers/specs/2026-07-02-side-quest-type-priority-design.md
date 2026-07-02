# Quest Type & Priority Fields — Design

**Date:** 2026-07-02
**Status:** Approved (brainstorm)
**Scope:** Schema + store layer only. CLI flags, list filters, and MCP surface are deferred to their own phases (3/4).

## Goal

Add two first-class, constrained-enum fields to the quest model:

- `Type` — `bug` | `feature`
- `Priority` — `high` | `low`

Both are always present on a persisted quest (no "unset" state), validated the
same way `Status` is today.

## Motivation

Quests currently carry only a lifecycle `Status`. There is no way to say *what
kind* of work a quest is or *how urgent* it is, which the user needs for
triage and (later) filtered listing. The user chose first-class fields with
constrained enums over free-form tags so the values are validated and
machine-filterable.

## Design

### 1. Model (`internal/quest/quest.go`)

Mirror the existing `Status` pattern exactly — a named string type with a
`Valid()` method and exported constants.

```go
type Type string

const (
	TypeBug     Type = "bug"
	TypeFeature Type = "feature"
)

func (t Type) Valid() bool {
	switch t {
	case TypeBug, TypeFeature:
		return true
	}
	return false
}

// DefaultType is applied when a quest is created without an explicit type.
const DefaultType = TypeFeature

type Priority string

const (
	PriorityHigh Priority = "high"
	PriorityLow  Priority = "low"
)

func (p Priority) Valid() bool {
	switch p {
	case PriorityHigh, PriorityLow:
		return true
	}
	return false
}

// DefaultPriority is applied when a quest is created without an explicit priority.
const DefaultPriority = PriorityLow
```

Two new fields on the `Quest` struct, placed immediately after `Status` so they
sit together in the frontmatter block. They are **not** `omitempty` — like
`status`, they are always serialized:

```go
type Quest struct {
	ID string `yaml:"-"`

	Title    string     `yaml:"title"`
	Status   Status     `yaml:"status"`
	Type     Type       `yaml:"type"`
	Priority Priority   `yaml:"priority"`
	Created  time.Time  `yaml:"created"`
	Completed *time.Time `yaml:"completed,omitempty"`
	Commits  []string   `yaml:"commits"`
	Context  string     `yaml:"context,omitempty"`
	Tags     map[string]string `yaml:"tags,omitempty"`

	Body string `yaml:"-"`
}
```

### 2. Creation (`store.Create`)

The fields are **required-with-defaults**. The signature grows by two positional
parameters (matching the existing positional style; no options struct):

```go
func (s *Store) Create(title, context string, typ quest.Type, prio quest.Priority, tags map[string]string) (*quest.Quest, error)
```

Coercion and validation rules, applied inside `Create` before the mutate:

- Empty `typ` (`""`) ⇒ set to `quest.DefaultType`. Empty `prio` (`""`) ⇒ set to
  `quest.DefaultPriority`. This keeps quick capture (`/sq <idea>`) a one-liner.
- A **non-empty but invalid** value (e.g. `"buggg"`) ⇒ return an error; do not
  create the quest. Omission is fine; a typo is not.
- After coercion the quest is written with valid `Type` and `Priority` — so
  every quest the tool writes carries both.

### 3. Mutators (`store`)

Add two setters that mirror `SetStatus` — validate via `Valid()`, then `Update`:

```go
func (s *Store) SetType(id string, t quest.Type) error {
	if !t.Valid() {
		return fmt.Errorf("invalid type %q", t)
	}
	return s.Update(id, func(q *quest.Quest) { q.Type = t })
}

func (s *Store) SetPriority(id string, p quest.Priority) error {
	if !p.Valid() {
		return fmt.Errorf("invalid priority %q", p)
	}
	return s.Update(id, func(q *quest.Quest) { q.Priority = p })
}
```

These are what a later `edit`/`reclassify` command (Phase 3) will call.

### 4. Validation boundary

Same as `Status` today: validity is enforced only at the **write** edge
(`Create`, `SetType`, `SetPriority`). `Unmarshal` remains a pure parser and does
**not** validate or coerce. A hand-edited or legacy quest whose YAML omits the
fields loads with empty values and is normalized only when next written. No
migration code is added — the ref is empty today, and every quest the tool
writes going forward includes both fields.

### 5. Documentation (living docs)

In the **same change** as the behavior:

- `docs/architecture.md` — extend the quest-model description with `type` and
  `priority` (allowed values, defaults, write-only validation boundary).
- `README.md` — add the two fields to the quest concepts/glossary.

The dated spec/plan files under `docs/superpowers/` are frozen history and are
not edited to match later code.

## Out of Scope (deferred)

- CLI flags `--type` / `--priority` on the create command (Phase 3).
- Filtered listing (`list --type bug`, `--priority high`) (Phase 3).
- MCP tool parameters (Phase 4).
- Importer default-assignment policy for babelmap items (Phase 6 — that plan
  decides how imported quests are classified).

## Testing

- `quest` package: `Valid()` truth tables for `Type` and `Priority` (each known
  value true; an unknown value false); round-trip Marshal/Unmarshal preserves
  both fields; the fields appear in serialized frontmatter.
- `store`: `Create` with explicit values persists them; `Create` with empty
  values applies the defaults; `Create` with a non-empty invalid value errors
  and writes nothing; `SetType`/`SetPriority` update a valid value and reject an
  invalid one.
