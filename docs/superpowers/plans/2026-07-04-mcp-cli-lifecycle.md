# MCP CLI lifecycle — `cli_*` tools + plugin guidance — Implementation Plan (SQ-0066)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the agent enable the terminal `side-quest` CLI in-process via MCP tools, and — once per install — proactively offer to do so, driven entirely by MCP guidance (no `SessionStart` hook).

**Architecture:** Four MCP tools in `internal/mcp` (`cli_status`, `cli_install`, `cli_uninstall`, `cli_dismiss`) wrap the `internal/cli` core from SQ-0065. The server *is* the plugin-provisioned binary, with `CLAUDE_PLUGIN_DATA` set, so `cli_install` writes the launcher in-process — no `side-quest` need be on PATH, and it doubles as an on-demand re-enable. A one-time offer sentinel (`$CLAUDE_PLUGIN_DATA/.cli-offered`) is written by `cli_install`/`cli_dismiss`. Discovery is a **plugin-only Instructions addendum**: `guidance.Core` stays the tight cross-agent brief, but when `CLAUDE_PLUGIN_DATA` is set the server appends `guidance.Plugin` to its initialize-time instructions, telling the agent to check `cli_status` and offer once.

**Tech Stack:** Go (`internal/mcp`, `internal/guidance`, the `go-sdk/mcp` server); the `internal/cli` core (SQ-0065); the existing in-memory MCP test transport.

**Depends on:** SQ-0065 (`internal/cli` — `Install`/`Uninstall`/`Status`/`Marker`, which these tools wrap) and SQ-0064 (plugin-aware `onboard`, referenced by the guidance copy). Land both first.

## Design note — why no hook

An earlier draft used a `SessionStart` hook to provision the binary and inject the first-run offer. Two problems dissolved once enable-CLI became an MCP tool (spec O2):

- **Provisioning was never the hook's unique job.** The MCP server already provisions the binary on startup (`bin/side-quest serve` runs the download shim, cached per VERSION, refreshed on update). So the hook's provisioning step was redundant, and with it the `SIDE_QUEST_PROVISION_ONLY` shim guard the earlier draft needed.
- **The chicken-and-egg vanished.** The hook draft had to hand the agent an absolute binary path to run `install-cli` from a Bash tool (no `side-quest` on PATH). `cli_install` runs *inside* the server, so there is nothing to locate.

The result: no `hooks/` subsystem, no Windows hook-wiring deferral, full cross-platform parity (everything runs in the server). The accepted trade-off (spec D5): the proactive offer and the remembered decline now depend on the agent following the Instructions addendum rather than a guaranteed injected nudge.

## Global Constraints

