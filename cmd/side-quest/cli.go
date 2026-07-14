// Human-facing CLI subcommands (init, new, list, show, status, reclassify,
// config). Each handler parses its own flags with the stdlib flag package and
// calls one or more store methods. Validation lives in the store, except for
// cmdList which validates its filter values.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/sharkusk/side-quest/internal/capture"
	"github.com/sharkusk/side-quest/internal/config"
	"github.com/sharkusk/side-quest/internal/filter"
	"github.com/sharkusk/side-quest/internal/quest"
	"github.com/sharkusk/side-quest/internal/store"
)

// usageErr marks a wrong-usage problem (missing arg, malformed flag). main()
// maps it to exit code 2; every other error exits 1.
type usageErr struct{ msg string }

func (e *usageErr) Error() string { return e.msg }

// tagFlag collects repeated --tag key=value flags into a map (a flag.Value).
type tagFlag struct{ m map[string]string }

func (t *tagFlag) String() string { return "" }

func (t *tagFlag) Set(v string) error {
	i := strings.IndexByte(v, '=')
	if i <= 0 {
		return fmt.Errorf("tag must be key=value, got %q", v)
	}
	if t.m == nil {
		t.m = map[string]string{}
	}
	t.m[v[:i]] = v[i+1:]
	return nil
}

// stringsFlag collects a repeated string flag (e.g. --show-tag) in order.
type stringsFlag []string

func (s *stringsFlag) String() string { return strings.Join(*s, ",") }

func (s *stringsFlag) Set(v string) error {
	if v == "" {
		return fmt.Errorf("tag key must be non-empty")
	}
	*s = append(*s, v)
	return nil
}

// newFlagSet returns a FlagSet that stays silent on error (we surface parse
// failures ourselves as usageErr) so output is not double-printed.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// setUsage gives fs the help screen shown for `<cmd> -h` / `--help`: a one-line
// synopsis followed by every flag and its description (flag.PrintDefaults). The
// flag package calls fs.Usage automatically on a help request, and parseInterspersed
// turns the resulting flag.ErrHelp into a clean exit. Help prints to stdout — an
// asked-for query is a success, not an error.
func setUsage(fs *flag.FlagSet, synopsis string) {
	fs.Usage = func() {
		fmt.Fprintln(os.Stdout, synopsis)
		hasFlags := false
		fs.VisitAll(func(*flag.Flag) { hasFlags = true })
		if hasFlags {
			fmt.Fprintln(os.Stdout, "\nflags:")
			fs.SetOutput(os.Stdout)
			fs.PrintDefaults()
		}
	}
}

// helpRequested reports whether a parse error is the flag package's help
// sentinel — the command already printed its help and should exit 0.
func helpRequested(err error) bool { return errors.Is(err, flag.ErrHelp) }

// parseInterspersed parses fs while allowing flags to appear before OR after
// positional arguments. Go's stdlib flag package stops at the first
// non-flag token, so `new "title" --tag x=y` would otherwise treat the
// trailing flags as positionals. We work around that by parsing, peeling off
// the one positional the parser stopped on, and re-parsing the remainder until
// no tokens are left. Returns the collected positional arguments in order.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positionals []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positionals, nil
		}
		positionals = append(positionals, rest[0])
		args = rest[1:]
	}
}

