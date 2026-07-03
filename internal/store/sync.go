// Package store — sync.go adds the git plumbing for the cross-clone three-way
// merge (design spec 2026-07-03-side-quest-sync). It reads snapshots at arbitrary
// commits, drives internal/merge, records the result as a two-parent merge
// commit, and publishes it with a fetch-merge-retry loop.
package store

import (
	"fmt"
	"strings"
	"time"

	"github.com/sharkusk/side-quest/internal/config"
	"github.com/sharkusk/side-quest/internal/merge"
	"github.com/sharkusk/side-quest/internal/quest"
)

// TrackingRef is where a fetch lands the remote quest ref, kept separate from the
// live Ref so the network never clobbers local quests.
const TrackingRef = "refs/side-quest-remote/quests"

// FetchRefspec maps the remote quest ref into the local tracking ref.
const FetchRefspec = Ref + ":" + TrackingRef

// sideAt reads the config and every quest present in the tree at commit into a
// merge.Side. An empty commit yields a zero Side (used as the no-common-ancestor
// base). Touch/ConfigTouch are left empty; callers fill them for the conflict set.
func (s *Store) sideAt(commit string) (merge.Side, error) {
	side := merge.Side{Config: config.Default(), Quests: map[string]*quest.Quest{}, Touch: map[string]time.Time{}}
	if commit == "" {
		return side, nil
	}
	if raw, err := s.readFile(commit, configPath); err == nil {
		cfg, err := config.Unmarshal(raw)
		if err != nil {
			return side, err
		}
		side.Config = cfg
	}
	ids, err := s.listIDs(commit)
	if err != nil {
		return side, err
	}
	for _, id := range ids {
		raw, err := s.readFile(commit, questPath(id))
		if err != nil {
			return side, err
		}
		q, err := quest.Unmarshal(id, raw)
		if err != nil {
			return side, err
		}
		side.Quests[id] = q
	}
	return side, nil
}

// fillTouch populates side.Touch for the given ids and side.ConfigTouch, reading
// the committer time of the last commit that changed each path at/under commit.
func (s *Store) fillTouch(side *merge.Side, commit string, ids []string) error {
	for _, id := range ids {
		t, err := s.lastTouch(commit, questPath(id))
		if err != nil {
			return err
		}
		side.Touch[id] = t
	}
	if t, err := s.lastTouch(commit, configPath); err == nil {
		side.ConfigTouch = t
	}
	return nil
}

// lastTouch returns the committer time (RFC3339, parsed) of the most recent
// commit reachable from commit that modified path.
func (s *Store) lastTouch(commit, path string) (time.Time, error) {
	out, err := s.git.Run("log", "-1", "--format=%cI", commit, "--", path)
	if err != nil {
		return time.Time{}, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return time.Time{}, fmt.Errorf("no commit touched %s", path)
	}
	return time.Parse(time.RFC3339, out)
}

// SyncResult summarizes what a sync did, for reporting.
type SyncResult struct {
	// Merged is a best-effort count for reporting, not a control signal: a
	// diverged merge that nets zero visible change vs local still writes a real
	// two-parent commit and moves the ref, yet may report Merged: 0.
	Merged   int  // quests newly integrated from the remote (adopted or field-merged)
	Renamed  int  // id collisions reassigned
	Pushed   bool // a push to the remote succeeded
	UpToDate bool // nothing to do
}

