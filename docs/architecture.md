# side-quest Architecture

> **This is a LIVING document.** It describes how side-quest actually works *now* and must
> be updated in the same change that alters the behavior it describes (see
> [CONTRIBUTING.md](../CONTRIBUTING.md) → "Documentation is part of the change"). It is
> distinct from the dated design records under `docs/superpowers/specs/`, which are
> point-in-time snapshots of decisions and are **not** edited to track the code.

## What side-quest is

A lightweight, project-specific quest/task tracker. You capture "side quests" — ideas and
follow-ups that occur to you mid-work — and each quest links to the git commits that address
it, with no chicken-and-egg between a quest and the commit hash it references.

## The core idea: quests live on a git ref, not in your files

Quests need to live *in* your repository (so they travel with a clone/push and version with
your code) but **without** being normal tracked files that clutter your working tree and
create the chicken-and-egg problem (recording a commit's hash back into a tracked quest file
requires *another* commit, which has its own hash…).

side-quest's answer: store quest data in git's object database on a **dedicated orphan ref**,
and manipulate it with git's low-level **plumbing** commands so your working files are never
touched.

- A **ref** is a named pointer to a commit. Your branches are refs (`main` is
  `refs/heads/main`).
- side-quest's data lives on **`refs/side-quest/quests`** — an **orphan ref**: it has its own
  root history, unrelated to `main`, in a custom namespace (`refs/side-quest/*`) so it never
  appears in `git branch` and can't be accidentally checked out. It is still a real ref, so it
  can be pushed/fetched to share quests.

The tree stored on that ref:

```
_config.yaml            # on-ref configuration (id strategy, counter, tone, …)
quests/
  SQ-0001.md            # one Markdown file per quest (YAML frontmatter + prose body)
  SQ-0002.md
  SQ-a3f9c2.md
```

The quest **id is the filename** (`quests/SQ-0001.md` → `SQ-0001`); it is never stored inside
the file, so there is no second source of truth to drift.

Every command that takes an `<id>` also accepts a **bare or zero-padded number** as
shorthand: `11` and `0011` both resolve to `SQ-0001` (using the configured prefix and
`seq_width`). Normalization happens in one place — `quest.NormalizeID`, applied by the
store at each id-entry method (`Get`, `Update`, `SetCurrent`) — so the CLI and the MCP
server behave identically and a shorthand id can never be silently persisted verbatim
(e.g. `SetCurrent` stores the canonical `SQ-0001`, never `11`). A non-numeric id (a
random hex id, a typo) passes through unchanged and is caught by the normal existence
check.

Each quest's frontmatter carries `title`, `status` (open/partial/done/deferred/discarded),
`type` (bug/feature), `priority` (high/low), `created`, an optional `completed`, `commits`,
an optional `context`, and optional `tags`. `type` and `priority` are constrained enums with
defaults (`feature`/`low`) applied at creation; like `status`, they are validated only at the
write boundary (`Create`/`Reclassify`/`SetStatus`/`Modify`/`Replace`), never on read.

## Durability: git won't garbage-collect the quest ref

A fair worry, given quests live on a ref you never check out: **will `git gc` (locally or on
GitHub) delete this data?** No. Git's garbage collector only prunes objects that are
**unreachable from any ref**, and a ref is itself a reachability root. It makes no difference
that `refs/side-quest/quests` lives in a custom namespace instead of `refs/heads/*` — every
commit, tree, and blob reachable from it is retained by `gc`, on GitHub's servers exactly as
locally. GitHub runs maintenance `gc`, but it never removes a ref or prunes what a ref still
points at. Custom ref namespaces are well-trodden ground — `refs/notes/*` (git-notes),
`refs/lfs/*`, Gerrit's `refs/changes/*` — and hosts preserve them the same way. (This is also
why it is a *ref*, not an "orphan branch": an orphan branch still lives under `refs/heads/*`
and shows in `git branch`; the quest ref is invisible to branch listings and PRs but just as
protected from `gc`.)

The only ways to actually lose quest data are therefore about the **ref**, never about `gc`
reclaiming a live ref's objects:

- **It was never pushed.** If the quest ref exists only locally (a misconfigured push refspec,
  a dropped push), the remote has nothing to protect. This is why `install-hooks` configures the
  fetch refspec and the `pre-push` hook publishes the ref on every push (see [Sync](#sync)).
- **Someone deletes the ref** — an explicit `git push origin :refs/side-quest/quests`, or a
  "delete every ref" cleanup tool. That removes the protection; nothing else does.

**One adjacent case that *is* real GC — but of a linked commit, not the quest ref.** If you
rebase or force-push your **branch** history and rewrite a commit a quest recorded, that old
commit becomes unreachable from `refs/heads/*` and *will* eventually be pruned. The quest
survives untouched; only the SHA it stored goes dangling. That is exactly what `relink` (and
`quest_relink_commit`) exist to fix — repoint the quest at the rewritten commit — and why the
old-sha match is by prefix and never git-resolved (a dangling hash can't be resolved).

## Reads

Reads dump objects straight out of git — no checkout:

- `git for-each-ref` → the ref's current commit (its **tip**); prints nothing if the ref
  doesn't exist yet (an empty store).
- `git cat-file -p <tip>:quests/SQ-0001.md` → a quest file's bytes.
- `git ls-tree --name-only <tip>:quests` → the list of quest ids.

A **`Snapshot`** is a read-only view at a given tip: the parsed config plus the list of ids.

## Writes: the mutation transaction

A **mutation** is any state change — create a quest, set a status, link a commit. Every
mutation goes through one routine, `mutate`, which assembles a brand-new commit on the ref
**without a working directory**, using a **scratch index**.

Normally `git add`/`git commit` stage into `.git/index` and need a checkout. Instead, side-quest
points git at a throwaway temp index file via the `GIT_INDEX_FILE` environment variable and
builds a commit by hand with plumbing:

1. `read-tree <tip>` — load the current tree into the scratch index
2. `hash-object -w` — write each new/changed quest file into the object store → a blob id
3. `update-index` — stage those blobs in the scratch index
4. `write-tree` — snapshot the scratch index into a tree object
5. `commit-tree <tree> -p <tip>` — wrap the tree in a commit whose parent is the old tip
6. move the ref to the new commit via **CAS** (below)

Because it's a scratch index, your real index and working files are never involved. That is
the invariant the whole design rests on, and there is a test asserting `git status` stays
empty after a write (`TestCreateDoesNotTouchWorkingTree`).

## CAS: safe concurrency without a lock

**CAS = Compare-And-Swap**: an atomic "set X to *new* only if X still equals *old*." Git
provides it directly:

```
git update-ref refs/side-quest/quests <new-commit> <old-tip>
```

This moves the ref **only if it still points at `<old-tip>`**. If another writer (e.g. a second
git worktree lane) committed a quest in the meantime, the ref no longer equals `<old-tip>` and
the swap is **rejected**. On rejection, `mutate` **re-reads a fresh snapshot and rebuilds** —
a bounded retry loop (up to 10 tries).

This is why multiple git worktrees can all write quests with **no shared lock and no lost
updates**: at most one CAS wins per round; losers rebuild against the winner's new state and
try again. It's also why `mutate`'s build step must be a pure function of the snapshot it's
handed — it may run several times.

### The discriminator

A rejected CAS can mean two very different things:

- a harmless **lost race** — someone beat us; just retry; or
- a **genuine error** — a bad object, a permissions/disk fault — which must **not** be
  silently retried into the cap and reported as fake "contention."

The **discriminator** is the logic that tells them apart. git's stderr distinguishes them:

| Situation | git stderr contains |
|---|---|
| lost race (oldvalue mismatch / ref already exists / ref vanished) | `cannot lock ref …` |
| genuine fault (e.g. writing a nonexistent object) | `cannot update ref … nonexistent object …` |

So the store **retries only when the error contains `cannot lock ref`**, and surfaces
everything else.

### Why `LC_ALL=C`

git localizes its messages: on a non-English system with translated message catalogs, that
stderr could come out in German, Chinese, etc., and the `cannot lock ref` match would silently
fail — misclassifying a routine lost race as a hard error. To prevent that, side-quest pins
the **`LC_ALL=C`** environment variable on **every** git subprocess. `LC_ALL` is the POSIX
master locale override; `C` is the default POSIX locale, which forces git's messages to their
original, stable English. It is a one-line insurance policy that makes all stderr parsing
deterministic regardless of the user's system language.

## Atomic id allocation

When you create a quest, the new quest file **and** the advanced id counter (`seq_next` in
`_config.yaml`) are written in the **same** commit. So two lanes racing to create can never
mint the same `SQ-0001`: only one CAS wins; the loser rebuilds, sees the counter already
advanced, and takes `SQ-0002`.

- **sequential**: `SQ-` + zero-padded `seq_next`; the counter lives on the ref and
  advances in the allocating commit.
- **random**: `SQ-` + 6 hex chars, for teams / concurrent offline clones where a shared
  counter can't be serialized.
- **Which is the default is chosen at `init` by remote presence** (SQ-0030): a repo with a
  configured remote — a shared/team workflow, where two offline clones would both mint
  `SQ-0007` (same filename, different content) — defaults to **random**; a solo repo with no
  remote defaults to the tidier **sequential**. `init` prints the choice, and it is always
  overridable with `config set id_strategy`.
- Both run an **existence guard** (skip any id whose file already exists), so the two id spaces
  can never collide at the file level, and switching strategies preserves `seq_next` so you can
  switch back later and resume the sequence.

## Commit linking & hooks (Phase 2)

Quests link to the commits that address them through **git trailers**, applied
by thin hook shims that call the `side-quest` binary. All logic lives in Go.

- `Quest: SQ-0001` — this commit worked on SQ-0001 (append its hash).
- `Completes: SQ-0001` — append the hash **and** close the quest.
- `Quest: none` — explicit escape hatch: a genuine chore, not linked.

Four hooks, installed by `side-quest install-hooks` (which writes/composes
marker-guarded shims and configures the `refs/side-quest/quests` fetch refspec
on `origin`, so a bare `git push` still sends the current branch via the
existing `HEAD` push refspec — see SQ-0016 and, for the quest ref's own publish
path, [Sync](#sync)):

| Hook | Does | Blocks the commit/push? |
|---|---|---|
| `prepare-commit-msg` | If a current quest is set and `auto_trailer` is on, inject `Quest: <current>`. | Never |
| `commit-msg` | No trailer present → **warn** (assisted) or **reject** (when `require_quest` is on). `Quest: none` satisfies both. | Only on an intentional `require_quest` reject |
| `post-commit` | Run `side-quest link HEAD`: parse the just-made commit's trailers and update each referenced quest. | Never |
| `pre-push` | Sync the quest ref with the remote being pushed to (see [Sync](#sync)). | Never — warns and exits 0 on any failure |

**Why this closes the chicken-and-egg:** `post-commit` runs *after* the hash
exists, and the quest update is a separate commit on the orphan ref whose own
hash nobody records. `Link` is tolerant — a trailer naming an unknown quest is
skipped rather than failing the user's already-made commit.

**Hook safety inside git:** git may export `GIT_INDEX_FILE` to hooks. `gitcmd`
collapses duplicate env keys keeping the last value, so the store's scratch
`GIT_INDEX_FILE` always wins and a hook can never mutate the user's real index.

**Where the shims land, and composing with existing hooks:** install-hooks
honors `core.hooksPath` when set, otherwise writes to `<common-git-dir>/hooks`.
Each shim calls the installing binary by absolute path, normalized to forward
slashes so it runs under Git-for-Windows' MSYS sh (a `C:\…` path would break;
`C:/…` works — SQ-0021). That path fix was unit-tested on Unix only until the
`ci` workflow added a `windows-latest` job that runs the end-to-end hook test
under real Git-for-Windows MSYS sh, verifying the extensionless shims actually
execute and invoke the `.exe` (SQ-0034). Each shim is a marker-guarded block (`# >>> side-quest
>>>` … `# <<< side-quest <<<`): installing into an existing hook **appends** our
block and leaves the rest intact, and re-installing replaces only our block
(idempotent, never duplicated). The block carries a version stamp
(`# side-quest-version: <v>`, the installing binary's `main.version`) between the
markers — the markers themselves stay version-free so a block written by any
version is still matched and replaced (SQ-0045). Because the shims are thin
delegators, upgrading the binary in place upgrades behavior with no hook change;
but when the binary **moves** (a new path) or the shim format itself changes
between versions, the shims go stale until you re-run `install-hooks`. Re-running
is safe and now reports what it did per hook: a byte-identical block is a true
no-op, while a refreshed block prints the version transition (`v0.1.0 → v0.2.0`,
or "predated version stamping"), so an upgrader can see the stale shims were
brought current.
Appending assumes the existing hook runs under a POSIX shell: a hook whose
shebang names a non-sh interpreter (python, node, …) is **skipped with a warning**
rather than corrupted (SQ-0020) — migrate it to call `side-quest <hook>` itself.
Our block still runs **last**, so an existing hook that exits early can shadow it;
that ordering caveat is documented but not yet enforced. install-hooks also
**warns** (SQ-0022) when `core.hooksPath` is set — another framework (husky,
pre-commit) likely owns that dir — and whenever it composes into a hook that
already had content, so a repo driving a different bookkeeping system surfaces
the conflict instead of getting side-quest silently appended; the
[manual-setup guide](manual-setup.md#existing-git-hooks) walks through migrating.

### How agent guidance is delivered (SQ-0051)

The knowledge an agent needs — the capture reflex, the auto-classify-when-obvious
rule, "set current and the hooks link your commits," the trailer forms — is carried
by the **MCP server itself**, so it reaches any MCP client with no file in the
user's repo. `internal/guidance` embeds one canonical brief, `guidance.Core`; the
server sends it verbatim as the initialize-time **`instructions`** field
(`ServerOptions.Instructions`), which a client MAY inject into the model's context.
The always-loaded **tool descriptions** are the reliable floor — `quest_new` and
`quest_set_current` carry the reflex / auto-classify / auto-link cues inline, so the
essentials survive a client that ignores `instructions`. Long-tail tools
(`quest_note`, `quest_reclassify`, `quest_relink_commit`, …) are self-describing
through their own descriptions; situational multi-step workflows live in the
reinforcement surfaces.

The skill and `AGENTS.md` are **opt-in reinforcement**, not the baseline: the
Claude plugin bundles the skill; a non-Claude user opts into `AGENTS.md` with
`onboard --agents-md`. All three surfaces derive from the same `guidance.Core`.
The agent-agnostic block is embedded from `internal/guidance/agents.md` (exposed
as `guidance.Agents`); a guard test there asserts it stays unwrapped and contains
`guidance.Core`, and a test in `internal/packaging` asserts
`skills/side-quest/SKILL.md` contains the core verbatim — so none can drift.

The **AGENTS.md guidance** uses the same drift-defeating pattern (SQ-0047). The
emitted block is wrapped in HTML-comment markers (`<!-- >>> side-quest >>> -->` …
`<!-- <<< side-quest <<< -->`, invisible in rendered Markdown) and carries a
`<!-- side-quest-version: <v> -->` stamp. `onboard --agents-md` (the merge is
opt-in — SQ-0051) manages that block **in place** in the project's
`AGENTS.md`: creating the file, appending after the user's own content, or
replacing only our block on a version change (never duplicating it, never
touching their text). So when the directives change release-to-release, re-running
`onboard` pulls the update in and reports the transition, rather than leaving every
merged copy silently stale. `agents-md` prints the same marked, stamped block for
those who prefer to paste by hand.

The **current-quest pointer** is worktree-local state (`<git-dir>/side-quest-current`),
not ref state: each worktree has its own, and it never travels with a push.
Setting it requires the target quest to exist — a missing id is rejected, so the
pointer can never dangle and `prepare-commit-msg` cannot inject a bogus trailer.

## Sync

Two clones of the quest ref can each gain commits independently, which git's ordinary
fetch/push can't reconcile (no working checkout exists to run `git merge` against). side-quest
solves this with a real three-way merge, run automatically by a `pre-push` hook and available
as `side-quest sync` for manual recovery. The full model — why the ref diverges, the tracking
ref, the merge rules, the id-collision handling, convergence, and the safety guarantees — is
its own document: **[`docs/sync.md`](sync.md)**. Summary of the moving parts, for orientation:

- **`internal/merge` is a pure function**, `Merge(base, local, remote Side) (Result, []Event)`
  — no git, no clock, no I/O. It implements every merge rule (per-id add/unchanged/both-changed,
  scalar last-writer-wins by touch time, commits/tags/notes union, id-collision resolution,
  config `seq_next = max`) as a deterministic function of its inputs, which is what makes the
  rules exhaustively table-testable and convergence provable.
- **`internal/store/sync.go` does the git plumbing**: it fetches the remote quest ref into a
  tracking ref, reads the three snapshots (`sideAt`) and the touch times the merge needs
  (`fillTouch`), calls `merge.Merge`, and writes the result as a real **two-parent merge
  commit** via a new `buildMergeCommit`/`commitTx` helper (`store.go`) — the same scratch-index
  machinery `buildCommit` uses for ordinary mutations, generalized to take an arbitrary parent
  list and build its tree from an empty base rather than reading a single parent's tree. `Sync`
  then runs a fetch-merge-push loop, retrying on a lost push race the same way `mutate`'s CAS
  loop retries a lost ref-update race.
- **The refspec changed.** The fetch refspec now maps the remote quest ref into a *separate*
  tracking ref, `refs/side-quest-remote/quests` (`store.FetchRefspec`), instead of the old
  `refs/side-quest/*:refs/side-quest/*`, which pointed fetch directly at the live ref and left
  it either stale (on divergence) or silently clobbered (on a fast-forward). The push refspec
  no longer includes the quest ref at all — only `HEAD` (which `install-hooks` itself
  configures as `remote.origin.push`), so a bare `git push` still sends your
  branch — because the quest ref is now published by the `pre-push` hook, which can guarantee
  it's fast-forwardable at push time in a way a static refspec cannot. `install-hooks`/`onboard`
  migrate old installs to the new refspecs idempotently (`addRefspec` in `cmd/side-quest/hooks.go`).

## Command-line interface (`cmd/side-quest`)

Beside the git-hook subcommands (`link`, `current`, `commit-msg`,
`prepare-commit-msg`, `install-hooks`), the binary exposes the human commands:

- `init` — create the quest ref.
- `new <title>` — create a quest; flags `--type`, `--priority`, `--context`,
  `--tag k=v` (repeatable), `--current` (also set the worktree's current quest),
  `--json`. Records the same mechanical context (branch/HEAD/cwd/current-quest,
  via `internal/capture.Body`) ahead of `--context` as the MCP `quest_new`.
- `list` — list quests; filters `--status`/`--type`/`--priority` (validated),
  `--tag k=v` (repeatable; a quest matches only if it has every given tag),
  combined with AND, and `--json`. With no `--status`/`--all`/`--filter` it
  defaults to the outstanding view (open + partial only); `--all` restores every
  status. `--filter "expr"` takes a boolean expression (compiled by
  `internal/filter`) over bare enum values and `key=value` tags with
  `and`/`or`/`not`/parens; it is the whole selection and cannot be combined with
  the simple flags above.
- `show <id>` — show one quest; `--json`. (`<id>` accepts shorthand: `11` or
  `0011` for `SQ-0011`, everywhere an id is taken.) Long field values and body
  lines are word-wrapped to the terminal width with a hanging indent; `--no-wrap`
  prints raw values, and piped/redirected output (a non-terminal) is never
  wrapped, so scripts see stable single-line fields.
- `status <id> <status>` — set the lifecycle status.
- `note <id> <text>` — append a note to a quest (the note text is every
  argument after the id, joined with spaces).
- `relink <id> <old-sha> <new-sha>` — repoint a recorded commit after a rebase
  rewrites its hash. The old sha is matched against the stored hashes **by
  prefix** and never git-resolved (it is typically dangling post-rebase); the new
  sha is resolved to its canonical hash. Order is preserved and the result
  deduped (`store.ReplaceCommit`). A rebase auto-links the new commit via the
  `post-commit`/`pre-push` hooks but cannot remove the old, dangling entry — this
  is the corrective for that (SQ-0048).
- `unlink <id> <sha>` — remove a recorded commit from a quest (prefix-matched,
  `store.RemoveCommit`). Both are mirrored on the MCP surface as
  `quest_relink_commit`/`quest_unlink_commit` (SQ-0049), so an MCP-only agent can
  repair a link it orphaned — the inverse of `quest_link_commit`.
- `edit <id>` — open the quest's Markdown (frontmatter + body) in `$EDITOR`
  (`VISUAL`→`EDITOR`→`vi`) and write the saved buffer back via `store.Replace`.
  The id is the filename, never part of the buffer, so it cannot be edited. A
  buffer that no longer parses or is rejected keeps its temp file and reports the
  path, so a long hand-edit is never lost. Edits are last-write-wins.
- `reclassify <id> [--type --priority]` — change type and/or priority.
- `config get` / `config set <key> <value>` — read config; set `require_quest`,
  `auto_trailer`, or `id_strategy`.
- `onboard` — one-shot per-repo setup: `init` + `install-hooks`, write a project
  `.mcp.json` if absent, then refresh the marker-guarded guidance block in the
  project's `AGENTS.md` in place (create/append/refresh). Safe to re-run (an
  existing ref, hooks, and `.mcp.json` are each left as they are; the AGENTS.md
  block is refreshed to the current version, the user's own content untouched).
- `agents-md` — print the canonical agent-guidance block (the embedded
  `AGENTS.md`, wrapped in refresh markers and version-stamped) for pasting into a
  project's own `AGENTS.md`.

Handlers live in `cli.go`; rendering in `render.go`. Each command is a thin
adapter over one `store` method — validation stays at the store write boundary
(the sole exception: `list` validates its filter values). `--json` marshals the
raw `quest.Quest`/`config.Config` structs, so the JSON keys are the Go field
names; this is the stable machine surface the MCP layer reuses. Flags may be
given before or after positional arguments: `parseInterspersed` (in `cli.go`)
re-parses around each positional because stdlib `flag` stops at the first one.
Usage errors exit 2; all other errors exit 1. `side-quest help` (or `-h`/`--help`)
prints the top-level command list; `<cmd> -h`/`--help` prints that command's own
synopsis and per-flag help (via `setUsage`, which renders the FlagSet's
`PrintDefaults`). A help request is a success — it prints to stdout and exits 0,
not the exit-2 usage-error path.

The CLI relies on two store/config additions: `store.SetAutoTrailer` and
`config.Strategy.Valid()`.

### Voice layer (`internal/voice`)

`internal/voice` renders the small set of human-facing confirmation/warning strings
(`QuestCreated`, `StatusSet`, `NoteAdded`, `QuestSelected`, `MissingTrailer`, `EmptyList`,
`Initialized`, `HooksInstalled`) in a selected **tone**. The package is pure — no I/O — and built from
three pieces: a `pools` table (`tone -> message key -> candidate lines`), an injectable
`source` interface for randomness (production uses `math/rand`; tests inject a
deterministic stub), and typed methods on `*Voice` so call sites never touch raw keys or
format strings.

Two tones actually render: `plain` (one neutral line per key) and `dcc` (several
candidate lines per key, picked at random, in the voice of *Dungeon Crawler Carl* — see
[Credits & permissions](#credits--permissions) below). `Voice.New` collapses anything
that isn't `plain` to `dcc`.

**Tone precedence**, resolved once per invocation by `voice.ResolveTone` +
`cmd/side-quest/voice.go`'s `newVoice`: the `SIDE_QUEST_TONE` environment variable wins
if it's a valid tone; otherwise the on-ref config's `tone` field is used; the config
default is `dcc`.

**Neutral-path rule:** `--json` output, data displays (quest bodies, config values), and
error messages never route through `voice` — they stay tone-free regardless of the
configured tone, so scripts and agents parsing output never see flavor text.
`TestNewJSONNeutralAcrossTones` asserts this holds across all tone settings.

**`dcc-superfan` status (this phase):** the tone is recognized and stored
(`config.Tone.Valid()`), but no verbatim-line file ships with side-quest. `voice.EffectiveTone` always
renders `dcc-superfan` as `dcc`, and when the user's own line file
(`~/.config/side-quest/superfan-lines.txt`) is absent, `newVoice` prints a one-time hint
to stderr pointing at `superfan-lines.example.txt`. Wiring an actual verbatim-line source
into the render path is unbuilt — deferred to a later phase.

**Configuring the tone (user-facing).** Three tones exist: `plain` (neutral),
`dcc` (the default flavored voice), and `dcc-superfan` (opt-in; currently falls
back to `dcc`). Set it persistently with `side-quest config set tone <value>`, or
override it for one invocation with the `SIDE_QUEST_TONE` environment variable
(`SIDE_QUEST_TONE=plain` forces neutral output). This is deliberately not
advertised in the README — the flavored default is meant to be discovered on first
run — but it is fully documented and configurable here.

## MCP frontend (`internal/mcp` + `side-quest serve`)

`side-quest serve` runs a stdio MCP server (JSON-RPC over stdin/stdout) built on
`github.com/modelcontextprotocol/go-sdk`. `cmd/side-quest/serve.go` is a thin
frontend: it opens the store for the cwd and hands it to `internal/mcp.NewServer`,
which registers twelve tools:

- `quest_new`, `quest_list`, `quest_show`, `quest_get_current` (capture/read)
- `quest_set_status`, `quest_reclassify`, `quest_update`, `quest_note`,
  `quest_set_current`, `quest_link_commit`, `quest_relink_commit`,
  `quest_unlink_commit` (mutation)

Each handler decodes typed params (the SDK infers each tool's JSON-Schema from a
Go struct), calls one store method, and returns neutral JSON of the
`quest.Quest`/ack shape. Validation stays in the store; invalid input is returned
as an MCP **tool error** (not a protocol error). Two frontend-side guards sit
ahead of the store: `quest_list` validates its filter values, and the closed
string domains (status/type/priority) are declared as JSON-Schema **enums** on
the relevant tools (via `enumSchema`), so a client's bad value is rejected at the
boundary before it reaches a handler. `quest_new` auto-records mechanical
context (branch/HEAD/cwd/current-quest, via `internal/capture.Mechanical`) ahead
of the agent's narrative note, and only moves the current-quest pointer when
`set_current:true`. stdout carries only JSON-RPC; diagnostics go to stderr.

The response's **first** content block is always neutral JSON, so parsers can
rely on it. As of SQ-0028 a **mutation** (`quest_new`, `quest_set_status`,
`quest_note`) may append a **second** text block carrying the same tone-flavored
line the CLI would print (via `internal/voice`), gated on the on-ref `tone`:
silent for `plain`, `dcc`/`dcc-superfan` add flavor (superfan collapses to dcc —
a server prints no fallback hint). Reads never voice. So an agent that wants pure
data selects `plain` (or just reads `content[0]`), while a human-facing client
surfaces the flavor.

On startup `serve` compares its own build version against the `side-quest` found
on `PATH` (which the git hooks and the human CLI invoke) and, if they differ,
prints a one-line warning to stderr (SQ-0039). An auto-updated plugin can run a
bundled binary for `serve` while a separately-installed `side-quest` stays on
`PATH`; the check surfaces that drift. It is best-effort and never blocks: no
`side-quest` on `PATH`, the same executable, or a failed lookup all stay silent.

The three store mutators the update tools use — `AppendNote` (append a dated note
to the body), `SetTitle`, and `MergeTags` (empty value deletes a key) — live in
`store` beside the other setters.

## Package map

| Package | Responsibility | I/O? |
|---|---|---|
| `internal/gitcmd` | Thin wrapper over the `git` binary (`Run`/`RunRaw`/`RunInput`/`WithEnv`); pins `LC_ALL=C` | subprocess |
| `internal/quest` | The `Quest` model + Markdown/YAML-frontmatter (de)serialization; id = filename | pure |
| `internal/config` | On-ref `_config.yaml` model; `Unmarshal` fills missing keys from `Default()` | pure |
| `internal/store` | Orphan-ref CRUD + the `mutate`/`buildCommit`/`cas` machinery + id allocation | git plumbing |
| `internal/trailer` | Parse Quest:/Completes: trailers + the commit-msg decision | pure |

**CRUD** — Create, Read, Update, Delete — the basic persistence operations. Today the store
implements Create (`Create`), Read (`Get`/`List`), and Update (`SetStatus`/`SetType`/
`SetPriority`/`AddCommit`/`Update`/`SetStrategy`). Delete is not built yet (the `txn.del`
plumbing exists for it).

## Dependencies

- **Go ≥ 1.25** — build + runtime (pure Go, no CGo; single static binary).
- **`git` ≥ 2.13** (May 2017) — invoked as a subprocess for all storage.
- **`gopkg.in/yaml.v3`** — YAML frontmatter + config parsing.

### git version floor — provenance

side-quest shells out to `git`, so the minimum git version is whatever the
highest-versioned command/flag it invokes requires. The current floor is **2.13**, set by a
single flag (`rev-parse --absolute-git-dir`); everything else works on far older git.

| git feature used | first available | why we use it |
|---|---|---|
| `rev-parse --absolute-git-dir` | **2.13** (2017) | resolve the `.git` dir for scratch index files (`store.Open`) — **sets the floor** |
| `update-index --cacheinfo <mode,sha,path>` (comma form) | 2.0 (2014) | stage a blob into the scratch index (`buildCommit`) |
| `commit-tree -m` | 1.7.7 (2011) | author the ref commit without an editor |
| `read-tree --empty`, `for-each-ref --format`, `cat-file`/`ls-tree <tree>:path`, `hash-object -w --stdin`, `write-tree`, `update-ref <new> <old>` (CAS), `update-index --force-remove`, `rev-parse --show-toplevel` | ≤ 1.8 (ancient) | reads + the mutation transaction |
| `rev-parse --git-common-dir` | 2.5 (2015) | resolve the shared hooks dir for `install-hooks` |
| `remote get-url origin` | 2.7 (2016) | detect origin before adding the refspec |

> **Maintenance rule (keep this current):** whenever you add or change a `git` command or
> flag, check the git version that introduced it. If it exceeds the floor above, **raise the
> documented floor — here, in [CONTRIBUTING.md](../CONTRIBUTING.md), and in the README/spec
> dependency lists — and add a row to this table.** The warn-only pre-commit hook nudges you
> when `internal/**/*.go` changes without a docs change, which covers most git-usage edits.
>
> The floor could be *lowered* to git 2.0 if broad old-git support is ever wanted: the only
> blocker is `--absolute-git-dir`, replaceable with `rev-parse --git-dir` + resolving to
> absolute in Go.

## Invariants (do not break)

1. **The working tree and the user's real index are never touched.** All writes go through a
   scratch `GIT_INDEX_FILE`.
2. **Every mutation is CAS-guarded and retries only on a genuine lost race** (`cannot lock
   ref`), surfacing all other errors.
3. **The id is the filename**, never serialized into the quest file.
4. **id allocation is atomic** — quest file + advanced counter in one commit.
5. **Multi-field edits are atomic** — `reclassify` (type + priority) and
   `update` (title + tags) validate every field up front and apply them in one
   commit, so a bad value rejects the whole change rather than landing half of it.
6. **git runs in `LC_ALL=C`** so stderr parsing is locale-independent.

## Glossary

| Term | Meaning here |
|---|---|
| **CRUD** | Create / Read / Update / Delete — the basic persistence operations. |
| **ref** | A named pointer to a git commit (e.g. `main`). |
| **orphan ref** | A ref with its own unrelated root history — here `refs/side-quest/quests`. |
| **plumbing** | git's low-level scriptable commands (`cat-file`, `hash-object`, `write-tree`, `commit-tree`, `update-ref`) vs. everyday "porcelain" (`add`, `commit`). |
| **tip** | The commit a ref currently points to. |
| **snapshot** | A read-only view of the store (config + ids) at a specific tip. |
| **mutation** | Any state-changing operation; each builds one new commit on the ref. |
| **scratch index** | A throwaway `GIT_INDEX_FILE` used to assemble commits without touching the real index/working tree. |
| **CAS** | Compare-And-Swap — move the ref only if it still equals the expected old commit; atomic. |
| **lost race** | A CAS rejected because another writer moved the ref first — retryable. |
| **discriminator** | Logic classifying a CAS failure as a lost race (retry) vs a real error (surface). |
| **TOCTOU** | Time-Of-Check-To-Time-Of-Use — a bug class where state changes between checking and acting; avoided by validating *inside* the CAS-retried closure. |
| **`LC_ALL=C`** | POSIX master locale override forcing the default "C" locale, so git's messages stay stable English. |

## Packaging & distribution (Phase 7)

side-quest ships both as a standalone binary and as a Claude Code plugin from the
same repository.

- **Plugin manifests** live in `.claude-plugin/` (`plugin.json`, `marketplace.json`).
  `commands/sq.md` is the `/sq` capture command (it calls the `quest_new` MCP tool);
  `AGENTS.md` is the agent-agnostic contract; `skills/side-quest/SKILL.md` is the
  Claude-flavored workflow skill.
- **The `.mcp.json`** launches the server by bare name (`side-quest serve`). Inside
  an installed plugin, Claude prepends the plugin's `bin/` to `PATH`, so `side-quest`
  resolves to `bin/side-quest` (POSIX) or `bin/side-quest.cmd` (Windows).
- **The launcher** (`bin/side-quest`) resolves the native binary in order: a cached
  copy under `${CLAUDE_PLUGIN_DATA}` → a `side-quest` already on `PATH` → download
  the matching release asset and verify its SHA-256 against the release
  `checksums.txt` → otherwise print a `go install` hint and exit non-zero. No
  compiled binaries are committed to the repo.
- **Versioning:** the root `VERSION` file is the single source of truth; `plugin.json`'s
  `version` matches it (test-enforced); the release tag is `v` + `VERSION`. The binary
  reports its version via `side-quest version`, stamped at release build time by
  GoReleaser (`-ldflags "-X main.version=<tag>"`). Dev builds via the `Makefile`
  (`build`/`install`) self-stamp `main.version` from `git describe --tags
  --always --dirty` (e.g. `590a5ae`, `v0.1.0-6-g590a5ae`, or `…-dirty`), falling
  back to `dev` outside a git repo — so a dogfood binary reports the exact commit
  it was built from, and a stale MCP server (advertising an older commit than
  HEAD) is visible at a glance (SQ-0050). A bare `go build`/`go install` with no
  ldflags still reports `dev`. The same `main.version` is threaded into
  `mcp.NewServer`, so the version the MCP server advertises to clients tracks
  `side-quest version` rather than a separate hardcoded constant that could drift
  (SQ-0044).
- **Releases** are produced by GoReleaser (`.goreleaser.yaml`) via a tag-triggered
  GitHub Actions workflow: six targets (darwin/linux/windows × amd64/arm64), archived
  with README + LICENSE, plus `checksums.txt`.

## Credits & permissions

side-quest's `dcc` tone is an original homage to *Dungeon Crawler Carl* by Matt
Dinniman — no verbatim book/show text is included or shipped. Verbatim catch
phrases are never distributed with side-quest; the `dcc-superfan` tone only loads
them from a file you create yourself, at `~/.config/side-quest/superfan-lines.txt`
(see [`superfan-lines.example.txt`](../superfan-lines.example.txt) for the format).
Public or committed use of verbatim phrases requires permission from the author.
