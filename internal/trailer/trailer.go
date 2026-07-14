// Package trailer parses side-quest commit-message trailers and decides the
// commit-msg hook's action. It is PURE — no git, no filesystem — so the policy
// is trivially unit-testable. The cmd layer feeds it a message string plus the
// require_quest flag and acts on the result.
//
// Trailers (git convention: key: value lines near the end of a message):
//
//	Quest: SQ-0001      // this commit did work on SQ-0001
//	Confirm: SQ-0001    // as above AND parks the quest in `confirm` for user sign-off
//	Completes: SQ-0001  // as above AND closes the quest
//	Quest: none         // explicit escape hatch: a genuine chore, not linked
package trailer

import "strings"

// Ref is one quest reference extracted from a commit message. Completes and
// Confirms are mutually exclusive (each comes from a distinct trailer key); a
// bare Quest: ref leaves both false.
type Ref struct {
	ID        string // e.g. "SQ-0001"
	Completes bool   // true for a Completes: trailer (closes the quest)
	Confirms  bool   // true for a Confirm: trailer (moves the quest to `confirm`)
}

// Parse scans a commit message for Quest:/Confirm:/Completes: trailers.
//
// It returns every reference found (a commit may touch several quests) and,
// separately, whether an explicit `Quest: none` was present — that is NOT a
// reference but it satisfies the commit-msg check. A trailer is recognized when
// a line, after trimming surrounding whitespace, begins with one of the keys —
// matched case-insensitively, like git's own trailer keys — and its value is a
// single whitespace-free token (an id, or "none"). That guard keeps prose like
// "Quest: none of the docs mentioned the hook" from reading as a trailer
// (SQ-0119). The hooks hand Parse the RAW message file, so it also skips `#`
// comment lines and stops at git's scissors line — under `git commit -v`
// everything below the scissors is the staged diff, whose context lines must
// never be scanned (SQ-0120).
func Parse(message string) (refs []Ref, explicitNone bool) {
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			if strings.Contains(line, ">8") {
				break // git scissors line — the diff follows; stop entirely
			}
			continue // comment line
		}
		key, rawVal, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		val := strings.TrimSpace(rawVal)
		if val == "" || strings.ContainsAny(val, " \t") {
			continue // empty, or prose after the colon — not a trailer value
		}
		switch {
		case strings.EqualFold(key, "Quest"):
			if strings.EqualFold(val, "none") {
				explicitNone = true
				continue
			}
			refs = append(refs, Ref{ID: val})
		case strings.EqualFold(key, "Confirm"):
			if !strings.EqualFold(val, "none") {
				refs = append(refs, Ref{ID: val, Confirms: true})
			}
		case strings.EqualFold(key, "Completes"):
			if !strings.EqualFold(val, "none") {
				refs = append(refs, Ref{ID: val, Completes: true})
			}
		}
	}
	return refs, explicitNone
}

// Action is the commit-msg hook's decision.
type Action int

const (
	Accept Action = iota // has a ref, or an explicit `Quest: none`
	Warn                 // no ref / no none, require_quest off -> warn but allow
	Reject               // no ref / no none, require_quest on  -> block the commit
)

// Decision picks the commit-msg action for a message under require_quest.
func Decision(message string, requireQuest bool) Action {
	refs, none := Parse(message)
	if len(refs) > 0 || none {
		return Accept
	}
	if requireQuest {
		return Reject
	}
	return Warn
}
