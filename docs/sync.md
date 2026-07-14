# Sync — reconciling quests across clones

> This doc assumes you've read [`docs/architecture.md`](architecture.md): quests live on
> `refs/side-quest/quests`, a dedicated **orphan ref** with its own root history, written
> with git's low-level **plumbing** commands so your working tree is never touched. This doc
> covers what happens once that ref exists on *two* clones — two machines, or two people —
> and both have added commits to it.

## Why the quest ref diverges

`refs/side-quest/quests` is a real ref, so like any ref it can be pushed and fetched. But
nobody has it checked out — there's no branch you `git merge` when two versions disagree, the
way you would with `main`. If you and a teammate (or you on two machines) each capture a
quest while offline from each other, you both commit to your own copy of the ref. Relative to
the last point you both agreed on, your ref now has commits neither side has seen: it has
**diverged**, the same relationship two divergent branches have.

Git's ordinary transport only moves a ref in a **fast-forward** — forward along a straight
line of history it already contains. It has no built-in way to reconcile two refs that both
moved:

- A plain `git fetch` of a diverged ref can't fast-forward your local copy (that would throw
  away commits only you have), so git just leaves your ref alone — silently. Nothing looks
  wrong; your quest data is simply stale, with no signal that the remote moved.
- A plain `git push` of a diverged ref is flatly **rejected** by the remote, for the same
  reason in reverse. Critically, this happens *independently* of your branch push, which
  succeeds normally — so it's easy to push your code, see it go through, and never notice the
  quest ref never left your machine (this was tracked as SQ-0032 before sync subsumed it).

Reconciling two diverged histories needs an actual **merge**: find where they last agreed,
look at what changed on each side since, and combine the changes. `side-quest sync` is that
merge, specialized for what a quest ref actually contains.

## The tracking ref

Before sync existed, `install-hooks` pointed the fetch refspec straight at the live ref:
`refs/side-quest/*:refs/side-quest/*`. That's the source of the silent-staleness problem
above — a `git fetch` was trying to update the exact ref every `side-quest` command reads,
using git's ordinary (non-merging) ref-update rules. On a fast-forward it would just clobber
the live ref outright with whatever the remote had.

Sync fixes this by giving the remote's copy of the quest ref a home of its own — a
**tracking ref**, `refs/side-quest-remote/quests`. This is the same idea as git's own
remote-tracking branches (`refs/remotes/origin/main`, which you see as `origin/main`): git
fetches the remote's branch into a local ref *namespaced by remote*, distinct from your own
`main`, so `git fetch` can never silently rewrite what you're working on. side-quest does the
analogous thing by hand for its custom ref.

The fetch refspec is now:

```
refs/side-quest/quests:refs/side-quest-remote/quests
```

A `git fetch` (or the fetch sync runs internally) always updates
`refs/side-quest-remote/quests` — that's an ordinary fast-forward from git's point of view,
since nothing else ever writes to it, so it never fails and never needs reconciling. Your
**live** ref, `refs/side-quest/quests` — the one `side-quest list`/`show`/`new` read and
write — is only ever touched by side-quest's own code, deliberately, after computing a real
merge. `install-hooks` (and `onboard`, which calls it) configure this fetch refspec
automatically and idempotently, and deliberately add no push refspec — see
[Automatic on push](#automatic-on-push-the-pre-push-hook) for why.

## The three-way merge

This is a **three-way merge**: given a common starting point (`base`) and two things that
each changed since (`local`, `remote`), decide the combined result. It's the same concept
git itself uses for `git merge`, just applied per *quest*, not per line of a text file — a
line-based diff of two quest Markdown files would be meaningless noise; the merge needs to
understand what a title, a status, a note actually are.

For every quest id that exists on any side, `internal/merge` looks at that id's version on
`base` (`b`), `local` (`l`), and `remote` (`r`) — any of which may be absent — and applies one
rule:

| Case | Condition | Result |
|------|-----------|--------|
| Add on one side | present on exactly one of `l`, `r` | take that version |
| Identical | `l` and `r` are equal | take it |
| Unchanged locally | `b` exists, `l == b`, `r != b` | take `r` |
| Unchanged remotely | `b` exists, `r == b`, `l != b` | take `l` |
| Both changed | `b` exists, `l != b`, `r != b`, `l != r` | **field-merge** (below) |
| Deleted on both | `b` exists, `l` and `r` absent | absent |

"Equal" compares the quest's parsed fields, not raw file bytes, so re-saving a file with
different YAML key order doesn't read as a change — `quest.Marshal` is deterministic (fixed
field order, sorted map keys), so every equality/tiebreak check in this document is stable
across machines.

### Both changed: field-merge (whole-quest last-writer-wins)

