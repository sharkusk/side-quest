# Quest Type & Priority Fields Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add two first-class constrained-enum fields — `Type` (bug|feature) and `Priority` (high|low) — to the quest model, validated like `Status`.

**Architecture:** Mirror the existing `Status` pattern exactly: a named string type with a `Valid()` method and exported constants in `internal/quest`, plus struct fields on `Quest`. The `store.Create` signature grows by two parameters (required-with-defaults: empty coerces to the default, a non-empty invalid value errors). Two new store mutators `SetType`/`SetPriority` mirror `SetStatus`. Validation lives only at the write boundary; `Unmarshal` stays a pure parser.

**Tech Stack:** Go, `gopkg.in/yaml.v3`, standard `testing`.

**Spec:** `docs/superpowers/specs/2026-07-02-side-quest-type-priority-design.md`

## Global Constraints

- Module path is `github.com/sharkusk/side-quest`. Go git version floor stays 2.13 (unchanged — this feature adds no git commands).
- `Type` enum values are exactly `bug` and `feature`. `Priority` enum values are exactly `high` and `low`. No other values.
- Defaults: `DefaultType = TypeFeature` (`"feature"`), `DefaultPriority = PriorityLow` (`"low"`).
- Both fields are **always serialized** — no `omitempty` (like `status`).
- Validation is enforced only at the **write** edge (`Create`, `SetType`, `SetPriority`). `Unmarshal` does NOT validate or coerce. No migration/read-coercion code.
- Required-with-defaults: in `Create`, empty (`""`) coerces to the default; a non-empty invalid value returns an error and writes nothing.
- Living docs (`docs/architecture.md`, `README.md`) are updated on this branch (Task 3). The dated `docs/superpowers/specs|plans/` files are frozen history — do not edit them to match code.
- Out of scope (do NOT build here): CLI `--type`/`--priority` flags, list filters, MCP parameters, importer classification. Those belong to later phases.
- Every commit message ends with the two footer lines:
  ```
  Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws
  ```
- A warn-only `.githooks/pre-commit` may remind that `internal/**/*.go` changed without a docs change on Tasks 1 and 2 — that is expected; the docs land in Task 3 on the same branch. It is a warning, not a blocker.

---

### Task 1: Quest model — `Type` and `Priority`

**Files:**
- Modify: `internal/quest/quest.go`
- Test: `internal/quest/quest_test.go`

**Interfaces:**
- Consumes: nothing new (extends the existing `quest` package).
- Produces:
  - `type Type string` with consts `TypeBug Type = "bug"`, `TypeFeature Type = "feature"`, method `func (t Type) Valid() bool`, const `DefaultType = TypeFeature`.
  - `type Priority string` with consts `PriorityHigh Priority = "high"`, `PriorityLow Priority = "low"`, method `func (p Priority) Valid() bool`, const `DefaultPriority = PriorityLow`.
  - New `Quest` struct fields: `Type Type ` + yaml tag `type`, `Priority Priority ` + yaml tag `priority`, placed immediately after `Status`.

- [ ] **Step 1: Write the failing validity tests**

Add to `internal/quest/quest_test.go`:

```go
func TestTypeValid(t *testing.T) {
	for _, ty := range []Type{TypeBug, TypeFeature} {
		if !ty.Valid() {
			t.Errorf("%q should be valid", ty)
		}
	}
	if Type("bogus").Valid() {
		t.Error("bogus type should be invalid")
	}
	if Type("").Valid() {
		t.Error("empty type should be invalid")
	}
}

func TestPriorityValid(t *testing.T) {
	for _, p := range []Priority{PriorityHigh, PriorityLow} {
		if !p.Valid() {
			t.Errorf("%q should be valid", p)
		}
	}
	if Priority("bogus").Valid() {
		t.Error("bogus priority should be invalid")
	}
	if Priority("").Valid() {
		t.Error("empty priority should be invalid")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/quest/ -run 'TestTypeValid|TestPriorityValid' -v`
