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

func TestDedupeEnvKeepLast(t *testing.T) {
	in := []string{"A=1", "B=2", "A=3", "PATH=/x", "B=5", "NOEQ"}
	got := dedupeEnvKeepLast(in)

	m := map[string]string{}
	for _, kv := range got {
		if k, v, ok := strings.Cut(kv, "="); ok {
			if _, dup := m[k]; dup {
				t.Fatalf("key %q appears more than once: %v", k, got)
			}
			m[k] = v
		}
	}
	if m["A"] != "3" || m["B"] != "5" || m["PATH"] != "/x" {
		t.Fatalf("keep-last wrong: %v", m)
	}
	// entries without '=' are preserved as-is
	found := false
	for _, kv := range got {
		if kv == "NOEQ" {
			found = true
		}
	}
	if !found {
		t.Fatalf("non key=value entry dropped: %v", got)
	}
}
