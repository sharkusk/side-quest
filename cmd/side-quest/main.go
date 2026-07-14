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

	"github.com/sharkusk/side-quest/internal/config"
	"github.com/sharkusk/side-quest/internal/gitcmd"
	"github.com/sharkusk/side-quest/internal/store"
	"github.com/sharkusk/side-quest/internal/trailer"
)

const usage = `usage: side-quest <command> [args]

Setup
  onboard [--agents-md]           set up or refresh this repo (ref + hooks + .mcp.json)

Quests
  new [--type --priority --context --tag k=v --current --json] <title>
  list [--status --type --priority --json]   list quests (filters combine)
  show <id> [--history] [--json]  show one quest (--history adds its change log)
  status <id> <status>            set a quest's status
  note <id> <text>                append a note to a quest
  edit <id>                       open a quest in $EDITOR and write it back
  reclassify <id> [--type --priority]  change a quest's type/priority
  current [<id> | --clear]        get / set / clear this worktree's active quest
  config get [--json]             show effective config
  config set <key> <value>        set require_quest | auto_trailer | local_only | id_strategy | tone
  sync [--dry-run] [--remote <name>]  reconcile quests with a remote (fetch+merge+push)
  export <dir>                    write every quest to <dir> as a native SQ-<id>.md file

Advanced
  init                            create the quest ref (_config.yaml)
  install-hooks                   install git hooks + refs/side-quest/* refspec
  install-cli                     put a side-quest launcher on your PATH (plugin users)
  uninstall-cli                   remove the side-quest launcher this CLI installed
  link <sha>                      apply a commit's Quest:/Confirm:/Completes: trailers
  relink <id> <old-sha> <new-sha> repoint a recorded commit after a rebase
  unlink <id> <sha>               remove a recorded commit from a quest
  commit-msg <file>               (hook) warn or reject when a trailer is missing
  prepare-commit-msg <file> [..]  (hook) inject the current-quest trailer
  pre-push [<remote> <url>]       (hook) auto-sync quests on git push
  agents-md                       print the agent-guidance block for AGENTS.md
  serve                           run the stdio MCP server
  version                         print the side-quest version

values:
  type      bug|feature (default feature)
  priority  high|low (default low)
  status    open|partial|done|deferred|discarded (new quests start open)`

// version is overwritten at release build time via -ldflags "-X main.version=<tag>".
// A plain `go build` / `go install` leaves it as "dev".
var version = "dev"

// helpText is the usage screen with a `side-quest <version>` header, so a user
// can see which build they're running without a separate `version` call. version
// is a runtime var (set via ldflags), so it can't live in the usage const.
func helpText() string {
	return "side-quest " + version + "\n\n" + usage
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, helpText())
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version", "--version", "-v":
		fmt.Println(version)
		return
	case "help", "--help", "-h":
		fmt.Println(helpText())
		return
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
	case "list":
		return cmdList(args)
	case "show":
		return cmdShow(args)
	case "status":
		return cmdStatus(args)
	case "note":
		return cmdNote(args)
	case "edit":
		return cmdEdit(args)
	case "reclassify":
		return cmdReclassify(args)
	case "config":
		return cmdConfig(args)
	case "link":
		return cmdLink(args)
	case "relink":
		return cmdRelink(args)
	case "unlink":
		return cmdUnlink(args)
	case "current":
		return cmdCurrent(args)
	case "commit-msg":
		return cmdCommitMsg(args)
	case "prepare-commit-msg":
		return cmdPrepareCommitMsg(args)
	case "install-hooks":
		return cmdInstallHooks(args)
	case "install-cli":
		return cmdInstallCli(args)
	case "uninstall-cli":
		return cmdUninstallCli(args)
	case "sync":
		return cmdSync(args)
	case "pre-push":
		return cmdPrePushHook(args)
	case "onboard":
		return cmdOnboard(args)
	case "export":
		return cmdExport(args)
	case "agents-md":
		return cmdAgentsMd(args)
	case "serve":
		return cmdServe(args)
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
	s, err := store.Open(cwd)
	if err != nil {
		return nil, err
	}
	_ = s.BootstrapFromTracking() // fresh-clone convenience; local-only, safe to ignore
	return s, nil
}

func cmdLink(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("link needs exactly one <sha>")
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	res, err := s.Link(args[0])
	// Warn about trailer ids that named no quest BEFORE deciding on err: the
	// commit-msg hook accepted those trailers, so silence here would let the
	// user believe they linked (SQ-0119). post-commit runs this command, so the
	// warning lands on the user's commit output; it never fails the commit.
	for _, id := range res.Skipped {
		fmt.Fprintf(os.Stderr, "side-quest: trailer names unknown quest %q — not linked (typo, or a quest from another clone?)\n", id)
	}
	return err
}

