# Voice / Tone System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a tone-selected voice layer that renders the CLI's human-facing confirmations and warnings in a `plain`/`dcc`/`dcc-superfan` register, while keeping all machine output neutral.

**Architecture:** A new pure `internal/voice` package holds Go-literal `plain`/`dcc` line pools keyed by an internal message enum, picks lines via an injectable random source (deterministic in tests), and exposes typed per-message methods. The CLI resolves the tone (`SIDE_QUEST_TONE` env > on-ref config > default `dcc`), builds a `Voice`, and calls it at the six confirmation/warning sites. `dcc-superfan` gets recognition + fallback-to-`dcc` + a one-time hint only (no verbatim line wiring this phase). `--json`, data displays, and errors never touch the voice layer.

**Tech Stack:** Go (stdlib only — `math/rand`, `os`, `fmt`, `sync`); existing `internal/config`, `internal/quest`, `internal/store`.

## Global Constraints

- **Three tones:** `plain`, `dcc` (default), `dcc-superfan` — values already defined in `internal/config` as `TonePlain`/`ToneDCC`/`ToneDCCSuperfan`.
- **Tone precedence:** `SIDE_QUEST_TONE` env → on-ref `config.Tone` → default `dcc`. An invalid/empty env value is ignored (never blocks a command).
- **Machine output is always neutral:** `--json`/`emitJSON` output is byte-identical regardless of tone. Data displays (`renderShow`, the populated `list` table, `config get`) and error/usage messages and the `require_quest` **block** stay neutral. Flavor is confined to confirmations and the assisted-mode warn.
- **Shipped `dcc` pool is ORIGINAL homage — NO verbatim *Dungeon Crawler Carl* text** (keeps the public repo IP-clean). Verbatim content lives only in the un-shipped, user-supplied superfan file; the shipped `superfan-lines.example.txt` is comments only.
- **`dcc-superfan` this phase = recognition + fallback + one-time hint only.** No superfan lines feed messages yet; when the file is absent, fall back to `dcc` and hint once per process.
- **Commit messages** end with the two footer lines:
  `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`
  and `Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws`.
- **Do not merge or push.** Per-task commits land on the feature branch only; merging/pushing waits for an explicit human request.

---

## Task 1: Config — `Tone.Valid()`, `store.SetTone`, `config set tone`

**Files:**
- Modify: `internal/config/config.go` (add `Tone.Valid`)
- Modify: `internal/store/store.go` (add `SetTone`, mirroring `SetStrategy` at line ~525)
- Modify: `cmd/side-quest/cli.go` (add `tone` case in `cmdConfigSet` ~266-286)
- Test: `internal/config/config_test.go`, `internal/store/store_test.go`, `cmd/side-quest/cli_test.go`

**Interfaces:**
- Consumes: existing `config.Tone` constants; `store.mutate`/`config.Marshal`/`configPath` (as used by `SetStrategy`).
- Produces: `func (t config.Tone) Valid() bool`; `func (s *store.Store) SetTone(t config.Tone) error`; a `config set tone <plain|dcc|dcc-superfan>` CLI path.

- [ ] **Step 1: Write the failing test — `Tone.Valid`**

In `internal/config/config_test.go`:

```go
func TestToneValid(t *testing.T) {
	for _, tn := range []Tone{TonePlain, ToneDCC, ToneDCCSuperfan} {
		if !tn.Valid() {
			t.Errorf("Tone(%q).Valid() = false, want true", tn)
		}
	}
	for _, bad := range []Tone{"", "loud", "DCC"} {
		if Tone(bad).Valid() {
			t.Errorf("Tone(%q).Valid() = true, want false", bad)
		}
	}
}
```

- [ ] **Step 2: Run it — expect FAIL**

Run: `go test ./internal/config/ -run TestToneValid`
Expected: FAIL — `t.Valid undefined (type Tone has no field or method Valid)`.

