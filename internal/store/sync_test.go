package store

import (
	"strings"
	"testing"
)

func TestBuildMergeCommitHasTwoParents(t *testing.T) {
	s := newStore(t)
	// two independent root commits on the quest ref namespace to use as parents
	tx1 := newTxn()
	tx1.put("quests/SQ-0001.md", []byte("one"))
	p1, err := s.buildCommit("", "p1", tx1)
	if err != nil {
		t.Fatal(err)
	}
	tx2 := newTxn()
	tx2.put("quests/SQ-0002.md", []byte("two"))
	p2, err := s.buildCommit("", "p2", tx2)
	if err != nil {
		t.Fatal(err)
	}

	tx := newTxn()
	tx.put("quests/SQ-0003.md", []byte("merged"))
	m, err := s.buildMergeCommit([]string{p1, p2}, "merge", tx)
	if err != nil {
		t.Fatal(err)
	}

	// exactly two parents, in order
	out, err := s.git.Run("rev-list", "--parents", "-n", "1", m)
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(out)
	if len(fields) != 3 || fields[1] != p1 || fields[2] != p2 {
		t.Fatalf("parents = %v, want [%s %s]", fields, p1, p2)
	}
	// tree is exactly tx (only SQ-0003), not a union with the parents' trees
	names, err := s.git.Run("ls-tree", "--name-only", "-r", m)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(names) != "quests/SQ-0003.md" {
		t.Fatalf("tree = %q, want only quests/SQ-0003.md", names)
	}
}

func TestSideAtReadsQuestsAndTouch(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	a := mustCreate(t, s) // SQ-0001
	tip, _ := s.tip()

	side, err := s.sideAt(tip)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := side.Quests[a.ID]; !ok {
		t.Fatalf("sideAt missing %s: %v", a.ID, side.Quests)
	}

	if err := s.fillTouch(&side, tip, []string{a.ID}); err != nil {
		t.Fatal(err)
	}
	if side.Touch[a.ID].IsZero() {
		t.Errorf("touch time for %s not populated", a.ID)
	}
}

func TestSideAtEmptyCommit(t *testing.T) {
	s := newStore(t)
	side, err := s.sideAt("")
	if err != nil {
		t.Fatal(err)
	}
	if len(side.Quests) != 0 {
		t.Errorf("empty commit should yield no quests, got %d", len(side.Quests))
	}
}
