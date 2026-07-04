package store

import (
	"strings"
	"testing"
)

func TestCommitMessageSubjectAndFull(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	sha := commitInWorktree(t, s, "f.txt", "feat: do a thing\n\nsome body detail\n\nToken: xyz\n")

	short, subj, ok := s.CommitMessage(sha, false)
	if !ok {
		t.Fatal("CommitMessage(full=false) ok=false for a real commit")
	}
	if subj != "feat: do a thing" {
		t.Errorf("subject = %q, want %q", subj, "feat: do a thing")
	}
	if short == "" || !strings.HasPrefix(sha, short) {
		t.Errorf("short %q is not an abbreviation of %q", short, sha)
	}

	_, full, ok := s.CommitMessage(sha, true)
	if !ok {
		t.Fatal("CommitMessage(full=true) ok=false for a real commit")
	}
	for _, want := range []string{"feat: do a thing", "some body detail", "Token: xyz"} {
		if !strings.Contains(full, want) {
			t.Errorf("full message missing %q:\n%s", want, full)
		}
	}
}

func TestCommitMessageMissingReturnsNotOK(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s.CommitMessage("deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", false); ok {
		t.Error("CommitMessage for an unknown sha should return ok=false")
	}
}