- [ ] **Step 3: Implement `Tone.Valid`**

In `internal/config/config.go`, after the `Tone` constants:

```go
// Valid reports whether t is one of the known tones.
func (t Tone) Valid() bool {
	switch t {
	case TonePlain, ToneDCC, ToneDCCSuperfan:
		return true
	}
	return false
}
```

- [ ] **Step 4: Run it — expect PASS**

Run: `go test ./internal/config/ -run TestToneValid`
Expected: PASS.

- [ ] **Step 5: Write the failing test — `store.SetTone`**

In `internal/store/store_test.go` (uses the existing `newStore(t)` helper):

```go
func TestSetTone(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTone(config.TonePlain); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.Config()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tone != config.TonePlain {
		t.Errorf("Tone = %q, want plain", cfg.Tone)
	}
}
```

(Ensure `internal/config` is imported in the test file; it is already used elsewhere in the package tests.)

- [ ] **Step 6: Run it — expect FAIL**

Run: `go test ./internal/store/ -run TestSetTone`
Expected: FAIL — `s.SetTone undefined`.

- [ ] **Step 7: Implement `store.SetTone`**

In `internal/store/store.go`, directly after `SetStrategy`:

```go
// SetTone persists the human-facing voice tone in the on-ref config.
func (s *Store) SetTone(t config.Tone) error {
	return s.mutate("side-quest: set tone "+string(t), func(snap *Snapshot, tx *txn) error {
		cfg := snap.Config
		cfg.Tone = t
		data, err := config.Marshal(cfg)
		if err != nil {
			return err
		}
		tx.put(configPath, data)
		return nil
	})
}
```

- [ ] **Step 8: Run it — expect PASS**

Run: `go test ./internal/store/ -run TestSetTone`
Expected: PASS.

- [ ] **Step 9: Write the failing test — `config set tone`**

In `cmd/side-quest/cli_test.go`, following the existing config-set test pattern in that file (use the same temp-repo/`openStore` harness the other `cmdConfigSet` tests use):

```go
func TestConfigSetTone(t *testing.T) {
	withTempRepo(t) // same helper the other cli config tests use to chdir into an initialized repo
	if err := cmdConfigSet([]string{"tone", "plain"}); err != nil {
		t.Fatalf("set tone plain: %v", err)
	}
	if err := cmdConfigSet([]string{"tone", "loud"}); err == nil {
		t.Fatal("set tone loud: want error, got nil")
	}
}
```

If the existing tests use a different harness name than `withTempRepo`, match theirs; the assertion (valid accepted, invalid rejected) is what matters.

- [ ] **Step 10: Run it — expect FAIL**

Run: `go test ./cmd/side-quest/ -run TestConfigSetTone`
Expected: FAIL — `unknown config key "tone"`.

- [ ] **Step 11: Add the `tone` case to `cmdConfigSet`**

In `cmd/side-quest/cli.go`, inside the `switch key` of `cmdConfigSet`, before `default:`:

```go
	case "tone":
		tn := config.Tone(val)
		if !tn.Valid() {
			return fmt.Errorf("invalid tone %q (want plain|dcc|dcc-superfan)", val)
		}
		return s.SetTone(tn)
```

And update the `default:` message to list `tone`:

```go
	default:
		return fmt.Errorf("unknown config key %q (settable: require_quest, auto_trailer, id_strategy, tone)", key)
```

- [ ] **Step 12: Run it — expect PASS, then the package**

Run: `go test ./cmd/side-quest/ -run TestConfigSetTone && go test ./internal/config/ ./internal/store/ ./cmd/side-quest/`
Expected: PASS.

- [ ] **Step 13: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/store/store.go internal/store/store_test.go cmd/side-quest/cli.go cmd/side-quest/cli_test.go
git commit -m "$(cat <<'EOF'
feat(config): make tone settable and validated