Expected: build failure — `undefined: Type`, `undefined: Priority`, etc.

- [ ] **Step 3: Add the types, constants, `Valid()` methods, and defaults**

In `internal/quest/quest.go`, after the `Status` block (which ends at the `Valid` method for `Status`, before the `Quest` struct), add:

```go
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
```

- [ ] **Step 4: Run the validity tests to verify they pass**

Run: `go test ./internal/quest/ -run 'TestTypeValid|TestPriorityValid' -v`
Expected: PASS.

- [ ] **Step 5: Add the struct fields**

In `internal/quest/quest.go`, in the `Quest` struct, insert the two fields immediately after the `Status` field so the block reads:

```go
	Title     string            `yaml:"title"`
	Status    Status            `yaml:"status"`
	Type      Type              `yaml:"type"`
	Priority  Priority          `yaml:"priority"`
	Created   time.Time         `yaml:"created"`
	Completed *time.Time        `yaml:"completed,omitempty"` // pointer => can be absent/null
	Commits   []string          `yaml:"commits"`
	Context   string            `yaml:"context,omitempty"`
	Tags      map[string]string `yaml:"tags,omitempty"`
```

- [ ] **Step 6: Extend the round-trip test to cover the new fields**

In `internal/quest/quest_test.go`, in `TestMarshalRoundTrip`, add `Type` and `Priority` to the `q` literal (after `Status: StatusOpen,`):

```go
		Status:   StatusOpen,
		Type:     TypeBug,
		Priority: PriorityHigh,
```

Then, after the existing `title/status` assertion block, add:

```go
	if got.Type != TypeBug {
		t.Errorf("type: got %q want bug", got.Type)
	}
	if got.Priority != PriorityHigh {
		t.Errorf("priority: got %q want high", got.Priority)
	}
	if !strings.Contains(string(data), "type: bug") || !strings.Contains(string(data), "priority: high") {
		t.Fatalf("type/priority not serialized into frontmatter:\n%s", data)
	}
```

- [ ] **Step 7: Run the full quest package test + fmt/vet/build**

Run: `go test ./internal/quest/ -v && gofmt -l internal/quest && go vet ./internal/quest/ && go build ./...`
Expected: all tests PASS; `gofmt -l` prints nothing; vet and build clean.

- [ ] **Step 8: Commit**

```bash
git add internal/quest/quest.go internal/quest/quest_test.go
git commit -m "feat(quest): add Type (bug|feature) and Priority (high|low) fields

Named string types mirroring Status, with Valid() and DefaultType/
DefaultPriority. Fields always serialized (no omitempty).

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws"
```

---

### Task 2: `store.Create` — required-with-defaults + update all callers

**Files:**
- Modify: `internal/store/store.go` (the `Create` method, currently around lines 307–345)
- Modify (caller arity): `internal/store/store_test.go`, `internal/store/link_test.go`, `cmd/side-quest/main_test.go`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes from Task 1: `quest.Type`, `quest.Priority`, `quest.DefaultType`, `quest.DefaultPriority`, `(quest.Type).Valid()`, `(quest.Priority).Valid()`, and the `Quest.Type`/`Quest.Priority` fields.
- Produces: new signature
  `func (s *Store) Create(title, context string, typ quest.Type, prio quest.Priority, tags map[string]string) (*quest.Quest, error)`.
  A later CLI/MCP phase and Task 3's tests call it. Empty `typ`/`prio` ⇒ defaults; non-empty invalid ⇒ error, nothing written.

**Note on scope:** This task changes an exported signature, so it MUST update every existing caller in the same commit or the build breaks. All current callers pass `(title, ctx, tags)` — insert `"", ""` before the final `tags` argument to opt into the defaults. `fmt` is already imported in `store.go`.

