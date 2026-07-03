package main

import (
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

func remoteHasQuestRef(t *testing.T, originDir string) bool {
	t.Helper()
	out, err := gitcmd.New(originDir).Run("for-each-ref", "--format=%(refname)", store.Ref)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(out) != ""
}
