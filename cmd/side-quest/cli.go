// Human-facing CLI subcommands (init, new, list, show, status, reclassify,
// config). Each handler parses its own flags with the stdlib flag package and
// calls one or more store methods. Validation lives in the store, except for
// cmdList which validates its filter values.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sharkusk/side-quest/internal/config"
	"github.com/sharkusk/side-quest/internal/quest"
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

// newFlagSet returns a FlagSet that stays silent on error (we surface parse
// failures ourselves as usageErr) so output is not double-printed.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

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
	return nil
}

func cmdNew(args []string) error {
	fs := newFlagSet("new")
	var typ, prio, context string
	var setCurrent, asJSON bool
	var tags tagFlag
	fs.StringVar(&typ, "type", "", "quest type (bug|feature)")
	fs.StringVar(&prio, "priority", "", "quest priority (high|low)")
	fs.StringVar(&context, "context", "", "context note")
	fs.Var(&tags, "tag", "tag as key=value (repeatable)")
	fs.BoolVar(&setCurrent, "current", false, "also set as this worktree's current quest")
	fs.BoolVar(&asJSON, "json", false, "emit the created quest as JSON")
	rest, err := parseInterspersed(fs, args)
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
	q, err := s.Create(rest[0], context, quest.Type(typ), quest.Priority(prio), tags.m)
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
	var status, typ, prio string
	var asJSON bool
	fs.StringVar(&status, "status", "", "filter by status")
	fs.StringVar(&typ, "type", "", "filter by type (bug|feature)")
	fs.StringVar(&prio, "priority", "", "filter by priority (high|low)")
	fs.BoolVar(&asJSON, "json", false, "emit JSON")
	if _, err := parseInterspersed(fs, args); err != nil {
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
	s, err := openStore()
	if err != nil {
		return err
	}
	all, err := s.List()
	if err != nil {
		return err
	}
	filtered := make([]*quest.Quest, 0, len(all))
	for _, q := range all {
		if status != "" && string(q.Status) != status {
			continue
		}
		if typ != "" && string(q.Type) != typ {
			continue
		}
		if prio != "" && string(q.Priority) != prio {
			continue
		}
		filtered = append(filtered, q)
	}
	if asJSON {
		return emitJSON(os.Stdout, filtered)
	}
	renderList(os.Stdout, filtered, voiceFor(s))
	return nil
}

func cmdShow(args []string) error {
	fs := newFlagSet("show")
	var asJSON bool
	fs.BoolVar(&asJSON, "json", false, "emit JSON")
	rest, err := parseInterspersed(fs, args)
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
	if asJSON {
		return emitJSON(os.Stdout, q)
	}
	renderShow(os.Stdout, q)
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
	fmt.Printf("noted %s\n", id)
	return nil
}

func cmdReclassify(args []string) error {
	fs := newFlagSet("reclassify")
	var typ, prio string
	fs.StringVar(&typ, "type", "", "new type (bug|feature)")
	fs.StringVar(&prio, "priority", "", "new priority (high|low)")
	rest, err := parseInterspersed(fs, args)
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
	if typ != "" {
		if err := s.SetType(id, quest.Type(typ)); err != nil {
			return err
		}
	}
	if prio != "" {
		if err := s.SetPriority(id, quest.Priority(prio)); err != nil {
			return err
		}
	}
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
	fs.BoolVar(&asJSON, "json", false, "emit JSON")
	if _, err := parseInterspersed(fs, args); err != nil {
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
		return fmt.Errorf("unknown config key %q (settable: require_quest, auto_trailer, id_strategy, tone)", key)
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
