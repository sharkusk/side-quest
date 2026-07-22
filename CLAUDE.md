# CLAUDE.md — side-quest

Orientation for an agent working on this repo. Keep it short; the deep, always-current
detail lives in [`docs/architecture.md`](docs/architecture.md) — read that before any
non-trivial change to storage, hooks, sync, or the CLI/MCP surface.

## What this is

A git-native quest (issue/task) tracker: a single Go binary that is **both** a CLI and a
stdio MCP server (`side-quest serve`), plus a Claude Code plugin. Quests live on a git
**orphan ref** (`refs/side-quest/quests`) — never as tracked files in your working tree —
so they travel with a clone/push and version alongside your code without cluttering it.

## Hard invariants — do not break

1. **Never touch the user's working tree, real index, or branches.** All writes build a
   commit on `refs/side-quest/*` via a scratch `GIT_INDEX_FILE`. This is the invariant the
   whole design rests on; there's a test (`TestCreateDoesNotTouchWorkingTree`) guarding it.
2. **Every mutation is CAS-guarded** and retries only on a genuine lost race
   (`cannot lock ref`); all other git errors surface, never silently retried.
3. **The quest id is the filename** (`quests/SQ-0001.md` → `SQ-0001`) — never serialized
   into the file. No second source of truth.
4. **git always runs `LC_ALL=C`** so stderr parsing is locale-independent.

The full invariant list and the reasoning behind each is in `docs/architecture.md` →
Invariants. When in doubt, that document is authoritative over this one.

## Build, test, run

Requires **Go ≥ 1.25** and the system `git` binary (≥ 2.13; invoked as a subprocess).

```sh
make build      # ./side-quest from HEAD (gitignored)
make install    # install HEAD to $GOBIN — the binary the hooks + dogfood MCP server use
make test       # go test ./...
make vet        # go vet ./...
make dev        # dogfood side-quest on itself: install + re-point hook shims + link /sq
```

Always run `make test` and `make vet` (and `gofmt -l internal/` — should print nothing)
before considering a change done. `go test ./... -race` for concurrency-touching work.

**Dogfood loop:** after changing code, `make install` then **restart the MCP server** so it
reloads HEAD — a running `serve` keeps the old binary until restarted (`server_info` /
`side-quest version` reveal a stale build).

## How we work here (conventions)

- **This repo tracks its own work with side-quest.** Real changes get a quest; capture
  tangents with `new` instead of derailing. Quest ids are **sequential** (`SQ-0131`, …) and
  monotonic — never reuse or renumber.
- **Link every commit to its quest** via a trailer: `Quest: SQ-0001` (work started →
  `partial`), `Confirm: SQ-0001` (parks for the user's sign-off), `Completes: SQ-0001`
  (closes it), or `Quest: none` for a genuine chore. Setting a quest current auto-injects
  the trailer via the git hooks.
- **Documentation is part of the change.** `docs/architecture.md` and the README are
  *living* docs — update them in the **same** change that alters the behavior they describe.
  The dated files under `docs/superpowers/{specs,plans}/` are point-in-time history —
  **do not** edit them to match later code. (See [`CONTRIBUTING.md`](CONTRIBUTING.md).)
- **Style:** small single-purpose files; teaching-quality doc comments (assume a C/Python
  reader newer to Go); machine/`--json` output stays neutral — never route it through the
  voice layer.

## Releases

`VERSION` is the single source of truth; `.claude-plugin/plugin.json`'s `version` must match
it (test-enforced) and the release tag is `v` + `VERSION`. Releases are cut **manually** via
a tag-triggered GoReleaser workflow — never tag or publish without an explicit go-ahead, and
defer the version bump to the release step so unrelated work doesn't drag a bump along.

## Where things live

- `cmd/side-quest/` — CLI + git-hook entrypoints (`main.go` routes; `cli.go` handlers;
  `render.go` output; `serve.go` starts the MCP server).
- `internal/store/` — the orphan-ref CRUD + the `mutate`/`buildCommit`/CAS machinery.
- `internal/quest/`, `internal/config/` — pure models (quest Markdown; on-ref `_config.yaml`).
- `internal/merge/` — the pure three-way sync merge; `internal/store/sync.go` does its git.
- `internal/mcp/` — the MCP tool surface; `internal/voice/` — tone/flavor (plain + dcc).
- `internal/guidance/` — the agent brief the MCP server ships as `instructions`.

Package responsibilities and the git-version floor table are in `docs/architecture.md`.