When both sides edited the same quest since the base, the merge needs to pick a winner for
the fields that can't be combined (a title can't be "half from each side"). It uses each
side's **`Touch` time** — the commit time of the last commit on that side that modified
`quests/<id>.md` — and the side touched *later* wins the scalar fields: `Title`, `Status`,
`Type`, `Priority`, `Context`, `Completed`. (On an exact tie, which second-granularity
timestamps can produce, the side whose marshaled bytes sort lexicographically larger wins —
a rule that depends only on content, not on which side happens to be "local," so both clones
independently compute the same winner.)

Not every field follows the winner, though — anything that can accumulate value from both
sides does, instead of picking one side and discarding the other's work:

- **`Created`** — the earliest timestamp seen on `base`/`local`/`remote`. A quest's birth time
  never moves forward just because it was edited.
- **`Commits`** — union: the base's list, in order, then any commit shas new to either side
  appended (deduped). Nobody's commit link is ever dropped.
- **`Tags`** — union of keys; a key present with different values on both sides takes the
  winner's value.
- **`Body`** — a quest body is an optional preamble followed by a list of timestamped notes
  (`--- note <ts> ---` sections). The preamble is the winner's. The notes are the **union** of
  `(timestamp, text)` pairs from both sides, deduplicated and sorted by timestamp — so if you
  and a teammate both added notes to the same quest while offline, sync keeps both.

### Id collisions

Two offline clones can independently mint the *same* id for two *different* quests — most
often under the sequential id strategy, where both clones' counters start from the same
`seq_next` and each allocates `SQ-0007` to something unrelated. The merge sees this as the
"add on both sides" case, except `l` and `r` aren't equal: same id, genuinely different
content, no common base. That's a **collision**, and it's resolved deterministically so every
clone computes the identical outcome without needing to talk to each other first:

1. The version with the **earlier `Created`** keeps `SQ-0007`. (Tie: the lexicographically
   smaller marshaled bytes keeps it — again, a rule independent of which side is "local".)
