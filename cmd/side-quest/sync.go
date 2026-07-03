package main

import (
	"flag"
	"fmt"

	"github.com/sharkusk/side-quest/internal/store"
)

// cmdSync reconciles this clone's quest ref with a remote: fetch into the tracking
// ref, three-way merge, push. --dry-run reports the plan without writing/pushing.
func cmdSync(args []string) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	dry := fs.Bool("dry-run", false, "show what would merge/push without writing anything")
	remote := fs.String("remote", "", "remote to sync with (default: origin, or the sole remote)")
	setUsage(fs, "sync [--dry-run] [--remote <name>]")
	if err := fs.Parse(args); err != nil {
		if helpRequested(err) {
			return nil
		}
		return err
	}
	s, err := openStore()
	if err != nil {
		return err
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
		return nil
	}
	fmt.Printf("side-quest: %s %s: merged %d, renamed %d, pushed %t.\n",
		prefix, rem, res.Merged, res.Renamed, res.Pushed)
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
