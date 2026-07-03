# Phase 7 Packaging & Distribution — Design

**Date:** 2026-07-03
**Status:** Approved (brainstorm)
**Scope:** Turn the built side-quest tool into an installable Claude Code plugin
and a publish-ready standalone binary, without flipping the GitHub repo public.
Realizes the main design's §17 (plugin packaging), §18 (agent-agnostic AGENTS.md),
and §19 (README). Adds a self-provisioning plugin binary launcher and an automated
cross-platform release pipeline (GoReleaser + GitHub Actions).

Out of scope: making the repo public or announcing it (the author does that
manually, later); building the importer (Phase 6) or a `sync` command (§15) — both
appear only as "planned" stubs in docs; bundling pre-compiled binaries in the repo
tree; CI beyond the release workflow.

## Goal

Someone should be able to install side-quest three ways — a prebuilt release
binary, `go install`, or the Claude Code plugin — and, for the plugin path, have
the native binary provisioned automatically on first use once the project is
public. This phase builds every artifact for that, wires the plugin to
self-provision, and rewrites the README to pitch side-quest as a streamlined,
git-native issue tracker for individuals and small teams. The repo stays private;
everything is staged so a single "make public + push a tag" flips it live.

## Background: what already exists

Verified against the code and Claude Code docs while designing:

- **`.mcp.json`** at repo root currently launches the server with
  `go run ./cmd/side-quest serve` (a dogfood convenience). This same file is what
  ships inside the installed plugin, so it must become production-correct.
- **`cmd/side-quest`** has no version string and no `version` subcommand.
- **`go.mod`** declares **`go 1.25.0`** (the MCP SDK forced the bump in Phase 4).
  The main design's §19 still says "Go ≥1.22" — stale; this phase corrects it.
