// Package config models the on-ref configuration stored at _config.yaml
// (spec §7, §12). It lives on the ref so every worktree and clone agrees on
// the id strategy, counter, and message tone.
package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Strategy selects how new ids are allocated.
type Strategy string

const (
	Sequential Strategy = "sequential" // SQ-0001, SQ-0002, ... (counter-based)
	Random     Strategy = "random"     // SQ-a3f9c2 (6 hex)
)

// Tone selects the message register (used by the voice layer in a later phase).
type Tone string

const (
	TonePlain       Tone = "plain"
	ToneDCC         Tone = "dcc"
	ToneDCCSuperfan Tone = "dcc-superfan"
)

// Config is the persisted configuration.
type Config struct {
	IDPrefix    string   `yaml:"id_prefix"`
	IDStrategy  Strategy `yaml:"id_strategy"`
	SeqNext     int      `yaml:"seq_next"`
	SeqWidth    int      `yaml:"seq_width"`
	Tone        Tone     `yaml:"tone"`
	AutoTrailer bool     `yaml:"auto_trailer"`
	// RequireQuest, when true, makes the commit-msg hook REJECT commits that
	// carry no Quest:/Completes: trailer (and no explicit `Quest: none`).
	// Default false = assisted mode (warn only).
	RequireQuest bool `yaml:"require_quest"`
}

// Default returns the configuration a freshly-initialized project starts with.
func Default() Config {
	return Config{
		IDPrefix:    "SQ",
		IDStrategy:  Sequential,
		SeqNext:     1,
		SeqWidth:    4,
		Tone:        ToneDCC,
		AutoTrailer: true,
	}
}

// Marshal renders c to YAML bytes.
func Marshal(c Config) ([]byte, error) { return yaml.Marshal(c) }

// Unmarshal parses YAML into a Config, starting from Default() so that keys
// absent in the file take their default value (forward/backward compatibility).
func Unmarshal(data []byte) (Config, error) {
	c := Default()
	if err := yaml.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	return c, nil
}
