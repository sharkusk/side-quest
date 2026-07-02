# side-quest Phase 2 — Commit Linking & Hooks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the quest↔commit loop end-to-end: git trailers (`Quest:` / `Completes:`) drive a `post-commit` hook that writes the now-known commit hash back onto the orphan ref, with an assisted (or enforced) `commit-msg` check and an auto-filled current-quest trailer.

**Architecture:** A pure `trailer` package parses/decides on commit messages; the existing `store` gains `Link` (apply a commit's trailers), a config accessor, the `require_quest` flag, and a per-worktree current-quest pointer; a new `cmd/side-quest` binary exposes the hook entrypoints; `install-hooks` writes thin shims that call that binary by absolute path. All hook logic stays in Go (spec §15).

**Tech Stack:** Go 1.22, `gopkg.in/yaml.v3`, the system `git` binary (via `internal/gitcmd`). No new third-party dependencies.

## Global Constraints

- Module path is `github.com/sharkusk/side-quest`; internal packages live under `internal/`.
- **Go ≥ 1.22**, **`git` ≥ 2.13**. This phase adds `rev-parse --git-common-dir` (git 2.5) and `remote get-url` (git 2.7) — both **below** the current 2.13 floor, so the floor does NOT change. Confirm this in the docs; do not raise the floor.
- **Assisted, never surprising:** the `commit-msg` hook exits non-zero **only** for an intentional `require_quest` rejection. Every other path (unreadable message, not-a-repo, internal hiccup) must allow the commit (exit 0). `prepare-commit-msg` and `post-commit` never block a commit.
- **Voice is Phase 5.** All human-facing strings in this phase are **plain text**, prefixed `side-quest:`. Do not build or wire the voice layer here.
- **Machine/`--json` output stays neutral.** (No `--json` flags are added this phase; just don't emit flavor.)
- `require_quest` config key defaults **`false`**; `auto_trailer` already defaults **`true`**. `Quest: none` (case-insensitive value) is the escape hatch that satisfies the check without linking.
- The quest **id is the filename** — never serialize it (unchanged Phase 1 invariant).
- **Living docs:** `docs/architecture.md` and `README.md` are updated in this same phase. This plan **consolidates** the Phase 2 living-doc update into Task 7 (the task where the feature becomes user-visible); earlier tasks are internal building blocks. This is a deliberate, stated exception to "docs in every commit," not an omission.
- **Do NOT edit** the dated spec (`docs/superpowers/specs/2026-07-02-side-quest-design.md`) — it is a frozen design record.
- TDD throughout; teaching-quality doc comments aimed at a C/Python reader (match the existing package style); small, single-purpose files.
- Commit identity in tests: the existing `store` test helper `newStore(t)` sets `user.email`/`user.name` in the temp repo — reuse that pattern for any repo you create.

---

## File Structure

| File | New/Modify | Responsibility |
|---|---|---|
| `internal/gitcmd/gitcmd.go` | Modify | Dedupe env so a `WithEnv` override wins over an inherited same-key var (protects the scratch index inside hooks). |
| `internal/gitcmd/gitcmd_test.go` | Modify | Unit-test the env dedupe. |
| `internal/config/config.go` | Modify | Add `RequireQuest bool` field (default false). |
| `internal/config/config_test.go` | Modify | Default + round-trip for `RequireQuest`. |
| `internal/store/store.go` | Modify | Add `Config()` accessor and `SetRequireQuest(bool)` (mirrors `SetStrategy`). |
| `internal/trailer/trailer.go` | New | Pure: `Ref`, `Parse`, `Action`, `Decision`. |
| `internal/trailer/trailer_test.go` | New | Unit tests for parse + decision. |
| `internal/store/link.go` | New | `Link(sha)` — read a commit's message, parse trailers, apply to quests. |
| `internal/store/link_test.go` | New | Integration: real commit → quest linked/closed; tolerant of unknown ids; index-override proof. |
| `internal/store/current.go` | New | Per-worktree current-quest pointer: `SetCurrent`/`Current`/`ClearCurrent`. |
| `internal/store/current_test.go` | New | Round-trip + clear + empty. |
| `cmd/side-quest/main.go` | New | Dispatch + subcommands `link`, `current`, `commit-msg`, `prepare-commit-msg`. |
| `cmd/side-quest/hooks.go` | New | `install-hooks`: resolve hooks dir, write/compose shims, add refspec. |
| `cmd/side-quest/main_test.go` | New | Built-binary tests: subcommands + capstone end-to-end through real git hooks. |
| `docs/architecture.md` | Modify | New "Commit linking & hooks" section; Dependencies note (floor unchanged). |
| `README.md` | Modify | Concepts: trailers + hooks + current quest. |

---

## Task 1: Harden `gitcmd` env override

**Files:**
- Modify: `internal/gitcmd/gitcmd.go` (the `run` method, ~lines 39–60)
- Test: `internal/gitcmd/gitcmd_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `func (g *Git) run(...)` now guarantees each env key appears once, with the **last** value winning — so `WithEnv("GIT_INDEX_FILE=...")` beats an inherited `GIT_INDEX_FILE`. No exported signature changes.

**Why:** In a git hook, git may export `GIT_INDEX_FILE` pointing at the real index. `buildCommit` appends its own scratch `GIT_INDEX_FILE`; with a duplicate key, `getenv` (first match) would pick git's real index and the store would mutate the user's index. Deduping keep-last makes the override authoritative.

- [ ] **Step 1: Write the failing test**

Add to `internal/gitcmd/gitcmd_test.go`:

```go
func TestDedupeEnvKeepLast(t *testing.T) {
	in := []string{"A=1", "B=2", "A=3", "PATH=/x", "B=5", "NOEQ"}
	got := dedupeEnvKeepLast(in)

	m := map[string]string{}
	for _, kv := range got {
		if k, v, ok := strings.Cut(kv, "="); ok {
			if _, dup := m[k]; dup {
				t.Fatalf("key %q appears more than once: %v", k, got)
			}
			m[k] = v
		}
	}
	if m["A"] != "3" || m["B"] != "5" || m["PATH"] != "/x" {
		t.Fatalf("keep-last wrong: %v", m)
	}
	// entries without '=' are preserved as-is
	found := false
	for _, kv := range got {
		if kv == "NOEQ" {
			found = true
		}
	}
	if !found {
		t.Fatalf("non key=value entry dropped: %v", got)
	}
}
```

Ensure the test file imports `strings` (add it to the existing import block if not present).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gitcmd/ -run TestDedupeEnvKeepLast -v`
Expected: FAIL — `undefined: dedupeEnvKeepLast`.

- [ ] **Step 3: Implement the dedupe and use it in `run`**

In `internal/gitcmd/gitcmd.go`, replace the env-building lines inside `run` (currently):

```go
	cmd.Env = append(cmd.Environ(), "LC_ALL=C") // Environ() = inherited env
	if len(g.env) > 0 {
		cmd.Env = append(cmd.Env, g.env...)
	}
```

with:

```go
	// Build env then collapse duplicate keys keeping the LAST value, so our
	// overrides (LC_ALL, and especially GIT_INDEX_FILE from WithEnv) beat any
	// inherited same-key var. Git reads env via getenv (first match), so a
	// duplicate GIT_INDEX_FILE inherited from a hook could otherwise point git
	// at the user's REAL index instead of our scratch one.
	env := append(cmd.Environ(), "LC_ALL=C")
	env = append(env, g.env...)
	cmd.Env = dedupeEnvKeepLast(env)
```

Add this helper at the bottom of the file:

```go
// dedupeEnvKeepLast returns env with each KEY=VALUE key appearing once, keeping
// the LAST value seen (later entries override earlier ones). Entries without a
// '=' are passed through unchanged. Order of first appearance is preserved.
func dedupeEnvKeepLast(env []string) []string {
	pos := map[string]int{} // key -> index in out
	out := make([]string, 0, len(env))
	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			out = append(out, kv)
			continue
		}
		key := kv[:eq]
		if i, ok := pos[key]; ok {
			out[i] = kv // overwrite earlier value with this later one
			continue
		}
		pos[key] = len(out)
		out = append(out, kv)
	}
	return out
}
```

`strings` is already imported in `gitcmd.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/gitcmd/ -v`
Expected: PASS (new test plus existing ones).

- [ ] **Step 5: Commit**

```bash
git add internal/gitcmd/gitcmd.go internal/gitcmd/gitcmd_test.go
git commit -m "fix(gitcmd): dedupe env so WithEnv override wins over inherited (hook-safe scratch index)"
```

---

## Task 2: `require_quest` config plumbing

**Files:**
- Modify: `internal/config/config.go` (struct + `Default`)
- Modify: `internal/config/config_test.go`
- Modify: `internal/store/store.go` (add `Config()` and `SetRequireQuest`)
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: `config.Config`, `config.Marshal`, the store's `mutate`/`snapshot`.
- Produces:
  - `config.Config.RequireQuest bool` (`yaml:"require_quest"`), default `false`.
  - `func (s *Store) Config() (config.Config, error)` — the on-ref config (`Default()` when empty).
  - `func (s *Store) SetRequireQuest(v bool) error` — flip enforcement on the ref.

> Note: there is no `config`-CLI in Phase 2 (deferred to Phase 3 per the tight-slice scope). `SetRequireQuest` is the library/tested path to enable enforcement now; the terminal toggle lands with Phase 3's `side-quest config set`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/config/config_test.go` (this file is `package config` — internal test, no `config.` qualifier):

```go
func TestRequireQuestDefaultsFalse(t *testing.T) {
	if Default().RequireQuest {
		t.Fatal("require_quest should default to false")
	}
}

func TestRequireQuestRoundTrips(t *testing.T) {
	c := Default()
	c.RequireQuest = true
	data, err := Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if !got.RequireQuest {
		t.Fatalf("require_quest did not round-trip: %+v", got)
	}
}

func TestRequireQuestAbsentKeyIsFalse(t *testing.T) {
	// A config file written before this key existed must default it to false.
	got, err := Unmarshal([]byte("id_prefix: SQ\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got.RequireQuest {
		t.Fatal("absent require_quest must default to false")
	}
}
```

Add to `internal/store/store_test.go`:

```go
func TestSetRequireQuestPersists(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.Config()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RequireQuest {
		t.Fatal("fresh store should have require_quest=false")
	}
	if err := s.SetRequireQuest(true); err != nil {
		t.Fatal(err)
	}
	cfg, err = s.Config()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.RequireQuest {
		t.Fatal("SetRequireQuest(true) did not persist")
	}
}

func TestConfigEmptyStoreIsDefault(t *testing.T) {
	s := newStore(t) // not initialized
	cfg, err := s.Config()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IDPrefix != "SQ" || cfg.RequireQuest {
		t.Fatalf("empty-store Config should be Default(): %+v", cfg)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ ./internal/store/ -run 'RequireQuest|ConfigEmptyStore' -v`
Expected: FAIL — `RequireQuest` field and `Config`/`SetRequireQuest` methods undefined.

- [ ] **Step 3: Add the config field**

In `internal/config/config.go`, add the field to `Config` (after `AutoTrailer`):

```go
	AutoTrailer bool     `yaml:"auto_trailer"`
	// RequireQuest, when true, makes the commit-msg hook REJECT commits that
	// carry no Quest:/Completes: trailer (and no explicit `Quest: none`).
	// Default false = assisted mode (warn only).
	RequireQuest bool `yaml:"require_quest"`
```

Leave `Default()` as-is: a `bool` field it does not set is already `false`, which is the intended default. (Optionally add `RequireQuest: false` there for explicitness — not required.)

- [ ] **Step 4: Add the store methods**

In `internal/store/store.go`, add near `SetStrategy` (end of file, before `contains`):

```go
// Config returns the on-ref configuration, or Default() when the store is empty.
func (s *Store) Config() (config.Config, error) {
	snap, err := s.snapshot()
	if err != nil {
		return config.Config{}, err
	}
	return snap.Config, nil
}

// SetRequireQuest flips the require_quest enforcement flag on the ref.
func (s *Store) SetRequireQuest(v bool) error {
	return s.mutate("side-quest: set require_quest", func(snap *Snapshot, tx *txn) error {
		cfg := snap.Config
		cfg.RequireQuest = v
		data, err := config.Marshal(cfg)
		if err != nil {
			return err
		}
		tx.put(configPath, data)
		return nil
	})
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/config/ ./internal/store/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/store/store.go internal/store/store_test.go
git commit -m "feat(config): add require_quest flag + store Config()/SetRequireQuest"
```

---

## Task 3: `trailer` package (parse + decision)

**Files:**
- Create: `internal/trailer/trailer.go`
- Test: `internal/trailer/trailer_test.go`

**Interfaces:**
- Consumes: nothing (pure, stdlib `strings` only).
- Produces:
  - `type Ref struct { ID string; Completes bool }`
  - `func Parse(message string) (refs []Ref, explicitNone bool)`
  - `type Action int` with `const ( Accept Action = iota; Warn; Reject )`
  - `func Decision(message string, requireQuest bool) Action`

- [ ] **Step 1: Write the failing tests**

Create `internal/trailer/trailer_test.go`:

```go
package trailer

import "testing"

func TestParseSingleQuest(t *testing.T) {
	refs, none := Parse("do work\n\nQuest: SQ-0001\n")
	if none {
		t.Fatal("did not expect explicitNone")
	}
	if len(refs) != 1 || refs[0].ID != "SQ-0001" || refs[0].Completes {
		t.Fatalf("bad parse: %+v", refs)
	}
}

func TestParseCompletes(t *testing.T) {
	refs, _ := Parse("finish\n\nCompletes: SQ-0002\n")
	if len(refs) != 1 || refs[0].ID != "SQ-0002" || !refs[0].Completes {
		t.Fatalf("bad parse: %+v", refs)
	}
}

func TestParseMultiple(t *testing.T) {
	refs, _ := Parse("msg\n\nQuest: SQ-1\nCompletes: SQ-2\n")
	if len(refs) != 2 {
		t.Fatalf("want 2 refs, got %+v", refs)
	}
	if refs[0].ID != "SQ-1" || refs[0].Completes {
		t.Fatalf("ref0 wrong: %+v", refs[0])
	}
	if refs[1].ID != "SQ-2" || !refs[1].Completes {
		t.Fatalf("ref1 wrong: %+v", refs[1])
	}
}

func TestParseNoneEscapeHatch(t *testing.T) {
	refs, none := Parse("chore\n\nQuest: none\n")
	if !none {
		t.Fatal("expected explicitNone for 'Quest: none'")
	}
	if len(refs) != 0 {
		t.Fatalf("none must yield no refs: %+v", refs)
	}
	// case-insensitive value
	if _, none := Parse("Quest: NONE\n"); !none {
		t.Fatal("expected case-insensitive none")
	}
}

func TestParseIgnoresNonTrailerLines(t *testing.T) {
	// "Question:" must not match "Quest:"; prose is ignored.
	refs, none := Parse("Question: is this a trailer?\nno it is not\n")
	if none || len(refs) != 0 {
		t.Fatalf("false positive: refs=%+v none=%v", refs, none)
	}
}

func TestParseTrimsIndentedTrailer(t *testing.T) {
	refs, _ := Parse("msg\n\n   Quest: SQ-0009  \n")
	if len(refs) != 1 || refs[0].ID != "SQ-0009" {
		t.Fatalf("indented/trailing-space trailer not handled: %+v", refs)
	}
}

func TestDecision(t *testing.T) {
	if Decision("Quest: SQ-1\n", false) != Accept {
		t.Error("ref present -> Accept")
	}
	if Decision("Quest: none\n", true) != Accept {
		t.Error("explicit none -> Accept even when required")
	}
	if Decision("no trailer\n", false) != Warn {
		t.Error("no trailer, not required -> Warn")
	}
	if Decision("no trailer\n", true) != Reject {
		t.Error("no trailer, required -> Reject")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/trailer/ -v`
Expected: FAIL — package/functions do not exist yet.

- [ ] **Step 3: Implement the package**

Create `internal/trailer/trailer.go`:

```go
// Package trailer parses side-quest commit-message trailers and decides the
// commit-msg hook's action. It is PURE — no git, no filesystem — so the policy
// is trivially unit-testable. The cmd layer feeds it a message string plus the
// require_quest flag and acts on the result.
//
// Trailers (git convention: key: value lines near the end of a message):
//
//	Quest: SQ-0001      // this commit did work on SQ-0001
//	Completes: SQ-0001  // as above AND closes the quest
//	Quest: none         // explicit escape hatch: a genuine chore, not linked
package trailer

import "strings"

// Ref is one quest reference extracted from a commit message.
type Ref struct {
	ID        string // e.g. "SQ-0001"
	Completes bool   // true for a Completes: trailer (also closes the quest)
}

// Parse scans a commit message for Quest:/Completes: trailers.
//
// It returns every reference found (a commit may touch several quests) and,
// separately, whether an explicit `Quest: none` was present — that is NOT a
// reference but it satisfies the commit-msg check. A trailer is recognized when
// a line, after trimming surrounding whitespace, begins with the exact key
// "Quest:" or "Completes:".
func Parse(message string) (refs []Ref, explicitNone bool) {
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Quest:"):
			val := strings.TrimSpace(strings.TrimPrefix(line, "Quest:"))
			if strings.EqualFold(val, "none") {
				explicitNone = true
				continue
			}
			if val != "" {
				refs = append(refs, Ref{ID: val})
			}
		case strings.HasPrefix(line, "Completes:"):
			val := strings.TrimSpace(strings.TrimPrefix(line, "Completes:"))
			if val != "" && !strings.EqualFold(val, "none") {
				refs = append(refs, Ref{ID: val, Completes: true})
			}
		}
	}
	return refs, explicitNone
}

// Action is the commit-msg hook's decision.
type Action int

const (
	Accept Action = iota // has a ref, or an explicit `Quest: none`
	Warn                 // no ref / no none, require_quest off -> warn but allow
	Reject               // no ref / no none, require_quest on  -> block the commit
)

// Decision picks the commit-msg action for a message under require_quest.
func Decision(message string, requireQuest bool) Action {
	refs, none := Parse(message)
	if len(refs) > 0 || none {
		return Accept
	}
	if requireQuest {
		return Reject
	}
	return Warn
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/trailer/ -v`
Expected: PASS (all cases).

- [ ] **Step 5: Commit**

```bash
git add internal/trailer/
git commit -m "feat(trailer): parse Quest:/Completes: trailers + commit-msg decision"
```

---

## Task 4: `store.Link` — apply a commit's trailers

**Files:**
- Create: `internal/store/link.go`
- Test: `internal/store/link_test.go`

**Interfaces:**
- Consumes: `trailer.Parse`, the store's `git` handle, `AddCommit(id, sha string, complete bool) error`, `ErrNotFound`.
- Produces: `func (s *Store) Link(sha string) error` — canonicalizes `sha`, reads its message, and for each trailer appends the commit hash to the quest (closing `Completes:` targets). Tolerant of unknown quest ids.

- [ ] **Step 1: Write the failing tests**

Create `internal/store/link_test.go`:

```go
package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sharkusk/side-quest/internal/quest"
)

// commitInWorktree makes a real commit on the working branch (not the orphan
// ref) with the given message, and returns its full sha. It writes a unique
// file so each commit has content.
func commitInWorktree(t *testing.T, s *Store, filename, message string) string {
	t.Helper()
	top, err := s.git.Run("rev-parse", "--show-toplevel")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(top, filename), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.git.Run("add", filename); err != nil {
		t.Fatal(err)
	}
	if _, err := s.git.Run("commit", "-m", message); err != nil {
		t.Fatal(err)
	}
	sha, err := s.git.Run("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return sha
}

func TestLinkCompletesClosesQuest(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	q, err := s.Create("close me", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	sha := commitInWorktree(t, s, "a.txt", "work\n\nCompletes: "+q.ID+"\n")

	if err := s.Link("HEAD"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(q.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != quest.StatusDone || got.Completed == nil {
		t.Fatalf("Completes: should close the quest: %+v", got)
	}
	if len(got.Commits) != 1 || got.Commits[0] != sha {
		t.Fatalf("commit hash not linked: %v (want %s)", got.Commits, sha)
	}
}

func TestLinkQuestAppendsWithoutClosing(t *testing.T) {
	s := newStore(t)
	_ = s.Init()
	q, _ := s.Create("ongoing", "", nil)
	commitInWorktree(t, s, "b.txt", "progress\n\nQuest: "+q.ID+"\n")

	if err := s.Link("HEAD"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(q.ID)
	if got.Status != quest.StatusOpen {
		t.Fatalf("Quest: (not Completes) must not close: %+v", got)
	}
	if len(got.Commits) != 1 {
		t.Fatalf("expected 1 linked commit, got %v", got.Commits)
	}
}

func TestLinkUnknownIDIsTolerant(t *testing.T) {
	s := newStore(t)
	_ = s.Init()
	commitInWorktree(t, s, "c.txt", "typo\n\nCompletes: SQ-9999\n")
	// Referenced quest does not exist; Link must not error (commit already made).
	if err := s.Link("HEAD"); err != nil {
		t.Fatalf("Link should tolerate unknown ids, got %v", err)
	}
}

func TestLinkNoTrailerIsNoop(t *testing.T) {
	s := newStore(t)
	_ = s.Init()
	commitInWorktree(t, s, "d.txt", "no trailer here\n")
	if err := s.Link("HEAD"); err != nil {
		t.Fatalf("no-trailer commit should be a no-op, got %v", err)
	}
}

// TestLinkIgnoresInheritedIndexFile proves the Task 1 hardening in the real
// hook scenario: even if GIT_INDEX_FILE is set in the environment (as git does
// inside hooks), Link's mutation uses its own scratch index and succeeds
// without touching that index.
func TestLinkIgnoresInheritedIndexFile(t *testing.T) {
	s := newStore(t)
	_ = s.Init()
	q, _ := s.Create("hooked", "", nil)
	// Make the commit BEFORE setting the bogus index (git add/commit need the
	// real index).
	commitInWorktree(t, s, "e.txt", "work\n\nCompletes: "+q.ID+"\n")

	bogus := filepath.Join(t.TempDir(), "nonexistent-index")
	os.Setenv("GIT_INDEX_FILE", bogus)
	err := s.Link("HEAD")
	os.Unsetenv("GIT_INDEX_FILE")
	if err != nil {
		t.Fatalf("Link failed under inherited GIT_INDEX_FILE: %v", err)
	}
	got, _ := s.Get(q.ID)
	if got.Status != quest.StatusDone {
		t.Fatalf("link did not apply under inherited index: %+v", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run TestLink -v`
Expected: FAIL — `s.Link` undefined.

- [ ] **Step 3: Implement `Link`**

Create `internal/store/link.go`:

```go
package store

import (
	"errors"

	"github.com/sharkusk/side-quest/internal/trailer"
)

// Link applies a commit's side-quest trailers to the store: for every
// Quest:/Completes: trailer in the commit's message, it appends the commit's
// canonical hash to that quest and, for Completes:, closes the quest.
//
// This is the post-commit entry point where the chicken-and-egg is resolved:
// the commit already exists (its hash is known), and the quest update is a
// SEPARATE commit on the orphan ref whose own hash nobody has to record.
//
// Link is deliberately TOLERANT: a trailer naming a quest that does not exist
// (a typo, or an id from another clone) is skipped — post-commit must never
// fail the user's already-made commit over a bad reference. Genuine errors
// (anything other than "not found") are surfaced.
func (s *Store) Link(sha string) error {
	full, err := s.git.Run("rev-parse", sha)
	if err != nil {
		return err
	}
	msg, err := s.git.Run("show", "-s", "--format=%B", full)
	if err != nil {
		return err
	}
	refs, _ := trailer.Parse(msg)
	for _, r := range refs {
		if err := s.AddCommit(r.ID, full, r.Completes); err != nil {
			if errors.Is(err, ErrNotFound) {
				continue // unknown id — skip, keep processing other refs
			}
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -run TestLink -v`
Expected: PASS (all five).

- [ ] **Step 5: Run the full store suite with the race detector**

Run: `go test ./internal/store/ -race`
Expected: PASS (Link changes must not regress the concurrency tests).

- [ ] **Step 6: Commit**

```bash
git add internal/store/link.go internal/store/link_test.go
git commit -m "feat(store): Link(sha) applies commit trailers to quests (closes the loop)"
```

---

## Task 5: Per-worktree current-quest pointer

**Files:**
- Create: `internal/store/current.go`
- Test: `internal/store/current_test.go`

**Interfaces:**
- Consumes: `s.gitDir` (already the per-worktree absolute git dir from `rev-parse --absolute-git-dir`).
- Produces:
  - `func (s *Store) SetCurrent(id string) error`
  - `func (s *Store) Current() (string, error)` — `""` when unset.
  - `func (s *Store) ClearCurrent() error` — no error when already unset.

- [ ] **Step 1: Write the failing tests**

Create `internal/store/current_test.go`:

```go
package store

import "testing"

func TestCurrentRoundTrip(t *testing.T) {
	s := newStore(t)
	cur, err := s.Current()
	if err != nil {
		t.Fatal(err)
	}
	if cur != "" {
		t.Fatalf("fresh worktree should have no current quest, got %q", cur)
	}
	if err := s.SetCurrent("SQ-0007"); err != nil {
		t.Fatal(err)
	}
	cur, err = s.Current()
	if err != nil {
		t.Fatal(err)
	}
	if cur != "SQ-0007" {
		t.Fatalf("current not persisted: %q", cur)
	}
}

func TestClearCurrent(t *testing.T) {
	s := newStore(t)
	if err := s.SetCurrent("SQ-0001"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearCurrent(); err != nil {
		t.Fatal(err)
	}
	cur, _ := s.Current()
	if cur != "" {
		t.Fatalf("expected cleared, got %q", cur)
	}
	// Clearing again is not an error.
	if err := s.ClearCurrent(); err != nil {
		t.Fatalf("double clear should be a no-op, got %v", err)
	}
}

// The pointer must NOT touch the orphan ref or the working tree.
func TestSetCurrentDoesNotTouchRefOrTree(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	before, _ := s.tip()
	if err := s.SetCurrent("SQ-0003"); err != nil {
		t.Fatal(err)
	}
	after, _ := s.tip()
	if before != after {
		t.Fatal("SetCurrent must not move the orphan ref")
	}
	out, _ := s.git.Run("status", "--porcelain")
	if out != "" {
		t.Fatalf("SetCurrent modified the working tree/index: %q", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -run 'Current' -v`
Expected: FAIL — `SetCurrent`/`Current`/`ClearCurrent` undefined.

- [ ] **Step 3: Implement the pointer**

Create `internal/store/current.go`:

```go
package store

import (
	"os"
	"path/filepath"
	"strings"
)

// The current-quest pointer is WORKTREE-LOCAL state, not ref state: it records
// which quest this worktree is "on" so prepare-commit-msg can auto-fill the
// Quest: trailer. It lives in the worktree's git dir (NOT on the orphan ref and
// NOT in the working tree), so each worktree/lane has its own and it never
// travels with a push. s.gitDir is already the per-worktree git dir
// (rev-parse --absolute-git-dir), so this is worktree-scoped for free.
func (s *Store) currentPath() string {
	return filepath.Join(s.gitDir, "side-quest-current")
}

// SetCurrent records id as this worktree's active quest.
func (s *Store) SetCurrent(id string) error {
	return os.WriteFile(s.currentPath(), []byte(id+"\n"), 0o644)
}

// Current returns the worktree's active quest id, or "" if none is set.
func (s *Store) Current() (string, error) {
	b, err := os.ReadFile(s.currentPath())
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// ClearCurrent removes the pointer; it is not an error if none was set.
func (s *Store) ClearCurrent() error {
	if err := os.Remove(s.currentPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -run 'Current' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/current.go internal/store/current_test.go
git commit -m "feat(store): per-worktree current-quest pointer (Set/Current/Clear)"
```

---

## Task 6: `cmd/side-quest` binary — hook subcommands

**Files:**
- Create: `cmd/side-quest/main.go`
- Test: `cmd/side-quest/main_test.go` (subcommand-level tests; the end-to-end-through-git test is added in Task 7)

**Interfaces:**
- Consumes: `store.Open`, `store.Link`, `store.Current`/`SetCurrent`/`ClearCurrent`, `store.Config`, `trailer.Decision`, `trailer.Parse`.
- Produces: an executable with subcommands `link <sha>`, `current [<id>|--clear]`, `commit-msg <file>`, `prepare-commit-msg <file> [..]`. Dispatch via `run(cmd string, args []string) error`; `main` maps a returned error to exit 1, and `commit-msg` uses `os.Exit(1)` only for an intentional reject.

- [ ] **Step 1: Write the failing tests**

Create `cmd/side-quest/main_test.go`:

```go
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharkusk/side-quest/internal/gitcmd"
	"github.com/sharkusk/side-quest/internal/store"
)

// buildBinary compiles cmd/side-quest to a temp path and returns it.
func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "side-quest")
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
	return bin
}

// newRepo makes a temp git repo with an identity and returns (dir, openedStore).
func newRepo(t *testing.T) (string, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	g := gitcmd.New(dir)
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "Tester"},
	} {
		if _, err := g.Run(args...); err != nil {
			t.Fatal(err)
		}
	}
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, s
}

// runBin runs the built binary in dir and returns (combined output, exit code).
func runBin(t *testing.T, bin, dir string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
	return string(out), code
}

func TestCurrentSubcommand(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)

	if _, code := runBin(t, bin, dir, "current", "SQ-0042"); code != 0 {
		t.Fatalf("set current exit=%d", code)
	}
	cur, _ := s.Current()
	if cur != "SQ-0042" {
		t.Fatalf("current not set via CLI: %q", cur)
	}
	out, code := runBin(t, bin, dir, "current")
	if code != 0 || out == "" {
		t.Fatalf("get current: out=%q code=%d", out, code)
	}
	if _, code := runBin(t, bin, dir, "current", "--clear"); code != 0 {
		t.Fatalf("clear exit=%d", code)
	}
	if cur, _ := s.Current(); cur != "" {
		t.Fatalf("current not cleared: %q", cur)
	}
}

func TestCommitMsgExitCodes(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	msg := filepath.Join(dir, "MSG")

	// Assisted (default): missing trailer warns but exits 0.
	if err := os.WriteFile(msg, []byte("no trailer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, code := runBin(t, bin, dir, "commit-msg", msg); code != 0 {
		t.Fatalf("warn path must exit 0, got %d", code)
	}

	// Enforced: missing trailer rejects (exit 1).
	if err := s.SetRequireQuest(true); err != nil {
		t.Fatal(err)
	}
	if _, code := runBin(t, bin, dir, "commit-msg", msg); code != 1 {
		t.Fatalf("reject path must exit 1, got %d", code)
	}

	// Escape hatch: Quest: none passes even when enforced.
	if err := os.WriteFile(msg, []byte("chore\n\nQuest: none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, code := runBin(t, bin, dir, "commit-msg", msg); code != 0 {
		t.Fatalf("Quest: none must pass enforcement, got %d", code)
	}
}

func TestPrepareCommitMsgInjectsCurrent(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCurrent("SQ-0005"); err != nil {
		t.Fatal(err)
	}
	msg := filepath.Join(dir, "MSG")
	if err := os.WriteFile(msg, []byte("a change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, code := runBin(t, bin, dir, "prepare-commit-msg", msg); code != 0 {
		t.Fatalf("prepare exit=%d", code)
	}
	out, _ := os.ReadFile(msg)
	if want := "Quest: SQ-0005"; !containsLine(string(out), want) {
		t.Fatalf("current trailer not injected: %q", out)
	}
	// Idempotent: running again does not add a second trailer.
	if _, code := runBin(t, bin, dir, "prepare-commit-msg", msg); code != 0 {
		t.Fatalf("second prepare exit=%d", code)
	}
	out2, _ := os.ReadFile(msg)
	if countLine(string(out2), "Quest: SQ-0005") != 1 {
		t.Fatalf("trailer injected twice: %q", out2)
	}
}

func containsLine(s, want string) bool { return countLine(s, want) > 0 }

func countLine(s, want string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if line == want {
			n++
		}
	}
	return n
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/side-quest/ -v`
Expected: FAIL — `go build` of `cmd/side-quest` fails (no `main.go`), so the build helper errors out.

- [ ] **Step 3: Implement the binary**

Create `cmd/side-quest/main.go`:

```go
// Command side-quest is the side-quest CLI and git-hook entrypoint. This phase
// exposes the subcommands the hooks call — link, commit-msg, prepare-commit-msg
// — plus current-quest management and install-hooks. The full human CLI (init,
// new, list, show, ...) arrives in a later phase.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/sharkusk/side-quest/internal/store"
	"github.com/sharkusk/side-quest/internal/trailer"
)

const usage = `usage: side-quest <command> [args]

  link <sha>                      apply a commit's Quest:/Completes: trailers
  current [<id> | --clear]        get / set / clear this worktree's active quest
  commit-msg <file>               (hook) warn or reject when a trailer is missing
  prepare-commit-msg <file> [..]  (hook) inject the current-quest trailer
  install-hooks                   install git hooks + refs/side-quest/* refspec`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "side-quest:", err)
		os.Exit(1)
	}
}

func run(cmd string, args []string) error {
	switch cmd {
	case "link":
		return cmdLink(args)
	case "current":
		return cmdCurrent(args)
	case "commit-msg":
		return cmdCommitMsg(args)
	case "prepare-commit-msg":
		return cmdPrepareCommitMsg(args)
	case "install-hooks":
		return cmdInstallHooks(args)
	default:
		return fmt.Errorf("unknown command %q\n%s", cmd, usage)
	}
}

// openStore opens the store for the current working directory. Git runs hooks
// with the working tree as CWD, so this resolves the right repo.
func openStore() (*store.Store, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return store.Open(cwd)
}

func cmdLink(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("link needs exactly one <sha>")
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	return s.Link(args[0])
}

func cmdCurrent(args []string) error {
	s, err := openStore()
	if err != nil {
		return err
	}
	switch {
	case len(args) == 0:
		cur, err := s.Current()
		if err != nil {
			return err
		}
		if cur == "" {
			fmt.Println("(no current quest)")
		} else {
			fmt.Println(cur)
		}
		return nil
	case args[0] == "--clear":
		return s.ClearCurrent()
	default:
		return s.SetCurrent(args[0])
	}
}

// cmdCommitMsg implements the commit-msg hook. Per the assisted philosophy it
// exits non-zero ONLY for an intentional require_quest rejection; every other
// path (unreadable file, not-a-repo) allows the commit.
func cmdCommitMsg(args []string) error {
	if len(args) < 1 {
		return nil
	}
	msg, err := os.ReadFile(args[0])
	if err != nil {
		return nil // can't read the message -> don't block the commit
	}
	requireQuest := false
	if s, err := openStore(); err == nil {
		if cfg, err := s.Config(); err == nil {
			requireQuest = cfg.RequireQuest
		}
	}
	switch trailer.Decision(string(msg), requireQuest) {
	case trailer.Reject:
		fmt.Fprintln(os.Stderr, "side-quest: no Quest:/Completes: trailer and require_quest is on — commit blocked.")
		fmt.Fprintln(os.Stderr, "  Add e.g.  Quest: SQ-0001   (or  Quest: none  for a genuine chore).")
		os.Exit(1)
	case trailer.Warn:
		fmt.Fprintln(os.Stderr, "side-quest: no Quest: trailer on this commit. (Add 'Quest: none' to silence.)")
	}
	return nil
}

// cmdPrepareCommitMsg implements the prepare-commit-msg hook: if a current
// quest is set and auto_trailer is on, append a Quest: trailer to the message
// (unless one is already present). Never blocks: any obstacle -> leave the
// message untouched and exit 0.
func cmdPrepareCommitMsg(args []string) error {
	if len(args) < 1 {
		return nil
	}
	s, err := openStore()
	if err != nil {
		return nil
	}
	cur, err := s.Current()
	if err != nil || cur == "" {
		return nil
	}
	cfg, err := s.Config()
	if err != nil || !cfg.AutoTrailer {
		return nil
	}
	raw, err := os.ReadFile(args[0])
	if err != nil {
		return nil
	}
	if refs, none := trailer.Parse(string(raw)); len(refs) > 0 || none {
		return nil // a trailer is already present — don't double-inject
	}
	out := string(raw)
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	out += "\nQuest: " + cur + "\n" // blank line before the trailer block
	return os.WriteFile(args[0], []byte(out), 0o644)
}
```

> `cmdInstallHooks` is referenced here but implemented in Task 7 (`hooks.go`). This task's build will not compile until Task 7 adds `hooks.go`. To keep Task 6 independently green, add a **temporary stub** in `main.go` for this task and REPLACE it in Task 7:
>
> ```go
> func cmdInstallHooks(args []string) error {
> 	return fmt.Errorf("install-hooks lands in the next task")
> }
> ```
>
> Task 7 removes this stub and adds the real `cmdInstallHooks` in `hooks.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/side-quest/ -v`
Expected: PASS — `TestCurrentSubcommand`, `TestCommitMsgExitCodes`, `TestPrepareCommitMsgInjectsCurrent`.

- [ ] **Step 5: Vet + build the whole module**

Run: `go build ./... && go vet ./...`
Expected: no output (clean).

- [ ] **Step 6: Commit**

```bash
git add cmd/side-quest/main.go cmd/side-quest/main_test.go
git commit -m "feat(cmd): side-quest binary with link/current/commit-msg/prepare-commit-msg"
```

---

## Task 7: `install-hooks`, shims, refspec + capstone end-to-end + docs

**Files:**
- Create: `cmd/side-quest/hooks.go`
- Modify: `cmd/side-quest/main.go` (remove the `cmdInstallHooks` stub)
- Modify: `cmd/side-quest/main_test.go` (add the capstone end-to-end test)
- Modify: `docs/architecture.md`, `README.md`

**Interfaces:**
- Consumes: `gitcmd.New`, `os.Executable`.
- Produces: `func cmdInstallHooks(args []string) error` — resolves the hooks dir (honoring `core.hooksPath`), writes/composes `prepare-commit-msg`, `commit-msg`, `post-commit` shims that call this binary by absolute path, and best-effort adds the `refs/side-quest/*` push/fetch refspec to `origin`.

- [ ] **Step 1: Write the failing capstone test**

Add to `cmd/side-quest/main_test.go`:

```go
// gitCommit runs a real `git commit` in dir (hooks fire) and returns the exit
// code — used to assert require_quest rejection blocks the commit.
func gitCommit(t *testing.T, dir, filename, message string) int {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte("data\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := gitcmd.New(dir)
	if _, err := g.Run("add", filename); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "commit", "-m", message)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		t.Fatalf("git commit: %v", err)
	}
	return 0
}

// TestEndToEndHooksDriveLinking installs the real hooks and drives them with
// real commits: current-quest injection, assisted warning, enforced rejection,
// and post-commit linking that closes a quest.
func TestEndToEndHooksDriveLinking(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	q, err := s.Create("ship it", "", nil) // SQ-0001
	if err != nil {
		t.Fatal(err)
	}

	// Install hooks (bakes the built binary's absolute path into the shims).
	if _, code := runBin(t, bin, dir, "install-hooks"); code != 0 {
		t.Fatalf("install-hooks exit=%d", code)
	}

	// 1) current-quest injection + post-commit linking (not closing).
	if _, code := runBin(t, bin, dir, "current", q.ID); code != 0 {
		t.Fatalf("set current exit=%d", code)
	}
	if code := gitCommit(t, dir, "f1.txt", "progress on the feature"); code != 0 {
		t.Fatalf("commit 1 blocked unexpectedly: %d", code)
	}
	got, _ := s.Get(q.ID)
	if len(got.Commits) != 1 {
		t.Fatalf("prepare+post-commit should have linked one commit: %v", got.Commits)
	}
	if got.Status != quest.StatusOpen {
		t.Fatalf("Quest: (auto) should not close: %+v", got)
	}

	// 2) explicit Completes: closes the quest.
	if _, code := runBin(t, bin, dir, "current", "--clear"); code != 0 {
		t.Fatalf("clear current exit=%d", code)
	}
	if code := gitCommit(t, dir, "f2.txt", "done\n\nCompletes: "+q.ID); code != 0 {
		t.Fatalf("commit 2 blocked unexpectedly: %d", code)
	}
	got, _ = s.Get(q.ID)
	if got.Status != quest.StatusDone {
		t.Fatalf("Completes: via hook should close: %+v", got)
	}
	if len(got.Commits) != 2 {
		t.Fatalf("expected 2 linked commits, got %v", got.Commits)
	}

	// 3) require_quest enforcement blocks a trailerless commit.
	if err := s.SetRequireQuest(true); err != nil {
		t.Fatal(err)
	}
	if code := gitCommit(t, dir, "f3.txt", "no trailer here"); code == 0 {
		t.Fatal("require_quest should have blocked a trailerless commit")
	}
	// ...but Quest: none passes.
	if code := gitCommit(t, dir, "f3.txt", "chore\n\nQuest: none"); code != 0 {
		t.Fatalf("Quest: none should pass enforcement, blocked with %d", code)
	}
}
```

Add `"github.com/sharkusk/side-quest/internal/quest"` to the test file's imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/side-quest/ -run TestEndToEndHooksDriveLinking -v`
Expected: FAIL — `install-hooks` still returns the stub error, so injection/linking never happen.

- [ ] **Step 3: Remove the stub and implement `install-hooks`**

In `cmd/side-quest/main.go`, delete the temporary stub:

```go
func cmdInstallHooks(args []string) error {
	return fmt.Errorf("install-hooks lands in the next task")
}
```

Create `cmd/side-quest/hooks.go`:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sharkusk/side-quest/internal/gitcmd"
)

const (
	hookMarker    = "# >>> side-quest >>>"
	hookEndMarker = "# <<< side-quest <<<"
)

// cmdInstallHooks writes (or composes into) the three git hooks and adds the
// refs/side-quest/* refspec. Shims call THIS binary by absolute path, so the
// hooks always run the exact side-quest that installed them (no PATH reliance).
func cmdInstallHooks(args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	g := gitcmd.New(cwd)
	if _, err := g.Run("rev-parse", "--is-inside-work-tree"); err != nil {
		return fmt.Errorf("not a git repository: %w", err)
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, err = filepath.Abs(self)
	if err != nil {
		return err
	}

	hooksDir, err := resolveHooksDir(g)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return err
	}

	q := `"` + self + `"`
	// commit-msg OMITS "|| true" so a require_quest reject (exit 1) blocks the
	// commit; the other two never block the user's workflow.
	shims := []struct{ name, body string }{
		{"prepare-commit-msg", q + ` prepare-commit-msg "$@" || true`},
		{"commit-msg", q + ` commit-msg "$@"`},
		{"post-commit", q + ` link HEAD || true`},
	}
	for _, sh := range shims {
		if err := installOneHook(filepath.Join(hooksDir, sh.name), sh.body); err != nil {
			return err
		}
	}

	addRefspec(g) // best-effort
	fmt.Println("side-quest: hooks installed in", hooksDir)
	return nil
}

// resolveHooksDir honors core.hooksPath, otherwise <common-git-dir>/hooks.
func resolveHooksDir(g *gitcmd.Git) (string, error) {
	top, topErr := g.Run("rev-parse", "--show-toplevel")
	if hp, err := g.Run("config", "--get", "core.hooksPath"); err == nil && hp != "" {
		if filepath.IsAbs(hp) {
			return hp, nil
		}
		if topErr != nil {
			return "", topErr
		}
		return filepath.Join(top, hp), nil
	}
	common, err := g.Run("rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(common) {
		if topErr != nil {
			return "", topErr
		}
		common = filepath.Join(top, common)
	}
	return filepath.Join(common, "hooks"), nil
}

// installOneHook creates a new hook or composes our marker-guarded block into an
// existing one (idempotent: re-install replaces our block, never duplicates it,
// and never clobbers a user's own hook body).
func installOneHook(path, body string) error {
	block := hookMarker + "\n" + body + "\n" + hookEndMarker + "\n"

	existing, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		return writeExec(path, "#!/bin/sh\n"+block)
	}

	text := string(existing)
	if i := strings.Index(text, hookMarker); i >= 0 {
		if j := strings.Index(text, hookEndMarker); j >= 0 {
			end := j + len(hookEndMarker)
			if end < len(text) && text[end] == '\n' {
				end++
			}
			return writeExec(path, text[:i]+block+text[end:])
		}
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return writeExec(path, text+block)
}

// writeExec writes content and ensures the file is executable (WriteFile only
// applies perms when creating, so we chmod explicitly for the compose case).
func writeExec(path, content string) error {
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		return err
	}
	return os.Chmod(path, 0o755)
}

// addRefspec adds push+fetch refspecs for refs/side-quest/* to origin so quest
// data travels with the repo. Best-effort: no origin -> a note, no error.
func addRefspec(g *gitcmd.Git) {
	if _, err := g.Run("remote", "get-url", "origin"); err != nil {
		fmt.Println("side-quest: no 'origin' remote — skipped refspec (add it later or use sync).")
		return
	}
	const refspec = "refs/side-quest/*:refs/side-quest/*"
	ensureConfigContains(g, "remote.origin.fetch", refspec)
	ensureConfigContains(g, "remote.origin.push", refspec)
}

// ensureConfigContains adds val to a multi-valued git config key unless already present.
func ensureConfigContains(g *gitcmd.Git, key, val string) {
	if out, err := g.Run("config", "--get-all", key); err == nil {
		for _, line := range strings.Split(out, "\n") {
			if strings.TrimSpace(line) == val {
				return
			}
		}
	}
	_, _ = g.Run("config", "--add", key, val)
}
```

- [ ] **Step 4: Run the capstone test (and the whole cmd suite)**

Run: `go test ./cmd/side-quest/ -v`
Expected: PASS — including `TestEndToEndHooksDriveLinking`.

- [ ] **Step 5: Run the entire suite with the race detector**

Run: `go test ./... -race && go vet ./... && gofmt -l internal cmd`
Expected: tests PASS, vet clean, `gofmt -l` prints nothing.

- [ ] **Step 6: Update the living docs**

In `docs/architecture.md`, add a new section after the "Atomic id allocation" section (before "Package map"):

````markdown
## Commit linking & hooks (Phase 2)

Quests link to the commits that address them through **git trailers**, applied
by thin hook shims that call the `side-quest` binary. All logic lives in Go.

- `Quest: SQ-0001` — this commit worked on SQ-0001 (append its hash).
- `Completes: SQ-0001` — append the hash **and** close the quest.
- `Quest: none` — explicit escape hatch: a genuine chore, not linked.

Three hooks, installed by `side-quest install-hooks` (which writes/composes
marker-guarded shims and adds a `refs/side-quest/*` push/fetch refspec to
`origin`):

| Hook | Does | Blocks the commit? |
|---|---|---|
| `prepare-commit-msg` | If a current quest is set and `auto_trailer` is on, inject `Quest: <current>`. | Never |
| `commit-msg` | No trailer present → **warn** (assisted) or **reject** (when `require_quest` is on). `Quest: none` satisfies both. | Only on an intentional `require_quest` reject |
| `post-commit` | Run `side-quest link HEAD`: parse the just-made commit's trailers and update each referenced quest. | Never |

**Why this closes the chicken-and-egg:** `post-commit` runs *after* the hash
exists, and the quest update is a separate commit on the orphan ref whose own
hash nobody records. `Link` is tolerant — a trailer naming an unknown quest is
skipped rather than failing the user's already-made commit.

**Hook safety inside git:** git may export `GIT_INDEX_FILE` to hooks. `gitcmd`
collapses duplicate env keys keeping the last value, so the store's scratch
`GIT_INDEX_FILE` always wins and a hook can never mutate the user's real index.

The **current-quest pointer** is worktree-local state (`<git-dir>/side-quest-current`),
not ref state: each worktree has its own, and it never travels with a push.
````

In the `docs/architecture.md` "Package map" table, add a row and update the store row:

```markdown
| `internal/trailer` | Parse Quest:/Completes: trailers + the commit-msg decision | pure |
```

In `docs/architecture.md` → **Dependencies** → the git-version table, add two rows (both below the 2.13 floor — the floor is unchanged):

```markdown
| `rev-parse --git-common-dir` | 2.5 (2015) | resolve the shared hooks dir for `install-hooks` |
| `remote get-url origin` | 2.7 (2016) | detect origin before adding the refspec |
```

In `README.md`, extend the "Concepts (overview)" glossary with:

```markdown
- **trailer** — `Quest: SQ-xxxx` / `Completes: SQ-xxxx` lines in a commit
  message; a `post-commit` hook reads them and links the commit to the quest
  (`Quest: none` opts a chore out).
- **current quest** — a per-worktree pointer (`side-quest current <id>`) that
  `prepare-commit-msg` uses to auto-fill the `Quest:` trailer.
```

- [ ] **Step 7: Verify docs render and nothing else drifted**

Run: `go test ./... && gofmt -l internal cmd`
Expected: PASS; no unformatted files.

- [ ] **Step 8: Commit**

```bash
git add cmd/side-quest/hooks.go cmd/side-quest/main.go cmd/side-quest/main_test.go docs/architecture.md README.md
git commit -m "feat(cmd): install-hooks + shims + refspec; end-to-end commit linking (docs updated)"
```

---

## Self-Review

**1. Spec coverage (§9, §10, §13 hook subset, §15):**
- `Quest:`/`Completes:` trailers, multiple per commit → Task 3 (`Parse`), Task 4 (`Link`). ✅
- `Quest: none` escape hatch → Task 3, Task 6 (`commit-msg`). ✅
- `require_quest` false=warn / true=reject → Task 2 (flag), Task 3 (`Decision`), Task 6 (`commit-msg`), Task 7 (e2e). ✅
- `prepare-commit-msg` injects current when `auto_trailer` on → Task 6, Task 7. ✅
- `post-commit` → `side-quest link <hash>` resolves chicken-and-egg → Task 4, Task 7. ✅
- `install-hooks` composes (no clobber), honors `core.hooksPath`, adds refspec → Task 7. ✅
- Current-quest pointer per worktree, get/set/clear (§10) → Task 5, Task 6. ✅
- Hooks are thin shims; logic in core (§15) → Task 7 shims call binary; all logic in Go. ✅
- Assisted-never-blocking except enforced reject → Task 6 (`commit-msg` exit-code discipline), Task 7 e2e. ✅
- Deferred by tight-slice scope (NOT in this plan): `sync` command, general `config get/set` CLI, `init`/`new`/`list`/`show`/`edit`/`tag`/`done`/`status` CLI, voice layer, importer. Stated in Global Constraints. ✅

**2. Placeholder scan:** No TBD/TODO/"handle edge cases"; every code step shows complete code; the one intentional stub (Task 6 `cmdInstallHooks`) is explicitly flagged and removed in Task 7. ✅

**3. Type consistency:** `Ref{ID, Completes}`, `Parse(message) ([]Ref, bool)`, `Decision(message, requireQuest) Action` with `Accept/Warn/Reject` are used identically in Tasks 3/4/6. `Link(sha string) error`, `Config() (config.Config, error)`, `SetRequireQuest(bool) error`, `SetCurrent/Current/ClearCurrent` match across store code, cmd, and tests. `config.Config.RequireQuest` / `yaml:"require_quest"` consistent. Binary subcommand names (`link`, `current`, `commit-msg`, `prepare-commit-msg`, `install-hooks`) match the shims in Task 7. ✅

---

## Execution Handoff

Two execution options:

1. **Subagent-Driven (recommended)** — fresh subagent per task, spec+quality review between tasks, broad review at the end. Matches how Phase 1 was built.
2. **Inline Execution** — execute tasks in this session with checkpoints.
