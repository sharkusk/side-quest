# PATH-relative hook shims (design B) — Design

**Quest:** SQ-0058
**Date:** 2026-07-04
**Status:** approved (brainstorm), pending implementation plan

## Problem

`side-quest install-hooks` writes each git hook as a shim that invokes the
side-quest binary by its **absolute path** (`os.Executable()` → `filepath.Abs`),
deliberately, "so the hooks always run the exact side-quest that installed them
(no PATH reliance)" — robust when git is launched by a GUI client or cron that
doesn't inherit the shell `PATH`.

That absolute path is the sole reason the shims are **machine-local**, and it
causes two failures in a tracked hooks directory (`core.hooksPath`, e.g. Husky,
a monorepo, or side-quest's own `.githooks`):

1. **Separate shim files** land as untracked files in a tracked dir — the user
   must hand-maintain `.gitignore` entries (the general form of SQ-0057).
2. **Composing into an existing *tracked* hook** (`installOneHook` appends its
   marker block to any POSIX-sh hook) writes a machine-local absolute path into
   a shared, committed file. `.gitignore` cannot help — you cannot ignore a
   modification to a tracked file. The user is forced to either commit the
   absolute path (breaking the hook for every teammate whose path differs) or
   keep a permanently dirty tree.

Failure (2) is decisive: no amount of `.gitignore` automation fixes it. Only
making the shim itself portable does.

## Decision

Adopt **design B: PATH-relative shims with graceful degradation.** Each shim
invokes `side-quest` by bare name (resolved via `PATH`) instead of an absolute
path. Shims become byte-identical across machines — committable into a shared
hook, and no longer needing any `.gitignore` management.

The cost of B is that a bare `side-quest` fails when git runs without side-quest
on `PATH` (GUI clients, cron). We accept this case as **supported but
degraded**: a missing binary must *skip cleanly and warn*, never block a commit.

### Why not the alternatives

- **Design A (absolute path + auto-managed `.gitignore`):** covers only the
  separate-shim case; does nothing for composing into a tracked hook (failure 2).
- **Absolute-path fallback (try `PATH`, else a recorded absolute path):** would
  re-embed a machine-local path in the shim, defeating byte-identical
  committability. Rejected.

## Shim format

Every shim wraps its `side-quest …` call in a `command -v` guard, written as an
`if/else` (not `… || { …; exit 0; }`). The `if/else` lets control flow *through*
the block, so any hook content that ends up after our marker block still runs —
an early `exit` would silently skip it.

Rendered `post-commit` (a non-blocking hook), freshly created:

```sh
#!/bin/sh
# >>> side-quest >>>
# side-quest-version: 0.1.0
if command -v side-quest >/dev/null 2>&1; then
	side-quest link HEAD || true
else
	echo "side-quest: not on PATH — skipping (add it to PATH; see install-hooks)" >&2
fi
# <<< side-quest <<<
```

Rendered `commit-msg` block (the one blocking hook). It keeps **no** `|| true`,
so a real `require_quest` rejection still returns non-zero and blocks the commit;
but a *missing binary* takes the `else` branch, whose last command is a
successful `echo`, so the hook exits 0 and the commit proceeds:

```sh
if command -v side-quest >/dev/null 2>&1; then
	side-quest commit-msg "$@"
else
	echo "side-quest: not on PATH — skipping (add it to PATH; see install-hooks)" >&2
fi
```

### Guard behavior summary

| Situation | commit-msg | prepare-commit-msg / post-commit / pre-push |
|---|---|---|
| binary present, succeeds | exit 0 (commit proceeds) | runs, `|| true` → exit 0 |
| binary present, rejects (`require_quest`) | **exit non-zero (blocks)** | `|| true` → exit 0 (never blocks) |
| binary missing | else branch → **exit 0 (never blocks)** + warn | else branch → exit 0 + warn |

## Implementation shape

In `cmd/side-quest/hooks.go`:

- Replace the `q := shimQuotedPath(self)` line-builder and the `shims` table
  bodies with a helper:

  ```go
  // guardedShim renders a PATH-relative hook body: it runs `side-quest
  // <invocation>` only when the binary is on PATH, and otherwise warns and
  // skips (never blocking). block=false appends `|| true` so the hook never
  // fails on side-quest's own exit status; block=true (commit-msg) omits it so
  // a require_quest reject still blocks the commit.
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
  ```

  The `shims` table becomes:

  ```go
  shims := []struct{ name, body string }{
  	{"prepare-commit-msg", guardedShim(`prepare-commit-msg "$@"`, false)},
  	{"commit-msg", guardedShim(`commit-msg "$@"`, true)},
  	{"post-commit", guardedShim("link HEAD", false)},
  	{"pre-push", guardedShim(`pre-push "$@"`, false)},
  }
  ```

- **Remove as now-dead** (orphaned by the change, nothing else uses them):
  - `os.Executable()` + `filepath.Abs(self)` block (currently hooks.go:56-63).
  - `shimQuotedPath` and its Windows-backslash rationale (currently 135-147).
  - Rewrite the `cmdInstallHooks` doc comment (44-46) that claims the shims call
    the binary "by absolute path … (no PATH reliance)" — now false.

The marker/version machinery (`hookBlock`, `parseHookVersion`, `installOneHook`,
`hookRefreshNote`) is unchanged.

## Migration

No new migration mechanism. The `version` stamp bumps, so re-running
`install-hooks` (or `onboard`) replaces the old absolute-path block in place via
the existing marker match (`hookUpdated`), and `hookRefreshNote` already reports
"binary path or shim format changed." Nothing auto-migrates without a re-run —
consistent with how upgrades already work, and acceptable pre-launch (effectively
only the maintainer's own repos exist).

## Scope boundary: SQ-0057 is *not* retired

B makes separate shim files *eligible* to commit, but committing generated,
version-stamped files invites churn (a contributor re-running install-hooks with
a different build `version` re-stamps them → dirty tree). So for side-quest's own
repo we **keep the shims gitignored** and commit the pending SQ-0057 `.gitignore`
fix as-is. B's committability benefit is realized unconditionally for the
*composed-into-tracked-hook* case, and is *available* to any user who chooses to
commit their separate shims — documented as a choice, not forced.

## Documentation changes

- **`docs/manual-setup.md` "Existing git hooks"** (37-49): the appended block is
  now portable (`side-quest` via `PATH`) and therefore safe to commit into a
  shared hook; add the new dependency — side-quest must be on the `PATH` git runs
  under, else the hook skips with a warning (GUI/cron without it → no-op, never
  blocks).
- **`docs/manual-setup.md` install-hooks description** (31-35): note the shims
  call `side-quest` on `PATH` rather than by absolute path.
- Document the separate-shim choice (commit *or* gitignore) briefly.
- Cross-reference the existing MCP-server `PATH` note (75-78); the hooks now share
  that exact dependency.
- **`docs/plugin.md`**: one line — hooks rely on the same agent-`PATH` the plugin
  provides (agent-run git works; a bare terminal needs side-quest installed).

## Testing

**Unit (on the generated text):**
- shim contains `command -v side-quest` and bare `side-quest <subcmd>`, and no
  absolute path;
- `commit-msg`'s side-quest line has no `|| true`; the other three do;
- `guardedShim(x, true)` and `guardedShim(x, false)` differ only by the `|| true`
  tail.

**Behavioral (render a hook, run it under `sh` with a controlled `PATH`):**
- missing binary → `commit-msg` exits **0** and warns on stderr (never blocks);
- a stub `side-quest` on `PATH` exiting 1 → `commit-msg` exits **1** (still
  blocks);
- stub exiting 0 → `commit-msg` exits 0;
- `post-commit` with missing binary → exits 0 and warns;
- **compose-safety:** a hook whose text is the side-quest block followed by
  `echo AFTER`, run with the binary missing, still prints `AFTER` (proves the
  `else` branch flows through, no early exit);
- **refresh:** an existing old absolute-path block is replaced in place by the
  portable block (`hookUpdated`), not duplicated.

**Manual (not automated):** the plugin-PATH end-to-end check below.

## Assumptions to verify

**Plugin PATH reaches agent-run git.** The Claude Code plugin puts side-quest on
the *agent's* `PATH` (`docs/plugin.md:24-26`), so agent-run `git commit` should
let the hook's `command -v side-quest` resolve. If that injection reaches only
the MCP server launch and **not** Bash-tool subprocesses, agent-run git would
also fail to find the binary, and graceful degradation would *silently mask* it
(hooks appear installed but never link).

Verification (do before relying on the plugin flow): a plugin-only install with
no side-quest on the shell `PATH` (e.g. no `~/go/bin` copy), have the agent make
a commit, and confirm the `post-commit` link actually fired. If it does not, that
is a separate plugin-PATH-plumbing finding, and the skip-warning must be made
discoverable rather than swallowed.
