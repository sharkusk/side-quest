package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharkusk/side-quest/internal/gitcmd"
	"github.com/sharkusk/side-quest/internal/store"
)

// buildBinaryVersion compiles cmd/side-quest with an injected main.version so a
// test can simulate an upgrade (two builds with different versions).
func buildBinaryVersion(t *testing.T, ver string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "side-quest")
	out, err := exec.Command("go", "build", "-ldflags", "-X main.version="+ver, "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("build %s: %v\n%s", ver, err, out)
	}
	return bin
}

func readFileStr(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestHookBlockStampsVersion (SQ-0045): the installed block carries a version
// stamp, and its parts stay in order (marker, version, body, end marker).
func TestHookBlockStampsVersion(t *testing.T) {
	b := hookBlock(`"/bin/side-quest" link HEAD || true`, "1.2.3")
	if !strings.Contains(b, hookMarker) || !strings.Contains(b, hookEndMarker) {
		t.Fatalf("block missing markers:\n%s", b)
	}
	if !strings.Contains(b, "# side-quest-version: 1.2.3") {
		t.Errorf("block missing version stamp:\n%s", b)
	}
	iMark := strings.Index(b, hookMarker)
	iVer := strings.Index(b, "side-quest-version")
	iBody := strings.Index(b, "link HEAD")
	iEnd := strings.Index(b, hookEndMarker)
	if !(iMark < iVer && iVer < iBody && iBody < iEnd) {
		t.Errorf("block parts out of order: mark=%d ver=%d body=%d end=%d", iMark, iVer, iBody, iEnd)
	}
}

// TestParseHookVersion (SQ-0045): the stamp round-trips, and a pre-stamp block
// (written by an older side-quest) parses to the empty version.
func TestParseHookVersion(t *testing.T) {
	if got := parseHookVersion(hookBlock("body", "9.9.9")); got != "9.9.9" {
		t.Errorf("parseHookVersion = %q, want 9.9.9", got)
	}
	old := hookMarker + "\nbody\n" + hookEndMarker + "\n" // predates version stamping
	if got := parseHookVersion(old); got != "" {
		t.Errorf("versionless block should parse to empty, got %q", got)
	}
}

// TestInstallOneHookRefreshDetection (SQ-0045): a fresh install stamps the
// version; an identical re-install is a true no-op (unchanged); a version bump
// refreshes in place and reports the previous version.
func TestInstallOneHookRefreshDetection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "post-commit")
	body := `"/bin/side-quest" link HEAD || true`

	if oc, prev, err := installOneHook(path, body, "1.0.0"); err != nil || oc != hookCreated || prev != "" {
		t.Fatalf("create: oc=%v prev=%q err=%v", oc, prev, err)
	}

	before := readFileStr(t, path)
	if oc, prev, err := installOneHook(path, body, "1.0.0"); err != nil || oc != hookUnchanged || prev != "1.0.0" {
		t.Fatalf("unchanged re-install: oc=%v prev=%q err=%v", oc, prev, err)
	}
	if after := readFileStr(t, path); after != before {
		t.Errorf("unchanged re-install must not modify the file")
	}

	if oc, prev, err := installOneHook(path, body, "2.0.0"); err != nil || oc != hookUpdated || prev != "1.0.0" {
		t.Fatalf("upgrade: oc=%v prev=%q err=%v", oc, prev, err)
	}
	if got := parseHookVersion(readFileStr(t, path)); got != "2.0.0" {
		t.Errorf("file not restamped to 2.0.0, got %q", got)
	}
}

