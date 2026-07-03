// Command side-quest is the side-quest CLI and git-hook entrypoint. This phase
// exposes the subcommands the hooks call — link, commit-msg, prepare-commit-msg
// — plus current-quest management and install-hooks. The full human CLI (init,
// new, list, show, ...) arrives in a later phase.
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/sharkusk/side-quest/internal/store"
	"github.com/sharkusk/side-quest/internal/trailer"
)

const usage = `usage: side-quest <command> [args]

  init                            create the quest ref (_config.yaml)
  new <title> [--type --priority --context --tag k=v --current --json]
  link <sha>                      apply a commit's Quest:/Completes: trailers
  current [<id> | --clear]        get / set / clear this worktree's active quest
  commit-msg <file>               (hook) warn or reject when a trailer is missing
  prepare-commit-msg <file> [..]  (hook) inject the current-quest trailer
  install-hooks                   install git hooks + refs/side-quest/* refspec`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2:]); err != nil {
		var ue *usageErr
		if errors.As(err, &ue) {
			fmt.Fprintln(os.Stderr, "side-quest:", ue.msg)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "side-quest:", err)
		os.Exit(1)
	}
}

func run(cmd string, args []string) error {
	switch cmd {
	case "init":
		return cmdInit(args)
	case "new":
		return cmdNew(args)
	case "link":
		return cmdLink(args)
	case "current":
		return cmdCurrent(args)
	case "commit-msg":
		return cmdCommitMsg(args)
	case "prepare-commit-msg":
		return cmdPrepareCommitMsg(args)
	case "install-hooks":
		return cmdInstallHooks(args)
	default:
		return fmt.Errorf("unknown command %q\n%s", cmd, usage)
	}
}

// openStore opens the store for the current working directory. Git runs hooks
// with the working tree as CWD, so this resolves the right repo.
func openStore() (*store.Store, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return store.Open(cwd)
}

func cmdLink(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("link needs exactly one <sha>")
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	return s.Link(args[0])
}

func cmdCurrent(args []string) error {
	s, err := openStore()
	if err != nil {
		return err
	}
	switch {
	case len(args) == 0:
		cur, err := s.Current()
		if err != nil {
			return err
		}
		if cur == "" {
			fmt.Println("side-quest: (no current quest)")
		} else {
			fmt.Println(cur)
		}
		return nil
	case args[0] == "--clear":
		return s.ClearCurrent()
	default:
		return s.SetCurrent(args[0])
	}
}

// cmdCommitMsg implements the commit-msg hook. Per the assisted philosophy it
// exits non-zero ONLY for an intentional require_quest rejection; every other
// path (unreadable file, not-a-repo) allows the commit.
func cmdCommitMsg(args []string) error {
	if len(args) < 1 {
		return nil
	}
	msg, err := os.ReadFile(args[0])
	if err != nil {
		return nil // can't read the message -> don't block the commit
	}
	requireQuest := false
	if s, err := openStore(); err == nil {
		if cfg, err := s.Config(); err == nil {
			requireQuest = cfg.RequireQuest
		}
	}
	switch trailer.Decision(string(msg), requireQuest) {
	case trailer.Reject:
		fmt.Fprintln(os.Stderr, "side-quest: no Quest:/Completes: trailer and require_quest is on — commit blocked.")
		fmt.Fprintln(os.Stderr, "  Add e.g.  Quest: SQ-0001   (or  Quest: none  for a genuine chore).")
		os.Exit(1)
	case trailer.Warn:
		fmt.Fprintln(os.Stderr, "side-quest: no Quest: trailer on this commit. (Add 'Quest: none' to silence.)")
	}
	return nil
}

// cmdPrepareCommitMsg implements the prepare-commit-msg hook: if a current
// quest is set and auto_trailer is on, append a Quest: trailer to the message
// (unless one is already present). Never blocks: any obstacle -> leave the
// message untouched and exit 0.
func cmdPrepareCommitMsg(args []string) error {
	if len(args) < 1 {
		return nil
	}
	s, err := openStore()
	if err != nil {
		return nil
	}
	cur, err := s.Current()
	if err != nil || cur == "" {
		return nil
	}
	cfg, err := s.Config()
	if err != nil || !cfg.AutoTrailer {
		return nil
	}
	raw, err := os.ReadFile(args[0])
	if err != nil {
		return nil
	}
	if refs, none := trailer.Parse(string(raw)); len(refs) > 0 || none {
		return nil // a trailer is already present — don't double-inject
	}
	out := string(raw)
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	out += "\nQuest: " + cur + "\n" // blank line before the trailer block
	if err := os.WriteFile(args[0], []byte(out), 0o644); err != nil {
		return nil // a write failure must not block the commit
	}
	return nil
}
