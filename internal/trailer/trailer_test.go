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
