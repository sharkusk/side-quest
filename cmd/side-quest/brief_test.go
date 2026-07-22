package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/sharkusk/side-quest/internal/brief"
	"github.com/sharkusk/side-quest/internal/quest"
)

func mkQuest(id string, st quest.Status, created time.Time, completed *time.Time) *quest.Quest {
	return &quest.Quest{
		ID:        id,
		Title:     id + " title",
		Status:    st,
		Type:      quest.TypeFeature,
		Priority:  quest.PriorityLow,
		Created:   created,
		Completed: completed,
	}
}

func tp(t time.Time) *time.Time { return &t }

func TestRenderBriefSections(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	cur := mkQuest("SQ-0002", quest.StatusPartial, base, nil)
	cur.Context = "branch: main\nhead: abc\ncwd: /r\n\nresume without re-reading"
	open := mkQuest("SQ-0001", quest.StatusOpen, base, nil)
	done := mkQuest("SQ-0003", quest.StatusDone, base, tp(base.AddDate(0, 0, 1)))
	d := brief.Build([]*quest.Quest{open, cur, done}, "SQ-0002", base.AddDate(0, 0, 2), 5)

	var buf bytes.Buffer
	renderBrief(&buf, d, "main", nil, 0) // width 0 = unwrapped, deterministic
	out := buf.String()

	for _, want := range []string{
		"side-quest brief · main · last activity",
		"1 current · 1 outstanding · 1 recently closed",
		"CURRENT",
		"SQ-0002  feature/low  partial",
		"why: resume without re-reading", // narrative shown, mechanical block stripped
		"OUTSTANDING (1)",
		"SQ-0001",
		"RECENTLY CLOSED (1)",
		"SQ-0003",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("renderBrief output missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, "cwd: /r") {
		t.Errorf("renderBrief leaked a mechanical capture line:\n%s", out)
	}
}

func TestRenderBriefNoCurrent(t *testing.T) {
	var buf bytes.Buffer
	renderBrief(&buf, brief.Build(nil, "", time.Now(), 5), "", nil, 0)
	if !strings.Contains(buf.String(), "none set") {
		t.Errorf("empty brief should hint to set a current quest:\n%s", buf.String())
	}
}

func TestRenderBriefMarkdown(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	cur := mkQuest("SQ-0002", quest.StatusPartial, base, nil)
	cur.Context = "branch: main\ncwd: /r\n\nwhy line"
	d := brief.Build([]*quest.Quest{cur}, "SQ-0002", base, 5)

	var buf bytes.Buffer
	renderBriefMarkdown(&buf, d, "main", nil)
	out := buf.String()
	for _, want := range []string{"# side-quest brief", "## Current", "**SQ-0002**", "why line", "## Outstanding (0)"} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown output missing %q\n---\n%s", want, out)
		}
	}
}
