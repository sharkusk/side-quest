// Package gitcmd is a thin wrapper over the system `git` binary. All git
// interaction in side-quest goes through it, so subprocess handling and error
// formatting live in exactly one place.
//
// Go note (for C/Python readers): methods here return (value, error) pairs.
// Go has no exceptions — the error is an ordinary second return value the
// caller must check. A nil error means success.
package gitcmd

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Git runs git commands in a fixed directory, optionally with extra
// environment variables (used to point GIT_INDEX_FILE at a scratch index so we
// never disturb the user's real index).
type Git struct {
	dir string   // working directory git runs in
	env []string // extra "KEY=VALUE" entries appended to the inherited env
}

// New returns a Git bound to dir.
func New(dir string) *Git { return &Git{dir: dir} }

// WithEnv returns a COPY of g with additional environment variables. We copy so
// callers can layer env without mutating a shared value (Go structs are copied
// on assignment; `cp := *g` duplicates the struct).
func (g *Git) WithEnv(kv ...string) *Git {
	cp := *g
	cp.env = append(append([]string{}, g.env...), kv...)
	return &cp
}

// run is the single execution path. It returns raw stdout bytes; the exported
// helpers decide whether to trim.
func (g *Git) run(stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.dir
	if len(g.env) > 0 {
		cmd.Env = append(cmd.Environ(), g.env...) // Environ() = inherited env
	}
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %v: %s",
			strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

// Run executes git and returns stdout with the trailing newline trimmed
// (convenient for shas, refs, single-line output).
func (g *Git) Run(args ...string) (string, error) {
	b, err := g.run(nil, args...)
	return strings.TrimRight(string(b), "\n"), err
}

// RunRaw returns stdout untrimmed. Use for file contents where a trailing
// newline is significant (e.g. `cat-file -p`).
func (g *Git) RunRaw(args ...string) ([]byte, error) {
	return g.run(nil, args...)
}

// RunInput feeds stdin to git (e.g. `hash-object --stdin`) and returns trimmed
// stdout.
func (g *Git) RunInput(stdin string, args ...string) (string, error) {
	b, err := g.run([]byte(stdin), args...)
	return strings.TrimRight(string(b), "\n"), err
}
