# Shipping a native binary as a Claude Code plugin's MCP server

A field guide to a problem the plugin docs don't cover: your MCP server is a
**compiled, per-OS executable**, not a Node script — how do you get it to launch
reliably on macOS, Linux, and Windows, including a Windows box that has no `node`
anywhere on it?

This is the pattern side-quest uses. It's written to be lifted into any plugin
that ships a native (Go/Rust/C/Zig/…) MCP server. The worked example is
side-quest's, but nothing here is specific to it.

## The constraint that forces the design

A Claude Code plugin registers its MCP server in `.claude-plugin/plugin.json`:

```json
{
  "mcpServers": {
    "my-server": { "command": "…", "args": ["serve"] }
  }
}
```

Four facts about how that `command` is launched box you in, and together they
rule out every obvious approach:

1. **It's spawned with no shell.** `command` is `exec`'d directly, not run
   through `sh -c`. So there is no `PATH` search on a bare name the way a shell
   would do it, no `&&`, no globbing, no shell builtins, no `~` expansion. What
   you write is what gets executed.
2. **There is one `command` for all platforms.** `mcpServers` has no per-OS
   variant — you cannot say "on Windows run X, on macOS run Y." The single
   string has to work everywhere.
3. **You cannot assume `node` (or anything) is on `PATH`.** The common advice —
   ship a Node launcher, `"command": "node"` — assumes a Node runtime is present.
   It often isn't. A Windows machine that installed Claude Code via the native
   installer (`irm https://claude.ai/install.ps1 | iex`) has **no `node` on
   `PATH` at all**. `"command": "node"` there fails to spawn with a generic
   `-32000` MCP error, and there is no shell to fall back through.
4. **You shouldn't commit prebuilt binaries.** A native server means one binary
   per `os/arch` (six targets is typical: darwin/linux/windows × amd64/arm64).
   Committing all of them bloats the repo, and you'd still have to pick the right
   one at launch — which needs logic, which needs a shell you don't have.

Put those together and only one shape survives:

> The MCP `command` must be a **real executable at an absolute path**, and that
> executable has to already be on disk **before** the spawn — because there is no
> shell, no interpreter guarantee, and no per-OS branching to arrange it at spawn
> time.

Which raises the question the rest of this doc answers: if the command is a fixed
absolute path to a binary, **how does the right per-OS binary get to that path?**

## The pattern: three pieces, one fixed path

The trick is to split "run the server" from "make the binary exist," and to have
a third, optional piece for terminal use — all three hard-coded to the **same
absolute path**. That shared path is the entire contract; get it identical in
three places and the system holds together.

```
                    ${CLAUDE_PLUGIN_DATA}/bin/my-server.exe
                                   ▲
              ┌────────────────────┼────────────────────┐
              │                    │                    │
   ①  MCP command            ② SessionStart          ③ terminal launcher
      (plugin.json)             provision hook           (optional, on PATH)
      spawns it                 downloads it             resolves + execs it
```

### ① The MCP command points straight at the provisioned binary

```json
{
  "mcpServers": {
    "my-server": {
      "command": "${CLAUDE_PLUGIN_DATA}/bin/my-server.exe",
      "args": ["serve"]
    }
  }
}
```

- **`${CLAUDE_PLUGIN_DATA}`** is expanded by Claude Code in the `command`/`args`
  fields *and* exported into the spawned process's environment. It resolves to a
  per-plugin, per-machine data directory
  (`~/.claude/plugins/data/<marketplace>-<plugin>` by default) that survives
  plugin updates — the right place for a machine-specific artifact that must not
  live in the git tree.
- **One fixed filename on every OS, `.exe` and all.** Don't try to vary the name
  per platform — you can't (fact #2), and you don't need to. The `.exe`
  extension is meaningful on Windows and simply cosmetic on macOS/Linux, where an
  executable bit is what matters. A single name means the provision hook and the
  launcher can hard-code the same string with no per-OS logic.

### ② The SessionStart hook provisions the binary ahead of the spawn

Claude Code runs plugin **hooks** at defined lifecycle points. `SessionStart`
fires before the model starts working — which is before the MCP server is needed
— so it's the place to guarantee the binary is on disk. Register two arms, one
per interpreter family, each self-selecting by which interpreter exists:

```json
{
  "hooks": {
    "SessionStart": [
      { "hooks": [ { "type": "command",
        "command": "sh \"${CLAUDE_PLUGIN_ROOT}/scripts/provision.sh\"" } ] },
      { "hooks": [ { "type": "command",
        "command": "powershell -NoProfile -ExecutionPolicy Bypass -File \"${CLAUDE_PLUGIN_ROOT}/scripts/provision.ps1\"" } ] }
    ]
  }
}
```

- **`${CLAUDE_PLUGIN_ROOT}`** is the checked-out plugin directory — where your
  `scripts/` ship. (Contrast `${CLAUDE_PLUGIN_DATA}`, the writable per-machine
  data dir the binary lands in.)
- **Two arms, not an if.** On macOS/Linux the `sh` arm runs and the `powershell`
  arm no-ops (no `powershell`); on Windows it's the reverse. Each script is free
  to assume its own platform.

Each provision script does the same four things:

1. Read the target **version** (side-quest ships a `VERSION` file at the plugin
   root; the release tag is `v` + that).
2. If the binary already exists **and** a version marker matches, exit
   immediately — this is what makes the hook cheap to run on every session.
3. Otherwise download the matching release asset
   (`my-server_<version>_<os>_<arch>.{tar.gz,zip}`) and the release
   `checksums.txt`, **verify the SHA-256**, and only on a match extract the
   binary to `${CLAUDE_PLUGIN_DATA}/bin/my-server.exe`.
4. Write the version into a marker file next to the binary.

Two properties make this safe to wire into session start:

- **Idempotent.** The version-marker check (step 2) means the steady-state cost
  is a `stat` and a string compare — no network. It only downloads on first run
  and after a version bump.
- **Non-fatal, always.** A failed download, a checksum mismatch, no network — none
  of it may block the session. The scripts swallow every error and exit 0. If
  provisioning fails, the MCP spawn simply surfaces "binary not found," and the
  next session (or an MCP reconnect) that provisions successfully recovers. A
  broken release must never leave a user unable to start Claude Code. (This is
  also the mechanical reason for the occasional "first connect fails, reconnect
  works": a cold session raced the first-ever download; by the reconnect the
  binary is there.)

See [`scripts/provision.sh`](../scripts/provision.sh) and
[`scripts/provision.ps1`](../scripts/provision.ps1) for the full implementations.
Two non-obvious lessons are baked into them:

- **Reconstruct `${CLAUDE_PLUGIN_DATA}` yourself as a fallback.** It's set inside
  a hook, but writing `${CLAUDE_PLUGIN_DATA:-$HOME/.claude/plugins/data/<marketplace>-<plugin>}`
  (and the PowerShell equivalent) means the script still targets the right path if
  ever run outside Claude — e.g. from a test. The default must exactly equal the
  path in the MCP command and the launcher.
- **Write PowerShell against the oldest runtime you'll meet.** The Windows arm
  uses **pure .NET** — `[System.IO.File]`, `[System.Security.Cryptography.SHA256]`,
  `[System.IO.Compression.ZipFile]`, a `Guid` temp dir — rather than the
  convenient cmdlets. On a real native-installer box, `New-TemporaryFile` and
  `Get-FileHash` raised `CommandNotFoundException`. The .NET types are always
  present; the cmdlets are not.

### ③ The terminal launcher (optional) resolves the same path

The plugin provisions the binary for *its* MCP server, but that location isn't on
the user's shell `PATH` — so out of the box they can't run `my-server` in a
terminal, and neither can the agent's Bash tool. If you want a terminal CLI too,
install a tiny **launcher** on `PATH` that resolves the exact same fixed path and
`exec`s it:

- It **never downloads** — the SessionStart hook already owns provisioning. The
  launcher only resolves-and-execs (side-quest's is
  [`launcher.sh`](../internal/cli/launcher.sh) / [`launcher.cmd`](../internal/cli/launcher.cmd)).
- It targets the **same** `${CLAUDE_PLUGIN_DATA}/bin/my-server.exe`. Three places,
  one path.
- It degrades gracefully: binary present → exec; data dir present but no binary →
  "open a session to finish setup"; data dir gone (plugin uninstalled) → announce
  itself inert and offer to self-remove.

## The fixed path is the whole contract

Everything above reduces to one rule:

> The MCP command, the provision hook, and the launcher must all name the
> **identical** absolute path — same directory, same filename, same extension —
> on every platform.

If they drift, the failure is invisible: the hook provisions a binary the MCP
command never looks at, or the launcher resolves a path nothing ever populated.
There's no error, just a server that won't start. Hard-code the path as a single
known string in all three, and derive the `${CLAUDE_PLUGIN_DATA}` fallback default
the same way in each.

