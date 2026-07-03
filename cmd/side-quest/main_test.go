package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharkusk/side-quest/internal/gitcmd"
	"github.com/sharkusk/side-quest/internal/quest"
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
	q, err := s.Create("a task", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, code := runBin(t, bin, dir, "current", q.ID); code != 0 {
		t.Fatalf("set current exit=%d", code)
	}
	cur, _ := s.Current()
	if cur != q.ID {
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

// TestShowAcceptsShorthandID proves the id shorthand reaches through the CLI
// frontend: `show 1` finds the same quest as `show SQ-0001` and renders the
// canonical id.
func TestShowAcceptsShorthandID(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)
	q, err := s.Create("a task", "", "", "", nil) // SQ-0001
	if err != nil {
		t.Fatal(err)
	}

	for _, raw := range []string{"1", "0001", q.ID} {
		out, code := runBin(t, bin, dir, "show", raw)
		if code != 0 {
			t.Fatalf("show %q exit=%d out=%q", raw, code, out)
		}
		if !strings.Contains(out, q.ID) {
			t.Errorf("show %q output missing canonical id %q:\n%s", raw, q.ID, out)
		}
	}

	if _, code := runBin(t, bin, dir, "show", "999"); code == 0 {
		t.Error("show of an unknown shorthand id should exit non-zero")
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
	q, err := s.Create("a task", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetCurrent(q.ID); err != nil {
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
	if want := "Quest: " + q.ID; !containsLine(string(out), want) {
		t.Fatalf("current trailer not injected: %q", out)
	}
	// Idempotent: running again does not add a second trailer.
	if _, code := runBin(t, bin, dir, "prepare-commit-msg", msg); code != 0 {
		t.Fatalf("second prepare exit=%d", code)
	}
	out2, _ := os.ReadFile(msg)
	if countLine(string(out2), "Quest: "+q.ID) != 1 {
		t.Fatalf("trailer injected twice: %q", out2)
	}
}

// TestPrepareCommitMsgWriteFailureDoesNotBlock locks in the hook's hard
// invariant: it must never block a commit. Git aborts the commit if
// prepare-commit-msg exits non-zero, so even a failure to write the injected
// trailer back to the message file must still result in exit 0 and the
// message left as-is.
func TestPrepareCommitMsgWriteFailureDoesNotBlock(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	q, err := s.Create("a task", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetCurrent(q.ID); err != nil {
		t.Fatal(err)
	}
	msg := filepath.Join(dir, "MSG")
	original := "a change\n"
	if err := os.WriteFile(msg, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(msg, 0o444); err != nil {
		t.Fatal(err)
	}

	// Some environments (e.g. running as root, or certain CI containers) can
	// write to a file regardless of the 0o444 permission bit. If so, the
	// hook's injecting write below won't actually fail, and this test can't
	// exercise the invariant it's meant to check.
	if f, err := os.OpenFile(msg, os.O_WRONLY, 0); err == nil {
		f.Close()
		t.Skip("cannot make file read-only in this environment")
	}

	_, code := runBin(t, bin, dir, "prepare-commit-msg", msg)
	if code != 0 {
		t.Fatalf("prepare-commit-msg must never block the commit, got exit=%d", code)
	}
	out, err := os.ReadFile(msg)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != original {
		t.Fatalf("message file changed despite write failure: got %q, want %q", out, original)
	}
}

// gitCommit runs a real `git commit` in dir (hooks fire) and returns the exit
// code — used to assert require_quest rejection blocks the commit.
func gitCommit(t *testing.T, dir, filename, message string) int {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte("data\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := gitcmd.New(dir)
	if _, err := g.Run("add", filename); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "commit", "-m", message)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		t.Fatalf("git commit: %v", err)
	}
	return 0
}

// TestEndToEndHooksDriveLinking installs the real hooks and drives them with
// real commits: current-quest injection, assisted warning, enforced rejection,
// and post-commit linking that closes a quest.
func TestEndToEndHooksDriveLinking(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	q, err := s.Create("ship it", "", "", "", nil) // SQ-0001
	if err != nil {
		t.Fatal(err)
	}

	// Install hooks (bakes the built binary's absolute path into the shims).
	if _, code := runBin(t, bin, dir, "install-hooks"); code != 0 {
		t.Fatalf("install-hooks exit=%d", code)
	}

	// 1) current-quest injection + post-commit linking (not closing).
	if _, code := runBin(t, bin, dir, "current", q.ID); code != 0 {
		t.Fatalf("set current exit=%d", code)
	}
	if code := gitCommit(t, dir, "f1.txt", "progress on the feature"); code != 0 {
		t.Fatalf("commit 1 blocked unexpectedly: %d", code)
	}
	got, _ := s.Get(q.ID)
	if len(got.Commits) != 1 {
		t.Fatalf("prepare+post-commit should have linked one commit: %v", got.Commits)
	}
	if got.Status != quest.StatusOpen {
		t.Fatalf("Quest: (auto) should not close: %+v", got)
	}

	// 2) explicit Completes: closes the quest.
	if _, code := runBin(t, bin, dir, "current", "--clear"); code != 0 {
		t.Fatalf("clear current exit=%d", code)
	}
	if code := gitCommit(t, dir, "f2.txt", "done\n\nCompletes: "+q.ID); code != 0 {
		t.Fatalf("commit 2 blocked unexpectedly: %d", code)
	}
	got, _ = s.Get(q.ID)
	if got.Status != quest.StatusDone {
		t.Fatalf("Completes: via hook should close: %+v", got)
	}
	if len(got.Commits) != 2 {
		t.Fatalf("expected 2 linked commits, got %v", got.Commits)
	}

	// 3) require_quest enforcement blocks a trailerless commit.
	if err := s.SetRequireQuest(true); err != nil {
		t.Fatal(err)
	}
	if code := gitCommit(t, dir, "f3.txt", "no trailer here"); code == 0 {
		t.Fatal("require_quest should have blocked a trailerless commit")
	}
	// ...but Quest: none passes.
	if code := gitCommit(t, dir, "f3.txt", "chore\n\nQuest: none"); code != 0 {
		t.Fatalf("Quest: none should pass enforcement, blocked with %d", code)
	}
}

// TestInstallHooksFromSubdirectory locks in that install-hooks resolves the
// --git-common-dir fallback against the directory the command actually ran
// in, not the worktree top. Before the fix, running from a subdirectory
// joined the (cwd-relative) common-dir output onto the wrong base and wrote
// the shims outside .git/hooks without any error.
func TestInstallHooksFromSubdirectory(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}

	sub := filepath.Join(dir, "sub", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, code := runBin(t, bin, sub, "install-hooks"); code != 0 {
		t.Fatalf("install-hooks (from subdir) exit=%d", code)
	}

	hooksDir := filepath.Join(dir, ".git", "hooks")
	for _, name := range []string{"prepare-commit-msg", "commit-msg", "post-commit"} {
		fi, err := os.Stat(filepath.Join(hooksDir, name))
		if err != nil {
			t.Fatalf("hook %s not installed at %s: %v", name, hooksDir, err)
		}
		if fi.Size() == 0 {
			t.Fatalf("hook %s is empty", name)
		}
	}
}

// TestInstallHooksPushKeepsBranchAndQuests is the SQ-0016 regression: a bare
// `git push` after install-hooks must send BOTH the current branch AND the quest
// refs. Before the fix, the lone refs/side-quest/* push refspec disabled
// push.default so the branch was silently skipped.
func TestInstallHooksPushKeepsBranchAndQuests(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("q", "", "", "", nil); err != nil { // creates refs/side-quest/quests
		t.Fatal(err)
	}
	g := gitcmd.New(dir)

	// A branch with a commit to push.
	if _, err := g.Run("checkout", "-q", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run("add", "f.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run("commit", "-q", "-m", "c1"); err != nil {
		t.Fatal(err)
	}

	// A bare origin remote, then install-hooks (which configures the push refspec).
	remote := t.TempDir()
	if _, err := gitcmd.New(remote).Run("init", "--bare", "-q"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run("remote", "add", "origin", remote); err != nil {
		t.Fatal(err)
	}
	if _, code := runBin(t, bin, dir, "install-hooks"); code != 0 {
		t.Fatalf("install-hooks exit=%d", code)
	}

	// Bare push: must land BOTH the branch and the quest ref.
	if _, err := g.Run("push", "origin"); err != nil {
		t.Fatalf("push: %v", err)
	}
	rg := gitcmd.New(remote)
	if _, err := rg.Run("show-ref", "--verify", "refs/heads/main"); err != nil {
		t.Errorf("branch not pushed (SQ-0016 regression): %v", err)
	}
	if _, err := rg.Run("show-ref", "--verify", "refs/side-quest/quests"); err != nil {
		t.Errorf("quest ref not pushed: %v", err)
	}
}

// TestInstallHooksSkipsNonShHook is the SQ-0020 correctness gate: a pre-existing
// hook with a non-sh shebang must be left byte-for-byte intact (appending our sh
// block would corrupt it), with a warning, while the other hooks still install.
func TestInstallHooksSkipsNonShHook(t *testing.T) {
	bin := buildBinary(t)
	dir, s := newRepo(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}

	hooksDir := filepath.Join(dir, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pyHook := filepath.Join(hooksDir, "commit-msg")
	pyBody := "#!/usr/bin/env python3\nimport sys\nsys.exit(0)\n"
	if err := os.WriteFile(pyHook, []byte(pyBody), 0o755); err != nil {
		t.Fatal(err)
	}

	out, code := runBin(t, bin, dir, "install-hooks")
	if code != 0 {
		t.Fatalf("install-hooks exit=%d out=%s", code, out)
	}

	// The python hook is untouched — no side-quest block appended.
	got, err := os.ReadFile(pyHook)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != pyBody {
		t.Errorf("non-sh commit-msg hook was modified:\n%s", got)
	}
	if strings.Contains(string(got), hookMarker) {
		t.Error("side-quest block was appended to a non-sh hook (corruption)")
	}
	// The user is warned about the skip.
	if !strings.Contains(out, "SKIPPED") || !strings.Contains(out, "commit-msg") {
		t.Errorf("expected a skip warning naming commit-msg, got:\n%s", out)
	}
	// The other two hooks still installed normally.
	for _, name := range []string{"prepare-commit-msg", "post-commit"} {
		b, err := os.ReadFile(filepath.Join(hooksDir, name))
		if err != nil || !strings.Contains(string(b), hookMarker) {
			t.Errorf("hook %s did not install: err=%v", name, err)
		}
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
