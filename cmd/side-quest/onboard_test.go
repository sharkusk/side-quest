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

// TestAgentsBlockMarkedAndStamped (SQ-0047): the emitted guidance is wrapped in
// side-quest markers and carries a version stamp, so a merged copy is findable
// and refreshable later — the guidance body still shows through.
func TestAgentsBlockMarkedAndStamped(t *testing.T) {
	block := agentsBlock("9.9.9")
	for _, want := range []string{agentsMarker, agentsEndMarker, "side-quest-version: 9.9.9", "Quest: SQ-"} {
		if !strings.Contains(block, want) {
			t.Errorf("agentsBlock missing %q\n%s", want, block)
		}
	}
	if got := parseAgentsVersion(block); got != "9.9.9" {
		t.Errorf("parseAgentsVersion = %q, want 9.9.9", got)
	}
}

// TestInstallAgentsGuidanceLifecycle (SQ-0047): installing the guidance block
// creates AGENTS.md when absent, appends to an existing file without disturbing
// its content, no-ops when already current, and refreshes its own block in place
// on a version change — always leaving exactly one block.
func TestInstallAgentsGuidanceLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")

	// create
	if o, _, err := installAgentsGuidance(path, "1.0.0"); err != nil || o != blockCreated {
		t.Fatalf("create: outcome=%v err=%v", o, err)
	}
	if n := strings.Count(readFileStr(t, path), agentsMarker); n != 1 {
		t.Fatalf("create should leave one block, got %d", n)
	}

	// unchanged (same version, same content)
	if o, _, err := installAgentsGuidance(path, "1.0.0"); err != nil || o != blockUnchanged {
		t.Fatalf("unchanged: outcome=%v err=%v", o, err)
	}

	// refresh in place on version bump — still exactly one block, new version stamped
	o, prev, err := installAgentsGuidance(path, "2.0.0")
	if err != nil || o != blockRefreshed || prev != "1.0.0" {
		t.Fatalf("refresh: outcome=%v prev=%q err=%v", o, prev, err)
	}
	body := readFileStr(t, path)
	if n := strings.Count(body, agentsMarker); n != 1 {
		t.Fatalf("refresh should leave one block, got %d", n)
	}
	if !strings.Contains(body, "side-quest-version: 2.0.0") {
		t.Errorf("refresh did not restamp version:\n%s", body)
	}
}

// TestInstallAgentsGuidanceAppendsPreservingContent (SQ-0047): an AGENTS.md with
// the user's own content gets our block appended, never overwriting their text.
func TestInstallAgentsGuidanceAppendsPreservingContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	userText := "# My project rules\n\nAlways be excellent.\n"
	if err := os.WriteFile(path, []byte(userText), 0o644); err != nil {
		t.Fatal(err)
	}
	o, _, err := installAgentsGuidance(path, "1.0.0")
	if err != nil || o != blockAppended {
		t.Fatalf("append: outcome=%v err=%v", o, err)
	}
	got := readFileStr(t, path)
	if !strings.Contains(got, "Always be excellent.") {
		t.Errorf("append clobbered user content:\n%s", got)
	}
	if !strings.Contains(got, agentsMarker) {
		t.Errorf("append did not add the block:\n%s", got)
	}
}

// TestOnboardRefreshesAgentsInPlace (SQ-0047): onboard writes the guidance block
// into the project's AGENTS.md, and a later onboard from a newer binary refreshes
// that block in place rather than leaving a stale merged copy or a duplicate.
func TestOnboardRefreshesAgentsInPlace(t *testing.T) {
	dir, _ := newRepo(t)
	agentsPath := filepath.Join(dir, "AGENTS.md")

	if _, code := runBin(t, buildBinaryVersion(t, "1.0.0"), dir, "onboard", "--agents-md"); code != 0 {
		t.Fatalf("first onboard exit=%d", code)
	}
	first := readFileStr(t, agentsPath)
	if !strings.Contains(first, "Quest: SQ-") || !strings.Contains(first, "side-quest-version: 1.0.0") {
		t.Fatalf("onboard did not write stamped guidance to AGENTS.md:\n%s", first)
	}

	if _, code := runBin(t, buildBinaryVersion(t, "2.0.0"), dir, "onboard", "--agents-md"); code != 0 {
		t.Fatalf("second onboard exit=%d", code)
	}
	second := readFileStr(t, agentsPath)
	if n := strings.Count(second, agentsMarker); n != 1 {
		t.Fatalf("refresh left %d blocks, want 1:\n%s", n, second)
	}
	if !strings.Contains(second, "side-quest-version: 2.0.0") || strings.Contains(second, "side-quest-version: 1.0.0") {
		t.Errorf("onboard did not refresh the AGENTS.md block to 2.0.0:\n%s", second)
	}
}

// onboard is the turnkey per-repo setup: init + install-hooks + write .mcp.json.
// By default it does NOT touch AGENTS.md (guidance rides the MCP server now; the
// merge is opt-in via --agents-md — SQ-0051). A fresh repo ends up wired.
func TestOnboardSetsUpRepo(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_DATA", "") // pin non-plugin: onboard must write .mcp.json
	t.Setenv("SIDE_QUEST_PLUGIN", "")  // and no launcher marker (SQ-0072)
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
	if !strings.Contains(strings.ToLower(out), "restart") {
		t.Error("onboard did not print a restart reminder")
	}
	if strings.Contains(out, "install-cli") {
		t.Errorf("standalone onboard must not push the terminal-CLI tip; got:\n%s", out)
	}
}

// Bare onboard no longer touches the project's AGENTS.md — guidance now rides the
// MCP server; the merge is opt-in (SQ-0051).
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

// --agents-md opts back into the AGENTS.md merge.
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

// Under the plugin, onboard nudges the user to enable the terminal CLI — but only
// when a marked launcher isn't already on PATH (SQ-0073).
func TestOnboardTipsEnableCliUnderPlugin(t *testing.T) {
	bin := buildBinary(t) // build with the real env before scrubbing PATH/HOME
	dir, _ := newRepo(t)  // git identity is local config, independent of HOME

	t.Setenv("CLAUDE_PLUGIN_DATA", t.TempDir()) // plugin active
	// Make the terminal-CLI probe deterministically empty: a fresh HOME (so the
	// candidate bin dirs don't exist) and a PATH scrubbed of the real home's bin
	// dirs, keeping the system dirs onboard still needs for git.
	realHome, _ := os.UserHomeDir()
	var kept []string
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if p != "" && !strings.HasPrefix(p, realHome) {
			kept = append(kept, p)
		}
	}
	t.Setenv("PATH", strings.Join(kept, string(os.PathListSeparator)))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_BIN_HOME", "")

	out, code := runBin(t, bin, dir, "onboard")
	if code != 0 {
		t.Fatalf("onboard exit=%d out=%q", code, out)
	}
	if !strings.Contains(out, "install-cli") {
		t.Errorf("onboard under the plugin should suggest enabling the terminal CLI; got:\n%s", out)
	}
}

// onboard writes .mcp.json only when absent — an existing one is never clobbered.
func TestOnboardPreservesExistingMcpJson(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_DATA", "") // pin non-plugin: onboard must write .mcp.json
	t.Setenv("SIDE_QUEST_PLUGIN", "")  // and no launcher marker (SQ-0072)
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
