package gitcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// initRepo makes an empty git repo in a temp dir and returns its path.
func initRepo(t *testing.T) string {
	t.Helper() // marks this as a helper so failures report the caller's line
	dir := t.TempDir()
	g := New(dir)
	if _, err := g.Run("init", "-q"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if _, err := g.Run("config", "user.email", "t@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run("config", "user.name", "Tester"); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRunTrimsStdout(t *testing.T) {
	dir := initRepo(t)
	out, err := New(dir).Run("rev-parse", "--is-inside-work-tree")
	if err != nil {
		t.Fatal(err)
	}
	if out != "true" { // Run trims the trailing newline git prints
		t.Fatalf("got %q, want %q", out, "true")
	}
}

func TestRunRawPreservesBytes(t *testing.T) {
	dir := initRepo(t)
	// hash-object of known content, then cat-file -p should return it verbatim.
	g := New(dir)
	blob, err := g.RunInput("hello\n", "hash-object", "-w", "--stdin")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := g.RunRaw("cat-file", "-p", blob)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "hello\n" { // RunRaw must NOT trim the trailing newline
		t.Fatalf("got %q, want %q", string(raw), "hello\n")
	}
}

func TestRunErrorIncludesStderr(t *testing.T) {
	dir := initRepo(t)
	_, err := New(dir).Run("cat-file", "-p", "deadbeef")
	if err == nil {
		t.Fatal("expected error for missing object")
	}
	if !strings.Contains(err.Error(), "cat-file") {
		t.Fatalf("error should name the command, got: %v", err)
	}
}

func TestWithEnvSetsGitIndexFile(t *testing.T) {
	dir := initRepo(t)
	idx := filepath.Join(t.TempDir(), "scratch-index")
	g := New(dir).WithEnv("GIT_INDEX_FILE=" + idx)
	// Stage nothing but write the (empty) tree using the scratch index.
	if _, err := g.Run("read-tree", "--empty"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run("write-tree"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(idx); err != nil {
		t.Fatalf("scratch index file not created: %v", err)
	}
}
