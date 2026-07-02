# side-quest Phase 1: Core Store + IDs — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the core Go library that stores quests on the `refs/side-quest/quests` orphan ref with compare-and-swap safety and configurable sequential/random id allocation.

**Architecture:** Four internal packages. `quest` and `config` are pure (model + YAML (de)serialization). `gitcmd` is a thin subprocess wrapper over the `git` binary. `store` composes them: it reads/writes the orphan ref purely through git plumbing (temp-index `read-tree`/`hash-object`/`write-tree`/`commit-tree`/`update-ref`), never touching the working tree, and retries under a CAS loop so concurrent worktree lanes are safe.

**Tech Stack:** Go (module `github.com/sharkusk/side-quest`); `gopkg.in/yaml.v3` for YAML; the system `git` binary (invoked as a subprocess); Go's standard `testing` package.

**Prerequisite:** Go toolchain installed (`go version` must work). Not currently installed on the author's machine — install Go ≥1.22 before executing this plan.

## Global Constraints

Copied from the design spec (`docs/superpowers/specs/2026-07-02-side-quest-design.md`). Every task implicitly includes these:

- **Language:** Go. Single module `github.com/sharkusk/side-quest`. Packages under `internal/`.
- **The id is the filename** (`quests/SQ-0001.md` → `SQ-0001`). The id is **never** stored in frontmatter (spec §5.5). The store attaches it on read.
- **Five statuses only:** `open`, `partial`, `done`, `deferred`, `discarded` (spec §6).
- **Ref:** `refs/side-quest/quests` — custom namespace, never checked out, mutated only via plumbing (spec §5).
- **CAS safety:** every mutation commits via `update-ref` compare-and-swap and retries on a lost race (spec §5.3). Never take a lock; never touch the working tree or the user's index.
- **Teaching-quality docs:** the author's comfort languages are C and Python; Go is new to them. Doc comments explain intent and call out Go idioms that differ from C/Python (multi-return + `error` values instead of exceptions, `defer`, slices, zero values, pointer vs value receivers). Prefer plain readable code over terse idiomatic Go; keep files small (spec §21).
- **Minimal schema:** core fields `title`, `status`, `created`, `completed`, `commits`; optional `context`; everything else is `tags` (spec §6).

---

## File Structure

- `go.mod`, `go.sum` — module definition + locked deps.
- `internal/gitcmd/gitcmd.go` — subprocess wrapper (`Git` type, `Run`/`RunRaw`/`RunInput`, `WithEnv`).
- `internal/quest/quest.go` — `Quest` struct, `Status` type, `Marshal`/`Unmarshal`.
- `internal/config/config.go` — `Config` struct, `Strategy`/`Tone` types, `Default`/`Marshal`/`Unmarshal`.
- `internal/store/store.go` — `Store`: `Open`, `Init`, `Create`, `Get`, `List`, `SetStatus`, `Update`, `AddCommit`, `SetStrategy`; internal `snapshot`/`mutate`/`buildCommit`/`cas`/`allocID`.
- Tests colocated: `*_test.go` beside each package (`internal/store/store_test.go` etc.).

---

## Task 1: Module scaffold

**Files:**
- Create: `go.mod`
- Create: `internal/gitcmd/doc_test.go` (temporary smoke test, deleted in Task 2)

**Interfaces:**
- Produces: a compiling module so `go test ./...` runs.

- [ ] **Step 1: Create the module**

Run:
```bash
cd /Volumes/Videos/Source/side-quest
go mod init github.com/sharkusk/side-quest
go mod edit -go=1.22
```

Expected: `go.mod` created containing `module github.com/sharkusk/side-quest` and `go 1.22`.

- [ ] **Step 2: Add a smoke test to prove the toolchain runs**

Create `internal/gitcmd/doc_test.go`:
```go
package gitcmd

import "testing"

// TestToolchain is a placeholder proving `go test` works; removed in Task 2.
func TestToolchain(t *testing.T) {
	if 1+1 != 2 {
		t.Fatal("math is broken")
	}
}
```

- [ ] **Step 3: Run it**

Run: `go test ./...`
Expected: PASS (`ok  github.com/sharkusk/side-quest/internal/gitcmd`).

- [ ] **Step 4: Commit**

```bash
git add go.mod internal/gitcmd/doc_test.go
git commit -m "chore: initialize Go module + toolchain smoke test"
```

---

## Task 2: `gitcmd` subprocess wrapper

Thin, typed wrapper over the `git` binary. Every other package reaches git through this, so plumbing invariants live in one place.

**Files:**
- Create: `internal/gitcmd/gitcmd.go`
- Create: `internal/gitcmd/gitcmd_test.go`
- Delete: `internal/gitcmd/doc_test.go`

**Interfaces:**
- Produces:
  - `type Git struct{ ... }`
  - `func New(dir string) *Git`
  - `func (g *Git) WithEnv(kv ...string) *Git`
  - `func (g *Git) Run(args ...string) (string, error)` — trimmed stdout
  - `func (g *Git) RunRaw(args ...string) ([]byte, error)` — untrimmed stdout
  - `func (g *Git) RunInput(stdin string, args ...string) (string, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/gitcmd/gitcmd_test.go`:
```go
package gitcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// initRepo makes an empty git repo in a temp dir and returns its path.
func initRepo(t *testing.T) string {
	t.Helper() // marks this as a helper so failures report the caller's line
	dir := t.TempDir()
	g := New(dir)
	if _, err := g.Run("init", "-q"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if _, err := g.Run("config", "user.email", "t@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run("config", "user.name", "Tester"); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRunTrimsStdout(t *testing.T) {
	dir := initRepo(t)
	out, err := New(dir).Run("rev-parse", "--is-inside-work-tree")
	if err != nil {
		t.Fatal(err)
	}
	if out != "true" { // Run trims the trailing newline git prints
		t.Fatalf("got %q, want %q", out, "true")
	}
}

func TestRunRawPreservesBytes(t *testing.T) {
	dir := initRepo(t)
	// hash-object of known content, then cat-file -p should return it verbatim.
	g := New(dir)
	blob, err := g.RunInput("hello\n", "hash-object", "-w", "--stdin")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := g.RunRaw("cat-file", "-p", blob)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "hello\n" { // RunRaw must NOT trim the trailing newline
		t.Fatalf("got %q, want %q", string(raw), "hello\n")
	}
}

func TestRunErrorIncludesStderr(t *testing.T) {
	dir := initRepo(t)
	_, err := New(dir).Run("cat-file", "-p", "deadbeef")
	if err == nil {
		t.Fatal("expected error for missing object")
	}
	if !strings.Contains(err.Error(), "cat-file") {
		t.Fatalf("error should name the command, got: %v", err)
	}
}

func TestWithEnvSetsGitIndexFile(t *testing.T) {
	dir := initRepo(t)
	idx := filepath.Join(t.TempDir(), "scratch-index")
	g := New(dir).WithEnv("GIT_INDEX_FILE=" + idx)
	// Stage nothing but write the (empty) tree using the scratch index.
	if _, err := g.Run("read-tree", "--empty"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run("write-tree"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(idx); err != nil {
		t.Fatalf("scratch index file not created: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/gitcmd/`
Expected: FAIL — `undefined: New` (and the other symbols).

- [ ] **Step 3: Implement `gitcmd`**

Create `internal/gitcmd/gitcmd.go`:
```go
// Package gitcmd is a thin wrapper over the system `git` binary. All git
// interaction in side-quest goes through it, so subprocess handling and error
// formatting live in exactly one place.
//
// Go note (for C/Python readers): methods here return (value, error) pairs.
// Go has no exceptions — the error is an ordinary second return value the
// caller must check. A nil error means success.
package gitcmd

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Git runs git commands in a fixed directory, optionally with extra
// environment variables (used to point GIT_INDEX_FILE at a scratch index so we
// never disturb the user's real index).
type Git struct {
	dir string   // working directory git runs in
	env []string // extra "KEY=VALUE" entries appended to the inherited env
}

// New returns a Git bound to dir.
func New(dir string) *Git { return &Git{dir: dir} }

// WithEnv returns a COPY of g with additional environment variables. We copy so
// callers can layer env without mutating a shared value (Go structs are copied
// on assignment; `cp := *g` duplicates the struct).
func (g *Git) WithEnv(kv ...string) *Git {
	cp := *g
	cp.env = append(append([]string{}, g.env...), kv...)
	return &cp
}

// run is the single execution path. It returns raw stdout bytes; the exported
// helpers decide whether to trim.
func (g *Git) run(stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.dir
	if len(g.env) > 0 {
		cmd.Env = append(cmd.Environ(), g.env...) // Environ() = inherited env
	}
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %v: %s",
			strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

// Run executes git and returns stdout with the trailing newline trimmed
// (convenient for shas, refs, single-line output).
func (g *Git) Run(args ...string) (string, error) {
	b, err := g.run(nil, args...)
	return strings.TrimRight(string(b), "\n"), err
}

// RunRaw returns stdout untrimmed. Use for file contents where a trailing
// newline is significant (e.g. `cat-file -p`).
func (g *Git) RunRaw(args ...string) ([]byte, error) {
	return g.run(nil, args...)
}

// RunInput feeds stdin to git (e.g. `hash-object --stdin`) and returns trimmed
// stdout.
func (g *Git) RunInput(stdin string, args ...string) (string, error) {
	b, err := g.run([]byte(stdin), args...)
	return strings.TrimRight(string(b), "\n"), err
}
```

- [ ] **Step 4: Delete the placeholder smoke test**

```bash
rm internal/gitcmd/doc_test.go
```

- [ ] **Step 5: Run tests to verify pass**

Run: `go test ./internal/gitcmd/`
Expected: PASS (4 tests).

- [ ] **Step 6: Commit**

```bash
git add internal/gitcmd/
git commit -m "feat(gitcmd): subprocess wrapper for git plumbing"
```

---

## Task 3: `quest` model + frontmatter

Pure package: the `Quest` struct and Markdown-with-YAML-frontmatter (de)serialization. No git, no I/O.

**Files:**
- Create: `internal/quest/quest.go`
- Create: `internal/quest/quest_test.go`

**Interfaces:**
- Produces:
  - `type Status string` with consts `StatusOpen/StatusPartial/StatusDone/StatusDeferred/StatusDiscarded` and `func (Status) Valid() bool`
  - `type Quest struct { ID, Title string; Status Status; Created time.Time; Completed *time.Time; Commits []string; Context string; Tags map[string]string; Body string }`
  - `func Marshal(q *Quest) ([]byte, error)`
  - `func Unmarshal(id string, data []byte) (*Quest, error)`