// cmdRelink swaps a quest's recorded commit for a new one — the fix for a rebase
// that rewrote a linked commit's sha (SQ-0048). The old sha is matched by prefix
// against the stored hashes (it may be dangling, so it is never git-resolved);
// the new sha is resolved to its canonical hash.
func cmdRelink(args []string) error {
	if len(args) != 3 {
		return &usageErr{"relink needs <id> <old-sha> <new-sha>"}
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	newSha, err := s.ResolveCommit(args[2])
	if err != nil {
		return fmt.Errorf("new commit %q not found: %w", args[2], err)
	}
	if err := s.ReplaceCommit(args[0], args[1], newSha); err != nil {
		return err
	}
	fmt.Printf("side-quest: relinked %s (%s → %s)\n", args[0], args[1], newSha)
	return nil
}

// cmdUnlink removes a recorded commit from a quest, matching by prefix (SQ-0048).
func cmdUnlink(args []string) error {
	if len(args) != 2 {
		return &usageErr{"unlink needs <id> <sha>"}
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	if err := s.RemoveCommit(args[0], args[1]); err != nil {
		return err
	}
	fmt.Printf("side-quest: unlinked %s from %s\n", args[1], args[0])
	return nil
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
		if err := s.SetCurrent(args[0]); err != nil {
			return err
		}
		cur, err := s.Current()
		if err != nil {
			return err
		}
		fmt.Println(voiceFor(s).QuestSelected(cur))
		return nil
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
	// Merge commits are exempt: `git pull`/`git merge` legitimately produce
	// commits about no quest, and rejecting one aborts the merge mid-flight with
	// MERGE_HEAD left behind (SQ-0120). No warning noise either — the user did
	// not author this message shape.
	if mergeInProgress() {
		return nil
	}
	tone := config.ToneDCC
	requireQuest := false
	if s, err := openStore(); err == nil {
		if cfg, err := s.Config(); err == nil {
			requireQuest = cfg.RequireQuest
			tone = cfg.Tone
		}
	}
	switch trailer.Decision(string(msg), requireQuest) {
	case trailer.Reject:
		fmt.Fprintln(os.Stderr, "side-quest: no Quest:/Confirm:/Completes: trailer and require_quest is on — commit blocked.")
		fmt.Fprintln(os.Stderr, "  Add e.g.  Quest: SQ-0001   (or  Quest: none  for a genuine chore).")
		os.Exit(1)
	case trailer.Warn:
		fmt.Fprintln(os.Stderr, newVoice(tone).MissingTrailer())
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
	out := injectTrailer(string(raw), "Quest: "+cur)
	if err := os.WriteFile(args[0], []byte(out), 0o644); err != nil {
		return nil // a write failure must not block the commit
	}
	return nil
}

// injectTrailer places trailerLine into a raw commit-message file ABOVE the
// first `#` line. Appending at EOF — the old behavior — broke `git commit -v`:
// there the file ends with git's scissors line plus the staged diff, and
// everything from the scissors down is discarded at cleanup, silently deleting
// the trailer (SQ-0120). Above the comment block it survives every cleanup mode.
// When nothing but comments precedes that point (editor-template case, no -m),
// the message body is still blank — the trailer is placed after a leading blank
// line so the user's subject goes on line one, exactly where git leaves it.
func injectTrailer(raw, trailerLine string) string {
	lines := strings.Split(raw, "\n")
	cut := len(lines)
	for i, ln := range lines {
		if strings.HasPrefix(ln, "#") {
			cut = i
			break
		}
	}
	body := strings.TrimRight(strings.Join(lines[:cut], "\n"), "\n")
	rest := strings.Join(lines[cut:], "\n")
	var b strings.Builder
	if body == "" {
		// No message yet: keep line 1 free for the subject the user will type.
		b.WriteString("\n\n" + trailerLine + "\n")
	} else {
		b.WriteString(body + "\n\n" + trailerLine + "\n")
	}
	if cut < len(lines) {
		b.WriteString(rest)
		if !strings.HasSuffix(rest, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// mergeInProgress reports whether the repository is mid-merge (MERGE_HEAD
// exists) — the state in which commit-msg must not enforce require_quest.
// Best-effort: any doubt reads as "not merging" so enforcement still applies.
func mergeInProgress() bool {
	p, err := gitcmd.New(".").Run("rev-parse", "--git-path", "MERGE_HEAD")
	if err != nil {
		return false
	}
	_, err = os.Stat(strings.TrimSpace(p))
	return err == nil
}
