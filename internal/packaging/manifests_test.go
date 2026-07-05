// Package packaging holds tests that validate the repo's distribution artifacts
// (plugin manifests, VERSION, LICENSE, launcher). It has no non-test code; the
// tests read repo-root files via paths relative to this directory.
package packaging

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sharkusk/side-quest/internal/guidance"
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

// The plugin registers its MCP server in plugin.json via `node` running the bundled
// bin/launch.js — the one command Claude's shell-less spawn can launch on every OS.
// An extensionless ${CLAUDE_PLUGIN_ROOT}/bin/side-quest ENOENTs on the Windows Node
// spawn (no PATHEXT), and mcpServers has no per-OS field, so a node dispatcher is how
// one declaration works cross-platform (SQ-0081).
func TestPluginRegistersMCPServerViaNodeLauncher(t *testing.T) {
	var m struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(repoFile(t, ".claude-plugin/plugin.json"), &m); err != nil {
		t.Fatalf("plugin.json invalid: %v", err)
	}
	sq, ok := m.MCPServers["side-quest"]
	if !ok {
		t.Fatal("plugin.json missing mcpServers.side-quest — an installed plugin would register no MCP server")
	}
	if sq.Command != "node" {
		t.Errorf("plugin.json mcp command = %q, want \"node\" (extensionless shim paths ENOENT on the Windows Node spawn — SQ-0081)", sq.Command)
	}
	want := []string{"${CLAUDE_PLUGIN_ROOT}/bin/launch.js", "serve"}
	if len(sq.Args) != 2 || sq.Args[0] != want[0] || sq.Args[1] != want[1] {
		t.Errorf("plugin.json mcp args = %v, want %v", sq.Args, want)
	}
}

