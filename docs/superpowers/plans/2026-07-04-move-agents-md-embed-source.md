# Move the AGENTS.md embed source out of repo root Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Relocate the `//go:embed` agent-guidance source from root `AGENTS.md` to `internal/guidance/agents.md` so it can't be mistaken for the repo's own config and corrupted, guarded by a test that the embedded template stays unwrapped.

**Architecture:** Fold the embed into the existing `internal/guidance` package (beside `core.md`), delete the now-empty root `package sidequest` (`assets.go`), repoint the single consumer (`onboard.go`) and three drift tests, and reword two user-facing doc links to point at the `side-quest agents-md` command instead of a browsable file.

**Tech Stack:** Go (`//go:embed`), git.

## Global Constraints

- **TDD for code:** the guard test is written first and must fail (missing `guidance.Agents` symbol) before the move. Docs are TDD-exempt.
- **The embed source stays UNWRAPPED:** `internal/guidance/agents.md` must never contain a `>>> side-quest >>>` marker; markers are added at runtime by the emitter only.
- **Behavior of `agents-md` / `onboard --agents-md` is unchanged** — only where the template lives changes.
- **Var name:** `guidance.Agents` (parity with `guidance.Core`).
- **Branch-safety (HARD):** only ever write `refs/side-quest/*`, `.git/hooks` (or configured hooksPath), and a scratch index. Never the user's branches/index/worktree.
- **Commit only when the user asks.** Executing this plan authorizes its per-task commits.
- **Commit trailer** (blank line before the co-author block):
  ```
  Quest: SQ-0059        (Task 1)   /   Completes: SQ-0059   (Task 2, the final task)

  Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws
  ```

---

### Task 1: Move the embed source into `internal/guidance` + guard test

This is an atomic refactor: the build is red partway through (assets.go embeds a file that no longer exists), so all steps land in one commit. Follow them in order.

**Files:**
- Create: `internal/guidance/agents.md` (via `git mv` from root `AGENTS.md`)
- Modify: `internal/guidance/guidance.go` (add the embed)
- Modify: `internal/guidance/guidance_test.go` (add the guard test)
- Delete: `AGENTS.md` (root, via `git mv`), `assets.go` (root)
- Modify: `cmd/side-quest/onboard.go` (import swap + line 31)
- Modify: `internal/packaging/manifests_test.go` (repoint three tests)

**Interfaces:**
- Produces: `guidance.Agents string` — the agent-agnostic guidance template, unwrapped (replaces `sidequest.AgentsGuidance`).
- Consumes: existing `guidance.Core`.

- [ ] **Step 1: Write the failing guard test**

Add to `internal/guidance/guidance_test.go` (it already imports `strings` and `testing`):

```go
// TestAgentsTemplateIsUnwrapped (SQ-0059): the embedded agents-guidance template
// must stay UNWRAPPED — the refresh markers are added at runtime by the emitter,
// never stored in the source. This is what makes onboard-ing side-quest's own repo
// unable to corrupt the embed source. It must also carry the core verbatim.
func TestAgentsTemplateIsUnwrapped(t *testing.T) {
	if strings.Contains(Agents, ">>> side-quest >>>") {
		t.Error("guidance.Agents contains a side-quest marker; the embed source must be unwrapped")
	}
	if !strings.Contains(Agents, Core) {
		t.Error("guidance.Agents must contain guidance.Core verbatim")
	}
}
```

- [ ] **Step 2: Run the guard test to verify it fails**

Run: `go test ./internal/guidance/ -run TestAgentsTemplateIsUnwrapped`
Expected: FAIL — compile error, `undefined: Agents`.

- [ ] **Step 3: Move the file**

Run: `git mv AGENTS.md internal/guidance/agents.md`
(Content is unchanged — it is still the unwrapped agent-agnostic template.)

- [ ] **Step 4: Fold the embed into `internal/guidance/guidance.go`**

After the existing `var Core = strings.TrimSpace(coreRaw)` line, add:

```go

//go:embed agents.md
var agentsRaw string

// Agents is the agent-agnostic guidance block that `onboard --agents-md` and
// `agents-md` emit — the UNWRAPPED template. The refresh markers are added at
// runtime by the emitter (cmd/side-quest/onboard.go), never stored here.
var Agents = agentsRaw
```

(The file already imports `_ "embed"`; no import change needed. `Agents` is left
un-trimmed because the emitter trims it at use — see Step 6.)

- [ ] **Step 5: Run the guard test to verify it passes**

