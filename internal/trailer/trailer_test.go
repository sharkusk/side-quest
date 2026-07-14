package trailer

import "testing"

func TestParseSingleQuest(t *testing.T) {
	refs, none := Parse("do work\n\nQuest: SQ-0001\n")
	if none {
		t.Fatal("did not expect explicitNone")
	}
	if len(refs) != 1 || refs[0].ID != "SQ-0001" || refs[0].Completes {
		t.Fatalf("bad parse: %+v", refs)
	}
}

func TestParseCompletes(t *testing.T) {
	refs, _ := Parse("finish\n\nCompletes: SQ-0002\n")
	if len(refs) != 1 || refs[0].ID != "SQ-0002" || !refs[0].Completes {
		t.Fatalf("bad parse: %+v", refs)
	}
}

func TestParseConfirm(t *testing.T) {
	refs, _ := Parse("ready for review\n\nConfirm: SQ-0003\n")
	if len(refs) != 1 || refs[0].ID != "SQ-0003" || !refs[0].Confirms || refs[0].Completes {
		t.Fatalf("bad parse: %+v", refs)
	}
	// A Confirm: trailer is a real ref, so it satisfies the commit-msg check.
	if Decision("Confirm: SQ-0003\n", true) != Accept {
		t.Error("Confirm: ref -> Accept even when a quest is required")
	}
}

func TestParseMultiple(t *testing.T) {
	refs, _ := Parse("msg\n\nQuest: SQ-1\nCompletes: SQ-2\n")
	if len(refs) != 2 {
		t.Fatalf("want 2 refs, got %+v", refs)
	}
	if refs[0].ID != "SQ-1" || refs[0].Completes {
		t.Fatalf("ref0 wrong: %+v", refs[0])
	}
	if refs[1].ID != "SQ-2" || !refs[1].Completes {
		t.Fatalf("ref1 wrong: %+v", refs[1])
	}
}

func TestParseNoneEscapeHatch(t *testing.T) {
	refs, none := Parse("chore\n\nQuest: none\n")
	if !none {
		t.Fatal("expected explicitNone for 'Quest: none'")
	}
	if len(refs) != 0 {
		t.Fatalf("none must yield no refs: %+v", refs)
	}
	// case-insensitive value
	if _, none := Parse("Quest: NONE\n"); !none {
		t.Fatal("expected case-insensitive none")
	}
}

func TestParseIgnoresNonTrailerLines(t *testing.T) {
	// "Question:" must not match "Quest:"; prose is ignored.
	refs, none := Parse("Question: is this a trailer?\nno it is not\n")
	if none || len(refs) != 0 {
		t.Fatalf("false positive: refs=%+v none=%v", refs, none)
	}
}

func TestParseTrimsIndentedTrailer(t *testing.T) {
	refs, _ := Parse("msg\n\n   Quest: SQ-0009  \n")
	if len(refs) != 1 || refs[0].ID != "SQ-0009" {
		t.Fatalf("indented/trailing-space trailer not handled: %+v", refs)
	}
}

// SQ-0119/SQ-0120: prose containing a key must not read as a trailer; comment
// lines are skipped; scanning stops at git's scissors line (under `git commit -v`
// the staged diff follows it, and diff context lines must never match).
func TestParseIgnoresProseCommentsAndDiff(t *testing.T) {
	// A multi-word value is prose, not a trailer — and must not satisfy `none`.
	refs, none := Parse("docs\n\nQuest: none of the existing docs mentioned the hook\n")
	if none || len(refs) != 0 {
		t.Fatalf("prose line parsed as trailer: refs=%+v none=%v", refs, none)
	}
	// Comment lines never match; the scissors line ends scanning entirely.
	msg := "fix thing\n" +
		"# Quest: SQ-0001 (comment — ignore)\n" +
		"# ------------------------ >8 ------------------------\n" +
		"# Do not modify or remove the line above.\n" +
		"diff --git a/x b/x\n" +
		" Quest: SQ-0002\n" + // diff context line — below scissors
		"+Completes: SQ-0003\n"
	refs, none = Parse(msg)
	if none || len(refs) != 0 {
		t.Fatalf("comment/diff content parsed as trailers: refs=%+v none=%v", refs, none)
	}
	// Keys are case-insensitive, like git's trailer keys.
	refs, _ = Parse("msg\n\ncompletes: SQ-0004\n")
	if len(refs) != 1 || !refs[0].Completes || refs[0].ID != "SQ-0004" {
		t.Fatalf("lowercase key not recognized: %+v", refs)
	}
}

func TestDecision(t *testing.T) {
	if Decision("Quest: SQ-1\n", false) != Accept {
		t.Error("ref present -> Accept")
	}
	if Decision("Quest: none\n", true) != Accept {
		t.Error("explicit none -> Accept even when required")
	}
	if Decision("no trailer\n", false) != Warn {
		t.Error("no trailer, not required -> Warn")
	}
	if Decision("no trailer\n", true) != Reject {
		t.Error("no trailer, required -> Reject")
	}
}