The complete list of current caller sites (line numbers may shift as you edit — match on the call text):
- `internal/store/store_test.go`: `Create("first", "", nil)`, `Create("second", "", nil)`, `Create("rand", "", nil)`, `Create("persist me", "ctx", map[string]string{"area": "engine"})`, `Create("concurrent", "", nil)`, `Create("alpha", "", nil)`, `Create("bravo", "", nil)`, `Create("finish me", "", nil)`, `Create("linkme", "", nil)`, `Create("one", "", nil)` (×2, in two different tests), `Create("two", "", nil)`, `Create("three", "", nil)`, `Create("no side effects", "", nil)`, `Create("target", "", nil)`.
- `internal/store/link_test.go`: `Create("close me", "", nil)`, `Create("ongoing", "", nil)`, `Create("hooked", "", nil)`.
- `cmd/side-quest/main_test.go`: `Create("ship it", "", nil)`.

- [ ] **Step 1: Write the failing behavior tests**

Add to `internal/store/store_test.go`:

```go
func TestCreateAppliesTypePriorityDefaults(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	q, err := s.Create("defaulted", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if q.Type != quest.TypeFeature {
		t.Errorf("default type: got %q want feature", q.Type)
	}
	if q.Priority != quest.PriorityLow {
		t.Errorf("default priority: got %q want low", q.Priority)
	}
}

func TestCreatePersistsExplicitTypePriority(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	q, err := s.Create("explicit", "", quest.TypeBug, quest.PriorityHigh, nil)
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := s.Get(q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Type != quest.TypeBug || reloaded.Priority != quest.PriorityHigh {
		t.Errorf("persisted type/priority wrong: %q/%q", reloaded.Type, reloaded.Priority)
	}
}

func TestCreateRejectsInvalidTypePriority(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("bad type", "", quest.Type("chore"), "", nil); err == nil {
		t.Error("expected error for invalid type")
	}
	if _, err := s.Create("bad prio", "", "", quest.Priority("urgent"), nil); err == nil {
		t.Error("expected error for invalid priority")
	}
	// Nothing should have been written by the rejected creates.
	snap, err := s.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.IDs) != 0 {
		t.Errorf("rejected creates wrote quests: %v", snap.IDs)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/store/ -run 'TestCreateAppliesTypePriorityDefaults|TestCreatePersistsExplicitTypePriority|TestCreateRejectsInvalidTypePriority' -v`
Expected: build failure — `Create` still has the old 3-argument signature, so these 5-argument calls do not compile.

- [ ] **Step 3: Change the `Create` signature, add coercion + validation, set the fields**

Replace the current `Create` method in `internal/store/store.go` with:

```go
// Create allocates an id and writes a new open quest. The quest file and the
// (possibly advanced) config are written in the SAME commit, so id allocation
// is atomic under the CAS loop: two racing lanes can never mint the same id —
// the loser's CAS fails and its rebuild sees the advanced counter / new files.
//
// typ and prio are required-with-defaults: an empty value is coerced to the
// package default; a non-empty but invalid value is rejected (nothing is
// written). This keeps quick capture a one-liner while guaranteeing every
// persisted quest carries a valid type and priority.
func (s *Store) Create(title, context string, typ quest.Type, prio quest.Priority, tags map[string]string) (*quest.Quest, error) {
	if typ == "" {
		typ = quest.DefaultType
	}
	if !typ.Valid() {
		return nil, fmt.Errorf("invalid type %q", typ)
	}
	if prio == "" {
		prio = quest.DefaultPriority
	}
	if !prio.Valid() {
		return nil, fmt.Errorf("invalid priority %q", prio)
	}
	now := time.Now().UTC().Truncate(time.Second)
	var created *quest.Quest
	err := s.mutate("side-quest: new quest", func(snap *Snapshot, tx *txn) error {
		id, cfg, err := allocID(snap)
		if err != nil {
			return err
		}
		q := &quest.Quest{
			ID:       id,
			Title:    title,
			Status:   quest.StatusOpen,
			Type:     typ,
			Priority: prio,
			Created:  now,
			Commits:  []string{},
			Context:  context,
			Tags:     tags,
		}
		data, err := quest.Marshal(q)
		if err != nil {
			return err
		}
		tx.put(questPath(id), data)
		cfgBytes, err := config.Marshal(cfg)
		if err != nil {
			return err
		}
		tx.put(configPath, cfgBytes)
		created = q
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}
```