// The launcher plugin.json runs (args[0], a ${CLAUDE_PLUGIN_ROOT}-relative path) must
// ship, and it must dispatch to BOTH per-OS provisioning shims so a plugin install
// works on either OS: the POSIX shell shim (executable, shebang) and the Windows
// .cmd (which Node can't spawn without a shell). Otherwise the MCP server has nothing
// to launch on one platform (SQ-0081).
func TestPluginLauncherAndShimsShip(t *testing.T) {
	var m struct {
		MCPServers map[string]struct {
			Args []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(repoFile(t, ".claude-plugin/plugin.json"), &m); err != nil {
		t.Fatalf("plugin.json invalid: %v", err)
	}
	args := m.MCPServers["side-quest"].Args
	if len(args) == 0 {
		t.Fatal("plugin.json mcp args empty; expected the launcher path first")
	}
	const prefix = "${CLAUDE_PLUGIN_ROOT}/"
	launcher := strings.TrimPrefix(args[0], prefix) // bin/launch.js
	if launcher == args[0] {
		t.Fatalf("launcher arg %q must be ${CLAUDE_PLUGIN_ROOT}-relative", args[0])
	}
	if _, err := os.Stat("../../" + launcher); err != nil {
		t.Fatalf("plugin.json launches %q, which is absent from the plugin: %v", launcher, err)
	}
	js := string(repoFile(t, launcher))
	if !strings.Contains(js, "'side-quest.cmd'") {
		t.Error("bin/launch.js does not dispatch to the Windows shim (side-quest.cmd)")
	}
	if !strings.Contains(js, "'side-quest'") {
		t.Error("bin/launch.js does not dispatch to the POSIX shim (side-quest)")
	}
	info, err := os.Stat("../../bin/side-quest")
	if err != nil {
		t.Fatalf("POSIX shim bin/side-quest missing: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		t.Errorf("bin/side-quest is not executable (mode %v)", info.Mode())
	}
	if _, err := os.Stat("../../bin/side-quest.cmd"); err != nil {
		t.Errorf("Windows shim bin/side-quest.cmd missing: %v", err)
	}
}

// The plugin must NOT ship a root .mcp.json: it would register a second, bare-command
// "side-quest" server that isn't on the plugin's MCP-spawn PATH (ENOENT), duplicating
// the plugin.json registration. The repo's .mcp.json is git-ignored dogfooding config
// — the plugin registers via plugin.json, and a non-plugin user's .mcp.json is written
// by `side-quest onboard`. Guard that .mcp.json is never git-tracked (SQ-0080).
func TestRepoDoesNotShipMcpJson(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "-C", root, "ls-files", ".mcp.json").Output()
	if err != nil {
		t.Skipf("git ls-files unavailable (%v) — cannot check tracked state", err)
	}
	if tracked := strings.TrimSpace(string(out)); tracked != "" {
		t.Errorf("%s is git-tracked — it must stay git-ignored so a plugin install never sees a stray PATH-resolved server (SQ-0080)", tracked)
	}
}

// Windows batch expands %VAR% inside a parenthesized block at PARSE time, so an ASSET
// `set` INSIDE the download `if (...)` block and used there via %ASSET% reads empty —
// the download URL loses its filename and provisioning silently fails (the empty data
// dir / -32000 class). Guard that ASSET is defined before the block that consumes it
// (SQ-0083).
func TestWindowsCmdSetsAssetBeforeDownloadBlock(t *testing.T) {
	cmd := string(repoFile(t, "bin/side-quest.cmd"))
	setIdx := strings.Index(cmd, `set "ASSET=`)
	blockIdx := strings.Index(cmd, `if not "%VERSION%"=="dev"`)
	if setIdx < 0 {
		t.Fatal("bin/side-quest.cmd no longer sets ASSET")
	}
	if blockIdx < 0 {
		t.Fatal("bin/side-quest.cmd no longer has the VERSION!=dev download block")
	}
	if setIdx > blockIdx {
		t.Error("bin/side-quest.cmd sets ASSET inside/after the download block — %ASSET% expands empty at parse time and the download URL loses its filename (SQ-0083)")
	}
}

// End-to-end on whatever OS runs the test: bin/launch.js must dispatch to the
// per-OS shim (the POSIX shell shim, or the .cmd on Windows) and pass args through.
// This is exactly what Claude relies on when it Node-spawns the plugin MCP command,
// and it is the behavior that lets one `node` command work on every OS (SQ-0081).
func TestPluginLauncherRunsPerOSShim(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not on PATH; skipping launcher behavior test")
	}
	dir := t.TempDir()
	src, err := os.ReadFile("../../bin/launch.js")
	if err != nil {
		t.Fatalf("read launch.js: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "launch.js"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	// Plant an OS-appropriate fake shim next to launch.js that echoes a marker + args.
	if runtime.GOOS == "windows" {
		if err := os.WriteFile(filepath.Join(dir, "side-quest.cmd"), []byte("@echo off\r\necho SHIM %*\r\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := os.WriteFile(filepath.Join(dir, "side-quest"), []byte("#!/bin/sh\necho SHIM \"$@\"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command(node, filepath.Join(dir, "launch.js"), "serve").CombinedOutput()
	if err != nil {
		t.Fatalf("run launcher: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "SHIM serve") {
		t.Errorf("launcher did not dispatch to the per-OS shim; got %q", out)
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

func TestSqCommandDrivesQuestNew(t *testing.T) {
	c := string(repoFile(t, "commands/sq.md"))
	if !strings.Contains(c, "quest_new") {
		t.Error("commands/sq.md must instruct the agent to call quest_new")
	}
	if !strings.Contains(c, "$ARGUMENTS") {
		t.Error("commands/sq.md must consume $ARGUMENTS")
	}
}

// TestSqCommandDisplayName (SQ-0060): the /sq command sets its menu display label
// to "side-quest" via frontmatter `name`, while the short `/sq` invocation is
// preserved (the trigger comes from the filename, the label from `name`).
func TestSqCommandDisplayName(t *testing.T) {
	c := string(repoFile(t, "commands/sq.md"))
	if !strings.Contains(c, "name: side-quest") {
		t.Error("commands/sq.md frontmatter must set `name: side-quest` for its menu display label")
	}
}

func TestAgentsDocPointsToSkill(t *testing.T) {
	a := string(repoFile(t, "internal/guidance/agents.md"))
	if !strings.Contains(a, "skills/side-quest/SKILL.md") {
		t.Error("AGENTS.md must reference skills/side-quest/SKILL.md")
	}
	for _, want := range []string{"Quest:", "Completes:", "current"} {
		if !strings.Contains(a, want) {
			t.Errorf("AGENTS.md missing mention of %q", want)
		}
	}
}

// Both guidance docs must tell the agent how to self-heal an unset-up repo:
// init + install-hooks have no MCP tool, so the agent runs them in the shell.
func TestFirstRunGuidancePresent(t *testing.T) {
	for _, f := range []string{"internal/guidance/agents.md", "skills/side-quest/SKILL.md"} {
		doc := string(repoFile(t, f))
		if !strings.Contains(doc, "side-quest install-hooks") {
			t.Errorf("%s must tell the agent to run `side-quest install-hooks` for first-run setup", f)
		}
	}
}

func TestReadmeReframedAndToneRemoved(t *testing.T) {
	r := string(repoFile(t, "README.md"))
	if strings.Contains(r, "\n## Tone\n") {
		t.Error("README must not have a Tone section (voice is kept a surprise)")
	}
	if strings.Contains(r, "Dungeon Crawler") {
		t.Error("README must not mention Dungeon Crawler (voice attribution moved to docs/architecture.md)")
	}
	if strings.Contains(r, "Credits & permissions") {
		t.Error("README must not have a Credits & permissions heading (attribution moved to docs/architecture.md)")
	}

	// Install instructions were split out of the README into docs/install.md;
	// the go install path and Go floor invariants live there now.
	inst := string(repoFile(t, "docs/install.md"))
	if !strings.Contains(inst, "go install github.com/sharkusk/side-quest/cmd/side-quest@latest") {
		t.Error("docs/install.md missing the corrected go install path")
	}
	if strings.Contains(inst, "go install github.com/sharkusk/side-quest@latest") {
		t.Error("docs/install.md still has the broken root-path go install command")
	}
	if !strings.Contains(inst, "1.25") {
		t.Error("docs/install.md must state the Go >=1.25 floor")
	}
}

// The reinforcement surfaces must contain the canonical core verbatim (single
// source of truth), and /sq must reflect the auto-classify rule — SQ-0051.
func TestGuidanceSurfacesContainCore(t *testing.T) {
	for _, f := range []string{"skills/side-quest/SKILL.md"} {
		if !strings.Contains(string(repoFile(t, f)), guidance.Core) {
			t.Errorf("%s must contain guidance.Core verbatim (single source of truth)", f)
		}
	}
	sq := string(repoFile(t, "commands/sq.md"))
	if strings.Contains(sq, "unless the user stated them") {
		t.Error("commands/sq.md still carries the old don't-set-type/priority rule; align to auto-classify-when-obvious")
	}
}

// TestDevMakefileDogfoodsHead (SQ-0025): the Makefile carries the one-command
// dogfood loop — rebuild side-quest from HEAD into the PATH binary the MCP server
// and git hooks resolve, repoint the hooks at it, and link the /sq command — so
// dogfooding side-quest on itself needs no manual reinstall dance.
func TestDevMakefileDogfoodsHead(t *testing.T) {
	mk := string(repoFile(t, "Makefile"))
	for _, want := range []string{
		"go install",       // HEAD -> $GOBIN, what bare `side-quest` resolves to
		"./cmd/side-quest", // the install target
		"-X main.version=", // dev builds self-stamp the git-describe version (SQ-0050)
		"install-hooks",    // repoint the git-hook shims at the fresh binary
		"commands/sq.md",   // link the /sq command into .claude/commands
	} {
		if !strings.Contains(mk, want) {
			t.Errorf("Makefile dogfood workflow missing %q", want)
		}
	}
}

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

func TestArchitectureHasToneAndPackaging(t *testing.T) {
	a := string(repoFile(t, "docs/architecture.md"))
	if !strings.Contains(a, "SIDE_QUEST_TONE") {
		t.Error("architecture.md should document the SIDE_QUEST_TONE override")
	}
	if !strings.Contains(a, "Packaging & distribution") {
		t.Error("architecture.md should have the packaging section")
	}
	if !strings.Contains(a, "launcher") {
		t.Error("architecture.md should document the plugin binary launcher")
	}
	if !strings.Contains(a, "Dungeon Crawler") {
		t.Error("architecture.md must contain the Dungeon Crawler voice attribution")
	}
	if !strings.Contains(a, "Credits & permissions") {
		t.Error("architecture.md must have a Credits & permissions heading")
	}
}
