package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sharkusk/side-quest/internal/gitcmd"
	"github.com/sharkusk/side-quest/internal/store"
)

const (
	hookMarker    = "# >>> side-quest >>>"
	hookEndMarker = "# <<< side-quest <<<"
	// hookVersionPrefix tags the installed block with the side-quest version that
	// wrote it, so a re-install can tell a stale block (older shim format or binary
	// path) from a current one (SQ-0045). The start/end markers stay version-FREE so
	// a block written by ANY version is still found and replaced.
	hookVersionPrefix = "# side-quest-version: "
)

// hookBlock renders the marker-guarded hook block for body, stamped with the
// installer version so upgrades are detectable (SQ-0045).
func hookBlock(body, version string) string {
	return hookMarker + "\n" +
		hookVersionPrefix + version + "\n" +
		body + "\n" +
		hookEndMarker + "\n"
}

// parseHookVersion returns the version stamped inside a side-quest hook block, or
// "" when the block predates version stamping (SQ-0045).
func parseHookVersion(block string) string {
	for _, line := range strings.Split(block, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), hookVersionPrefix); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// cmdInstallHooks writes (or composes into) the four git hooks and migrates
// origin's refspecs to the sync model. Shims call `side-quest` via PATH (not by
// absolute path), so the installed block is byte-identical across machines —
// committable into a shared hooks dir — and skips gracefully when the binary is
// absent (SQ-0058).
func cmdInstallHooks(args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	g := gitcmd.New(cwd)
	if _, err := g.Run("rev-parse", "--is-inside-work-tree"); err != nil {
		return fmt.Errorf("not a git repository: %w", err)
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

	// Shims call `side-quest` via PATH so they are byte-identical across machines
	// (committable into a shared hooks dir) and degrade gracefully when the binary
	// is absent. commit-msg is the one blocking hook (block=true): it keeps no
	// `|| true`, so a require_quest reject blocks the commit; the others never block.
	shims := []struct{ name, body string }{
		{"prepare-commit-msg", guardedShim(`prepare-commit-msg "$@"`, false)},
		{"commit-msg", guardedShim(`commit-msg "$@"`, true)},
		{"post-commit", guardedShim("link HEAD", false)},
		{"pre-push", guardedShim(`pre-push "$@"`, false)},
	}
	for _, sh := range shims {
		outcome, prev, err := installOneHook(filepath.Join(hooksDir, sh.name), sh.body, version)
		if err != nil {
			return err
		}
		switch outcome {
		case hookSkipped:
			fmt.Fprintf(os.Stderr, "side-quest: SKIPPED the existing %s hook — it has a non-sh shebang, and appending our sh block would corrupt it.\n", sh.name)
			fmt.Fprintf(os.Stderr, "  Migrate that hook to call `side-quest %s` itself, or remove it and re-run install-hooks.\n", sh.name)
		case hookComposed:
			fmt.Fprintf(os.Stderr, "side-quest: composed into the existing %s hook (it already had content) — verify the two coexist.\n", sh.name)
		case hookUpdated:
			fmt.Fprintln(os.Stderr, hookRefreshNote(sh.name, prev, version))
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

// hookOutcome reports what installOneHook did to a hook file, so the caller can
// message the user (a foreign hook composed into, or one skipped as unsafe).
type hookOutcome int

const (
	hookCreated   hookOutcome = iota // wrote a fresh hook
	hookUpdated                      // replaced our own marker block (content changed)
	hookUnchanged                    // our block was already byte-identical — left as is
	hookComposed                     // appended our block to a user's existing hook
	hookSkipped                      // left a non-sh hook untouched (would corrupt it)
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

// guardedShim renders a PATH-relative hook body: it runs `side-quest
// <invocation>` only when the binary is on PATH, and otherwise warns and skips
// (never blocking). The if/else lets control flow THROUGH the block, so any
// hook content after our marker block still runs (an early exit would skip it).
// block=false appends `|| true` so the hook never fails on side-quest's own exit
// status; block=true (commit-msg) omits it so a require_quest reject still blocks
// the commit. A MISSING binary always takes the else branch and exits 0 (SQ-0058).
func guardedShim(invocation string, block bool) string {
	tail := " || true"
	if block {
		tail = ""
	}
	return "if command -v side-quest >/dev/null 2>&1; then\n" +
		"\tside-quest " + invocation + tail + "\n" +
		"else\n" +
		"\techo \"side-quest: not on PATH — skipping (add it to PATH; see install-hooks)\" >&2\n" +
		"fi"
}

// installOneHook creates a new hook or composes our marker-guarded block into an
// existing one (idempotent: re-install replaces our block, never duplicates it,
// and never clobbers a user's own hook body). It refuses to append to a hook with
// a non-sh interpreter — that would corrupt it. It reports what it did and, when
// it replaced an existing side-quest block, the version that block was stamped
// with (SQ-0045) — "" for a block that predates version stamping.
func installOneHook(path, body, version string) (hookOutcome, string, error) {
	block := hookBlock(body, version)

	existing, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return 0, "", err
		}
		return hookCreated, "", writeExec(path, "#!/bin/sh\n"+block)
	}

	text := string(existing)
	if i := strings.Index(text, hookMarker); i >= 0 {
		// j >= i: a stray end marker BEFORE the start marker (hand-mangled hook)
		// would make text[i:end] slice out of order and panic (SQ-0123); treat it
		// as no valid block and fall through to the foreign-hook path.
		if j := strings.Index(text, hookEndMarker); j >= i {
			end := j + len(hookEndMarker)
			if end < len(text) && text[end] == '\n' {
				end++
			}
			existingBlock := text[i:end]
			prev := parseHookVersion(existingBlock)
			if existingBlock == block {
				return hookUnchanged, prev, nil // already current — touch nothing
			}
			return hookUpdated, prev, writeExec(path, text[:i]+block+text[end:])
		}
	}
	// A foreign hook with no side-quest block yet: only safe to append if it runs
	// under a POSIX shell. Otherwise leave it entirely alone.
	if !hookShebangCompatible(text) {
		return hookSkipped, "", nil
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return hookComposed, "", writeExec(path, text+block)
}

// hookRefreshNote describes an in-place refresh of a side-quest hook block, so an
// upgrader sees that re-running install-hooks brought a stale shim current. It
// distinguishes an upgrade (version changed) from a same-version rewrite (the
// binary path or shim format changed) (SQ-0045).
func hookRefreshNote(name, prev, version string) string {
	switch {
	case prev == "":
		return fmt.Sprintf("side-quest: refreshed the %s hook to v%s (it predated version stamping).", name, version)
	case prev != version:
		return fmt.Sprintf("side-quest: refreshed the %s hook (v%s → v%s).", name, prev, version)
	default:
		return fmt.Sprintf("side-quest: refreshed the %s hook (v%s; binary path or shim format changed).", name, version)
	}
}

// writeExec writes content and ensures the file is executable (WriteFile only
// applies perms when creating, so we chmod explicitly for the compose case).
func writeExec(path, content string) error {
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		return err
	}
	return os.Chmod(path, 0o755)
}

// addRefspec migrates origin's refspecs to the sync model: the quest ref is
// fetched into a SEPARATE tracking ref (never clobbering the live ref), and is no
// longer pushed by git's refspec — the pre-push hook publishes it. Best-effort:
// no origin -> a note, no error. Idempotent, and it removes the pre-sync
// refspecs so upgrades converge.
func addRefspec(g *gitcmd.Git) {
	if _, err := g.Run("remote", "get-url", "origin"); err != nil {
		fmt.Println("side-quest: no 'origin' remote — skipped refspec (add it later or use sync).")
		return
	}
	const oldRefspec = "refs/side-quest/*:refs/side-quest/*"
	unsetConfigValue(g, "remote.origin.fetch", oldRefspec)
	unsetConfigValue(g, "remote.origin.push", oldRefspec)
	// Fetch the remote quest ref into the tracking ref sync merges from.
	ensureConfigContains(g, "remote.origin.fetch", store.FetchRefspec)
	// Deliberately NO push refspec (SQ-0121). Since SQ-0031 nothing here disables
	// push.default (the pre-push hook publishes the quest ref), so the old
	// `remote.origin.push=HEAD` compensation had no job left — while actively
	// OVERRIDING the user's push.default repo-wide (upstream/nothing/simple all
	// change meaning once any push refspec is configured). A HEAD entry an older
	// version added is left in place: silently removing config the user can see
	// (and may have set themselves) is worse — the release notes document it.
}

// unsetConfigValue removes every occurrence of an exact value from a multi-valued
// git config key. git matches --unset-all against a value regex, so we anchor and
// escape the value. A "key/value not found" (exit 5) is fine.
func unsetConfigValue(g *gitcmd.Git, key, value string) {
	re := "^" + regexp.QuoteMeta(value) + "$"
	_, _ = g.Run("config", "--unset-all", key, re)
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
