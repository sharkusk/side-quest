# Onboard: the single, plugin-aware front door — Implementation Plan (SQ-0064)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `side-quest onboard` the one command users and agents run to set up or refresh a repo in both distribution paths, and make it skip writing a project `.mcp.json` when the Claude Code plugin is already supplying the MCP server.

**Architecture:** Add a small, pure plugin-detection helper (`cmd/side-quest/plugin.go`). Gate `onboard`'s `.mcp.json` step on `!pluginActive()` so the plugin path never creates a redundant server, silently. Regroup the top-level `usage` text so `onboard` leads and `init`/`install-hooks` move to an "Advanced" grouping (they stay valid subcommands `onboard` composes).

**Tech Stack:** Go (stdlib only for this plan — `os`, `path/filepath`, `strings`); existing test harness in `cmd/side-quest/*_test.go` (`buildBinary`, `newRepo`, `runBin`, `gitcmd`).

## Global Constraints

- **TDD, no exceptions for code:** RED (write failing test) → verify it fails → GREEN (minimal code) → verify it passes → commit. Never write production code before a failing test. Docs-only edits are TDD-exempt and ride the commit of the task whose behavior they describe.
- **Branch-safety invariant (HARD RULE):** side-quest may only ever write `refs/side-quest/*`, git hooks (or the configured `hooksPath`), and a scratch index. Nothing in this plan writes a user branch/index/worktree — do not introduce any such write.
- **Silent skip:** when the plugin is active, `onboard` must write no `.mcp.json` **and print nothing about it** — `.mcp.json` is internal plumbing an end user need not hear about (spec D6).
- **Plugin-detection signals (exactly these two, OR'd):** `CLAUDE_PLUGIN_DATA` is a non-empty env var, **or** the running binary's path is under `<home>/.claude/plugins/`. No cache-dir parsing.
- **Commit trailer format** (every commit; blank line before the co-author block, matching prior commits on this repo):

  ```
  <subject> (SQ-0064)

  <optional one-line body>

  Quest: SQ-0064

  Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws
  ```

- **Commit/push discipline:** commit per task as the steps specify; do **not** `git push` (the user pushes explicitly).
- **Test command:** `go test ./cmd/side-quest/` runs this package's suite; single tests via `-run`.

---

## File Structure

- **Create `cmd/side-quest/plugin.go`** — plugin detection: `pluginActive() bool` (reads env + `os.Executable()` + `os.UserHomeDir()`) delegating to the pure `pluginActiveFrom(pluginData, exePath, home string) bool`. One responsibility: "are we running under the plugin?"
- **Create `cmd/side-quest/plugin_test.go`** — table test for `pluginActiveFrom`.
- **Modify `cmd/side-quest/onboard.go:179-199`** — gate the `.mcp.json` step on `!pluginActive()`; keep the `top` computation unconditional (step 4 still needs it).
- **Modify `cmd/side-quest/onboard_test.go`** — add the plugin-active skip test; pin `CLAUDE_PLUGIN_DATA=""` in the two tests that assert `.mcp.json` **is** written, so ambient env can't flip them.
- **Modify `cmd/side-quest/main.go:18-47`** — regroup the `usage` const (Setup / Quests / Advanced), demoting `init` and `install-hooks`.
- **Modify `cmd/side-quest/main_test.go`** — add a usage-grouping test.
- **Modify `docs/architecture.md:378-382`** — keep the `onboard` bullet in sync (plugin-aware `.mcp.json`, then the Advanced framing).

---

### Task 1: Plugin detection helper

**Files:**
- Create: `cmd/side-quest/plugin.go`
- Test: `cmd/side-quest/plugin_test.go`

**Interfaces:**
- Produces: `pluginActive() bool` (used by Task 2) and `pluginActiveFrom(pluginData, exePath, home string) bool` (pure core, used by the test).

- [ ] **Step 1: Write the failing test**

Create `cmd/side-quest/plugin_test.go`:

```go
package main

import (
	"path/filepath"
	"testing"
)

// pluginActiveFrom is the pure core of plugin detection: CLAUDE_PLUGIN_DATA being
// set, or the running binary residing under <home>/.claude/plugins/, means the
// Claude Code plugin is active (SQ-0064, D6).
func TestPluginActiveFrom(t *testing.T) {
	home := t.TempDir()
	underPlugins := filepath.Join(home, ".claude", "plugins", "data",
		"side-quest-side-quest", "bin", "side-quest-1.2.3")
	elsewhere := filepath.Join(t.TempDir(), "side-quest")

	cases := []struct {
		name       string
		pluginData string
		exePath    string
		home       string
		want       bool
	}{
		{"env set wins", "/somewhere/data", elsewhere, home, true},
		{"exe under plugins dir", "", underPlugins, home, true},
		{"exe elsewhere, no env", "", elsewhere, home, false},
		{"empty everything", "", "", "", false},
		{"exe set but home empty", "", underPlugins, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := pluginActiveFrom(c.pluginData, c.exePath, c.home); got != c.want {
				t.Errorf("pluginActiveFrom(%q,%q,%q) = %v, want %v",
					c.pluginData, c.exePath, c.home, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/side-quest/ -run TestPluginActiveFrom`
Expected: FAIL — build error `undefined: pluginActiveFrom`.

- [ ] **Step 3: Write minimal implementation**

Create `cmd/side-quest/plugin.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"strings"
)

// pluginActive reports whether this side-quest process is running as part of the
// Claude Code plugin. onboard uses it to skip writing a project .mcp.json — the
// plugin already registers the side-quest MCP server, so a second identically
// named one would be redundant (SQ-0064, D6).
func pluginActive() bool {
	exe, _ := os.Executable()
	home, _ := os.UserHomeDir()
	return pluginActiveFrom(os.Getenv("CLAUDE_PLUGIN_DATA"), exe, home)
}

// pluginActiveFrom is the pure core of pluginActive, taking its three inputs
// explicitly so the detection logic is testable without a real plugin install.
// Two independent signals, per the design's established plugin facts:
//   - CLAUDE_PLUGIN_DATA is set — the plugin's persistent data dir, exported into
//     every Claude-spawned process (e.g. the MCP server).
//   - the running binary lives under <home>/.claude/plugins/ — the terminal
//     launcher execs the data-dir binary, so even outside a Claude process the
//     executable path betrays the plugin origin.
func pluginActiveFrom(pluginData, exePath, home string) bool {
	if pluginData != "" {
		return true
	}
	if home == "" || exePath == "" {
		return false
	}
	pluginsDir := filepath.Join(home, ".claude", "plugins") + string(os.PathSeparator)
	return strings.HasPrefix(exePath, pluginsDir)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/side-quest/ -run TestPluginActiveFrom`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/side-quest/plugin.go cmd/side-quest/plugin_test.go
git commit -m "feat: pluginActive detects the Claude Code plugin (SQ-0064)" \
  -m "Detect via CLAUDE_PLUGIN_DATA or the binary residing under ~/.claude/plugins/, so onboard can adapt its .mcp.json behavior." \
  -m "Quest: SQ-0064" \
  -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws"
```

---

### Task 2: `onboard` skips `.mcp.json` when the plugin is active

**Files:**
- Modify: `cmd/side-quest/onboard.go:179-199`
- Modify: `cmd/side-quest/onboard_test.go` (add one test; pin env in two existing tests)
- Modify: `docs/architecture.md:378-382`

**Interfaces:**
- Consumes: `pluginActive()` from Task 1.

- [ ] **Step 1: Write the failing test**

Add to `cmd/side-quest/onboard_test.go` (the file already imports `os`, `path/filepath`, `strings`, `testing`, and `gitcmd`):

```go
// With the plugin active (CLAUDE_PLUGIN_DATA set), onboard still wires the repo
// (ref + hooks) but skips writing .mcp.json — the plugin already registers the
// MCP server — and says nothing about the skip (SQ-0064, D6).
func TestOnboardSkipsMcpJsonUnderPlugin(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)
	t.Setenv("CLAUDE_PLUGIN_DATA", t.TempDir()) // simulate the plugin's data dir

	out, code := runBin(t, bin, dir, "onboard")
	if code != 0 {
		t.Fatalf("onboard exit=%d out=%q", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); !os.IsNotExist(err) {
		t.Errorf("onboard under the plugin must not write .mcp.json (stat err=%v)", err)
	}
	if strings.Contains(out, ".mcp.json") {
		t.Errorf("onboard under the plugin must not mention .mcp.json; got:\n%s", out)
	}
	// Repo is still wired: quest ref + hooks.
	if _, err := os.Stat(filepath.Join(dir, ".git", "hooks", "post-commit")); err != nil {
		t.Errorf("onboard did not install hooks under the plugin: %v", err)
	}
	g := gitcmd.New(dir)
	if ref, _ := g.Run("for-each-ref", "--format=%(objectname)", "refs/side-quest/quests"); strings.TrimSpace(ref) == "" {
		t.Error("onboard did not create the quest ref under the plugin")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/side-quest/ -run TestOnboardSkipsMcpJsonUnderPlugin`
Expected: FAIL — onboard currently writes `.mcp.json` regardless, so `os.Stat` finds it (the first assertion fires) and `out` contains `.mcp.json`.

- [ ] **Step 3: Write minimal implementation**

In `cmd/side-quest/onboard.go`, replace the step-3 block (currently lines 179-199) with the plugin-gated version. Keep the `cwd`/`top` computation **unconditional** — step 4 (AGENTS.md) still uses `top`:

```go
	// 3. project .mcp.json, at the repo root, only if absent — but never when the
	// plugin is active, since it already registers the side-quest MCP server; a
	// second identically-named one would be redundant. We skip silently: .mcp.json
	// is internal plumbing an end user need not hear about (SQ-0064, D6).
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	top, err := gitcmd.New(cwd).Run("rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	if !pluginActive() {
		mcpPath := filepath.Join(top, ".mcp.json")
		switch _, err := os.Stat(mcpPath); {
		case err == nil:
			fmt.Println("side-quest: .mcp.json already exists — leaving it as is.")
		case os.IsNotExist(err):
			if err := os.WriteFile(mcpPath, []byte(mcpJSON), 0o644); err != nil {
				return err
			}
			fmt.Println("side-quest: wrote .mcp.json (registers the side-quest MCP server).")
		default:
			return err
		}
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/side-quest/ -run TestOnboardSkipsMcpJsonUnderPlugin`
Expected: PASS.

- [ ] **Step 5: Pin the env in the two "writes .mcp.json" tests**

`onboard` is now env-sensitive, so the tests asserting `.mcp.json` **is** written must not flake if the ambient environment ever has `CLAUDE_PLUGIN_DATA` set. Add one line as the **first statement** of each of `TestOnboardSetsUpRepo` and `TestOnboardPreservesExistingMcpJson` in `cmd/side-quest/onboard_test.go`:

```go
	t.Setenv("CLAUDE_PLUGIN_DATA", "") // pin non-plugin: onboard must write .mcp.json
```

- [ ] **Step 6: Run the full onboard suite to verify it passes**

Run: `go test ./cmd/side-quest/ -run 'TestOnboard'`
Expected: PASS (all `TestOnboard*` green, including `TestOnboardSetsUpRepo`, `TestOnboardPreservesExistingMcpJson`, and the new skip test).

- [ ] **Step 7: Update the architecture doc's `onboard` bullet (plugin clause)**

In `docs/architecture.md`, replace the `onboard` bullet (lines 378-382). This edit updates the `.mcp.json` clause and corrects the AGENTS.md clause to the `--agents-md` opt-in it actually is; the Advanced framing sentence is added in Task 3.

Replace:

```markdown
- `onboard` — one-shot per-repo setup: `init` + `install-hooks`, write a project
  `.mcp.json` if absent, then refresh the marker-guarded guidance block in the
  project's `AGENTS.md` in place (create/append/refresh). Safe to re-run (an
  existing ref, hooks, and `.mcp.json` are each left as they are; the AGENTS.md
  block is refreshed to the current version, the user's own content untouched).
```

with:

```markdown
- `onboard` — one-shot per-repo setup: `init` + `install-hooks`, and — unless the
  Claude Code plugin is active (`CLAUDE_PLUGIN_DATA` set, or the binary runs from
  under `~/.claude/plugins/`) — write a project `.mcp.json` if absent; when the
  plugin is active it skips `.mcp.json` silently, since the plugin already
  registers the server. With `--agents-md` it also refreshes the marker-guarded
  guidance block in the project's `AGENTS.md` in place (create/append/refresh).
  Safe to re-run (an existing ref, hooks, and `.mcp.json` are each left as they
  are; the AGENTS.md block is refreshed to the current version, the user's own
  content untouched).
```

- [ ] **Step 8: Commit**

```bash
git add cmd/side-quest/onboard.go cmd/side-quest/onboard_test.go docs/architecture.md
git commit -m "feat: onboard skips .mcp.json when the plugin is active (SQ-0064)" \
  -m "The plugin already registers the side-quest MCP server, so onboard no longer writes a redundant project .mcp.json under it — silently, since the file is internal plumbing." \
  -m "Quest: SQ-0064" \
  -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws"
```

---

### Task 3: `onboard` leads the usage; demote `init`/`install-hooks` to Advanced (D9)

**Files:**
- Modify: `cmd/side-quest/main.go:18-47` (the `usage` const)
- Modify: `cmd/side-quest/main_test.go` (add a grouping test)
- Modify: `docs/architecture.md` (append the Advanced framing sentence to the `onboard` bullet)

**Interfaces:** none new. Pure text regroup; every existing subcommand still appears, so `run()`'s switch is untouched.

- [ ] **Step 1: Write the failing test**

Add to `cmd/side-quest/main_test.go` (already imports `strings`, `testing`):

```go
// D9: onboard leads the usage as the front-door command, and init/install-hooks
// are demoted under an "Advanced" grouping — still listed (they stay valid).
func TestUsageDemotesInitAndInstallHooks(t *testing.T) {
	bin := buildBinary(t)
	out, _ := runBin(t, bin, t.TempDir()) // no args -> usage on stderr, exit 2

	adv := strings.Index(out, "Advanced")
	if adv < 0 {
		t.Fatalf("usage has no Advanced section:\n%s", out)
	}
	if i := strings.Index(out, "onboard"); i < 0 || i > adv {
		t.Errorf("onboard should lead the usage above Advanced (onboard=%d advanced=%d)", i, adv)
	}
	for _, cmd := range []string{"init", "install-hooks"} {
		i := strings.Index(out, "\n  "+cmd)
		if i < 0 {
			t.Errorf("usage dropped %q entirely:\n%s", cmd, out)
		} else if i < adv {
			t.Errorf("%q should be under Advanced (line at %d, Advanced at %d)", cmd, i, adv)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/side-quest/ -run TestUsageDemotesInitAndInstallHooks`
Expected: FAIL — the current flat `usage` has no "Advanced" line, so `adv < 0` fires.

- [ ] **Step 3: Regroup the usage text**

In `cmd/side-quest/main.go`, replace the entire `usage` const (lines 18-47) with:

```go
const usage = `usage: side-quest <command> [args]

Setup
  onboard [--agents-md]           set up or refresh this repo (ref + hooks + .mcp.json)

Quests
  new [--type --priority --context --tag k=v --current --json] <title>
  list [--status --type --priority --json]   list quests (filters combine)
  show <id> [--json]              show one quest
  status <id> <status>            set a quest's status
  note <id> <text>                append a note to a quest
  edit <id>                       open a quest in $EDITOR and write it back
  reclassify <id> [--type --priority]  change a quest's type/priority
  current [<id> | --clear]        get / set / clear this worktree's active quest
  config get [--json]             show effective config
  config set <key> <value>        set require_quest | auto_trailer | id_strategy | tone
  sync [--dry-run] [--remote <name>]  reconcile quests with a remote (fetch+merge+push)

Advanced
  init                            create the quest ref (_config.yaml)
  install-hooks                   install git hooks + refs/side-quest/* refspec
  link <sha>                      apply a commit's Quest:/Completes: trailers
  relink <id> <old-sha> <new-sha> repoint a recorded commit after a rebase
  unlink <id> <sha>               remove a recorded commit from a quest
  commit-msg <file>               (hook) warn or reject when a trailer is missing
  prepare-commit-msg <file> [..]  (hook) inject the current-quest trailer
  pre-push [<remote> <url>]       (hook) auto-sync quests on git push
  agents-md                       print the agent-guidance block for AGENTS.md
  serve                           run the stdio MCP server
  version                         print the side-quest version

values:
  type      bug|feature (default feature)
  priority  high|low (default low)
  status    open|partial|done|deferred|discarded (new quests start open)`
```

- [ ] **Step 4: Run the test to verify it passes, and the existing usage test still passes**

Run: `go test ./cmd/side-quest/ -run 'TestUsageDemotesInitAndInstallHooks|TestUsageListsEnumValues'`
Expected: PASS both — the `values:` block is preserved verbatim, so `TestUsageListsEnumValues` stays green.

- [ ] **Step 5: Append the Advanced framing to the architecture doc**

In `docs/architecture.md`, the `onboard` bullet was rewritten in Task 2. Append this sentence to the end of that bullet (after "…the user's own content untouched)."):

```markdown
  `init` and `install-hooks` remain lower-level subcommands `onboard` composes
  (grouped under "Advanced" in `side-quest help`).
```

- [ ] **Step 6: Run the full package suite**

Run: `go test ./cmd/side-quest/`
Expected: PASS (whole package green).

- [ ] **Step 7: Commit**

```bash
git add cmd/side-quest/main.go cmd/side-quest/main_test.go docs/architecture.md
git commit -m "docs: onboard leads usage; init/install-hooks demoted to Advanced (SQ-0064)" \
  -m "Regroup the top-level help into Setup / Quests / Advanced so onboard reads as the single front door, with init/install-hooks kept as the plumbing it composes." \
  -m "Quest: SQ-0064" \
  -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws"
```

---

## Out of scope (this plan)

- `install-cli` / `uninstall-cli` and the PATH launcher — **SQ-0065** (separate plan).
- The MCP `cli_*` tools and the plugin guidance that drives the first-run CLI offer — **SQ-0066** (separate plan). Binary provisioning is the plugin's MCP server on startup (unchanged); there is no `SessionStart` hook.
- Changing `cmdOnboard`'s own `setUsage` synopsis string (line 150) — it still reads "write .mcp.json"; left as-is because it describes the non-plugin default and is not asserted by any test. Revisit only if it becomes misleading.
- `make dev` still calls `install-hooks` directly (Makefile:48); demotion is docs/help-only, so the dev loop is unaffected.

## Self-Review

- **Spec coverage:** D6 (onboard skips `.mcp.json` when plugin active, silently) → Tasks 1+2. D9 (onboard as single front door; `init`/`install-hooks` demoted) → Task 3. Detection signals (env OR `~/.claude/plugins/` path, no cache parsing) → Task 1. Both distribution paths' repo-activation UX collapse to `onboard` → satisfied by the non-plugin path being unchanged (still writes `.mcp.json`) and the plugin path skipping it.
- **Type consistency:** `pluginActive()`/`pluginActiveFrom(...)` names match between `plugin.go`, `plugin_test.go`, and the `onboard.go` call site. `mcpJSON`, `top`, `installAgentsGuidance` unchanged.
- **Placeholder scan:** none — every step carries exact code, exact paths, exact commands.
