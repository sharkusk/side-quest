# side-quest MCP-Delivered Guidance — Design (SQ-0051)

**Goal:** Make the MCP server the primary carrier of side-quest's agent guidance,
so an agent using the server gets the core know-how portably (any MCP client) with
minimal always-on context clutter. The Claude skill and the `AGENTS.md` merge become
*opt-in reinforcement*, not the default.

**Why now:** Today the guidance ships as two hand-mirrored files — the embedded
`AGENTS.md` (merged into a project by `onboard`) and `skills/side-quest/SKILL.md`
(the Claude plugin). This has two costs the launch review surfaced: it isn't
portable (a Claude skill only works in Claude Code; the AGENTS.md merge mutates the
user's repo), and it clutters always-on context even when the agent already loads
the tools. The MCP server can carry its own guidance natively, and currently does
not: `internal/mcp/server.go:20` passes `nil` for `ServerOptions`, leaving the
protocol's `instructions` channel unused.

## Decisions (locked in brainstorming)

- **Baseline vs. reinforcement.** The MCP server carries the *core* guidance and is
  the baseline everywhere. The Claude skill (bundled in the plugin) and the
  `AGENTS.md` block are *optional reinforcement*.
- **The Claude plugin stays batteries-included.** Installing the full plugin bundles
  the skill + `/sq` — that install *is* the Claude reinforcement choice. The
  baseline-only route is adding just the MCP server to a client's config.
- **`onboard` stops merging `AGENTS.md` by default.** It still does
  `init` + `install-hooks` + write `.mcp.json`. A new `--agents-md` flag opts into
  the merge; the existing `side-quest agents-md` print-for-paste command is unchanged.
- **Content architecture = Approach A: one canonical core, composed outward.** A
  single embedded *core* brief is the source of truth. MCP `instructions` uses it
  verbatim; the skill and `AGENTS.md` must contain it and add their own framing.
  Only the core is single-sourced (the reinforcement text stays authored
  per-surface) — enough to stop drift on the essentials without coupling the two
  reinforcement surfaces.
- **Auto-classify when obvious.** The guidance tells the agent to set `type`/
  `priority` only when the request makes them obvious (a crash/regression is a bug;
  explicit "urgent"/"critical"/"blocking" is high) and otherwise keep defaults —
  replacing the current "never set them unless the user stated them" rule. This new
  philosophy propagates to the `/sq` command and the `quest_new` tool description so
  all surfaces agree.

## The core brief (single source of truth)

The canonical text, ~100 words, sized to sit in an always-on channel:

```
side-quest is a git-native tracker; a "quest" is just an issue, task, or
follow-up you manage through these tools, not by editing files.

- Capture without derailing. An idea surfaces mid-task? File it with quest_new
  (one-line restatement + why it came up) and resume. Set type/priority only
  when the request makes it obvious — a crash or regression is a bug;
  "urgent"/"critical"/"blocking" is high — else keep defaults.
- Work one at a time. Make the quest you're on current (quest_set_current); the
  git hooks then link your commits to it — you never touch hashes.
- Close it by committing "Completes: SQ-1234" (or "Quest: SQ-1234" to link
  only), or quest_set_status.
- List work with quest_list; read one with quest_show.
```

Every downstream surface either uses this verbatim (MCP `instructions`) or must
contain it (the skill, the `AGENTS.md` block).

## How guidance reaches the agent

```
                         ┌─────────────────────────────┐
   guidance.Core (embed) │  the single canonical brief  │
                         └──────┬───────────┬───────────┘
             verbatim ──────────┘           └────────── must contain (drift-guarded)
                    │                                    │
        ┌───────────▼───────────┐            ┌───────────▼────────────┐
        │ MCP instructions       │            │ Reinforcement surfaces │
        │ + enriched tool descrs │            │  skill · AGENTS.md      │
        │ (baseline, portable)   │            │  (opt-in)               │
        └────────────────────────┘            └────────────────────────┘
```

- **Baseline (every MCP client):** on connect, the server returns `guidance.Core`
  in the `initialize` response's `instructions` field (clients *may* inject it into
  the system prompt), and the client lists the tools — so the *enriched tool
  descriptions* are always in context. Tool descriptions are the reliable floor:
  a client can't use a tool without loading its description.
