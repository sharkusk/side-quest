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
	keyNoteAdded
)

// pools maps tone -> key -> candidate lines. plain has exactly one neutral line
// per key; dcc has several so output varies (discarded/deferred get the most
// character). Lines for interpolating keys carry a single %s.
var pools = map[config.Tone]map[msgKey][]string{
	config.TonePlain: {
		keyQuestCreated:    {"created %s"},
		keyStatusOpen:      {"%s -> open"},
		keyStatusPartial:   {"%s -> partial"},
		keyStatusDone:      {"%s -> done"},
		keyStatusDeferred:  {"%s -> deferred"},
		keyStatusDiscarded: {"%s -> discarded"},
		keyMissingTrailer:  {"side-quest: no Quest: trailer on this commit. (Add 'Quest: none' to silence.)"},
		keyEmptyList:       {"no quests"},
		keyInitialized:     {"side-quest: initialized"},
		keyHooksInstalled:  {"side-quest: hooks installed in %s"},
		keyNoteAdded:       {"noted %s"},
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
		keyNoteAdded: {
			"Noted on %s. The System files it away.",
			"A line added to %s's record. The audience takes notes too.",
		},
	},
}
