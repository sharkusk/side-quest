# side-quest README Restructure — Design (SQ-0037)

**Goal:** Reframe the README so it leads with the *accelerated agent workflow* —
the benefit — instead of the storage mechanics, moves deep detail to existing
sub-pages, and adds two diagrams plus screenshot slots.

**Why now:** The current README opens with mechanics (`The problem it solves` →
`Concepts`: git ref, orphan ref, plumbing, CAS) before a reader ever sees why the
workflow is good. A newcomer meets compare-and-swap before they meet the payoff.

## Decisions (locked in brainstorming)

- **Hero = before/after narrative.** Open with a concrete moment: mid-refactor,
  the agent spots a flaky test; old way you derail or forget; with side-quest it's
  "captured as SQ-0042" and you never left the diff. Then: *here's how that works.*
- **Concepts → a light "How it works."** Replace the mechanics section with the
  Capture → Work → Link → Sync loop and one line on git-ref storage. Plumbing,
  CAS, and orphan-ref detail move to `docs/architecture.md` (already there).
- **Diagrams (Mermaid):** the workflow loop and the storage model. NOT the
  two-surface (agent/CLI) diagram.
- **Screenshots:** captioned placeholders the user fills later; nothing blocks on
  them.
- **"Why it's different" sits *after* "How it works"** — the narrative hook
  already delivers the benefit, so orient the reader on the loop first, then land
  the "no other tracker closes this loop" point.

## New outline

```
# side-quest
  - Narrative hook (before/after)
  - One-line what-it-is + install one-liner
  - [Diagram: workflow loop]

## How it works
  - The 4-step loop: Capture -> Work -> Link -> Sync (tight prose)
  - [Diagram: storage model — main history vs refs/side-quest, hook writes hash back]
  - One line: quests live on a dedicated git ref, off your history -> architecture.md

## Why it's different   (2–3 sentences)
  - The quest<->commit loop other trackers can't close; deep version -> architecture.md

## Quickstart
  - Lead with the Claude Code plugin path (`/plugin install side-quest` —
    auto-provisions the binary), then `onboard` + first capture
  - Alternate/manual install (any MCP agent, go install) -> install.md,
    plugin.md, manual-setup.md

## Using it
  - The command / MCP-tool surface (from today's "Usage", trimmed)
  - [Screenshot placeholders: a capture, a `list`, a `show`]

## Working with agents / others   (kept, condensed)

## Development   (kept)
```

## What moves out

| Leaves the README | Lands in |
|---|---|
| orphan ref / plumbing / CAS detail | `docs/architecture.md` (already present) |
| the full commit chicken-and-egg explanation | `docs/architecture.md` |
| install variants, refspec-by-hand, plugin specifics | `docs/install.md`, `docs/manual-setup.md`, `docs/plugin.md` |

Content is *relocated or condensed with a link*, never dropped. If a detail the
README currently carries has no home in a sub-page, it stays (condensed) rather
than being lost.

## Diagrams (Mermaid, rendered by GitHub)

1. **Workflow loop** — `Capture -> Work -> Link -> Sync` as a cycle, anchoring
   "How it works."
2. **Storage model** — `main` history (A-B-C) alongside the `refs/side-quest`
   ref, with the post-commit hook writing the commit hash back onto the quest ref.

## Screenshots

Placeholders use real Markdown image syntax with descriptive alt text, a
`docs/img/<name>.png` path, and a `<!-- TODO: real terminal screenshot -->`
marker so the intent is unmistakable and the user can drop PNGs in later. Missing
images degrade to alt text; nothing in CI or tests depends on the files existing.

## Constraints / invariants (must survive the rewrite)

- `internal/packaging/manifests_test.go: TestReadmeReframedAndToneRemoved` — the
  README must NOT contain a `## Tone` section, the string "Dungeon Crawler", or a
  "Credits & permissions" heading. The voice/tone stays an unadvertised surprise.
- The corrected `go install` path and the Go ≥ 1.25 floor live in
  `docs/install.md`, not the README (same test asserts this).
- All existing sub-page links must remain valid: `architecture.md`, `install.md`,
  `manual-setup.md`, `plugin.md`, `sync.md`.
- Keep the useful bits that already earn their place: the `sq` alias tip, the
  "quests sync on push" note (→ `sync.md`), the Development section.

## Out of scope

- Producing the actual screenshots (user supplies).
- Restructuring any sub-page beyond receiving relocated content that already
  fits.
- Any code or behavior change — this is a documentation-only restructure.

## Success criteria

- A reader sees the workflow benefit (narrative hook + loop) before any storage
  mechanics.
- Mechanics detail is reachable via one hop to `architecture.md`, not inline.
- Two Mermaid diagrams render on GitHub; screenshot slots are present with alt
  text and TODO markers.
- `go test ./...` stays green (the README invariants above hold).
