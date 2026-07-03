# side-quest

A streamlined, git-native issue tracker for individuals and small teams. Capture
the *side quests* — the new ideas, follow-ups, and research tangents that occur to
you mid-work — without derailing your current session, and keep a clean two-way
link between every quest and the git commits that address it.

> **Status: CLI + MCP server + plugin packaging ready.** The quest store, git
> hooks, CLI (init/new/list/show/status/reclassify/config), MCP server
> (`side-quest serve`), and the Claude Code plugin are built and tested. A
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
side-quest reclassify SQ-0001 --priority low
side-quest config set require_quest true
side-quest config get
```

Add `--json` to `new`, `list`, `show`, or `config get` for machine-readable output.
Flags may appear before or after the title/id positional argument.

## Installation

### Prebuilt binary (no toolchain)

Download the archive for your platform from the
[Releases](https://github.com/sharkusk/side-quest/releases) page, extract the
`side-quest` binary, and put it on your `PATH`.

| Platform | Where to put it | Notes |
|---|---|---|
| macOS | `/usr/local/bin` or `~/.local/bin` | `chmod +x side-quest`; first run may be blocked by Gatekeeper (unsigned) — clear it with `xattr -d com.apple.quarantine side-quest` |
| Linux | `~/.local/bin` (often on `PATH`) or `/usr/local/bin` (sudo) | `chmod +x side-quest` |
| Windows | a folder you add to `Path`, e.g. `%LOCALAPPDATA%\Programs\side-quest\` | use `side-quest.exe` |

### `go install` (needs Go ≥ 1.25)

```
go install github.com/sharkusk/side-quest/cmd/side-quest@latest
```

This installs to `~/go/bin` (`%USERPROFILE%\go\bin` on Windows), which is **not on
`PATH` by default** — add it:

- macOS/Linux: `export PATH="$HOME/go/bin:$PATH"` in your shell profile.
- Windows: add `%USERPROFILE%\go\bin` to your user `Path` environment variable.

### Build from source (needs Go ≥ 1.25)

```
git clone https://github.com/sharkusk/side-quest && cd side-quest
go build -o side-quest ./cmd/side-quest
```

### Per-project setup

Inside the repo you want to track:

```
side-quest init            # create the quest ref
side-quest install-hooks   # install git hooks + the refs/side-quest/* refspec
```

### Claude Code plugin

```
/plugin marketplace add sharkusk/side-quest
/plugin install side-quest
```

The plugin registers the `side-quest` MCP server and the `/sq` capture command. On
first use it **auto-provisions** the matching `side-quest` binary (downloaded from
the release and checksum-verified) into a per-plugin cache. If a download isn't
possible (offline, or before the project is public), install the binary yourself
with `go install github.com/sharkusk/side-quest/cmd/side-quest@latest` and the
plugin will use it from your `PATH`.

### Sharing quests across machines

Custom refs like `refs/side-quest/quests` are not fetched or pushed by default.
`side-quest install-hooks` configures both refspecs on `origin`, so once it has
run:

- `git fetch` / `git pull` also retrieve quest updates (the fetch refspec is
  additive — your normal fetch is unchanged).
- `git push` sends your current branch **and** the quest ref together (the push
  refspec keeps pushing your branch; it does not replace it).

A fresh `git clone` does **not** include the quest ref (Git skips custom refs on
clone) — run `side-quest install-hooks` in the clone, then `git fetch`, to pull
existing quests. To publish before the hooks are configured, push the ref
explicitly:

```
git push origin refs/side-quest/quests
```

A dedicated `sync` command that automates this is **planned** (see "Roadmap").

## Adopting side-quest in a project

Bringing side-quest into an existing repo — the full checklist:

1. **Install the binary** ([Installation](#installation)) and put it on your `PATH`.
2. **`side-quest init`** — create the quest ref (once per repo).
3. **`side-quest install-hooks`** — install the hooks and refspecs.
   > **Already have a git hook framework?** (Husky, pre-commit, or a custom setup
   > via `core.hooksPath`.) install-hooks composes into whatever hooks directory
   > git uses, appending its own marker-guarded block without clobbering yours.
   > Retire or migrate any *conflicting* bookkeeping first, and unset a stale
   > `core.hooksPath` if you want the shims in the default `.git/hooks`.
4. **Wire up your agent:**
   - **Plugin (Claude Code):** `/plugin install side-quest` registers the MCP
     server, the `/sq` command, and the guidance skill — nothing to add to your
     `AGENTS.md`.
   - **Manual (other agents, or before the plugin is public):** add a project
     [`.mcp.json`](#mcp-server) that runs `side-quest serve`, and add side-quest's
     guidance to your **`AGENTS.md`**. If the project already has an `AGENTS.md`,
     append side-quest's block as a new section — **merge, don't overwrite** (the
     block to copy is this repo's [`AGENTS.md`](AGENTS.md)). Optionally add a `/sq`
     command at `.claude/commands/sq.md`.
5. **Restart the agent session** so the MCP server, commands, and `AGENTS.md` load
   — you'll be prompted once to approve a new project MCP server.
6. **Share across machines** — see
   [Sharing quests across machines](#sharing-quests-across-machines).

**`PATH` note:** a bare `side-quest serve` in `.mcp.json` needs `side-quest` on the
launching shell's `PATH` (e.g. `~/go/bin`). A GUI-launched agent may not inherit
your shell `PATH` — use an absolute path to the binary if the server fails to
start.

## MCP server

`side-quest serve` runs a stdio MCP server so any MCP-capable agent can capture,
read, and drive quests. Register it (assumes `side-quest` is on `PATH`):

```json
{ "mcpServers": { "side-quest": { "command": "side-quest", "args": ["serve"] } } }
```

Tools: `quest_new`, `quest_list`, `quest_show`, `quest_set_status`,
`quest_reclassify`, `quest_update`, `quest_note`, `quest_set_current`,
`quest_get_current`, `quest_link_commit`. Responses are neutral JSON. For
agent-facing guidance see [`AGENTS.md`](AGENTS.md) and
[`skills/side-quest/SKILL.md`](skills/side-quest/SKILL.md).

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
