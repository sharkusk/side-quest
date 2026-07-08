package commands

import (
	"strings"
	"testing"
)

// The embedded command must carry the managed marker (so install-cli can
// recognize its own copy to refresh) and still be a working capture command.
func TestSqEmbedsMarkerAndBody(t *testing.T) {
	if !strings.Contains(Sq, ManagedMarker) {
		t.Errorf("embedded sq.md is missing the managed marker %q", ManagedMarker)
	}
	for _, want := range []string{"quest_new", "$ARGUMENTS"} {
		if !strings.Contains(Sq, want) {
			t.Errorf("embedded sq.md missing %q", want)
		}
	}
}
