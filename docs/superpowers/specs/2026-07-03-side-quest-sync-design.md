# side-quest — Sync Design Spec (SQ-0031)

**Date:** 2026-07-03
**Status:** Approved for implementation planning
**Author:** Marcus Kellerman (with Claude)
**Quests:** SQ-0031 (sync), subsumes SQ-0032 (warn on diverged quest ref)

---

## 1. Overview & Problem Statement

Quests live on a dedicated orphan ref, `refs/side-quest/quests`, with its own root
history (main design spec §5). That ref is pushable, so two people — or one person on two
machines — can each build on their own copy. The moment both copies gain commits the ref
has **diverged**, and git's ordinary transport has no idea how to reconcile two quest
histories: a plain `git fetch` either fast-forwards (when only one side moved) or is
rejected (when both did), and a plain `git push` of a diverged ref is silently rejected
while the branch push succeeds (SQ-0032).

The current fetch refspec makes this worse. `install-hooks` configures
`remote.origin.fetch = refs/side-quest/*:refs/side-quest/*`, which points the fetch
*directly at the live local ref*. On divergence git refuses to move it, so the local ref
is left stale with no signal; on a fast-forward it clobbers the live ref with the remote's
value, discarding nothing only by luck.

**`side-quest sync`** replaces that with a real, domain-aware three-way merge: fetch the
remote quest ref into a *separate tracking ref* (never touching the live ref), merge the
two histories against their common ancestor using rules that understand what a quest *is*
(union the commit lists, keep the most recent edit of each field, union the notes, resolve
id collisions), record the result as a genuine two-parent merge commit, and push it back
with a fetch-merge-retry loop. The merge runs automatically inside a `pre-push` hook so a
bare `git push` reconciles quests transparently, and is also available as an explicit
command for CI or recovery.

### Why a merge commit, not a local rewrite

A resolution has to become **history**, or clones re-derive the same conflict forever.
The merge produces a commit with *two parents* — the local tip and the fetched tracking
tip. Once any clone resolves a divergence and pushes that merge commit, every other clone's
`git merge-base` walks *past* the resolution: relative to it, the remote already made the
change, so they adopt it instead of re-conflicting. Determinism (below) is the backstop for
the narrow window where two clones resolve the *same* divergence before either pushes — they
compute byte-identical results and still converge.

---

## 2. Goals & Non-Goals

### Goals

- Reconcile a diverged quest ref across clones without losing either side's work.
- Make a bare `git push` publish quests correctly and automatically.
- Never clobber the live local quest ref from the network.
- Guarantee **convergence**: all clones that sync reach the same quest tree regardless of
  sync order.
- Never block the user's real (branch) push over quest bookkeeping.
- Fold in SQ-0032: a reliable, network-free divergence warning.

### Non-Goals (this phase)

- No interactive conflict resolution UI. The merge always resolves automatically by rule.
- No field-level last-writer granularity: when both sides edit a quest, one side wins *all*
  its scalar fields (whole-quest), not a per-field cherry-pick.
- No recovery of *which human* made an edit; the only signal used is git commit time.
- No general N-remote topology. Sync targets one remote per invocation (the remote named by
  the push, defaulting to `origin`).

---

## 3. Architecture

Three layers, isolated so the hard part is pure and testable without git:

```
cmd/side-quest        cmdSync, pre-push hook shim   (entry points)
      │
internal/store        fetch → read 3 sides → write 2-parent merge commit → push-retry
      │                (all git plumbing; the only layer that touches the network)
      │
internal/merge        Merge(base, local, remote) → (Result, []Event)   (PURE, no I/O)
```

- **`internal/merge` is a pure function.** All the rules in §5–§7 live here. It takes three
  in-memory snapshots and returns the merged snapshot plus a list of events (renames,
  conflicts resolved) for reporting. No git, no clock, no randomness — every rule is a
  deterministic function of its inputs, which is what makes convergence provable and the
  behavior exhaustively table-testable.