Add Tone.Valid(), store.SetTone, and a `config set tone` case
(plain|dcc|dcc-superfan) — the config prerequisite for the voice layer.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws
EOF
)"
```

---

## Task 2: The `internal/voice` package

**Files:**
- Create: `internal/voice/pools.go` (message-key enum + `plain`/`dcc` line pools)
- Create: `internal/voice/voice.go` (`Voice`, source seam, `New`, typed methods, `ResolveTone`, `EffectiveTone`)
- Test: `internal/voice/voice_test.go`

**Interfaces:**
- Consumes: `config.Tone` (+ its constants and `Valid`), `quest.Status` (+ its constants).
- Produces:
  - `func New(tone config.Tone) *Voice`
  - methods: `(*Voice) QuestCreated(id string) string`, `(*Voice) StatusSet(id string, st quest.Status) string`, `(*Voice) MissingTrailer() string`, `(*Voice) EmptyList() string`, `(*Voice) Initialized() string`, `(*Voice) HooksInstalled(dir string) string`
  - `func ResolveTone(env string, cfg config.Tone) config.Tone`
  - `func EffectiveTone(configured config.Tone, superfanExists bool) (config.Tone, bool)`

- [ ] **Step 1: Write the pools file**

Create `internal/voice/pools.go`. The `dcc` lines are original homage (no verbatim source text). Keys that interpolate (`QuestCreated`, the five statuses, `HooksInstalled`) contain exactly one `%s`; the rest contain none.

```go
// Package voice renders the CLI's human-facing confirmations and warnings in a
// selected tone. It never touches machine (--json) output. All randomness flows
// through an injectable source so tests are deterministic.
package voice

import "github.com/sharkusk/side-quest/internal/config"

// msgKey identifies a message point. Internal: callers use the typed methods.
type msgKey int

const (
	keyQuestCreated msgKey = iota
	keyStatusOpen
	keyStatusPartial
	keyStatusDone
	keyStatusDeferred
	keyStatusDiscarded
	keyMissingTrailer
	keyEmptyList
	keyInitialized
	keyHooksInstalled
)

// pools maps tone -> key -> candidate lines. plain has exactly one neutral line
// per key; dcc has several so output varies (discarded/deferred get the most
// character). Lines for interpolating keys carry a single %s.
var pools = map[config.Tone]map[msgKey][]string{
	config.TonePlain: {
		keyQuestCreated:   {"created %s"},
		keyStatusOpen:     {"%s -> open"},
		keyStatusPartial:  {"%s -> partial"},
		keyStatusDone:     {"%s -> done"},
		keyStatusDeferred: {"%s -> deferred"},
		keyStatusDiscarded: {"%s -> discarded"},
		keyMissingTrailer: {"side-quest: no Quest: trailer on this commit. (Add 'Quest: none' to silence.)"},
		keyEmptyList:      {"no quests"},
		keyInitialized:    {"side-quest: initialized"},
		keyHooksInstalled: {"side-quest: hooks installed in %s"},
	},
	config.ToneDCC: {
		keyQuestCreated: {
			"The System logs a new side quest: %s. The audience stirs.",
			"A new objective materializes: %s. Try not to die.",
			"Quest %s enters the dungeon. Somewhere, a sponsor takes note.",
		},
		keyStatusOpen: {
			"%s is live again. Back into the fray, crawler.",
			"%s reopened. The System raises an eyebrow.",
		},
		keyStatusPartial: {
			"Progress on %s. Don't get cocky.",
			"%s: partial credit. The System is unimpressed but noting it.",
		},
		keyStatusDone: {
			"%s cleared. The crowd goes wild.",
			"Objective %s complete. Loot box incoming.",
			"%s done. The System deducts one excuse from your ledger.",
		},
		keyStatusDeferred: {
			"%s deferred. 'Later' is a beautiful lie, crawler.",
			"%s shelved under 'someday, probably never.' The System has seen this before.",
			"You postpone %s. The dungeon is patient. The dungeon remembers.",
		},
		keyStatusDiscarded: {
			"%s discarded. Even the sponsors have standards.",
			"%s tossed into the lava. Nobody will speak of it again.",
			"You abandon %s. The audience boos. A sponsor quietly unsubscribes.",
		},
		keyMissingTrailer: {
			"No Quest: trailer on this commit. The System notices everything. (Add 'Quest: none' to silence.)",
			"This commit names no quest. The audience whispers. (Add 'Quest: none' to silence.)",
		},
		keyEmptyList: {
			"No quests. The dungeon is quiet. Too quiet.",
			"Zero side quests. Either you're finished or you're doomed.",
		},
		keyInitialized: {
			"side-quest online. Welcome to the floor, crawler.",
			"The System boots up. Let the crawl begin.",
		},
		keyHooksInstalled: {
			"Hooks installed in %s. The System now watches your commits.",
			"Git hooks wired into %s. Nowhere to hide, crawler.",
		},
	},
}
```

- [ ] **Step 2: Write the failing test**

Create `internal/voice/voice_test.go`:

```go
package voice