- [ ] **Step 4: Update every existing caller to the new arity**

In `internal/store/store_test.go`, `internal/store/link_test.go`, and `cmd/side-quest/main_test.go`, add `"", ""` before the final argument of each `Create(...)` call. Examples:

- `s.Create("first", "", nil)` → `s.Create("first", "", "", "", nil)`
- `s.Create("persist me", "ctx", map[string]string{"area": "engine"})` → `s.Create("persist me", "ctx", "", "", map[string]string{"area": "engine"})`

Apply to all sites listed in the task header. Verify none remain: `grep -rn 'Create(' internal/store cmd/side-quest | grep -v '"", ""' | grep 'Create("'` should print nothing (every `Create("…"` call now contains `"", ""`).

- [ ] **Step 5: Run the store + cmd tests and the whole build**

Run: `go build ./... && go test ./internal/store/ ./cmd/... -v && gofmt -l internal cmd && go vet ./...`
Expected: build clean; all tests PASS (including the 3 new ones and every pre-existing test now compiling under the new arity); `gofmt -l` prints nothing; vet clean.

- [ ] **Step 6: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go internal/store/link_test.go cmd/side-quest/main_test.go
git commit -m "feat(store): Create takes type & priority (required-with-defaults)

Empty coerces to feature/low; a non-empty invalid value errors and writes
nothing. All existing callers updated to the new arity.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws"
```

---

### Task 3: `SetType`/`SetPriority` mutators + living docs

**Files:**
- Modify: `internal/store/store.go` (add two methods near `SetStatus`, currently around line 410)
- Test: `internal/store/store_test.go`
- Modify: `docs/architecture.md`
- Modify: `README.md`

**Interfaces:**
- Consumes from Task 1/2: `quest.Type`, `quest.Priority`, their `Valid()` methods, the `Quest.Type`/`Quest.Priority` fields, and `store.Update`.
- Produces: `func (s *Store) SetType(id string, t quest.Type) error` and `func (s *Store) SetPriority(id string, p quest.Priority) error`. Both validate then `Update`; an invalid value returns an error and does not mutate.

- [ ] **Step 1: Write the failing mutator tests**

Add to `internal/store/store_test.go`:

```go
func TestSetTypeAndPriority(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	q, err := s.Create("reclassify me", "", "", "", nil) // defaults: feature/low
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetType(q.ID, quest.TypeBug); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPriority(q.ID, quest.PriorityHigh); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != quest.TypeBug || got.Priority != quest.PriorityHigh {
		t.Errorf("after set: got %q/%q want bug/high", got.Type, got.Priority)
	}
}

