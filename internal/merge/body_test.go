package merge

import (
	"strings"
	"testing"
	"time"

	"github.com/sharkusk/side-quest/internal/quest"
)

func withBody(id, title, body string) *quest.Quest {
	x := q(id, title, quest.StatusOpen)
	x.Body = body
	return x
}

func TestMergeBodyUnionsNotesKeepsWinnerPreamble(t *testing.T) {
	base := side(q("SQ-0001", "orig", quest.StatusOpen))
	lBody := "local preamble\n\n--- note 2026-01-02T10:00:00Z ---\n\nfrom local\n"
	rBody := "remote preamble\n\n--- note 2026-01-02T11:00:00Z ---\n\nfrom remote\n"
	l := withBody("SQ-0001", "l", lBody)
	r := withBody("SQ-0001", "r", rBody)
	local := side(l)
	local.Touch["SQ-0001"] = time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	remote := side(r)
	remote.Touch["SQ-0001"] = time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC) // remote wins preamble

	got := mustMergeBody(t, base, local, remote)
	if !strings.HasPrefix(got, "remote preamble") {
		t.Errorf("preamble should be winner's (remote); got:\n%s", got)
	}
	if !strings.Contains(got, "from local") || !strings.Contains(got, "from remote") {
		t.Errorf("both notes should survive; got:\n%s", got)
	}
	// ordered by timestamp: local (10:00) before remote (11:00)
	if strings.Index(got, "from local") > strings.Index(got, "from remote") {
		t.Errorf("notes out of timestamp order:\n%s", got)
	}
}

func TestMergeBodyDedupesIdenticalNote(t *testing.T) {
	base := side(q("SQ-0001", "orig", quest.StatusOpen))
	shared := "--- note 2026-01-02T10:00:00Z ---\n\nsame note\n"
	l := withBody("SQ-0001", "l", "p\n\n"+shared)
	r := withBody("SQ-0001", "r", shared)
	local := side(l)
	local.Touch["SQ-0001"] = time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	remote := side(r)
	remote.Touch["SQ-0001"] = time.Date(2026, 1, 2, 11, 0, 0, 0, time.UTC)
	got := mustMergeBody(t, base, local, remote)
	if n := strings.Count(got, "same note"); n != 1 {
		t.Errorf("identical note should appear once, got %d:\n%s", n, got)
	}
}

func mustMergeBody(t *testing.T, base, local, remote Side) string {
	t.Helper()
	res, _ := Merge(base, local, remote)
	return res.Quests["SQ-0001"].Body
}
