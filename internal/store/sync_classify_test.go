package store

import (
	"errors"
	"testing"
)

// TestIsNonFastForwardClassification (SQ-0124): only a DIVERGED push is
// retryable. A remote-side policy denial ("[remote rejected]", pre-receive hook
// or protected ref) must not be retried — the old bare "rejected" match burned
// the whole retry budget on an error that could never clear and then reported a
// misleading "stayed contended".
func TestIsNonFastForwardClassification(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"non-fast-forward", errors.New(`! [rejected] refs/side-quest/quests -> refs/side-quest/quests (non-fast-forward)`), true},
		{"fetch first", errors.New(`! [rejected] main -> main (fetch first)`), true},
		{"stale info", errors.New(`! [rejected] main -> main (stale info)`), true},
		{"remote hook denial", errors.New(`! [remote rejected] refs/side-quest/quests -> refs/side-quest/quests (pre-receive hook declined)`), false},
		{"permission denied", errors.New(`fatal: unable to access 'https://x/': The requested URL returned error: 403`), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isNonFastForward(c.err); got != c.want {
				t.Errorf("isNonFastForward(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}
