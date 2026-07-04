# `side-quest show` dumps linked commit messages Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the human `side-quest show` render each linked commit as `<short-sha>  <subject>` (default) or the complete message (`--full`), replacing today's bare comma-joined SHA line.

**Architecture:** A new `store.CommitMessage` reads a commit's abbreviated SHA + message via the store's existing git handle. `cmdShow` gains a `--full` flag, resolves each linked commit through it, and passes the result to `renderShow`, which prints a per-commit block. MCP `quest_show` and `show --json` are untouched.

**Tech Stack:** Go, `git show -s --format`, the existing `internal/gitcmd`.

## Global Constraints

- **CLI human render only.** No change to MCP `quest_show` or to `show --json` (still the raw quest with 40-char SHAs).
- **TDD:** every new function gets a failing test first.
- **Each linked SHA appears once** — the per-commit listing replaces the old `commits: <joined>` line; no separate redundant SHA line.
- **A missing commit** (SHA no longer resolves) renders `<stored-sha>  (message unavailable)` and `show` still exits 0.
- **Flag name is `--full`.**
- **Branch-safety (HARD):** only ever write `refs/side-quest/*`, `.git/hooks` (or configured hooksPath), and a scratch index. Never the user's branches/index/worktree.
- **Commit only when the user asks.** Executing this plan authorizes its per-task commits.
- **Commit trailer** (blank line before the co-author block):
  ```
  Quest: SQ-0062        (Task 1)   /   Completes: SQ-0062   (Task 2, the final task)

  Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws
  ```

---

### Task 1: `store.CommitMessage` — read a commit's SHA + message

**Files:**
- Modify: `internal/store/store.go` (add the method near `readFile`)
- Test: `internal/store/commitmsg_test.go` (new)

**Interfaces:**
- Produces: `func (s *Store) CommitMessage(sha string, full bool) (short, text string, ok bool)` — `full=false` → `text` is the subject line; `full=true` → `text` is the complete message; `ok=false` when `sha` no longer resolves.
- Consumes: the existing `s.git *gitcmd.Git`; the test helper `commitInWorktree(t, s, filename, message string) string` and `newStore(t)` (both already in `internal/store`).

- [ ] **Step 1: Write the failing test**

Create `internal/store/commitmsg_test.go`:

```go
package store

import (
	"strings"
	"testing"
)

func TestCommitMessageSubjectAndFull(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	sha := commitInWorktree(t, s, "f.txt", "feat: do a thing\n\nsome body detail\n\nToken: xyz\n")

	short, subj, ok := s.CommitMessage(sha, false)
	if !ok {
		t.Fatal("CommitMessage(full=false) ok=false for a real commit")
	}
	if subj != "feat: do a thing" {
		t.Errorf("subject = %q, want %q", subj, "feat: do a thing")
	}
	if short == "" || !strings.HasPrefix(sha, short) {
		t.Errorf("short %q is not an abbreviation of %q", short, sha)
	}

	_, full, ok := s.CommitMessage(sha, true)
	if !ok {
		t.Fatal("CommitMessage(full=true) ok=false for a real commit")
	}
	for _, want := range []string{"feat: do a thing", "some body detail", "Token: xyz"} {
		if !strings.Contains(full, want) {
			t.Errorf("full message missing %q:\n%s", want, full)
		}
	}
}

func TestCommitMessageMissingReturnsNotOK(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s.CommitMessage("deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", false); ok {
		t.Error("CommitMessage for an unknown sha should return ok=false")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/store/ -run TestCommitMessage`
Expected: FAIL — compile error, `s.CommitMessage undefined`.

- [ ] **Step 3: Implement `CommitMessage`**

In `internal/store/store.go`, add (near `readFile`):

```go
// CommitMessage returns a linked commit's abbreviated SHA and its message, for
// the `show` command. full=false returns the subject line as text; full=true
// returns the complete message. ok is false when sha no longer resolves to a
// commit (rebased or gc'd) — the caller renders a placeholder rather than
// failing the whole command.
func (s *Store) CommitMessage(sha string, full bool) (short, text string, ok bool) {
	format := "%h%x00%s"
	if full {
		format = "%h%x00%B"
	}
	out, err := s.git.Run("show", "-s", "--format="+format, sha)
	if err != nil {
		return "", "", false
	}
	short, msg, found := strings.Cut(out, "\x00")
	if !found {
		return "", "", false
	}
	return short, strings.TrimRight(msg, "\n"), true
}
```

