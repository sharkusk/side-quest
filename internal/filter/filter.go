// Package filter compiles a boolean quest-filter expression into a predicate
// over a *quest.Quest. It backs `side-quest list --filter`.
//
// The grammar is the usual boolean one, with `and` binding tighter than `or`
// and `not` tighter still, and `( )` for grouping:
//
//	expr    := or
//	or      := and ("or" and)*
//	and     := not ("and" not)*
//	not     := "not" not | primary
//	primary := "(" expr ")" | atom
//
// An atom is either a bare enum value — auto-classified against the status,
// type, and priority value sets, which are disjoint so a bare word is
// unambiguous — or a `key=value` tag match. `and`, `or`, `not` are reserved
// words; a tag whose key collides with one is still reachable via `key=value`,
// since only the bare word is treated as an operator.
package filter

import (
	"fmt"
	"strings"

	"github.com/sharkusk/side-quest/internal/quest"
)

// Predicate reports whether a quest matches a compiled filter expression.
type Predicate func(*quest.Quest) bool

// Parse compiles expr into a Predicate, or returns an error describing the
// first problem (unknown term, unbalanced parentheses, dangling operator).
func Parse(expr string) (Predicate, error) {
	toks, err := tokenize(expr)
	if err != nil {
		return nil, err
	}
	if len(toks) == 0 {
		return nil, fmt.Errorf("empty filter expression")
	}
	p := &parser{toks: toks}
	pred, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.pos < len(p.toks) {
		return nil, fmt.Errorf("unexpected %q in filter", p.toks[p.pos])
	}
	return pred, nil
}

// tokenize splits expr into words and parenthesis tokens. Parentheses are their
// own tokens even when written flush against a word (`not(done`).
func tokenize(expr string) ([]string, error) {
	var toks []string
	var word strings.Builder
	flush := func() {
		if word.Len() > 0 {
			toks = append(toks, word.String())
			word.Reset()
		}
	}
	for _, r := range expr {
		switch {
		case r == '(' || r == ')':
			flush()
			toks = append(toks, string(r))
		case r == ' ' || r == '\t' || r == '\n':
			flush()
		default:
			word.WriteRune(r)
		}
	}
	flush()
	return toks, nil
}

type parser struct {
	toks []string
	pos  int
}

func (p *parser) peek() string {
	if p.pos < len(p.toks) {
		return p.toks[p.pos]
	}
	return ""
}

func (p *parser) next() string {
	t := p.peek()
	p.pos++
	return t
}

func (p *parser) parseOr() (Predicate, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek() == "or" {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		l, r := left, right
		left = func(q *quest.Quest) bool { return l(q) || r(q) }
	}
	return left, nil
}

func (p *parser) parseAnd() (Predicate, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.peek() == "and" {
		p.next()
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		l, r := left, right
		left = func(q *quest.Quest) bool { return l(q) && r(q) }
	}
	return left, nil
}

func (p *parser) parseNot() (Predicate, error) {
	if p.peek() == "not" {
		p.next()
		inner, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return func(q *quest.Quest) bool { return !inner(q) }, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (Predicate, error) {
	switch tok := p.peek(); tok {
	case "":
		return nil, fmt.Errorf("unexpected end of filter expression")
	case ")":
		return nil, fmt.Errorf("unexpected %q in filter", tok)
	case "(":
		p.next()
		inner, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.peek() != ")" {
			return nil, fmt.Errorf("missing ) in filter")
		}
		p.next()
		return inner, nil
	case "and", "or":
		return nil, fmt.Errorf("filter cannot start a term with %q", tok)
	default:
		p.next()
		return atom(tok)
	}
}

// atom compiles a single term: a `key=value` tag match, or a bare enum value
// classified against the (disjoint) status, type, and priority value sets.
func atom(tok string) (Predicate, error) {
	if key, val, isTag := strings.Cut(tok, "="); isTag {
		if key == "" || val == "" {
			return nil, fmt.Errorf("invalid tag term %q: want key=value", tok)
		}
		return func(q *quest.Quest) bool { return q.Tags[key] == val }, nil
	}
	switch {
	case quest.Status(tok).Valid():
		s := quest.Status(tok)
		return func(q *quest.Quest) bool { return q.Status == s }, nil
	case quest.Type(tok).Valid():
		ty := quest.Type(tok)
		return func(q *quest.Quest) bool { return q.Type == ty }, nil
	case quest.Priority(tok).Valid():
		pr := quest.Priority(tok)
		return func(q *quest.Quest) bool { return q.Priority == pr }, nil
	default:
		return nil, fmt.Errorf("unknown filter term %q", tok)
	}
}
