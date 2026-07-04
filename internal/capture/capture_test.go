package capture

import (
	"strings"
	"testing"

	"github.com/sharkusk/side-quest/internal/gitcmd"
)

func TestMechanicalInRepo(t *testing.T) {
	dir := t.TempDir()
	g := gitcmd.New(dir)
	for _, a := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "Tester"},
		{"commit", "--allow-empty", "-q", "-m", "root"},
	} {
		if _, err := g.Run(a...); err != nil {
			t.Fatal(err)
		}
	}
	out := Mechanical(dir, "SQ-0007")
	for _, want := range []string{"branch:", "head:", "cwd:", "current: SQ-0007"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestBodyCombinesMechanicalThenContext(t *testing.T) {
	dir := t.TempDir() // not a git repo: mechanical is just the cwd line

	// With a user note, the mechanical capture comes first, joined by a blank line.
	got := Body(dir, "", "why now")
	if !strings.HasPrefix(got, "cwd:") {
		t.Errorf("mechanical capture should lead:\n%s", got)
	}
	if !strings.HasSuffix(got, "\n\nwhy now") {
		t.Errorf("user context should follow after a blank line:\n%s", got)
	}

	// Empty user context yields just the mechanical capture, no trailing separator.
	only := Body(dir, "", "")
	if only != Mechanical(dir, "") {
		t.Errorf("empty context should equal mechanical alone: %q vs %q", only, Mechanical(dir, ""))
	}
}

func TestMechanicalBestEffortNoCurrent(t *testing.T) {
	dir := t.TempDir() // not a git repo
	out := Mechanical(dir, "")
	if strings.Contains(out, "current:") {
		t.Fatalf("no current expected, got:\n%s", out)
	}
	if !strings.Contains(out, "cwd:") {
		t.Fatalf("cwd should always be present, got:\n%s", out)
	}
}
