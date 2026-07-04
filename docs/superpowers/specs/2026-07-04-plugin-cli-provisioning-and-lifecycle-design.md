# Plugin CLI Provisioning & Lifecycle — Design Spec

**Date:** 2026-07-04
**Quests:** [[SQ-0064]] (onboard as the single, plugin-aware front door), [[SQ-0065]] (expose the plugin's binary on the user's PATH — `link`/`unlink` + launcher), [[SQ-0066]] (agent-guidance lifecycle — `SessionStart` hook + first-run consent offers).

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

**D1 — Command surface: option C.** A dedicated, machine-global `side-quest link` / `side-quest unlink` owns the PATH mechanism. `onboard` becomes plugin-aware (D6). The CLI-enable is offered by the agent (D5), not silently forced.

**D2 — Resolve via the data dir, never the cache dir.** The launcher and the shim provision the binary to `$CLAUDE_PLUGIN_DATA/bin/side-quest-<ver>` = `~/.claude/plugins/data/side-quest-side-quest/bin/side-quest-<ver>` — a **stable, documented** directory (fact 1). We do **not** parse the version-stamped cache dir (fact 3). The launcher reconstructs the data-dir path by the documented id mapping (fact 4).

**D3 — One binary location, written only by Claude-side code.** The binary lives only in the data dir, provisioned **exclusively** by the `SessionStart` hook / MCP shim — which run in Claude's environment, where `CLAUDE_PLUGIN_DATA` is set and writing there is legitimate. The terminal launcher is **read-only**: it never downloads and never writes into the data dir. Consequences: one copy shared by agent and terminal; on plugin uninstall Claude wipes it automatically (fact 6), leaving only the launcher for us to remove; and no terminal process ever reaches into a Claude-managed directory to write.

