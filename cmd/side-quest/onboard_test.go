package main

import (
	"strings"
	"testing"
)

// agents-md prints the canonical, paste-ready agent guidance. It needs no repo
// and no init — the content is embedded in the binary.
func TestAgentsMdPrintsGuidance(t *testing.T) {
	bin := buildBinary(t)
	out, code := runBin(t, bin, t.TempDir(), "agents-md")
	if code != 0 {
		t.Fatalf("agents-md exit=%d out=%q", code, out)
	}
	for _, want := range []string{"Quest: SQ-", "Completes: SQ-", "Quest: none", "quest_new"} {
		if !strings.Contains(out, want) {
			t.Errorf("agents-md output missing %q", want)
		}
	}
}