func (s *Store) trackingTip() (string, error) {
	out, err := s.git.Run("for-each-ref", "--format=%(objectname)", TrackingRef)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// isAncestor reports whether a is an ancestor of b (a == b counts as false here;
// callers test equality separately).
func (s *Store) isAncestor(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	_, err := s.git.Run("merge-base", "--is-ancestor", a, b)
	return err == nil
}

// mergeBase returns the common ancestor of a and b, or "" if there is none.
func (s *Store) mergeBase(a, b string) string {
	out, err := s.git.Run("merge-base", a, b)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// reconcile brings the live Ref into agreement with TrackingRef using the domain
// merge, with no network I/O. It fast-forwards when possible, otherwise writes a
// two-parent merge commit. With dryRun it computes counts but writes nothing.
func (s *Store) reconcile(dryRun bool) (SyncResult, error) {
	local, err := s.tip()
	if err != nil {
		return SyncResult{}, err
	}
	remote, err := s.trackingTip()
	if err != nil {
		return SyncResult{}, err
	}
	switch {
	case remote == "":
		return SyncResult{UpToDate: true}, nil // no remote data
	case local == remote:
		return SyncResult{UpToDate: true}, nil
	case local == "":
		// fresh: adopt remote wholesale
		if !dryRun {
			if _, err := s.git.Run("update-ref", Ref, remote); err != nil {
				return SyncResult{}, err
			}
		}
		rs, err := s.sideAt(remote)
		if err != nil {
			return SyncResult{}, err
		}
		return SyncResult{Merged: len(rs.Quests)}, nil
	case s.isAncestor(local, remote):
		// local strictly behind -> fast-forward
		if !dryRun {
			if _, err := s.git.Run("update-ref", Ref, remote, local); err != nil {
				return SyncResult{}, err
			}
		}
		ls, err := s.sideAt(local)
		if err != nil {
			return SyncResult{}, err
		}
		rs, err := s.sideAt(remote)
		if err != nil {
			return SyncResult{}, err
		}
		return SyncResult{Merged: countNew(ls, rs)}, nil
	case s.isAncestor(remote, local):
		return SyncResult{}, nil // local ahead: nothing to merge (push will publish)
	default:
		return s.writeMerge(local, remote, dryRun)
	}
}

// writeMerge performs the true 3-way merge of local vs remote and records it as a
// two-parent merge commit (unless dryRun).
func (s *Store) writeMerge(local, remote string, dryRun bool) (SyncResult, error) {
	baseCommit := s.mergeBase(local, remote)
	base, err := s.sideAt(baseCommit)
	if err != nil {
		return SyncResult{}, err
	}
	lSide, err := s.sideAt(local)
	if err != nil {
		return SyncResult{}, err
	}
	rSide, err := s.sideAt(remote)
	if err != nil {
		return SyncResult{}, err
	}
	// Touch is needed only where both sides have the id; fill that set.
	var both []string
	for id := range lSide.Quests {
		if _, ok := rSide.Quests[id]; ok {
			both = append(both, id)
		}
	}
	if err := s.fillTouch(&lSide, local, both); err != nil {
		return SyncResult{}, err
	}
	if err := s.fillTouch(&rSide, remote, both); err != nil {
		return SyncResult{}, err
	}

	result, events := merge.Merge(base, lSide, rSide)
	renamed := 0
	for _, e := range events {
		if e.Kind == merge.Renamed {
			renamed++
		}
	}
	res := SyncResult{Merged: countNew(lSide, mergeSideOf(result)), Renamed: renamed}
	if dryRun {
		return res, nil
	}

	tx := newTxn()
	cfgBytes, err := config.Marshal(result.Config)
	if err != nil {
		return SyncResult{}, err
	}
	tx.put(configPath, cfgBytes)
	for id, q := range result.Quests {
		data, err := quest.Marshal(q)
		if err != nil {
			return SyncResult{}, err
		}
		tx.put(questPath(id), data)
	}
	commit, err := s.buildMergeCommit([]string{local, remote}, "side-quest: sync merge", tx)
	if err != nil {
		return SyncResult{}, err
	}
	if _, err := s.git.Run("update-ref", Ref, commit, local); err != nil {
		return SyncResult{}, err
	}
	return res, nil
}

// SyncOptions tunes a sync. DryRun computes and reports without writing/pushing;
// NoVerify skips hooks on the internal quest-ref push (set by the pre-push hook to
// avoid re-entering itself).
type SyncOptions struct {
	DryRun   bool
	NoVerify bool
}

const maxSyncTries = 10

// Sync fetches the remote quest ref into the tracking ref, reconciles the live
// ref with a domain merge, and publishes the result, retrying on a lost push
// race. It never mutates the remote in DryRun.
func (s *Store) Sync(remote string, opts SyncOptions) (SyncResult, error) {
	var last SyncResult
	for try := 0; try < maxSyncTries; try++ {
		if _, err := s.git.Run("fetch", remote, FetchRefspec); err != nil && !isMissingRemoteRef(err) {
			return SyncResult{}, fmt.Errorf("fetch %s: %w", remote, err)
		}
		res, err := s.reconcile(opts.DryRun)
		if err != nil {
			return SyncResult{}, err
		}
		last = res

		local, err := s.tip()
		if err != nil {
			return SyncResult{}, err
		}
		remoteTip, err := s.trackingTip()
		if err != nil {
			return SyncResult{}, err
		}
		// Nothing to publish if the remote already contains local.
		if local == "" || local == remoteTip || s.isAncestor(local, remoteTip) {
			last.UpToDate = last.UpToDate || (res.Merged == 0 && res.Renamed == 0)
			return last, nil
		}
		if opts.DryRun {
			return last, nil
		}

		if err := s.push(remote, opts.NoVerify); err == nil {
			last.Pushed = true
			return last, nil
		} else if !isNonFastForward(err) {
			return SyncResult{}, err
		}
		// lost the race: loop, re-fetch, re-merge.
	}
	return SyncResult{}, fmt.Errorf("sync: %s stayed contended after %d tries", Ref, maxSyncTries)
}

// push publishes the live quest ref to remote. noVerify skips hooks so the
// pre-push hook's own publish does not re-enter the hook.
func (s *Store) push(remote string, noVerify bool) error {
	args := []string{"push"}
	if noVerify {
		args = append(args, "--no-verify")
	}
	args = append(args, remote, Ref+":"+Ref)
	_, err := s.git.Run(args...)
	return err
}

// isMissingRemoteRef reports whether err is git's fetch failure because the
// remote has no quest ref yet (nobody has ever synced/pushed there). This is
// expected before the first sync, not a real failure: reconcile() already
// treats an empty TrackingRef as "no remote data". gitcmd pins LC_ALL=C, so the
// English text is stable.
func isMissingRemoteRef(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "couldn't find remote ref")
}

// isNonFastForward reports whether err is git's rejection of a diverged push (the
// retryable case). gitcmd pins LC_ALL=C, so the English text is stable.
func isNonFastForward(err error) bool {
	if err == nil {
		return false
	}
	m := err.Error()
	return strings.Contains(m, "non-fast-forward") ||
		strings.Contains(m, "fetch first") ||
		strings.Contains(m, "rejected")
}

// countNew counts ids in `to` that are absent from or differ from `from`.
func countNew(from, to merge.Side) int {
	n := 0
	for id, q := range to.Quests {
		old, ok := from.Quests[id]
		if !ok {
			n++
			continue
		}
		ob, _ := quest.Marshal(old)
		nb, _ := quest.Marshal(q)
		if string(ob) != string(nb) {
			n++
		}
	}
	return n
}

// mergeSideOf wraps a Result as a Side for countNew.
func mergeSideOf(r merge.Result) merge.Side {
	return merge.Side{Config: r.Config, Quests: r.Quests}
}

// BootstrapFromTracking fast-forwards the live Ref to the tracking ref when the
// live ref is absent or a strict ancestor of it — the fresh-clone case, where
// quests should appear without a full sync. It never touches a diverged or ahead
// live ref and does no network I/O, so it is safe to call on every command.
func (s *Store) BootstrapFromTracking() error {
	remote, err := s.trackingTip()
	if err != nil || remote == "" {
		return nil
	}
	local, err := s.tip()
	if err != nil {
		return err
	}
	switch {
	case local == "":
		_, err := s.git.Run("update-ref", Ref, remote)
		return err
	case local != remote && s.isAncestor(local, remote):
		_, err := s.git.Run("update-ref", Ref, remote, local)
		return err
	}
	return nil
}