- **Reinforcement (opt-in):** the Claude plugin bundles the skill (loaded on
  demand); a non-Claude user runs `onboard --agents-md` or `agents-md` to add the
  block.

### Division of labor: where each feature's guidance lives

The core brief carries the *model*, not a *catalog* — which is what keeps it ~100
words. Guidance for a feature lives at the tier matching how often it's needed and
how obvious it is:

1. **Core brief (MCP `instructions`)** — the mental model and the non-obvious habits
   an agent won't infer from the tools alone: the capture reflex,
   auto-classify-when-obvious, "set current and the hooks link your commits," and the
   two trailer forms.
2. **Tool descriptions (always loaded, self-describing)** — the per-feature mechanics
   for everything else. The agent learns each long-tail feature from the tool it
   would call, exactly when it's relevant, so none of these need a core-brief mention:
   - `quest_note` — append a timestamped note.
   - `quest_update` — change title and/or tags.
   - `quest_reclassify` — change type and/or priority.
   - `quest_relink_commit` — repoint a recorded commit after a rebase rewrote its hash.
   - `quest_unlink_commit` — remove a recorded commit.
   - `quest_show` / `quest_get_current` / `quest_set_status` / `quest_link_commit` —
     read/lifecycle/hook-facing operations.
3. **Reinforcement surfaces (skill / `AGENTS.md`, opt-in)** — fuller prose for
   *situational multi-step workflows* no single tool description captures. The prime
   example is the rebase→relink repair: after a rebase the `post-commit`/`pre-push`
   hooks auto-link the *new* commit, but cannot remove the *old, dangling* entry —
   `quest_relink_commit` is the corrective. Occasional and detailed, so it belongs
   here rather than the always-on core.

**Implication for the tool-description changes below:** only `quest_new` and
`quest_set_current` get enriched, because they carry *core habits* that must survive a
client that drops `instructions`. The long-tail descriptions above are already
adequate and stay as-is (the plan gives them only a light consistency pass).

## Components & changes

### New: `internal/guidance`

- Embeds `core.md` (the brief above) and exposes it as `guidance.Core` (a trimmed
  string). One responsibility: own the canonical text. Pure, no I/O.

### `internal/mcp`

- `server.go`: pass `&sdk.ServerOptions{Instructions: guidance.Core}` to
  `sdk.NewServer` instead of `nil`.
- `tools.go`: enrich two descriptions so the essentials survive clients that drop
  `instructions`:
  - `quest_new` — add the reflex + auto-classify cue: "Capture a tangent that
    surfaced mid-task without derailing. Set type/priority only when the request
    makes them obvious (a crash/regression is a bug; explicit urgency is high),
    else keep defaults."
  - `quest_set_current` — "The quest you're actively working on; the git hooks then
    link your commits to it automatically."

### `cmd/side-quest` (onboard / CLI)

- `onboard`: remove the default `installAgentsGuidance` call; gate it behind a new
  `--agents-md` flag. `init` + `install-hooks` + `.mcp.json` writing are unchanged.
- `agents-md` command: unchanged mechanism (marker-wrapped, version-stamped block);
  its emitted body now leads with `guidance.Core`.

### Reinforcement surfaces (must contain the core)

- `skills/side-quest/SKILL.md`: reframed to lead with `guidance.Core`, then its
  Claude-specific framing (`/sq`, skill-invocation notes).
- `AGENTS.md` (embedded): leads with `guidance.Core`, then agent-agnostic framing.
- `commands/sq.md` (`/sq`): update the capture instruction from "don't set
  type/priority unless the user stated them" to the auto-classify-when-obvious rule,
  matching the core.

### Docs

- `README.md`: rewrite the two-surface story → "the MCP server carries the core
  guidance; the skill (Claude) and `AGENTS.md` (non-Claude) are optional
  reinforcement." Update the `onboard` description (no longer merges `AGENTS.md`
  unless `--agents-md`). Must not trip `TestReadmeReframedAndToneRemoved` (no
  tone/voice strings).