// TestInstallHooksStampsAndReportsUpgrade (SQ-0045): end-to-end, installing with
// one version then a newer one restamps the hook and reports the v1→v2 refresh.
func TestInstallHooksStampsAndReportsUpgrade(t *testing.T) {
	binV1 := buildBinaryVersion(t, "1.0.0")
	binV2 := buildBinaryVersion(t, "2.0.0")
	dir, _ := newRepo(t)

	if _, code := runBin(t, binV1, dir, "install-hooks"); code != 0 {
		t.Fatalf("install v1 exit=%d", code)
	}
	hook := filepath.Join(dir, ".git", "hooks", "post-commit")
	if got := parseHookVersion(readFileStr(t, hook)); got != "1.0.0" {
		t.Fatalf("hook not stamped v1.0.0, got %q", got)
	}

	out, code := runBin(t, binV2, dir, "install-hooks")
	if code != 0 {
		t.Fatalf("install v2 exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, "1.0.0") || !strings.Contains(out, "2.0.0") {
		t.Errorf("expected a refresh note naming v1.0.0 -> v2.0.0, got:\n%s", out)
	}
	if got := parseHookVersion(readFileStr(t, hook)); got != "2.0.0" {
		t.Errorf("hook not restamped to v2.0.0, got %q", got)
	}
}

// TestHookShebangCompatible: appending our POSIX-sh block is safe only when the
// existing hook runs under a POSIX shell. A hook with an explicit non-sh
// interpreter (python, node, ...) must be recognized as incompatible so
// install-hooks skips it rather than corrupting it. A missing shebang is treated
// as compatible (git runs a hook via sh by default).
func TestHookShebangCompatible(t *testing.T) {
	cases := []struct {
		content string
		want    bool
	}{
		{"#!/bin/sh\necho hi\n", true},
		{"#!/bin/bash -e\n", true},
		{"#!/usr/bin/env bash\n", true},
		{"#!/usr/bin/env sh\n", true},
		{"#!/bin/dash\n", true},
		{"echo no shebang here\n", true},
		{"", true},
		{"#!/usr/bin/env python3\nprint('x')\n", false},
		{"#!/usr/bin/node\n", false},
		{"#!/usr/bin/perl\n", false},
		{"#!/usr/bin/env\n", true}, // degenerate: no interpreter after env
	}
	for _, c := range cases {
		if got := hookShebangCompatible(c.content); got != c.want {
			t.Errorf("hookShebangCompatible(%q) = %v, want %v", c.content, got, c.want)
		}
	}
}

// TestShimQuotedPathNormalizesSlashes: the hook shim embeds the binary path for
// a POSIX-sh hook, so a Windows backslash path must be normalized to forward
// slashes (Git for Windows runs hooks under MSYS sh, where "\" is an escape and
// C:\... is fragile). ToSlash is a no-op on an already-slashed Unix path.
func TestShimQuotedPathNormalizesSlashes(t *testing.T) {
	cases := []struct{ in, want string }{
		{`C:\Users\dev\go\bin\side-quest.exe`, `"C:/Users/dev/go/bin/side-quest.exe"`},
		{"/usr/local/bin/side-quest", `"/usr/local/bin/side-quest"`},
	}
	for _, c := range cases {
		if got := shimQuotedPath(c.in); got != c.want {
			t.Errorf("shimQuotedPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestAddRefspecMigratesOldConfig: a pre-sync install left the old
// refs/side-quest/*:refs/side-quest/* refspec on both fetch and push.
// addRefspec must migrate it — removing the old fetch+push entries, adding
// the new tracking-ref fetch refspec, keeping HEAD as push, and never
// re-adding a quest push refspec (the pre-push hook publishes it) — and stay
// idempotent on a second call.
func TestAddRefspecMigratesOldConfig(t *testing.T) {
	dir := t.TempDir()
	g := gitcmd.New(dir)
	for _, args := range [][]string{
		{"init", "-q"},
		{"remote", "add", "origin", "https://example.com/x.git"},
		// simulate a pre-sync install:
		{"config", "--add", "remote.origin.fetch", "refs/side-quest/*:refs/side-quest/*"},
		{"config", "--add", "remote.origin.push", "HEAD"},
		{"config", "--add", "remote.origin.push", "refs/side-quest/*:refs/side-quest/*"},
	} {
		if _, err := g.Run(args...); err != nil {
			t.Fatal(err)
		}
	}

	addRefspec(g)

	fetch, _ := g.Run("config", "--get-all", "remote.origin.fetch")
	if strings.Contains(fetch, "refs/side-quest/*:refs/side-quest/*") {
		t.Errorf("old fetch refspec not removed:\n%s", fetch)
	}
	if !strings.Contains(fetch, store.FetchRefspec) {
		t.Errorf("new fetch refspec missing:\n%s", fetch)
	}
	push, _ := g.Run("config", "--get-all", "remote.origin.push")
	if strings.Contains(push, "refs/side-quest/*:refs/side-quest/*") {
		t.Errorf("old quest push refspec not removed:\n%s", push)
	}
	if !strings.Contains(push, "HEAD") {
		t.Errorf("HEAD push refspec should remain:\n%s", push)
	}

	// idempotent: a second call does not duplicate or re-add the old ones
	addRefspec(g)
	fetch2, _ := g.Run("config", "--get-all", "remote.origin.fetch")
	if strings.Count(fetch2, store.FetchRefspec) != 1 {
		t.Errorf("fetch refspec duplicated:\n%s", fetch2)
	}
}
