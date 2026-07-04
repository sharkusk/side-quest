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

// Both guidance docs must tell the agent how to self-heal an unset-up repo:
// init + install-hooks have no MCP tool, so the agent runs them in the shell.
func TestFirstRunGuidancePresent(t *testing.T) {
	for _, f := range []string{"AGENTS.md", "skills/side-quest/SKILL.md"} {
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

// TestDevMakefileDogfoodsHead (SQ-0025): the Makefile carries the one-command
// dogfood loop — rebuild side-quest from HEAD into the PATH binary the MCP server
// and git hooks resolve, repoint the hooks at it, and link the /sq command — so
// dogfooding side-quest on itself needs no manual reinstall dance.
func TestDevMakefileDogfoodsHead(t *testing.T) {
	mk := string(repoFile(t, "Makefile"))
	for _, want := range []string{
		"go install ./cmd/side-quest", // HEAD -> $GOBIN, what bare `side-quest` resolves to
		"install-hooks",               // repoint the git-hook shims at the fresh binary
		"commands/sq.md",              // link the /sq command into .claude/commands
	} {
		if !strings.Contains(mk, want) {
			t.Errorf("Makefile dogfood workflow missing %q", want)
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
