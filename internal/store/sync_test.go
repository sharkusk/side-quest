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
