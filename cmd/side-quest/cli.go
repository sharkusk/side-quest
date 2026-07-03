// Human-facing CLI subcommands (init, new, list, show, status, reclassify,
// config). Each handler parses its own flags with the stdlib flag package and
// calls exactly one store method — validation lives in the store, not here.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

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
	fmt.Println("side-quest: initialized")
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
	if err := fs.Parse(args); err != nil {
		return &usageErr{err.Error()}
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return &usageErr{"new needs exactly one <title> (quote multi-word titles; put flags before it)"}
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
	fmt.Println(q.ID)
	return nil
}
