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
	if strings.Contains(r, "Dungeon Crawler") {
		t.Error("README must not mention Dungeon Crawler (voice attribution moved to docs/architecture.md)")
	}
	if strings.Contains(r, "Credits & permissions") {
		t.Error("README must not have a Credits & permissions heading (attribution moved to docs/architecture.md)")
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
	if !strings.Contains(a, "Dungeon Crawler") {
		t.Error("architecture.md must contain the Dungeon Crawler voice attribution")
	}
	if !strings.Contains(a, "Credits & permissions") {
		t.Error("architecture.md must have a Credits & permissions heading")
	}
}
