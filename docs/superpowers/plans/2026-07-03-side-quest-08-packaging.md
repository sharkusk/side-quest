# Phase 7 Packaging & Distribution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make side-quest an installable Claude Code plugin and a publish-ready standalone binary (with an automated cross-platform release pipeline and a self-provisioning plugin launcher), without flipping the repo public.

**Architecture:** Mostly declarative artifacts (JSON manifests, Markdown, a GoReleaser config, a GitHub Actions workflow, two shell launchers) plus one small Go change (version plumbing). The plugin's `.mcp.json` launches the binary by bare name `side-quest`; inside an installed plugin, a `bin/` launcher resolves-or-downloads the right binary; in this dev repo, that bare name resolves to the `go install`ed binary on `PATH`.

**Tech Stack:** Go 1.25 (single change to `cmd/side-quest/main.go`), GoReleaser v2, GitHub Actions, POSIX `sh` + Windows batch, JSON, Markdown.

## Global Constraints

- Module path is `github.com/sharkusk/side-quest`; the main package is at `./cmd/side-quest` (NOT the module root). The correct install command is `go install github.com/sharkusk/side-quest/cmd/side-quest@latest`.
- Go toolchain floor is **1.25** (`go.mod` says `go 1.25.0`). Every doc says Go ≥1.25, never 1.22.
- Plugin/binary version baseline is **0.1.0**. The root `VERSION` file is the single source of truth; `plugin.json`'s `version` must equal it (test-enforced); the release tag is `v` + `VERSION`.
- LICENSE is **MIT, `Copyright (c) 2026 Marcus Kellerman`**. The MIT grant covers code only; the README "Credits & permissions" note still governs the dcc voice.
- `.mcp.json` `command` is the bare string `side-quest` with args `["serve"]` (no `go run`, no absolute path).
- The README must **not** advertise the dcc tone (kept a first-run surprise); the tone details live only in `docs/architecture.md`. The "Credits & permissions" note stays (it is attribution, not a spoiler).
- Importer (Phase 6) and a `sync` command (§15) do not exist — docs mention them only as "planned" stubs.
- No Go code changes beyond version plumbing in `main.go`.
- The launcher downloads nothing that it does not checksum-verify; on any failure it prints the `go install …/cmd/side-quest@latest` hint and exits non-zero (never silently wrong).
- `docs/superpowers/specs|plans/` are frozen history; `docs/architecture.md` + `README.md` are living docs, updated in the same change as behavior.
- Commit messages end with the two required footer lines:
  `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws`.

---

## File Structure

- `cmd/side-quest/main.go` (modify) — add `var version` + `version`/`--version`/`-v` handling.
- `cmd/side-quest/version_test.go` (create) — version output + ldflags injection.
- `.claude-plugin/plugin.json` (create) — plugin manifest.
- `.claude-plugin/marketplace.json` (create) — single-plugin marketplace.
- `VERSION` (create) — `0.1.0`.
- `LICENSE` (create) — MIT © 2026 Marcus Kellerman.
- `.mcp.json` (modify) — `go run …` → bare `side-quest serve`.
- `internal/packaging/manifests_test.go` (create) — manifest validity + version consistency + LICENSE.
- `commands/sq.md` (create) — `/sq` capture command.
- `AGENTS.md` (create) — agent-agnostic contract.
- `bin/side-quest` (create) — POSIX launcher.
- `bin/side-quest.cmd` (create) — Windows launcher.
- `internal/packaging/launcher_test.go` (create) — launcher resolution branches (download stubbed).
- `.goreleaser.yaml` (create) — 6-target build, archives, checksums.
- `.github/workflows/release.yml` (create) — tag-triggered release.
- `README.md` (rewrite) — reframe, install, dev, stubs; tone section removed.
- `docs/architecture.md` (modify) — relocated tone-config note + packaging subsection.

Tasks are ordered so each builds on committed predecessors. Task 1 (version) and Task 2 (VERSION file) are consumed by Task 4 (launcher) and Task 5 (release).

---

### Task 1: Version plumbing

**Files:**
- Modify: `cmd/side-quest/main.go`
- Test: `cmd/side-quest/version_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: package-level `var version = "dev"` in `package main`, overwritten at release build time via `-ldflags "-X main.version=<tag>"`. A `version` subcommand and `--version`/`-v` flags print the bare version string on its own line.

- [ ] **Step 1: Write the failing test**

Create `cmd/side-quest/version_test.go`:

```go
package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A plain build reports the default version "dev" for all three spellings.
func TestVersionReportsDevByDefault(t *testing.T) {
	bin := buildBinary(t)
	for _, arg := range []string{"version", "--version", "-v"} {
		out, code := runBin(t, bin, t.TempDir(), arg)
		if code != 0 {
			t.Fatalf("%q exit=%d out=%s", arg, code, out)
		}
		if strings.TrimSpace(out) != "dev" {
			t.Errorf("%q = %q, want dev", arg, strings.TrimSpace(out))
		}
	}
}

