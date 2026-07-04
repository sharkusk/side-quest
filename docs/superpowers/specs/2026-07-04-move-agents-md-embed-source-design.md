# Move the AGENTS.md embed source out of repo root — Design

**Quest:** SQ-0059
**Date:** 2026-07-04
**Status:** approved (brainstorm), pending implementation plan

## Problem

Root `AGENTS.md` serves double duty: it is both

1. the `//go:embed` source (`assets.go`, `package sidequest`) that
   `side-quest agents-md` and `onboard --agents-md` emit, and
2. treated as side-quest's own root-level reinforcement file — `README.md:214`
   links to it, and three `internal/packaging` drift tests read it via
   `repoFile("AGENTS.md")`.

That double duty is the root cause of a dogfooding corruption: because the file
sits at root named `AGENTS.md`, running `onboard --agents-md` inside side-quest's
own repo treated it as the repo's project config and merged the guidance block
*into* it — wrapping the embed source in `>>> side-quest >>>` markers and setting
up a double-wrap on the next build. It was caught and reverted by hand, but
nothing prevents a recurrence.

## Decision

Adopt the quest's Approach 1: **move the embed source into
`internal/guidance/agents.md`** (beside `core.md`, in the package that already
owns guidance), delete the now-empty root `package sidequest`, and add a guard
test that the embedded template is never marker-wrapped. This removes the root
cause rather than guarding around it.

**Accepted tradeoff:** no more browsable root `AGENTS.md` example. A
Claude-developed repo having no root `AGENTS.md` (it relies on the skill for
reinforcement) is an honest signal, not a gap.

### Rejected alternatives
- **Rename to `AGENTS.template.md` at root:** stops `onboard` treating it as
  config, but still clutters root and half-preserves the "looks shipped" confusion.
- **Guard test only, no move:** treats the symptom; leaves the misleading root file.

## Changes

### 1. The move
- `AGENTS.md` (root) → `internal/guidance/agents.md`, content unchanged (still
  leads with the core and the agent-agnostic framing).
- Fold the embed into `internal/guidance/guidance.go` beside `core.md`:
  ```go
  //go:embed agents.md
  var agentsRaw string

  // Agents is the agent-agnostic guidance block that `onboard --agents-md` and
  // `agents-md` emit — the UNWRAPPED template. The refresh markers are added at
  // runtime by the emitter, never stored here.
  var Agents = agentsRaw
  ```
  Named `guidance.Agents` for symmetry with `guidance.Core`.
- **Delete `assets.go`** — the only file in root `package sidequest`, so that
  package disappears. Verified: nothing imports the bare module path
  `github.com/sharkusk/side-quest` except `cmd/side-quest/onboard.go`.

### 2. Repoint consumers
- `cmd/side-quest/onboard.go:31`: `sidequest.AgentsGuidance` → `guidance.Agents`.
  Remove the `sidequest "github.com/sharkusk/side-quest"` import; add
  `internal/guidance` if not already imported.
- `internal/packaging/manifests_test.go`: the three tests reading
  `repoFile("AGENTS.md")` — `TestAgentsDocPointsToSkill`,
  `TestFirstRunGuidancePresent` (its AGENTS.md arm), and
  `TestGuidanceSurfacesContainCore` (its AGENTS.md arm) — repoint to
  `internal/guidance/agents.md`. The `skills/side-quest/SKILL.md` arms are unchanged.

### 3. The guard test (the "never again" fix)
In `internal/guidance/guidance_test.go`:
```go
// The embedded agents template must stay UNWRAPPED — the refresh markers are a
// runtime concern. This is what makes onboard-ing side-quest's own repo unable to
// corrupt the embed source (SQ-0059).
func TestAgentsTemplateIsUnwrapped(t *testing.T) {
	if strings.Contains(Agents, ">>> side-quest >>>") {
		t.Error("guidance.Agents contains a side-quest marker; the embed source must be unwrapped")
	}
	if !strings.Contains(Agents, Core) {
		t.Error("guidance.Agents must contain guidance.Core verbatim")
	}
}
```
The second assertion absorbs the `AGENTS.md`-contains-`Core` check that
`manifests_test.go` currently does by file path, testing the embedded string
directly.

### 4. Docs (reword to the command, not a browsable file)
- `README.md:214` and `docs/manual-setup.md:95`: replace the
  `[AGENTS.md](AGENTS.md)` / `[AGENTS.md](../AGENTS.md)` links with a pointer to
  `side-quest agents-md` (prints the agent-agnostic block) — no link into
  `internal/`.
- `docs/architecture.md`: one contributor-facing line noting the embed source now
  lives at `internal/guidance/agents.md`.
- Leave unrelated mentions of `AGENTS.md` (the concept / a user's own project
  file) as-is; only the two links to the moved file change.

## Testing / verification
- `go build ./... && go test ./...` green (root package removed, imports updated,
  drift tests repointed, new guard test passing).
- `side-quest agents-md` still prints the block **with** runtime markers.
- `side-quest onboard --agents-md` in a scratch repo still creates/merges a
  project `AGENTS.md` correctly.
- **Proof the bug is dead:** running `onboard --agents-md` inside side-quest's own
  repo creates a *separate* root `AGENTS.md` (a normal user artifact) and cannot
  reach `internal/guidance/agents.md`.

## Out of scope
- The `onboard`/`agents-md` runtime marker-wrapping logic is unchanged.
- The MCP `instructions` path (`guidance.Core`) is unchanged.
- No change to what the guidance block *says*.