- [ ] **Step 1: Add the YAML dependency**

Run: `go get gopkg.in/yaml.v3`
Expected: `go.mod`/`go.sum` updated with `gopkg.in/yaml.v3`.

- [ ] **Step 2: Write the failing tests**

Create `internal/quest/quest_test.go`:
```go
package quest

import (
	"strings"
	"testing"
	"time"
)

func TestStatusValid(t *testing.T) {
	for _, s := range []Status{StatusOpen, StatusPartial, StatusDone, StatusDeferred, StatusDiscarded} {
		if !s.Valid() {
			t.Errorf("%q should be valid", s)
		}
	}
	if Status("bogus").Valid() {
		t.Error("bogus should be invalid")
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	created := time.Date(2026, 7, 2, 14, 3, 11, 0, time.UTC)
	q := &Quest{
		ID:      "SQ-0001", // must NOT appear in the serialized bytes
		Title:   "Crash stack-trace diagnostic",
		Status:  StatusOpen,
		Created: created,
		Commits: []string{"a62d4fa"},
		Context: "branch=main head=a62d4fa\nCaptured while debugging.",
		Tags:    map[string]string{"area": "engine"},
		Body:    "Full prose description.\nWith two lines.",
	}
	data, err := Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "SQ-0001") {
		t.Fatal("id must not be serialized into the file (filename is the id)")
	}
	if !strings.HasPrefix(string(data), "---\n") {
		t.Fatalf("expected leading frontmatter fence, got:\n%s", data)
	}

	got, err := Unmarshal("SQ-0001", data)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "SQ-0001" {
		t.Errorf("id: got %q want SQ-0001 (from filename)", got.ID)
	}
	if got.Title != q.Title || got.Status != q.Status {
		t.Errorf("title/status mismatch: %+v", got)
	}
	if !got.Created.Equal(created) {
		t.Errorf("created: got %v want %v", got.Created, created)
	}
	if len(got.Commits) != 1 || got.Commits[0] != "a62d4fa" {
		t.Errorf("commits mismatch: %v", got.Commits)
	}
	if got.Tags["area"] != "engine" {
		t.Errorf("tags mismatch: %v", got.Tags)
	}
	if got.Body != q.Body {
		t.Errorf("body: got %q want %q", got.Body, q.Body)
	}
}

func TestUnmarshalRejectsMissingFence(t *testing.T) {
	_, err := Unmarshal("SQ-0001", []byte("no frontmatter here"))
	if err == nil {
		t.Fatal("expected error for missing frontmatter fence")
	}
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./internal/quest/`
Expected: FAIL — undefined symbols.

- [ ] **Step 4: Implement `quest`**

Create `internal/quest/quest.go`:
```go
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
```

- [ ] **Step 5: Run tests to verify pass**

Run: `go test ./internal/quest/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/quest/ go.mod go.sum
git commit -m "feat(quest): model + frontmatter (de)serialization"
```

---

## Task 4: `config` model

Pure package: the on-ref `_config.yaml` model with defaults.

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

**Interfaces:**
- Produces:
  - `type Strategy string` (`Sequential`, `Random`)
  - `type Tone string` (`TonePlain`, `ToneDCC`, `ToneDCCSuperfan`)
  - `type Config struct { IDPrefix string; IDStrategy Strategy; SeqNext int; SeqWidth int; Tone Tone; AutoTrailer bool }`
  - `func Default() Config`
  - `func Marshal(c Config) ([]byte, error)`
  - `func Unmarshal(data []byte) (Config, error)` — starts from Default so missing keys are filled

- [ ] **Step 1: Write the failing tests**

Create `internal/config/config_test.go`:
```go
package config

import "testing"

func TestDefault(t *testing.T) {
	c := Default()
	if c.IDPrefix != "SQ" || c.IDStrategy != Sequential || c.SeqNext != 1 ||
		c.SeqWidth != 4 || c.Tone != ToneDCC || !c.AutoTrailer {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestRoundTrip(t *testing.T) {
	in := Default()
	in.IDStrategy = Random
	in.SeqNext = 42
	data, err := Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if out.IDStrategy != Random || out.SeqNext != 42 {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
}

func TestUnmarshalFillsMissingFromDefault(t *testing.T) {
	// Only id_strategy present; the rest must come from Default().
	out, err := Unmarshal([]byte("id_strategy: random\n"))
	if err != nil {
		t.Fatal(err)
	}
	if out.IDStrategy != Random {
		t.Errorf("id_strategy not parsed: %+v", out)
	}
	if out.IDPrefix != "SQ" || out.SeqWidth != 4 || out.Tone != ToneDCC {
		t.Errorf("missing keys not defaulted: %+v", out)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/config/`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Implement `config`**

