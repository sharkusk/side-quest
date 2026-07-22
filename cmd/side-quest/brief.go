// `side-quest brief` — a read-only "resume" view. It assembles the current
// state (current quest, outstanding backlog, most-recently-closed quests) from
// existing quest data so a fresh session, machine, or agent can pick up without
// re-reading everything. Nothing is persisted: the brief is a pure projection of
// the store at read time, like list/show, so it can never go stale. The human
// render is the default; --markdown emits the same content with absolute times
// for pasting into an agent's context (the SessionStart-injection shape). The
// partitioning itself lives in internal/brief, shared with the MCP quest_brief
// tool.
package main

import (
	"os"
	"strings"
	"time"

	"github.com/sharkusk/side-quest/internal/brief"
	"github.com/sharkusk/side-quest/internal/gitcmd"
	"github.com/sharkusk/side-quest/internal/quest"
	"github.com/sharkusk/side-quest/internal/store"
)

func cmdBrief(args []string) error {
	fs := newFlagSet("brief")
	var asJSON, markdown, noWrap bool
	var closedN int
	fs.BoolVar(&asJSON, "json", false, "emit the brief as JSON")
	fs.BoolVar(&markdown, "markdown", false, "emit the brief as Markdown with absolute times (for an agent's context)")
	fs.BoolVar(&noWrap, "no-wrap", false, "print without word-wrapping")
	fs.IntVar(&closedN, "closed", brief.DefaultClosedShown, "number of recently-closed quests to show")
	setUsage(fs, "usage: side-quest brief [flags]\nsummarize the current state — current quest, outstanding backlog, recently closed — for resuming work")
	rest, err := parseInterspersed(fs, args)
	if helpRequested(err) {
		return nil
	}
	if err != nil {
		return &usageErr{err.Error()}
	}
	if len(rest) != 0 {
		return &usageErr{"brief takes no positional arguments"}
	}
	if closedN < 0 {
		return &usageErr{"--closed must be zero or greater"}
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	quests, err := s.List()
	if err != nil {
		return err
	}
	cur, _ := s.Current() // best-effort: no pointer just means no current quest
	data := brief.Build(quests, cur, time.Now(), closedN)
	branch := currentBranch()
	var commits []commitLine
	if data.Current != nil {
		commits = resolveCommits(s, data.Current)
	}
	switch {
	case asJSON:
		return emitJSON(os.Stdout, briefToJSON(data, branch))
	case markdown:
		renderBriefMarkdown(os.Stdout, data, branch, commits)
	default:
		width := 0
		if !noWrap {
			width = terminalWidth(os.Stdout)
		}
		renderBrief(os.Stdout, data, branch, commits, width)
	}
	return nil
}

// currentBranch reports the worktree's branch, or "" on any failure (a detached
// HEAD or a git error) — the header simply omits the branch then.
func currentBranch() string {
	out, err := gitcmd.New(".").Run("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// resolveCommits turns a quest's stored SHAs into displayable subject lines, the
// same resolution `show` uses (subject only, not the full message); a commit
// whose SHA no longer resolves shows "(message unavailable)".
func resolveCommits(s *store.Store, q *quest.Quest) []commitLine {
	var commits []commitLine
	for _, sha := range q.Commits {
		short, text, ok := s.CommitMessage(sha, false)
		if !ok {
			commits = append(commits, commitLine{short: sha, text: "(message unavailable)"})
			continue
		}
		commits = append(commits, commitLine{short: short, text: text})
	}
	return commits
}

// briefJSON is the neutral machine shape for `brief --json`: the assembled state
// as raw quest structs (the same shape list/show emit), plus header metadata.
type briefJSON struct {
	Branch       string         `json:"branch,omitempty"`
	LastActivity *time.Time     `json:"last_activity,omitempty"`
	Current      *quest.Quest   `json:"current,omitempty"`
	Outstanding  []*quest.Quest `json:"outstanding"`
	Closed       []*quest.Quest `json:"closed"`
	ClosedTotal  int            `json:"closed_total"`
}

func briefToJSON(d brief.Data, branch string) briefJSON {
	j := briefJSON{
		Branch:      branch,
		Current:     d.Current,
		Outstanding: d.Outstanding,
		Closed:      d.Closed,
		ClosedTotal: d.ClosedTotal,
	}
	if !d.LastActivity.IsZero() {
		la := d.LastActivity
		j.LastActivity = &la
	}
	if j.Outstanding == nil {
		j.Outstanding = []*quest.Quest{}
	}
	if j.Closed == nil {
		j.Closed = []*quest.Quest{}
	}
	return j
}
