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

// NoteAdded confirms a note was appended to a quest.
func (v *Voice) NoteAdded(id string) string { return fmt.Sprintf(v.pick(keyNoteAdded), id) }

// QuestSelected confirms a quest was made current.
func (v *Voice) QuestSelected(id string) string { return fmt.Sprintf(v.pick(keyQuestSelected), id) }

// LocalOnly announces that a sync was skipped because local-only mode is on.
func (v *Voice) LocalOnly() string { return v.pick(keyLocalOnlySync) }

// Exported confirms n quests were written to dir.
func (v *Voice) Exported(n int, dir string) string { return fmt.Sprintf(v.pick(keyExported), n, dir) }

// Reclassified confirms a quest's type/priority changed.
func (v *Voice) Reclassified(id string) string { return fmt.Sprintf(v.pick(keyReclassified), id) }

// Updated confirms a quest's title/tags were edited.
func (v *Voice) Updated(id string) string { return fmt.Sprintf(v.pick(keyUpdated), id) }

// Linked confirms a commit was linked; sha is the commit (link has no quest id).
func (v *Voice) Linked(sha string) string { return fmt.Sprintf(v.pick(keyLinked), sha) }

// Relinked confirms a quest's commit was repointed after a rebase.
func (v *Voice) Relinked(id string) string { return fmt.Sprintf(v.pick(keyRelinked), id) }

// Unlinked confirms a commit was removed from a quest.
func (v *Voice) Unlinked(id string) string { return fmt.Sprintf(v.pick(keyUnlinked), id) }

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
