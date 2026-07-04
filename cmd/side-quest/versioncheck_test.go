package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestVersionDrift locks in the pure comparison: warn only when both versions are
// known and differ; name both in the warning so the user can tell them apart.
func TestVersionDrift(t *testing.T) {
	cases := []struct {
		own, other string
		wantEmpty  bool
	}{
		{"1.0.0", "1.0.0", true},  // same build -> quiet
		{"", "1.0.0", true},       // unknown own -> quiet
		{"1.0.0", "", true},       // unknown other -> quiet
		{"dev", "0.1.0", false},   // dev build vs a release -> warn
		{"0.1.0", "0.2.0", false}, // two releases differ -> warn
	}
	for _, c := range cases {
		got := versionDrift(c.own, c.other)
		if (got == "") != c.wantEmpty {
			t.Errorf("versionDrift(%q,%q)=%q, wantEmpty=%v", c.own, c.other, got, c.wantEmpty)
		}
		if got != "" && (!strings.Contains(got, c.own) || !strings.Contains(got, c.other)) {
			t.Errorf("warning should name both versions, got %q", got)
		}
	}
}

// TestPathBinaryDrift covers the PATH lookup around versionDrift: a differing
// side-quest on PATH warns, a matching one is quiet, and no side-quest on PATH is
// quiet. A real side-quest binary reporting 1.2.3 stands in for the installed one —
// buildBinaryVersion names it side-quest(.exe) so exec.LookPath resolves it on every
// platform (a shell-script fake failed on Windows, which has no shebang support).
func TestPathBinaryDrift(t *testing.T) {
	bin := buildBinaryVersion(t, "1.2.3")
	t.Setenv("PATH", filepath.Dir(bin))

	if got := pathBinaryDrift("9.9.9"); got == "" {
		t.Errorf("expected a drift warning for 9.9.9 vs the PATH binary's 1.2.3")
	}
	if got := pathBinaryDrift("1.2.3"); got != "" {
		t.Errorf("matching versions must be quiet, got %q", got)
	}

	t.Setenv("PATH", t.TempDir()) // no side-quest here
	if got := pathBinaryDrift("9.9.9"); got != "" {
		t.Errorf("no side-quest on PATH must be quiet, got %q", got)
	}
}