Create `internal/config/config.go`:
```go
// Package config models the on-ref configuration stored at _config.yaml
// (spec §7, §12). It lives on the ref so every worktree and clone agrees on
// the id strategy, counter, and message tone.
package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Strategy selects how new ids are allocated.
type Strategy string

const (
	Sequential Strategy = "sequential" // SQ-0001, SQ-0002, ... (counter-based)
	Random     Strategy = "random"     // SQ-a3f9c2 (6 hex)
)

// Tone selects the message register (used by the voice layer in a later phase).
type Tone string

const (
	TonePlain       Tone = "plain"
	ToneDCC         Tone = "dcc"
	ToneDCCSuperfan Tone = "dcc-superfan"
)

// Config is the persisted configuration.
type Config struct {
	IDPrefix    string   `yaml:"id_prefix"`
	IDStrategy  Strategy `yaml:"id_strategy"`
	SeqNext     int      `yaml:"seq_next"`
	SeqWidth    int      `yaml:"seq_width"`
	Tone        Tone     `yaml:"tone"`
	AutoTrailer bool     `yaml:"auto_trailer"`
}

// Default returns the configuration a freshly-initialized project starts with.
func Default() Config {
	return Config{
		IDPrefix:    "SQ",
		IDStrategy:  Sequential,
		SeqNext:     1,
		SeqWidth:    4,
		Tone:        ToneDCC,
		AutoTrailer: true,
	}
}

// Marshal renders c to YAML bytes.
func Marshal(c Config) ([]byte, error) { return yaml.Marshal(c) }

// Unmarshal parses YAML into a Config, starting from Default() so that keys
// absent in the file take their default value (forward/backward compatibility).
func Unmarshal(data []byte) (Config, error) {
	c := Default()
	if err := yaml.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	return c, nil
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/config/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat(config): on-ref config model with defaults"
```

---

## Task 5: `store` — open, snapshot, init

Repo discovery, reading the ref tip, a read-only `Snapshot`, and `Init` (which exercises the mutation path once). This task also introduces the shared test helper.

**Files:**
- Create: `internal/store/store.go`
- Create: `internal/store/store_test.go`

**Interfaces:**
- Consumes: `gitcmd.New/Run/RunRaw/RunInput/WithEnv`, `config.Default/Marshal/Unmarshal`, `quest.Unmarshal`.
- Produces:
  - `const Ref = "refs/side-quest/quests"`
  - `type Store struct{ ... }`
  - `func Open(dir string) (*Store, error)`
  - `type Snapshot struct { Tip string; Config config.Config; IDs []string }`
  - `func (s *Store) Init() error`
  - internal: `tip()`, `snapshot()`, `readFile()`, `listIDs()`, `questPath()`

- [ ] **Step 1: Write the failing tests**

Create `internal/store/store_test.go`:
```go
package store

import (
	"testing"

	"github.com/sharkusk/side-quest/internal/config"
	"github.com/sharkusk/side-quest/internal/gitcmd"
)

// newStore creates a throwaway git repo with a committer identity and returns
// an opened Store. Integration tests run against real git plumbing.
func newStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	g := gitcmd.New(dir)
	if _, err := g.Run("init", "-q"); err != nil {
		t.Fatal(err)
	}
	// commit-tree needs an identity; set it locally in the temp repo.
	if _, err := g.Run("config", "user.email", "t@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run("config", "user.name", "Tester"); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSnapshotEmptyBeforeInit(t *testing.T) {
	s := newStore(t)
	snap, err := s.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Tip != "" {
		t.Errorf("expected empty tip before init, got %q", snap.Tip)
	}
	if len(snap.IDs) != 0 {
		t.Errorf("expected no ids, got %v", snap.IDs)
	}
	// Defaults apply when no config exists yet.
	if snap.Config.IDStrategy != config.Sequential {
		t.Errorf("expected default strategy, got %v", snap.Config.IDStrategy)
	}
}

func TestInitCreatesRefAndConfig(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	snap, err := s.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Tip == "" {
		t.Fatal("ref not created by Init")
	}
	if snap.Config.SeqNext != 1 || snap.Config.IDPrefix != "SQ" {
		t.Errorf("unexpected initialized config: %+v", snap.Config)
	}
}

func TestInitTwiceFails(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.Init(); err == nil {
		t.Fatal("second Init should fail (already initialized)")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/store/`
