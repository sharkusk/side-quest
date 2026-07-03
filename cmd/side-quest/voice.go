package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/sharkusk/side-quest/internal/config"
	"github.com/sharkusk/side-quest/internal/store"
	"github.com/sharkusk/side-quest/internal/voice"
)

var superfanHintOnce sync.Once

// superfanPath is the fixed default location for the user's verbatim line file.
func superfanPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "~/.config/side-quest/superfan-lines.txt"
	}
	return filepath.Join(home, ".config", "side-quest", "superfan-lines.txt")
}

func superfanFileExists() bool {
	info, err := os.Stat(superfanPath())
	return err == nil && !info.IsDir()
}

// newVoice resolves the effective tone (SIDE_QUEST_TONE env > cfgTone > dcc)
// and returns a Voice. When dcc-superfan is requested but the user's line file
// is absent, it prints a one-time hint to stderr and falls back to dcc.
func newVoice(cfgTone config.Tone) *voice.Voice {
	tone := voice.ResolveTone(os.Getenv("SIDE_QUEST_TONE"), cfgTone)
	eff, hint := voice.EffectiveTone(tone, superfanFileExists())
	if hint {
		superfanHintOnce.Do(func() {
			fmt.Fprintln(os.Stderr, "side-quest: tone dcc-superfan set but no superfan file at "+superfanPath()+" — using dcc. See superfan-lines.example.txt.")
		})
	}
	return voice.New(eff)
}

// voiceFor builds a Voice from an already-open store's config tone.
func voiceFor(s *store.Store) *voice.Voice {
	tone := config.ToneDCC
	if cfg, err := s.Config(); err == nil {
		tone = cfg.Tone
	}
	return newVoice(tone)
}

// bestEffortVoice builds a Voice when no store is open yet (opens one best-effort).
func bestEffortVoice() *voice.Voice {
	tone := config.ToneDCC
	if s, err := openStore(); err == nil {
		if cfg, err := s.Config(); err == nil {
			tone = cfg.Tone
		}
	}
	return newVoice(tone)
}
