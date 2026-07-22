// Package brief assembles the read-only "resume" view of the quest store: the
// current quest, the outstanding backlog, and the most-recently-closed quests.
//
// It is a pure projection — no store, no clock, no rendering — so the CLI
// `brief` command and the MCP `quest_brief` tool share one partitioning rule set
// and can never drift. Callers pass the quests, the current-quest id, a
// reference `now`, and how many closed quests to keep; rendering and commit
// resolution live in the frontends.
package brief

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sharkusk/side-quest/internal/quest"
)

// DefaultClosedShown is how many recently-closed quests a brief keeps unless the
// caller overrides it. A fixed count (not a time window) keeps the brief a
// predictable length regardless of how busy the recent stretch was.
const DefaultClosedShown = 5

// Data is the assembled, render-ready projection. Nothing here is persisted.
type Data struct {
	Now          time.Time      // reference time for relative "N ago" rendering
	LastActivity time.Time      // newest create/complete time; zero on an empty store
	Current      *quest.Quest   // the worktree's current quest, or nil
	Outstanding  []*quest.Quest // open/partial/confirm, current excluded, id order
	Closed       []*quest.Quest // recently closed, newest first, capped
	ClosedTotal  int            // total closed quests (> len(Closed) when capped)
}

// outstandingStatuses are the states treated as active work — the "what's on the
// board" set the outstanding list view already defines.
var outstandingStatuses = map[quest.Status]bool{
	quest.StatusOpen:    true,
	quest.StatusPartial: true,
	quest.StatusConfirm: true,
}

// Build partitions quests into the current quest, the outstanding backlog, and
// the newest closedN closed quests. The current quest is featured regardless of
// its status and never duplicated into the other lists; a negative closedN means
// "no cap".
func Build(quests []*quest.Quest, current string, now time.Time, closedN int) Data {
	d := Data{Now: now}
	for _, q := range quests {
		if q.Created.After(d.LastActivity) {
			d.LastActivity = q.Created
		}
		if q.Completed != nil && q.Completed.After(d.LastActivity) {
			d.LastActivity = *q.Completed
		}
		if q.ID == current {
			d.Current = q
		}
	}
	var closed []*quest.Quest
	for _, q := range quests {
		if q == d.Current {
			continue // featured on its own; never list it twice
		}
		if outstandingStatuses[q.Status] {
			d.Outstanding = append(d.Outstanding, q)
		} else {
			closed = append(closed, q)
		}
	}
	d.ClosedTotal = len(closed)
	// Newest first by close time; SliceStable keeps id order among equal times.
	sort.SliceStable(closed, func(i, j int) bool {
		return ClosedTime(closed[i]).After(ClosedTime(closed[j]))
	})
	if closedN >= 0 && len(closed) > closedN {
		closed = closed[:closedN]
	}
	d.Closed = closed
	return d
}

// ClosedTime is the best available "when closed" timestamp: the completion time
// when set (done quests carry it), else the creation time (deferred/discarded
// need not), so recently-closed sorting is stable across all closed statuses.
func ClosedTime(q *quest.Quest) time.Time {
	if q.Completed != nil {
		return *q.Completed
	}
	return q.Created
}

// Narrative strips the leading mechanical capture block (the branch/head/cwd/
// current lines capture.Body prepends) from a quest's Context, returning just the
// human "why it came up" note — or "" when the context is mechanical-only. A
// brief shows the branch in its header, so the provenance lines are noise here.
func Narrative(context string) string {
	lines := strings.Split(context, "\n")
	i := 0
	for i < len(lines) && isMechanicalLine(lines[i]) {
		i++
	}
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++ // drop the blank separator capture.Body inserts
	}
	return strings.TrimSpace(strings.Join(lines[i:], "\n"))
}

func isMechanicalLine(s string) bool {
	for _, p := range []string{"branch: ", "head: ", "cwd: ", "current: "} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// HumanizeSince renders now-t as a coarse relative age ("just now", "5m ago",
// "2h ago", "3d ago", "2w ago"). A future or sub-minute t reads as "just now".
func HumanizeSince(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dw ago", int(d.Hours()/24/7))
	}
}
