# PATH-relative hook shims (design B) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `install-hooks` write PATH-relative git hook shims (bare `side-quest`, guarded by `command -v`) that are byte-identical across machines and degrade gracefully when the binary is absent.

**Architecture:** Replace the absolute-path shim builder in `cmd/side-quest/hooks.go` with a `guardedShim` helper that wraps each `side-quest` call in an `if command -v … then … else warn … fi` block. The one blocking hook (`commit-msg`) omits `|| true` so a real reject still blocks; a *missing* binary always exits 0. The marker/version/compose machinery is unchanged, so upgrades refresh old absolute-path blocks in place.

**Tech Stack:** Go, POSIX `sh` (hook runtime), git.

## Global Constraints

- **TDD, no exceptions for code:** RED → GREEN → REFACTOR. No production code without a failing test first. Docs are TDD-exempt.
- **Shim guard is `if/else`, never early `exit`** — control must flow through the block so trailing hook content still runs.
- **`commit-msg` shim carries NO `|| true`** on its `side-quest` line; the other three hooks (`prepare-commit-msg`, `post-commit`, `pre-push`) do.
- **A missing binary never blocks a commit** — the `else` (skip) branch exits 0 in every hook.
- **No absolute path anywhere in a shim** — invoke `side-quest` by bare name only.
- **Branch-safety (HARD):** only ever write `refs/side-quest/*`, `.git/hooks` (or configured hooksPath), and a scratch index. Never the user's branches/index/worktree.
- **Commit only when the user asks.** Executing this plan authorizes its per-task commits.
- **Commit trailer** (blank line before the co-author block):
  ```
  Quest: SQ-0058

  Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws
  ```
  Do NOT use `Completes: SQ-0058` — the quest stays open until the manual plugin-PATH verification (post-plan) passes.
- **Living docs** updated in the same change as behavior (Task 3).

---

### Task 1: `guardedShim` helper + wire it in, remove absolute-path machinery

**Files:**
- Modify: `cmd/side-quest/hooks.go` (add `guardedShim`; rewrite the `shims` table at ~82-87; delete the `os.Executable()`/`filepath.Abs` block at 56-63; delete `shimQuotedPath` at 135-147; fix the `cmdInstallHooks` doc comment at 44-46)
- Modify: `cmd/side-quest/hooks_test.go` (add `TestGuardedShimIsPathRelative`; delete `TestShimQuotedPathNormalizesSlashes` at 149-163, whose function no longer exists)

**Interfaces:**
- Produces: `func guardedShim(invocation string, block bool) string` — renders a hook body that runs `side-quest <invocation>` only when it's on `PATH`, else warns and skips. `block=false` appends `|| true`; `block=true` omits it.
- Consumes: existing `hookBlock(body, version string) string`, the `shims []struct{ name, body string }` table, `installOneHook`.

- [ ] **Step 1: Write the failing test**

Add to `cmd/side-quest/hooks_test.go`:

```go
// TestGuardedShimIsPathRelative (SQ-0058): shims invoke `side-quest` via PATH
// (no absolute path), guarded by `command -v`; only the blocking commit-msg
// omits `|| true` so a require_quest reject can still block the commit.
func TestGuardedShimIsPathRelative(t *testing.T) {
	nonBlocking := guardedShim("link HEAD", false)
	if !strings.Contains(nonBlocking, "if command -v side-quest >/dev/null 2>&1; then\n") {
		t.Errorf("missing PATH guard:\n%s", nonBlocking)
	}
	if !strings.Contains(nonBlocking, "\tside-quest link HEAD || true\n") {
		t.Errorf("non-blocking hook must call bare side-quest with || true:\n%s", nonBlocking)
	}
	blocking := guardedShim(`commit-msg "$@"`, true)
	if !strings.Contains(blocking, "\tside-quest commit-msg \"$@\"\n") {
		t.Errorf("blocking hook must call bare side-quest:\n%s", blocking)
	}
	if strings.Contains(blocking, "|| true") {
		t.Errorf("commit-msg must NOT append || true (it must be able to block):\n%s", blocking)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/side-quest/ -run TestGuardedShimIsPathRelative`
Expected: FAIL — compile error, `undefined: guardedShim`.

- [ ] **Step 3: Add `guardedShim` to `cmd/side-quest/hooks.go`**

Insert this function (e.g. just above `installOneHook`):

