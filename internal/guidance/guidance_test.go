package guidance

import (
	"strings"
	"testing"
)

// The core brief rides in always-on context, so it must stay compact — yet carry
// the tool names an agent needs to act on it.
func TestCoreIsCompactAndComplete(t *testing.T) {
	if Core == "" {
		t.Fatal("guidance.Core is empty")
	}
	if len(Core) > 1400 {
		t.Errorf("guidance.Core is %d bytes; keep it under 1400 (always-on context)", len(Core))
	}
	for _, want := range []string{"quest_new", "quest_set_current", "Completes:", "quest_list"} {
		if !strings.Contains(Core, want) {
			t.Errorf("guidance.Core missing %q", want)
		}
	}
	if strings.HasPrefix(Core, " ") || strings.HasSuffix(Core, "\n") {
		t.Error("guidance.Core must be whitespace-trimmed")
	}
}

// TestAgentsTemplateIsUnwrapped (SQ-0059): the embedded agents-guidance template
// must stay UNWRAPPED — the refresh markers are added at runtime by the emitter,
// never stored in the source. This is what makes onboard-ing side-quest's own repo
// unable to corrupt the embed source. It must also carry the core verbatim.
func TestAgentsTemplateIsUnwrapped(t *testing.T) {
	if strings.Contains(Agents, ">>> side-quest >>>") {
		t.Error("guidance.Agents contains a side-quest marker; the embed source must be unwrapped")
	}
	if !strings.Contains(Agents, Core) {
		t.Error("guidance.Agents must contain guidance.Core verbatim")
	}
}
