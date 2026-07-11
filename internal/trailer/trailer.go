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
// a line, after trimming surrounding whitespace, begins with the exact key
// "Quest:", "Confirm:", or "Completes:".
func Parse(message string) (refs []Ref, explicitNone bool) {
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Quest:"):
			val := strings.TrimSpace(strings.TrimPrefix(line, "Quest:"))
			if strings.EqualFold(val, "none") {
				explicitNone = true
				continue
			}
			if val != "" {
				refs = append(refs, Ref{ID: val})
			}
		case strings.HasPrefix(line, "Confirm:"):
			val := strings.TrimSpace(strings.TrimPrefix(line, "Confirm:"))
			if val != "" && !strings.EqualFold(val, "none") {
				refs = append(refs, Ref{ID: val, Confirms: true})
			}
		case strings.HasPrefix(line, "Completes:"):
			val := strings.TrimSpace(strings.TrimPrefix(line, "Completes:"))
			if val != "" && !strings.EqualFold(val, "none") {
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