import (
	"strings"
	"testing"

	"github.com/sharkusk/side-quest/internal/config"
	"github.com/sharkusk/side-quest/internal/quest"
)

// fixedSource returns a constant index (mod n) so line choice is deterministic.
type fixedSource int

func (f fixedSource) intn(n int) int { return int(f) % n }

func TestPickDeterministic(t *testing.T) {
	v := &Voice{tone: config.ToneDCC, src: fixedSource(0)}
	got := v.QuestCreated("SQ-0007")
	want := "The System logs a new side quest: SQ-0007. The audience stirs."
	if got != want {
		t.Errorf("QuestCreated = %q, want %q", got, want)
	}
}

func TestPlainNeutralAndContainsData(t *testing.T) {
	v := New(config.TonePlain)
	if got := v.QuestCreated("SQ-1"); got != "created SQ-1" {
		t.Errorf("plain QuestCreated = %q", got)
	}
	if got := v.HooksInstalled("/x"); !strings.Contains(got, "/x") {
		t.Errorf("plain HooksInstalled missing dir: %q", got)
	}
}

func TestNoFormatErrorsAllTonesAllMethods(t *testing.T) {
	for _, tone := range []config.Tone{config.TonePlain, config.ToneDCC} {
		v := New(tone)
		outs := []string{
			v.QuestCreated("SQ-1"),
			v.StatusSet("SQ-1", quest.StatusOpen),
			v.StatusSet("SQ-1", quest.StatusPartial),
			v.StatusSet("SQ-1", quest.StatusDone),
			v.StatusSet("SQ-1", quest.StatusDeferred),
			v.StatusSet("SQ-1", quest.StatusDiscarded),
			v.MissingTrailer(),
			v.EmptyList(),
			v.Initialized(),
			v.HooksInstalled("/d"),
		}
		for _, o := range outs {
			if o == "" || strings.Contains(o, "%!") {
				t.Errorf("tone %q produced bad output %q", tone, o)
			}
		}
	}
}

func TestDCCKeysNonEmpty(t *testing.T) {
	for k := keyQuestCreated; k <= keyHooksInstalled; k++ {
		if len(pools[config.ToneDCC][k]) == 0 {
			t.Errorf("dcc pool missing key %d", k)
		}
	}
}

func TestResolveTone(t *testing.T) {
	if got := ResolveTone("plain", config.ToneDCC); got != config.TonePlain {
		t.Errorf("valid env should win: got %q", got)
	}
	if got := ResolveTone("", config.TonePlain); got != config.TonePlain {
		t.Errorf("empty env -> config: got %q", got)
	}
	if got := ResolveTone("bogus", config.ToneDCC); got != config.ToneDCC {
		t.Errorf("invalid env ignored -> config: got %q", got)
	}
}

