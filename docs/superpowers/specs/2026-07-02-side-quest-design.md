# side-quest — Design Spec

**Date:** 2026-07-02
**Status:** Approved for implementation planning
**Author:** Marcus Kellerman (with Claude)

---

## 1. Overview & Problem Statement

`side-quest` is a lightweight, project-specific quest/task tracker for capturing and
managing "side quests" — the new ideas, follow-ups, and research tangents that occur to a
developer *during* unrelated work — without derailing the current session, and while
keeping a clean two-way link between quests and the git commits that address them.

It replaces an existing `TODO.md` + `COMPLETED.md` + `scripts/todo-done` workflow (used in
the author's `babelmap` project). That workflow has two structural weaknesses this project
fixes:

1. **The chicken-and-egg link.** A quest should reference the commit(s) that worked on or
   completed it, but a commit's hash does not exist until *after* the commit is made. When
   the quest data lives as tracked files in the same repo, recording the hash back into the
   quest requires a *second* commit — which has its own hash — and the loop never closes
   cleanly.
2. **Clunky, collision-prone ids.** The old system allocated random ids from frozen,
   per-worktree file snapshots with no shared coordination point, because there was nowhere
   to serialize allocation safely.

### The core insight

Store quest data on a **dedicated git ref** (`refs/side-quest/quests`) with its own root
history, off the main line and never checked out into the working tree. A `post-commit`
hook can then write the *now-known* commit hash into the relevant quest as a **separate
commit on that ref** — and nobody needs to record *that* commit's hash anywhere, so the
loop closes. The same ref is a single shared object that all worktrees contend on, giving
us an atomic allocation point (compare-and-swap) the old design lacked.

---

## 2. Goals & Non-Goals

### Goals

- Frictionless in-session capture (`/sq <idea>`) that does **not** interrupt current work.
- Clean, automatic two-way linking between quests and commits, no chicken-and-egg.
- Project-specific data that travels with the repo (pushable).
- Safe under multiple concurrent git worktrees.
- Configurable id strategy: human-friendly **sequential** (solo default) or collision-proof
  **random** (teams / concurrent clones).
- Date-based sorting that works regardless of id strategy.
- Usable by **any** MCP-capable AI agent, not just Claude.
- Distributable as a standalone Go binary, a public GitHub project (good README), **and** a
  Claude Code plugin.
- A fun, toggleable *Dungeon Crawler Carl*–flavored voice for human-facing messages.

### Non-Goals (v1)

- No web UI, no server/daemon beyond the stdio MCP server.
- No cross-clone distributed-counter guarantees for **sequential** ids (random mode is the
  answer for that case).
- No dependency-graph / blocking-relationships between quests (tags can approximate).
- No time tracking, assignees, or sprint/board concepts (this is deliberately *not* JIRA).

---

## 3. Core Concepts

- **Quest** — one unit of work/idea. Stored as a single Markdown file with YAML frontmatter.
- **Id** — `SQ-` + either a zero-padded sequential number (`SQ-0001`) or 6 hex chars
  (`SQ-a3f9c2`), per configured strategy.
- **Status** — one of `open`, `partial`, `done`, `deferred`, `discarded`.
- **Tags** — arbitrary key/value metadata (area, horizon, category, imported markers…).
  Everything not in the minimal core lives here.
- **Context** — an optional short block auto-captured at creation, so a quest is legible
  days later.
- **Commit links** — hashes of code commits that referenced the quest via a trailer.
- **Current quest** — a per-worktree pointer to "the quest I'm working on right now,"
  used to auto-fill commit trailers.

---

## 4. Architecture

One Go binary, two thin frontends over a shared core library:

```
                       ┌─────────────────────────┐
   side-quest <cmd>    │   cli (cobra-style)      │
   (also git hooks) ─▶ │                          │─┐
                       └─────────────────────────┘ │
                       ┌─────────────────────────┐ │   ┌──────────────────┐
   side-quest serve ─▶ │   mcp (stdio server)    │─┼──▶│   core library   │
   (AI agents)         └─────────────────────────┘ │   │  quest / store / │
                                                    │   │  config / voice  │
                                                    │   │  trailer / hooks │
                                                    │   │  importer/gitcmd │
                                                    │   └────────┬─────────┘
                                                    │            │
                                                    └────────────┤ shells out to
                                                                 ▼
                                                          git (plumbing)
                                                       refs/side-quest/quests
```

Both frontends call identical core logic, so the CLI, the git hooks, and any AI agent all
behave the same. The core never manipulates the working tree; it talks to git purely
through plumbing commands via the `gitcmd` wrapper.

### Module boundaries

Each is independently understandable and testable (what it does / how you use it / what it
depends on):

| Module     | Responsibility                                                      | Depends on        | Purity |
|------------|---------------------------------------------------------------------|-------------------|--------|
| `quest`    | Quest model + frontmatter (de)serialization                         | —                 | pure   |
| `config`   | On-ref config model (`tone`, `id_strategy`, `seq_next`, …)           | `quest` types     | pure   |
| `trailer`  | Parse `Quest:` / `Completes:` trailers from a commit message         | —                 | pure   |
| `importer` | Parse legacy `TODO.md` / `COMPLETED.md` into quests                  | `quest`           | pure   |
| `voice`    | Map message keys → flavored/plain strings; randomized line pools     | `config`          | pure   |
| `gitcmd`   | Thin, typed wrapper over `git` plumbing subprocess calls            | os/exec, git      | I/O    |
| `store`    | Orphan-ref CRUD, CAS-guarded writes, id allocation                  | `gitcmd`,`quest`  | I/O    |
| `hooks`    | Install git hooks + push/fetch refspec; hook entrypoints            | `gitcmd`,`store`  | I/O    |
| `cli`      | Command-line frontend                                                | core              | I/O    |
| `mcp`      | stdio MCP server frontend                                            | core              | I/O    |

Pure modules get table-driven unit tests. I/O modules get integration tests against a
throwaway temp git repo.

---

## 5. Storage Design

### 5.1 The orphan ref

All quest data lives in a tree committed to **`refs/side-quest/quests`**. This is a custom
ref namespace, deliberately **not** under `refs/heads/*`, so:

- it never appears in `git branch` and can't be accidentally checked out,
- it is still a first-class ref that can be pushed/fetched,
- it is **shared across all linked worktrees** (worktrees share `refs/` except `HEAD` and a
  few per-worktree pseudo-refs), so every lane sees the same quest set.

The tree on that ref looks like:

```
_config.yaml            # on-ref configuration (see §7, §12)
quests/
  SQ-0001.md
  SQ-0002.md
  SQ-a3f9c2.md
```

### 5.2 Reading

`git cat-file` / `git ls-tree` against the ref tip. The store builds an in-memory view by
listing `quests/` and reading the files it needs. Listing ids is `git ls-tree --name-only`
(cheap).

### 5.3 Writing (plumbing, no checkout)

Every mutation builds a new commit on the ref without touching the working tree, using a
**temporary index** (`GIT_INDEX_FILE` pointing at a scratch file):

1. `read-tree <old-tip-tree>` into the temp index.
2. `hash-object -w` the new/updated file blob(s); `update-index --add` them.
3. `write-tree` → new tree.
4. `commit-tree <new-tree> -p <old-tip>` → new commit.
5. `update-ref refs/side-quest/quests <new-commit> <old-tip>` — **compare-and-swap**.

Step 5 succeeds only if the ref still points at `<old-tip>`. If another worktree lane moved
it in the meantime, the CAS fails; the store **re-reads the tip and retries the whole
sequence** (bounded retry loop). This makes concurrent mutations safe with no external lock.

> **Go note (for C/Python readers):** `gitcmd` returns `(stdout, err)`; there are no
> exceptions. The retry loop inspects `err` (specifically the non-fast-forward signal from
> `update-ref`) and loops. `defer` is used to clean up the temp index file.

### 5.4 File-per-quest rationale

One file per quest (rather than a single DB file) means two concurrent lanes creating
different quests touch different paths and never conflict at the tree level — matching the
author's multi-worktree workflow. It is also human-readable and hand-editable, and makes
the importer a natural fit (legacy items are already Markdown prose).

### 5.5 The filename is the id (single source of truth)

The quest's id is its filename stem: `quests/SQ-0001.md` → `SQ-0001`. The id is **not**
duplicated in the frontmatter, so there is no second source of truth to drift. The store
always discovers quests by listing `quests/`, so the id is known from the path on every
read. The `cli` and `mcp` frontends synthesize the id from the path and include it in their
output (e.g. `quest_show` returns `{ "id": "SQ-0001", ...frontmatter, "body": ... }`), so
consumers still receive an explicit id. Quest files are never renamed (ids are stable across
id-strategy switches), so the filename is a safe anchor. This is also consistent with the
`created` ground truth (§8), which is likewise keyed on the path.

---

## 6. Quest Schema

A quest file is YAML frontmatter + a free-form Markdown body. **The id is the filename**
(`quests/SQ-0001.md` → `SQ-0001`), not a stored field — see §5.5. It is synthesized from the
path and included in CLI/MCP output, so consumers still get an explicit id without it being
stored twice.

```markdown
---
title: Crash stack-trace diagnostic on VM fault
status: open            # open | partial | done | deferred | discarded
created: 2026-07-02T14:03:11Z
completed: null
commits: []             # linked code-commit hashes (full sha)
context: |              # optional; auto-filled at capture (see §11)
  branch=glulx-accel head=a62d4fa from=SQ-0007 cwd=crates/gvm
  Captured while debugging the Glulx heap fault in gvm exec.rs.
tags:                   # arbitrary k/v; area/horizon/category/import markers live here
  area: engine
  horizon: near
  legacy_id: TODO-4f8a1c
---

Full prose description goes here — the rich notes the author likes to keep,
including sub-tasks, investigation results, and "REMAINING:" breadcrumbs.
```

**Minimal core fields:** `title`, `status`, `created`, `completed`, `commits`. Optional:
`context`. Everything else is `tags`. The **id is not stored** — it is the filename (§5.5).
This keeps the schema small and lets the prose-heavy style live in the body.

---

## 7. ID Strategies

Configured on the ref in `_config.yaml`:

```yaml
id_prefix: SQ
id_strategy: sequential   # sequential | random
seq_next: 1               # next sequential number (used by sequential only)
seq_width: 4              # zero-pad width -> SQ-0001
```

### Sequential (default)

Allocation is part of the **same CAS-guarded commit** that creates the quest:

1. Read tip `T`; read `seq_next` from `_config.yaml` (= `N`).
2. Compose id `SQ-<zero-pad(N, seq_width)>`; if that filename already exists (paranoia
   guard against manual edits or a stray random-mode id), increment and retry the number.
3. Build the commit containing both the new `quests/SQ-000N.md` **and** an updated
   `_config.yaml` with `seq_next = N+1`.
4. CAS `update-ref`. On failure, re-read and restart from step 1 (so a lost race simply
   takes the next number).

Because the counter lives in the CAS-committed config, two racing lanes can never mint the
same number: only one CAS wins; the loser retries and sees `seq_next` already advanced.

### Random

6 lowercase hex chars (`SQ-a3f9c2`, ~16.7M space). No counter needed; the store still does
the "does this filename already exist?" existence check before committing. Safe even across
concurrent disconnected clones.

### Switching strategies

Flip `id_strategy` (a normal, auditable config commit). Existing ids of either form remain
valid. `seq_next` is **preserved across switches**, so a team that later consolidates to a
single maintainer can flip back to `sequential` and resume the counter. The final
filename-existence check guarantees a fluke all-numeric random id can never collide with a
sequential one at the file level.

### Guidance (documented in README)

- **Solo / single clone with many worktrees:** `sequential` — human-friendly, sortable,
  chronological; fully collision-safe via the shared-ref CAS.
- **Team / concurrent disconnected clones:** `random` — the only case where two clones can
  independently mint the same sequential number and collide on merge.

---

## 8. Dates & Sorting

Random ids discard the implicit chronological ordering that sequential ids give for free,
so ordering is carried by explicit timestamps, never inferred from the id:

- **`created`** — written into frontmatter at allocation. Its ground truth is the git
  commit time of the commit that first added `quests/SQ-xxxx.md` to the ref (recoverable via
  `git log --diff-filter=A --format=%aI -- quests/SQ-xxxx.md`). The frontmatter value is
  that time captured at write, so normal reads need no per-quest `git log`.
- **`completed`** — set on transition to `done` (or taken from the `Completes:` commit's
  time).
- **`last_commit`** (derived, not stored) — newest linked code-commit time
  (`git show -s --format=%cI <sha>`), giving a "last activity" sort.

`side-quest list --sort created|completed|updated` orders correctly under either id
strategy, where **`updated` = the most recent of (`created`, `completed`, `last_commit`)**.
Sequential still reads as a sequence; random loses nothing.

---

## 9. Commit Linking

### Trailers

Commits reference quests with git trailers:

- `Quest: SQ-xxxx` — this commit did work on the quest (appends the hash to `commits`).
- `Completes: SQ-xxxx` — as above **and** flips the quest to `done`.

Multiple trailers are allowed (a commit can touch several quests). A quest accumulates every
linked commit, so it carries its full work trail, not just the closing commit.

### Hooks (assisted, never blocking)

Installed by `side-quest install-hooks` (honors `core.hooksPath`; composes with existing
hooks rather than clobbering them):

- **`prepare-commit-msg`** — if a current quest is set for this worktree **and** the
  `auto_trailer` config flag is on (default on), inject `Quest: <current>` into the message.
  No-op when no current quest.
- **`commit-msg`** — if no `Quest:`/`Completes:` trailer is present, print a **warning**
  (in the configured voice) and allow the commit. Never blocks. An explicit `Quest: none`
  silences the warning for genuine chores.
- **`post-commit`** — call `side-quest link <new-hash>`: read the just-made commit's
  trailers and, for each referenced quest, append the (now-known) hash and close any
  `Completes:` targets. **This is where the chicken-and-egg is resolved** — the hash exists
  before the quest is updated, and the quest update is a separate commit on the orphan ref
  whose own hash nobody records.

---

## 10. Current-Quest Pointer

`side-quest current SQ-xxxx` (and MCP `quest_set_current`) records an active quest **per
worktree** in that worktree's git dir (e.g. `<gitdir>/side-quest-current`). It is:

- uncommitted (each worktree lane has its own),
- read by `prepare-commit-msg` to auto-fill the `Quest:` trailer,
- cleared with `side-quest current --clear`.

`side-quest current` with no argument prints the current pointer.

---

## 11. Quick Capture (`/sq`) & Context

### The capture contract

Whether invoked as the `/sq` slash command (AI agents) or `side-quest new "..."` (terminal),
capture is **create-and-return-only**: it creates the quest, prints one terse confirmation,
and does nothing else — no planning, no starting the work, no derailing the session.

### Context, two tiers

- **Mechanical context** — captured by the tool on *every* creation path: timestamp,
  current git branch, HEAD short-hash, current-quest pointer (if any), and cwd. Written to
  the `context` field. Zero effort, always present.
- **Narrative context** — added when capture goes through an AI agent (`/sq` or the MCP
  tool): a **one-sentence summary of what the user was doing**, written from the live
  session and prepended to `context`. Only an agent-in-the-loop can produce this, and it is
  the part that makes a quest legible days later.

The MCP `quest_new` tool accepts an optional `context` string; the `/sq` command/skill is
instructed to fill it with that one-sentence summary.

---

## 12. Voice / Tone System

A `voice` module routes all **human-facing** strings through message keys, selecting from a
randomized, categorized line pool so messages vary. Purpose: make the tool fun (a sardonic
"System" announcer, in homage to *Dungeon Crawler Carl*) without ever obscuring information.

### Tones

On-ref config `tone`, overridable per-invocation by `SIDE_QUEST_TONE`:

- **`plain`** — neutral, professional strings.
- **`dcc`** (default) — dialed-up original homage in the DCC voice: crawlers, the System,
  floors, the show/audience, sponsors, loot boxes, sarcastic achievements. **Original text
  evoking the style — no verbatim book passages** — so the public repo is IP-clean.
- **`dcc-superfan`** — opt-in tier that can use verbatim catch phrases. The verbatim lines
  are **never shipped in the repo**. They load from a **user-supplied pool file** (default
  `~/.config/side-quest/superfan-lines.txt`, overridable via config). If `tone:
  dcc-superfan` is set but no pool file is found, it **falls back to `dcc`** and prints a
  one-time hint naming the expected file path. A clearly-marked empty
  `superfan-lines.example.txt` and a README "Credits & permissions" section are provided so
  that, **if the author obtains permission from Matt Dinniman**, the pool can be dropped in
  (locally) or committed (with attribution).

### Hard rules

1. **Toggleable** (`plain` always available; `SIDE_QUEST_TONE=plain` env override).
2. **Never obscures information.** Flavor appears in confirmations and warnings only. Actual
   error messages stay clear, and **all `--json`/machine-readable output is neutral
   regardless of tone** — the System heckles humans, never scripts.
3. **Homage, not reproduction** for the shipped `dcc` pool. Verbatim content is confined to
   the un-shipped, user-supplied superfan pool.

Message keys cover at least: quest created, status transitions (each of the five statuses,
with `discarded`/`deferred` getting the most character), missing-trailer warning, empty
list, list/recap header, `install-hooks` welcome, sync/broadcast, and milestone
achievements (e.g. first quest, N discarded, N done).

---

## 13. CLI Surface

```
side-quest init                         # create the orphan ref + default _config.yaml
side-quest new "<title>" [--context S] [--tag k=v]...
side-quest list [--status S] [--tag k=v] [--sort created|completed|updated]
side-quest show <id>
side-quest edit <id>                    # opens $EDITOR on the quest markdown
side-quest done <id>                    # -> status done (manual, without a commit)
side-quest status <id> <status>         # set any status (open/partial/done/deferred/discarded)
side-quest tag <id> k=v [k=v]...        # add/update tags
side-quest current [<id> | --clear]     # get/set/clear per-worktree current quest
side-quest link <commit-sha>            # (hook entrypoint) apply a commit's trailers
side-quest import <TODO.md> <COMPLETED.md>
side-quest install-hooks                # install git hooks + push/fetch refspec
side-quest sync [--push | --pull]       # push/pull refs/side-quest/*
side-quest config [get|set] [key] [val] # read/update on-ref config
side-quest serve                        # start the stdio MCP server
```

Every command supports `--json` for neutral machine output (bypasses the voice layer).

---

## 14. MCP Surface

Tools exposed by `side-quest serve` (mirrors the CLI; minimal set):

- `quest_new(title, context?, tags?)`
- `quest_list(status?, tag?, sort?)`
- `quest_show(id)`
- `quest_update(id, title?, body?)`
- `quest_set_status(id, status)`
- `quest_tag(id, tags)`
- `quest_set_current(id | clear)`
- `quest_link_commit(sha)`

Tool responses are neutral/structured (no voice flavor), so agents parse clean data. The
`/sq` reflex and any user-facing flavor is applied by the frontend (command/skill), not the
tool payloads.

---

## 15. Git Hooks, Refspec & Sync

- `install-hooks` writes/composes `prepare-commit-msg`, `commit-msg`, `post-commit`, and
  adds a push/fetch refspec (`refs/side-quest/*:refs/side-quest/*`) to `.git/config` so
  quest data travels with the repo.
- `sync` pushes/pulls `refs/side-quest/*`. Because these refs aren't fetched by default,
  README documents that collaborators run `side-quest sync --pull` (or the tool can offer a
  fetch-refspec too).
- Hooks are thin shims that call the `side-quest` binary; all logic stays in the core.

---

## 16. Importer

`side-quest import TODO.md COMPLETED.md` — best-effort parse of the legacy format:

- `[ ]` → `open`, `[~]` → `partial`, `[x]` → `done`.
- Section: "Deferred" → `deferred`; "Will Not Implement" → `discarded`.
- `[TODO-NNNN]` → `tags.legacy_id`.
- Section headings (APP/MAP/Engine; Near/Mid/Long) → `tags.area` / `tags.horizon`.
- Inline `[category; impl-notes]` and `— completed YYYY-MM-DD` → tags / `completed`.
- Prose → quest body verbatim.

It is best-effort given the freeform source; the user reviews the generated quests after.
Sequential ids continue from `seq_next` (or import can preserve legacy ids as tags only).

---

## 17. Claude Code Plugin Packaging

The same GitHub repo doubles as an installable Claude Code plugin (confirmed against Claude
Code v2.1.196). Files at repo root:

- **`.claude-plugin/plugin.json`** — `name`, `displayName`, `version`, `description`,
  `author`, `repository`, `homepage`.
- **`.mcp.json`** — registers the stdio MCP server:
  ```json
  { "mcpServers": { "side-quest": { "command": "side-quest", "args": ["serve"] } } }
  ```
- **`.claude-plugin/marketplace.json`** — lets users
  `/plugin marketplace add <user>/side-quest` then `/plugin install side-quest`.
- **`commands/`** — thin slash commands: **`/sq`** (capture-only, per §11), plus
  `/side-quest:list`, `/side-quest:current`.
- **`skills/side-quest/SKILL.md`** — a small skill teaching the *reflex*: when a new,
  unrelated idea surfaces mid-task, capture it as a quest (with a one-sentence context note)
  instead of derailing — then keep working.

**Binary dependency:** the plugin references `side-quest` on `PATH` (installed via
`go install github.com/<user>/side-quest@latest` or a GitHub release). Plugins *can* bundle
a binary in `bin/`, but that's per-platform; v1 documents the install as a prerequisite. Git
hooks are installed by the CLI (`install-hooks`), not by the plugin.

---

## 18. Agent-Agnostic Design

The core (CLI, MCP server, hooks) makes **no Claude-specific assumptions** — MCP is an open
protocol, and the trailer/current-quest conventions are agent-neutral. A generic
**`AGENTS.md`** documents, for any MCP-capable agent: when to capture a quest, how to write
the context note, the `Quest:`/`Completes:` trailer contract, and the current-quest pointer.
Anything Claude-specific lives only in the plugin wrapper (§17), never in the core.

---

## 19. README (deliverable)

A descriptive, publish-ready `README.md`:

- Elevator pitch + **the chicken-and-egg problem it solves** (the hook for strangers).
- 60-second quickstart: install → `init` → `install-hooks`.
- Concepts: quests, ids, statuses, tags, context, trailers, current quest.
- CLI reference; MCP setup; Claude-plugin install.
- Config + id-strategy guidance (solo→sequential, team→random).
- Voice/tone section, including `dcc-superfan` setup and a **"Credits & permissions"** block
  (attribution to *Dungeon Crawler Carl* / Matt Dinniman; states the shipped pool is
  original homage and that verbatim lines require permission).
- Importer usage; publishing/for-teams notes.

### 19.1 Installation / Deployment (required section)

The README MUST include an explicit install/deploy section covering every supported path:

- **Prebuilt binary:** download the platform binary from the GitHub Releases page, put it on
  `PATH` (per-OS notes: macOS/Linux `chmod +x` + move to a `PATH` dir; Windows `.exe`).
- **`go install`:** `go install github.com/sharkusk/side-quest@latest` (needs Go ≥1.22).
- **Build from source:** `git clone … && cd side-quest && go build -o side-quest .`
- **Per-project setup:** `side-quest init` then `side-quest install-hooks` inside the repo.
- **Claude plugin:** `/plugin marketplace add sharkusk/side-quest` → `/plugin install side-quest`
  (documents that the plugin requires the `side-quest` binary on `PATH` first).
- **Remote / multi-environment use (§15):** how to make quests travel to another clone
  (laptop ↔ cloud/remote Claude Code session): the ref `refs/side-quest/quests` is not
  fetched by default, so document configuring the fetch refspec (auto-configured by `init`)
  and/or running `side-quest sync --pull`. Note the eventual-consistency model and the
  recommendation to use `random` ids when clones are used concurrently offline.

### 19.2 Development (required section)

The README MUST include a development section for contributors:

- **Runtime/build dependencies:** Go ≥1.22; the system `git` binary (used as a subprocess);
  `gopkg.in/yaml.v3`; the MCP Go SDK (added in Phase 4). No CGo; pure-Go static binary.
- **Layout:** the `internal/` package map (`quest`, `config`, `gitcmd`, `store`, `trailer`,
  `importer`, `voice`, `hooks`) + the `cli`/`mcp` frontends.
- **Build & test:** `go build ./...`, `go test ./... -race`, `go vet ./...`,
  `go mod tidy -diff` (CI-clean check).
- **Conventions:** teaching-quality doc comments for a C/Python audience (§21); TDD; the
  filename-is-id invariant (§5.5); machine output stays neutral (§12).
- **Developing side-quest while using it in another project (required):** how to iterate on
  the tool without disturbing a live install driving a real repo (the author develops `sq`
  while using it in `babelmap`). Cover: keep the *released* `side-quest` on `PATH` for the
  production project; use an explicit **local dev build** (`go build -o ./side-quest .` and
  invoke `./side-quest …`) for development; never point a WIP binary at your live repo — run
  it against the `sq` repo itself or throwaway `git init` scratch repos (the test suite
  already isolates via temp repos). Note that quest data is per-repo on `refs/side-quest/*`,
  so working in the `sq` repo cannot touch another project's quests; the only risk is running
  a buggy dev binary directly against a live project, which this workflow avoids. If a project
  needs to pin a specific binary, document overriding the plugin `.mcp.json` `command` (or a
  `SIDE_QUEST_BIN`-style path) to point at the dev build deliberately.

### 19.3 Dogfooding

Once the CLI lands (Phase 3), side-quest **uses itself**: run `side-quest init` in the `sq`
repo and track its own remaining work (Phases 2–7, deferred ideas, follow-ups) as quests
instead of a markdown TODO. This is both a philosophical fit (a side-quest tracker built by
managing its own side quests) and a continuous real-world end-to-end test of capture,
linking, ids, sync, and the voice. The README notes the project dogfoods itself.

---

## 20. Testing Strategy

- **Pure modules** (`quest`, `config`, `trailer`, `importer`, `voice`) — table-driven unit
  tests. `voice` tests assert: `plain` is neutral, `--json` paths never call the voice
  layer, and `dcc-superfan` falls back to `dcc` when no pool file exists.
- **I/O modules** (`store`, `hooks`) — integration tests that create a throwaway git repo in
  a temp dir, exercise real plumbing, and assert end-to-end ref state (create → list → link
  → complete; concurrent-CAS retry; sequential/random allocation; strategy switch).
- **Hook behavior** — a test commits in the temp repo and asserts `post-commit` links the
  hash and `commit-msg` warns-but-does-not-block.

---

## 21. Code Documentation Conventions

The author's comfort languages are **C and Python**; Go is new to them. Therefore:

- Package- and function-level doc comments explain **intent**, not just the signature.
- Explicitly call out Go idioms that differ from C/Python at first use: multiple returns +
  `error` values (not exceptions), `defer`, slices vs arrays, zero values, pointer vs value
  receivers, and any goroutine/channel use.
- Annotate git-plumbing call sites with the invariant they rely on (what CAS guarantees, why
  we retry).
- Prefer plain, readable code over terse idiomatic Go; keep files small and single-purpose.

---

## 22. Deliverables Checklist

- [ ] Go module: core (`quest`, `config`, `trailer`, `importer`, `voice`, `gitcmd`,
      `store`, `hooks`) + frontends (`cli`, `mcp`).
- [ ] Git hooks + `install-hooks` + refspec + `sync`.
- [ ] Importer for legacy `TODO.md` / `COMPLETED.md`.
- [ ] Voice pools: `plain`, `dcc` (original homage), `dcc-superfan` (external file loader +
      fallback) + `superfan-lines.example.txt`.
- [ ] `README.md`, `AGENTS.md`.
- [ ] Claude plugin: `.claude-plugin/plugin.json`, `.mcp.json`, `marketplace.json`,
      `commands/` (incl. `/sq`), `skills/side-quest/SKILL.md`.
- [ ] Tests: unit (pure) + integration (temp-repo).

---

## 23. Open Questions / Future

- Fetch-refspec ergonomics for collaborators (auto-configure on `init`, or document `sync
  --pull`).
- Optional per-clone id prefix (`SQ-A0007`) to make sequential safe across clones, if ever
  needed.
- Bundling per-platform binaries in the plugin `bin/` (deferred; documented install for v1).
- Whether `import` should mint fresh ids or preserve legacy ids as primary (v1: fresh ids,
  legacy id kept as a tag).
