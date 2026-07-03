package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sharkusk/side-quest/internal/gitcmd"
)

const (
	hookMarker    = "# >>> side-quest >>>"
	hookEndMarker = "# <<< side-quest <<<"
)

// cmdInstallHooks writes (or composes into) the three git hooks and adds the
// refs/side-quest/* refspec. Shims call THIS binary by absolute path, so the
// hooks always run the exact side-quest that installed them (no PATH reliance).
func cmdInstallHooks(args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	g := gitcmd.New(cwd)
	if _, err := g.Run("rev-parse", "--is-inside-work-tree"); err != nil {
		return fmt.Errorf("not a git repository: %w", err)
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, err = filepath.Abs(self)
	if err != nil {
		return err
	}

	hooksDir, err := resolveHooksDir(g, cwd)
	if err != nil {
		return err
	}
	// A non-default core.hooksPath usually means another framework (husky,
	// pre-commit) owns the hooks dir. We still honor it, but say so — a silent
	// install into someone else's hooks dir is how workflows get tangled.
	if hp, err := g.Run("config", "--get", "core.hooksPath"); err == nil && hp != "" {
		fmt.Fprintf(os.Stderr, "side-quest: core.hooksPath is set (%s) — installing there. If another tool (husky, pre-commit) manages it, migrate off that tool first.\n", hp)
	}
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return err
	}

	q := shimQuotedPath(self)
	// commit-msg OMITS "|| true" so a require_quest reject (exit 1) blocks the
	// commit; the other two never block the user's workflow.
	shims := []struct{ name, body string }{
		{"prepare-commit-msg", q + ` prepare-commit-msg "$@" || true`},
		{"commit-msg", q + ` commit-msg "$@"`},
		{"post-commit", q + ` link HEAD || true`},
	}
	for _, sh := range shims {
		outcome, err := installOneHook(filepath.Join(hooksDir, sh.name), sh.body)
		if err != nil {
			return err
		}
		switch outcome {
		case hookSkipped:
			fmt.Fprintf(os.Stderr, "side-quest: SKIPPED the existing %s hook — it has a non-sh shebang, and appending our sh block would corrupt it.\n", sh.name)
			fmt.Fprintf(os.Stderr, "  Migrate that hook to call `side-quest %s` itself, or remove it and re-run install-hooks.\n", sh.name)
		case hookComposed:
			fmt.Fprintf(os.Stderr, "side-quest: composed into the existing %s hook (it already had content) — verify the two coexist.\n", sh.name)
		}
	}

	addRefspec(g) // best-effort
	fmt.Println(bestEffortVoice().HooksInstalled(hooksDir))
	return nil
}

// resolveHooksDir honors core.hooksPath, otherwise <common-git-dir>/hooks.
// cwd is the directory g's git commands run in: a relative core.hooksPath is
// resolved by git relative to the worktree top, but a relative
// --git-common-dir is resolved by git relative to cwd — so each fallback
// branch below joins against the base git actually used.
func resolveHooksDir(g *gitcmd.Git, cwd string) (string, error) {
	top, topErr := g.Run("rev-parse", "--show-toplevel")
	if hp, err := g.Run("config", "--get", "core.hooksPath"); err == nil && hp != "" {
		if filepath.IsAbs(hp) {
			return hp, nil
		}
		if topErr != nil {
			return "", topErr
		}
		return filepath.Join(top, hp), nil
	}
	common, err := g.Run("rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(cwd, common)
	}
	return filepath.Join(common, "hooks"), nil
}