func TestEffectiveTone(t *testing.T) {
	if tn, hint := EffectiveTone(config.ToneDCCSuperfan, false); tn != config.ToneDCC || !hint {
		t.Errorf("superfan+absent = (%q,%v), want (dcc,true)", tn, hint)
	}
	if tn, hint := EffectiveTone(config.ToneDCCSuperfan, true); tn != config.ToneDCC || hint {
		t.Errorf("superfan+present = (%q,%v), want (dcc,false)", tn, hint)
	}
	if tn, hint := EffectiveTone(config.TonePlain, false); tn != config.TonePlain || hint {
		t.Errorf("plain = (%q,%v), want (plain,false)", tn, hint)
	}
}
```

- [ ] **Step 3: Run it — expect FAIL (compile)**

Run: `go test ./internal/voice/`
Expected: FAIL — `undefined: Voice`, `New`, `ResolveTone`, `EffectiveTone`.

- [ ] **Step 4: Implement `voice.go`**

Create `internal/voice/voice.go`:

```go
package voice

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/sharkusk/side-quest/internal/config"
	"github.com/sharkusk/side-quest/internal/quest"
)

// source is the randomness seam; tests inject a deterministic implementation.
type source interface{ intn(n int) int }

type mathSource struct{ r *rand.Rand }

func (m mathSource) intn(n int) int { return m.r.Intn(n) }

// Voice renders human-facing CLI strings in a fixed tone.
type Voice struct {
	tone config.Tone
	src  source
}

// New builds a Voice. It only renders plain or dcc; dcc-superfan (and any
// unknown value) collapses to dcc — the caller handles the superfan fallback
// hint via EffectiveTone before calling New.
func New(tone config.Tone) *Voice {
	if tone != config.TonePlain {
		tone = config.ToneDCC
	}
	return &Voice{tone: tone, src: mathSource{rand.New(rand.NewSource(time.Now().UnixNano()))}}
}

func (v *Voice) pick(k msgKey) string {
	lines := pools[v.tone][k]
	if len(lines) == 0 {
		lines = pools[config.TonePlain][k]
	}
	if len(lines) == 1 {
		return lines[0]
	}
	return lines[v.src.intn(len(lines))]
}

func statusKey(st quest.Status) msgKey {
	switch st {
	case quest.StatusPartial:
		return keyStatusPartial
	case quest.StatusDone:
		return keyStatusDone
	case quest.StatusDeferred:
		return keyStatusDeferred
	case quest.StatusDiscarded:
		return keyStatusDiscarded
	default:
		return keyStatusOpen
	}
}

// QuestCreated confirms a new quest.
func (v *Voice) QuestCreated(id string) string { return fmt.Sprintf(v.pick(keyQuestCreated), id) }

// StatusSet confirms a status transition.
func (v *Voice) StatusSet(id string, st quest.Status) string {
	return fmt.Sprintf(v.pick(statusKey(st)), id)
}

// MissingTrailer is the assisted-mode commit warning.
func (v *Voice) MissingTrailer() string { return v.pick(keyMissingTrailer) }

// EmptyList is shown when a list has no quests.
func (v *Voice) EmptyList() string { return v.pick(keyEmptyList) }

// Initialized confirms `side-quest init`.
func (v *Voice) Initialized() string { return v.pick(keyInitialized) }

// HooksInstalled confirms hook installation.
func (v *Voice) HooksInstalled(dir string) string { return fmt.Sprintf(v.pick(keyHooksInstalled), dir) }

// ResolveTone applies the SIDE_QUEST_TONE override: a valid env value wins,
// otherwise the config value is used. Invalid/empty env is ignored.
func ResolveTone(env string, cfg config.Tone) config.Tone {
	if t := config.Tone(env); t.Valid() {
		return t
	}
	return cfg
}

