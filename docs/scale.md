# Scale & performance

Does side-quest hold up as a repo accumulates hundreds or thousands of quests?
This page is the assessment (SQ-0053) and the one optimization it produced.

## The concern

Every quest lives as a file on an orphan git ref, and side-quest reads and
writes it purely through git plumbing subprocesses — no database, no long-lived
process. The open question was whether that subprocess-per-operation model needs
scale handling (pagination, archival/pruning, faster reads) as quest count grows,
since spawning `git` is not free — especially on Windows, where process creation
costs ~10–30× what it does on Linux (no `fork()`; each spawn is a full
`CreateProcess`, and Defender scans the image).

So the question isn't "how much data" — quests are tiny — it's **"how many `git`
processes does each operation spawn, and does that grow with N?"**

## What we measured

A throwaway repo, macOS, timing a single `list` and a single `new` as the quest
count climbs:

| N quests | `list` (before) | `new` |
|---------:|----------------:|------:|
| 25       | 303 ms          | 173 ms |
| 100      | 988 ms          | 178 ms |
| 250      | 2329 ms         | 176 ms |
| 500      | 5552 ms         | 191 ms |

Two clear signals:

- **`new` (and every mutation) is flat** — ~175 ms regardless of N. This matches
  the code: a mutation's `snapshot()` reads only `_config.yaml` and one
  `ls-tree` (2 spawns), and `read-tree`/`write-tree` do O(N) work *inside a
  single git process*, which git handles into the thousands trivially. Create,
  update, status, note, set-current — all **O(1) in subprocess spawns**.
- **`list` was linear** — ~11 ms per quest, hitting 5.5 s at 500. That's the one
  place the model didn't scale.

## The cause, and the fix

`List()` did `1 ls-tree + N × cat-file -p` — **one subprocess per quest** — then
filtered in Go. Both the CLI `list` and the MCP `quest_list` load every quest
this way, so even `list --status open` paid the full N. At ~11 ms of pure
process-spawn overhead per quest, the wall time was almost entirely fork/exec,
not work.

The fix reads every quest in **one** git process with `git cat-file --batch`:
feed it all the blob paths on stdin, parse the length-delimited stream back out
(`readFilesBatch` / `parseBatch` in `internal/store/store.go`). `List()` goes
from O(N) spawns to **O(1)**.

| N quests | `list` (before) | `list` (batched) | speedup |
|---------:|----------------:|-----------------:|--------:|
| 25       | 303 ms          | 98 ms            | 3×      |
| 100      | 988 ms          | 110 ms           | 9×      |
| 250      | 2329 ms         | 147 ms           | 16×     |
| 500      | 5552 ms         | 201 ms           | 28×     |

The curve is now essentially flat — the residual growth is in-process YAML
parsing and the single batch's data volume, not spawns. On Windows the win is
larger still: 500 spawns become 1.

## What we deliberately did *not* build

- **Pagination.** It doesn't address the root cost: filtering and sorting need
  every quest loaded anyway, so a "page" can't be served without reading the set.
  It's an output-ergonomics feature, and with batched reads the read is cheap for
  a long time. Terminal paging and `--status`/`--type`/`--priority` filters
  already cover "I don't want to see everything." **Deferred.**
- **Archival / pruning.** Moving done/discarded quests out of the working set
  reduces what `list` loads, but it's genuinely complex — a separate ref? an
  un-archive flow? sync/merge implications across clones? — for a saving that
  batched reads make unnecessary until *many* thousands of quests. **Deferred**
  until there's evidence the flat curve has actually bent.

## Bottom line

The orphan-ref, subprocess-per-operation design scales well: mutations are
constant-cost in spawns, and the single weak spot — `List()`'s per-quest read —
is fixed with a self-contained batched read. Pagination and archival are
premature; revisit only if real-world quest counts push list latency back up.