(`strings` is already imported in `store.go`.)

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/store/ -run TestCommitMessage`
Expected: PASS (both).

- [ ] **Step 5: Commit**

```bash
git add internal/store/store.go internal/store/commitmsg_test.go
git commit
```
Trailer `Quest: SQ-0062`. Subject: `feat: store.CommitMessage reads a commit's sha + message (SQ-0062)`.

---

### Task 2: `--full` flag + per-commit render in `show`

**Files:**
- Modify: `cmd/side-quest/render.go` (add `commitLine` type; change `renderShow` signature + commits block)
- Modify: `cmd/side-quest/cli.go` (`cmdShow`: add `--full`, resolve commits, pass to `renderShow`)
- Test: `cmd/side-quest/render_test.go` (render-unit test), `cmd/side-quest/cli_test.go` (end-to-end)

**Interfaces:**
- Consumes: `store.CommitMessage(sha, full) (short, text string, ok bool)` from Task 1.
- Produces: `type commitLine struct { short, text string }`; `renderShow(w io.Writer, q *quest.Quest, width int, commits []commitLine)`.

- [ ] **Step 1: Write the failing render-unit test**

Add to `cmd/side-quest/render_test.go` (package `main`):

```go
func TestRenderShowCommitBlock(t *testing.T) {
	// Only the commits block is under test, so a minimal quest suffices (renderShow
	// prints empty strings for the unset status/type/priority fields — fine here).
	q := &quest.Quest{ID: "SQ-0001", Title: "t", Created: time.Now()}
	commits := []commitLine{
		{short: "b510826", text: "refactor: move the thing"},
		{short: "d5eb4b2", text: "docs: reword it\n\nbody line here\n\nToken: xyz"},
		{short: "cafef00dcafef00d", text: "(message unavailable)"},
	}
	var buf bytes.Buffer
	renderShow(&buf, q, 0, commits)
	out := buf.String()

	for _, want := range []string{
		"commits:",
		"b510826  refactor: move the thing",
		"d5eb4b2  docs: reword it",
		"body line here",          // --full body is printed
		"cafef00dcafef00d  (message unavailable)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q:\n%s", want, out)
		}
	}
	// The subject must NOT be duplicated inside the body block.
	if strings.Count(out, "docs: reword it") != 1 {
		t.Errorf("subject duplicated in body:\n%s", out)
	}
}
```

