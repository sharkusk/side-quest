package main

import (
	"strings"
	"testing"

	"github.com/sharkusk/side-quest/internal/gitcmd"
	"github.com/sharkusk/side-quest/internal/store"
)

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