// EffectiveTone resolves dcc-superfan to its rendered tone. Superfan line-wiring
// is deferred, so it always renders as dcc; the bool is true when the caller
// should print the one-time "superfan file not found" hint (superfan requested
// but the file is absent).
func EffectiveTone(configured config.Tone, superfanExists bool) (config.Tone, bool) {
	if configured == config.ToneDCCSuperfan {
		return config.ToneDCC, !superfanExists
	}
	return configured, false
}
```

- [ ] **Step 5: Run it — expect PASS**

Run: `go test ./internal/voice/`
Expected: PASS (all tests).

- [ ] **Step 6: Commit**

```bash
git add internal/voice/
git commit -m "$(cat <<'EOF'
feat(voice): tone-selected message pools and typed render methods

New internal/voice package: Go-literal plain/dcc pools, injectable random
source, typed per-message methods, plus ResolveTone/EffectiveTone. dcc lines
are original DCC-style homage, no verbatim source text.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws
EOF
)"
```

---

## Task 3: Wire the voice layer into the CLI

**Files:**
- Create: `cmd/side-quest/voice.go` (tone-resolution helpers + superfan hint)
- Modify: `cmd/side-quest/cli.go` (`cmdInit`, `cmdNew`, `cmdStatus`, and `cmdList`'s call to `renderList`)
- Modify: `cmd/side-quest/render.go` (`renderList` takes a `*voice.Voice` for the empty case)
- Modify: `cmd/side-quest/main.go` (commit-msg `Warn` case)
- Modify: `cmd/side-quest/hooks.go` (hooks-installed line)
- Test: `cmd/side-quest/cli_test.go`

**Interfaces:**
- Consumes: `voice.New`, `voice.ResolveTone`, `voice.EffectiveTone`, and the `Voice` methods from Task 2; `config.ToneDCC`; `store.Store.Config`.
- Produces (package-internal): `func bestEffortVoice() *voice.Voice`, `func voiceFor(s *store.Store) *voice.Voice`, `func superfanPath() string`, `func superfanFileExists() bool`.

- [ ] **Step 1: Write the resolution helpers**

Create `cmd/side-quest/voice.go`:

```go
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
```

- [ ] **Step 2: Wire the confirmation sites in `cli.go`**

In `cmdInit`, replace `fmt.Println("side-quest: initialized")` with:

```go
	fmt.Println(voiceFor(s).Initialized())
```

In `cmdNew`, replace the final `fmt.Println(q.ID)` (the human, non-`--json` path) with:

```go
	fmt.Println(voiceFor(s).QuestCreated(q.ID))
```

In `cmdStatus`, replace `return s.SetStatus(args[0], quest.Status(args[1]))` with:

```go
	if err := s.SetStatus(args[0], quest.Status(args[1])); err != nil {
		return err
	}
	fmt.Println(voiceFor(s).StatusSet(args[0], quest.Status(args[1])))
	return nil
```

- [ ] **Step 3: Wire `renderList` empty case in `render.go`**

Change `renderList` to accept a Voice for the empty line:

```go
func renderList(w io.Writer, quests []*quest.Quest, v *voice.Voice) {
	if len(quests) == 0 {
		fmt.Fprintln(w, v.EmptyList())
		return
	}
	// ... unchanged table code ...
}
```

Add `"github.com/sharkusk/side-quest/internal/voice"` to `render.go` imports. In `cmdList` (cli.go), update the call from `renderList(os.Stdout, quests)` to `renderList(os.Stdout, quests, voiceFor(s))`.

- [ ] **Step 4: Wire the commit-msg warn in `main.go`**

In the commit-msg handler, capture the tone alongside `requireQuest` and flavor the `Warn` line (the `Reject` block stays neutral):

```go
	tone := config.ToneDCC
	requireQuest := false
	if s, err := openStore(); err == nil {
		if cfg, err := s.Config(); err == nil {
			requireQuest = cfg.RequireQuest
			tone = cfg.Tone
		}
	}
	switch trailer.Decision(string(msg), requireQuest) {
	case trailer.Reject:
		fmt.Fprintln(os.Stderr, "side-quest: no Quest:/Completes: trailer and require_quest is on — commit blocked.")
		fmt.Fprintln(os.Stderr, "  Add e.g.  Quest: SQ-0001   (or  Quest: none  for a genuine chore).")
		os.Exit(1)
	case trailer.Warn:
		fmt.Fprintln(os.Stderr, newVoice(tone).MissingTrailer())
	}
