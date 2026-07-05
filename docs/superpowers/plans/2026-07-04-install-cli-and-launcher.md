# `install-cli` / `uninstall-cli` + the self-healing launcher — Implementation Plan (SQ-0065)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a plugin user run `side-quest` from their own terminal by placing a small, read-only launcher on their PATH that resolves the binary the plugin already provisioned — with matching `install-cli` / `uninstall-cli` subcommands and a launcher that self-heals when the plugin is gone.

**Architecture:** The mechanism lives in a new **`internal/cli`** package: PATH-dir selection, the launcher marker, the embedded launcher scripts, and `Install` / `Uninstall` / `Status`. Two thin binary subcommands in `cmd/side-quest/installcli.go` (`install-cli`, `uninstall-cli`) format the results for the terminal. The **same `internal/cli` core** is what SQ-0066's MCP `cli_*` tools call, so enabling the CLI is one implementation reachable two ways. The launcher itself is a **pure resolver**: newest `~/.claude/plugins/data/side-quest-side-quest/bin/side-quest-*` → exec; data dir present but empty → "open a Claude session"; data dir absent → announce/offer self-removal. It never downloads.

**Tech Stack:** Go (`os`, `path/filepath`, `runtime`, `bytes`, `embed`); POSIX `sh` + Windows batch for the launchers; existing `cmd/side-quest` and (for the package unit tests) standard `testing` harness.

**Depends on:** SQ-0064 (plan `2026-07-04-onboard-single-front-door.md`) — this plan's usage edit (Task 5) targets the regrouped `usage` const that SQ-0064 Task 3 produced. Land SQ-0064 first.

**Produces for SQ-0066:** the exported `internal/cli` surface (`Install`, `Uninstall`, `Status`, `Marker`) that the MCP `cli_*` tools wrap. Keep the signatures in this plan's Interfaces blocks stable.

## Global Constraints

