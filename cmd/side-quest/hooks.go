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
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return err
	}

	q := `"` + self + `"`
	// commit-msg OMITS "|| true" so a require_quest reject (exit 1) blocks the
	// commit; the other two never block the user's workflow.
	shims := []struct{ name, body string }{
		{"prepare-commit-msg", q + ` prepare-commit-msg "$@" || true`},
		{"commit-msg", q + ` commit-msg "$@"`},
		{"post-commit", q + ` link HEAD || true`},
	}
	for _, sh := range shims {
		if err := installOneHook(filepath.Join(hooksDir, sh.name), sh.body); err != nil {
			return err
		}
	}

	addRefspec(g) // best-effort
	fmt.Println("side-quest: hooks installed in", hooksDir)
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

// installOneHook creates a new hook or composes our marker-guarded block into an
// existing one (idempotent: re-install replaces our block, never duplicates it,
// and never clobbers a user's own hook body).
func installOneHook(path, body string) error {
	block := hookMarker + "\n" + body + "\n" + hookEndMarker + "\n"

	existing, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		return writeExec(path, "#!/bin/sh\n"+block)
	}

	text := string(existing)
	if i := strings.Index(text, hookMarker); i >= 0 {
		if j := strings.Index(text, hookEndMarker); j >= 0 {
			end := j + len(hookEndMarker)
			if end < len(text) && text[end] == '\n' {
				end++
			}
			return writeExec(path, text[:i]+block+text[end:])
		}
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return writeExec(path, text+block)
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