func TestSetTypeAndPriorityRejectInvalid(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	q, err := s.Create("keep me", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetType(q.ID, quest.Type("chore")); err == nil {
		t.Error("expected error for invalid type")
	}
	if err := s.SetPriority(q.ID, quest.Priority("urgent")); err == nil {
		t.Error("expected error for invalid priority")
	}
	// The quest keeps its original defaulted values.
	got, err := s.Get(q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != quest.TypeFeature || got.Priority != quest.PriorityLow {
		t.Errorf("rejected sets mutated the quest: %q/%q", got.Type, got.Priority)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/store/ -run 'TestSetTypeAndPriority' -v`
Expected: build failure — `s.SetType` / `s.SetPriority` undefined.

- [ ] **Step 3: Add the mutators**

In `internal/store/store.go`, immediately after the `SetStatus` method, add:

```go
// SetType sets a quest's type after validating it.
func (s *Store) SetType(id string, t quest.Type) error {
	if !t.Valid() {
		return fmt.Errorf("invalid type %q", t)
	}
	return s.Update(id, func(q *quest.Quest) { q.Type = t })
}

// SetPriority sets a quest's priority after validating it.
func (s *Store) SetPriority(id string, p quest.Priority) error {
	if !p.Valid() {
		return fmt.Errorf("invalid priority %q", p)
	}
	return s.Update(id, func(q *quest.Quest) { q.Priority = p })
}
```

- [ ] **Step 4: Run the mutator tests to verify they pass**

Run: `go test ./internal/store/ -run 'TestSetTypeAndPriority' -v`
Expected: PASS (both).

- [ ] **Step 5: Update `docs/architecture.md`**

(a) After the paragraph ending "…so there is no second source of truth to drift." (the "id is the filename" paragraph, around line 43–44), add a new paragraph:

```markdown
Each quest's frontmatter carries `title`, `status` (open/partial/done/deferred/discarded),
`type` (bug/feature), `priority` (high/low), `created`, an optional `completed`, `commits`,
an optional `context`, and optional `tags`. `type` and `priority` are constrained enums with
defaults (`feature`/`low`) applied at creation; like `status`, they are validated only at the
write boundary (`Create`/`SetType`/`SetPriority`), never on read.
```

(b) In the CRUD paragraph (around line 182–184), update the Update list to include the two new mutators. Change:

```
implements Create (`Create`), Read (`Get`/`List`), and Update (`SetStatus`/`AddCommit`/
`Update`/`SetStrategy`).
```

to:

```
implements Create (`Create`), Read (`Get`/`List`), and Update (`SetStatus`/`SetType`/
`SetPriority`/`AddCommit`/`Update`/`SetStrategy`).
```

- [ ] **Step 6: Update `README.md`**

In the "Quick glossary" list (around lines 32–46), add a bullet immediately after the `CRUD` bullet:

```markdown
- **type / priority** — every quest carries a `type` (bug/feature) and a `priority`
  (high/low), constrained enums that default to feature/low when a quick capture omits them.
```

- [ ] **Step 7: Run the full suite + fmt/vet/build**

Run: `go build ./... && go test ./... && gofmt -l internal cmd && go vet ./...`
Expected: all packages PASS; `gofmt -l` prints nothing; vet and build clean.

- [ ] **Step 8: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go docs/architecture.md README.md
git commit -m "feat(store): SetType/SetPriority mutators; document type & priority

Validate-then-Update mirroring SetStatus. Architecture + README updated in
the same change (living docs).

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws"
```

---

## Self-Review (author checklist — completed)

**1. Spec coverage:**
- Model (Type/Priority named types, consts, Valid, defaults, struct fields) → Task 1. ✅
- Creation (required-with-defaults, signature grows by two params, empty→default, invalid→error) → Task 2. ✅
- Mutators (SetType/SetPriority mirroring SetStatus) → Task 3. ✅
- Validation boundary write-only, Unmarshal stays pure → enforced by Tasks 1–3 (no read coercion added anywhere). ✅
- Docs (architecture.md + README, same change) → Task 3. ✅
- Testing (Valid truth tables, round-trip, Create defaults/explicit/invalid, Set valid/invalid) → Tasks 1–3. ✅
- Out-of-scope items (CLI/filters/MCP/importer) → excluded, stated in Global Constraints. ✅

**2. Placeholder scan:** No TBD/TODO/"handle edge cases"/"similar to". Every code step shows complete code. ✅

**3. Type consistency:** `Type`/`Priority`/`TypeBug`/`TypeFeature`/`PriorityHigh`/`PriorityLow`/`DefaultType`/`DefaultPriority`, the `Create(title, context string, typ quest.Type, prio quest.Priority, tags map[string]string)` signature, and `SetType(id string, t quest.Type)` / `SetPriority(id string, p quest.Priority)` are used identically across all three tasks. Struct field order (Status → Type → Priority → Created) is consistent between the Task 1 struct edit and the Task 2 `Create` literal. ✅