// shimQuotedPath returns the side-quest binary path quoted for a POSIX-sh hook,
// normalized to forward slashes. Git hooks always run under sh — even on Windows,
// via Git-for-Windows' MSYS sh, where a backslash is an escape and a C:\... drive
// path is fragile — so the embedded path must use "/" (MSYS sh accepts C:/...).
//
// The conversion is done explicitly rather than with filepath.ToSlash, which
// keys off the BUILD host's separator ('/' on Unix) and so would silently leave
// a Windows path untouched when this binary is cross-compiled or tested on Unix.
// A literal backslash in a Unix install path (pathological for a binary) is the
// only case this would mangle, a trade we accept for correct Windows shims.
func shimQuotedPath(self string) string {
	return `"` + strings.ReplaceAll(self, `\`, "/") + `"`
}

// hookOutcome reports what installOneHook did to a hook file, so the caller can
// message the user (a foreign hook composed into, or one skipped as unsafe).
type hookOutcome int

const (
	hookCreated  hookOutcome = iota // wrote a fresh hook
	hookUpdated                     // replaced our own marker block in place
	hookComposed                    // appended our block to a user's existing hook
	hookSkipped                     // left a non-sh hook untouched (would corrupt it)
)

// hookShebangCompatible reports whether content's interpreter can run our POSIX-sh
// block. A missing shebang is compatible: git runs an extensionless hook via sh.
// An explicit non-shell interpreter (python, node, ...) is not — appending sh
// lines to it would corrupt the file.
func hookShebangCompatible(content string) bool {
	if !strings.HasPrefix(content, "#!") {
		return true
	}
	line := content
	if i := strings.IndexByte(content, '\n'); i >= 0 {
		line = content[:i]
	}
	fields := strings.Fields(strings.TrimSpace(line[2:])) // drop "#!"
	if len(fields) == 0 {
		return true
	}
	interp := filepath.Base(fields[0])
	if interp == "env" { // "#!/usr/bin/env bash" -> look at the next token
		if len(fields) < 2 {
			return true // degenerate "#!/usr/bin/env" with no interpreter
		}
		interp = filepath.Base(fields[1])
	}
	switch interp {
	case "sh", "bash", "dash", "ash", "zsh", "ksh":
		return true
	}
	return false
}

// installOneHook creates a new hook or composes our marker-guarded block into an
// existing one (idempotent: re-install replaces our block, never duplicates it,
// and never clobbers a user's own hook body). It refuses to append to a hook with
// a non-sh interpreter — that would corrupt it — and reports what it did.
func installOneHook(path, body string) (hookOutcome, error) {
	block := hookMarker + "\n" + body + "\n" + hookEndMarker + "\n"

	existing, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return 0, err
		}
		return hookCreated, writeExec(path, "#!/bin/sh\n"+block)
	}

	text := string(existing)
	if i := strings.Index(text, hookMarker); i >= 0 {
		if j := strings.Index(text, hookEndMarker); j >= 0 {
			end := j + len(hookEndMarker)
			if end < len(text) && text[end] == '\n' {
				end++
			}
			return hookUpdated, writeExec(path, text[:i]+block+text[end:])
		}
	}
	// A foreign hook with no side-quest block yet: only safe to append if it runs
	// under a POSIX shell. Otherwise leave it entirely alone.
	if !hookShebangCompatible(text) {
		return hookSkipped, nil
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return hookComposed, writeExec(path, text+block)
}

// writeExec writes content and ensures the file is executable (WriteFile only
// applies perms when creating, so we chmod explicitly for the compose case).
func writeExec(path, content string) error {
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		return err
	}
	return os.Chmod(path, 0o755)
}

// addRefspec adds push+fetch refspecs for refs/side-quest/* to origin so quest
// data travels with the repo. Best-effort: no origin -> a note, no error.
func addRefspec(g *gitcmd.Git) {
	if _, err := g.Run("remote", "get-url", "origin"); err != nil {
		fmt.Println("side-quest: no 'origin' remote — skipped refspec (add it later or use sync).")
		return
	}
	const refspec = "refs/side-quest/*:refs/side-quest/*"
	ensureConfigContains(g, "remote.origin.fetch", refspec)
	// A configured remote.origin.push disables push.default entirely, so pushing
	// only the quest refspec would make a bare `git push` send quests but SKIP
	// the user's branch. Restore current-branch push explicitly with "HEAD" (it
	// pushes the checked-out branch to a like-named remote branch, matching
	// push.default's "current" intent without pushing other branches), then add
	// the quest refs alongside it. See SQ-0016.
	ensureConfigContains(g, "remote.origin.push", "HEAD")
	ensureConfigContains(g, "remote.origin.push", refspec)
}

// ensureConfigContains adds val to a multi-valued git config key unless already present.
func ensureConfigContains(g *gitcmd.Git, key, val string) {
	if out, err := g.Run("config", "--get-all", key); err == nil {
		for _, line := range strings.Split(out, "\n") {
			if strings.TrimSpace(line) == val {
				return
			}
		}
	}
	_, _ = g.Run("config", "--add", key, val)
}