- `docs/architecture.md`: update the `AGENTS.md` guidance section; add a short "how
  guidance reaches the agent" note (instructions + tool descriptions + opt-in
  reinforcement). Living-doc rule applies — same change as the behavior.
- `docs/manual-setup.md`, `docs/plugin.md`: adjust setup steps to the new default
  (MCP server carries guidance; AGENTS.md is an opt-in extra).
- **User-side reinforcement how-to** (`docs/manual-setup.md`, new subsection
  "Customizing guidance for your project"): document the pattern for teaching an
  agent a project convention that the universal core *deliberately omits*, using a
  **tag taxonomy** as the worked example. side-quest ships tag *mechanics* (`tags` on
  `quest_new`/`quest_update`, `--tag` and `--filter`) but no taxonomy — there's no
  universal one. So the user declares theirs in their *own* `AGENTS.md`/`CLAUDE.md`/
  custom skill (e.g. "tag each quest with `area=<subsystem>`; bugs also with
  `platform=<os>`"); the agent applies it on capture; they filter/report with
  `list --filter "area=parser and bug"`. This makes the reinforcement layer concrete
  — it is where *project-specific* conventions live, not merely a fuller copy of the
  core. The core brief and tool descriptions stay taxonomy-free.

## Testing (TDD)

- `internal/guidance`: `guidance.Core` is non-empty and within a length bound (e.g.
  ≤ 1200 bytes) so it stays compact; contains the key tool names (`quest_new`,
  `quest_set_current`).
- **Drift guard** (`internal/packaging/manifests_test.go`, which already reads
  repo-root files like `AGENTS.md` and `commands/sq.md` via `repoFile`): the shipped
  `AGENTS.md` and `skills/side-quest/SKILL.md` each contain `guidance.Core` verbatim.
  This is the mechanism that keeps the single source honest — matching the project's
  existing test-enforced-manifest style (e.g. `TestReadmeReframedAndToneRemoved`,
  the VERSION/`plugin.json` sync test).
- `internal/mcp/server_test.go`: the server advertises
  `Instructions == guidance.Core` (assert via the SDK's server/initialize surface).
- `cmd/side-quest` onboard tests:
  - bare `onboard` writes **no** `AGENTS.md`;
  - `onboard --agents-md` merges the block (contains `guidance.Core`);
  - `agents-md` prints a block containing `guidance.Core`.
- `/sq` alignment: a check (packaging manifest test) that `commands/sq.md` no longer
  carries the "don't set type/priority" rule and reflects auto-classify.

## Assumptions & risks

- **Stated assumption:** within Claude, a skill is as good as `AGENTS.md`. The design
  relies on this — Claude users get reinforcement via the bundled skill and are never
  asked to also merge `AGENTS.md`.
- **Open uncertainty — does Claude Code surface MCP `instructions` into context?**
  The spec includes an empirical check (connect the server; confirm whether the
  `instructions` text appears in the model's context). If it does *not*, the enriched
  tool descriptions remain the reliable floor, and the bundled skill covers Claude —
  so the design degrades gracefully. Record the finding in `architecture.md`.

## Out of scope

- Changing the tool set, the store, or the hook mechanics.
- MCP prompts/resources as additional guidance channels (instructions + tool
  descriptions suffice for the core).
- Unifying the skill and `AGENTS.md` *reinforcement* bodies into one shared source
  (that was Approach C; only the core is single-sourced here).

## Success criteria

- An agent connected to only the MCP server (no skill, no `AGENTS.md`) can capture,
  work, link, and close quests correctly from the server-delivered guidance alone.
- `guidance.Core` is the single source: `AGENTS.md`, the skill, and MCP
  `instructions` cannot drift on the core (test-enforced).
- `onboard` no longer mutates a project's `AGENTS.md` unless `--agents-md` is passed.
- `/sq`, the `quest_new` description, and the core all express the same
  auto-classify-when-obvious rule.
- The docs show the user-side reinforcement pattern concretely — teaching an agent a
  tag taxonomy the core deliberately omits — so a reader can wire up their own
  project convention.
- `go test ./...` stays green (including the README/tone invariants and the new
  drift guard).
```
