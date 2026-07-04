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
> (`side-quest serve`), quest sync across machines, and the Claude Code plugin
> are built and tested.

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
side-quest list                    # outstanding work: open + partial quests
side-quest list --all              # every status, including done/deferred/discarded
side-quest list --status done --type bug
side-quest list --filter "bug and not (done or deferred)"
side-quest show SQ-0001
side-quest status SQ-0001 done
side-quest note SQ-0001 "flaky since the timer refactor"
side-quest edit SQ-0001            # open the quest in $EDITOR, save to write it back
side-quest reclassify SQ-0001 --priority low
side-quest config set require_quest true
side-quest config get
```

A bare `list` shows only the outstanding quests (open and partial) — the common
"what's left?" view; pass `--all` to include every status, or an explicit
`--status` to select one. For richer selection, `--filter` takes a boolean
expression over bare enum values (`bug`, `high`, `done`, …) and `key=value`
tags, with `and`, `or`, `not`, and parentheses — e.g.
`--filter "not (done or deferred)"`. It replaces the simple `--status`/`--type`/
`--priority`/`--tag`/`--all` flags rather than combining with them. Add `--json`
to `new`, `list`, `show`, or `config get` for machine-readable output.
Flags may appear before or after the title/id positional argument. Anywhere an
`<id>` is expected you can use shorthand — `side-quest show 1` (or `0001`) is the
same as `side-quest show SQ-0001`. Every command also prints its own help with
`side-quest <command> -h`.

**Tip — a shorter `sq`.** `side-quest` is a lot to type. Alias it to `sq`, which
matches the plugin's `/sq` slash command:

```sh
# bash / zsh — add to ~/.bashrc or ~/.zshrc
alias sq=side-quest
```

```powershell
# PowerShell — add to $PROFILE
Set-Alias sq side-quest
```

Then `sq list`, `sq show 1`, and so on. (An alias, not a shipped binary, so it
works the same however you installed side-quest — `go install`, a release
download, or your package manager.)

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
creates the quest ref, installs the git hooks, writes a project `.mcp.json` if
absent, and merges side-quest's guidance into the project's `AGENTS.md`. It's
safe to re-run after an upgrade — the guidance is a marker-wrapped, version-
stamped block that `onboard` refreshes in place (your own `AGENTS.md` content is
left untouched). (Or do it by hand with `side-quest init` +
`side-quest install-hooks`.)

### Working with agents

An MCP-capable agent drives side-quest through the `side-quest serve` tools, but
what makes it useful is the *guidance* the agent loads alongside them — the reflex
to capture a stray idea without derailing, and the commit-trailer contract that
links work to quests. That guidance ships as two mirrored files:

- [`AGENTS.md`](AGENTS.md) — agent-agnostic. `side-quest onboard` merges it into
  your project's own `AGENTS.md` as a marker-wrapped, version-stamped block it can
  refresh in place on later upgrades; run `side-quest agents-md` to print that
  block for a manual paste (the [manual setup](docs/manual-setup.md) covers this).
- [`skills/side-quest/SKILL.md`](skills/side-quest/SKILL.md) — the same guidance in
  Claude-plugin form, loaded automatically by the [Claude Code plugin](docs/plugin.md)
  and surfaced as the `/sq` capture command.

### Working with others

Once onboarded, quests sync automatically: every `git push` reconciles your quest
ref with the remote's, merging in anything a teammate or your other machine added
— no extra command needed. See [`docs/sync.md`](docs/sync.md) for how that
merge works, or run `side-quest sync` to reconcile without pushing (e.g. after
working offline). For wiring the refspec by hand instead of via `onboard`, see
[Sharing quests across machines](docs/manual-setup.md#sharing-quests-across-machines).

## Development

- **Requirements:** Go ≥ 1.25; the system `git` binary (used as a subprocess);
  `gopkg.in/yaml.v3`; the MCP Go SDK. No CGo — a pure-Go static binary.
  [GoReleaser](https://goreleaser.com) is needed only to cut releases.
- **Layout:** `internal/` packages (`quest`, `config`, `gitcmd`, `store`,
  `trailer`, `voice`, `filter`) with the `cli` and `mcp` frontends under
  `cmd/side-quest`.
- **Build & test:**

  ```
  go build ./...
  go test ./...
  go vet ./...
  ```

- **CI:** the [`ci`](.github/workflows/ci.yml) workflow runs build/vet/test on
  Linux, macOS, and Windows for every push to `main` and every pull request. The
  Windows job is load-bearing, not incidental: it runs the end-to-end hook test
  under Git for Windows' MSYS `sh`, proving the extensionless shims actually fire
  and invoke the `.exe` — the one thing a Unix runner can't verify (SQ-0034).

- **Cutting a release:** bump `VERSION` and `plugin.json`'s `version` together, then
  `git tag v$(cat VERSION) && git push --tags`. The release workflow runs GoReleaser
  and publishes the six platform archives + `checksums.txt`. The build requires the
  Go toolchain named by the `go` directive in `go.mod` (currently **1.25**); CI pins
  it via `go-version-file: go.mod`, so bumping the directive moves the release
  toolchain with it. Validate the config locally with `goreleaser check` and
  `goreleaser build --snapshot --clean`.
- **Dogfooding side-quest on itself:** the repo's `.mcp.json` is the bare
  end-user reference (`side-quest serve`, resolved on `PATH`), so the MCP server
  runs whatever `side-quest` is installed — not your working tree. To point it and
  the git hooks at HEAD, run `make dev`: it `go install`s HEAD to your `GOBIN`
  (the binary both resolve to), re-points the hook shims at it, and links the
  plugin's `/sq` command into `.claude/commands/`. Re-run `make dev` (or just
  `make install`) after code changes, then **restart the MCP server** so it
  reloads the new binary. There's no separate MCP artifact to update — `serve`
  *is* the binary.
- **Dogfooding your dev build on another repo:** `make install` puts HEAD on your
  `PATH` (via `GOBIN`), and `PATH` is global — so a dev build is available in any
  repo. In the *other* repo, once: run `~/go/bin/side-quest onboard` (use the
  `GOBIN` binary explicitly so the hook shims bake in that stable path, which
  `make install` keeps refreshing). That creates the quest ref, installs hooks,
  writes `.mcp.json`, and prints the AGENTS.md snippet to merge; add `/sq` by
  installing the plugin globally or symlinking `commands/sq.md` into that repo's
  `.claude/commands/`. Steady state: edit side-quest → `make install` here →
  **restart the MCP server** there (hooks need no re-install — they point at
  `GOBIN/side-quest`). Prefer a scratch `git init` repo over a live project for a
  work-in-progress build: side-quest only ever writes `refs/side-quest/*` and
  `.git/hooks` (never your branches/index/worktree), so your code is safe, but a
  buggy build could still corrupt *quest* data.
- **Developing side-quest while using it elsewhere:** keep the released
  `side-quest` on your `PATH` for the project you track in production; for
  development, build a local `./side-quest` (`go build -o side-quest ./cmd/side-quest`)
  and invoke it explicitly. Never point a work-in-progress binary at a live repo —
  run it against the side-quest repo itself or throwaway `git init` scratch repos
  (the test suite already isolates via temp repos). Quest data is per-repo on
  `refs/side-quest/*`, so working in this repo cannot touch another project's quests.

