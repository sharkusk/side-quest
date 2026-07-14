package main

import (
	"fmt"
	"os"

	"github.com/sharkusk/side-quest/internal/store"
)

// cmdSync reconciles this clone's quest ref with a remote: fetch into the tracking
// ref, three-way merge, push. --dry-run reports the plan without writing/pushing.
func cmdSync(args []string) error {
	// newFlagSet discards the flag package's own error printing, so a bad flag
	// reports once via usageErr (exit 2) instead of twice with exit 1 — matching
	// every other subcommand (SQ-0123).
	fs := newFlagSet("sync")
	dry := fs.Bool("dry-run", false, "show what would merge/push without writing anything")
	remote := fs.String("remote", "", "remote to sync with (default: origin, or the sole remote)")
	setUsage(fs, "sync [--dry-run] [--remote <name>]")
	if err := fs.Parse(args); err != nil {
		if helpRequested(err) {
			return nil
		}
		return &usageErr{err.Error()}
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	// Local-only mode: announce and stop before touching a remote (and skip the
	// clash-prone-ids nag — you never push, so ids can't clash across clones).
	if cfg, err := s.Config(); err == nil && cfg.LocalOnly {
		fmt.Println(voiceFor(s).LocalOnly())
		return nil
	}
	rem, err := resolveRemote(s, *remote)
	if err != nil {
		return err
	}
	res, err := s.Sync(rem, store.SyncOptions{DryRun: *dry})
	if err != nil {
		return err
	}
	prefix := "synced"
	if *dry {
		prefix = "dry-run"
	}
	if res.UpToDate && res.Merged == 0 && res.Renamed == 0 {
		fmt.Printf("side-quest: %s %s: already up to date.\n", prefix, rem)
	} else {
		fmt.Printf("side-quest: %s %s: merged %d, renamed %d, pushed %t.\n",
			prefix, rem, res.Merged, res.Renamed, res.Pushed)
	}
	noticeSequentialWithRemote(s) // remote-added-after-init leaves clash-prone sequential ids (SQ-0035)
	return nil
}

// cmdPrePushHook is the git pre-push hook entry point. git passes the remote name
// as args[0] (and URL as args[1]); we ignore the hook's stdin, since git omits a
// non-fast-forward ref from it (see SQ-0032). It syncs the quest ref out-of-band
// and NEVER blocks the user's branch push: any failure is a warning + exit 0.
func cmdPrePushHook(args []string) error {
	remote := "origin"
	if len(args) > 0 && args[0] != "" {
		remote = args[0]
	}
	s, err := openStore()
	if err != nil {
		return nil // not a side-quest repo state we can act on; let the push proceed
	}
	if _, err := s.Sync(remote, store.SyncOptions{NoVerify: true}); err != nil {
		fmt.Fprintf(os.Stderr, "warning (side-quest): couldn't publish quests to %s: %v\n", remote, err)
		fmt.Fprintln(os.Stderr, "                     run `side-quest sync` when back online.")
	}
	return nil
}

// resolveRemote picks the remote to sync with: the flag if set, else "origin" if
// it exists, else the sole configured remote, else an error.
func resolveRemote(s *store.Store, flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	names, err := s.Remotes()
	if err != nil {
		return "", err
	}
	for _, n := range names {
		if n == "origin" {
			return "origin", nil
		}
	}
	if len(names) == 1 {
		return names[0], nil
	}
	return "", fmt.Errorf("no remote to sync with (configure 'origin' or pass --remote)")
}