Expected: FAIL — `undefined: Open` etc. (Note: `Init` uses `mutate`, added in this task's implementation.)

- [ ] **Step 3: Implement the read side + mutation plumbing + Init**

Create `internal/store/store.go`:
```go
// Package store persists quests on the orphan ref refs/side-quest/quests.
//
// It never checks the ref out into the working tree. Reads use `cat-file` /
// `ls-tree`; writes build a new commit through a SCRATCH index
// (read-tree -> hash-object -> update-index -> write-tree -> commit-tree) and
// move the ref with `update-ref` compare-and-swap, retrying on a lost race so
// concurrent worktree lanes need no lock (spec §5).
package store

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sharkusk/side-quest/internal/config"
	"github.com/sharkusk/side-quest/internal/gitcmd"
	"github.com/sharkusk/side-quest/internal/quest"
)

const (
	Ref        = "refs/side-quest/quests"
	configPath = "_config.yaml"
	questDir   = "quests"
)

// ErrNotFound is returned when a quest id has no file on the ref.
var ErrNotFound = errors.New("quest not found")

// Store is bound to one git repository.
type Store struct {
	git    *gitcmd.Git
	gitDir string // absolute .git dir, where scratch index files are created
}

// Open finds the git repo containing dir and returns a Store for it.
func Open(dir string) (*Store, error) {
	probe := gitcmd.New(dir)
	top, err := probe.Run("rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("not a git repository: %w", err)
	}
	gitDir, err := probe.Run("rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, err
	}
	return &Store{git: gitcmd.New(top), gitDir: gitDir}, nil
}

func questPath(id string) string { return questDir + "/" + id + ".md" }

// tip returns the commit the ref points at, or "" if the ref does not exist.
// `for-each-ref` exits 0 and prints nothing for a missing ref, which is how we
// distinguish "empty store" from a real error.
func (s *Store) tip() (string, error) {
	out, err := s.git.Run("for-each-ref", "--format=%(objectname)", Ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// readFile returns the bytes of path in the tree at commit tip.
func (s *Store) readFile(tip, path string) ([]byte, error) {
	return s.git.RunRaw("cat-file", "-p", tip+":"+path)
}

// listIDs returns the sorted quest ids present at tip (filenames minus ".md").
func (s *Store) listIDs(tip string) ([]string, error) {
	out, err := s.git.Run("ls-tree", "--name-only", tip+":"+questDir)
	if err != nil {
		// Missing quests/ directory => no quests yet.
		return nil, nil
	}
	var ids []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasSuffix(line, ".md") {
			ids = append(ids, strings.TrimSuffix(line, ".md"))
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// Snapshot is a read-only view of the store at a specific ref tip.
type Snapshot struct {
	Tip    string // "" when the ref does not exist yet
	Config config.Config
	IDs    []string
}

func (s *Store) snapshot() (*Snapshot, error) {
	tip, err := s.tip()
	if err != nil {
		return nil, err
	}
	snap := &Snapshot{Tip: tip, Config: config.Default()}
	if tip == "" {
		return snap, nil
	}
	if raw, err := s.readFile(tip, configPath); err == nil {
		cfg, err := config.Unmarshal(raw)
		if err != nil {
			return nil, err
		}
		snap.Config = cfg
	}
	ids, err := s.listIDs(tip)
	if err != nil {
		return nil, err
	}
	snap.IDs = ids
	return snap, nil
}

// --- mutation transaction -------------------------------------------------

// txn accumulates the file changes for one commit.
type txn struct {
	puts    map[string][]byte
	deletes map[string]bool
}

func newTxn() *txn {
	return &txn{puts: map[string][]byte{}, deletes: map[string]bool{}}
}

func (t *txn) put(path string, data []byte) {
	t.puts[path] = data
	delete(t.deletes, path)
}

func (t *txn) del(path string) {
	t.deletes[path] = true
	delete(t.puts, path)
}

// mutate runs build against the current snapshot, commits the staged changes,
// and moves the ref via CAS. If another writer advanced the ref first, it
// retries build against the fresh snapshot. build MUST be deterministic given
// the snapshot (it may run several times).
func (s *Store) mutate(msg string, build func(snap *Snapshot, tx *txn) error) error {
	const maxTries = 10
	for try := 0; try < maxTries; try++ {
		snap, err := s.snapshot()
		if err != nil {
			return err
		}
		tx := newTxn()
		if err := build(snap, tx); err != nil {
			return err
		}
		commit, err := s.buildCommit(snap.Tip, msg, tx)
		if err != nil {
			return err
		}
		ok, err := s.cas(snap.Tip, commit)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		// Lost the race: loop and rebuild against the new tip.
	}
	return fmt.Errorf("store: ref %s stayed contended after %d tries", Ref, maxTries)
}

// buildCommit stages tx into a scratch index and returns a new commit whose
// parent is `parent` ("" for the first, parentless commit).
func (s *Store) buildCommit(parent, msg string, tx *txn) (string, error) {
	idxFile, err := os.CreateTemp(s.gitDir, "sq-index-*")
	if err != nil {
		return "", err
	}
	idxPath := idxFile.Name()
	idxFile.Close()
	defer os.Remove(idxPath) // defer runs on return, like a Python finally block

	g := s.git.WithEnv("GIT_INDEX_FILE=" + idxPath)

	if parent != "" {
		if _, err := g.Run("read-tree", parent); err != nil {
			return "", err
		}
	} else {
		if _, err := g.Run("read-tree", "--empty"); err != nil {
			return "", err
		}
	}
	for path, data := range tx.puts {
		blob, err := g.RunInput(string(data), "hash-object", "-w", "--stdin")
		if err != nil {
			return "", err
		}
		if _, err := g.Run("update-index", "--add", "--cacheinfo",
			"100644,"+blob+","+path); err != nil {
			return "", err
		}
	}
	for path := range tx.deletes {
		if _, err := g.Run("update-index", "--force-remove", path); err != nil {
			return "", err
		}
	}
	tree, err := g.Run("write-tree")
	if err != nil {
		return "", err
	}
	args := []string{"commit-tree", tree, "-m", msg}
	if parent != "" {
		args = []string{"commit-tree", tree, "-p", parent, "-m", msg}
	}
	return g.Run(args...)
}

// cas points the ref at newCommit only if it currently equals oldTip (or does
// not exist, when oldTip is ""). Returns (false, nil) when update-ref rejects
// the move because the ref changed — a retryable lost race.
func (s *Store) cas(oldTip, newCommit string) (bool, error) {
	old := oldTip
	// An empty oldvalue tells update-ref the ref must not currently exist.
	if _, err := s.git.Run("update-ref", Ref, newCommit, old); err != nil {
		return false, nil
	}
	return true, nil
}

// Init creates the ref with a default config. Errors if already initialized.
func (s *Store) Init() error {
	tip, err := s.tip()
	if err != nil {
		return err
	}
	if tip != "" {
		return errors.New("side-quest already initialized")
	}
	cfgBytes, err := config.Marshal(config.Default())
	if err != nil {
		return err
	}
	return s.mutate("side-quest: init", func(snap *Snapshot, tx *txn) error {
		tx.put(configPath, cfgBytes)
		return nil
	})
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/store/`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat(store): repo open, snapshot, CAS mutation plumbing, init"
```

---

## Task 6: `store` — id allocation + Create

Allocate ids per strategy (sequential advances the on-ref counter within the same commit; random uses 6 hex with an existence guard) and write new quests. Includes a CAS-retry test.

**Files:**
- Modify: `internal/store/store.go` (add `allocID`, `randomID`, `Create`)
- Modify: `internal/store/store_test.go` (add tests)

**Interfaces:**
- Consumes: `mutate`, `Snapshot`, `quest.Marshal`, `config.Marshal`.
- Produces:
  - `func (s *Store) Create(title, context string, tags map[string]string) (*quest.Quest, error)`
  - internal: `allocID(snap *Snapshot) (string, config.Config, error)`, `randomID(prefix string) (string, error)`

- [ ] **Step 1: Write the failing tests**

Add to `internal/store/store_test.go`:
```go
func TestCreateSequential(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	a, err := s.Create("first", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Create("second", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != "SQ-0001" || b.ID != "SQ-0002" {
		t.Fatalf("sequential ids wrong: %q, %q", a.ID, b.ID)
	}
	// Counter must have advanced on the ref.
	snap, _ := s.snapshot()
	if snap.Config.SeqNext != 3 {
		t.Errorf("seq_next: got %d want 3", snap.Config.SeqNext)
	}
}

func TestCreateRandom(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStrategyForTest(t); err != nil { // helper flips to random
		t.Fatal(err)
	}
	q, err := s.Create("rand", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// SQ- + 6 hex chars.
	if len(q.ID) != len("SQ-")+6 {
		t.Fatalf("random id wrong shape: %q", q.ID)
	}
}

func TestCreatePersistsAndReloads(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	created, err := s.Create("persist me", "ctx", map[string]string{"area": "engine"})
	if err != nil {
		t.Fatal(err)
	}
	// Re-open the same repo to prove it was persisted on the ref.
	s2 := s // same dir; snapshot reads from git, not memory
	snap, err := s2.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.IDs) != 1 || snap.IDs[0] != created.ID {
		t.Fatalf("persisted ids wrong: %v", snap.IDs)
	}
	raw, err := s2.readFile(snap.Tip, questPath(created.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "title: persist me") {
		t.Fatalf("quest content not persisted: %s", raw)
	}
}
```

Add the small test helper at the bottom of the test file (kept out of production code):
```go
// SetStrategyForTest flips the on-ref strategy to random by rewriting config.
// (A public SetStrategy lands in Task 7; this keeps Task 6's test self-contained.)
func (s *Store) SetStrategyForTest(t *testing.T) error {
	t.Helper()
	return s.mutate("test: strategy random", func(snap *Snapshot, tx *txn) error {
		cfg := snap.Config
		cfg.IDStrategy = config.Random
		b, err := config.Marshal(cfg)
		if err != nil {
			return err
		}
		tx.put(configPath, b)
		return nil
	})
}
```

> Note: `strings` is already imported by the test file via Task 5? It is not — add `"strings"` to the test imports now.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/store/ -run TestCreate`
Expected: FAIL — `undefined: Create` (and `s.Create`).

- [ ] **Step 3: Implement allocation + Create**

Add to `internal/store/store.go` (imports: add `crypto/rand`, `encoding/hex`, `time`):
```go
// allocID picks the next free id for the snapshot's strategy and returns it
// together with the config to persist (seq_next advanced, for sequential). The
// existence check guarantees the id collides with no current file — so even a
// fluke all-numeric random id can never clash with a sequential one (spec §7).
func allocID(snap *Snapshot) (string, config.Config, error) {
	cfg := snap.Config
	existing := make(map[string]bool, len(snap.IDs))
	for _, id := range snap.IDs {
		existing[id] = true
	}
	switch cfg.IDStrategy {
	case config.Random:
		for i := 0; i < 100; i++ {
			id, err := randomID(cfg.IDPrefix)
			if err != nil {
				return "", cfg, err
			}
			if !existing[id] {
				return id, cfg, nil
			}
		}
		return "", cfg, errors.New("could not find a free random id in 100 tries")
	default: // sequential
		n := cfg.SeqNext
		for {
			id := fmt.Sprintf("%s-%0*d", cfg.IDPrefix, cfg.SeqWidth, n)
			if !existing[id] {
				cfg.SeqNext = n + 1
				return id, cfg, nil
			}
			n++
		}
	}
}

// randomID returns prefix + "-" + 6 lowercase hex chars (3 random bytes).
func randomID(prefix string) (string, error) {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return prefix + "-" + hex.EncodeToString(b[:]), nil
}

// Create allocates an id and writes a new open quest. The quest file and the
// (possibly advanced) config are written in the SAME commit, so id allocation
// is atomic under the CAS loop: two racing lanes can never mint the same id —
// the loser's CAS fails and its rebuild sees the advanced counter / new files.
func (s *Store) Create(title, context string, tags map[string]string) (*quest.Quest, error) {
	now := time.Now().UTC().Truncate(time.Second)
	var created *quest.Quest
	err := s.mutate("side-quest: new quest", func(snap *Snapshot, tx *txn) error {
		id, cfg, err := allocID(snap)
		if err != nil {
			return err
		}
		q := &quest.Quest{
			ID:      id,
			Title:   title,
			Status:  quest.StatusOpen,
			Created: now,
			Commits: []string{},
			Context: context,
			Tags:    tags,
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

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/store/`
Expected: PASS.

- [ ] **Step 5: Add a CAS-retry regression test**

Add to `internal/store/store_test.go`:
```go
// TestCreateConcurrentNoDuplicateIDs launches several creates concurrently and
// asserts every id is unique — exercising the CAS retry loop under contention.
func TestCreateConcurrentNoDuplicateIDs(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	const n = 8
	ids := make(chan string, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() { // goroutine: a lightweight concurrent function (like a green thread)
			q, err := s.Create("concurrent", "", nil)
			if err != nil {
				errs <- err
				return
			}
			ids <- q.ID
		}()
	}
	seen := map[string]bool{}
	for i := 0; i < n; i++ {
		select {
		case err := <-errs:
			t.Fatal(err)
		case id := <-ids:
			if seen[id] {
				t.Fatalf("duplicate id allocated: %q", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != n {
		t.Fatalf("expected %d unique ids, got %d", n, len(seen))
	}
}
```

- [ ] **Step 6: Run the concurrency test (with the race detector)**

Run: `go test ./internal/store/ -race -run TestCreateConcurrent`
Expected: PASS, no race warnings.

> If this flakes because separate `Store` values share the repo fine but the test reuses one `*Store` across goroutines: that is intended — `Store` holds no mutable state, only the git dir, so it is safe to share. The safety comes from git's ref CAS, not in-process locks.

- [ ] **Step 7: Commit**

```bash
git add internal/store/
git commit -m "feat(store): id allocation (sequential/random) + Create, CAS-safe"
```

---

## Task 7: `store` — Get, List, SetStatus, Update, AddCommit, SetStrategy

The remaining read/update surface the later phases (CLI, MCP, hooks) build on.

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/store_test.go`

**Interfaces:**
- Produces:
  - `func (s *Store) Get(id string) (*quest.Quest, error)` — `ErrNotFound` if absent
  - `func (s *Store) List() ([]*quest.Quest, error)` — sorted by id
  - `func (s *Store) SetStatus(id string, st quest.Status) error`
  - `func (s *Store) Update(id string, apply func(*quest.Quest)) error`
  - `func (s *Store) AddCommit(id, sha string, complete bool) error`
  - `func (s *Store) SetStrategy(st config.Strategy) error`

- [ ] **Step 1: Write the failing tests**

Add to `internal/store/store_test.go`:
```go
import ( // ensure quest is imported in the test file
	"github.com/sharkusk/side-quest/internal/quest"
)

func TestGetAndList(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	a, _ := s.Create("alpha", "", nil)
	b, _ := s.Create("bravo", "", nil)

	got, err := s.Get(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "alpha" {
		t.Errorf("Get returned wrong quest: %+v", got)
	}
	if _, err := s.Get("SQ-9999"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != a.ID || list[1].ID != b.ID {
		t.Fatalf("List wrong: %v", list)
	}
}

func TestSetStatusSetsCompletedOnDone(t *testing.T) {
	s := newStore(t)
	_ = s.Init()
	q, _ := s.Create("finish me", "", nil)

	if err := s.SetStatus(q.ID, quest.StatusDone); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(q.ID)
	if got.Status != quest.StatusDone {
		t.Errorf("status not set: %v", got.Status)
	}
	if got.Completed == nil {
		t.Error("completed timestamp should be set when moving to done")
	}

	if err := s.SetStatus(q.ID, quest.Status("bogus")); err == nil {
		t.Error("invalid status should error")
	}
}

func TestAddCommitAppendsAndDedupes(t *testing.T) {
	s := newStore(t)
	_ = s.Init()
	q, _ := s.Create("linkme", "", nil)

	if err := s.AddCommit(q.ID, "abc123", false); err != nil {
		t.Fatal(err)
	}
	if err := s.AddCommit(q.ID, "abc123", false); err != nil { // duplicate
		t.Fatal(err)
	}
	if err := s.AddCommit(q.ID, "def456", true); err != nil { // completing link
		t.Fatal(err)
	}
	got, _ := s.Get(q.ID)
	if len(got.Commits) != 2 {
		t.Fatalf("commits not deduped: %v", got.Commits)
	}
	if got.Status != quest.StatusDone || got.Completed == nil {
		t.Errorf("completing link should mark done: %+v", got)
	}
}

func TestSetStrategyPreservesSeqNext(t *testing.T) {
	s := newStore(t)
	_ = s.Init()
	_, _ = s.Create("one", "", nil) // seq_next -> 2

	if err := s.SetStrategy(config.Random); err != nil {
		t.Fatal(err)
	}
	snap, _ := s.snapshot()
	if snap.Config.IDStrategy != config.Random {
		t.Errorf("strategy not switched: %v", snap.Config.IDStrategy)
	}
	if snap.Config.SeqNext != 2 {
		t.Errorf("seq_next must be preserved across switch, got %d", snap.Config.SeqNext)
	}
}
```

Remove the temporary `SetStrategyForTest` helper from Task 6 (superseded by `SetStrategy`), and update `TestCreateRandom` to call `s.SetStrategy(config.Random)` instead.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/store/`
Expected: FAIL — `undefined: s.Get` etc.

- [ ] **Step 3: Implement the remaining methods**

Add to `internal/store/store.go`:
```go
// Get loads one quest by id.
func (s *Store) Get(id string) (*quest.Quest, error) {
	tip, err := s.tip()
	if err != nil {
		return nil, err
	}
	if tip == "" {
		return nil, ErrNotFound
	}
	raw, err := s.readFile(tip, questPath(id))
	if err != nil {
		return nil, ErrNotFound
	}
	return quest.Unmarshal(id, raw)
}

// List returns all quests, sorted by id.
func (s *Store) List() ([]*quest.Quest, error) {
	snap, err := s.snapshot()
	if err != nil {
		return nil, err
	}
	out := make([]*quest.Quest, 0, len(snap.IDs))
	for _, id := range snap.IDs {
		raw, err := s.readFile(snap.Tip, questPath(id))
		if err != nil {
			return nil, err
		}
		q, err := quest.Unmarshal(id, raw)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, nil
}

// Update loads a quest, applies apply, and writes it back under the CAS loop.
// apply may run more than once (on CAS retry), so it must be a pure function of
// its argument.
func (s *Store) Update(id string, apply func(*quest.Quest)) error {
	return s.mutate("side-quest: update "+id, func(snap *Snapshot, tx *txn) error {
		if snap.Tip == "" {
			return ErrNotFound
		}
		raw, err := s.readFile(snap.Tip, questPath(id))
		if err != nil {
			return ErrNotFound
		}
		q, err := quest.Unmarshal(id, raw)
		if err != nil {
			return err
		}
		apply(q)
		data, err := quest.Marshal(q)
		if err != nil {
			return err
		}
		tx.put(questPath(id), data)
		return nil
	})
}

// SetStatus sets a quest's status, stamping Completed when moving to done.
func (s *Store) SetStatus(id string, st quest.Status) error {
	if !st.Valid() {
		return fmt.Errorf("invalid status %q", st)
	}
	return s.Update(id, func(q *quest.Quest) {
		q.Status = st
		if st == quest.StatusDone && q.Completed == nil {
			t := time.Now().UTC().Truncate(time.Second)
			q.Completed = &t
		}
	})
}

// AddCommit appends sha to a quest's commit list (deduped). When complete is
// true it also closes the quest (used by the Completes: trailer in a later
// phase).
func (s *Store) AddCommit(id, sha string, complete bool) error {
	return s.Update(id, func(q *quest.Quest) {
		if !contains(q.Commits, sha) {
			q.Commits = append(q.Commits, sha)
		}
		if complete && q.Status != quest.StatusDone {
			q.Status = quest.StatusDone
			t := time.Now().UTC().Truncate(time.Second)
			q.Completed = &t
		}
	})
}

// SetStrategy switches the id strategy, preserving seq_next so a later switch
// back to sequential resumes the counter (spec §7).
func (s *Store) SetStrategy(st config.Strategy) error {
	return s.mutate("side-quest: set id strategy "+string(st), func(snap *Snapshot, tx *txn) error {
		cfg := snap.Config
		cfg.IDStrategy = st
		data, err := config.Marshal(cfg)
		if err != nil {
			return err
		}
		tx.put(configPath, data)
		return nil
	})
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/store/ -race`
Expected: PASS (all store tests), no race warnings.

- [ ] **Step 5: Run the whole suite**

Run: `go test ./... -race`
Expected: PASS across `gitcmd`, `quest`, `config`, `store`.

- [ ] **Step 6: Commit**

```bash
git add internal/store/
git commit -m "feat(store): Get/List/Update/SetStatus/AddCommit/SetStrategy"
```

---

## Phase 1 Definition of Done

- `go test ./... -race` passes.
- A quest can be created (`Create`), read (`Get`/`List`), updated (`SetStatus`/`Update`/`AddCommit`), and the id strategy switched (`SetStrategy`) — all persisted on `refs/side-quest/quests`, none touching the working tree.
- Sequential and random allocation both work; concurrent creation yields unique ids under `-race`.
- The id is never stored in a file; it always derives from the filename.

## Deferred to later phases (do NOT build here)

- Trailer parsing + git hooks + `link` (Phase 2).
- Date sorting (`--sort`), `last_commit` derivation (Phase 3, surfaced by the CLI).
- CLI and `--json` output (Phase 3).
- MCP server (Phase 4).
- Voice/tone rendering (Phase 5) — `config.Tone` exists but is unused in Phase 1.
- Importer (Phase 6).
- Claude plugin, README, AGENTS.md (Phase 7).

## Self-review notes

- **Spec coverage (Phase 1 slice):** storage model §5 ✓ (orphan ref, plumbing, CAS, file-per-quest); schema §6 ✓ (fields, id-not-stored); id strategies §7 ✓ (sequential counter, random, switch preserving seq_next, existence guard); module boundaries §4 ✓ (`quest`/`config`/`gitcmd`/`store`, pure vs I/O). Dates §8, linking §9, and everything else are explicitly deferred above.
- **CAS caveat (carry to Phase 2 review):** `cas` treats *any* `update-ref` failure as a lost race. A genuinely broken environment therefore surfaces as "contended after 10 tries" rather than the root error. Acceptable for v1; revisit if it obscures a real failure.
- **Test determinism:** no wall-clock assertions on exact values; `Created`/`Completed` are checked for presence/relative correctness, not equality to `now`.