- **`skills/side-quest/SKILL.md`** exists (Phase 4.5) but is not yet referenced
  from the README or an AGENTS.md (deferred living-docs item #14).
- **No** `.claude-plugin/`, `commands/`, `AGENTS.md`, `LICENSE`, `bin/`,
  `.goreleaser.yaml`, or `.github/` exist yet.
- The main package is at **`./cmd/side-quest`**, not the module root, so the
  correct install command is
  `go install github.com/sharkusk/side-quest/cmd/side-quest@latest`
  (the §19.1 root-path form would fail — corrected here).

### Confirmed Claude Code plugin capabilities (checked against current docs)

- `${CLAUDE_PLUGIN_ROOT}` **is** expanded in a plugin's `.mcp.json`
  (`command`, `args`, `env`); `${CLAUDE_PLUGIN_DATA}` gives a persistent
  per-plugin data dir that survives plugin updates.
- A plugin `bin/` directory is **automatically prepended to the Bash tool's
  `PATH`** while the plugin is enabled. Claude does **not** auto-select by
  OS/arch — a wrapper must do that.
- There is **no install-time lifecycle hook**. `SessionStart` hooks run on first
  session, not on install. (We do not use a hook; see §6 rationale.)
- There is **no** official pattern for distributing native-binary MCP servers.

## Design

### 1. Plugin manifest artifacts

- **`.claude-plugin/plugin.json`** — `name: side-quest`, `displayName`,
  `version: 0.1.0`, `description` (the issue-tracker pitch), `author`
  (Marcus Kellerman), `repository`, `homepage`.
- **`VERSION`** — a plain-text file at the repo root holding just the version
  string (`0.1.0`). It is the single source of truth the shell launcher can read
  with a bare `cat` (parsing `plugin.json` in POSIX `sh` is not portable);
  `plugin.json`'s `version` must match it (verified in tests), and the release tag
  is `v` + its contents.
- **`.claude-plugin/marketplace.json`** — a single-plugin marketplace whose one
  entry points at this repo (`.`), so that after the repo is public
  `/plugin marketplace add sharkusk/side-quest` then `/plugin install side-quest`
  resolves.
- **`.mcp.json`** — changed to launch the binary by name:
  `{ "mcpServers": { "side-quest": { "command": "side-quest", "args": ["serve"] } } }`.
  The bare `side-quest` resolves through the plugin `bin/` that Claude prepends to
  `PATH`, which gives per-OS dispatch (`bin/side-quest` on unix,
  `bin/side-quest.cmd` on Windows) from one static file. See §6 for the Windows
  resolution risk and its fallback.

### 2. The `/sq` command

- **`commands/sq.md`** — the only command shipped in v1 (`list`/`current` are
  already reachable via the MCP tools and CLI; wrapping them is duplication —
  YAGNI). It is a prompt instructing the agent: treat the argument as a newly
  surfaced idea, call the **`quest_new` MCP tool** with a concise one-sentence
  `context` note, confirm tersely, and **do not** derail the current task or set
  the quest current. This mirrors the capture reflex taught by
  `skills/side-quest/SKILL.md`.

### 3. AGENTS.md (agent-agnostic)

- **`AGENTS.md`** at repo root — prose for *any* MCP-capable agent (not
  Claude-specific): when to capture a quest, how to write the context note, the
  `Quest:` / `Completes:` trailer contract, and the current-quest pointer. It
  points to `skills/side-quest/SKILL.md` as the Claude-plugin-flavored version and
  keeps all Claude-specific material out of the core (main design §18). This also
  discharges deferred living-docs item #14.

### 4. Release pipeline (GoReleaser + GitHub Actions)

- **`.goreleaser.yaml`** — one `build` producing the six targets
  (`darwin/arm64`, `darwin/amd64`, `linux/amd64`, `linux/arm64`,
  `windows/amd64`, `windows/arm64`), CGo disabled, `-trimpath`,
  `-ldflags "-s -w -X main.version={{.Version}}"`. Archives are `.tar.gz` (unix)
  and `.zip` (Windows), each bundling `README.md` and `LICENSE`. A `checksums.txt`
  (SHA-256) is emitted — the launcher (§6) verifies against it.
- **`.github/workflows/release.yml`** — triggers on a pushed tag matching `v*`,
  checks out, sets up Go 1.25, and runs `goreleaser release --clean`. Uses the
  workflow's `GITHUB_TOKEN` to publish the GitHub Release.
- **Version plumbing (minimal Go change):** add `var version = "dev"` in
  `cmd/side-quest/main.go`, consumed by a new `version` subcommand (and `--version`
  flag) that prints it. GoReleaser overwrites `version` at build time via ldflags,
  so release binaries self-report their tag; source/`go build` binaries report
  `dev`.

### 5. LICENSE

- **`LICENSE`** — MIT, `Copyright (c) 2026 Marcus Kellerman`. The MIT grant covers
  the *code only*; it does not grant any rights to *Dungeon Crawler Carl* IP. The
  README's "Credits & permissions" note (already required by §19) still governs the
  dcc voice: the shipped pool is original homage, and verbatim lines require Matt
  Dinniman's permission. The two are independent and both stand.

### 6. The self-provisioning binary launcher

The plugin ships thin launchers instead of committing ~50 MB of per-platform
binaries to git (which would also fight GoReleaser, whose artifacts live on
Releases, not in the tree). We deliberately do **not** use a `SessionStart` hook:
a hook fires on every session regardless of whether the server is used and adds a
second, parallel install path to reason about; making the MCP `command` itself the
provisioning point means the binary is fetched exactly when — and only when — the
server is actually launched.

**Files:** `bin/side-quest` (POSIX `sh`) and `bin/side-quest.cmd` (Windows batch).
No compiled binaries are committed.

**Resolution chain (first hit wins):**

1. **Cached** — if `${CLAUDE_PLUGIN_DATA}/bin/side-quest-<version>` exists and is
   executable, `exec` it.
2. **Already on PATH** — if a `side-quest` resolves elsewhere on `PATH` (a dev
   build or `go install` result) and is *not* this launcher itself (compared by
   absolute path, to avoid infinite recursion), `exec` that. This is the path that
   works while the repo is private.
3. **Download** — detect OS and arch (`uname -s` / `uname -m` on unix; `%OS%` /
   `%PROCESSOR_ARCHITECTURE%` on Windows), map to GoReleaser's naming
   (`x86_64`→`amd64`, `arm64`/`aarch64`→`arm64`), fetch
   `side-quest_<version>_<os>_<arch>.<ext>` and `checksums.txt` from the matching
   GitHub Release over HTTPS, verify the archive's SHA-256 against
   `checksums.txt`, extract the binary into the cache dir, then `exec` it.
4. **Fail gracefully** — on any failure (private repo, offline, no release yet,
   checksum mismatch), print a one-line install hint to stderr
   (`go install github.com/sharkusk/side-quest/cmd/side-quest@latest`) and exit
   non-zero. The MCP server simply does not come up; nothing is silently wrong.

**Version-locking:** the launcher reads the version from the root `VERSION` file
(a bare `cat`) and downloads the *matching* tag, so the plugin and the binary never
drift. `plugin.json`'s `version` is kept equal to `VERSION` (test-enforced).

**Known risks, carried into the plan (not resolved here):**

- **Windows exec resolution.** Whether Claude's MCP runner honors `PATHEXT` for a
  bare `side-quest` command (so `bin/side-quest.cmd` is found) is unverified. The
  plan must validate this; the fallback is to reference
  `${CLAUDE_PLUGIN_ROOT}/bin/side-quest` explicitly and/or ship an `.exe` shim.
- **Download path is untestable while private.** Steps 1, 2, and 4 are testable
  now; step 3 (download + checksum) cannot be exercised end-to-end until the repo
  is public and a v0.1.0 Release exists. The plan tests what it can (arch mapping,
  cache hit, PATH passthrough, graceful failure) with the network path stubbed,
  and the spec records that live download is a go-public verification item.

### 7. README rewrite (main design §19, adjusted)

The README is reframed per SQ-0011 and adjusted for what actually ships this phase.

- **Reframe (SQ-0011).** Pitch side-quest as a *streamlined, git-native issue
  tracker for individuals and small teams* — not babelmap's internal
  TODO.md/COMPLETED.md workflow. Lead with the chicken-and-egg hook (an id exists
  before the commit; the commit hash is linked back after). Remove
  babelmap-internal framing.
- **Voice kept a surprise (SQ-0011).** The README does **not** advertise the dcc
  tone. The existing README "Tone" section is **removed** and its content
  relocated to `docs/architecture.md`, so the voice is a first-run surprise. (The
  "Credits & permissions" note stays — it is a legal/attribution notice, not a
  spoiler of the flavor.)
- **§19.1 Installation.** Cover every path with, for each, *where the binary lands*
  and *whether it is on `PATH`*:
  - **Prebuilt binary** from GitHub Releases → user places it; conventions
    `/usr/local/bin` or `~/.local/bin` (unix, `chmod +x`), a `PATH` folder on
    Windows; macOS Gatekeeper quarantine note (`xattr -d com.apple.quarantine`,
    unsigned in v1).
  - **`go install github.com/sharkusk/side-quest/cmd/side-quest@latest`** (needs
    **Go ≥1.25**) → lands in `~/go/bin` (`%USERPROFILE%\go\bin` on Windows), which
    is **not on `PATH` by default** — show the `PATH` export/edit.
  - **Build from source** → `go build -o side-quest ./cmd/side-quest`.
  - **Per-project setup** → `side-quest init` then `side-quest install-hooks`.
  - **Claude plugin** → `/plugin marketplace add sharkusk/side-quest` →
    `/plugin install side-quest`; explain the plugin **auto-provisions** the binary
    on first use once published, and that while unpublished you install it via
    `go install`.
  - **Remote / multi-environment** → the `refs/side-quest/quests` ref is not
    fetched by default; document the fetch refspec (auto-configured by `init`) and
    the manual `git push origin refs/side-quest/quests` to share, noting a `sync`
    command is **planned**.
- **§19.2 Development.** Go ≥1.25; system `git`; `gopkg.in/yaml.v3`; the MCP Go
  SDK; **GoReleaser** as a release-only dev dependency. The `internal/` package
  map + `cli`/`mcp` frontends. Build/test/vet commands. Teaching-quality-comment
  and TDD conventions. The **dev-while-in-use** workflow (released binary on `PATH`
  for production repos; `./side-quest` local build for WIP; never point a WIP
  binary at a live repo). How to cut a release (`git tag v… && git push --tags`).
- **Importer** appears only as a brief **"planned (Phase 6)"** stub.

### 8. Living docs

- **`docs/architecture.md`** — receives the relocated Tone/voice details, and gains
  a short "packaging" note: the plugin layout, the `.mcp.json` → `bin/` launcher
  resolution chain, and the version-stamping mechanism.
- **`README.md`** — rewritten per §7.

## Testing / verification

This phase is mostly declarative artifacts; verification is correspondingly about
validity and shape rather than behavior.

- **Manifest validity:** `plugin.json`, `marketplace.json`, and `.mcp.json` parse
  as JSON and carry their required keys (a small test or a `jq`/`go` check).
- **Release config:** `goreleaser check` passes; `goreleaser build --snapshot
  --clean` produces all six targets locally (proves cross-compilation without
  publishing). Requires GoReleaser installed locally.
- **Version plumbing:** `go test` covers that the `version` command prints the
  `version` var, and that a `-ldflags -X main.version=...` build reports the
  injected value; `side-quest version` and `side-quest --version` both work.
- **Version consistency:** a test asserts `plugin.json`'s `version` equals the
  `VERSION` file contents, so the launcher's download tag can never drift from the
  advertised plugin version.
- **Launcher:** unit-test the arch-mapping and URL construction as pure shell
  logic where feasible; test the cache-hit and PATH-passthrough branches; test
  that a forced-failure prints the install hint and exits non-zero. The live
  download + checksum branch is a **go-public verification item**, recorded, not
  run here.
- **Regression:** `go build ./...`, `go test ./...`, `go vet ./...` stay green.
- **Docs:** README internal links resolve; no reference to the removed Tone
  section remains; the `go install` path and Go-version floor are correct
  everywhere.

## Go-public checklist (deferred, recorded here)

Not executed this phase; the author runs these when flipping the repo public:

1. Make the GitHub repo public.
2. Push a `v0.1.0` tag → the release workflow builds and publishes the six
   archives + `checksums.txt`.
3. Verify `/plugin marketplace add sharkusk/side-quest` → `/plugin install
   side-quest` resolves, and that the launcher's **download path** provisions the
   binary on a machine without it (the one path untestable while private).
4. Confirm the macOS Gatekeeper note is accurate for the unsigned artifacts.

## Out of scope (deferred)

- Making the repo public / announcing (manual, later — see checklist).
- The importer (Phase 6) and a `sync` command (§15) — docs stub them as planned.
- Committing pre-built binaries to the tree (`bin/` ships launchers only).
- Code-signing / notarization of macOS and Windows binaries (documented as a
  known gap; a later concern).
- CI beyond the release workflow (no separate test/lint workflow this phase).
