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

// Claude spawns a plugin's MCP command with no shell and honors no per-OS field, so on a
// native-installer Windows box (no `node`, so nothing to run a launch.js) the only thing
// it can launch is a real executable by absolute path. The command therefore points at
// the provisioned native binary in the plugin data dir — never `node`/`npx`, which
// ENOENT when the interpreter is absent (SQ-0089 supersedes the SQ-0081 node launcher).
func TestPluginRegistersMCPServerViaNativeBinary(t *testing.T) {
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
	const want = "${CLAUDE_PLUGIN_DATA}/bin/side-quest.exe"
	if sq.Command != want {
		t.Errorf("plugin.json mcp command = %q, want %q (SQ-0089)", sq.Command, want)
	}
	for _, bad := range []string{"node", "npx", "npm", "python", "sh", "bash"} {
		if sq.Command == bad {
			t.Errorf("plugin.json mcp command must not be a bare interpreter (%q) — it isn't guaranteed on PATH on a native install (SQ-0089)", bad)
		}
	}
	if len(sq.Args) != 1 || sq.Args[0] != "serve" {
		t.Errorf("plugin.json mcp args = %v, want [\"serve\"]", sq.Args)
	}
}

// The MCP command names a binary that exists only after provisioning, so the plugin must
// place it ahead of the spawn via a SessionStart hook — with BOTH per-OS arms (the POSIX
// scripts/provision.sh and the Windows scripts/provision.ps1), each self-selecting by
// which interpreter exists, so a plugin install provisions on either OS (SQ-0089).
func TestPluginProvisionsViaSessionStartHook(t *testing.T) {
	var m struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(repoFile(t, ".claude-plugin/plugin.json"), &m); err != nil {
		t.Fatalf("plugin.json invalid: %v", err)
	}
	ss, ok := m.Hooks["SessionStart"]
	if !ok || len(ss) == 0 {
		t.Fatal("plugin.json has no SessionStart hook — nothing provisions the binary the MCP command spawns (SQ-0089)")
	}
	var cmds []string
	for _, group := range ss {
		for _, h := range group.Hooks {
			cmds = append(cmds, h.Command)
		}
	}
	joined := strings.Join(cmds, "\n")
	for _, want := range []string{"scripts/provision.sh", "scripts/provision.ps1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("SessionStart hooks do not run %s; a plugin install won't provision on that OS (SQ-0089)", want)
		}
	}
	// The scripts the hook names must actually ship.
	sh, err := os.Stat("../../scripts/provision.sh")
	if err != nil {
		t.Fatalf("scripts/provision.sh missing: %v", err)
	}
	if runtime.GOOS != "windows" && sh.Mode()&0o111 == 0 {
		t.Errorf("scripts/provision.sh is not executable (mode %v)", sh.Mode())
	}
	if _, err := os.Stat("../../scripts/provision.ps1"); err != nil {
		t.Errorf("scripts/provision.ps1 missing: %v", err)
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
