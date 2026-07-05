package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharkusk/side-quest/internal/gitcmd"
	"github.com/sharkusk/side-quest/internal/store"
)

// TestSyncCommandPublishesAndDryRun locks in the `side-quest sync` CLI surface:
// --dry-run must not touch the remote, and a real sync must publish the quest
// ref to a fresh bare origin.
func TestSyncCommandPublishesAndDryRun(t *testing.T) {
	bin := buildBinary(t)
	origin := t.TempDir()
	if _, err := gitcmd.New(origin).Run("init", "--bare", "-q"); err != nil {
		t.Fatal(err)
	}
	dir, s := newRepo(t)
	if _, err := gitcmd.New(dir).Run("remote", "add", "origin", origin); err != nil {
		t.Fatal(err)
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("first", "", "", "", nil); err != nil {
		t.Fatal(err)
	}

	// dry-run writes/pushes nothing
	out, code := runBin(t, bin, dir, "sync", "--dry-run")
	if code != 0 {
		t.Fatalf("sync --dry-run exit=%d out=%s", code, out)
	}
	if remoteHasQuestRef(t, origin) {
		t.Errorf("dry-run must not push; origin has the quest ref")
	}

	// real sync publishes
	if _, code := runBin(t, bin, dir, "sync"); code != 0 {
		t.Fatalf("sync exit=%d", code)
	}
	if !remoteHasQuestRef(t, origin) {
		t.Errorf("sync should have published the quest ref")
	}
}

// TestSyncNudgesSequentialWithRemote (SQ-0035): a repo initialized before a
// remote existed keeps sequential ids, which clash across clones. When such a
// repo gains a remote and syncs, `side-quest sync` nudges the user toward random
// ids. A repo that had a remote at init time already defaults to random and must
// stay quiet.
func TestSyncNudgesSequentialWithRemote(t *testing.T) {
	bin := buildBinary(t)

	newBareOrigin := func(t *testing.T) string {
		o := t.TempDir()
		if _, err := gitcmd.New(o).Run("init", "--bare", "-q"); err != nil {
			t.Fatal(err)
		}
		return o
	}

	// Positive: init with no remote -> sequential; add the remote afterward.
	t.Run("sequential+remote nudges", func(t *testing.T) {
		origin := newBareOrigin(t)
		dir, s := newRepo(t)
		if err := s.Init(); err != nil { // no remote yet -> sequential ids
			t.Fatal(err)
		}
		if _, err := s.Create("first", "", "", "", nil); err != nil {
			t.Fatal(err)
		}
		if _, err := gitcmd.New(dir).Run("remote", "add", "origin", origin); err != nil {
			t.Fatal(err)
		}
		out, code := runBin(t, bin, dir, "sync")
		if code != 0 {
			t.Fatalf("sync exit=%d out=%s", code, out)
		}
		if !strings.Contains(out, "id_strategy random") {
			t.Errorf("expected a nudge toward random ids, got:\n%s", out)
		}
	})

	// Negative: a remote present at init -> random ids already; no nudge.
	t.Run("random stays quiet", func(t *testing.T) {
		origin := newBareOrigin(t)
		dir, s := newRepo(t)
		if _, err := gitcmd.New(dir).Run("remote", "add", "origin", origin); err != nil {
			t.Fatal(err)
		}
		if err := s.Init(); err != nil { // remote present -> random ids
			t.Fatal(err)
		}
		if _, err := s.Create("first", "", "", "", nil); err != nil {
			t.Fatal(err)
		}
		out, code := runBin(t, bin, dir, "sync")
		if code != 0 {
			t.Fatalf("sync exit=%d out=%s", code, out)
		}
		if strings.Contains(out, "id_strategy random") {
			t.Errorf("random-id repo must not be nudged, got:\n%s", out)
		}
	})
}

func remoteHasQuestRef(t *testing.T, originDir string) bool {
	t.Helper()
	out, err := gitcmd.New(originDir).Run("for-each-ref", "--format=%(refname)", store.Ref)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(out) != ""
}

// TestPrePushHookPublishesQuests locks in the automatic path (SQ-0032): a bare
// `git push` fires the installed pre-push hook, which syncs and publishes the
// quest ref without the user running `side-quest sync` themselves.
func TestPrePushHookPublishesQuests(t *testing.T) {
	bin := buildBinary(t)
	putBinDirOnPath(t, bin) // pre-push hook resolves side-quest via PATH (SQ-0086)
	origin := t.TempDir()
	if _, err := gitcmd.New(origin).Run("init", "--bare", "-q"); err != nil {
		t.Fatal(err)
	}
	dir, s := newRepo(t)
	g := gitcmd.New(dir)
	if _, err := g.Run("remote", "add", "origin", origin); err != nil {
		t.Fatal(err)
	}
	// install hooks (adds pre-push shim + refspecs) via the real binary
	if _, code := runBin(t, bin, dir, "install-hooks"); code != 0 {
		t.Fatalf("install-hooks exit=%d", code)
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("hooked", "", "", "", nil); err != nil {
		t.Fatal(err)
	}
	// make a branch commit so there is something to push
	writeFile(t, dir, "f.txt", "x")
	if _, err := g.Run("add", "f.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run("commit", "-q", "-m", "work"); err != nil {
		t.Fatal(err)
	}

	// a bare `git push` fires the pre-push hook, which syncs the quest ref
	if _, err := g.Run("push", "origin", "HEAD"); err != nil {
		t.Fatalf("push: %v", err)
	}
	if !remoteHasQuestRef(t, origin) {
		t.Errorf("pre-push hook should have published the quest ref")
	}
}

// TestPrePushHookOfflineWarnsExitsZero locks in the warn-never-block invariant:
// a sync failure (e.g. offline, broken remote) must warn on stderr and still
// exit 0, so the hook can never block the user's branch push.
func TestPrePushHookOfflineWarnsExitsZero(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)
	// origin points at a nonexistent path -> fetch fails (offline analogue)
	if _, err := gitcmd.New(dir).Run("remote", "add", "origin", filepath.Join(dir, "nope.git")); err != nil {
		t.Fatal(err)
	}
	out, code := runBin(t, bin, dir, "pre-push", "origin", "file://nope")
	if code != 0 {
		t.Fatalf("pre-push must exit 0 even when sync fails; got %d out=%s", code, out)
	}
	if !strings.Contains(out, "couldn't publish quests") {
		t.Errorf("expected a warning; got %q", out)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
