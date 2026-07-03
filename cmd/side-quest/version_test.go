package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A plain build reports the default version "dev" for all three spellings.
func TestVersionReportsDevByDefault(t *testing.T) {
	bin := buildBinary(t)
	for _, arg := range []string{"version", "--version", "-v"} {
		out, code := runBin(t, bin, t.TempDir(), arg)
		if code != 0 {
			t.Fatalf("%q exit=%d out=%s", arg, code, out)
		}
		if strings.TrimSpace(out) != "dev" {
			t.Errorf("%q = %q, want dev", arg, strings.TrimSpace(out))
		}
	}
}

// A release build injects the version via ldflags; the binary must report it.
func TestVersionReflectsLdflags(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "side-quest")
	out, err := exec.Command("go", "build",
		"-ldflags", "-X main.version=9.9.9", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	got, code := runBin(t, bin, t.TempDir(), "version")
	if code != 0 || strings.TrimSpace(got) != "9.9.9" {
		t.Fatalf("version = %q (exit %d), want 9.9.9", got, code)
	}
}
