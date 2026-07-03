package main

import "testing"

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
