package main

import (
	"os/exec"
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

// The help screen and the bare-usage output both carry the version, so a user
// sees which side-quest they're running without a separate `version` call.
func TestHelpShowsVersion(t *testing.T) {
	bin := buildBinaryVersion(t, "9.9.9")

	// `help` -> stdout, exit 0.
	out, code := runBin(t, bin, t.TempDir(), "help")
	if code != 0 {
		t.Fatalf("help exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, "side-quest 9.9.9") {
		t.Errorf("help output missing version header:\n%s", out)
	}

	// no args -> usage on stderr, exit 2.
	uout, ucode := runBin(t, bin, t.TempDir())
	if ucode != 2 {
		t.Fatalf("no-arg exit=%d, want 2\n%s", ucode, uout)
	}
	if !strings.Contains(uout, "side-quest 9.9.9") {
		t.Errorf("no-arg usage missing version header:\n%s", uout)
	}
}

// A release build injects the version via ldflags; the binary must report it.
func TestVersionReflectsLdflags(t *testing.T) {
	bin := exePath(t.TempDir())
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