- **`internal/store` does the git plumbing.** It reads the three snapshots at three commits,
  computes the commit timestamps the merge needs, writes the two-parent merge commit, and
  runs the push loop. It is the only layer that does I/O.
- **`cmd/side-quest` wires the entry points.** `cmdSync` for humans/CI; the `pre-push` shim
  for the automatic path. Both call one store method.

### 3.1 Data passed to the pure merge

```go
// Side is one snapshot of the store: everything the merge needs from one commit.
type Side struct {
    Config      config.Config
    Quests      map[string]*quest.Quest // keyed by id (the filename stem)
    Touch       map[string]time.Time    // per-quest: commit time of the last commit that
                                         // touched quests/<id>.md on this side
    ConfigTouch time.Time                // commit time of the last commit that touched _config.yaml
}

// Merge is the whole engine. base may be the zero Side (no common ancestor).
func Merge(base, local, remote Side) (Result, []Event)

type Result struct {
    Config config.Config
    Quests map[string]*quest.Quest
}

type Event struct {
    Kind   EventKind // Renamed | Conflicted (reported to the user)
    ID     string    // the id involved (post-rename id for Renamed)
    Detail string    // human-readable, e.g. "renamed from SQ-0007 (id collision)"
}
```

`Touch` and `ConfigTouch` are the only git-derived inputs. The store computes them **only
for the quests in the conflict set** (present and differing on both sides) — one
`git log -1 --format=%cI <commit> -- quests/<id>.md` per side per conflicted quest. The
common case (disjoint edits) needs none. A whole-history walk is a later optimization, not
needed for correctness.

---

## 4. The tracking ref and refspec change

- **Fetch:** `refs/side-quest/quests:refs/side-quest-remote/quests`. A plain `git fetch`
  now updates the *tracking* ref, `refs/side-quest-remote/quests`, and never touches the
  live `refs/side-quest/quests`. The tracking ref is what sync reads as "remote" and what
  the divergence warning compares against.
- **Push:** keep `HEAD` (so a bare `git push` still pushes the current branch, per SQ-0016),
  and **remove** the `refs/side-quest/*` push entry. The quest ref is now published by the
  hook, not by git's refspec, because only the hook can guarantee the ref is
  fast-forwardable at push time.

### 4.1 Migration of existing installs

`install-hooks` (and `onboard`, which calls it) reconcile the refspecs idempotently:

1. Remove any `remote.<r>.fetch` value equal to the old `refs/side-quest/*:refs/side-quest/*`.
2. Remove any `remote.<r>.push` value equal to `refs/side-quest/*:refs/side-quest/*`.
3. Add the new fetch refspec (§4) if absent.
4. Ensure `remote.<r>.push` still contains `HEAD`.

This runs against the remote the push refspec already targets (`origin` today). It rewrites
config on upgrade — an intentional, one-time behavior change flagged in the release notes.

### 4.2 Fresh-clone bootstrap

A clone has no live `refs/side-quest/quests` until a sync runs, so `side-quest list` would
be empty right after cloning. `openStore()` performs a **local-only** bootstrap (no
network): if the tracking ref exists and the live ref is absent or a strict ancestor of it,
fast-forward the live ref to the tracking ref. Combined with `onboard` running one
`git fetch` after adding the refspec, quests appear immediately on setup. The bootstrap is
pure ref surgery — it only ever *advances* the live ref along ancestry it already contains,
so it can never lose local work.

---

## 5. The three-way merge

The merge iterates the **union of ids** across `base`, `local`, and `remote`. For each id,
let `b`, `l`, `r` be that id's quest on each side (any may be absent).

