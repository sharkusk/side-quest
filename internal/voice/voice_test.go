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

// TestEveryPoolLineInterpolatesCleanly sweeps every line of every pool in every
// tone — not one random sample per method — so a single line with the wrong
// number of %s verbs can't slip through. fixedSource(i) forces pick to return
// line i, and each key is invoked through its real method with the exact arg
// count production uses, so a bad line surfaces as a "%!" Sprintf error.
func TestEveryPoolLineInterpolatesCleanly(t *testing.T) {
	invokers := map[msgKey]func(v *Voice) string{
		keyQuestCreated:    func(v *Voice) string { return v.QuestCreated("SQ-1") },
		keyStatusOpen:      func(v *Voice) string { return v.StatusSet("SQ-1", quest.StatusOpen) },
		keyStatusPartial:   func(v *Voice) string { return v.StatusSet("SQ-1", quest.StatusPartial) },
		keyStatusDone:      func(v *Voice) string { return v.StatusSet("SQ-1", quest.StatusDone) },
		keyStatusDeferred:  func(v *Voice) string { return v.StatusSet("SQ-1", quest.StatusDeferred) },
		keyStatusDiscarded: func(v *Voice) string { return v.StatusSet("SQ-1", quest.StatusDiscarded) },
		keyMissingTrailer:  func(v *Voice) string { return v.MissingTrailer() },
		keyEmptyList:       func(v *Voice) string { return v.EmptyList() },
		keyInitialized:     func(v *Voice) string { return v.Initialized() },
		keyHooksInstalled:  func(v *Voice) string { return v.HooksInstalled("/d") },
		keyNoteAdded:       func(v *Voice) string { return v.NoteAdded("SQ-1") },
		keyQuestSelected:   func(v *Voice) string { return v.QuestSelected("SQ-1") },
	}
	for tone, keys := range pools {
		for key, lines := range keys {
			invoke, ok := invokers[key]
			if !ok {
				t.Fatalf("no invoker for key %d (tone %q); add one so the sweep stays exhaustive", key, tone)
			}
			for i := range lines {
				v := &Voice{tone: tone, src: fixedSource(i)}
				got := invoke(v)
				if got == "" || strings.Contains(got, "%!") {
					t.Errorf("tone %q key %d line %d (%q) -> bad output %q", tone, key, i, lines[i], got)
				}
			}
		}
	}
}

// TestNoteAdded (SQ-0027): the note confirmation renders through the voice layer
// like its sibling mutations — plain stays the bland "noted <id>", dcc carries the
// id in a flavored line.
func TestNoteAdded(t *testing.T) {
	if got := New(config.TonePlain).NoteAdded("SQ-1"); got != "noted SQ-1" {
		t.Errorf("plain NoteAdded = %q, want 'noted SQ-1'", got)
	}
	v := &Voice{tone: config.ToneDCC, src: fixedSource(0)}
	if got := v.NoteAdded("SQ-7"); !strings.Contains(got, "SQ-7") {
		t.Errorf("dcc NoteAdded missing id: %q", got)
	}
}

// TestNoCrawlerInDCCVoice (SQ-0046): the dcc voice addresses the user as "coder",
// never "crawler" — a blanket guard so no line reintroduces the old word.
func TestNoCrawlerInDCCVoice(t *testing.T) {
	for key, lines := range pools[config.ToneDCC] {
		for _, l := range lines {
			if strings.Contains(strings.ToLower(l), "crawler") {
				t.Errorf("dcc key %d line %q still says 'crawler' (should be 'coder')", key, l)
			}
		}
	}
}

// TestErrorScenariosCarryCatchphrase (SQ-0046): every dcc error/warning line ends
// on our catchphrase. Today the voice layer's sole error scenario is the missing
// -trailer warning; new error keys join errorKeys so the guard stays exhaustive.
func TestErrorScenariosCarryCatchphrase(t *testing.T) {
	const catchphrase = "Claude Dammit Opus!"
	errorKeys := []msgKey{keyMissingTrailer}
	for _, k := range errorKeys {
		for i, l := range pools[config.ToneDCC][k] {
			if !strings.Contains(l, catchphrase) {
				t.Errorf("dcc error key %d line %d %q missing catchphrase %q", k, i, l, catchphrase)
			}
		}
	}
}

// TestQuestSelected (SQ-0046): selecting a quest renders through the voice layer —
// plain carries the bare id, dcc carries the id and, on at least one line, the
// "get out there and code" rallying cry.
func TestQuestSelected(t *testing.T) {
	if got := New(config.TonePlain).QuestSelected("SQ-1"); !strings.Contains(got, "SQ-1") {
		t.Errorf("plain QuestSelected missing id: %q", got)
	}
	const rally = "Now get out there and code, Code, CODE!"
	found := false
	for i := range pools[config.ToneDCC][keyQuestSelected] {
		v := &Voice{tone: config.ToneDCC, src: fixedSource(i)}
		got := v.QuestSelected("SQ-7")
		if !strings.Contains(got, "SQ-7") {
			t.Errorf("dcc QuestSelected line %d missing id: %q", i, got)
		}
		if strings.Contains(got, rally) {
			found = true
		}
	}
	if !found {
		t.Errorf("no dcc quest-selected line carries the rally %q", rally)
	}
}

func TestDCCKeysNonEmpty(t *testing.T) {
	for k := keyQuestCreated; k <= keyQuestSelected; k++ {
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
