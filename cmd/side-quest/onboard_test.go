package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharkusk/side-quest/internal/gitcmd"
)

// agents-md prints the canonical, paste-ready agent guidance. It needs no repo
// and no init — the content is embedded in the binary.
func TestAgentsMdPrintsGuidance(t *testing.T) {
	bin := buildBinary(t)
	out, code := runBin(t, bin, t.TempDir(), "agents-md")
	if code != 0 {
		t.Fatalf("agents-md exit=%d out=%q", code, out)
	}
	for _, want := range []string{"Quest: SQ-", "Completes: SQ-", "Quest: none", "quest_new"} {
		if !strings.Contains(out, want) {
			t.Errorf("agents-md output missing %q", want)
		}
	}
}

// onboard is the turnkey per-repo setup: init + install-hooks + write .mcp.json
// + print the guidance. A fresh repo ends up fully wired.
func TestOnboardSetsUpRepo(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)
	out, code := runBin(t, bin, dir, "onboard")
	if code != 0 {
		t.Fatalf("onboard exit=%d out=%q", code, out)
	}
	g := gitcmd.New(dir)
	if ref, _ := g.Run("for-each-ref", "--format=%(objectname)", "refs/side-quest/quests"); strings.TrimSpace(ref) == "" {
		t.Error("onboard did not create the quest ref")
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "hooks", "post-commit")); err != nil {
		t.Errorf("onboard did not install hooks: %v", err)
	}
	mcp, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatalf("onboard did not write .mcp.json: %v", err)
	}
	if !strings.Contains(string(mcp), `"side-quest"`) || !strings.Contains(string(mcp), `"serve"`) {
		t.Errorf(".mcp.json missing side-quest serve: %s", mcp)
	}
	if !strings.Contains(out, "Quest: SQ-") {
		t.Error("onboard did not print the agent guidance")
	}
	if !strings.Contains(strings.ToLower(out), "restart") {
		t.Error("onboard did not print a restart reminder")
	}
}

// Re-running onboard is safe: init reports already-initialized without failing,
// and hooks compose idempotently.
func TestOnboardIsIdempotent(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)
	if _, code := runBin(t, bin, dir, "onboard"); code != 0 {
		t.Fatalf("first onboard exit=%d", code)
	}
	if out, code := runBin(t, bin, dir, "onboard"); code != 0 {
		t.Fatalf("second onboard exit=%d out=%q", code, out)
	}
}

// onboard writes .mcp.json only when absent — an existing one is never clobbered.
func TestOnboardPreservesExistingMcpJson(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)
	custom := `{"mcpServers":{"other":{"command":"x"}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, code := runBin(t, bin, dir, "onboard"); code != 0 {
		t.Fatalf("onboard exit=%d", code)
	}
	got, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if string(got) != custom {
		t.Errorf("onboard clobbered an existing .mcp.json: %s", got)
	}
}
