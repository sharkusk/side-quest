package main

import (
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/sharkusk/side-quest/internal/config"
)

// TestSuperfanHintFiresOnce verifies the dcc-superfan fallback: when the tone is
// requested but the user's superfan file is absent, newVoice prints the hint to
// stderr exactly once no matter how many times it is called (the sync.Once), and
// falls back to dcc.
func TestSuperfanHintFiresOnce(t *testing.T) {
	// A home with no superfan file, so superfanFileExists() is false.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SIDE_QUEST_TONE", "") // no env override; use the tone we pass

	// Reset the package-global Once so this test controls the first fire.
	superfanHintOnce = sync.Once{}
	t.Cleanup(func() { superfanHintOnce = sync.Once{} })

	// Capture stderr around the calls.
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w

	v1 := newVoice(config.ToneDCCSuperfan)
	newVoice(config.ToneDCCSuperfan) // second call must not print again

	w.Close()
	os.Stderr = old
	out, _ := io.ReadAll(r)

	got := string(out)
	if n := strings.Count(got, "no superfan file"); n != 1 {
		t.Fatalf("superfan hint printed %d times, want exactly 1:\n%s", n, got)
	}
	// Superfan collapses to dcc, so the returned voice renders dcc lines.
	if v1.QuestCreated("SQ-1") == "created SQ-1" {
		t.Error("superfan fallback should render dcc, not plain")
	}
}