```go
// guardedShim renders a PATH-relative hook body: it runs `side-quest
// <invocation>` only when the binary is on PATH, and otherwise warns and skips
// (never blocking). The if/else lets control flow THROUGH the block, so any
// hook content after our marker block still runs (an early exit would skip it).
// block=false appends `|| true` so the hook never fails on side-quest's own exit
// status; block=true (commit-msg) omits it so a require_quest reject still blocks
// the commit. A MISSING binary always takes the else branch and exits 0 (SQ-0058).
func guardedShim(invocation string, block bool) string {
	tail := " || true"
	if block {
		tail = ""
	}
	return "if command -v side-quest >/dev/null 2>&1; then\n" +
		"\tside-quest " + invocation + tail + "\n" +
		"else\n" +
		"\techo \"side-quest: not on PATH — skipping (add it to PATH; see install-hooks)\" >&2\n" +
		"fi"
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/side-quest/ -run TestGuardedShimIsPathRelative`
Expected: PASS.

- [ ] **Step 5: Wire `guardedShim` into the shims table and remove the absolute-path machinery**

In `cmd/side-quest/hooks.go`, delete the executable-path lookup (currently lines 56-63):

```go
	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, err = filepath.Abs(self)
	if err != nil {
		return err
	}
```

Replace the `q := shimQuotedPath(self)` line and the `shims` table (currently 79-87) with:

```go
	// Shims call `side-quest` via PATH so they are byte-identical across machines
	// (committable into a shared hooks dir) and degrade gracefully when the binary
	// is absent. commit-msg is the one blocking hook (block=true): it keeps no
	// `|| true`, so a require_quest reject blocks the commit; the others never block.
	shims := []struct{ name, body string }{
		{"prepare-commit-msg", guardedShim(`prepare-commit-msg "$@"`, false)},
		{"commit-msg", guardedShim(`commit-msg "$@"`, true)},
		{"post-commit", guardedShim("link HEAD", false)},
		{"pre-push", guardedShim(`pre-push "$@"`, false)},
	}
```

Delete `shimQuotedPath` entirely (currently 135-147, the doc comment plus the function).

Update the `cmdInstallHooks` doc comment (currently 44-46) from:

```go
// cmdInstallHooks writes (or composes into) the three git hooks and migrates
// origin's refspecs to the sync model. Shims call THIS binary by absolute path, so the
// hooks always run the exact side-quest that installed them (no PATH reliance).
```

to:

```go
// cmdInstallHooks writes (or composes into) the four git hooks and migrates
// origin's refspecs to the sync model. Shims call `side-quest` via PATH (not by
// absolute path), so the installed block is byte-identical across machines —
// committable into a shared hooks dir — and skips gracefully when the binary is
// absent (SQ-0058).
```

- [ ] **Step 6: Delete the obsolete `shimQuotedPath` test**

Delete `TestShimQuotedPathNormalizesSlashes` (currently `cmd/side-quest/hooks_test.go` 149-163) — its subject function no longer exists.

- [ ] **Step 7: Verify build and full hook tests pass**

Run: `go build ./... && go test ./cmd/side-quest/`
Expected: PASS. No `undefined`/`declared and not used` errors (confirms `os`/`filepath` are still otherwise used and no orphan imports remain).

- [ ] **Step 8: Commit**

```bash
git add cmd/side-quest/hooks.go cmd/side-quest/hooks_test.go
git commit
```
Use the trailer from Global Constraints (`Quest: SQ-0058`). Message subject: `feat: PATH-relative hook shims with command -v guard (SQ-0058)`.

---

### Task 2: Behavioral tests — blocking semantics, graceful skip, compose-safety, migration

**Files:**
- Modify: `cmd/side-quest/hooks_test.go` (add three helpers + five behavioral tests + one migration test)

**Interfaces:**
- Consumes: `guardedShim` (Task 1), `hookBlock`, `installOneHook`, `hookUpdated`, `readFileStr` (existing helper).
- Produces: nothing consumed downstream (tests only).

These tests render the real shim under `sh` with a controlled `PATH` to prove runtime behavior, not just text.

- [ ] **Step 1: Add the test helpers and the imports they need**

At the top of `cmd/side-quest/hooks_test.go`, add `"bytes"` and `"fmt"` to the import block (keep the existing imports). Then add:

```go
// runHook writes script to a temp executable and runs it under sh with exactly
// the given PATH, returning stdout, stderr, and the exit code. A controlled PATH
// lets a test simulate the binary being present (a stub dir) or absent.
func runHook(t *testing.T, script, pathEnv string, args ...string) (string, string, int) {
	t.Helper()
	f := filepath.Join(t.TempDir(), "hook")
	if err := os.WriteFile(f, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", append([]string{f}, args...)...)
	cmd.Env = []string{"PATH=" + pathEnv}
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run hook: %v", err)
	}
	return so.String(), se.String(), code
}

// stubSideQuest writes a fake `side-quest` that exits exitCode into a fresh dir,
// and returns that dir so a test can put it on PATH.
func stubSideQuest(t *testing.T, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\nexit %d\n", exitCode)
	if err := os.WriteFile(filepath.Join(dir, "side-quest"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// renderHook builds a complete, runnable hook file the way installOneHook does
// for a fresh hook: shebang + the marker-guarded block.
func renderHook(body string) string {
	return "#!/bin/sh\n" + hookBlock(body, "test")
}
```

- [ ] **Step 2: Write the failing behavioral tests**

Add:

```go
// TestCommitMsgShimMissingBinaryNeverBlocks (SQ-0058): with side-quest absent,
// the blocking commit-msg hook must still exit 0 (commit proceeds) and warn.
func TestCommitMsgShimMissingBinaryNeverBlocks(t *testing.T) {
	_, se, code := runHook(t, renderHook(guardedShim(`commit-msg "$@"`, true)), "/nonexistent")
	if code != 0 {
		t.Errorf("missing binary must not block the commit, got exit %d", code)
	}
	if !strings.Contains(se, "not on PATH") {
		t.Errorf("expected a skip warning on stderr, got:\n%s", se)
	}
}

// TestCommitMsgShimBlocksOnReject (SQ-0058): a present side-quest that rejects
// (exit 1) must propagate non-zero so the commit is blocked.
func TestCommitMsgShimBlocksOnReject(t *testing.T) {
	_, _, code := runHook(t, renderHook(guardedShim(`commit-msg "$@"`, true)), stubSideQuest(t, 1))
	if code == 0 {
		t.Errorf("a rejecting side-quest must block the commit (non-zero exit), got 0")
	}
}

// TestCommitMsgShimPassesOnAccept (SQ-0058): a present side-quest that accepts
// (exit 0) leaves the commit unblocked.
func TestCommitMsgShimPassesOnAccept(t *testing.T) {
	_, _, code := runHook(t, renderHook(guardedShim(`commit-msg "$@"`, true)), stubSideQuest(t, 0))
	if code != 0 {
		t.Errorf("an accepting side-quest must allow the commit, got exit %d", code)
	}
}

// TestNonBlockingShimMissingBinarySkips (SQ-0058): a non-blocking hook
// (post-commit) exits 0 and warns when the binary is absent.
func TestNonBlockingShimMissingBinarySkips(t *testing.T) {
	_, se, code := runHook(t, renderHook(guardedShim("link HEAD", false)), "/nonexistent")
	if code != 0 {
		t.Errorf("non-blocking shim must exit 0 when binary missing, got %d", code)
	}
	if !strings.Contains(se, "not on PATH") {
		t.Errorf("expected a skip warning, got:\n%s", se)
	}
}

// TestShimComposeSafetyFlowsThrough (SQ-0058): the else branch must NOT early-exit
// — content after our block still runs. Simulate a composed hook: our block then
// a trailing `echo AFTER`, binary absent; AFTER must still print.
func TestShimComposeSafetyFlowsThrough(t *testing.T) {
	script := "#!/bin/sh\n" + hookBlock(guardedShim("link HEAD", false), "test") + "echo AFTER\n"
	so, _, code := runHook(t, script, "/nonexistent")
	if code != 0 {
		t.Errorf("hook should exit 0, got %d", code)
	}
	if !strings.Contains(so, "AFTER") {
		t.Errorf("content after the block must still run (no early exit), stdout:\n%s", so)
	}
}

// TestInstallOneHookMigratesAbsoluteToPathRelative (SQ-0058): re-installing over
// an old absolute-path block replaces it in place (hookUpdated) with the portable
// shim, dropping the machine-local path — the migration path for existing installs.
func TestInstallOneHookMigratesAbsoluteToPathRelative(t *testing.T) {
	path := filepath.Join(t.TempDir(), "post-commit")
	old := `"/Users/x/go/bin/side-quest" link HEAD || true`
	if _, _, err := installOneHook(path, old, "0.0.9"); err != nil {
		t.Fatal(err)
	}
	oc, prev, err := installOneHook(path, guardedShim("link HEAD", false), "0.1.0")
	if err != nil || oc != hookUpdated || prev != "0.0.9" {
		t.Fatalf("migrate: oc=%v prev=%q err=%v", oc, prev, err)
	}
	got := readFileStr(t, path)
	if strings.Contains(got, "/Users/x/go/bin/side-quest") {
		t.Errorf("old absolute path not removed:\n%s", got)
	}
	if !strings.Contains(got, "command -v side-quest") {
		t.Errorf("portable shim not written:\n%s", got)
	}
}
```

- [ ] **Step 3: Run the new tests**

Run: `go test ./cmd/side-quest/ -run 'CommitMsgShim|NonBlockingShim|ShimComposeSafety|MigratesAbsolute'`
Expected: PASS (all six). They exercise Task 1's already-shipped code, so they should pass immediately; if any fails, the shim logic is wrong — fix `guardedShim`, not the test.

Note on TDD: Task 1 delivered the behavior, so these characterization tests pass on first run. That is acceptable here — they lock in the runtime contract (blocking vs skip) that the Task 1 text assertions cannot prove. Do not weaken a test to make it pass; a failure means the contract is broken.

- [ ] **Step 4: Run the full package to confirm no regressions**

Run: `go test ./cmd/side-quest/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/side-quest/hooks_test.go
git commit
```
Trailer `Quest: SQ-0058`. Subject: `test: shim blocking/skip/compose-safety/migration behavior (SQ-0058)`.

---

### Task 3: Documentation

**Files:**
- Modify: `docs/manual-setup.md` (install-hooks description ~31-35; "Existing git hooks" ~37-49)
- Modify: `docs/plugin.md` (add one PATH line)

Docs are TDD-exempt. Each change is a plain edit; verify by re-reading.

- [ ] **Step 1: Update the install-hooks description in `docs/manual-setup.md`**

In the paragraph beginning "`init` creates the orphan ref" (~29-35), append one sentence after the existing description of the shims:

> The shims call `side-quest` via your `PATH` (not by an absolute path), so the installed block is identical on every machine — you can commit it into a shared hooks dir — and it skips with a warning if `side-quest` is not on the `PATH` git runs under.

- [ ] **Step 2: Update "Existing git hooks" in `docs/manual-setup.md`**

In that section (~37-49), after the sentence explaining that `install-hooks` "composes into whatever hooks directory git uses, appending its own marker-guarded block", add:

> Because the appended block invokes `side-quest` via `PATH`, it is safe to **commit** into a shared hook — it carries no machine-local path. The trade-off: the hook needs `side-quest` on the `PATH` that git runs under. A terminal or agent that has side-quest on `PATH` works; a GUI client or cron job launched without it simply skips the side-quest step (with a warning) rather than failing. See the `PATH` note under [Wire up your agent](#wire-up-your-agent) — the hooks share that dependency.

- [ ] **Step 3: Add the PATH line to `docs/plugin.md`**

After the paragraph about the plugin putting `side-quest` on the *agent's* `PATH` (~20-26), add:

> The git hooks rely on that same `PATH`: an agent-run `git commit` finds `side-quest` and records the link, while a commit from your own terminal only does so if you've [installed side-quest](install.md) there — otherwise the hook skips cleanly.

- [ ] **Step 4: Verify**

Run: `git diff --stat docs/`
Expected: `docs/manual-setup.md` and `docs/plugin.md` modified. Re-read both changed sections to confirm the prose reads correctly and links resolve.

- [ ] **Step 5: Commit**

```bash
git add docs/manual-setup.md docs/plugin.md
git commit
```
Trailer `Quest: SQ-0058`. Subject: `docs: PATH-relative shims are committable and PATH-dependent (SQ-0058)`.

---

## Post-plan: manual verification & closing SQ-0058

These are NOT automated tasks — do them after the three tasks land, then close the quest.

1. **Rebuild + reinstall in this repo:** `make install` then `side-quest install-hooks`. Confirm the refresh note fires (old absolute block → portable) and inspect a hook (e.g. `.githooks/post-commit` or `.git/hooks/post-commit`) to see the `command -v side-quest` guard with no absolute path.
2. **Plugin-PATH end-to-end (the spec's flagged assumption):** in a repo with side-quest installed *only* via the Claude Code plugin (no `side-quest` on the shell `PATH` — e.g. no `~/go/bin` copy), have the agent make a commit and confirm the `post-commit` link actually fired (the quest gained the commit). If it did NOT, the plugin's `PATH` injection does not reach agent-run git: file that as a **separate** finding (plugin PATH plumbing) and make the skip-warning discoverable rather than swallowed — do not close SQ-0058 as fully done.
3. **SQ-0057 decision:** this plan keeps the shims gitignored in side-quest's own repo. Commit the pending `.gitignore` fix (the four-entry block) or leave it, per the user.
4. **Close:** once verification passes, set SQ-0058 status done via `quest_set_status` (or a final commit with `Completes: SQ-0058`).