Run: `go test ./internal/guidance/ -run TestAgentsTemplateIsUnwrapped`
Expected: PASS. (The root package build is still broken at this point — that's fixed in the next steps.)

- [ ] **Step 6: Delete the root package and repoint `onboard.go`**

Run: `git rm assets.go`

In `cmd/side-quest/onboard.go`, swap the import: remove
`sidequest "github.com/sharkusk/side-quest"` and add
`"github.com/sharkusk/side-quest/internal/guidance"`. The import group should end
up (gofmt order):

```go
	"github.com/sharkusk/side-quest/internal/gitcmd"
	"github.com/sharkusk/side-quest/internal/guidance"
	"github.com/sharkusk/side-quest/internal/store"
```

Then change the one usage (in `agentsBlock`):

```go
		strings.TrimRight(sidequest.AgentsGuidance, "\n") + "\n" +
```
to:
```go
		strings.TrimRight(guidance.Agents, "\n") + "\n" +
```

Run `gofmt -w cmd/side-quest/onboard.go internal/guidance/guidance.go`.

- [ ] **Step 7: Repoint the drift tests in `internal/packaging/manifests_test.go`**

Three edits:

1. In `TestAgentsDocPointsToSkill`, change
   `a := string(repoFile(t, "AGENTS.md"))`
   to
   `a := string(repoFile(t, "internal/guidance/agents.md"))`

2. In `TestFirstRunGuidancePresent`, change the loop
   `for _, f := range []string{"AGENTS.md", "skills/side-quest/SKILL.md"} {`
   to
   `for _, f := range []string{"internal/guidance/agents.md", "skills/side-quest/SKILL.md"} {`

3. In `TestGuidanceSurfacesContainCore`, **drop** `"AGENTS.md"` from the loop (the
   embedded-template arm now lives in the guidance guard test from Step 1):
   `for _, f := range []string{"AGENTS.md", "skills/side-quest/SKILL.md"} {`
   becomes
   `for _, f := range []string{"skills/side-quest/SKILL.md"} {`

- [ ] **Step 8: Build and run the full suite**

Run: `go build ./... && go test ./...`
Expected: PASS across all packages. In particular:
- `internal/guidance` — guard test green;
- `internal/packaging` — repointed drift tests green;
- `cmd/side-quest` — `onboard`/`agents-md` tests green (the binary now embeds from `internal/guidance/agents.md`, so runtime behavior is identical).
There must be no `undefined: sidequest` or missing-embed errors.

- [ ] **Step 9: Manual sanity check (the point of the change)**

Run: `go run ./cmd/side-quest agents-md | head -3`
Expected: the printed block still starts with the runtime marker
`<!-- >>> side-quest >>> -->` — proving markers are added at emit time while the
source stays unwrapped.

- [ ] **Step 10: Commit**

```bash
git add -A
git commit
```
`git add -A` is acceptable here (the working tree holds only this task's changes: the renamed file, the deleted `assets.go`, and the edited `.go` files). Use the trailer from Global Constraints with `Quest: SQ-0059`. Subject: `refactor: move AGENTS.md embed source to internal/guidance/agents.md (SQ-0059)`.

---

### Task 2: Reword the docs from a browsable file to the command

Docs are TDD-exempt; each is a plain edit. These complete SQ-0059.

**Files:**
- Modify: `README.md` (~214-217)
- Modify: `docs/manual-setup.md` (~95)
- Modify: `docs/architecture.md` (~269-272)

- [ ] **Step 1: Reword the README reinforcement bullet**

In `README.md`, replace this bullet (currently ~214-217):

```markdown
- [`AGENTS.md`](AGENTS.md) — agent-agnostic, for non-Claude tools.
  `side-quest onboard --agents-md` merges it into your project's own `AGENTS.md`
  as a marker-wrapped, version-stamped block it refreshes in place; `side-quest
  agents-md` prints it for a manual paste.
```

with:

```markdown
- **Agent-agnostic guidance**, for non-Claude tools — run `side-quest agents-md`
  to print it, or `side-quest onboard --agents-md` to merge it into your
  project's own `AGENTS.md` as a marker-wrapped, version-stamped block it
  refreshes in place.
```

- [ ] **Step 2: Reword the manual-setup pointer**

In `docs/manual-setup.md` (~95), replace:

```markdown
agent-facing guidance see [`AGENTS.md`](../AGENTS.md) and
[`skills/side-quest/SKILL.md`](../skills/side-quest/SKILL.md).
```

with:

```markdown
agent-facing guidance, run `side-quest agents-md` (the agent-agnostic block) or
see [`skills/side-quest/SKILL.md`](../skills/side-quest/SKILL.md).
```

- [ ] **Step 3: Update the architecture drift-guard description + add the source pointer**

In `docs/architecture.md` (~269-272), replace:

```markdown
Claude plugin bundles the skill; a non-Claude user opts into `AGENTS.md` with
`onboard --agents-md`. All three surfaces derive from the same `guidance.Core` — a
test in `internal/packaging` asserts `AGENTS.md` and `skills/side-quest/SKILL.md`
contain it verbatim, so they cannot drift from the source.
```

with:

```markdown
Claude plugin bundles the skill; a non-Claude user opts into `AGENTS.md` with
`onboard --agents-md`. All three surfaces derive from the same `guidance.Core`.
The agent-agnostic block is embedded from `internal/guidance/agents.md` (exposed
as `guidance.Agents`); a guard test there asserts it stays unwrapped and contains
`guidance.Core`, and a test in `internal/packaging` asserts
`skills/side-quest/SKILL.md` contains the core verbatim — so none can drift.
```

- [ ] **Step 4: Verify no other in-repo link points at the moved file**

Run: `grep -rn "](AGENTS.md)\|](../AGENTS.md)\|](./AGENTS.md)" README.md docs/ || echo "no stale links"`
Expected: `no stale links`. (Bare mentions of `AGENTS.md` as a concept or a user's own project file are fine and stay.)

- [ ] **Step 5: Commit**

```bash
git add README.md docs/manual-setup.md docs/architecture.md
git commit
```
Use the trailer with `Completes: SQ-0059`. Subject: `docs: point agent-guidance refs at the agents-md command (SQ-0059)`.