| Case | Condition | Result |
|------|-----------|--------|
| Add on one side | present on exactly one of `l`, `r` | take that version |
| Identical | `l` and `r` equal (field-for-field) | take it |
| Unchanged locally | `b` exists, `l == b`, `r != b` | take `r` |
| Unchanged remotely | `b` exists, `r == b`, `l != b` | take `l` |
| Both changed | `b` exists, `l != b`, `r != b`, `l != r` | **field-merge** (§5.1) |
| Deleted both | `b` exists, `l` and `r` absent | absent |

There is no public delete operation in the store, so delete/modify cases cannot arise in
practice; if a hand-edited ref ever produced one, the modified side is kept (never silently
drop work).

Quest equality compares the marshaled fields, not the raw bytes, so a cosmetic
re-serialization does not read as a change. Marshaling is deterministic — `quest.Marshal`
emits fields in declaration order and yaml.v3 sorts map keys — so every byte comparison in
this spec (equality, the §5.1 tiebreak, the §5.2 collision id and keeper) is stable across
machines.

### 5.1 Field-merge (both sides changed the same quest)

The winner is the side with the **later `Touch` time** for that quest (whole-quest
last-writer-wins). On equal `Touch` (second-granularity stamps can tie), the side whose
marshaled bytes are lexicographically larger wins — a tiebreak that depends only on content,
not on which side happens to be "local," so both clones pick the same winner and converge.
Then:

- **Scalars** — `Title`, `Status`, `Type`, `Priority`, `Context`, `Completed`: take the
  winner's value.
- **`Created`**: earliest of `b`, `l`, `r` (birth time never moves; sides that share a base
  already agree).
- **`Commits`**: union — start from the base order, then append shas new to either side,
  deduped.
- **`Tags`**: union of keys; a key present on both sides with different values takes the
  winner's value; an empty value is not special here (deletion semantics belong to the edit
  commands, not the merge).
- **`Body`**: split each side's body into an optional *preamble* (prose before the first
  `--- note <ts> ---` header) and a list of note entries. Preamble = winner's. Notes = union
  of `(timestamp, text)` entries across both sides, deduped, sorted by timestamp. This
  preserves both people's notes, which is the field's whole purpose.

The `Touch`-based tiebreak relies on committer clocks being roughly sane. side-quest stamps
every commit itself in UTC, so skew is normally sub-second; the fields it decides (a title
wording, a priority flip) are low-stakes, and notes/commits — the fields that actually
accumulate value — never depend on the tiebreak because they union unconditionally.

### 5.2 ID collisions

A collision is the *add-on-both-sides* case where `b` is absent, both `l` and `r` exist, and
they are **not equal** — two genuinely different quests minted under the same id by two
offline clones. (SQ-0030 defaults remote-configured repos to random ids to prevent this
going forward; the merge handles legacy sequential data and any repo that opted back to
sequential.)

Resolution, fully deterministic so every clone computes the same outcome:

1. The quest with the **earlier `Created`** keeps the id. Tiebreak on equal `Created`: the
   lexicographically smaller marshaled byte string keeps the id.
2. The loser is re-keyed to `PREFIX-<first 6 hex of SHA-256 of its marshaled bytes>` — the
   same shape as a random id, identical on every clone, astronomically unlikely to
   re-collide. In the vanishing case that the derived id is already taken in the union, take
   the next 6 hex characters of the same hash (still deterministic).
3. A note is appended to the loser recording the rename (`renamed from SQ-0007 on sync: id
   collision`), and a `Renamed` event is emitted for reporting.

Because the resolution is written into the two-parent merge commit (§1), other clones
inherit it as history rather than re-deriving it; determinism only has to cover the
simultaneous-resolution window, which it does.

### 5.3 Config merge

- **`seq_next`**: `max(base, local, remote)` — the counter only ever moves forward, so no id
  is re-minted after a merge.
- **`id_prefix`, `seq_width`**: last-writer-wins by `ConfigTouch` (these should be identical
  across clones; the rule just needs to be defined).