(Ensure `render_test.go` imports `bytes`, `time`, `strings`, and the `quest` package; add any that are missing.)

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./cmd/side-quest/ -run TestRenderShowCommitBlock`
Expected: FAIL — `renderShow` takes 3 args / `commitLine` undefined.

- [ ] **Step 3: Update `renderShow` and add `commitLine` (render.go)**

In `cmd/side-quest/render.go`, add the type above `renderShow`:

```go
// commitLine is one linked commit resolved for display: short is the abbreviated
// sha (or the stored sha when the commit is gone), and text is the subject line
// (default) or the complete message (--full). A missing commit has
// text == "(message unavailable)".
type commitLine struct {
	short, text string
}
```

Change the signature:

```go
func renderShow(w io.Writer, q *quest.Quest, width int, commits []commitLine) {
```

Replace the current commits block:

```go
	if len(q.Commits) > 0 {
		showField(w, "commits", strings.Join(q.Commits, ", "), width)
	}
```

with:

```go
	if len(commits) > 0 {
		fmt.Fprintln(w, "commits:")
		for _, c := range commits {
			subject, rest, _ := strings.Cut(c.text, "\n")
			for _, wl := range wrapText("  "+c.short+"  "+subject, width) {
				fmt.Fprintln(w, wl)
			}
			if body := strings.Trim(rest, "\n"); body != "" {
				fmt.Fprintln(w)
				for _, bl := range strings.Split(body, "\n") {
					for _, wl := range wrapText("      "+bl, width) {
						fmt.Fprintln(w, wl)
					}
				}
				fmt.Fprintln(w)
			}
		}
	}
```

- [ ] **Step 4: Wire `cmdShow` (cli.go): add `--full`, resolve commits, pass them**

In `cmd/side-quest/cli.go` `cmdShow`, add the flag beside `asJSON`/`noWrap`:

```go
	var full bool
	fs.BoolVar(&full, "full", false, "with the linked commits, print each commit's complete message (default: subject only)")
```

Update the usage string to mention it:

```go
	setUsage(fs, "usage: side-quest show [flags] <id>\nshow one quest; --full prints linked commits' complete messages; <id> accepts shorthand (11 or 0011 for SQ-0011)")
```

The `--json` branch is unchanged (it returns before commit resolution). Replace the final render call:

```go
	renderShow(os.Stdout, q, width)
```

with the resolution + call (`s` is the store already opened above):

```go
	var commits []commitLine
	for _, sha := range q.Commits {
		short, text, ok := s.CommitMessage(sha, full)
		if !ok {
			commits = append(commits, commitLine{short: sha, text: "(message unavailable)"})
			continue
		}
		commits = append(commits, commitLine{short: short, text: text})
	}
	renderShow(os.Stdout, q, width, commits)
```

- [ ] **Step 5: Run the render-unit test to verify it passes**

Run: `go test ./cmd/side-quest/ -run TestRenderShowCommitBlock`
Expected: PASS.

- [ ] **Step 6: Write the end-to-end CLI test**

Add to `cmd/side-quest/cli_test.go` (it already imports `gitcmd`, `os`, `path/filepath`, `strings`, `testing`):

```go
func TestShowRendersCommitMessages(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)
	t.Setenv("SIDE_QUEST_TONE", "plain")

	out, _ := runBin(t, bin, dir, "new", "Commit render")
	id := idFromCreated(t, out)
	if _, code := runBin(t, bin, dir, "current", id); code != 0 {
		t.Fatalf("set current exit=%d", code)
	}

	// A real commit carrying the quest trailer, then link it.
	g := gitcmd.New(dir)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run("add", "f.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run("commit", "-m", "feat: a thing\n\nbody detail here\n\nQuest: "+id); err != nil {
		t.Fatal(err)
	}
	if _, code := runBin(t, bin, dir, "link", "HEAD"); code != 0 {
		t.Fatalf("link exit=%d", code)
	}

	// Default: subject line, no full body.
	out, code := runBin(t, bin, dir, "show", id)
	if code != 0 || !strings.Contains(out, "feat: a thing") {
		t.Fatalf("show default: exit=%d out=%s", code, out)
	}
	if strings.Contains(out, "body detail here") {
		t.Errorf("default show must not print the commit body:\n%s", out)
	}

	// --full: includes the body.
	out, code = runBin(t, bin, dir, "show", "--full", id)
	if code != 0 || !strings.Contains(out, "body detail here") {
		t.Fatalf("show --full must print the body: exit=%d out=%s", code, out)
	}
}
```

- [ ] **Step 7: Run the full package suite**

Run: `go test ./cmd/side-quest/`
Expected: PASS. The existing `show` tests still pass — they create commit-less quests, so the commits block is omitted and their `--json` assertions are unaffected.

- [ ] **Step 8: Build the whole module**

Run: `go build ./... && go vet ./cmd/side-quest/`
Expected: clean.

- [ ] **Step 9: Commit**

```bash
git add cmd/side-quest/render.go cmd/side-quest/cli.go cmd/side-quest/render_test.go cmd/side-quest/cli_test.go
git commit
```
Trailer `Completes: SQ-0062`. Subject: `feat: show renders linked commit messages, --full for full text (SQ-0062)`.

---

## Notes for the implementer
- The `--full` vs default distinction lives entirely in what `cmdShow` puts in `commitLine.text` (subject vs whole message); `renderShow` renders whatever it is — header is the first line, anything after prints indented. So the renderer needs no `full` flag.
- Don't add commit-message fields to `show --json` or the MCP `quest_show` — out of scope (spec §"Out of scope").
