<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/logo-dark.png">
    <img src="docs/logo-light.png" alt="side-quest" width="280">
  </picture>
</p>

# side-quest

A streamlined, git-native issue tracker for individuals and small teams. Capture
the *side quests* — the new ideas, follow-ups, and research tangents that occur to
you mid-work — without derailing your current session, and keep a clean two-way
link between every quest and the git commits that address it.

> **Status: CLI + MCP server + plugin packaging ready.** The quest store, git
> hooks, CLI (init/new/list/show/status/reclassify/config), MCP server
> (`side-quest serve`), and the Claude Code plugin are built and tested.
> A `sync` command to move quests across machines is planned (see "Roadmap").

## The problem it solves

Most trackers can't cleanly link a task to the commit that resolved it: a commit's
hash doesn't exist until *after* the commit, and if the task lives in the same repo,
recording that hash needs another commit — with its own hash. The loop never closes.

side-quest stores quest data on a dedicated git ref (`refs/side-quest/quests`), off
your main history and never checked out. A `post-commit` hook writes the now-known
hash back into the quest as a separate commit on that ref — so the loop closes
cleanly, and the data still travels with your repo.

## Concepts

side-quest stores quests as one Markdown file per quest on a dedicated git **ref**
(`refs/side-quest/quests`) — an **orphan ref** with its own history, off your main
line and never checked out. It reads and writes that ref with git's low-level
**plumbing** commands (never touching your working tree), and every change is
committed with a **compare-and-swap (CAS)** so parallel git worktrees stay safe
without a lock.

- **ref / orphan ref** — a named pointer to a commit; the orphan ref holds quest
  data on its own root history.
- **type / priority** — every quest carries a `type` (bug/feature) and a `priority`
  (high/low), enums that default to feature/low when a quick capture omits them.
- **status** — `open` (default), `partial`, `done`, `deferred`, `discarded`.
- **trailer** — `Quest: SQ-xxxx` / `Completes: SQ-xxxx` lines in a commit message; a
  `post-commit` hook reads them and links the commit to the quest (`Quest: none`
  opts a chore out).
- **current quest** — a per-worktree pointer (`side-quest current <id>`) that
  `prepare-commit-msg` uses to auto-fill the `Quest:` trailer.

**→ For the storage model, CAS, the mutation flow, and id allocation, see
[`docs/architecture.md`](docs/architecture.md).**

## Usage

```
side-quest init
side-quest new "Fix the flaky parser test" --type bug --priority high
side-quest list --status open --type bug
side-quest show SQ-0001
side-quest status SQ-0001 done
side-quest note SQ-0001 "flaky since the timer refactor"
side-quest reclassify SQ-0001 --priority low
side-quest config set require_quest true
side-quest config get
```

Add `--json` to `new`, `list`, `show`, or `config get` for machine-readable output.
Flags may appear before or after the title/id positional argument.

## Installation & setup

side-quest is a single Go binary plus per-project git hooks. How you get the
binary depends on your agent; setting up each repo is the same either way.

**1. Get side-quest running with your agent — pick one:**

- **[Claude Code plugin](docs/plugin.md)** — `/plugin install side-quest`
  registers the MCP server, the `/sq` command, and the guidance skill, and
  **auto-provisions the binary** (downloaded and checksum-verified). No separate
  install.
- **[Manual setup](docs/manual-setup.md)** — for any MCP-capable agent:
  [install the binary](docs/install.md) yourself, then register the MCP server
  and merge side-quest's guidance into your `AGENTS.md`.

**2. Set up each repo you want to track** — run `side-quest onboard` once: it
creates the quest ref, installs the git hooks, and writes a project `.mcp.json`
if absent. (Or do it by hand with `side-quest init` + `side-quest install-hooks`.)

Moving between machines? See
[Sharing quests across machines](docs/manual-setup.md#sharing-quests-across-machines).

## Development

- **Requirements:** Go ≥ 1.25; the system `git` binary (used as a subprocess);
  `gopkg.in/yaml.v3`; the MCP Go SDK. No CGo — a pure-Go static binary.
  [GoReleaser](https://goreleaser.com) is needed only to cut releases.
- **Layout:** `internal/` packages (`quest`, `config`, `gitcmd`, `store`,
  `trailer`, `voice`) with the `cli` and `mcp` frontends under `cmd/side-quest`.
- **Build & test:**

  ```
  go build ./...
  go test ./...
  go vet ./...
  ```

- **Cutting a release:** bump `VERSION` and `plugin.json`'s `version` together, then
  `git tag v$(cat VERSION) && git push --tags`. The release workflow runs GoReleaser
  and publishes the six platform archives + `checksums.txt`. Validate the config
  locally with `goreleaser check` and `goreleaser build --snapshot --clean`.
- **Developing side-quest while using it elsewhere:** keep the released
  `side-quest` on your `PATH` for the project you track in production; for
  development, build a local `./side-quest` (`go build -o side-quest ./cmd/side-quest`)
  and invoke it explicitly. Never point a work-in-progress binary at a live repo —
  run it against the side-quest repo itself or throwaway `git init` scratch repos
  (the test suite already isolates via temp repos). Quest data is per-repo on
  `refs/side-quest/*`, so working in this repo cannot touch another project's quests.

## Roadmap

- **`sync`** — a command to pull/push the quest ref across machines (planned).