- **TDD, no exceptions for code:** RED → verify fail → GREEN → verify pass → commit.
- **Branch-safety invariant (HARD RULE):** unchanged. The tools write only a user-owned PATH dir (via `internal/cli`, SQ-0065) and the sentinel inside `$CLAUDE_PLUGIN_DATA` (Claude's own data dir) — never a git ref/index/worktree.
- **`guidance.Core` is not touched.** It stays the cross-agent brief that AGENTS.md and SKILL.md mirror verbatim (drift-guarded by `TestGuidanceSurfacesContainCore`). The plugin lifecycle guidance is a *separate* `guidance.Plugin` block, appended to the server's instructions only when `CLAUDE_PLUGIN_DATA` is set.
- **Sentinel lives in `$CLAUDE_PLUGIN_DATA/.cli-offered`** on purpose: Claude deletes that dir on uninstall, so the offer self-cleans and a reinstall re-offers (spec D5). When `CLAUDE_PLUGIN_DATA` is unset (not under the plugin), there is no sentinel and `offered` is reported false.
- **The `cli_*` tools take no arguments** (reuse the existing `emptyIn`); their JSON results are the neutral content block (no voice).
- **Commit trailer format** (every commit; blank line before the co-author block):

  ```
  <subject> (SQ-0066)

  <optional one-line body>

  Quest: SQ-0066

  Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws
  ```

- **Do not `git push`** (the user pushes explicitly).
- **Test command:** `go test ./internal/mcp/ ./internal/guidance/ ./internal/packaging/`.

---

## File Structure

- **Create `internal/mcp/cli.go`** — the four `cli_*` handlers and the sentinel helpers (`offerSentinel`, `offerMade`, `markOffered`).
- **Modify `internal/mcp/tools.go`** — register the four tools in `register()`.
- **Create `internal/mcp/cli_test.go`** — tool lifecycle tests (status → install → status → uninstall; dismiss marks offered).
- **Modify `internal/mcp/server.go`** — build instructions via a new `instructions()` helper that appends `guidance.Plugin` under the plugin.
- **Modify `internal/mcp/server_test.go`** — bump the tool-count test 12 → 16; add the `instructions()` tests; pin `TestServerAdvertisesCoreInstructions` to the no-plugin env.
- **Create `internal/guidance/plugin.md`** — the plugin lifecycle guidance block.
- **Modify `internal/guidance/guidance.go`** — embed `plugin.md` as `guidance.Plugin`.
- **Modify `skills/side-quest/SKILL.md`** — add a human-readable "Plugin lifecycle" section.
- **Modify `internal/packaging/manifests_test.go`** — drift-guard that SKILL.md documents the lifecycle.

---

### Task 1: The four `cli_*` MCP tools

**Files:**
- Create: `internal/mcp/cli.go`
- Modify: `internal/mcp/tools.go` (register the tools)
- Create: `internal/mcp/cli_test.go`
- Modify: `internal/mcp/server_test.go` (tool-count 12 → 16)

**Interfaces:**
- Consumes: `internal/cli` — `Install() (cli.InstallResult, error)`, `Uninstall() (cli.UninstallResult, error)`, `Status() cli.StatusResult` (SQ-0065); the existing `emptyIn`, `jsonResult`, `handlers` (internal/mcp).
- Produces: tools `cli_status`, `cli_install`, `cli_uninstall`, `cli_dismiss`; the sentinel helpers `offerSentinel()/offerMade()/markOffered()`.

- [ ] **Step 1: Write the failing test**

Create `internal/mcp/cli_test.go`:

```go
package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// callJSON calls a no-arg tool and unmarshals its neutral JSON content block.
func callJSON(t *testing.T, cs *sdk.ClientSession, ctx context.Context, name string) map[string]any {
	t.Helper()
	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("%s error: %s", name, contentText(t, res))
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(contentText(t, res)), &m); err != nil {
		t.Fatalf("%s: bad JSON: %v\n%s", name, err, contentText(t, res))
	}
	return m
}

// The lifecycle: fresh -> cli_install writes a launcher + marks offered -> status
// reflects both -> cli_uninstall removes it.
func TestCliToolsLifecycle(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("PATH", dir)
	t.Setenv("CLAUDE_PLUGIN_DATA", data)

	cs, ctx := dialTest(t, newTestStore(t))

	if st := callJSON(t, cs, ctx, "cli_status"); st["installed"] != false || st["offered"] != false {
		t.Fatalf("fresh status = %v, want installed/offered false", st)
	}

	in := callJSON(t, cs, ctx, "cli_install")
	if in["path"] == "" || in["path"] == nil {
		t.Fatalf("cli_install returned no path: %v", in)
	}
	if _, err := os.Stat(filepath.Join(dir, "side-quest")); err != nil {
		t.Fatalf("cli_install did not write the launcher: %v", err)
	}
	if _, err := os.Stat(filepath.Join(data, ".cli-offered")); err != nil {
		t.Fatalf("cli_install did not mark offered: %v", err)
	}

	if st := callJSON(t, cs, ctx, "cli_status"); st["installed"] != true || st["offered"] != true {
		t.Fatalf("post-install status = %v, want both true", st)
	}

	callJSON(t, cs, ctx, "cli_uninstall")
	if _, err := os.Stat(filepath.Join(dir, "side-quest")); !os.IsNotExist(err) {
		t.Fatalf("cli_uninstall did not remove the launcher (err=%v)", err)
	}
}

// cli_dismiss records a decline (writes the sentinel) so status reports offered.
func TestCliDismissMarksOffered(t *testing.T) {
	data := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("CLAUDE_PLUGIN_DATA", data)

	cs, ctx := dialTest(t, newTestStore(t))
	callJSON(t, cs, ctx, "cli_dismiss")
	if _, err := os.Stat(filepath.Join(data, ".cli-offered")); err != nil {
		t.Fatalf("cli_dismiss did not write the sentinel: %v", err)
	}
	if st := callJSON(t, cs, ctx, "cli_status"); st["offered"] != true {
		t.Fatalf("after dismiss, offered = %v, want true", st["offered"])
	}
}
```

Then, in `internal/mcp/server_test.go`, update the tool-count test — rename it and change the expected count from 12 to 16:

```go
func TestListToolsExposesSixteen(t *testing.T) {
	cs, ctx := dialTest(t, newTestStore(t))
	lt, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(lt.Tools) != 16 {
		names := make([]string, len(lt.Tools))
		for i, tl := range lt.Tools {
			names[i] = tl.Name
		}
		t.Fatalf("want 16 tools, got %d: %v", len(lt.Tools), names)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/ -run 'TestCliTools|TestCliDismiss|TestListToolsExposesSixteen'`
Expected: FAIL — the `cli_*` tools are not registered (tool errors / count is 12).

- [ ] **Step 3: Write minimal implementation**

Create `internal/mcp/cli.go`:

```go
package mcp

import (
	"context"
	"os"
	"path/filepath"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sharkusk/side-quest/internal/cli"
)

// offerSentinel is the path of the "CLI offer already made" marker, or "" when not
// running under the plugin (no CLAUDE_PLUGIN_DATA). It lives in the data dir so
// Claude deletes it on uninstall — a reinstall then re-offers (spec D5).
func offerSentinel() string {
	d := os.Getenv("CLAUDE_PLUGIN_DATA")
	if d == "" {
		return ""
	}
	return filepath.Join(d, ".cli-offered")
}

func offerMade() bool {
	p := offerSentinel()
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

func markOffered() {
	if p := offerSentinel(); p != "" {
		_ = os.WriteFile(p, nil, 0o644) // best-effort; the offer never blocks
	}
}

// cliStatus reports whether the terminal launcher is present and whether the
// one-time enable offer has already been made.
func (h *handlers) cliStatus(ctx context.Context, req *sdk.CallToolRequest, in emptyIn) (*sdk.CallToolResult, any, error) {
	st := cli.Status()
	return jsonResult(struct {
		Installed bool   `json:"installed"`
		Path      string `json:"path,omitempty"`
		Offered   bool   `json:"offered"`
	}{st.Installed, st.Path, offerMade()})
}

// cliInstall writes the launcher in-process and records that the offer was made.
func (h *handlers) cliInstall(ctx context.Context, req *sdk.CallToolRequest, in emptyIn) (*sdk.CallToolResult, any, error) {
	r, err := cli.Install()
	if err != nil {
		return nil, nil, err
	}
	markOffered()
	return jsonResult(struct {
		Path   string `json:"path"`
		Dir    string `json:"dir"`
		OnPath bool   `json:"on_path"`
	}{r.Path, r.Dir, r.OnPath})
}

// cliUninstall removes the marked launcher(s) this tool installed.
func (h *handlers) cliUninstall(ctx context.Context, req *sdk.CallToolRequest, in emptyIn) (*sdk.CallToolResult, any, error) {
	r, err := cli.Uninstall()
	if err != nil {
		return nil, nil, err
	}
	return jsonResult(struct {
		Removed []string `json:"removed"`
		Refused []string `json:"refused"`
	}{r.Removed, r.Refused})
}

// cliDismiss records that the user declined the offer, so it is not repeated.
func (h *handlers) cliDismiss(ctx context.Context, req *sdk.CallToolRequest, in emptyIn) (*sdk.CallToolResult, any, error) {
	markOffered()
	return jsonResult(struct {
		Offered bool `json:"offered"`
	}{true})
}
```

In `internal/mcp/tools.go`, add the imports `"github.com/sharkusk/side-quest/internal/cli"` is **not** needed here (the handlers live in `cli.go`); only add the four `AddTool` calls at the end of `register()`:

```go
	sdk.AddTool(s, &sdk.Tool{Name: "cli_status", Description: "Report whether the terminal side-quest CLI is enabled (a launcher on the user's PATH) and whether the one-time enable offer has been made. Call early in a plugin session to decide whether to offer."}, h.cliStatus)
	sdk.AddTool(s, &sdk.Tool{Name: "cli_install", Description: "Enable the terminal side-quest CLI: write a read-only launcher onto the user's PATH so they can run side-quest from their own terminal (and have their own git commits link). Runs in-process; safe to re-run to re-enable if the launcher was removed. Offer before calling."}, h.cliInstall)
	sdk.AddTool(s, &sdk.Tool{Name: "cli_uninstall", Description: "Disable the terminal side-quest CLI by removing the launcher this tool installed (never touches a side-quest it did not install)."}, h.cliUninstall)
	sdk.AddTool(s, &sdk.Tool{Name: "cli_dismiss", Description: "Record that the user declined the terminal-CLI offer, so it is not offered again for this install."}, h.cliDismiss)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/mcp/ -run 'TestCliTools|TestCliDismiss|TestListToolsExposesSixteen'`
Expected: PASS (lifecycle works end-to-end over the in-memory transport; count is 16).

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/cli.go internal/mcp/tools.go internal/mcp/cli_test.go internal/mcp/server_test.go
git commit -m "feat: MCP cli_* tools enable/disable/query the terminal CLI in-process (SQ-0066)" \
  -m "cli_status/cli_install/cli_uninstall/cli_dismiss wrap internal/cli; cli_install writes the launcher in-process (no side-quest needed on PATH) and marks the offer sentinel; a reinstall re-offers." \
  -m "Quest: SQ-0066" \
  -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws"
```

---

### Task 2: Plugin-only Instructions addendum

**Files:**
- Create: `internal/guidance/plugin.md`
- Modify: `internal/guidance/guidance.go` (embed it as `guidance.Plugin`)
- Modify: `internal/mcp/server.go` (build instructions via `instructions()`)
- Modify: `internal/mcp/server_test.go` (add `instructions()` tests; pin the existing Core-instructions test)

**Interfaces:**
- Consumes: `guidance.Core` (existing).
- Produces: `var guidance.Plugin string`; `func instructions() string` (internal/mcp) — returns `guidance.Core`, plus `guidance.Plugin` when `CLAUDE_PLUGIN_DATA` is set.

- [ ] **Step 1: Write the failing test**

In `internal/mcp/server_test.go`, add:

```go
func TestInstructionsAppendsPluginBlockUnderPlugin(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_DATA", t.TempDir())
	got := instructions()
	if !strings.Contains(got, guidance.Core) {
		t.Error("instructions under the plugin should still contain guidance.Core")
	}
	if !strings.Contains(got, guidance.Plugin) {
		t.Error("instructions under the plugin should append guidance.Plugin")
	}
}

func TestInstructionsCoreOnlyOutsidePlugin(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_DATA", "")
	if got := instructions(); got != guidance.Core {
		t.Errorf("instructions outside the plugin = %q, want exactly guidance.Core", got)
	}
}
```

And pin the existing `TestServerAdvertisesCoreInstructions` to the no-plugin env by adding this as its first line (so a dev/CI run that happens to be under the plugin doesn't flip it):

```go
	t.Setenv("CLAUDE_PLUGIN_DATA", "")
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/ -run 'TestInstructions'`
Expected: FAIL — `undefined: instructions` and `undefined: guidance.Plugin`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/guidance/plugin.md`:

```markdown
This session runs under the side-quest Claude Code plugin, so the terminal
`side-quest` command may not be on the user's PATH yet.

Enable it once: early in the session, call `cli_status`. If it reports
`installed: false` and `offered: false`, offer to put `side-quest` on the user's
PATH so they can run it — and have their own git commits link — from the terminal.
On yes, call `cli_install` and report the path it returns. On no, call
`cli_dismiss` so you don't ask again. (A reinstall re-offers.)

Re-enable or remove anytime: if the launcher was deleted, `cli_install` restores
it; `cli_uninstall` removes it.
```

In `internal/guidance/guidance.go`, add the embed and exported var (next to the existing `Core`):

```go
//go:embed plugin.md
var pluginRaw string

// Plugin is the Claude-Code-plugin-only lifecycle guidance. The MCP server appends
// it to its initialize-time instructions when CLAUDE_PLUGIN_DATA is set (internal/mcp),
// so the cross-agent Core brief — mirrored verbatim in AGENTS.md/SKILL.md — is unaffected.
var Plugin = strings.TrimSpace(pluginRaw)
```

In `internal/mcp/server.go`, add `"os"` to the imports, introduce the helper, and use it:

```go
// instructions is the server's initialize-time guidance: the cross-agent Core
// brief, plus the plugin lifecycle block when running under the Claude Code plugin
// (CLAUDE_PLUGIN_DATA is set) — where the cli_* tools are relevant (SQ-0066).
func instructions() string {
	if os.Getenv("CLAUDE_PLUGIN_DATA") != "" {
		return guidance.Core + "\n\n" + guidance.Plugin
	}
	return guidance.Core
}
```

and change `NewServer`'s options to use it:

```go
	srv := sdk.NewServer(
		&sdk.Implementation{Name: "side-quest", Version: version},
		&sdk.ServerOptions{Instructions: instructions()},
	)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/mcp/ ./internal/guidance/ -run 'TestInstructions|TestServerAdvertisesCore|TestCore'`
Expected: PASS — the addendum appears only under the plugin; Core-only holds outside it; the existing guidance tests stay green.

- [ ] **Step 5: Commit**

```bash
git add internal/guidance/plugin.md internal/guidance/guidance.go internal/mcp/server.go internal/mcp/server_test.go
git commit -m "feat: append plugin-only CLI lifecycle guidance to server instructions (SQ-0066)" \
  -m "guidance.Plugin drives the first-run cli_install offer; appended to the server's instructions only when CLAUDE_PLUGIN_DATA is set, leaving guidance.Core (and AGENTS.md/SKILL.md) untouched." \
  -m "Quest: SQ-0066" \
  -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws"
```

---

### Task 3: Skill lifecycle documentation

**Files:**
- Modify: `skills/side-quest/SKILL.md`
- Test: `internal/packaging/manifests_test.go`

**Interfaces:** none — human-readable documentation. Must not disturb the existing `guidance.Core` block (kept verbatim by `TestGuidanceSurfacesContainCore`) or the `side-quest install-hooks` mention (`TestFirstRunGuidancePresent`).

- [ ] **Step 1: Write the failing test**

Add to `internal/packaging/manifests_test.go`:

```go
// The skill documents the plugin CLI lifecycle so a human reader (and an agent
// reading the skill) sees how to enable/query the terminal CLI and set up a repo.
func TestSkillDocumentsPluginLifecycle(t *testing.T) {
	s := string(repoFile(t, "skills/side-quest/SKILL.md"))
	for _, want := range []string{"cli_status", "cli_install", "onboard"} {
		if !strings.Contains(s, want) {
			t.Errorf("SKILL.md should mention %q for the plugin lifecycle", want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/packaging/ -run TestSkillDocumentsPluginLifecycle`
Expected: FAIL — SKILL.md does not yet mention `cli_status`/`cli_install` (confirm by reading the file first).

- [ ] **Step 3: Append the lifecycle section to `skills/side-quest/SKILL.md`**

Read the current file, then append at the end (match the file's existing `##` heading style):

```markdown
## Plugin lifecycle (Claude Code)

When side-quest runs as the Claude Code plugin, the MCP server's instructions guide
you through its lifecycle — this section is the human-readable version.

- **Enable the terminal CLI (once).** Early in a session, call `cli_status`. If the
  terminal CLI isn't enabled and hasn't been offered, offer to put `side-quest` on
  the user's PATH; on yes call `cli_install` (report the path it returns), on no
  call `cli_dismiss`. Re-run `cli_install` anytime to re-enable if the launcher was
  removed; `cli_uninstall` removes it.
- **Set up or refresh a repo.** To track a repo (or refresh it after a plugin
  update), run `side-quest onboard` — it creates the quest ref, installs the git
  hooks, and (outside the plugin) writes `.mcp.json`. Safe to re-run.
```

- [ ] **Step 4: Run the tests (including the drift guards)**

Run: `go test ./internal/packaging/`
Expected: PASS — the new test plus `TestGuidanceSurfacesContainCore` and `TestFirstRunGuidancePresent` stay green (we only appended content; `install-hooks` and the Core block are untouched).

- [ ] **Step 5: Commit**

```bash
git add skills/side-quest/SKILL.md internal/packaging/manifests_test.go
git commit -m "docs: skill documents the plugin CLI/onboard lifecycle (SQ-0066)" \
  -m "Quest: SQ-0066" \
  -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws"
```

---

## Out of scope (this plan)

- The `internal/cli` launcher mechanism itself (`Install`/`Uninstall`/`Status`, the launcher scripts, the `install-cli`/`uninstall-cli` subcommands) — **SQ-0065**. This plan only wraps it.
- Provisioning the binary into the data dir — the plugin's MCP server does this on startup via the existing `bin/side-quest` download shim; unchanged.
- Any `SessionStart`/`hooks/` subsystem — deliberately not built (see the Design note).
- `/sq` on the manual path (spec O5) — unchanged; the MCP guidance already carries the capture reflex.

## Self-Review

- **Spec coverage:** D5 (consent via MCP tools + plugin guidance; sentinel; reinstall re-offers) → Tasks 1-2. The four tools and their JSON shapes → Task 1. The plugin-only Instructions addendum, `guidance.Core` left intact → Task 2. Human-readable lifecycle guidance → Task 3. Provisioning-is-the-server / no-hook resolutions are recorded in the Design note.
- **Type consistency:** the tool names (`cli_status`/`cli_install`/`cli_uninstall`/`cli_dismiss`), the sentinel path `$CLAUDE_PLUGIN_DATA/.cli-offered`, the helper names (`offerSentinel`/`offerMade`/`markOffered`/`instructions`), and the consumed `internal/cli` surface (`Install`/`Uninstall`/`Status`/`InstallResult`/`UninstallResult`/`StatusResult`) match SQ-0065's Interfaces blocks and are referenced consistently across `cli.go`, `tools.go`, `server.go`, and the tests.
- **Placeholder scan:** none — every step carries exact code, paths, and commands. Task 3's exact insertion point is the one item that depends on reading SKILL.md's current tail (called out in the step); its content is given in full.
