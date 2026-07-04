# `side-quest show` dumps linked commit messages — Design

**Quest:** SQ-0062
**Date:** 2026-07-04
**Status:** approved (brainstorm), pending implementation plan

## Problem

`side-quest show <id>` lists a quest's linked commits as bare SHAs
(`commits: <sha1>, <sha2>`). Auditing what a quest actually did means copying
each SHA and running `git show` by hand. The messages are one `git` call away.

## Decision

Enrich the **human** `show` render (CLI only) so each linked commit prints with
its message. No change to the MCP `quest_show` tool or to `show --json` — an
agent that needs a message can run `git show` itself, and keeping commit bodies
out of the JSON keeps every agent read lean.

### Scope
- **In:** the human render of `cmd/side-quest` `show`; a new `--full` flag; a
  store helper to read a commit's message via the existing git handle.
- **Out:** MCP `quest_show` (unchanged), `show --json` (unchanged — still the raw
  quest with 40-char SHAs), any other command.

## Behavior

The current single line
```
commits: <sha1>, <sha2>
```
is **replaced** by a per-commit listing (each SHA appears exactly once, now as
the abbreviated handle beside its subject — no separate redundant SHA line):

**Default** — `side-quest show SQ-0059`:
```
commits:
  b510826  refactor: move AGENTS.md embed source to internal/guidance/agents.md (SQ-0059)
  d5eb4b2  docs: point agent-guidance refs at the agents-md command (SQ-0059)
```

**`--full`** — `side-quest show --full SQ-0059`: same header line per commit, each
followed by the complete message (body + trailers), indented under it:
```
commits:
  b510826  refactor: move AGENTS.md embed source to internal/guidance/agents.md (SQ-0059)

      <body paragraphs and Quest:/Co-Authored-By trailers, indented>

  d5eb4b2  docs: point agent-guidance refs at the agents-md command (SQ-0059)

      <body>
```

### Edge cases
- **Missing commit** (rebased / gc'd — the SHA no longer resolves): the line
  renders `<stored-sha>  (message unavailable)` and `show` still succeeds. `--full`
  behaves the same for that commit (no body).
- **No linked commits:** the `commits:` section is omitted entirely, as today.
- The abbreviated SHA and subject come from git (`%h`, `%s`); a missing commit
  falls back to the stored SHA.

## Components

### 1. Store helper (reads via the existing git handle)
`internal/store` already holds `s.git *gitcmd.Git` at the repo top. Add:
```go
// CommitMessage returns a commit's abbreviated SHA and its message for `show`.
// full=false returns the subject line as text; full=true returns the complete
// message. ok is false when the SHA no longer resolves to a commit (rebased/gc'd)
// — the caller renders a placeholder rather than failing.
func (s *Store) CommitMessage(sha string, full bool) (short, text string, ok bool)
```
Implementation: one `git show -s --format=%h%x00%s%x00%B <sha>` call, split on
NUL into (abbrev, subject, full body); pick subject or body by `full`. A git
error (unknown revision) → `ok=false`.

### 2. `cmdShow` (cmd/side-quest/cli.go)
- Add `--full` to the flag set: `fs.BoolVar(&full, "full", false, "with the linked commits, print each commit's complete message (default: subject only)")`.
- After loading the quest, resolve each `q.Commits[i]` through
  `store.CommitMessage(sha, full)` into a small display slice, and pass it to the
  renderer. `--json` path is untouched (returns before this).

### 3. `renderShow` (cmd/side-quest/render.go)
- Replace the `commits: <joined>` line with the per-commit block described above,
  driven by the resolved display slice. Subject lines wrap with the existing
  `wrapText`/width logic; `--full` bodies print with a fixed indent, each line
  passed through the same width wrapping so long lines fold and short ones (blank
  lines, trailers) stay put.

## Testing
- **Store** (`internal/store`): a temp repo with two commits linked to a quest —
  `CommitMessage(sha, false)` returns the abbreviated SHA and subject;
  `CommitMessage(sha, true)` returns a body containing a known trailer; a bogus
  SHA returns `ok=false`.
- **CLI** (`cmd/side-quest`, via the built binary):
  - `show <id>` default lists each commit as `<short>  <subject>` and no longer
    prints a comma-joined SHA line;
  - `show --full <id>` includes a commit body line (e.g. a `Co-Authored-By:` or a
    known body word);
  - a quest whose linked SHA has been removed prints `(message unavailable)` and
    exits 0;
  - update the existing `show` assertion in `cli_test.go` for the new commits
    format;
  - `show --json <id>` is unchanged (still the raw quest).

## Out of scope
- MCP `quest_show` and `show --json` output shape.
- Any change to how commits are linked or stored.