- **TDD, no exceptions for code:** RED → verify fail → GREEN → verify pass → commit. Docs-only edits ride the commit of the task whose behavior they describe.
- **Branch-safety invariant (HARD RULE):** side-quest may only ever write `refs/side-quest/*`, git hooks, and a scratch index. `install-cli`/`uninstall-cli` write **only** a user-owned PATH dir (`~/.local/bin` and friends) — never a git ref/index/worktree and never a Claude-managed data dir (the launcher is *read-only* toward the data dir; provisioning is the plugin's MCP server on startup, not this code).
- **The launcher never downloads.** If the plugin is installed, its MCP server already provisioned the binary on startup. A download path here would be dead weight (spec D4).
- **Marker:** every launcher we write contains the token `side-quest-cli-launcher`. `Uninstall` removes only files carrying it; `Install` refuses to overwrite a `side-quest` that lacks it (spec D8). (Distinct from the plugin shim's comment "side-quest plugin launcher", which has no hyphen between `quest` and `launcher`, so there is no false match.)
- **PATH-dir preference (spec D7):** first on-PATH dir among `$XDG_BIN_HOME`, `~/.local/bin`, `~/bin`, `~/go/bin`; else fall back to `~/.local/bin` (create it) and report a "add to PATH" notice.
- **Detection/removal scan the union of `$PATH` and the candidate dirs (spec D7).** `Status`/`Uninstall` are also called from the MCP server, whose `$PATH` can be the GUI PATH — not the user's login shell — so scanning only `$PATH` would miss a launcher we wrote into `~/.local/bin`. Scan both, deduped.
- **Windows (spec D4.3):** a running `.cmd` cannot reliably delete itself, so the Windows launcher never self-deletes — the plugin-gone case prints its path as safe to remove.
- **Commit trailer format** (every commit; blank line before the co-author block):

  ```
  <subject> (SQ-0065)

  <optional one-line body>

  Quest: SQ-0065

  Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws
  ```

- **Do not `git push`** (the user pushes explicitly).
- **Test commands:** `go test ./internal/cli/` (core + launcher runtime) and `go test ./cmd/side-quest/` (subcommand wiring).

---

## File Structure

- **Create `internal/cli/cli.go`** — `Marker`; `InstallDirCandidates`; `ChooseInstallDir`; `LauncherName`/`LauncherBody`; the `//go:embed` of the two scripts; `Install`/`Uninstall`/`Status` and their result structs; the internal `launcherDirs` helper.
- **Create `internal/cli/launcher.sh`** — the embedded POSIX resolver (written by `Install` as `side-quest`). Also exec'd directly by the package tests.
- **Create `internal/cli/launcher.cmd`** — the embedded Windows resolver (written as `side-quest.cmd`).
- **Create `internal/cli/cli_test.go`** — Go unit tests: `ChooseInstallDir` table; `Install` writes marked / refuses unmarked / reports off-PATH; `Uninstall` removes marked / leaves unmarked / friendly empty; `Status` finds a marked launcher in a candidate dir off `$PATH`.
- **Create `internal/cli/launcher_test.go`** — POSIX launcher runtime tests (exec `launcher.sh` with a controlled `HOME`) + a `.cmd` content check.
- **Create `cmd/side-quest/installcli.go`** — thin `cmdInstallCli` / `cmdUninstallCli` wrappers that call `internal/cli` and format for the terminal.
- **Create `cmd/side-quest/installcli_test.go`** — end-to-end wiring smoke: `install-cli` writes a launcher; `uninstall-cli` removes it.
- **Modify `cmd/side-quest/main.go`** — add `install-cli` / `uninstall-cli` to `run()`; add two lines to the `usage` "Advanced" block (SQ-0064).
- **Modify `cmd/side-quest/main_test.go`** — assert the two commands are listed in usage.
- **Modify `docs/architecture.md`** — document the two subcommands, the `internal/cli` core, and the read-only launcher in the packaging section.

---

### Task 1: `internal/cli` core — marker + PATH-dir selection (pure logic)

**Files:**
- Create: `internal/cli/cli.go` (partial — marker + the two pure helpers; the embeds and `Install`/`Uninstall`/`Status` land in Tasks 2-4)
- Test: `internal/cli/cli_test.go`

**Interfaces:**
- Produces: `const Marker = "side-quest-cli-launcher"`; `func InstallDirCandidates(home, xdgBinHome string) []string`; `func ChooseInstallDir(candidates, pathDirs []string) string` (returns `""` when none on PATH).

- [ ] **Step 1: Write the failing test**

Create `internal/cli/cli_test.go`:

```go
package cli

import (
	"path/filepath"
	"testing"
)

func TestChooseInstallDir(t *testing.T) {
	home := "/home/dev"
	cands := InstallDirCandidates(home, "/home/dev/.xbin")
	// InstallDirCandidates order: [$XDG_BIN_HOME, ~/.local/bin, ~/bin, ~/go/bin].
	local := filepath.Join(home, ".local", "bin")
	gobin := filepath.Join(home, "go", "bin")

	cases := []struct {
		name     string
		pathDirs []string
		want     string
	}{
		{"xdg preferred when on path", []string{"/home/dev/.xbin", local}, "/home/dev/.xbin"},
		{"falls to ~/.local/bin", []string{"/usr/bin", local}, local},
		{"skips off-path, picks go/bin", []string{gobin}, gobin},
		{"none on path -> empty", []string{"/usr/bin", "/bin"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ChooseInstallDir(cands, c.pathDirs); got != c.want {
				t.Errorf("ChooseInstallDir = %q, want %q", got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestChooseInstallDir`
Expected: FAIL — the package does not compile: `undefined: InstallDirCandidates` / `ChooseInstallDir`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/cli/cli.go` (no `//go:embed` yet — the script files don't exist until Task 2, and an embed of a missing file fails the build):

```go
// Package cli is the shared core of side-quest's terminal-CLI launcher: choosing
// a PATH dir, and writing / removing / detecting the marked launcher that resolves
// the binary the Claude Code plugin provisions into its data dir. Both the
// install-cli/uninstall-cli subcommands (cmd/side-quest) and the MCP cli_* tools
// (internal/mcp, SQ-0066) call this one place, so enabling the CLI is a single
// implementation reachable two ways.
package cli

import (
	"path/filepath"
)

// Marker identifies a launcher this package wrote. Uninstall removes only files
// carrying it, and Install refuses to overwrite a side-quest that lacks it (a
// user's own build) — spec D8. It is distinct from the plugin shim's comment
// "side-quest plugin launcher" (no hyphen there), so the two never collide.
const Marker = "side-quest-cli-launcher"

// InstallDirCandidates lists the conventional user bin dirs Install prefers,
// most-preferred first (spec D7).
func InstallDirCandidates(home, xdgBinHome string) []string {
	return []string{
		xdgBinHome,
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, "bin"),
		filepath.Join(home, "go", "bin"),
	}
}

// ChooseInstallDir returns the first candidate already on PATH, or "" when none
// is — pure, so it is testable without touching the real environment.
func ChooseInstallDir(candidates, pathDirs []string) string {
	on := make(map[string]bool, len(pathDirs))
	for _, p := range pathDirs {
		if p != "" {
			on[p] = true
		}
	}
	for _, c := range candidates {
		if c != "" && on[c] {
			return c
		}
	}
	return ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run TestChooseInstallDir`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cli.go internal/cli/cli_test.go
git commit -m "feat: internal/cli core — PATH-dir selection + launcher marker (SQ-0065)" \
  -m "Pure ChooseInstallDir/InstallDirCandidates and the side-quest-cli-launcher marker the subcommands and MCP tools build on." \
  -m "Quest: SQ-0065" \
  -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws"
```

---

### Task 2: The read-only launcher scripts

**Files:**
- Create: `internal/cli/launcher.sh`
- Create: `internal/cli/launcher.cmd`
- Test: `internal/cli/launcher_test.go`

**Interfaces:** none exported to Go yet — these are asset files. Task 3 adds the `//go:embed` that pulls them in.

- [ ] **Step 1: Write the failing test**

Create `internal/cli/launcher_test.go`:

```go
package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// launcherSrc is the POSIX launcher asset, exec'd directly to test its resolution.
func launcherSrc(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs("launcher.sh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("launcher.sh missing: %v", err)
	}
	return p
}

// runLauncher execs launcher.sh with a controlled HOME (and no CLAUDE_PLUGIN_DATA,
// as in a real terminal), returning combined output and the run error.
func runLauncher(t *testing.T, home string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(launcherSrc(t), args...)
	cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func writeExecFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func dataDir(home string) string {
	return filepath.Join(home, ".claude", "plugins", "data", "side-quest-side-quest")
}

// Case 1: newest provisioned binary in the data dir is exec'd.
func TestLauncherExecsProvisionedBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX launcher")
	}
	home := t.TempDir()
	bin := filepath.Join(dataDir(home), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecFile(t, filepath.Join(bin, "side-quest-9.9.9"), "#!/bin/sh\necho PROVISIONED \"$@\"\n")

	out, err := runLauncher(t, home, "serve")
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.HasPrefix(out, "PROVISIONED serve") {
		t.Errorf("got %q, want the provisioned binary", out)
	}
}

// Case 2: data dir present but no binary -> "open a Claude Code session", exit != 0.
func TestLauncherAsksToFinishSetup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX launcher")
	}
	home := t.TempDir()
	if err := os.MkdirAll(dataDir(home), 0o755); err != nil { // dir, but no bin/
		t.Fatal(err)
	}
	out, err := runLauncher(t, home, "list")
	if err == nil {
		t.Fatalf("expected non-zero exit, got success: %s", out)
	}
	if !strings.Contains(out, "open a Claude Code session") {
		t.Errorf("missing finish-setup notice: %s", out)
	}
}

// Case 3: data dir absent (plugin gone), non-interactive -> announce safe-to-remove.
func TestLauncherSelfRemovalNoticeWhenPluginGone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX launcher")
	}
	home := t.TempDir() // no .claude/... at all
	out, err := runLauncher(t, home, "list")
	if err == nil {
		t.Fatalf("expected non-zero exit, got success: %s", out)
	}
	if !strings.Contains(out, "safe to remove") {
		t.Errorf("missing self-removal notice: %s", out)
	}
}

// The Windows launcher asset carries the marker and the same two notices.
func TestWindowsLauncherAssetContent(t *testing.T) {
	b, err := os.ReadFile("launcher.cmd")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{Marker, "open a Claude Code session", "safe to remove"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("launcher.cmd missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestLauncher|TestWindowsLauncherAsset'`
Expected: FAIL — `launcher.sh` / `launcher.cmd` do not exist (`Stat` fatals / `ReadFile` errors).

- [ ] **Step 3: Write the launcher assets**

Create `internal/cli/launcher.sh` (executable content; git tracks the mode when committed with `chmod +x`):

```sh
#!/bin/sh
# side-quest-cli-launcher — read-only resolver for the side-quest binary the Claude
# Code plugin provisions into its data dir. Installed by `side-quest install-cli`;
# removed by `side-quest uninstall-cli` or, once the plugin is gone, by this script
# itself. It NEVER downloads: if the plugin is installed, its MCP server has already
# placed the binary. Resolution:
#   1. newest <data>/bin/side-quest-* present  -> exec it
#   2. data dir present, no binary yet         -> ask the user to open a session
#   3. data dir absent (plugin uninstalled)    -> inert; offer/announce removal
set -eu

DATA="${CLAUDE_PLUGIN_DATA:-$HOME/.claude/plugins/data/side-quest-side-quest}"
BINDIR="$DATA/bin"

SELF_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
SELF="$SELF_DIR/$(basename -- "$0")"

# 1. newest provisioned binary wins.
if [ -d "$BINDIR" ]; then
	newest=
	for f in "$BINDIR"/side-quest-*; do
		[ -x "$f" ] || continue
		if [ -z "$newest" ] || [ "$f" -nt "$newest" ]; then
			newest=$f
		fi
	done
	if [ -n "$newest" ]; then
		exec "$newest" "$@"
	fi
fi

# 2. data dir exists but nothing provisioned yet.
if [ -d "$DATA" ]; then
	echo "side-quest: binary not found — open a Claude Code session to finish setup." >&2
	exit 1
fi

# 3. data dir absent => the plugin is gone. This launcher is inert.
if [ -t 0 ] && [ -t 1 ]; then
	printf 'side-quest: the plugin is no longer installed; this launcher (%s) is inert.\n' "$SELF" >&2
	printf 'Remove it now? [y/N] ' >&2
	read -r ans
	case "$ans" in
	[yY]*)
		rm -f "$SELF" "$SELF.cmd" && echo "side-quest: removed $SELF" >&2
		;;
	*)
		echo "side-quest: left in place — safe to remove: rm $SELF" >&2
		;;
	esac
else
	echo "side-quest: the plugin is gone; this launcher is inert — safe to remove: rm $SELF" >&2
fi
exit 1
```

Create `internal/cli/launcher.cmd`:

```bat
@echo off
setlocal enabledelayedexpansion
rem side-quest-cli-launcher — read-only resolver for the plugin-provisioned binary.
rem Never downloads. Windows note: a running .cmd cannot reliably delete itself, so
rem when the plugin is gone it prints its path as safe to remove (no self-delete).
if defined CLAUDE_PLUGIN_DATA (set "DATA=%CLAUDE_PLUGIN_DATA%") else (set "DATA=%USERPROFILE%\.claude\plugins\data\side-quest-side-quest")
set "BINDIR=%DATA%\bin"

rem 1. newest provisioned binary wins.
set "NEWEST="
if exist "%BINDIR%\" (
  for /f "delims=" %%f in ('dir /b /o-d "%BINDIR%\side-quest-*.exe" 2^>nul') do (
    if not defined NEWEST set "NEWEST=%BINDIR%\%%f"
  )
)
if defined NEWEST (
  "!NEWEST!" %*
  exit /b !errorlevel!
)

rem 2. data dir present but no binary yet.
if exist "%DATA%\" (
  echo side-quest: binary not found — open a Claude Code session to finish setup.>&2
  exit /b 1
)

rem 3. data dir absent => plugin gone; inert launcher, safe to remove.
echo side-quest: the plugin is gone; this launcher is inert — safe to remove: del "%~f0">&2
exit /b 1
```

- [ ] **Step 4: Make `launcher.sh` executable and run the tests**

```bash
chmod +x internal/cli/launcher.sh
go test ./internal/cli/ -run 'TestLauncher|TestWindowsLauncherAsset'
```
Expected: PASS (three POSIX runtime cases + the `.cmd` content check).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/launcher.sh internal/cli/launcher.cmd internal/cli/launcher_test.go
git commit -m "feat: read-only launcher that resolves the plugin's provisioned binary (SQ-0065)" \
  -m "Newest data-dir binary -> exec; data dir empty -> finish-setup notice; data dir gone -> self-removal offer. Never downloads." \
  -m "Quest: SQ-0065" \
  -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws"
```

---

### Task 3: `internal/cli.Install` + the `install-cli` subcommand

**Files:**
- Modify: `internal/cli/cli.go` (add the embeds, `LauncherName`/`LauncherBody`, `Install`, `InstallResult`)
- Create: `cmd/side-quest/installcli.go` (thin `cmdInstallCli` wrapper)
- Modify: `cmd/side-quest/main.go` (wire `install-cli` into `run()`)
- Test: `internal/cli/cli_test.go` (core), `cmd/side-quest/installcli_test.go` (wiring smoke)

**Interfaces:**
- Consumes: `ChooseInstallDir`, `InstallDirCandidates`, `Marker` (Task 1); `launcher.sh`/`launcher.cmd` (Task 2).
- Produces: `func LauncherName() string`; `func LauncherBody() []byte`; `type InstallResult struct { Path, Dir string; OnPath bool }`; `func Install() (InstallResult, error)`; `func cmdInstallCli(args []string) error`.

- [ ] **Step 1: Write the failing test**

Add to `internal/cli/cli_test.go` (add imports `bytes`, `os`, `path/filepath`, `strings` alongside `testing`):

```go
// Install writes a marked launcher into an on-PATH conventional dir and reports it.
func TestInstallWritesMarkedLauncher(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("PATH", dir)

	r, err := Install()
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !r.OnPath {
		t.Errorf("chosen dir %s is on PATH; OnPath should be true", r.Dir)
	}
	b, err := os.ReadFile(filepath.Join(dir, LauncherName()))
	if err != nil {
		t.Fatalf("launcher not written: %v", err)
	}
	if !bytes.Contains(b, []byte(Marker)) {
		t.Error("written launcher is missing the marker")
	}
}

// Install refuses to overwrite a side-quest it did not install (no marker).
func TestInstallRefusesUnmarked(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mine := "#!/bin/sh\necho my own build\n"
	if err := os.WriteFile(filepath.Join(dir, LauncherName()), []byte(mine), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("PATH", dir)

	if _, err := Install(); err == nil {
		t.Fatal("Install should refuse to clobber an unmarked side-quest")
	}
	got, _ := os.ReadFile(filepath.Join(dir, LauncherName()))
	if string(got) != mine {
		t.Errorf("Install clobbered the user's own side-quest:\n%s", got)
	}
}

// With no candidate on PATH, Install falls back to ~/.local/bin and reports off-PATH.
func TestInstallFallbackReportsOffPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("PATH", "/usr/bin:/bin") // none of our candidates is on PATH

	r, err := Install()
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if r.OnPath {
		t.Error("no candidate was on PATH; OnPath should be false")
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "bin", LauncherName())); err != nil {
		t.Errorf("fallback did not write ~/.local/bin/%s: %v", LauncherName(), err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestInstall`
Expected: FAIL — `undefined: LauncherName` / `Install`.

- [ ] **Step 3: Write minimal implementation**

In `internal/cli/cli.go`, change the import block and add the embeds + functions. The imports become:

```go
import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

//go:embed launcher.sh
var launcherSh []byte

//go:embed launcher.cmd
var launcherCmd []byte
```

(Keep `Marker`, `InstallDirCandidates`, `ChooseInstallDir` from Task 1.) Then add:

```go
// LauncherName is the launcher filename for this OS: the native batch file on
// Windows, the extensionless POSIX script elsewhere.
func LauncherName() string {
	if runtime.GOOS == "windows" {
		return "side-quest.cmd"
	}
	return "side-quest"
}

// LauncherBody is the embedded launcher asset for this OS.
func LauncherBody() []byte {
	if runtime.GOOS == "windows" {
		return launcherCmd
	}
	return launcherSh
}

// InstallResult reports where Install placed the launcher.
type InstallResult struct {
	Path   string // absolute path of the written launcher
	Dir    string // the dir it was written into
	OnPath bool   // whether Dir is already on PATH
}

// Install writes the read-only launcher onto the user's PATH (spec D7). It reads
// $HOME, $XDG_BIN_HOME and $PATH from the environment, never clobbers a side-quest
// lacking Marker (D8), and reports where it landed and whether that dir is on PATH.
func Install() (InstallResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return InstallResult{}, err
	}
	candidates := InstallDirCandidates(home, os.Getenv("XDG_BIN_HOME"))
	dir := ChooseInstallDir(candidates, filepath.SplitList(os.Getenv("PATH")))
	onPath := dir != ""
	if dir == "" {
		dir = filepath.Join(home, ".local", "bin") // conventional fallback (D7)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return InstallResult{}, err
	}
	target := filepath.Join(dir, LauncherName())
	if b, err := os.ReadFile(target); err == nil && !bytes.Contains(b, []byte(Marker)) {
		return InstallResult{}, fmt.Errorf("refusing to overwrite %s — not a launcher we installed (no %q marker)", target, Marker)
	}
	if err := os.WriteFile(target, LauncherBody(), 0o755); err != nil {
		return InstallResult{}, err
	}
	return InstallResult{Path: target, Dir: dir, OnPath: onPath}, nil
}
```

Create `cmd/side-quest/installcli.go`:

```go
package main

import (
	"fmt"

	"github.com/sharkusk/side-quest/internal/cli"
)

// cmdInstallCli writes the read-only launcher onto the user's PATH so a plugin
// user can run side-quest from the terminal (SQ-0065). The mechanism lives in
// internal/cli (shared with the MCP cli_install tool); this wrapper just formats
// the result for the terminal.
func cmdInstallCli(args []string) error {
	if len(args) != 0 {
		return &usageErr{"install-cli takes no arguments"}
	}
	r, err := cli.Install()
	if err != nil {
		return err
	}
	fmt.Printf("side-quest: installed the CLI launcher at %s\n", r.Path)
	if !r.OnPath {
		fmt.Printf("  Add %s to your PATH to run `side-quest` from the terminal.\n", r.Dir)
	}
	return nil
}
```

In `cmd/side-quest/main.go`, add to the `run()` switch (next to the other cases):

```go
	case "install-cli":
		return cmdInstallCli(args)
```

- [ ] **Step 4: Write the wiring smoke test**

Create `cmd/side-quest/installcli_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharkusk/side-quest/internal/cli"
)

// install-cli is wired into run() and writes a marked launcher end-to-end.
func TestInstallCliCommand(t *testing.T) {
	bin := buildBinary(t)
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("PATH", dir)

	out, code := runBin(t, bin, home, "install-cli")
	if code != 0 {
		t.Fatalf("install-cli exit=%d out=%q", code, out)
	}
	b, err := os.ReadFile(filepath.Join(dir, cli.LauncherName()))
	if err != nil {
		t.Fatalf("launcher not written: %v", err)
	}
	if !strings.Contains(string(b), cli.Marker) {
		t.Error("written launcher is missing the marker")
	}
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/cli/ ./cmd/side-quest/ -run 'TestInstall|TestInstallCliCommand'`
Expected: PASS (core writes/refuses/fallback; the subcommand writes end-to-end).

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cli.go internal/cli/cli_test.go cmd/side-quest/installcli.go cmd/side-quest/installcli_test.go cmd/side-quest/main.go
git commit -m "feat: side-quest install-cli puts the launcher on PATH (SQ-0065)" \
  -m "internal/cli.Install chooses the first conventional on-PATH bin dir (else ~/.local/bin + a notice) and never clobbers an unmarked side-quest; the subcommand is a thin wrapper the MCP cli_install tool will share." \
  -m "Quest: SQ-0065" \
  -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws"
```

---

### Task 4: `internal/cli.Uninstall` + `Status` + the `uninstall-cli` subcommand

**Files:**
- Modify: `internal/cli/cli.go` (add `launcherDirs`, `Uninstall`, `UninstallResult`, `Status`, `StatusResult`)
- Modify: `cmd/side-quest/installcli.go` (add `cmdUninstallCli`)
- Modify: `cmd/side-quest/main.go` (wire `uninstall-cli` into `run()`)
- Test: `internal/cli/cli_test.go` (core), `cmd/side-quest/installcli_test.go` (wiring smoke)

**Interfaces:**
- Consumes: `Marker`, `InstallDirCandidates` (Task 1).
- Produces: `type UninstallResult struct { Removed, Refused []string }`; `func Uninstall() (UninstallResult, error)`; `type StatusResult struct { Installed bool; Path string }`; `func Status() StatusResult`; `func cmdUninstallCli(args []string) error`. `Status`/`Uninstall` scan the union of `$PATH` and the candidate dirs.

- [ ] **Step 1: Write the failing test**

Add to `internal/cli/cli_test.go`:

```go
// Uninstall removes a marked launcher and leaves an unmarked side-quest untouched;
// it scans candidate dirs even when they are not on $PATH (the MCP-server case).
func TestUninstallRemovesOnlyMarked(t *testing.T) {
	home := t.TempDir()
	// The marked launcher lives in ~/.local/bin, which we deliberately keep OFF
	// $PATH to prove Uninstall scans the candidate dirs too (spec D7).
	local := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "side-quest"),
		[]byte("#!/bin/sh\n# "+Marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ownDir := t.TempDir()
	own := filepath.Join(ownDir, "side-quest")
	if err := os.WriteFile(own, []byte("#!/bin/sh\necho mine\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("PATH", ownDir) // ~/.local/bin intentionally NOT on PATH

	r, err := Uninstall()
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if len(r.Removed) != 1 || !strings.HasSuffix(r.Removed[0], filepath.Join(".local", "bin", "side-quest")) {
		t.Errorf("expected the marked launcher removed, got %v", r.Removed)
	}
	if _, err := os.Stat(filepath.Join(local, "side-quest")); !os.IsNotExist(err) {
		t.Errorf("marked launcher not removed (err=%v)", err)
	}
	if _, err := os.Stat(own); err != nil {
		t.Errorf("Uninstall removed the user's own side-quest: %v", err)
	}
}

// Uninstall reports nothing removed and nothing refused when no launcher exists.
func TestUninstallEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("PATH", t.TempDir())
	r, err := Uninstall()
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if len(r.Removed) != 0 || len(r.Refused) != 0 {
		t.Errorf("expected empty result, got removed=%v refused=%v", r.Removed, r.Refused)
	}
}

// Status finds a marked launcher in a candidate dir that is off $PATH.
func TestStatusFindsMarkedOffPath(t *testing.T) {
	home := t.TempDir()
	local := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "side-quest"),
		[]byte("#!/bin/sh\n# "+Marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("PATH", "/usr/bin:/bin") // launcher dir not on PATH

	st := Status()
	if !st.Installed || !strings.HasSuffix(st.Path, filepath.Join(".local", "bin", "side-quest")) {
		t.Errorf("Status = %+v, want installed at ~/.local/bin/side-quest", st)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestUninstall|TestStatus'`
Expected: FAIL — `undefined: Uninstall` / `Status`.

- [ ] **Step 3: Write minimal implementation**

Add to `internal/cli/cli.go`:

```go
// launcherDirs is the deduped set of dirs a launcher might live in: everything on
// $PATH plus the conventional install candidates (D7). Scanning the union makes
// Status/Uninstall robust even when the caller's $PATH is the GUI PATH rather than
// the user's login shell (the MCP server sees that — spec D7).
func launcherDirs() []string {
	home, _ := os.UserHomeDir()
	all := append(filepath.SplitList(os.Getenv("PATH")), InstallDirCandidates(home, os.Getenv("XDG_BIN_HOME"))...)
	seen := make(map[string]bool, len(all))
	out := make([]string, 0, len(all))
	for _, d := range all {
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

// UninstallResult reports what Uninstall did.
type UninstallResult struct {
	Removed []string // launcher paths removed
	Refused []string // paths left in place (present but unmarked)
}

// Uninstall removes the marked launcher(s) while the plugin is still installed
// (the plugin-gone case is the launcher's own self-removal — spec D4.3/D8). It
// never removes a side-quest lacking Marker.
func Uninstall() (UninstallResult, error) {
	var res UninstallResult
	for _, dir := range launcherDirs() {
		for _, name := range []string{"side-quest", "side-quest.cmd"} {
			p := filepath.Join(dir, name)
			b, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			if !bytes.Contains(b, []byte(Marker)) {
				res.Refused = append(res.Refused, p)
				continue
			}
			if err := os.Remove(p); err != nil {
				return res, err
			}
			res.Removed = append(res.Removed, p)
		}
	}
	return res, nil
}

// StatusResult reports whether a marked launcher is present.
type StatusResult struct {
	Installed bool
	Path      string // the first marked launcher found, if any
}

// Status scans for a marked launcher (spec D5's cli_status). Like Uninstall it
// looks in the union of $PATH and the candidate dirs.
func Status() StatusResult {
	for _, dir := range launcherDirs() {
		for _, name := range []string{"side-quest", "side-quest.cmd"} {
			p := filepath.Join(dir, name)
			b, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			if bytes.Contains(b, []byte(Marker)) {
				return StatusResult{Installed: true, Path: p}
			}
		}
	}
	return StatusResult{}
}
```

Add `cmdUninstallCli` to `cmd/side-quest/installcli.go`:

```go
// cmdUninstallCli removes the marked launcher(s) while the plugin is still
// installed (the plugin-gone case is handled by the launcher's own self-removal
// — SQ-0065, D8). The mechanism is internal/cli.Uninstall; this wrapper formats.
func cmdUninstallCli(args []string) error {
	if len(args) != 0 {
		return &usageErr{"uninstall-cli takes no arguments"}
	}
	r, err := cli.Uninstall()
	if err != nil {
		return err
	}
	for _, p := range r.Removed {
		fmt.Printf("side-quest: removed the CLI launcher at %s\n", p)
	}
	for _, p := range r.Refused {
		fmt.Printf("side-quest: left %s in place — not one of our launchers (no %q marker).\n", p, cli.Marker)
	}
	if len(r.Removed) == 0 && len(r.Refused) == 0 {
		fmt.Println("side-quest: no CLI launcher found on PATH — nothing to remove.")
	}
	return nil
}
```

In `cmd/side-quest/main.go`, add to the `run()` switch:

```go
	case "uninstall-cli":
		return cmdUninstallCli(args)
```

- [ ] **Step 4: Write the wiring smoke test**

Add to `cmd/side-quest/installcli_test.go`:

```go
// uninstall-cli is wired into run() and removes a marked launcher end-to-end.
func TestUninstallCliCommand(t *testing.T) {
	bin := buildBinary(t)
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(dir, "side-quest")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\n# "+cli.Marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("PATH", dir)

	out, code := runBin(t, bin, home, "uninstall-cli")
	if code != 0 {
		t.Fatalf("uninstall-cli exit=%d out=%q", code, out)
	}
	if _, err := os.Stat(launcher); !os.IsNotExist(err) {
		t.Errorf("uninstall-cli did not remove the marked launcher (err=%v)", err)
	}
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/cli/ ./cmd/side-quest/ -run 'TestUninstall|TestStatus|TestUninstallCliCommand'`
Expected: PASS (removes marked / leaves unmarked / empty; Status finds off-PATH; the subcommand removes end-to-end).

- [ ] **Step 6: Commit**

```bash
git add internal/cli/cli.go internal/cli/cli_test.go cmd/side-quest/installcli.go cmd/side-quest/installcli_test.go cmd/side-quest/main.go
git commit -m "feat: side-quest uninstall-cli + internal/cli Status (SQ-0065)" \
  -m "Uninstall/Status scan the union of PATH and the candidate dirs (so they work from the MCP server's PATH too); remove only files carrying the side-quest-cli-launcher marker; the subcommand is a thin wrapper the MCP cli_uninstall tool will share." \
  -m "Quest: SQ-0065" \
  -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws"
```

---

### Task 5: Usage entries + architecture doc

**Files:**
- Modify: `cmd/side-quest/main.go` (usage "Advanced" block — the SQ-0064 regrouped version)
- Modify: `cmd/side-quest/main_test.go` (assert the two commands are listed)
- Modify: `docs/architecture.md` (packaging section)

- [ ] **Step 1: Write the failing test**

Add to `cmd/side-quest/main_test.go`:

```go
// install-cli / uninstall-cli are discoverable in the usage text.
func TestUsageListsCliCommands(t *testing.T) {
	bin := buildBinary(t)
	out, _ := runBin(t, bin, t.TempDir())
	for _, want := range []string{"install-cli", "uninstall-cli"} {
		if !strings.Contains(out, want) {
			t.Errorf("usage missing %q:\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/side-quest/ -run TestUsageListsCliCommands`
Expected: FAIL — neither command is in the usage text yet.

- [ ] **Step 3: Add the two lines to the Advanced block**

In `cmd/side-quest/main.go`, in the `usage` const's `Advanced` section (added by SQ-0064), insert after the `install-hooks` line:

```
  install-cli                     put a side-quest launcher on your PATH (plugin users)
  uninstall-cli                   remove the side-quest launcher this CLI installed
```

- [ ] **Step 4: Run the tests**

Run: `go test ./cmd/side-quest/ -run 'TestUsage'`
Expected: PASS (`TestUsageListsCliCommands`, `TestUsageDemotesInitAndInstallHooks`, `TestUsageListsEnumValues` all green).

- [ ] **Step 5: Document in architecture.md**

In `docs/architecture.md`, append a bullet at the end of the "Packaging & distribution" list (after the Versioning bullet):

```markdown
- **Terminal CLI for plugin users** — `side-quest install-cli` writes a small,
  **read-only** launcher (marked `side-quest-cli-launcher`) into the first
  conventional on-PATH user-bin dir (`$XDG_BIN_HOME`, `~/.local/bin`, `~/bin`,
  `~/go/bin`; else `~/.local/bin` with a PATH notice). The launcher resolves the
  newest `~/.claude/plugins/data/side-quest-side-quest/bin/side-quest-*` and execs
  it — it never downloads (the plugin's MCP server provisions the binary on
  startup). When the plugin's data dir is gone it announces itself as safe to
  remove (offers to self-delete when interactive; the `.cmd` never self-deletes).
  The mechanism lives in `internal/cli` (`Install`/`Uninstall`/`Status`), shared by
  the `install-cli`/`uninstall-cli` subcommands and the MCP `cli_*` tools (SQ-0066);
  `uninstall-cli` removes the marked launcher while the plugin is still installed,
  and neither command touches a `side-quest` lacking the marker.
```

- [ ] **Step 6: Full-suite check + commit**

```bash
go test ./internal/cli/ ./cmd/side-quest/
git add cmd/side-quest/main.go cmd/side-quest/main_test.go docs/architecture.md
git commit -m "docs: list install-cli/uninstall-cli and document the launcher (SQ-0065)" \
  -m "Quest: SQ-0065" \
  -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws"
```

---

## Out of scope (this plan)

- The MCP `cli_*` tools (`cli_status`/`cli_install`/`cli_uninstall`/`cli_dismiss`) and the plugin guidance that drives the first-run offer — **SQ-0066**. This plan produces the `internal/cli` core they wrap.
- Provisioning the binary into the data dir — that is the plugin's MCP server on startup (the existing `bin/side-quest` download shim); no code here or in SQ-0066 changes it.
- A Windows runtime exec-test for `launcher.cmd` (mirroring `internal/packaging/launcher_windows_test.go`). This plan ships the `.cmd` and asserts its content cross-platform; a full `cmd /c` runtime test can be added under `//go:build windows` following the existing harness. Flagged (spec O4).

## Self-Review

- **Spec coverage:** D1 (`install-cli`/`uninstall-cli` pair, shared `internal/cli` core) → Tasks 1-4. D2 (resolve via data dir) → launcher `DATA=` line (Task 2). D3 (read-only; never writes the data dir) → launcher has no write/download path. D4 + D4.3 (self-healing, self-removal, Windows caveat) → Task 2 cases 2-3 + `.cmd`. D7 (placement; union scan for the GUI-PATH case) → Tasks 1, 3, 4. D8 (marker, refuse unmarked, two removal paths) → marker (Task 1), refuse (Task 3), `Uninstall` + launcher self-removal (Tasks 4, 2).
- **Type consistency:** `Marker`, `InstallDirCandidates`, `ChooseInstallDir`, `LauncherName`, `LauncherBody`, `launcherSh`, `launcherCmd`, `Install`/`InstallResult`, `Uninstall`/`UninstallResult`, `Status`/`StatusResult`, `launcherDirs`, `cmdInstallCli`, `cmdUninstallCli` are each defined once and referenced consistently across `internal/cli/cli.go`, its tests, `cmd/side-quest/installcli.go`, and `main.go`.
- **Placeholder scan:** none — every step carries exact code, paths, and commands.
- **Ordering:** Task 1 defines pure helpers with no `//go:embed` (build stays green); Task 3 introduces the embeds once `launcher.sh`/`launcher.cmd` exist (Task 2); Tasks 3-4 add the `cmd` wrappers that import `internal/cli`. No task references a symbol a later task defines.
