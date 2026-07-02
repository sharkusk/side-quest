# side-quest

A lightweight, project-specific quest/task tracker for capturing the *side quests* — the
new ideas, follow-ups, and research tangents that occur to you while working on something
else — without derailing your current session, and with a clean two-way link between quests
and the git commits that address them.

> **Status: pre-implementation.** The design is complete; the tool is not built yet.
> See the design spec:
> [`docs/superpowers/specs/2026-07-02-side-quest-design.md`](docs/superpowers/specs/2026-07-02-side-quest-design.md)

## The problem it solves

Markdown `TODO.md` / `COMPLETED.md` files can't cleanly link a quest to the commit that
completed it: a commit's hash doesn't exist until *after* the commit, and if the quest file
lives in the same repo, recording that hash needs another commit — with its own hash. The
loop never closes.

`side-quest` stores quest data on a dedicated git ref (`refs/side-quest/quests`), off your
main history and never checked out. A `post-commit` hook writes the now-known hash back into
the quest as a separate commit on that ref — so the loop closes cleanly, and the data still
travels with your repo.

## Concepts (overview)

side-quest stores quests as one Markdown file per quest on a dedicated git **ref**
(`refs/side-quest/quests`) — an **orphan ref** with its own history, off your main line and
never checked out. It reads and writes that ref with git's low-level **plumbing** commands
(never touching your working tree), and every change is committed with a **compare-and-swap
(CAS)** so parallel git worktrees stay safe without a lock.

Quick glossary:

- **ref / orphan ref** — a named pointer to a commit; the orphan ref holds quest data on its
  own root history.
- **plumbing** — git's scriptable low-level commands (`cat-file`, `write-tree`, `commit-tree`,
  `update-ref`), as opposed to everyday `add`/`commit`.
- **mutation** — any state change (create/update); each builds one new commit on the ref.
- **CAS (compare-and-swap)** — move the ref only if it still equals the expected old commit;
  how concurrent writers avoid lost updates without locking.
- **CRUD** — Create, Read, Update, Delete — the basic persistence operations the store exposes.
- **trailer** — `Quest: SQ-xxxx` / `Completes: SQ-xxxx` lines in a commit
  message; a `post-commit` hook reads them and links the commit to the quest
  (`Quest: none` opts a chore out).
- **current quest** — a per-worktree pointer (`side-quest current <id>`) that
  `prepare-commit-msg` uses to auto-fill the `Quest:` trailer.

**→ For the full explanation of the storage model, CAS, the mutation flow, and id
allocation, see [`docs/architecture.md`](docs/architecture.md).**

A full README (quickstart, CLI/MCP reference, plugin install, configuration) ships with the
implementation.