```

Add `"github.com/sharkusk/side-quest/internal/config"` to `main.go` imports if not present.

- [ ] **Step 5: Wire the hooks line in `hooks.go`**

Replace `fmt.Println("side-quest: hooks installed in", hooksDir)` with:

```go
	fmt.Println(bestEffortVoice().HooksInstalled(hooksDir))
```

- [ ] **Step 6: Write the failing tests**

In `cmd/side-quest/cli_test.go` (match the file's existing stdout-capture / temp-repo harness; the snippet below shows intent — adapt helper names to the ones already in that file):

```go
func TestNewJSONNeutralAcrossTones(t *testing.T) {
	withTempRepo(t)
	capture := func(tone string) string {
		t.Setenv("SIDE_QUEST_TONE", tone)
		return captureStdout(t, func() { _ = cmdNew([]string{"--json", "A title"}) })
	}
	// --json output must not depend on tone. Compare stable fields, not the id
	// (ids differ per create); create separate quests but assert the JSON shape
	// is byte-identical after masking the volatile id/created fields, OR assert
	// neither output contains any dcc flavor words.
	for _, tone := range []string{"plain", "dcc", ""} {
		out := capture(tone)
		if strings.Contains(out, "System") || strings.Contains(out, "crawler") || strings.Contains(out, "dungeon") {
			t.Errorf("--json under tone %q leaked flavor: %s", tone, out)
		}
	}
}

func TestNewHumanFlavoredContainsID(t *testing.T) {
	withTempRepo(t)
	t.Setenv("SIDE_QUEST_TONE", "plain")
	out := captureStdout(t, func() { _ = cmdNew([]string{"A title"}) })
	if !strings.Contains(out, "SQ-") {
		t.Errorf("human new output missing id: %q", out)
	}
}
```

If the file has no `captureStdout`/`withTempRepo` helpers, add a minimal stdout-capture helper local to the test file; do not modify unrelated tests.

- [ ] **Step 7: Run it — expect FAIL, then implement is already done; expect PASS**

Run: `go test ./cmd/side-quest/`
Expected: the two new tests PASS and the whole package still passes. (If `renderList` callers in existing tests broke due to the new parameter, update those call sites to pass `voiceFor(s)` or `voice.New(config.ToneDCC)` — that is part of this task.)

- [ ] **Step 8: Full build + suite**

Run: `go build ./... && go test ./...`
Expected: PASS across all packages.

- [ ] **Step 9: Commit**

```bash
git add cmd/side-quest/
git commit -m "$(cat <<'EOF'
feat(cli): render confirmations and warnings through the voice layer

