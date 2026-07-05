# Plugin CLI Provisioning & Lifecycle — Design Spec

**Date:** 2026-07-04
**Quests:** [[SQ-0064]] (onboard as the single, plugin-aware front door), [[SQ-0065]] (expose the plugin's binary on the user's PATH — `install-cli`/`uninstall-cli` + launcher), [[SQ-0066]] (MCP-driven CLI lifecycle — `cli_*` tools + plugin guidance; no `SessionStart` hook).

## Goal

Make side-quest pleasant across its whole install/update/uninstall lifecycle on **both** distribution paths — the Claude Code **plugin** and the **manual CLI** — without the two stepping on each other. In particular: let a plugin user run `side-quest` from their own terminal (and have their own `git commit`s link) by **reusing the binary the plugin already provisioned**, instead of doing a second global install; and stop `onboard` from creating a duplicate MCP server when the plugin already supplies one.

## Background: how the plugin is wired today

- The plugin (`.claude-plugin/plugin.json`, name `side-quest`) ships: a bundled `.mcp.json` registering the `side-quest` MCP server (`side-quest serve`), the `/sq` command (`commands/sq.md`), the `using-side-quest` skill (`skills/side-quest/SKILL.md`), and a **shell shim** (`bin/side-quest`).
- The shim resolves the native binary in order: (1) cached binary for this version, (2) a real `side-quest` already on PATH, (3) download the matching GitHub release + verify SHA-256, (4) fail with an install hint.
- Per-repo tracking (quest ref + git hooks) is still a separate step; the plugin wires the *agent*, not the *repo*.

## Established Claude Code plugin facts (verified against docs)

These constrain the design and are load-bearing; each was confirmed against `code.claude.com/docs`:

1. **`CLAUDE_PLUGIN_DATA`** is a documented, **persistent** per-plugin directory that **survives updates**: `~/.claude/plugins/data/{id}/`, where `{id}` = `<plugin>-<marketplace>` with non-`[A-Za-z0-9_-]` replaced by `-`. For us: `~/.claude/plugins/data/side-quest-side-quest/`. It is the officially recommended home for provisioned artifacts (binaries, deps, caches).
2. **`CLAUDE_PLUGIN_ROOT`** (the plugin's install dir) is **version-stamped and ephemeral** — "do not write state here."
3. **The cache layout** `~/.claude/plugins/cache/<mkt>/<plugin>/<version>/` is **not a documented stable API** — "do not parse it."
4. **Plugin env vars are only present inside Claude-spawned processes** (MCP server, hooks) — **never** in the user's interactive terminal. The `CLAUDE_PLUGIN_DATA` *path*, however, is a documented deterministic mapping, so a terminal launcher can **reconstruct** it without the env var.
5. **There is no install or uninstall hook.** `SessionStart` is the idiomatic "runs when the plugin is active" event and the recommended provisioning point. On uninstall Claude does a **silent deletion** with no plugin script.
6. **On uninstall (from the last scope) Claude deletes the data dir by default** (`--keep-data` opts out; the `/plugin` UI prompts with the size). So anything under the data dir self-cleans; artifacts **outside** it (e.g. a file on the user's PATH) do not — there is no cleanup mechanism for those.
7. **Hooks are non-interactive** — they can emit a message but cannot prompt-and-wait. Interactive consent comes from either the **agent** (natural-language ask) or **MCP elicitation** (`Elicitation`/`ElicitationResult`).

## Design decisions

**D1 — Command surface: option C.** A dedicated, machine-global `side-quest install-cli` / `side-quest uninstall-cli` pair owns the PATH mechanism. Their core (choose a PATH dir, write/remove the marked launcher, detect it) lives in a shared `internal/cli` package so the **same code** backs both the CLI subcommands and the MCP `cli_*` tools (D5). `onboard` becomes plugin-aware (D6). The CLI-enable is offered by the agent and executed in-process via an MCP tool (D5), not silently forced.

**D2 — Resolve via the data dir, never the cache dir.** The launcher and the shim provision the binary to `$CLAUDE_PLUGIN_DATA/bin/side-quest-<ver>` = `~/.claude/plugins/data/side-quest-side-quest/bin/side-quest-<ver>` — a **stable, documented** directory (fact 1). We do **not** parse the version-stamped cache dir (fact 3). The launcher reconstructs the data-dir path by the documented id mapping (fact 4).

**D3 — One binary location, written only by Claude-side code.** The binary lives only in the data dir, provisioned **exclusively** by the plugin's MCP server as it starts (`bin/side-quest serve` runs the download shim) — which runs in Claude's environment, where `CLAUDE_PLUGIN_DATA` is set and writing there is legitimate. The terminal launcher is **read-only**: it never downloads and never writes into the data dir. Consequences: one copy shared by agent and terminal; on plugin uninstall Claude wipes it automatically (fact 6), leaving only the launcher for us to remove; and no terminal process ever reaches into a Claude-managed directory to write.

**D4 — Self-healing, read-only runtime-resolving launcher.** `install-cli` installs a small **fixed** launcher (not a versioned symlink) that is a *pure resolver* — it **never downloads or provisions**. If the plugin installed correctly the binary is already in the data dir (the MCP server provisioned it on startup, D5), so a download path in the launcher would be dead weight and would duplicate the shim's logic. On each run it resolves:
1. newest `~/.claude/plugins/data/side-quest-side-quest/bin/side-quest-*` present → `exec` (the normal path — reuse the plugin's binary),
2. else the **data dir exists but has no binary** ⇒ the plugin is installed but not yet provisioned (no session has started the MCP server, or provisioning failed — offline / no published release) → print *"side-quest binary not found — open a Claude Code session to finish setup"*; do **not** download,
3. else the **data dir is absent** ⇒ plugin uninstalled (fact 6). The binary is gone, so `side-quest uninstall-cli` (a *binary* subcommand) **cannot run** — the only `side-quest` left on PATH is this launcher, which would just re-hit case 3. So the launcher **cleans up itself**: it prints its own absolute path (`$0` canonicalized) and either
   - if invoked interactively (stdin is a TTY), **offers to self-delete**: *"the side-quest plugin is no longer installed; remove this launcher at `<path>` now? [y/N]"* → on `y`, `rm "$0"` (and its `.cmd` sibling);
   - if non-interactive, states it is inert and **safe to delete**: *"the side-quest plugin is gone; this launcher is inert — safe to remove: `rm <path>`"*.

   It never silently self-deletes. (**Windows caveat:** a running `.cmd` can't reliably delete itself, so on Windows prefer the print-the-path form and let the user remove it.)

Because it's a fixed file resolving dynamically, a plugin **update** is picked up automatically — no re-run needed. On Windows the launcher is a copied `side-quest.cmd` (symlinks are admin-gated).

**D5 — Consent via MCP tools + plugin guidance, not a hook and not a silent write.** There is **no `SessionStart` hook**. Provisioning is not the hook's job — the MCP server already provisions on startup (D3), every session, refreshing when the bundled VERSION changes. Enabling the CLI is instead exposed as four MCP tools backed by the `internal/cli` core (D1):

- **`cli_status`** — read-only: is a marked launcher on PATH (`installed`), and has the offer been made (`offered`, from the sentinel below)?
- **`cli_install`** — writes the launcher in-process (the server *is* the provisioned binary, with `CLAUDE_PLUGIN_DATA` set), and marks the offer made. This is the clean answer to the chicken-and-egg problem: no `side-quest` need be on PATH to enable the CLI, and it can be re-run any time to **re-enable** after the launcher is deleted.
- **`cli_uninstall`** — removes the marked launcher(s) (D8).
- **`cli_dismiss`** — records a decline (writes the sentinel) so the offer is not repeated.

Discovery is a **plugin-only Instructions addendum**: `guidance.Core` stays the tight cross-agent brief (AGENTS.md untouched), but when `CLAUDE_PLUGIN_DATA` is set the server appends a `guidance.Plugin` block to its initialize-time instructions telling the agent to, early in a session, call `cli_status` and — if `installed:false` and `offered:false` — offer once (*"Want me to put `side-quest` on your PATH for terminal use?"*), then `cli_install` on yes or `cli_dismiss` on no. The sentinel `$CLAUDE_PLUGIN_DATA/.cli-offered` lives in the data dir **on purpose**: Claude deletes that dir on uninstall (fact 6), so it self-cleans — no orphaned state — and a later **reinstall re-offers the CLI**, treating it as a fresh install. (Trade-off, accepted: with no hook the proactive offer and the remembered decline depend on the agent following the Instructions addendum rather than a guaranteed injected nudge; the win is zero hook subsystem and full cross-platform parity, since everything runs in the server.)

**D6 — `onboard` becomes plugin-aware ([[SQ-0064]]).** When `onboard` detects the plugin (its own binary resolves under `~/.claude/plugins/`, or `CLAUDE_PLUGIN_DATA` is set), it **silently skips writing the project `.mcp.json`** (the plugin already registers the server) rather than creating a second identically-named server. It says **nothing** about the skip — `.mcp.json` is internal plumbing, and a "skipping .mcp.json" message would only confuse an end user who doesn't know the file exists. `onboard` just does the right thing quietly. The non-plugin path is unchanged: `onboard` writes `.mcp.json` as today.

**D7 — `install-cli` placement is convention-first, with a notice.** `install-cli` places the launcher in the first **writable, on-PATH** dir among `$XDG_BIN_HOME`, `~/.local/bin`, `~/bin`, `~/go/bin`; failing that it targets the conventional `~/.local/bin` (creating it) and **notifies** the user to ensure it's on PATH. Rationale: the MCP server (which runs `cli_install`) — or a GUI-launched Claude — may see a different `$PATH` than the user's login shell (fact 4 + the known GUI-PATH gotcha), so we don't over-trust a probe — convention + a clear message is robust across launch methods. `install-cli` never clobbers a `side-quest` that lacks our marker (D8).

**D8 — Marked launcher, minimal artifacts, removal in two places.** The launcher embeds a recognizable marker (e.g. `# side-quest-launcher`). `install-cli` writes **no shell-profile edits** and no state file beyond the launcher itself, so removal = "delete one marked file." Two removal paths, because the binary isn't always available:
- **`side-quest uninstall-cli` (binary subcommand)** — the deliberate path *while the plugin is still installed*: the real binary finds marked `side-quest`/`side-quest.cmd` files on PATH and removes them; refuses and warns on an unmarked file (e.g. the user's own build).
- **Launcher self-removal** — the fallback for *after the plugin is gone* (D4.3), when the binary can't run.

Together they cover "remove while installed" and "clean up after the plugin's gone." Everything else (the data-dir binary) is Claude's to clean on uninstall.

**D9 — `onboard` is the single front-door command; `init`/`install-hooks` are demoted.** Because `onboard` is context-aware (D6) and idempotent, it is the one command users and the agent run to set up *or* refresh a repo — in *both* the plugin and manual paths — collapsing the old "plugin users run `init`+`install-hooks`, manual users run `onboard`" split into one instruction. `init` and `install-hooks` remain as lower-level subcommands that `onboard` composes (and that `make dev` still calls in the dev loop), but they move out of the front-door docs into an "advanced" grouping in `--help`. Re-running `onboard` is the "update a repo" path: an existing ref and `.mcp.json` are left as-is; the hooks are refreshed (`vOLD → vNEW`).

## New / changed surfaces

- **`side-quest install-cli`** — new subcommand: probe/choose a PATH dir (D7), write the marked launcher (`side-quest` + `.cmd`), report the location or the "add to PATH" fix.
- **`side-quest uninstall-cli`** — new subcommand: remove the marked launcher(s) while the plugin is present; no-op with a friendly message if absent; refuse unmarked files (D8). (The plugin-gone case is handled by the launcher's own self-removal, D4.3 — not this subcommand.)
- **launcher installed by `install-cli`** — a pure, **read-only** resolver of the data-dir binary (D4). Simpler than the plugin shim: **no download/checksum path** (provisioning is the MCP server's job, D3). Ships as a small `side-quest` shell script plus a `side-quest.cmd` for Windows.
- **`internal/cli`** — the shared core: PATH-dir selection, launcher marker, write/remove/detect the launcher. Backs both the CLI subcommands and the MCP `cli_*` tools.
- **MCP `cli_*` tools** (`cli_status`, `cli_install`, `cli_uninstall`, `cli_dismiss`) — enable/disable/query the terminal CLI in-process (D5). New in `internal/mcp`; take the tool count from 12 to 16.
- **`guidance.Plugin` + Instructions addendum** — the plugin-only lifecycle guidance the server appends to its instructions when `CLAUDE_PLUGIN_DATA` is set (D5). Drives the first-run CLI offer; `guidance.Core` (and AGENTS.md) is unchanged.
- **`onboard`** — the single front-door command: plugin detection + skip/write `.mcp.json` accordingly (D6), idempotent setup-or-refresh (D9). `init`/`install-hooks` demoted to advanced subcommands it composes.
- **Agent guidance (MCP `instructions` / skill)** — the offers: enable-CLI (machine, once, via `cli_*`), set-up-repo (per repo), refresh-hooks (post-update).

## User experience — the lifecycle matrix

### Claude Code (plugin) path

**1. Installing as a plugin.**
`/plugin marketplace add sharkusk/side-quest` → `/plugin install side-quest@side-quest` (pick scope: user / project=collab / local=repo-specific) → restart. Result: MCP server registered (from the bundled `.mcp.json`), `side-quest:sq` command + `side-quest:using-side-quest` skill available; the MCP server provisions the binary into the data dir as it starts. **No PATH changes, no repo setup yet.**

**2. Enabling the CLI on first use.**
The server's plugin Instructions addendum tells the agent to check `cli_status`; on a fresh install it reports `installed:false, offered:false`, so the agent offers to put `side-quest` on your PATH. On yes it calls the **`cli_install`** MCP tool (D5/D7) — writing `~/.local/bin/side-quest` (+ `.cmd` on Windows) in-process and reporting the path (or telling you to add a dir to PATH). Now `side-quest …` works in your terminal, and your **own** terminal commits fire the hooks. On no the agent calls `cli_dismiss` and the offer isn't repeated.

**3. Activating a new repo.**
Working in a repo that isn't tracked (no `refs/side-quest/*`), the agent offers to set it up and, on yes, runs **`side-quest onboard`**. Because the plugin is detected, `onboard` does `init` + `install-hooks` and **skips `.mcp.json`** (the plugin already supplies the server; D6/D9). Repo is now tracked; agent commits and — thanks to step 2 — your terminal commits both link. (Same single command as the manual path's step 2 — `onboard` adapts to context.)

**4. Updating the plugin.**
`/plugin marketplace update side-quest`. On the next session the MCP server starts from the new VERSION and re-provisions the new binary into the data dir. The CLI launcher is self-healing (D4) → it resolves the new version automatically; **no re-run needed**. Old cache dirs are Claude's to clean.

**5. Updating a repo (after a plugin update).**
Nothing is required — the repo's hooks are PATH-relative shims (SQ-0058) and keep working against the new binary. If the shim *text* improved in the new version, the agent may **offer** to re-run **`side-quest onboard`** (idempotent — leaves ref/`.mcp.json` as-is, refreshes the hooks `vOLD → vNEW`); low-stakes. Quest data (the ref) needs no update.

*(Uninstall, for completeness:* `/plugin uninstall` deletes the plugin + the data dir (binary included) by default. The one leftover is the PATH launcher. Two ways to clear it: run `side-quest uninstall-cli` **before** uninstalling (while the binary still exists), or — after uninstalling — the launcher detects the plugin is gone on its next run and offers to self-delete / prints its path as safe to remove (D4.3). Since the binary is gone post-uninstall, `side-quest uninstall-cli` is *not* available then — that's why the launcher self-handles.)*

### Manual CLI path (any MCP agent — and Claude Code users who want full control)

This path suits agents other than Claude Code **and** Claude Code users who prefer explicit control — their own binary on `PATH`, an explicit project `.mcp.json`, and (optionally) a managed `AGENTS.md` block — instead of the plugin's automation. It's a deliberate full-control alternative, not a lesser one.

**1. Installing the CLI.**
Put the binary on your PATH via a prebuilt release, `go install github.com/sharkusk/side-quest/cmd/side-quest@latest`, or build from source (`docs/install.md`). Deliberate, global; no plugin, no auto-provision, no `install-cli` needed (you placed it yourself).

**2. Per-repo activation (MCP & AGENTS.md).**
In each repo: `side-quest onboard` — creates the quest ref, installs the git hooks, and **writes the project `.mcp.json`** (registers `side-quest serve` for your agent). Add `--agents-md` to also merge the guidance block into `AGENTS.md` (marker-guarded, appended — never overwriting your content). Restart the agent session so the MCP server loads. Here — no plugin detected — `onboard` writes `.mcp.json`; it's the **same single command** as the plugin path's repo activation (step 3), just adapting to context (D9).

*Note on `/sq`:* the `/sq` slash command is **Claude-Code-specific** and is **not** installed by `onboard` — it's a convenience the *plugin* provides. On this path the capture reflex still works: the MCP server carries the guidance (its `instructions` field, delivered to any MCP client including Claude Code) and `--agents-md` reinforces it in `AGENTS.md`, so the agent captures via `quest_new` without needing a slash command. A Claude Code user on this path who specifically wants the `/sq` shortcut can copy `commands/sq.md` into `.claude/commands/` themselves (or just install the plugin for that one convenience).

**3. Updating the CLI.**
Replace the binary on your PATH (`go install …@latest`, or drop in a new release). No data dir or launcher is involved; it's entirely user-driven.

**4. Updating a repo (after a CLI update).**
Re-run **`side-quest onboard`** (idempotent — leaves an existing ref/`.mcp.json` untouched, refreshes the hooks `vOLD → vNEW`) to bring the version-stamped shims current. As in the plugin path, the shims are PATH-relative and keep working even if you don't; the refresh only updates their text.

## Open questions

- **O1 — resolved:** on detecting the plugin is gone, the launcher **self-handles** (it must — the binary's `uninstall-cli` is gone): offer self-delete when interactive, else print the path as safe-to-delete (D4.3). Remaining nuance is the Windows self-delete fragility (prefer print-the-path there).
- **O2 — resolved: CLI-enable channel is an MCP tool** (`cli_install`), offered by the agent under a plugin-only Instructions addendum (D5). Chosen over both a shell-out to `install-cli` (chicken-and-egg: no `side-quest` on PATH) and MCP elicitation: the tool runs in-process, is cleaner, and doubles as an on-demand **re-enable** if the launcher is later deleted.
- **O3 — obsolete:** there is no `SessionStart` hook, so there is no hook output channel to choose. Discovery is the server's initialize-time Instructions (the plugin addendum).
- **O4 — Windows PATH-dir selection** specifics (which user dir is reliably on `Path`) — confirm during the Windows pass.
- **O5 — `/sq` on the manual path:** should `onboard` (or a small subcommand) optionally install the Claude-Code `/sq` command for a Claude Code user who chose the manual path, or is "copy it yourself / use the plugin" fine? Leaning: leave it manual — the MCP guidance already delivers the capture reflex; `/sq` is a minor convenience the plugin covers.

## Out of scope / quest mapping

- **[[SQ-0064]]** → D6 (onboard plugin-awareness) + D9 (onboard as the single front door; `init`/`install-hooks` demoted) + their UX (repo activation/update in both paths).
- **[[SQ-0065]]** → D1–D4, D7, D8 (`install-cli`/`uninstall-cli`, self-healing launcher) + plugin path step 2.
- **[[SQ-0066]]** → the MCP CLI lifecycle: D5 (the `cli_*` tools + the plugin Instructions addendum that drives the first-run offer), building on SQ-0065's `internal/cli` core. Touches `internal/mcp`, `internal/guidance`, and the skill — distinct from the CLI mechanism itself.
- Not addressed here: publishing/GoReleaser (SQ-0055), the `bin/side-quest.cmd` shim's existing Windows behavior beyond `install-cli`.

## Testing considerations

- Unit: launcher resolution order (data-dir hit → exec / data-dir-empty → "finish setup" notice, **no download** / data-dir-absent → self-removal warn); marker detection in `internal/cli` (write/remove/detect); `install-cli` PATH-dir selection given a synthetic `$PATH`; the `cli_*` MCP tools (`cli_status` reports installed/offered; `cli_install` writes a marked launcher + the sentinel; `cli_uninstall` removes it; `cli_dismiss` sets the sentinel) over an in-memory transport with a controlled `$HOME`/`$PATH`; the server appends the `guidance.Plugin` addendum to its instructions only when `CLAUDE_PLUGIN_DATA` is set; `onboard` plugin-detection branch (skips `.mcp.json` when `CLAUDE_PLUGIN_DATA`/plugin path present).
- End-to-end: `install-cli` then invoke `side-quest` from a shell whose PATH includes the chosen dir; `uninstall-cli` refuses an unmarked `side-quest`; `onboard` under a simulated plugin env writes no `.mcp.json`.
- Cross-platform: exercise the Windows `.cmd` launcher path (or at least unit-test its generation).