- **`id_strategy`, `tone`, `auto_trailer`, `require_quest`**: last-writer-wins by
  `ConfigTouch`.

### 5.4 No common ancestor

If `git merge-base refs/side-quest/quests refs/side-quest-remote/quests` finds nothing (two
independently-`init`ed histories joined by adding a remote), `base` is the zero `Side`.
Every quest is then an add-on-both-or-one-side case: identical ids that are genuinely
different quests fall to §5.2, everything else unions. The result is still recorded as a
two-parent merge commit (git permits merges across unrelated histories), and convergence
holds.

---

## 6. Reconcile, commit, and push

`store.Sync(remote string, opts SyncOptions)` runs the loop. Before merging it classifies
the relationship between the live ref (`local`) and the tracking ref (`remote`):

| Relationship | Action |
|--------------|--------|
| `local == remote` | nothing to merge |
| remote is ancestor of local | local is ahead — skip merge, go straight to push |
| local is ancestor of remote | fast-forward the live ref to remote; nothing to push |
| diverged | run `merge.Merge`, write a two-parent merge commit, move the live ref via CAS |

Then the publish loop:

```
for try := 0; try < maxSyncTries; try++ {
    git fetch <remote> refs/side-quest/quests:refs/side-quest-remote/quests
    reconcile()                       // fast-forward or two-parent merge commit
    if nothingToPush { return ok }
    err := git push <remote> refs/side-quest/quests   // --no-verify on the hook path
    if err == nil { return ok }
    if isNonFastForward(err) { continue }             // someone pushed meanwhile; re-fetch
    return err                                        // genuine failure
}
return errStillContended
```

The two-parent merge commit is what makes each push a **fast-forward**: the local tip
descends from the tracking tip it just merged, so the remote (still at that tracking tip,
absent a race) accepts it. A race is caught by the non-fast-forward branch and retried
against the newly-fetched tip — the network analogue of the store's existing CAS loop.

The merge commit is built by a new `store` helper that extends the existing scratch-index
machinery (`buildCommit`) to accept **two parents**; the message is
`side-quest: sync merge` with a one-line summary of counts.

---

## 7. CLI and hook

### 7.1 `side-quest sync`

```
side-quest sync [--dry-run] [--remote <name>]
```

- Default remote: `origin` (or the sole remote if exactly one is configured).
- Prints a one-line summary: `synced <remote>: merged N, renamed M, pushed` (or
  `already up to date`).
- `--dry-run`: fetch and compute the merge, print the plan (what would merge/rename/push),
  **write nothing** and push nothing.
- Exit non-zero only on a genuine failure the user must act on (e.g. a merge the engine
  cannot complete — which, by construction, should not happen — or a push that stays
  contended past the retry cap). Offline is *not* a hard failure here: it prints the reason
  and exits non-zero **only** when invoked directly (so CI notices); see §7.2 for the hook.

### 7.2 The `pre-push` hook

A new shim, installed by `install-hooks` alongside the existing three, invokes the sync
engine with the remote name from the hook's **argv** (`$1`). It deliberately ignores the
hook's **stdin**: the SQ-0032 investigation proved git omits a non-fast-forward ref from
that stdin, so it cannot be trusted to reveal the very divergence sync exists to fix. argv
carries the remote name and URL unconditionally, which is all the hook needs.

- **Recursion guard.** The engine's inner `git push <remote> refs/side-quest/quests` runs
  with `--no-verify`, so it does not re-enter the pre-push hook.
- **Warn, never block.** Quest bookkeeping must never fail a real code push. If sync cannot
  complete — offline, remote unreachable, still contended after retries — the hook prints a
  warning and **exits 0**:

  ```
  warning (side-quest): couldn't publish quests to origin: <reason>
                        run `side-quest sync` when back online.
  ```

  This warning is the SQ-0032 deliverable, now reliable because it is backed by the tracking
  ref rather than an untrustworthy hook stdin.
