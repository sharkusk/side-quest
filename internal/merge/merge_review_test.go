package merge

import (
	"strings"
	"testing"
	"time"

	"github.com/sharkusk/side-quest/internal/config"
	"github.com/sharkusk/side-quest/internal/quest"
)

// TestUnionCommitsRespectsRemovals (SQ-0118): a commit sha dropped by a side
// (unlink, or relink after a rebase) must NOT be resurrected from the merge base
// when the quest also conflicts on other fields; a sha both sides still carry
// survives, and each side's additions are kept.
func TestUnionCommitsRespectsRemovals(t *testing.T) {
	mk := func(title string, commits ...string) *quest.Quest {
		x := q("SQ-0001", title, quest.StatusOpen)
		x.Commits = commits
		return x
	}
	base := side(mk("t", "KEEP", "GONE"))
	// Local unlinked GONE and added LNEW; remote kept both and added RNEW —
	// a genuine both-changed conflict (titles differ too).
	localQ := mk("local title", "KEEP", "LNEW")
	remoteQ := mk("remote title", "KEEP", "GONE", "RNEW")
	local := side(localQ)
	remote := side(remoteQ)
	local.Touch["SQ-0001"] = time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	remote.Touch["SQ-0001"] = time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)

	res, _ := Merge(base, local, remote)
	got := res.Quests["SQ-0001"].Commits
	want := []string{"KEEP", "LNEW", "RNEW"}
	if len(got) != len(want) {
		t.Fatalf("commits = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("commits = %v, want %v", got, want)
		}
	}

	// Both sides removed GONE (alongside different other edits): it must stay gone.
	local2 := side(mk("local title", "KEEP"))
	remote2 := side(mk("remote title", "KEEP"))
	local2.Touch["SQ-0001"] = local.Touch["SQ-0001"]
	remote2.Touch["SQ-0001"] = remote.Touch["SQ-0001"]
	res2, _ := Merge(base, local2, remote2)
	for _, sha := range res2.Quests["SQ-0001"].Commits {
		if sha == "GONE" {
			t.Fatalf("both-sides-removed sha resurrected: %v", res2.Quests["SQ-0001"].Commits)
		}
	}
}

// TestCollisionRenameUsesMergedPrefix (SQ-0118): the loser of an add/add id
// collision is renamed under the MERGED config's prefix, not the pre-merge local
// one — otherwise two clones merging the same divergence mint different ids when
// one side changed id_prefix, breaking cross-clone determinism.
func TestCollisionRenameUsesMergedPrefix(t *testing.T) {
	// Same id, two different quests (independent adds), no base.
	local := side(q("SQ-0007", "local thing", quest.StatusOpen))
	remote := side(q("SQ-0007", "remote thing", quest.StatusPartial))
	// Remote changed the prefix, and its config edit is the later touch, so the
	// merged config must carry QQ — and so must the rename.
	remote.Config.IDPrefix = "QQ"
	local.ConfigTouch = time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	remote.ConfigTouch = time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	base := Side{Config: config.Default(), Quests: map[string]*quest.Quest{}, Touch: map[string]time.Time{}}

	res, events := Merge(base, local, remote)
	if res.Config.IDPrefix != "QQ" {
		t.Fatalf("merged prefix = %q, want QQ", res.Config.IDPrefix)
	}
	var renamed string
	for _, e := range events {
		if e.Kind == Renamed {
			renamed = e.ID
		}
	}
	if renamed == "" {
		t.Fatal("no rename event for the add/add collision")
	}
	if !strings.HasPrefix(renamed, "QQ-") {
		t.Fatalf("loser renamed to %q, want the merged QQ- prefix", renamed)
	}
}
