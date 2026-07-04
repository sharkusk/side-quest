package filter

import (
	"testing"

	"github.com/sharkusk/side-quest/internal/quest"
)

func q(status quest.Status, typ quest.Type, prio quest.Priority, tags map[string]string) *quest.Quest {
	return &quest.Quest{Status: status, Type: typ, Priority: prio, Tags: tags}
}

func TestParseAndMatch(t *testing.T) {
	cases := []struct {
		expr string
		q    *quest.Quest
		want bool
	}{
		// bare enum atoms, auto-classified by which value set they belong to
		{"bug", q(quest.StatusOpen, quest.TypeBug, quest.PriorityLow, nil), true},
		{"bug", q(quest.StatusOpen, quest.TypeFeature, quest.PriorityLow, nil), false},
		{"done", q(quest.StatusDone, quest.TypeBug, quest.PriorityLow, nil), true},
		{"high", q(quest.StatusOpen, quest.TypeBug, quest.PriorityHigh, nil), true},

		// and / or
		{"bug and high", q(quest.StatusOpen, quest.TypeBug, quest.PriorityHigh, nil), true},
		{"bug and high", q(quest.StatusOpen, quest.TypeBug, quest.PriorityLow, nil), false},
		{"bug or feature", q(quest.StatusOpen, quest.TypeFeature, quest.PriorityLow, nil), true},

		// not, and parenthesised grouping (the quest's own example)
		{"not done", q(quest.StatusOpen, quest.TypeBug, quest.PriorityLow, nil), true},
		{"not done", q(quest.StatusDone, quest.TypeBug, quest.PriorityLow, nil), false},
		{"not (done or deferred)", q(quest.StatusPartial, quest.TypeBug, quest.PriorityLow, nil), true},
		{"not (done or deferred)", q(quest.StatusDeferred, quest.TypeBug, quest.PriorityLow, nil), false},

		// precedence: and binds tighter than or -> bug OR (feature AND high)
		{"bug or feature and high", q(quest.StatusOpen, quest.TypeBug, quest.PriorityLow, nil), true},
		{"bug or feature and high", q(quest.StatusOpen, quest.TypeFeature, quest.PriorityLow, nil), false},
		{"bug or feature and high", q(quest.StatusOpen, quest.TypeFeature, quest.PriorityHigh, nil), true},

		// key=value tag atoms
		{"area=cli", q(quest.StatusOpen, quest.TypeBug, quest.PriorityLow, map[string]string{"area": "cli"}), true},
		{"area=cli", q(quest.StatusOpen, quest.TypeBug, quest.PriorityLow, map[string]string{"area": "mcp"}), false},
		{"bug and area=cli", q(quest.StatusOpen, quest.TypeBug, quest.PriorityLow, map[string]string{"area": "cli"}), true},
	}
	for _, c := range cases {
		pred, err := Parse(c.expr)
		if err != nil {
			t.Errorf("Parse(%q) unexpected error: %v", c.expr, err)
			continue
		}
		if got := pred(c.q); got != c.want {
			t.Errorf("Parse(%q) match = %v, want %v", c.expr, got, c.want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, expr := range []string{
		"",                 // empty
		"   ",              // whitespace only
		"bug and",          // trailing operator
		"or bug",           // leading binary operator
		"not",              // not with no operand
		"(bug",             // unbalanced open paren
		"bug)",             // unbalanced close paren
		"banana",           // unknown bare term (no enum owns it)
		"=cli",             // tag with empty key
		"area=",            // tag with empty value
		"bug feature",      // two atoms with no operator between them
	} {
		if _, err := Parse(expr); err == nil {
			t.Errorf("Parse(%q) = nil error, want error", expr)
		}
	}
}