- **No remote / not configured.** If the pushed remote has no side-quest fetch refspec, the
  hook is a no-op.

---

## 8. Error handling summary

| Situation | Direct `side-quest sync` | `pre-push` hook |
|-----------|--------------------------|-----------------|
| Already up to date | print, exit 0 | silent, exit 0 |
| Merged & pushed | print summary, exit 0 | print summary, exit 0 |
| Offline / remote unreachable | print reason, exit non-zero | warn, **exit 0** |
| Still contended past retry cap | print reason, exit non-zero | warn, **exit 0** |
| Genuine git fault (bad object, etc.) | surface error, exit non-zero | warn, **exit 0** |
| id collision / both-changed | resolved by rule (never an error) | resolved by rule |

The asymmetry is deliberate: the standalone command is a tool whose failures a human or CI
should see; the hook is a courtesy that must never stand between the user and their branch
push.

---

## 9. Testing strategy

- **`internal/merge` (pure, table-driven).** One table per rule in §5–§7: add-on-one-side,
  identical, unchanged-side (both directions), both-changed scalar LWW (winner by `Touch`),
  `Created`-earliest, commits union, tags union with conflicting key, body preamble +
  note-union + dedup + ordering, id collision (correct keeper, deterministic new id, rename
  note/event), config `seq_next` max, config scalar LWW, empty base. Determinism is asserted
  by running the same inputs with `local`/`remote` swapped where the rule is symmetric, and
  by asserting the derived collision id is a pure function of bytes.
- **`internal/store` (two real repos, one origin).** Extend the existing `newRepo` harness:
  clone A and clone B sharing a bare origin, drive genuine divergence (each creates/edits
  quests), run sync on each, and assert (a) both converge to the same tree, and (b) a clone
  that syncs *after* a resolution **inherits** it — its second sync produces no new merge
  commit. Cover the fast-forward, ahead, and diverged relationships from §6, and the
  fetch-merge-retry path by advancing the origin between a clone's fetch and push.
- **`cmd/side-quest` (binary harness).** `side-quest sync --dry-run` writes nothing and
  leaves the ref unchanged; the hook path publishes fast-forward on a normal push; an
  offline hook degrades to a warning and exit 0 without blocking the branch push; the
  refspec migration rewrites old config to new idempotently.

Per the project's TDD rule, every one of these is written and watched fail before the code
that satisfies it.

---

## 10. Documentation deliverables

- **`docs/sync.md`** — a standalone, teaching-quality explainer: why the quest ref diverges,
  the tracking-ref model, the three-way merge rules, the convergence-via-merge-commit
  argument, the automatic hook flow, and how to recover manually with `side-quest sync`.
- **`docs/architecture.md`** — a Sync section cross-linking `docs/sync.md` and recording the
  refspec change and the two-parent merge-commit helper.
- **`README.md`** — a short "working with others" note pointing at `docs/sync.md`.
- **`docs/manual-setup.md`** — the new fetch refspec and the removal of the quest push
  refspec, for users wiring hooks by hand.

Living-doc rule applies: each doc changes in the *same* commit as the behavior it describes.

---

## 11. Open risks (accepted)

- **Clock skew** decides scalar tiebreaks (§5.1). Accepted: side-quest stamps commits in
  UTC itself, the decided fields are low-stakes, and the accumulating fields union
  regardless.
- **Post-rename edits to a collided quest** (a clone edits its `SQ-0007` after another clone
  already renamed that quest to a hash id on the remote) can misattach an additive note to
  the surviving `SQ-0007`. Accepted: it requires editing inside the resolve-but-not-yet-
  synced window, notes are additive (no loss of other data), and SQ-0030 makes collisions
  rare by construction.
- **The hook becomes the publish path.** A repo with hooks uninstalled won't publish quests
  on a bare `git push`. Accepted and mitigated: hooks are part of standard setup, and
  `side-quest sync` is the explicit fallback.
```
