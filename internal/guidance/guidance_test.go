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
	if len(Core) > 1200 {
		t.Errorf("guidance.Core is %d bytes; keep it under 1200 (always-on context)", len(Core))
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