## How a session actually flows

```
Claude Code session starts
        │
        ├─ SessionStart hook runs  ──▶ provision.sh / provision.ps1
        │                                  ├─ marker matches? ─▶ exit 0 (fast path)
        │                                  └─ else: download ▸ verify SHA-256 ▸
        │                                          extract to <data>/bin/my-server.exe ▸
        │                                          stamp marker
        │
        └─ MCP client connects  ──▶ spawn  <data>/bin/my-server.exe serve
                                          ├─ binary present ─▶ server up
                                          └─ absent (provision failed) ─▶ -32000;
                                             next session / reconnect recovers
```

## Building and publishing the assets

The pattern assumes a conventional release layout — the provision scripts are
written to it:

- **One archive per `os/arch`**, named `my-server_<version>_<os>_<arch>.tar.gz`
  (`.zip` for Windows), each containing the single binary. side-quest builds six
  targets with [GoReleaser](https://goreleaser.com); any tool that emits
  per-platform archives plus a checksums file works.
- **A `checksums.txt`** in the release listing the SHA-256 of every asset. This is
  what makes the download trustworthy without shipping signatures — the scripts
  refuse to install a binary whose hash isn't in it.
- **No binaries in git.** The repo carries source, the plugin manifest, and the
  provision scripts; the binaries live only in tagged releases.
- **One source of truth for the version.** side-quest's root `VERSION` file feeds
  the plugin manifest's `version`, the release tag, and the string the provision
  scripts download — a test enforces that `plugin.json`'s version matches
  `VERSION`, so they can't drift.

An escape hatch worth copying: the scripts honor a `MY_SERVER_RELEASE_BASE`
environment variable that overrides the download host. That's what lets an
integration test point provisioning at a local fixture server, and lets an
air-gapped site mirror the assets internally.

## Alternatives, and why they don't work

| Approach | Why it fails |
|---|---|
| `"command": "node"` + a `launch.js` | No `node` on a native-installer Windows box → `-32000`, and no shell to recover. This is the default advice and the trap. |
| `"command": "npx …"` | Same missing-runtime problem, plus a network dependency at every spawn. |
| A shell script or `.cmd` as the `command` | Spawned with no shell, so a script isn't directly executable as a process across platforms; `.cmd` isn't a thing on macOS/Linux anyway. |
| Per-OS `command` in `plugin.json` | Not supported — there's one command for all platforms. |
| Commit all six binaries into the plugin | Repo bloat, and you *still* need shell logic at spawn to select one — which you don't have. |
| Provision at first MCP call instead of a hook | The MCP spawn has nowhere to run download logic; the hook is the only pre-spawn seam. |

## Adapting this to your plugin — checklist

1. Pick the fixed path once: `${CLAUDE_PLUGIN_DATA}/bin/<name>.exe`. Use it
   verbatim in the MCP command, both provision scripts, and any launcher.
2. Point `mcpServers.<name>.command` at that path; put your subcommand in `args`.
3. Add a two-arm `SessionStart` hook (`sh` + `powershell`) invoking
   `scripts/provision.sh` and `scripts/provision.ps1`.
4. Make each script: version-marker fast-path → download asset + `checksums.txt`
   → verify SHA-256 → extract to the fixed path → stamp marker. Swallow every
   error and exit 0.
5. Reconstruct the `${CLAUDE_PLUGIN_DATA}` default inside the scripts, identical
   to the manifest path.
6. On Windows, use pure .NET, not `New-TemporaryFile`/`Get-FileHash`.
7. Release per-`os/arch` archives + a `checksums.txt`; keep binaries out of git;
   single-source the version.
8. (Optional) Ship a PATH launcher that resolves the same path and never
   downloads.

## Related

- [`docs/architecture.md`](architecture.md) → "Packaging & distribution" — how
  this sits in side-quest's overall design.
- [`docs/plugin.md`](plugin.md) — the user-facing install/uninstall story,
  including per-OS cleanup.
- [`scripts/provision.sh`](../scripts/provision.sh),
  [`scripts/provision.ps1`](../scripts/provision.ps1),
  [`internal/cli/launcher.sh`](../internal/cli/launcher.sh),
  [`internal/cli/launcher.cmd`](../internal/cli/launcher.cmd) — the four files
  that share the fixed path.
