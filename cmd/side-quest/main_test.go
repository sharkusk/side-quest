package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharkusk/side-quest/internal/gitcmd"
	"github.com/sharkusk/side-quest/internal/store"
)

// buildBinary compiles cmd/side-quest to a temp path and returns it.
func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "side-quest")
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}
	return bin
}

// newRepo makes a temp git repo with an identity and returns (dir, openedStore).
func newRepo(t *testing.T) (string, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	g := gitcmd.New(dir)
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "Tester"},
	} {
		if _, err := g.Run(args...); err != nil {
			t.Fatal(err)
		}
	}
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, s
}

// runBin runs the built binary in dir and returns (combined output, exit code).
func runBin(t *testing.T, bin, dir string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
	return string(out), code
}

func TestCurrentSubcommand(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)

	if _, code := runBin(t, bin, dir, "current", "SQ-0042"); code != 0 {
		t.Fatalf("set current exit=%d", code)
	}
	cur, _ := s.Current()
	if cur != "SQ-0042" {
		t.Fatalf("current not set via CLI: %q", cur)
	}
	out, code := runBin(t, bin, dir, "current")
	if code != 0 || out == "" {
		t.Fatalf("get current: out=%q code=%d", out, code)
	}
	if _, code := runBin(t, bin, dir, "current", "--clear"); code != 0 {
		t.Fatalf("clear exit=%d", code)
	}
	if cur, _ := s.Current(); cur != "" {
		t.Fatalf("current not cleared: %q", cur)
	}
}

func TestCommitMsgExitCodes(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	msg := filepath.Join(dir, "MSG")

	// Assisted (default): missing trailer warns but exits 0.
	if err := os.WriteFile(msg, []byte("no trailer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, code := runBin(t, bin, dir, "commit-msg", msg); code != 0 {
		t.Fatalf("warn path must exit 0, got %d", code)
	}

	// Enforced: missing trailer rejects (exit 1).
	if err := s.SetRequireQuest(true); err != nil {
		t.Fatal(err)
	}
	if _, code := runBin(t, bin, dir, "commit-msg", msg); code != 1 {
		t.Fatalf("reject path must exit 1, got %d", code)
	}

	// Escape hatch: Quest: none passes even when enforced.
	if err := os.WriteFile(msg, []byte("chore\n\nQuest: none\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, code := runBin(t, bin, dir, "commit-msg", msg); code != 0 {
		t.Fatalf("Quest: none must pass enforcement, got %d", code)
	}
}

func TestPrepareCommitMsgInjectsCurrent(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCurrent("SQ-0005"); err != nil {
		t.Fatal(err)
	}
	msg := filepath.Join(dir, "MSG")
	if err := os.WriteFile(msg, []byte("a change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, code := runBin(t, bin, dir, "prepare-commit-msg", msg); code != 0 {
		t.Fatalf("prepare exit=%d", code)
	}
	out, _ := os.ReadFile(msg)
	if want := "Quest: SQ-0005"; !containsLine(string(out), want) {
		t.Fatalf("current trailer not injected: %q", out)
	}
	// Idempotent: running again does not add a second trailer.
	if _, code := runBin(t, bin, dir, "prepare-commit-msg", msg); code != 0 {
		t.Fatalf("second prepare exit=%d", code)
	}
	out2, _ := os.ReadFile(msg)
	if countLine(string(out2), "Quest: SQ-0005") != 1 {
		t.Fatalf("trailer injected twice: %q", out2)
	}
}

func containsLine(s, want string) bool { return countLine(s, want) > 0 }

func countLine(s, want string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if line == want {
			n++
		}
	}
	return n
}
