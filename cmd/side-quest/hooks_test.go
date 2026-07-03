package main

import "testing"

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
