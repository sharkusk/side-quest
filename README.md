# side-quest

A lightweight, project-specific quest/task tracker for capturing the *side quests* — the
new ideas, follow-ups, and research tangents that occur to you while working on something
else — without derailing your current session, and with a clean two-way link between quests
and the git commits that address them.

> **Status: CLI + MCP server ready; further phases in progress.** The quest store, git hooks, CLI commands (init/new/list/show/status/reclassify/config), and MCP server (`side-quest serve`) are built and tested. Voice/tone is built (see "Tone" below); the babelmap importer and plugin packaging remain in development.
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
- **type / priority** — every quest carries a `type` (bug/feature) and a `priority`
  (high/low), constrained enums that default to feature/low when a quick capture omits them.
- **trailer** — `Quest: SQ-xxxx` / `Completes: SQ-xxxx` lines in a commit
  message; a `post-commit` hook reads them and links the commit to the quest
  (`Quest: none` opts a chore out).
- **current quest** — a per-worktree pointer (`side-quest current <id>`) that
  `prepare-commit-msg` uses to auto-fill the `Quest:` trailer.

**→ For the full explanation of the storage model, CAS, the mutation flow, and id
allocation, see [`docs/architecture.md`](docs/architecture.md).**

## Usage

```
side-quest init
side-quest new "Fix the flaky parser test" --type bug --priority high
side-quest list --status open --type bug
side-quest show SQ-0001
side-quest status SQ-0001 done
side-quest reclassify SQ-0001 --priority low
side-quest config set require_quest true
side-quest config get
```

Add `--json` to `new`, `list`, `show`, or `config get` for machine-readable
output. Flags come before the title/id positional argument.

## Tone

Human-facing confirmations and warnings (not `--json`, quest bodies, config values, or
errors — those stay neutral no matter what) render in one of three tones: `plain`; `dcc`
(the default — a *Dungeon Crawler Carl*-flavored voice, an original homage with no
verbatim text); and `dcc-superfan`, which currently falls back to `dcc` with a one-time
hint (see "Credits & permissions" below). Set it with `side-quest config set tone
<value>`, or override it per-invocation with the `SIDE_QUEST_TONE` environment variable.

## MCP server

`side-quest serve` runs a stdio MCP server so any MCP-capable agent can capture,
read, and drive quests. Register it with your agent (end-user form, assumes
`side-quest` is on PATH):

```json
{ "mcpServers": { "side-quest": { "command": "side-quest", "args": ["serve"] } } }
```

Tools: `quest_new`, `quest_list`, `quest_show`, `quest_set_status`,
`quest_reclassify`, `quest_update`, `quest_note`, `quest_set_current`,
`quest_get_current`, `quest_link_commit`. Responses are neutral JSON.

**Developing side-quest with side-quest (dogfooding):** this repo's `.mcp.json`
uses `go run ./cmd/side-quest serve`, which recompiles from source on each
launch, so every new session runs your latest code — no install step, and it
won't disturb a `side-quest` you use elsewhere. Restart the server to pick up
code or tool-schema changes. Quest data lives on the git ref and is
binary-version-independent (the on-ref parser is default-tolerant), so switching
binaries mid-session is safe.

The babelmap importer and plugin distribution are in development.

## Credits & permissions

side-quest's `dcc` tone is an original homage to *Dungeon Crawler Carl* by Matt
Dinniman — no verbatim book/show text is included or shipped. Verbatim catch phrases are
never distributed with side-quest; the `dcc-superfan` tone only loads them from a file
you create yourself, at `~/.config/side-quest/superfan-lines.txt` (see
[`superfan-lines.example.txt`](superfan-lines.example.txt) for the format). Public or
committed use of verbatim phrases requires permission from the author.