Resolve tone (SIDE_QUEST_TONE > config > dcc) and flavor init, new, status,
empty-list, the assisted commit warn, and hooks-installed. --json, data
displays, and errors stay neutral. dcc-superfan falls back to dcc with a
one-time hint when its file is absent.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws
EOF
)"
```

---

## Task 4: Living docs + superfan example file

**Files:**
- Create: `superfan-lines.example.txt` (repo root; comments only, no verbatim content)
- Modify: `docs/architecture.md` (voice-layer subsection)
- Modify: `README.md` (tone paragraph + Credits & permissions note)

**Interfaces:** none (documentation).

- [ ] **Step 1: Create the example file**

Create `superfan-lines.example.txt` at the repo root:

```text
# side-quest superfan lines (EXAMPLE — ships empty on purpose)
#
# This file is where you may place your OWN verbatim Dungeon Crawler Carl
# catch phrases for the `dcc-superfan` tone. It is intentionally empty.
#
# - Verbatim book/show text is NEVER shipped in this repository.
# - Copy this file to  ~/.config/side-quest/superfan-lines.txt  and add one
#   phrase per line. Lines beginning with '#' and blank lines are ignored.
# - Using verbatim phrases publicly (committed or shared) requires permission
#   from the author, Matt Dinniman. See README "Credits & permissions".
#
# Until this file exists at the path above, `tone: dcc-superfan` behaves as
# `dcc` and prints a one-time hint naming the expected location.
```

- [ ] **Step 2: Update `docs/architecture.md`**

Add a "Voice layer" subsection describing: the `internal/voice` package boundary (pure, pools + injectable source + typed methods); tone precedence `SIDE_QUEST_TONE` > on-ref config > default `dcc`; the neutral-path rule (`--json`, data displays, errors never flavored); and the `dcc-superfan` status (recognition + fallback-to-`dcc` + one-time hint; verbatim lines un-shipped and unwired this phase). Match the file's existing heading style and place it near the CLI/rendering discussion.

- [ ] **Step 3: Update `README.md`**

Add a short "Tone" paragraph: the three tones (`plain` / `dcc` default / `dcc-superfan`), how to set them (`side-quest config set tone <value>` or the `SIDE_QUEST_TONE` env override), and the guarantee that `--json` output is always neutral. Then add a "Credits & permissions" note: side-quest's `dcc` voice is an original homage to *Dungeon Crawler Carl* by Matt Dinniman with no verbatim text; verbatim phrases are never shipped and load only from the user's own `~/.config/side-quest/superfan-lines.txt`; public/committed use of verbatim phrases requires the author's permission.

- [ ] **Step 4: Verify build (docs don't break anything) and view diff**

Run: `go build ./... && git status --short`
Expected: build OK; the three doc/example files staged-ready.

- [ ] **Step 5: Commit**

```bash
git add superfan-lines.example.txt docs/architecture.md README.md
git commit -m "$(cat <<'EOF'
docs(voice): document the tone system and ship superfan example

Architecture voice-layer subsection, README tone paragraph + Credits &
permissions note, and an empty superfan-lines.example.txt (no verbatim text).

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws
EOF
)"
```

---

## Self-Review (planner)

- **Spec coverage:** §1 voice package (pools/source/typed methods) → Task 2. §2 tone resolution/precedence → `ResolveTone` (Task 2) + `newVoice` (Task 3). §3 the six message points → Task 3 Steps 2-5. §4 neutral paths (`--json`/data/errors) → Task 3 keeps `emitJSON`/`renderShow`/table/`config get`/error paths untouched; asserted by `TestNewJSONNeutralAcrossTones`. §5 dcc-superfan recognition+fallback+hint + example file + Credits note → `EffectiveTone` (Task 2), `newVoice` hint (Task 3), example file + README (Task 4). §6 config `Tone.Valid` + `config set tone` → Task 1. §7 wiring → Task 3. Testing bullets → Tasks 2 & 3 tests. Living docs → Task 4. All covered.
- **Placeholder scan:** none — every code step carries full code. Test steps that must adapt to existing harness names (`withTempRepo`/`captureStdout`) say so explicitly and give the intent + assertions.
- **Type consistency:** `Voice` methods, `ResolveTone`, `EffectiveTone`, `New`, `SetTone`, `Tone.Valid` signatures match between the package (Task 2), config/store (Task 1), and all call sites (Task 3). `renderList`'s new `*voice.Voice` parameter is threaded at its one caller and any test callers (called out in Task 3 Step 7).
- **Note on the docs warn-hook:** Tasks 2-3 touch `internal/**`/`cmd/**` without docs; the warn-only `.githooks/pre-commit` may remind — that is expected, docs land in Task 4 before merge (living-docs-same-branch).