func cmdInit(args []string) error {
	if len(args) != 0 {
		return &usageErr{"init takes no arguments"}
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	if err := s.Init(); err != nil {
		return err
	}
	fmt.Println(voiceFor(s).Initialized())
	noticeRandomIDs(s)
	return nil
}

// noticeRandomIDs tells the user when Init selected the random strategy (because
// a remote is configured — SQ-0030), since it deviates from the tidy sequential
// default and is easy to miss. Silent for sequential; silent on any read error
// (a cosmetic notice must never fail the command).
func noticeRandomIDs(s *store.Store) {
	if cfg, err := s.Config(); err == nil && cfg.IDStrategy == config.Random {
		fmt.Println("side-quest: a remote is configured, so ids are random (e.g. SQ-a1b2c3) to avoid clashes across clones — override with `side-quest config set id_strategy sequential`.")
	}
}

// noticeSequentialWithRemote nudges toward random ids when a remote is configured
// but ids are still sequential (SQ-0035). Init picks random automatically when a
// remote already exists (SQ-0030), so this state only arises when the remote is
// added AFTER init — leaving sequential ids that clash across clones. Cosmetic:
// silent for random ids, for a remote-less repo, and on any read error.
func noticeSequentialWithRemote(s *store.Store) {
	cfg, err := s.Config()
	if err != nil || cfg.IDStrategy != config.Sequential {
		return
	}
	if names, err := s.Remotes(); err != nil || len(names) == 0 {
		return
	}
	fmt.Println("side-quest: a remote is configured but ids are still sequential (SQ-0001, ...), which can clash across clones — switch with `side-quest config set id_strategy random`.")
}

func cmdNew(args []string) error {
	fs := newFlagSet("new")
	var typ, prio, context string
	var setCurrent, asJSON bool
	var tags tagFlag
	fs.StringVar(&typ, "type", "", "quest type: bug|feature (default feature)")
	fs.StringVar(&prio, "priority", "", "quest priority: high|low (default low)")
	fs.StringVar(&context, "context", "", "one-line context note (why the quest came up)")
	fs.Var(&tags, "tag", "annotation as key=value; repeat for multiple tags")
	fs.BoolVar(&setCurrent, "current", false, "also set as this worktree's current quest")
	fs.BoolVar(&asJSON, "json", false, "emit the created quest as JSON")
	setUsage(fs, "usage: side-quest new [flags] <title>\ncreate a quest; quote a multi-word title")
	rest, err := parseInterspersed(fs, args)
	if helpRequested(err) {
		return nil
	}
	if err != nil {
		return &usageErr{err.Error()}
	}
	if len(rest) != 1 {
		return &usageErr{"new needs exactly one <title> (quote multi-word titles)"}
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	cur, _ := s.Current()
	body := capture.Body(cwd, cur, context)
	q, err := s.Create(rest[0], body, quest.Type(typ), quest.Priority(prio), tags.m)
	if err != nil {
		return err
	}
	if setCurrent {
		if err := s.SetCurrent(q.ID); err != nil {
			return err
		}
	}
	if asJSON {
		return emitJSON(os.Stdout, q)
	}
	fmt.Println(voiceFor(s).QuestCreated(q.ID))
	return nil
}

func cmdList(args []string) error {
	fs := newFlagSet("list")
	var status, typ, prio, filterExpr string
	var asJSON, all, noWrap bool
	var tags tagFlag
	var showTags stringsFlag
	fs.StringVar(&status, "status", "", "filter by status: open|partial|confirm|done|deferred|discarded")
	fs.StringVar(&typ, "type", "", "filter by type: bug|feature")
	fs.StringVar(&prio, "priority", "", "filter by priority: high|low")
	fs.Var(&tags, "tag", "filter by tag key=value; repeat for AND across tags")
	fs.Var(&showTags, "show-tag", "add a column showing tag KEY's value; repeat for more columns")
	fs.BoolVar(&all, "all", false, "include every status (default shows only open, partial, and confirm)")
	fs.StringVar(&filterExpr, "filter", "", `boolean expression, e.g. "bug and not (done or deferred)"`)
	fs.BoolVar(&asJSON, "json", false, "emit the matching quests as JSON")
	fs.BoolVar(&noWrap, "no-wrap", false, "print raw titles without word-wrapping")
	setUsage(fs, "usage: side-quest list [flags]\nlist quests; simple filters combine with AND, or use --filter for a boolean expression")
	rest, err := parseInterspersed(fs, args)
	if helpRequested(err) {
		return nil
	}
	if err == nil && len(rest) != 0 {
		// `list done` silently showed the DEFAULT view — the user believed they
		// were looking at the done set (SQ-0123). Reject with a pointer instead.
		return &usageErr{fmt.Sprintf("list takes no positional arguments — did you mean --status %s or --filter %q?", rest[0], rest[0])}
	}
	if err != nil {
		return &usageErr{err.Error()}
	}
	if status != "" && !quest.Status(status).Valid() {
		return fmt.Errorf("invalid status %q", status)
	}
	if typ != "" && !quest.Type(typ).Valid() {
		return fmt.Errorf("invalid type %q", typ)
	}
	if prio != "" && !quest.Priority(prio).Valid() {
		return fmt.Errorf("invalid priority %q", prio)
	}
	// --filter is the whole filter: it can't be mixed with the simple flags,
	// which would make the combined semantics ambiguous.
	var pred filter.Predicate
	if filterExpr != "" {
		if status != "" || typ != "" || prio != "" || len(tags.m) > 0 || all {
			return &usageErr{"--filter cannot be combined with --status/--type/--priority/--tag/--all"}
		}
		if pred, err = filter.Parse(filterExpr); err != nil {
			return err
		}
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	quests, err := s.List()
	if err != nil {
		return err
	}
	// Default to the "what's outstanding?" view — open, partial, and confirm only —
	// unless the caller asked for a specific --status, opted into --all, or supplied
	// an explicit --filter expression (which takes full control of the selection).
	openOnly := status == "" && !all && filterExpr == ""
	filtered := make([]*quest.Quest, 0, len(quests))
	for _, q := range quests {
		if openOnly && q.Status != quest.StatusOpen && q.Status != quest.StatusPartial && q.Status != quest.StatusConfirm {
			continue
		}
		if pred != nil {
			if !pred(q) {
				continue
			}
			filtered = append(filtered, q)
			continue
		}
		if status != "" && string(q.Status) != status {
			continue
		}
		if typ != "" && string(q.Type) != typ {
			continue
		}
		if prio != "" && string(q.Priority) != prio {
			continue
		}
		if !quest.MatchTags(q.Tags, tags.m) {
			continue
		}
		filtered = append(filtered, q)
	}
	if asJSON {
		return emitJSON(os.Stdout, filtered)
	}
	// Wrap to the terminal width, but only for an interactive terminal: piped
	// output and --no-wrap both fall back to width 0 (unwrapped, script-stable).
	width := 0
	if !noWrap {
		width = terminalWidth(os.Stdout)
	}
	renderList(os.Stdout, filtered, voiceFor(s), width, showTags)
	return nil
}

func cmdShow(args []string) error {
	fs := newFlagSet("show")
	var asJSON, noWrap, full, history bool
	fs.BoolVar(&asJSON, "json", false, "emit the quest as JSON")
	fs.BoolVar(&noWrap, "no-wrap", false, "print raw field values without word-wrapping")
	fs.BoolVar(&full, "full", false, "with the linked commits, print each commit's complete message (default: subject only)")
	fs.BoolVar(&history, "history", false, "also show the quest's change history: date, who, and what changed per commit")
	setUsage(fs, "usage: side-quest show [flags] <id>\nshow one quest; --full prints linked commits' complete messages; --history adds the change log; <id> accepts shorthand (11 or 0011 for SQ-0011)")
	rest, err := parseInterspersed(fs, args)
	if helpRequested(err) {
		return nil
	}
	if err != nil {
		return &usageErr{err.Error()}
	}
	if len(rest) != 1 {
		return &usageErr{"show needs exactly one <id>"}
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	q, err := s.Get(rest[0])
	if err != nil {
		return err
	}
	var hist []store.HistoryEntry
	if history {
		if hist, err = s.History(rest[0]); err != nil {
			return err
		}
	}
	if asJSON {
		if history {
			return emitJSON(os.Stdout, struct {
				Quest   *quest.Quest         `json:"quest"`
				History []store.HistoryEntry `json:"history"`
			}{q, hist})
		}
		return emitJSON(os.Stdout, q)
	}
	// Wrap to the terminal width, but only for an interactive terminal: piped
	// output and --no-wrap both fall back to width 0 (unwrapped, script-stable).
	width := 0
	if !noWrap {
		width = terminalWidth(os.Stdout)
	}
	var commits []commitLine
	for _, sha := range q.Commits {
		short, text, ok := s.CommitMessage(sha, full)
		if !ok {
			commits = append(commits, commitLine{short: sha, text: "(message unavailable)"})
			continue
		}
		commits = append(commits, commitLine{short: short, text: text})
	}
	renderShow(os.Stdout, q, width, commits)
	if history {
		renderHistory(os.Stdout, hist)
	}
	return nil
}

func cmdStatus(args []string) error {
	if len(args) != 2 {
		return &usageErr{"status needs <id> <status>"}
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	if err := s.SetStatus(args[0], quest.Status(args[1])); err != nil {
		return err
	}
	fmt.Println(voiceFor(s).StatusSet(args[0], quest.Status(args[1])))
	return nil
}

// cmdNote appends a note to a quest. The note text is every argument after the
// id, joined with spaces, so callers need not quote a multi-word note. The
// store rejects empty text and a nonexistent id.
func cmdNote(args []string) error {
	if len(args) < 2 {
		return &usageErr{"note needs <id> <text>"}
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	id := args[0]
	if err := s.AppendNote(id, strings.Join(args[1:], " ")); err != nil {
		return err
	}
	fmt.Println(voiceFor(s).NoteAdded(id))
	return nil
}

func cmdReclassify(args []string) error {
	fs := newFlagSet("reclassify")
	var typ, prio string
	fs.StringVar(&typ, "type", "", "new type: bug|feature (omit to leave unchanged)")
	fs.StringVar(&prio, "priority", "", "new priority: high|low (omit to leave unchanged)")
	setUsage(fs, "usage: side-quest reclassify [flags] <id>\nchange a quest's type and/or priority (at least one flag)")
	rest, err := parseInterspersed(fs, args)
	if helpRequested(err) {
		return nil
	}
	if err != nil {
		return &usageErr{err.Error()}
	}
	if len(rest) != 1 {
		return &usageErr{"reclassify needs exactly one <id>"}
	}
	if typ == "" && prio == "" {
		return &usageErr{"reclassify needs --type and/or --priority"}
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	id := rest[0]
	return s.Reclassify(id, quest.Type(typ), quest.Priority(prio))
}

// editorValue resolves the editor command line, honoring VISUAL then EDITOR
// (the long-standing Unix convention) and falling back to vi.
func editorValue() string {
	for _, env := range []string{"VISUAL", "EDITOR"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return v
		}
	}
	return "vi"
}

// runEditor opens path in the resolved editor with the terminal attached, so an
// interactive editor (vi, nano) works normally. On POSIX the value runs through
// `sh -c`, exactly like git runs $EDITOR — so a program path containing spaces
// (`EDITOR="/Applications/Visual Studio Code.app/.../code --wait"`) or quoting
// works here whenever it works for `git commit` (SQ-0123). Windows has no sh;
// there the value is split on spaces as before.
func runEditor(path string) error {
	ed := editorValue()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		argv := append(strings.Fields(ed), path)
		cmd = exec.Command(argv[0], argv[1:]...)
	} else {
		// `sh -c 'ed "$@"' ed <path>`: $0 is the editor string (for error
		// messages), $1 the file — git's own invocation shape.
		cmd = exec.Command("sh", "-c", ed+` "$@"`, ed, path)
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// cmdEdit round-trips a quest through $EDITOR: it serializes the quest to its
// Markdown form, opens it, and writes the saved buffer back to the ref. The id
// is the filename, not part of the buffer, so it is never editable. If the saved
// buffer no longer parses or is rejected, the temp file is KEPT and its path is
// reported, so a long hand-edit is never silently lost.
func cmdEdit(args []string) error {
	if len(args) != 1 {
		return &usageErr{"edit needs exactly one <id>"}
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	q, err := s.Get(args[0])
	if err != nil {
		return err
	}
	orig, err := quest.Marshal(q)
	if err != nil {
		return err
	}

	f, err := os.CreateTemp("", "side-quest-"+q.ID+"-*.md")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(orig)
	f.Close()
	if werr != nil {
		os.Remove(tmp)
		return werr
	}

	if err := runEditor(tmp); err != nil {
		// KEEP the temp file: an editor can exit non-zero after a successful
		// save (a crash after :w, a flaky `code --wait`), and deleting it here
		// would destroy a long hand-edit (SQ-0123).
		return fmt.Errorf("editor: %w (if you saved, your edits are kept at %s)", err, tmp)
	}
	edited, err := os.ReadFile(tmp)
	if err != nil {
		return fmt.Errorf("%w (your edits, if saved, are kept at %s)", err, tmp)
	}
	// A CRLF-saving editor (common on Windows) must not break the round-trip:
	// the frontmatter fences are matched on "\n", so normalize line endings
	// before comparing/parsing (SQ-0123).
	edited = bytes.ReplaceAll(edited, []byte("\r\n"), []byte("\n"))
	if bytes.Equal(edited, orig) {
		os.Remove(tmp)
		fmt.Println("no changes")
		return nil
	}

	// From here on a failure keeps tmp so the edits survive — report its path.
	nq, err := quest.Unmarshal(q.ID, edited)
	if err != nil {
		return fmt.Errorf("edited quest did not parse (your edits are kept at %s): %w", tmp, err)
	}
	if err := s.Replace(q.ID, nq); err != nil {
		return fmt.Errorf("edited quest rejected (your edits are kept at %s): %w", tmp, err)
	}
	os.Remove(tmp)
	fmt.Printf("updated %s\n", q.ID)
	return nil
}

func cmdConfig(args []string) error {
	if len(args) < 1 {
		return &usageErr{"config needs a subcommand: get | set"}
	}
	switch args[0] {
	case "get":
		return cmdConfigGet(args[1:])
	case "set":
		return cmdConfigSet(args[1:])
	default:
		return &usageErr{fmt.Sprintf("unknown config subcommand %q (want get|set)", args[0])}
	}
}

func cmdConfigGet(args []string) error {
	fs := newFlagSet("config get")
	var asJSON bool
	fs.BoolVar(&asJSON, "json", false, "emit the configuration as JSON")
	setUsage(fs, "usage: side-quest config get [flags]\nshow the effective on-ref configuration")
	rest, err := parseInterspersed(fs, args)
	if helpRequested(err) {
		return nil
	}
	if err == nil && len(rest) != 0 {
		return &usageErr{"config get takes no positional arguments (did you mean `config set " + rest[0] + " ...`?)"}
	}
	if err != nil {
		return &usageErr{err.Error()}
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	cfg, err := s.Config()
	if err != nil {
		return err
	}
	if asJSON {
		return emitJSON(os.Stdout, cfg)
	}
	renderConfig(os.Stdout, cfg)
	return nil
}

func cmdConfigSet(args []string) error {
	if len(args) != 2 {
		return &usageErr{"config set needs <key> <value>"}
	}
	key, val := args[0], args[1]
	s, err := openStore()
	if err != nil {
		return err
	}
	switch key {
	case "require_quest":
		b, err := parseBool(val)
		if err != nil {
			return err
		}
		return s.SetRequireQuest(b)
	case "auto_trailer":
		b, err := parseBool(val)
		if err != nil {
			return err
		}
		return s.SetAutoTrailer(b)
	case "local_only":
		b, err := parseBool(val)
		if err != nil {
			return err
		}
		return s.SetLocalOnly(b)
	case "id_strategy":
		st := config.Strategy(val)
		if !st.Valid() {
			return fmt.Errorf("invalid id_strategy %q (want sequential|random)", val)
		}
		return s.SetStrategy(st)
	case "tone":
		tn := config.Tone(val)
		if !tn.Valid() {
			return fmt.Errorf("invalid tone %q (want plain|dcc|dcc-superfan)", val)
		}
		return s.SetTone(tn)
	default:
		return fmt.Errorf("unknown config key %q (settable: require_quest, auto_trailer, local_only, id_strategy, tone)", key)
	}
}

// parseBool accepts only "true" or "false" (stricter than strconv.ParseBool).
func parseBool(v string) (bool, error) {
	switch v {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("want true or false, got %q", v)
}
