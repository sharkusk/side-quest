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

- **sequential** (default): `SQ-` + zero-padded `seq_next`; the counter lives on the ref and
  advances in the allocating commit.
- **random**: `SQ-` + 6 hex chars, for teams / concurrent offline clones where a shared
  counter can't be serialized.
- Both run an **existence guard** (skip any id whose file already exists), so the two id spaces
  can never collide at the file level, and switching strategies preserves `seq_next` so you can
  switch back later and resume the sequence.

## Commit linking & hooks (Phase 2)

Quests link to the commits that address them through **git trailers**, applied
by thin hook shims that call the `side-quest` binary. All logic lives in Go.

- `Quest: SQ-0001` — this commit worked on SQ-0001 (append its hash).
- `Completes: SQ-0001` — append the hash **and** close the quest.
- `Quest: none` — explicit escape hatch: a genuine chore, not linked.

Three hooks, installed by `side-quest install-hooks` (which writes/composes
marker-guarded shims and adds a `refs/side-quest/*` push/fetch refspec to
`origin`):

| Hook | Does | Blocks the commit? |
|---|---|---|
| `prepare-commit-msg` | If a current quest is set and `auto_trailer` is on, inject `Quest: <current>`. | Never |
| `commit-msg` | No trailer present → **warn** (assisted) or **reject** (when `require_quest` is on). `Quest: none` satisfies both. | Only on an intentional `require_quest` reject |
| `post-commit` | Run `side-quest link HEAD`: parse the just-made commit's trailers and update each referenced quest. | Never |

**Why this closes the chicken-and-egg:** `post-commit` runs *after* the hash
exists, and the quest update is a separate commit on the orphan ref whose own
hash nobody records. `Link` is tolerant — a trailer naming an unknown quest is
skipped rather than failing the user's already-made commit.

**Hook safety inside git:** git may export `GIT_INDEX_FILE` to hooks. `gitcmd`
collapses duplicate env keys keeping the last value, so the store's scratch
`GIT_INDEX_FILE` always wins and a hook can never mutate the user's real index.

The **current-quest pointer** is worktree-local state (`<git-dir>/side-quest-current`),
not ref state: each worktree has its own, and it never travels with a push.

## Package map

| Package | Responsibility | I/O? |
|---|---|---|
| `internal/gitcmd` | Thin wrapper over the `git` binary (`Run`/`RunRaw`/`RunInput`/`WithEnv`); pins `LC_ALL=C` | subprocess |
| `internal/quest` | The `Quest` model + Markdown/YAML-frontmatter (de)serialization; id = filename | pure |
| `internal/config` | On-ref `_config.yaml` model; `Unmarshal` fills missing keys from `Default()` | pure |
| `internal/store` | Orphan-ref CRUD + the `mutate`/`buildCommit`/`cas` machinery + id allocation | git plumbing |
| `internal/trailer` | Parse Quest:/Completes: trailers + the commit-msg decision | pure |

**CRUD** — Create, Read, Update, Delete — the basic persistence operations. Today the store
implements Create (`Create`), Read (`Get`/`List`), and Update (`SetStatus`/`AddCommit`/
`Update`/`SetStrategy`). Delete is not built yet (the `txn.del` plumbing exists for it).

## Dependencies

- **Go ≥ 1.22** — build + runtime (pure Go, no CGo; single static binary).
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
5. **git runs in `LC_ALL=C`** so stderr parsing is locale-independent.

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