// A release build injects the version via ldflags; the binary must report it.
func TestVersionReflectsLdflags(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "side-quest")
	out, err := exec.Command("go", "build",
		"-ldflags", "-X main.version=9.9.9", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	got, code := runBin(t, bin, t.TempDir(), "version")
	if code != 0 || strings.TrimSpace(got) != "9.9.9" {
		t.Fatalf("version = %q (exit %d), want 9.9.9", got, code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/side-quest/ -run TestVersion -v`
Expected: FAIL — `version`/`--version`/`-v` are treated as unknown commands (non-zero exit, usage text), so the assertions fail.

- [ ] **Step 3: Implement the minimal code**

In `cmd/side-quest/main.go`, add the package-level var just after the `usage` const block (before `func main`):

```go
// version is overwritten at release build time via -ldflags "-X main.version=<tag>".
// A plain `go build` / `go install` leaves it as "dev".
var version = "dev"
```

Add a `version` line to the `usage` string (place it right after the `serve` line, keeping the existing back-tick block; add before the closing back-tick):

```
  version                         print the side-quest version`
```

In `func main`, insert the early switch immediately after the `len(os.Args) < 2` guard and before the `run(...)` call:

```go
	switch os.Args[1] {
	case "version", "--version", "-v":
		fmt.Println(version)
		return
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/side-quest/ -run TestVersion -v`
Expected: PASS (both tests).

- [ ] **Step 5: Full build/vet and commit**

Run: `go build ./... && go vet ./... && go test ./cmd/side-quest/`
Expected: all pass.

```bash
git add cmd/side-quest/main.go cmd/side-quest/version_test.go
git commit -m "$(cat <<'EOF'
feat: add version subcommand and --version flag

Adds a package-level `version` var (default "dev") that GoReleaser
overwrites via -ldflags at release build time, plus `version`,
`--version`, and `-v` that print it. Prepares Phase 7 release stamping.

Quest: none

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws
EOF
)"
```

---

### Task 2: Plugin manifests, VERSION, LICENSE, and .mcp.json

**Files:**
- Create: `.claude-plugin/plugin.json`, `.claude-plugin/marketplace.json`, `VERSION`, `LICENSE`
- Modify: `.mcp.json`
- Test: `internal/packaging/manifests_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `VERSION` file (`0.1.0`) read by the launcher (Task 4) and release tag; `.claude-plugin/plugin.json` with `version` equal to `VERSION`; `.mcp.json` launching bare `side-quest serve`.

- [ ] **Step 1: Write the failing test**

Create `internal/packaging/manifests_test.go`:

```go
// Package packaging holds tests that validate the repo's distribution artifacts
// (plugin manifests, VERSION, LICENSE, launcher). It has no non-test code; the
// tests read repo-root files via paths relative to this directory.
package packaging

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// repoFile reads a file relative to the repo root. `go test` runs with CWD set
// to this package's directory (internal/packaging), so the root is two levels up.
func repoFile(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile("../../" + rel)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return b
}

func TestPluginJSONValidAndRequiredKeys(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal(repoFile(t, ".claude-plugin/plugin.json"), &m); err != nil {
		t.Fatalf("plugin.json invalid JSON: %v", err)
	}
	for _, k := range []string{"name", "version", "description", "author", "repository"} {
		if _, ok := m[k]; !ok {
			t.Errorf("plugin.json missing required key %q", k)
		}
	}
	if m["name"] != "side-quest" {
		t.Errorf("plugin.json name = %v, want side-quest", m["name"])
	}
}

func TestMarketplaceJSONValid(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal(repoFile(t, ".claude-plugin/marketplace.json"), &m); err != nil {
		t.Fatalf("marketplace.json invalid JSON: %v", err)
	}
	if _, ok := m["plugins"]; !ok {
		t.Error("marketplace.json missing plugins array")
	}
}

func TestMCPJSONUsesBareBinary(t *testing.T) {
	var m struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(repoFile(t, ".mcp.json"), &m); err != nil {
		t.Fatalf(".mcp.json invalid: %v", err)
	}
	sq, ok := m.MCPServers["side-quest"]
	if !ok {
		t.Fatal(".mcp.json missing side-quest server")
	}
	if sq.Command != "side-quest" || len(sq.Args) != 1 || sq.Args[0] != "serve" {
		t.Errorf(".mcp.json launches %q %v, want side-quest [serve]", sq.Command, sq.Args)
	}
}

func TestPluginVersionMatchesVERSION(t *testing.T) {
	ver := strings.TrimSpace(string(repoFile(t, "VERSION")))
	var m struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(repoFile(t, ".claude-plugin/plugin.json"), &m); err != nil {
		t.Fatal(err)
	}
	if m.Version != ver {
		t.Errorf("plugin.json version %q != VERSION %q", m.Version, ver)
	}
}

func TestLicenseIsMIT(t *testing.T) {
	l := string(repoFile(t, "LICENSE"))
	if !strings.Contains(l, "Permission is hereby granted") {
		t.Error("LICENSE does not contain the MIT grant text")
	}
	if !strings.Contains(l, "Marcus Kellerman") {
		t.Error("LICENSE missing copyright holder Marcus Kellerman")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/packaging/ -v`
Expected: FAIL — the files do not exist yet (`read …: no such file`).

- [ ] **Step 3: Create the artifacts**

Create `VERSION` (exactly this, with a trailing newline):

```
0.1.0
```

Create `.claude-plugin/plugin.json`:

```json
{
  "name": "side-quest",
  "displayName": "side-quest",
  "version": "0.1.0",
  "description": "A streamlined, git-native issue tracker for individuals and small teams — capture side quests without derailing, and link every commit to the quest it addresses.",
  "author": { "name": "Marcus Kellerman" },
  "repository": "https://github.com/sharkusk/side-quest",
  "homepage": "https://github.com/sharkusk/side-quest"
}
```

Create `.claude-plugin/marketplace.json`:

```json
{
  "name": "side-quest",
  "owner": { "name": "Marcus Kellerman" },
  "plugins": [
    {
      "name": "side-quest",
      "source": ".",
      "description": "A streamlined, git-native issue tracker for individuals and small teams."
    }
  ]
}
```

Create `LICENSE` (standard MIT):

```
MIT License

Copyright (c) 2026 Marcus Kellerman

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

Replace `.mcp.json` entirely with:

```json
{
  "mcpServers": {
    "side-quest": {
      "command": "side-quest",
      "args": ["serve"]
    }
  }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/packaging/ -v`
Expected: PASS (all five tests).

- [ ] **Step 5: Verify the dev MCP server can resolve the bare name, then commit**

The new `.mcp.json` launches `side-quest` by bare name, so it must be on `PATH`
(the git hooks call it by absolute path and are unaffected; this is only for the
MCP server in the dev repo).

Run: `command -v side-quest`
Expected: prints a path. If it prints nothing, the `go install`ed binary at
`~/go/bin/side-quest` exists but that dir is not on `PATH`. Add it (in your shell
profile so Claude Code inherits it) and re-open the shell/session:

```
export PATH="$HOME/go/bin:$PATH"
```

Re-run `command -v side-quest` and confirm it prints `…/go/bin/side-quest` before
committing. (If the binary itself is absent, run `go install ./cmd/side-quest`.)

```bash
git add .claude-plugin/plugin.json .claude-plugin/marketplace.json VERSION LICENSE .mcp.json internal/packaging/manifests_test.go
git commit -m "$(cat <<'EOF'
feat: add plugin manifests, VERSION, LICENSE; point .mcp.json at PATH binary

Adds .claude-plugin/plugin.json + marketplace.json, a root VERSION file
(single source of truth, matched by plugin.json), an MIT LICENSE
(c) 2026 Marcus Kellerman, and switches .mcp.json from `go run` to the
bare `side-quest serve` used by the shipped plugin.

Quest: none

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws
EOF
)"
```

---

### Task 3: `/sq` command and AGENTS.md

**Files:**
- Create: `commands/sq.md`, `AGENTS.md`
- Test: extend `internal/packaging/manifests_test.go`

**Interfaces:**
- Consumes: `repoFile` helper from Task 2's test file.
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Write the failing test**

Append to `internal/packaging/manifests_test.go`:

```go
func TestSqCommandDrivesQuestNew(t *testing.T) {
	c := string(repoFile(t, "commands/sq.md"))
	if !strings.Contains(c, "quest_new") {
		t.Error("commands/sq.md must instruct the agent to call quest_new")
	}
	if !strings.Contains(c, "$ARGUMENTS") {
		t.Error("commands/sq.md must consume $ARGUMENTS")
	}
}

func TestAgentsDocPointsToSkill(t *testing.T) {
	a := string(repoFile(t, "AGENTS.md"))
	if !strings.Contains(a, "skills/side-quest/SKILL.md") {
		t.Error("AGENTS.md must reference skills/side-quest/SKILL.md")
	}
	for _, want := range []string{"Quest:", "Completes:", "current"} {
		if !strings.Contains(a, want) {
			t.Errorf("AGENTS.md missing mention of %q", want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/packaging/ -run 'TestSqCommand|TestAgentsDoc' -v`
Expected: FAIL — `commands/sq.md` and `AGENTS.md` do not exist.

- [ ] **Step 3: Create the files**

Create `commands/sq.md`:

```markdown
---
description: Capture a side quest without derailing your current work
argument-hint: <the idea to capture>
---

A new idea just surfaced mid-task: **$ARGUMENTS**

Capture it as a side quest, then immediately return to what we were doing. Do
NOT start working on the idea.

1. Call the `quest_new` MCP tool (side-quest server) with:
   - `title`: a concise, self-contained restatement of the idea (not a verbatim echo).
   - `context`: one sentence on *why it came up now* — what we were doing when it
     surfaced — so it is recoverable later.
   - Do not set it current. Do not set `type`/`priority` unless the user stated them.
2. Confirm in one line: the returned quest id and its title. Nothing more.
3. Resume the previous task exactly where we left off.

If the side-quest MCP server or the `quest_new` tool is unavailable, tell the
user to install and enable the side-quest plugin (and its binary). Do not fall
back to editing files.
```

Create `AGENTS.md`:

```markdown
# side-quest for agents

side-quest is a git-native issue tracker. Quests live on a dedicated git ref
(`refs/side-quest/quests`), not in the working tree. Any MCP-capable agent can
drive it through the `side-quest serve` stdio MCP server. This file is
agent-agnostic; the Claude-plugin-flavored version of the same guidance is
`skills/side-quest/SKILL.md`.

## Capture reflex

When a new, unrelated idea surfaces mid-task, capture it instead of derailing:
call `quest_new` with a concise `title` and a one-sentence `context` note
explaining *why it came up now*. Do not set it current. Then keep working.

## Attributing commits (the trailer contract)

Commits link to quests through message trailers, read by a `post-commit` hook:

- `Quest: SQ-0001` — link this commit to SQ-0001 (no status change).
- `Completes: SQ-0001` — link it and mark SQ-0001 done.
- `Quest: none` — an explicit opt-out for a genuine chore.

Prefer explicit, per-commit trailers over implicit state — nothing is sticky, so
unrelated commits are never mis-attributed.

## The current quest

Each worktree can have one "current" quest (`quest_set_current`). When
`auto_trailer` is on (the default), the `prepare-commit-msg` hook injects that
quest's `Quest:` trailer automatically. Setting a current quest is optional and
mainly useful when teeing up a human's commits; agents should prefer writing the
trailer explicitly.

## Triage values

`type` is `bug` or `feature` (default `feature`); `priority` is `high` or `low`
(default `low`); `status` is `open`, `partial`, `done`, `deferred`, or
`discarded` (new quests start `open`). Tags are free-form annotations.
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/packaging/ -run 'TestSqCommand|TestAgentsDoc' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add commands/sq.md AGENTS.md internal/packaging/manifests_test.go
git commit -m "$(cat <<'EOF'
feat: add /sq capture command and agent-agnostic AGENTS.md

commands/sq.md drives capture via the quest_new MCP tool without
derailing. AGENTS.md documents the capture reflex, trailer contract,
and current-quest pointer for any MCP agent, pointing at
skills/side-quest/SKILL.md for the Claude-flavored version.

Quest: none

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws
EOF
)"
```

---

### Task 4: Self-provisioning binary launcher

**Files:**
- Create: `bin/side-quest`, `bin/side-quest.cmd`
- Test: `internal/packaging/launcher_test.go`

**Interfaces:**
- Consumes: the root `VERSION` file (Task 2); the `repoFile` helper (Task 2's test file).
- Produces: `bin/side-quest` — a POSIX launcher resolving the native binary (cache → PATH → download+verify → fail). Referenced by the shipped `.mcp.json` via the plugin `bin/` on PATH.

**Note on scope:** the download branch (step 3) cannot be exercised end-to-end until the repo is public with a v0.1.0 Release; the tests cover cache-hit, PATH-passthrough, and graceful failure (download stubbed by forcing `VERSION=dev`). The Windows launcher's live behavior is a go-public verification item; it is created here but not automatically tested.

- [ ] **Step 1: Write the failing test**

Create `internal/packaging/launcher_test.go`:

```go
package packaging

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func launcherPath(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs("../../bin/side-quest")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("launcher missing: %v", err)
	}
	return p
}

// writeExec writes an executable shell script.
func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// cleanEnv isolates the launcher from the developer's real PATH/HOME so that a
// real `side-quest` on the machine cannot leak into the resolution.
func cleanEnv(pathDir, pluginData string) []string {
	return []string{
		"PATH=" + pathDir + ":/usr/bin:/bin",
		"CLAUDE_PLUGIN_DATA=" + pluginData,
		"HOME=" + pluginData,
	}
}

// Step 1 of the chain: a cached binary for this VERSION is exec'd.
func TestLauncherExecsCachedBinary(t *testing.T) {
	ver := strings.TrimSpace(string(repoFile(t, "VERSION")))
	data := t.TempDir()
	cache := filepath.Join(data, "bin")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExec(t, filepath.Join(cache, "side-quest-"+ver), "#!/bin/sh\necho CACHED \"$@\"\n")

	cmd := exec.Command(launcherPath(t), "serve")
	cmd.Env = cleanEnv(t.TempDir(), data)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.HasPrefix(string(out), "CACHED serve") {
		t.Errorf("got %q, want the cached binary", out)
	}
}

// Step 2 of the chain: a side-quest already on PATH (dev build) is exec'd.
func TestLauncherExecsPathBinary(t *testing.T) {
	shim := t.TempDir()
	writeExec(t, filepath.Join(shim, "side-quest"), "#!/bin/sh\necho PATHBIN \"$@\"\n")

	cmd := exec.Command(launcherPath(t), "serve")
	cmd.Env = cleanEnv(shim, t.TempDir()) // empty plugin-data dir => no cache hit
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.HasPrefix(string(out), "PATHBIN serve") {
		t.Errorf("got %q, want the PATH binary", out)
	}
}

// Step 4 of the chain: nothing resolves and download is disabled (VERSION=dev),
// so the launcher prints the install hint and exits non-zero.
func TestLauncherFailsWithHint(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte("dev\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(launcherPath(t))
	if err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(root, "bin", "side-quest")
	if err := os.WriteFile(fake, src, 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(fake, "serve")
	cmd.Env = cleanEnv(t.TempDir(), t.TempDir())
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit, got success: %s", out)
	}
	if !strings.Contains(string(out), "go install github.com/sharkusk/side-quest/cmd/side-quest@latest") {
		t.Errorf("missing install hint: %s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/packaging/ -run TestLauncher -v`
Expected: FAIL — `bin/side-quest` does not exist (`launcher missing`).

- [ ] **Step 3: Create the launchers**

Create `bin/side-quest` (POSIX `sh`, mark executable):

```sh
#!/bin/sh
# side-quest plugin launcher. Resolves the native `side-quest` binary for this
# OS/arch, provisioning it from the matching GitHub Release on first use.
# Resolution order (first hit wins):
#   1. a cached binary for this version
#   2. a side-quest already on PATH (dev build / `go install`)
#   3. download the matching release asset + verify its SHA-256
#   4. print an install hint and fail (never run something wrong)
set -eu

REPO="sharkusk/side-quest"
SELF="$0"

# Plugin root: this script lives at <root>/bin/side-quest.
ROOT=$(CDPATH= cd -- "$(dirname -- "$SELF")/.." && pwd -P)
VERSION=$(cat "$ROOT/VERSION" 2>/dev/null || echo dev)

# Cache dir; CLAUDE_PLUGIN_DATA survives plugin updates when Claude provides it.
CACHE="${CLAUDE_PLUGIN_DATA:-${XDG_CACHE_HOME:-$HOME/.cache}/side-quest}/bin"
BIN="$CACHE/side-quest-$VERSION"

# sha256 of a file, portable across Linux (sha256sum) and macOS (shasum).
sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	else
		echo ""
	fi
}

# 1. Cached binary for this version.
if [ -x "$BIN" ]; then
	exec "$BIN" "$@"
fi

# 2. A real side-quest already on PATH, as long as it is not this launcher.
found=$(command -v side-quest 2>/dev/null || true)
if [ -n "$found" ]; then
	found_dir=$(CDPATH= cd -- "$(dirname -- "$found")" && pwd -P)
	found_abs="$found_dir/$(basename -- "$found")"
	if [ "$found_abs" != "$SELF" ] && [ -x "$found_abs" ]; then
		exec "$found_abs" "$@"
	fi
fi

# 3. Download the matching release asset and verify its checksum.
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
esac
if [ "$VERSION" != dev ] && command -v curl >/dev/null 2>&1; then
	asset="side-quest_${VERSION}_${os}_${arch}.tar.gz"
	base="https://github.com/$REPO/releases/download/v$VERSION"
	tmp=$(mktemp -d)
	if curl -fsSL "$base/$asset" -o "$tmp/$asset" &&
		curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"; then
		want=$(grep " $asset\$" "$tmp/checksums.txt" | awk '{print $1}')
		got=$(sha256 "$tmp/$asset")
		if [ -n "$want" ] && [ "$want" = "$got" ]; then
			mkdir -p "$CACHE"
			tar -xzf "$tmp/$asset" -C "$tmp"
			mv "$tmp/side-quest" "$BIN"
			chmod +x "$BIN"
			rm -rf "$tmp"
			exec "$BIN" "$@"
		fi
	fi
	rm -rf "$tmp"
fi

# 4. Could not provision — tell the user how, and fail.
echo "side-quest: could not locate or download the side-quest binary." >&2
echo "  Install it with:  go install github.com/$REPO/cmd/side-quest@latest" >&2
echo "  (then ensure ~/go/bin is on your PATH), or download a release from" >&2
echo "  https://github.com/$REPO/releases and put it on your PATH." >&2
exit 1
```

Make it executable:

```bash
chmod +x bin/side-quest
```

Create `bin/side-quest.cmd` (Windows; same resolution order via PowerShell):

```bat
@echo off
setlocal enabledelayedexpansion
set "REPO=sharkusk/side-quest"
set "ROOT=%~dp0.."
set /p VERSION=<"%ROOT%\VERSION" 2>nul
if "%VERSION%"=="" set "VERSION=dev"
if not defined CLAUDE_PLUGIN_DATA set "CLAUDE_PLUGIN_DATA=%LOCALAPPDATA%\side-quest"
set "CACHE=%CLAUDE_PLUGIN_DATA%\bin"
set "BIN=%CACHE%\side-quest-%VERSION%.exe"

rem 1. Cached binary for this version.
if exist "%BIN%" (
  "%BIN%" %*
  exit /b %errorlevel%
)

rem 3. Download + checksum-verify via PowerShell (skipped when VERSION=dev).
if not "%VERSION%"=="dev" (
  set "ASSET=side-quest_%VERSION%_windows_amd64.zip"
  powershell -NoProfile -ExecutionPolicy Bypass -Command ^
    "$ErrorActionPreference='Stop';" ^
    "$base='https://github.com/%REPO%/releases/download/v%VERSION%';" ^
    "New-Item -Force -ItemType Directory '%CACHE%' | Out-Null;" ^
    "$tmp=New-TemporaryFile; Invoke-WebRequest \"$base/%ASSET%\" -OutFile \"$tmp.zip\";" ^
    "Invoke-WebRequest \"$base/checksums.txt\" -OutFile \"$tmp.sums\";" ^
    "$want=(Select-String -Path \"$tmp.sums\" -Pattern ([regex]::Escape('%ASSET%'))).Line.Split(' ')[0];" ^
    "$got=(Get-FileHash \"$tmp.zip\" -Algorithm SHA256).Hash.ToLower();" ^
    "if ($want -ne $got) { exit 3 };" ^
    "Expand-Archive \"$tmp.zip\" -DestinationPath \"$tmp.dir\" -Force;" ^
    "Move-Item -Force \"$tmp.dir\\side-quest.exe\" '%BIN%'"
  if exist "%BIN%" (
    "%BIN%" %*
    exit /b %errorlevel%
  )
)

rem 4. Could not provision — hint and fail.
echo side-quest: could not locate or download the side-quest binary.>&2
echo   Install it with:  go install github.com/%REPO%/cmd/side-quest@latest>&2
echo   or download a release from https://github.com/%REPO%/releases>&2
exit /b 1
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/packaging/ -run TestLauncher -v`
Expected: PASS (cache, PATH, failure). The download branch is not exercised (that is a go-public verification item recorded in the spec).

- [ ] **Step 5: Commit**

```bash
git add bin/side-quest bin/side-quest.cmd internal/packaging/launcher_test.go
git commit -m "$(cat <<'EOF'
feat: add self-provisioning plugin binary launcher

bin/side-quest (POSIX) and bin/side-quest.cmd (Windows) resolve the
native binary: cached -> on PATH -> download the matching release and
verify its SHA-256 -> else print a `go install` hint and fail. Wired via
the plugin bin/ that Claude prepends to PATH. The download path activates
once the repo is public with a v0.1.0 release.

Quest: none

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws
EOF
)"
```

---

### Task 5: Release pipeline (GoReleaser + GitHub Actions)

**Files:**
- Create: `.goreleaser.yaml`, `.github/workflows/release.yml`

**Interfaces:**
- Consumes: `main.version` (Task 1) for ldflags stamping; `LICENSE` + `README.md` bundled into archives; the release tag `v<VERSION>`.
- Produces: on a pushed `v*` tag, six release archives named `side-quest_<version>_<os>_<arch>.{tar.gz,zip}` plus `checksums.txt` — the exact asset names the launcher (Task 4) downloads.

**Note:** there is no Go unit test here; verification is `goreleaser check` + a local snapshot build. `dist/` is already git-ignored (`.gitignore` line `/dist/`).

- [ ] **Step 1: Create `.goreleaser.yaml`**

```yaml
version: 2

project_name: side-quest

builds:
  - id: side-quest
    main: ./cmd/side-quest
    binary: side-quest
    env:
      - CGO_ENABLED=0
    flags:
      - -trimpath
    ldflags:
      - -s -w -X main.version={{ .Version }}
    goos: [darwin, linux, windows]
    goarch: [amd64, arm64]

archives:
  - id: default
    name_template: "side-quest_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    formats: [tar.gz]
    format_overrides:
      - goos: windows
        formats: [zip]
    files:
      - README.md
      - LICENSE

checksum:
  name_template: checksums.txt
  algorithm: sha256

release:
  draft: false
```

- [ ] **Step 2: Verify the config and a full cross-compile**

Ensure GoReleaser is installed (`goreleaser --version`); if absent, install with `go install github.com/goreleaser/goreleaser/v2@latest` (adds `~/go/bin/goreleaser`) or `brew install goreleaser`.

Run: `goreleaser check`
Expected: `1 configuration file(s) validated` (no errors).

Run: `goreleaser build --snapshot --clean`
Expected: success; `dist/` contains six binaries (darwin/linux/windows × amd64/arm64). Confirm with:

Run: `ls dist/*/side-quest* | wc -l`
Expected: `6`.

Verify the snapshot stamped the version:

Run: `./dist/side-quest_*_$(go env GOOS)_$(go env GOARCH)*/side-quest version` (pick the built dir for your host; e.g. `dist/side-quest_*_darwin_arm64/side-quest version`)
Expected: a non-`dev` snapshot version string (GoReleaser injects one for snapshots).

- [ ] **Step 3: Create `.github/workflows/release.yml`**

```yaml
name: release

on:
  push:
    tags:
      - 'v*'

permissions:
  contents: write

jobs:
  goreleaser:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
      - uses: goreleaser/goreleaser-action@v6
        with:
          version: '~> v2'
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

- [ ] **Step 4: Confirm nothing leaked into git and the build is clean**

Run: `git status --porcelain dist/`
Expected: empty (dist/ is git-ignored).

Run: `go build ./... && go test ./...`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add .goreleaser.yaml .github/workflows/release.yml
git commit -m "$(cat <<'EOF'
feat: add GoReleaser config and tag-triggered release workflow

Builds six targets (darwin/linux/windows x amd64/arm64), stamps
main.version via ldflags, archives with README + LICENSE, and emits
checksums.txt. A pushed v* tag runs goreleaser release on Go 1.25.
Asset names match the launcher's download expectations.

Quest: none

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws
EOF
)"
```

---

### Task 6: README rewrite and architecture.md updates

**Files:**
- Rewrite: `README.md`
- Modify: `docs/architecture.md`
- Test: `internal/packaging/manifests_test.go` (append doc-invariant checks)

**Interfaces:**
- Consumes: the `repoFile` helper.
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Write the failing test**

Append to `internal/packaging/manifests_test.go`:

```go
func TestReadmeReframedAndToneRemoved(t *testing.T) {
	r := string(repoFile(t, "README.md"))
	if strings.Contains(r, "\n## Tone\n") {
		t.Error("README must not have a Tone section (voice is kept a surprise)")
	}
	if !strings.Contains(r, "go install github.com/sharkusk/side-quest/cmd/side-quest@latest") {
		t.Error("README missing the corrected go install path")
	}
	if !strings.Contains(r, "1.25") {
		t.Error("README must state the Go >=1.25 floor")
	}
	if strings.Contains(r, "go install github.com/sharkusk/side-quest@latest") {
		t.Error("README still has the broken root-path go install command")
	}
}

func TestArchitectureHasToneAndPackaging(t *testing.T) {
	a := string(repoFile(t, "docs/architecture.md"))
	if !strings.Contains(a, "SIDE_QUEST_TONE") {
		t.Error("architecture.md should document the SIDE_QUEST_TONE override")
	}
	if !strings.Contains(a, "CLAUDE_PLUGIN_ROOT") && !strings.Contains(a, "launcher") {
		t.Error("architecture.md should document the plugin launcher/packaging")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/packaging/ -run 'TestReadme|TestArchitecture' -v`
Expected: FAIL — the current README still has a `## Tone` section and the broken install path; architecture.md has no packaging note.

- [ ] **Step 3: Rewrite `README.md`**

Replace the entire file with:

```markdown
# side-quest

A streamlined, git-native issue tracker for individuals and small teams. Capture
the *side quests* — the new ideas, follow-ups, and research tangents that occur to
you mid-work — without derailing your current session, and keep a clean two-way
link between every quest and the git commits that address it.

> **Status: CLI + MCP server + plugin packaging ready.** The quest store, git
> hooks, CLI (init/new/list/show/status/reclassify/config), MCP server
> (`side-quest serve`), and the Claude Code plugin are built and tested. A
> TODO/COMPLETED importer and a `sync` command are planned (see "Roadmap").

## The problem it solves

Most trackers can't cleanly link a task to the commit that resolved it: a commit's
hash doesn't exist until *after* the commit, and if the task lives in the same repo,
recording that hash needs another commit — with its own hash. The loop never closes.

side-quest stores quest data on a dedicated git ref (`refs/side-quest/quests`), off
your main history and never checked out. A `post-commit` hook writes the now-known
hash back into the quest as a separate commit on that ref — so the loop closes
cleanly, and the data still travels with your repo.

## Concepts

side-quest stores quests as one Markdown file per quest on a dedicated git **ref**
(`refs/side-quest/quests`) — an **orphan ref** with its own history, off your main
line and never checked out. It reads and writes that ref with git's low-level
**plumbing** commands (never touching your working tree), and every change is
committed with a **compare-and-swap (CAS)** so parallel git worktrees stay safe
without a lock.

- **ref / orphan ref** — a named pointer to a commit; the orphan ref holds quest
  data on its own root history.
- **type / priority** — every quest carries a `type` (bug/feature) and a `priority`
  (high/low), enums that default to feature/low when a quick capture omits them.
- **status** — `open` (default), `partial`, `done`, `deferred`, `discarded`.
- **trailer** — `Quest: SQ-xxxx` / `Completes: SQ-xxxx` lines in a commit message; a
  `post-commit` hook reads them and links the commit to the quest (`Quest: none`
  opts a chore out).
- **current quest** — a per-worktree pointer (`side-quest current <id>`) that
  `prepare-commit-msg` uses to auto-fill the `Quest:` trailer.

**→ For the storage model, CAS, the mutation flow, and id allocation, see
[`docs/architecture.md`](docs/architecture.md).**

## Usage

```
side-quest init
side-quest new "Fix the flaky parser test" --type bug --priority high
side-quest list --status open --type bug
side-quest show SQ-0001
side-quest status SQ-0001 done
side-quest reclassify SQ-0001 --priority low
side-quest config set require_quest true
side-quest config get
```

Add `--json` to `new`, `list`, `show`, or `config get` for machine-readable output.
Flags may appear before or after the title/id positional argument.

## Installation

### Prebuilt binary (no toolchain)

Download the archive for your platform from the
[Releases](https://github.com/sharkusk/side-quest/releases) page, extract the
`side-quest` binary, and put it on your `PATH`.

| Platform | Where to put it | Notes |
|---|---|---|
| macOS | `/usr/local/bin` or `~/.local/bin` | `chmod +x side-quest`; first run may be blocked by Gatekeeper (unsigned) — clear it with `xattr -d com.apple.quarantine side-quest` |
| Linux | `~/.local/bin` (often on `PATH`) or `/usr/local/bin` (sudo) | `chmod +x side-quest` |
| Windows | a folder you add to `Path`, e.g. `%LOCALAPPDATA%\Programs\side-quest\` | use `side-quest.exe` |

### `go install` (needs Go ≥ 1.25)

```
go install github.com/sharkusk/side-quest/cmd/side-quest@latest
```

This installs to `~/go/bin` (`%USERPROFILE%\go\bin` on Windows), which is **not on
`PATH` by default** — add it:

- macOS/Linux: `export PATH="$HOME/go/bin:$PATH"` in your shell profile.
- Windows: add `%USERPROFILE%\go\bin` to your user `Path` environment variable.

### Build from source (needs Go ≥ 1.25)

```
git clone https://github.com/sharkusk/side-quest && cd side-quest
go build -o side-quest ./cmd/side-quest
```

### Per-project setup

Inside the repo you want to track:

```
side-quest init            # create the quest ref
side-quest install-hooks   # install git hooks + the refs/side-quest/* refspec
```

### Claude Code plugin

```
/plugin marketplace add sharkusk/side-quest
/plugin install side-quest
```

The plugin registers the `side-quest` MCP server and the `/sq` capture command. On
first use it **auto-provisions** the matching `side-quest` binary (downloaded from
the release and checksum-verified) into a per-plugin cache. If a download isn't
possible (offline, or before the project is public), install the binary yourself
with `go install github.com/sharkusk/side-quest/cmd/side-quest@latest` and the
plugin will use it from your `PATH`.

### Sharing quests across machines

The `refs/side-quest/quests` ref is not fetched by default. `side-quest init`
configures the fetch refspec; to publish quests to a remote, push the ref:

```
git push origin refs/side-quest/quests
```

A dedicated `sync` command that automates pull/push is **planned** (see "Roadmap").

## MCP server

`side-quest serve` runs a stdio MCP server so any MCP-capable agent can capture,
read, and drive quests. Register it (assumes `side-quest` is on `PATH`):

```json
{ "mcpServers": { "side-quest": { "command": "side-quest", "args": ["serve"] } } }
```

Tools: `quest_new`, `quest_list`, `quest_show`, `quest_set_status`,
`quest_reclassify`, `quest_update`, `quest_note`, `quest_set_current`,
`quest_get_current`, `quest_link_commit`. Responses are neutral JSON. For
agent-facing guidance see [`AGENTS.md`](AGENTS.md) and
[`skills/side-quest/SKILL.md`](skills/side-quest/SKILL.md).

## Development

- **Requirements:** Go ≥ 1.25; the system `git` binary (used as a subprocess);
  `gopkg.in/yaml.v3`; the MCP Go SDK. No CGo — a pure-Go static binary.
  [GoReleaser](https://goreleaser.com) is needed only to cut releases.
- **Layout:** `internal/` packages (`quest`, `config`, `gitcmd`, `store`,
  `trailer`, `voice`) with the `cli` and `mcp` frontends under `cmd/side-quest`.
- **Build & test:**

  ```
  go build ./...
  go test ./...
  go vet ./...
  ```

- **Cutting a release:** bump `VERSION` and `plugin.json`'s `version` together, then
  `git tag v$(cat VERSION) && git push --tags`. The release workflow runs GoReleaser
  and publishes the six platform archives + `checksums.txt`. Validate the config
  locally with `goreleaser check` and `goreleaser build --snapshot --clean`.
- **Developing side-quest while using it elsewhere:** keep the released
  `side-quest` on your `PATH` for the project you track in production; for
  development, build a local `./side-quest` (`go build -o side-quest ./cmd/side-quest`)
  and invoke it explicitly. Never point a work-in-progress binary at a live repo —
  run it against the side-quest repo itself or throwaway `git init` scratch repos
  (the test suite already isolates via temp repos). Quest data is per-repo on
  `refs/side-quest/*`, so working in this repo cannot touch another project's quests.

## Roadmap

- **Importer** — a best-effort importer for existing `TODO.md` / `COMPLETED.md`
  files (planned).
- **`sync`** — a command to pull/push the quest ref across machines (planned).

## Credits & permissions

side-quest's output has a bit of personality under the hood. Its flavored voice is
an original homage to *Dungeon Crawler Carl* by Matt Dinniman — no verbatim
book/show text is included or shipped. Verbatim catch phrases are never distributed
with side-quest; they load only from a file you create yourself. Public or committed
use of verbatim phrases requires permission from the author.
```

- [ ] **Step 4: Update `docs/architecture.md`**

The "Voice layer (`internal/voice`)" section already documents tone precedence and
`SIDE_QUEST_TONE`. Add a short **user-facing configuration** paragraph at the end of
that section (immediately before the `## MCP frontend` header at
`docs/architecture.md:238`), so the README no longer needs a Tone section:

```markdown
**Configuring the tone (user-facing).** Three tones exist: `plain` (neutral),
`dcc` (the default flavored voice), and `dcc-superfan` (opt-in; currently falls
back to `dcc`). Set it persistently with `side-quest config set tone <value>`, or
override it for one invocation with the `SIDE_QUEST_TONE` environment variable
(`SIDE_QUEST_TONE=plain` forces neutral output). This is deliberately not
advertised in the README — the flavored default is meant to be discovered on first
run — but it is fully documented and configurable here.
```

Then add a new packaging subsection at the very end of the file (after the
`## Glossary` section), documenting the distribution layer:

```markdown
## Packaging & distribution (Phase 7)

side-quest ships both as a standalone binary and as a Claude Code plugin from the
same repository.

- **Plugin manifests** live in `.claude-plugin/` (`plugin.json`, `marketplace.json`).
  `commands/sq.md` is the `/sq` capture command (it calls the `quest_new` MCP tool);
  `AGENTS.md` is the agent-agnostic contract; `skills/side-quest/SKILL.md` is the
  Claude-flavored workflow skill.
- **The `.mcp.json`** launches the server by bare name (`side-quest serve`). Inside
  an installed plugin, Claude prepends the plugin's `bin/` to `PATH`, so `side-quest`
  resolves to `bin/side-quest` (POSIX) or `bin/side-quest.cmd` (Windows).
- **The launcher** (`bin/side-quest`) resolves the native binary in order: a cached
  copy under `${CLAUDE_PLUGIN_DATA}` → a `side-quest` already on `PATH` → download
  the matching release asset and verify its SHA-256 against the release
  `checksums.txt` → otherwise print a `go install` hint and exit non-zero. No
  compiled binaries are committed to the repo.
- **Versioning:** the root `VERSION` file is the single source of truth; `plugin.json`'s
  `version` matches it (test-enforced); the release tag is `v` + `VERSION`. The binary
  reports its version via `side-quest version`, stamped at release build time by
  GoReleaser (`-ldflags "-X main.version=<tag>"`); plain builds report `dev`.
- **Releases** are produced by GoReleaser (`.goreleaser.yaml`) via a tag-triggered
  GitHub Actions workflow: six targets (darwin/linux/windows × amd64/arm64), archived
  with README + LICENSE, plus `checksums.txt`.
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/packaging/ -run 'TestReadme|TestArchitecture' -v`
Expected: PASS.

- [ ] **Step 6: Full regression and commit**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all pass.

```bash
git add README.md docs/architecture.md internal/packaging/manifests_test.go
git commit -m "$(cat <<'EOF'
docs: reframe README as a git-native issue tracker; document packaging

Rewrites the README around the individual/small-team issue-tracker pitch
(SQ-0011), adds per-platform install paths with the corrected go install
path and the Go >=1.25 floor, a development section (GoReleaser,
dev-while-in-use), and importer/sync as planned stubs. Removes the README
Tone section to keep the voice a first-run surprise; relocates tone
configuration and adds a packaging subsection to architecture.md.

Completes: SQ-0011

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws
EOF
)"
```

---

## Definition of done

- `go build ./...`, `go vet ./...`, `go test ./...` all green.
- `internal/packaging` tests pass: manifest validity, `plugin.json` version == `VERSION`, MIT LICENSE, `/sq` drives `quest_new`, AGENTS.md points to the skill, launcher cache/PATH/failure branches, README reframed + tone-section-free + correct install path/Go floor, architecture.md has tone config + packaging note.
- `goreleaser check` passes and `goreleaser build --snapshot --clean` produces six binaries.
- SQ-0011 is closed by the Task 6 commit's `Completes:` trailer.
- Repo remains private; `/plugin marketplace add` + live download are recorded as go-public verification items (spec §"Go-public checklist"), not run here.
```