2. The loser is re-keyed to `<prefix>-<first 6 hex of sha256(its marshaled bytes)>` (where
   `<prefix>` is the repo's configured `id_prefix`) — a content-derived id, so both clones
   compute the identical replacement without coordinating. For example, in a repo with the
   default `SQ` prefix, if the local clone's `SQ-0007` ("Add timeout to retry loop") is the
   loser, it might become `SQ-a3f9c2`. In the vanishing chance that id is already taken, the
   merge widens to 8 hex characters, then 10, and so on — still a pure function of the bytes.
3. A note is appended to the loser recording what happened —
   `renamed from SQ-0007 on sync: id collision` — so the history of the rename is visible
   inside the quest itself, and a `Renamed` event is reported (`side-quest sync` prints a
   count).

This means the *content* of both clones' "SQ-0007" survives; only the losing one's id
changes, and it says so.

Collisions are only possible under the sequential strategy, so `side-quest init` defaults
to **random** ids when a remote already exists. A repo that gained its remote *after* init
keeps sequential ids silently, though — so `side-quest sync` nudges you toward random ids
(`config set id_strategy random`) whenever it runs against a remote while ids are still
sequential (SQ-0035).

### Config merge

`_config.yaml` merges too, since it lives on the same ref: `seq_next` (the sequential-id
counter) always takes `max(base, local, remote)` — the counter can only move forward, so a
merge can never cause an id to be re-minted. Every other config field
(`id_prefix`, `seq_width`, `id_strategy`, `tone`, `auto_trailer`, `require_quest`) is
last-writer-wins by the commit time of the last change to `_config.yaml` on each side — these
are expected to agree across clones already; the rule just needs to be defined for the rare
case they don't.

## Convergence via two-parent merge commits

A **merge commit** is git's normal representation of "these two histories are now one": a
commit with **two parents** instead of the usual one, recording that this point in history
descends from both. `git merge` produces these routinely for branches; sync produces one for
the quest ref, with parents `[local tip, tracking-ref tip]`, and a tree containing exactly the
merged result — the on-ref equivalent of the mutation transaction described in
[`docs/architecture.md`](architecture.md#writes-the-mutation-transaction): a scratch index,
no working tree involved.

The reason this matters beyond bookkeeping: **the resolution becomes history**. Once that
merge commit is pushed, any other clone that later syncs runs `git merge-base` between its own
tip and the tracking ref — and finds this merge commit sitting *between* the two old tips.
Relative to it, the remote has already "made" the resolved changes, so that clone's own merge
sees an ordinary fast-forward or unchanged-side case, not a fresh collision to re-derive. A
resolution computed once and pushed is *inherited*, not recomputed, by everyone downstream.

The remaining question is the narrow window before that push lands: what if two clones both
resolve the *same* divergence independently, before either has pushed? This is what the
determinism in every rule above buys: because every tiebreak depends only on the quests'
content (`Created`, marshaled bytes) and never on which side is "local," both clones compute
byte-identical merge results even though neither saw the other's resolution. Whichever pushes
first "wins" the race for that particular commit object, and the other's push is rejected as
non-fast-forward and retried against the newly-fetched tip — but the *content* both clones
independently produced already agreed, so nothing is lost or re-conflicted in the retry.

## Automatic on push: the `pre-push` hook

git runs a **`pre-push` hook** locally, before any objects are sent to the remote, whenever
you run `git push`. `install-hooks` installs a `pre-push` shim alongside the existing three
(`prepare-commit-msg`, `commit-msg`, `post-commit` — see
[Commit linking & hooks](architecture.md#commit-linking--hooks-phase-2)) that calls
`side-quest pre-push`, which runs the sync engine — fetch into the tracking ref, reconcile,
push — targeting whichever remote you're pushing to (git passes the remote name as an
argument to the hook).

Two details matter here:

- **It ignores the hook's stdin.** git's `pre-push` hook also receives, on stdin, a list of
  the refs being updated — but the investigation behind SQ-0032 found that git *omits* a
  ref from that list precisely when the push would be a non-fast-forward — which is the one
  case sync exists to fix. Trusting stdin would mean the hook silently does nothing exactly
  when it's needed most. Instead it always runs, using only the remote name from argv.
- **It warns, never blocks.** Quest bookkeeping must never be the reason your actual code
  push fails. If sync can't complete for any reason — offline, the remote unreachable, still
  contended after its retry budget — the hook prints a warning to stderr and exits `0` so
  your branch push proceeds regardless:

  ```
  warning (side-quest): couldn't publish quests to origin: <reason>
                        run `side-quest sync` when back online.
  ```

  (Its own inner push of the quest ref runs with `--no-verify`, so it doesn't recursively
  re-trigger this same hook.)

Because of this, **the hook is the normal publish path** — a bare `git push` reconciles and
publishes quests transparently, with no extra step. `side-quest sync` (below) exists for
everything the hook's warn-and-move-on philosophy can't cover: CI, explicit recovery after
you were offline, or just wanting to see the plan before it happens.

## Recovering manually

```
side-quest sync [--dry-run] [--remote <name>]
```

Run this any time you want to reconcile without going through a `git push` — after working
offline, in CI, or after the `pre-push` hook has warned that it couldn't publish. It runs the
same fetch → reconcile → push loop as the hook and prints a one-line summary:

```
side-quest: synced origin: merged 2, renamed 1, pushed true.
```

or, if nothing needed doing, `side-quest: synced origin: already up to date.`

- **`--remote <name>`** — sync against a specific remote. Defaults to `origin` if configured,
  or the sole remote if exactly one exists; otherwise it's an error (there's no way to guess).
- **`--dry-run`** — fetch and compute the merge, print what *would* happen, and stop:
  nothing is written to the live ref and nothing is pushed. Use it to preview a merge — how
  many quests would be integrated, whether any id collisions would be renamed — before
  committing to it.

Unlike the hook, a direct `side-quest sync` exits non-zero on a genuine failure (offline,
still contended past the retry cap, a real git fault) — it's a tool a human or CI is expected
to look at, not a courtesy that must never interrupt anything.

## Safety

The guarantee sync makes, stated plainly:

- **It only ever writes `refs/side-quest/*` and a throwaway scratch index.** Every merge
  commit is built the same way every other quest mutation is (see
  [Writes: the mutation transaction](architecture.md#writes-the-mutation-transaction)) — a
  temporary `GIT_INDEX_FILE`, never `.git/index`.
- **It never touches your branches, your real index, or your working tree.** There is no
  `checkout`, `reset`, `branch`, or `git merge` anywhere in the sync code path — nothing sync
  does can move `HEAD`, change what's on disk, or alter a file you have open.
- **Pushes are never forced.** The only ref sync ever pushes is
  `refs/side-quest/quests:refs/side-quest/quests`, and a non-fast-forward push is *rejected*
  by the remote and retried against a fresh fetch, not overridden — so the remote's quest
  history only ever grows (via fast-forwards and two-parent merge commits), never rewrites.
- **Your `git push` semantics are untouched.** `install-hooks` configures no push refspec at
  all (SQ-0121), so a bare `git push` keeps whatever `push.default` behavior you chose; the
  quest ref rides along via the `pre-push` hook. (Versions before SQ-0121 set
  `remote.origin.push = HEAD`, which silently overrode `push.default` — if you find a `HEAD`
  entry you never added yourself, `git config --unset-all remote.origin.push '^HEAD$'`
  restores your defaults.)
