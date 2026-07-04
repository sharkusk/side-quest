# MCP-Delivered Agent Guidance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the MCP server the portable, low-clutter carrier of side-quest's core agent guidance, with the skill and the AGENTS.md merge demoted to opt-in reinforcement.

**Architecture:** A new `internal/guidance` package embeds one canonical `core.md` brief exposed as `guidance.Core`. The MCP server sends it verbatim as its initialize-time `instructions`; two capture tools get enriched descriptions; `onboard` stops merging AGENTS.md unless `--agents-md` is passed; the reinforcement surfaces (`AGENTS.md`, `SKILL.md`) must contain `guidance.Core`, enforced by a drift-guard test; `/sq` and the docs are aligned.

**Tech Stack:** Go ≥ 1.25 (pure Go, `//go:embed`), `github.com/modelcontextprotocol/go-sdk/mcp`, system `git`.

## Global Constraints

- **TDD:** no production code without a failing test first (RED→GREEN→REFACTOR). Docs and static `.md`/config content are TDD-exempt but must land in the same change as the behavior they describe.
- **`guidance.Core` is the single source of truth.** MCP `instructions`, `AGENTS.md`, and `skills/side-quest/SKILL.md` must not drift from it — enforced by a test (`strings.Contains` verbatim).
- **The core brief text is fixed** (Task 1 `core.md`) — reproduce it byte-for-byte; the reinforcement surfaces embed the same bytes (no added backticks/rewrapping, or `Contains` fails).
- **Auto-classify rule** replaces "don't set type/priority unless the user stated them" on `/sq`, the `quest_new` description, and the core: set them only when the request makes them obvious (crash/regression = bug; explicit "urgent"/"critical"/"blocking" = high), else omit.
- **README invariants** (`TestReadmeReframedAndToneRemoved`): README must NOT contain `\n## Tone\n`, `Dungeon Crawler`, or `Credits & permissions`.
- **Branch-safety invariant:** side-quest only ever writes `refs/side-quest/*`, `.git/hooks`, and a scratch index — never the user's branches/index/worktree. (Untouched by this feature; do not regress it.)
- **Commit trailer format** (blank line before the co-author block):

  ```
  Quest: SQ-0051

  Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws
  ```

---

## File Structure

| File | Responsibility | Change |
|---|---|---|
| `internal/guidance/core.md` | The canonical core brief (bytes) | Create |
| `internal/guidance/guidance.go` | Embed `core.md` → `guidance.Core` | Create |
| `internal/guidance/guidance_test.go` | Core is compact + complete | Create |
| `internal/mcp/server.go` | Set `ServerOptions.Instructions = guidance.Core` | Modify (line 20) |
| `internal/mcp/tools.go` | Enrich `quest_new` + `quest_set_current` descriptions | Modify (lines 28, 40) |
| `internal/mcp/server_test.go` | Assert instructions + enriched descriptions | Modify |
| `cmd/side-quest/onboard.go` | `--agents-md` flag gates the AGENTS.md merge | Modify (`cmdOnboard`) |
| `cmd/side-quest/onboard_test.go` | Default-skip + flag-merge; update existing | Modify |
| `AGENTS.md` | Lead with `guidance.Core` verbatim | Modify |
| `skills/side-quest/SKILL.md` | Lead with `guidance.Core` verbatim | Modify |
| `commands/sq.md` | Align to auto-classify-when-obvious | Modify (line 15) |
| `internal/packaging/manifests_test.go` | Drift guard + `/sq` alignment | Modify |
| `README.md`, `docs/architecture.md`, `docs/manual-setup.md`, `docs/plugin.md` | Reframe + tag-taxonomy how-to | Modify |

---

## Task 1: `internal/guidance` — the canonical core

**Files:**
- Create: `internal/guidance/core.md`
- Create: `internal/guidance/guidance.go`
- Test: `internal/guidance/guidance_test.go`

**Interfaces:**
- Produces: `guidance.Core` (a `string`, whitespace-trimmed) — imported by `internal/mcp` (Task 2) and `internal/packaging` (Task 4).

- [ ] **Step 1: Write the failing test**

`internal/guidance/guidance_test.go`:

```go
package guidance

import (
	"strings"
	"testing"
)

// The core brief rides in always-on context, so it must stay compact — yet carry
// the tool names an agent needs to act on it.
func TestCoreIsCompactAndComplete(t *testing.T) {
	if Core == "" {
		t.Fatal("guidance.Core is empty")
	}
	if len(Core) > 1200 {
		t.Errorf("guidance.Core is %d bytes; keep it under 1200 (always-on context)", len(Core))
	}
	for _, want := range []string{"quest_new", "quest_set_current", "Completes:", "quest_list"} {
		if !strings.Contains(Core, want) {
			t.Errorf("guidance.Core missing %q", want)
		}
	}
	if strings.HasPrefix(Core, " ") || strings.HasSuffix(Core, "\n") {
		t.Error("guidance.Core must be whitespace-trimmed")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/guidance/`
Expected: FAIL to compile — `undefined: Core` (package has no non-test source yet).

- [ ] **Step 3: Create `core.md` (the canonical brief)**

`internal/guidance/core.md` (exact bytes):

```
side-quest is a git-native tracker; a "quest" is just an issue, task, or
follow-up you manage through these tools, not by editing files.

- Capture without derailing. An idea surfaces mid-task? File it with quest_new
  (one-line restatement + why it came up) and resume. Set type/priority only
  when the request makes it obvious — a crash or regression is a bug;
  "urgent"/"critical"/"blocking" is high — else keep defaults.
- Work one at a time. Make the quest you're on current (quest_set_current); the
  git hooks then link your commits to it — you never touch hashes.
- Close it by committing "Completes: SQ-1234" (or "Quest: SQ-1234" to link
  only), or quest_set_status.
- List work with quest_list; read one with quest_show.
```

- [ ] **Step 4: Create the embed**

`internal/guidance/guidance.go`:

```go
// Package guidance holds side-quest's canonical agent guidance. Core is the
// single source of truth for the compact behavioral brief: the MCP server sends
// it verbatim as its initialize-time instructions (internal/mcp), and the
// reinforcement surfaces (AGENTS.md, skills/side-quest/SKILL.md) must contain it,
// drift-guarded by a test in internal/packaging.
package guidance

import (
	_ "embed"
	"strings"
)

//go:embed core.md
var coreRaw string

// Core is the canonical core guidance brief, trimmed of surrounding whitespace.
var Core = strings.TrimSpace(coreRaw)
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/guidance/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/guidance/
git commit   # message ends with the Quest: SQ-0051 trailer block (Global Constraints)
```

---

## Task 2: MCP `instructions` + enriched capture-tool descriptions

**Files:**
- Modify: `internal/mcp/server.go` (line 20)
- Modify: `internal/mcp/tools.go` (lines 28, 40)
- Test: `internal/mcp/server_test.go`

**Interfaces:**
- Consumes: `guidance.Core` (Task 1).
- The test harness already provides `dialTest(t, s) (*sdk.ClientSession, context.Context)` and `newTestStore(t) *store.Store`. Read back the negotiated instructions with `cs.InitializeResult().Instructions`; list tools with `cs.ListTools(ctx, nil)` → `res.Tools[i].{Name,Description}`.

- [ ] **Step 1: Write the failing test (instructions)**

Append to `internal/mcp/server_test.go` (add `"github.com/sharkusk/side-quest/internal/guidance"` to its imports):

```go
// The server advertises the canonical core brief as its initialize-time
// instructions, so any MCP client can surface it — no repo file required.
func TestServerAdvertisesCoreInstructions(t *testing.T) {
	cs, _ := dialTest(t, newTestStore(t))
	if got := cs.InitializeResult().Instructions; got != guidance.Core {
		t.Errorf("server instructions = %q, want guidance.Core", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/mcp/ -run TestServerAdvertisesCoreInstructions -v`
Expected: FAIL — instructions is `""` (server built with `nil` options).

- [ ] **Step 3: Wire the instructions**

In `internal/mcp/server.go`, add the import and change the constructor:

```go
import (
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sharkusk/side-quest/internal/guidance"
	"github.com/sharkusk/side-quest/internal/store"
)
```

```go
func NewServer(s *store.Store, version string) *sdk.Server {
	srv := sdk.NewServer(
		&sdk.Implementation{Name: "side-quest", Version: version},
		&sdk.ServerOptions{Instructions: guidance.Core},
	)
	(&handlers{store: s}).register(srv)
	return srv
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/mcp/ -run TestServerAdvertisesCoreInstructions -v`
Expected: PASS.

- [ ] **Step 5: Write the failing test (enriched descriptions)**

Append to `internal/mcp/server_test.go`:

```go
// The two capture tools carry the reflex + auto-classify cues in their own
// descriptions, so the essentials survive even a client that ignores instructions.
func TestCaptureToolsCarryReflexCues(t *testing.T) {
	cs, ctx := dialTest(t, newTestStore(t))
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	desc := map[string]string{}
	for _, tl := range res.Tools {
		desc[tl.Name] = tl.Description
	}
	if !strings.Contains(desc["quest_new"], "without derailing") || !strings.Contains(desc["quest_new"], "obvious") {
		t.Errorf("quest_new description missing capture/auto-classify cue: %q", desc["quest_new"])
	}
	if !strings.Contains(desc["quest_set_current"], "link the commits") {
		t.Errorf("quest_set_current description missing auto-link cue: %q", desc["quest_set_current"])
	}
}
```

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./internal/mcp/ -run TestCaptureToolsCarryReflexCues -v`
Expected: FAIL — current descriptions lack "without derailing"/"obvious"/"link the commits".

- [ ] **Step 7: Enrich the descriptions**

In `internal/mcp/tools.go`, replace the `quest_new` description (line 28):

```go
	sdk.AddTool(s, &sdk.Tool{Name: "quest_new", Description: "Capture a new quest — an issue, task, or follow-up. Note a tangent that surfaced mid-task without derailing: restate the idea in a line. Mechanical git context (branch/head/cwd/current) is recorded automatically; pass a one-sentence narrative in context. Set type/priority only when the request makes them obvious (a crash or regression is a bug; explicit \"urgent\"/\"critical\"/\"blocking\" is high), else omit them.",
```

and the `quest_set_current` description (line 40):

```go
	sdk.AddTool(s, &sdk.Tool{Name: "quest_set_current", Description: "Set this worktree's current quest by id (the quest you're actively working on), or clear it with clear:true. While a quest is current, the git hooks link the commits you make to it automatically."}, h.questSetCurrent)
```

- [ ] **Step 8: Run to verify it passes (and no regressions)**

Run: `go test ./internal/mcp/`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/mcp/
git commit   # Quest: SQ-0051 trailer block
```

---

## Task 3: `onboard --agents-md` opt-in

**Files:**
- Modify: `cmd/side-quest/onboard.go` (`cmdOnboard`)
- Test: `cmd/side-quest/onboard_test.go`

**Interfaces:**
- Consumes existing package helpers: `newFlagSet(name)`, `setUsage(fs, synopsis)`, `parseInterspersed(fs, args) ([]string, error)`, `installAgentsGuidance(path, version)`, `agentsGuidanceNote(...)`, `&usageErr{...}`. Test helpers: `buildBinary(t)`, `newRepo(t) (dir, store)`, `runBin(t, bin, dir, args...) (string, int)`, and the package const `agentsMarker`.

- [ ] **Step 1: Write the failing tests**

Append to `cmd/side-quest/onboard_test.go` (imports `os`, `path/filepath`, `strings` are already present in that file):

```go
// Bare onboard no longer touches the project's AGENTS.md (guidance now rides the
// MCP server; the merge is opt-in) — SQ-0051.
func TestOnboardSkipsAgentsByDefault(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)
	if _, code := runBin(t, bin, dir, "onboard"); code != 0 {
		t.Fatalf("onboard exit nonzero")
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Errorf("bare onboard must not create AGENTS.md (stat err=%v)", err)
	}
}

// --agents-md opts back into the merge.
func TestOnboardAgentsMdFlagMerges(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)
	if _, code := runBin(t, bin, dir, "onboard", "--agents-md"); code != 0 {
		t.Fatalf("onboard --agents-md exit nonzero")
	}
	b, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("onboard --agents-md did not write AGENTS.md: %v", err)
	}
	if !strings.Contains(string(b), agentsMarker) {
		t.Error("onboard --agents-md did not write the marked guidance block")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./cmd/side-quest/ -run 'TestOnboardSkipsAgentsByDefault|TestOnboardAgentsMdFlagMerges' -v`
Expected: `TestOnboardSkipsAgentsByDefault` FAILS (bare onboard currently writes AGENTS.md); `TestOnboardAgentsMdFlagMerges` FAILS to compile/parse the unknown flag (exit 2 → non-zero).

- [ ] **Step 3: Add the flag and gate the merge**

In `cmd/side-quest/onboard.go`, replace the head of `cmdOnboard` (the `if len(args) != 0 { return &usageErr{...} }` guard) with flag parsing:

```go
func cmdOnboard(args []string) error {
	fs := newFlagSet("onboard")
	var withAgents bool
	fs.BoolVar(&withAgents, "agents-md", false, "also merge the side-quest guidance block into the project's AGENTS.md")
	setUsage(fs, "usage: side-quest onboard [--agents-md]\nper-repo setup: create the quest ref, install hooks, write .mcp.json (add --agents-md to also merge AGENTS.md guidance)")
	rest, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return &usageErr{"onboard takes no positional arguments"}
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	// ... steps 1–3 (quest ref, hooks, .mcp.json) unchanged ...
```

Then replace step 4 (the unconditional `installAgentsGuidance` block + trailing prints) with:

```go
	// 4. AGENTS.md guidance is opt-in reinforcement now — only with --agents-md (SQ-0051).
	if withAgents {
		agentsPath := filepath.Join(top, "AGENTS.md")
		outcome, prev, err := installAgentsGuidance(agentsPath, version)
		if err != nil {
			return err
		}
		fmt.Println(agentsGuidanceNote(outcome, prev, version))
	}
	fmt.Println("Then restart your agent session so the MCP server loads its guidance.")
	return nil
}
```

- [ ] **Step 4: Update the existing AGENTS-dependent onboard tests**

Two existing tests drive AGENTS.md through bare `onboard`; point them at the flag so they still exercise the merge:
- `TestOnboardRefreshesAgentsInPlace`: change both `runBin(..., "onboard")` calls to `runBin(..., "onboard", "--agents-md")`.
- `TestOnboardSetsUpRepo`: change its `runBin(t, bin, dir, "onboard")` to `runBin(t, bin, dir, "onboard", "--agents-md")` (it asserts AGENTS.md is written).

- [ ] **Step 5: Run to verify all pass**

Run: `go test ./cmd/side-quest/ -run Onboard -v`
Expected: PASS (new + updated tests green).

- [ ] **Step 6: Commit**

```bash
git add cmd/side-quest/onboard.go cmd/side-quest/onboard_test.go
git commit   # Quest: SQ-0051 trailer block
```

---

## Task 4: Align reinforcement surfaces + `/sq`, with a drift guard

**Files:**
- Modify: `AGENTS.md`, `skills/side-quest/SKILL.md`, `commands/sq.md`
- Test: `internal/packaging/manifests_test.go`

**Interfaces:**
- Consumes: `guidance.Core` (Task 1). `manifests_test.go` already has `repoFile(t, rel) []byte` for repo-root reads.

- [ ] **Step 1: Write the failing drift-guard test**

Append to `internal/packaging/manifests_test.go` (add `"github.com/sharkusk/side-quest/internal/guidance"` to its imports; `strings` is already imported):

```go
// The reinforcement surfaces must contain the canonical core verbatim (single
// source of truth), and /sq must reflect the auto-classify rule — SQ-0051.
func TestGuidanceSurfacesContainCore(t *testing.T) {
	for _, f := range []string{"AGENTS.md", "skills/side-quest/SKILL.md"} {
		if !strings.Contains(string(repoFile(t, f)), guidance.Core) {
			t.Errorf("%s must contain guidance.Core verbatim (single source of truth)", f)
		}
	}
	sq := string(repoFile(t, "commands/sq.md"))
	if strings.Contains(sq, "unless the user stated them") {
		t.Error("commands/sq.md still carries the old don't-set-type/priority rule; align to auto-classify-when-obvious")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/packaging/ -run TestGuidanceSurfacesContainCore -v`
Expected: FAIL — neither surface contains the core yet; `commands/sq.md` still has the old rule.

- [ ] **Step 3: Insert the core verbatim into `AGENTS.md`**

Immediately after the intro paragraph in `AGENTS.md` (after the "`skills/side-quest/SKILL.md`." sentence, before "## First-run setup"), insert a `## Core` section whose body is the **exact** bytes of `internal/guidance/core.md` (no backticks added, no rewrapping):

```markdown
## Core

side-quest is a git-native tracker; a "quest" is just an issue, task, or
follow-up you manage through these tools, not by editing files.

- Capture without derailing. An idea surfaces mid-task? File it with quest_new
  (one-line restatement + why it came up) and resume. Set type/priority only
  when the request makes it obvious — a crash or regression is a bug;
  "urgent"/"critical"/"blocking" is high — else keep defaults.
- Work one at a time. Make the quest you're on current (quest_set_current); the
  git hooks then link your commits to it — you never touch hashes.
- Close it by committing "Completes: SQ-1234" (or "Quest: SQ-1234" to link
  only), or quest_set_status.
- List work with quest_list; read one with quest_show.
```

- [ ] **Step 4: Insert the same core verbatim into `SKILL.md`**

In `skills/side-quest/SKILL.md`, after the intro paragraph (after "the reflex to capture a stray idea without derailing your current work." and before "## First-run setup"), insert the same `## Core` section with the identical body from Step 3.

- [ ] **Step 5: Align `/sq` to auto-classify**

In `commands/sq.md`, replace line 15:

```
   - Do not set it current. Do not set `type`/`priority` unless the user stated them.
```

with:

```
   - Do not set it current. Set `type`/`priority` only when the request makes them obvious (a crash or regression is a bug; explicit "urgent"/"critical"/"blocking" is high); otherwise leave them unset.
```

- [ ] **Step 6: Run to verify it passes**

Run: `go test ./internal/packaging/ -run TestGuidanceSurfacesContainCore -v`
Expected: PASS. If it fails on `Contains`, diff the inserted block against `internal/guidance/core.md` — the bytes must match exactly (whitespace/line breaks included).

- [ ] **Step 7: Commit**

```bash
git add AGENTS.md skills/side-quest/SKILL.md commands/sq.md internal/packaging/manifests_test.go
git commit   # Quest: SQ-0051 trailer block
```

---

## Task 5: Docs reframe + tag-taxonomy how-to (docs-only)

**Files:**
- Modify: `README.md`, `docs/architecture.md`, `docs/manual-setup.md`, `docs/plugin.md`

TDD-exempt (docs), but the packaging manifest tests read `README.md`/`docs/install.md`, so the suite must stay green.

- [ ] **Step 1: README — reframe the two-surface story**

In `README.md`'s "### Working with agents" area, replace the "ships as two mirrored files" framing with the baseline/reinforcement model: the `side-quest serve` MCP server carries the **core guidance** (portable to any MCP client); the skill (Claude) and `AGENTS.md` (non-Claude) are **optional reinforcement**. Update the Quickstart `onboard` description to note it no longer merges `AGENTS.md` unless `--agents-md` is passed. Do NOT introduce `## Tone`, `Dungeon Crawler`, or `Credits & permissions` (guarded by `TestReadmeReframedAndToneRemoved`).

- [ ] **Step 2: architecture.md — update the guidance section**

In `docs/architecture.md`, update the AGENTS.md-guidance paragraph (the SQ-0047 block description) to state that `onboard` merges AGENTS.md only with `--agents-md`, and add a short "How guidance reaches the agent" note: MCP `instructions` (= `guidance.Core`, from `internal/guidance`) + always-loaded tool descriptions form the baseline; skill and AGENTS.md are opt-in reinforcement; the core is single-sourced and drift-guarded.

- [ ] **Step 3: manual-setup.md — new default + tag-taxonomy how-to**

In `docs/manual-setup.md`: adjust the setup steps so the MCP server is the guidance baseline and AGENTS.md is an explicit opt-in (`side-quest onboard --agents-md`, or `side-quest agents-md` to paste). Add a new subsection **"Customizing guidance for your project"** documenting the tag-taxonomy pattern: side-quest ships tag mechanics (`tags` on `quest_new`/`quest_update`, `--tag`/`--filter`) but no taxonomy; declare yours in your own `AGENTS.md`/`CLAUDE.md`/skill (e.g. "tag each quest with `area=<subsystem>`; bugs also `platform=<os>`"); the agent applies it on capture; filter/report with `side-quest list --filter "area=parser and bug"`.

- [ ] **Step 4: plugin.md — adjust setup default**

In `docs/plugin.md`, reflect that installing the plugin bundles the skill (the Claude reinforcement) on top of the MCP baseline, and that AGENTS.md is not merged by the plugin path.

- [ ] **Step 5: Run the full suite**

Run: `go test ./...`
Expected: PASS (including `internal/packaging` README/install and the new drift guard).

- [ ] **Step 6: Commit**

```bash
git add README.md docs/architecture.md docs/manual-setup.md docs/plugin.md
git commit   # Quest: SQ-0051 trailer block
```

---

## Self-Review notes (author checklist, resolved)

- **Spec coverage:** core brief → T1; `instructions` + descriptions → T2; `onboard --agents-md` → T3; single-source + drift guard + `/sq` alignment → T4; docs reframe + tag-taxonomy how-to → T5. The empirical "does Claude Code surface `instructions`?" check from the spec is a manual verification, not code — perform it after T2 lands and record the finding in `docs/architecture.md` during T5.
- **Type consistency:** `guidance.Core` (exported `string`) is used identically in T2 (`internal/mcp`) and T4 (`internal/packaging`); both add the same import path. `newFlagSet`/`setUsage`/`parseInterspersed` signatures match `cmd/side-quest/cli.go`.
- **No placeholders:** every code step carries complete code; the `core.md` bytes are reproduced verbatim in T1/T4.
```