**D4 — Self-healing, read-only runtime-resolving launcher.** `link` installs a small **fixed** launcher (not a versioned symlink) that is a *pure resolver* — it **never downloads or provisions**. If the plugin installed correctly the binary is already in the data dir (the `SessionStart` hook put it there, D5), so a download path in the launcher would be dead weight and would duplicate the hook's logic. On each run it resolves:
1. newest `~/.claude/plugins/data/side-quest-side-quest/bin/side-quest-*` present → `exec` (the normal path — reuse the plugin's binary),
2. else the **data dir exists but has no binary** ⇒ the plugin is installed but not yet provisioned (no session has run the hook, or provisioning failed — offline / no published release) → print *"side-quest binary not found — open a Claude Code session to finish setup"*; do **not** download,
3. else the **data dir is absent** ⇒ plugin uninstalled (fact 6). The binary is gone, so `side-quest unlink` (a *binary* subcommand) **cannot run** — the only `side-quest` left on PATH is this launcher, which would just re-hit case 3. So the launcher **cleans up itself**: it prints its own absolute path (`$0` canonicalized) and either
   - if invoked interactively (stdin is a TTY), **offers to self-delete**: *"the side-quest plugin is no longer installed; remove this launcher at `<path>` now? [y/N]"* → on `y`, `rm "$0"` (and its `.cmd` sibling);
   - if non-interactive, states it is inert and **safe to delete**: *"the side-quest plugin is gone; this launcher is inert — safe to remove: `rm <path>`"*.

   It never silently self-deletes. (**Windows caveat:** a running `.cmd` can't reliably delete itself, so on Windows prefer the print-the-path form and let the user remove it.)

Because it's a fixed file resolving dynamically, a plugin **update** is picked up automatically — no re-link. On Windows the launcher is a copied `side-quest.cmd` (symlinks are admin-gated).

**D5 — Consent via a first-run agent offer, not a silent write.** A `SessionStart` hook checks a sentinel (`$CLAUDE_PLUGIN_DATA/.cli-offered`); if absent it (a) provisions/refreshes the binary (compare bundled VERSION vs the data-dir copy, re-provision on change — the documented pattern) and (b) injects a one-time nudge so the **agent asks**: *"Want me to put `side-quest` on your PATH for terminal use?"* On yes → agent runs `side-quest link`. Writes the sentinel so it asks once; a decline is remembered. The sentinel lives in `$CLAUDE_PLUGIN_DATA` **on purpose**: Claude deletes that dir on uninstall (fact 6), so the sentinel self-cleans with everything else — no orphaned state — and a later **reinstall re-offers the CLI**, treating it as a fresh install (a deliberate, reasonable consequence rather than a decline remembered forever across uninstall).

**D6 — `onboard` becomes plugin-aware ([[SQ-0064]]).** When `onboard` detects the plugin (its own binary resolves under `~/.claude/plugins/`, or `CLAUDE_PLUGIN_DATA` is set), it **silently skips writing the project `.mcp.json`** (the plugin already registers the server) rather than creating a second identically-named server. It says **nothing** about the skip — `.mcp.json` is internal plumbing, and a "skipping .mcp.json" message would only confuse an end user who doesn't know the file exists. `onboard` just does the right thing quietly. The non-plugin path is unchanged: `onboard` writes `.mcp.json` as today.

**D7 — `link` placement is convention-first, with a notice.** `link` places the launcher in the first **writable, on-PATH** dir among `$XDG_BIN_HOME`, `~/.local/bin`, `~/bin`, `~/go/bin`; failing that it targets the conventional `~/.local/bin` (creating it) and **notifies** the user to ensure it's on PATH. Rationale: a `SessionStart` hook (or GUI-launched Claude) may see a different `$PATH` than the user's login shell (fact 4 + the known GUI-PATH gotcha), so we don't over-trust a probe — convention + a clear message is robust across launch methods. `link` never clobbers a `side-quest` that lacks our marker (D8).

**D8 — Marked launcher, minimal artifacts, removal in two places.** The launcher embeds a recognizable marker (e.g. `# side-quest-launcher`). `link` writes **no shell-profile edits** and no state file beyond the launcher itself, so removal = "delete one marked file." Two removal paths, because the binary isn't always available:
- **`side-quest unlink` (binary subcommand)** — the deliberate path *while the plugin is still installed*: the real binary finds marked `side-quest`/`side-quest.cmd` files on PATH and removes them; refuses and warns on an unmarked file (e.g. the user's own build).
- **Launcher self-removal** — the fallback for *after the plugin is gone* (D4.3), when the binary can't run.

Together they cover "remove while installed" and "clean up after the plugin's gone." Everything else (the data-dir binary) is Claude's to clean on uninstall.

**D9 — `onboard` is the single front-door command; `init`/`install-hooks` are demoted.** Because `onboard` is context-aware (D6) and idempotent, it is the one command users and the agent run to set up *or* refresh a repo — in *both* the plugin and manual paths — collapsing the old "plugin users run `init`+`install-hooks`, manual users run `onboard`" split into one instruction. `init` and `install-hooks` remain as lower-level subcommands that `onboard` composes (and that `make dev` still calls in the dev loop), but they move out of the front-door docs into an "advanced" grouping in `--help`. Re-running `onboard` is the "update a repo" path: an existing ref and `.mcp.json` are left as-is; the hooks are refreshed (`vOLD → vNEW`).

## New / changed surfaces

- **`side-quest link`** — new subcommand: probe/choose a PATH dir (D7), write the marked launcher (`side-quest` + `.cmd`), report the location or the "add to PATH" fix.
- **`side-quest unlink`** — new subcommand: remove the marked launcher(s) while the plugin is present; no-op with a friendly message if absent; refuse unmarked files (D8). (The plugin-gone case is handled by the launcher's own self-removal, D4.3 — not this subcommand.)
- **launcher installed by `link`** — a pure, **read-only** resolver of the data-dir binary (D4). Simpler than the plugin shim: **no download/checksum path** (provisioning is the hook's job). Ships as a small `side-quest` shell script plus a `side-quest.cmd` for Windows.
- **`hooks/hooks.json` (`SessionStart`)** — provision/refresh the binary into the data dir and drive the one-time CLI offer (D5). New; the plugin currently ships no hooks.
- **`onboard`** — the single front-door command: plugin detection + skip/write `.mcp.json` accordingly (D6), idempotent setup-or-refresh (D9). `init`/`install-hooks` demoted to advanced subcommands it composes.
- **Agent guidance (MCP `instructions` / skill)** — the offers: enable-CLI (machine, once), set-up-repo (per repo), refresh-hooks (post-update). *Flagged as possibly its own quest.*

## User experience — the lifecycle matrix

### Claude Code (plugin) path

**1. Installing as a plugin.**
`/plugin marketplace add sharkusk/side-quest` → `/plugin install side-quest@side-quest` (pick scope: user / project=collab / local=repo-specific) → restart. Result: MCP server registered (from the bundled `.mcp.json`), `side-quest:sq` command + `side-quest:using-side-quest` skill available; `SessionStart` provisions the binary into the data dir. **No PATH changes, no repo setup yet.**

**2. Enabling the CLI on first use.**
First session after install, the `SessionStart` sentinel is absent → the agent offers to put `side-quest` on your PATH. On yes it runs `side-quest link` (D7): writes `~/.local/bin/side-quest` (+ `.cmd` on Windows), reports the path, or tells you to add a dir to PATH. Now `side-quest …` works in your terminal, and your **own** terminal commits fire the hooks. Declining is remembered.

**3. Activating a new repo.**
Working in a repo that isn't tracked (no `refs/side-quest/*`), the agent offers to set it up and, on yes, runs **`side-quest onboard`**. Because the plugin is detected, `onboard` does `init` + `install-hooks` and **skips `.mcp.json`** (the plugin already supplies the server; D6/D9). Repo is now tracked; agent commits and — thanks to step 2 — your terminal commits both link. (Same single command as the manual path's step 2 — `onboard` adapts to context.)

**4. Updating the plugin.**
`/plugin marketplace update side-quest`. `SessionStart` sees the bundled VERSION changed and re-provisions the new binary into the data dir. The CLI launcher is self-healing (D4) → it resolves the new version automatically; **no re-link**. Old cache dirs are Claude's to clean.

**5. Updating a repo (after a plugin update).**
Nothing is required — the repo's hooks are PATH-relative shims (SQ-0058) and keep working against the new binary. If the shim *text* improved in the new version, the agent may **offer** to re-run **`side-quest onboard`** (idempotent — leaves ref/`.mcp.json` as-is, refreshes the hooks `vOLD → vNEW`); low-stakes. Quest data (the ref) needs no update.

*(Uninstall, for completeness:* `/plugin uninstall` deletes the plugin + the data dir (binary included) by default. The one leftover is the PATH launcher. Two ways to clear it: run `side-quest unlink` **before** uninstalling (while the binary still exists), or — after uninstalling — the launcher detects the plugin is gone on its next run and offers to self-delete / prints its path as safe to remove (D4.3). Since the binary is gone post-uninstall, `side-quest unlink` is *not* available then — that's why the launcher self-handles.)*

### Manual CLI path (any MCP agent — and Claude Code users who want full control)

This path suits agents other than Claude Code **and** Claude Code users who prefer explicit control — their own binary on `PATH`, an explicit project `.mcp.json`, and (optionally) a managed `AGENTS.md` block — instead of the plugin's automation. It's a deliberate full-control alternative, not a lesser one.

**1. Installing the CLI.**
Put the binary on your PATH via a prebuilt release, `go install github.com/sharkusk/side-quest/cmd/side-quest@latest`, or build from source (`docs/install.md`). Deliberate, global; no plugin, no auto-provision, no `link` needed (you placed it yourself).

**2. Per-repo activation (MCP & AGENTS.md).**
In each repo: `side-quest onboard` — creates the quest ref, installs the git hooks, and **writes the project `.mcp.json`** (registers `side-quest serve` for your agent). Add `--agents-md` to also merge the guidance block into `AGENTS.md` (marker-guarded, appended — never overwriting your content). Restart the agent session so the MCP server loads. Here — no plugin detected — `onboard` writes `.mcp.json`; it's the **same single command** as the plugin path's repo activation (step 3), just adapting to context (D9).

*Note on `/sq`:* the `/sq` slash command is **Claude-Code-specific** and is **not** installed by `onboard` — it's a convenience the *plugin* provides. On this path the capture reflex still works: the MCP server carries the guidance (its `instructions` field, delivered to any MCP client including Claude Code) and `--agents-md` reinforces it in `AGENTS.md`, so the agent captures via `quest_new` without needing a slash command. A Claude Code user on this path who specifically wants the `/sq` shortcut can copy `commands/sq.md` into `.claude/commands/` themselves (or just install the plugin for that one convenience).

**3. Updating the CLI.**
Replace the binary on your PATH (`go install …@latest`, or drop in a new release). No data dir or launcher is involved; it's entirely user-driven.

**4. Updating a repo (after a CLI update).**
Re-run **`side-quest onboard`** (idempotent — leaves an existing ref/`.mcp.json` untouched, refreshes the hooks `vOLD → vNEW`) to bring the version-stamped shims current. As in the plugin path, the shims are PATH-relative and keep working even if you don't; the refresh only updates their text.

## Open questions

- **O1 — resolved:** on detecting the plugin is gone, the launcher **self-handles** (it must — the binary's `unlink` is gone): offer self-delete when interactive, else print the path as safe-to-delete (D4.3). Remaining nuance is the Windows self-delete fragility (prefer print-the-path there).
- **O2 — CLI-enable channel:** agent-mediated ask (D5) vs. MCP elicitation. Recommend agent-mediated; revisit if elicitation gives a cleaner one-click UX.
- **O3 — exact `SessionStart` output channel** for surfacing the offer/notice (context injection vs. a user-visible message) — confirm at implementation.
- **O4 — Windows PATH-dir selection** specifics (which user dir is reliably on `Path`) — confirm during the Windows pass.
- **O5 — `/sq` on the manual path:** should `onboard` (or a small subcommand) optionally install the Claude-Code `/sq` command for a Claude Code user who chose the manual path, or is "copy it yourself / use the plugin" fine? Leaning: leave it manual — the MCP guidance already delivers the capture reflex; `/sq` is a minor convenience the plugin covers.

## Out of scope / quest mapping

- **[[SQ-0064]]** → D6 (onboard plugin-awareness) + D9 (onboard as the single front door; `init`/`install-hooks` demoted) + their UX (repo activation/update in both paths).
- **[[SQ-0065]]** → D1–D4, D7, D8 (`link`/`unlink`, self-healing launcher) + plugin path step 2.
- **[[SQ-0066]]** → the agent-guidance lifecycle: D5 (`SessionStart` hook + first-run CLI offer) and the repo-setup / refresh offers; touches MCP `instructions`/skill content and the hook, distinct from the CLI mechanism.
- Not addressed here: publishing/GoReleaser (SQ-0055), the `bin/side-quest.cmd` shim's existing Windows behavior beyond `link`.

## Testing considerations

- Unit: launcher resolution order (data-dir hit → exec / data-dir-empty → "finish setup" notice, **no download** / data-dir-absent → "unlink" warn); marker detection in `unlink`; `link` PATH-dir selection given a synthetic `$PATH`; `onboard` plugin-detection branch (skips `.mcp.json` when `CLAUDE_PLUGIN_DATA`/plugin path present).
- End-to-end: `link` then invoke `side-quest` from a shell whose PATH includes the chosen dir; `unlink` refuses an unmarked `side-quest`; `onboard` under a simulated plugin env writes no `.mcp.json`.
- Cross-platform: exercise the Windows `.cmd` launcher path (or at least unit-test its generation).
