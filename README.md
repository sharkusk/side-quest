# side-quest

A lightweight, project-specific quest/task tracker for capturing the *side quests* — the
new ideas, follow-ups, and research tangents that occur to you while working on something
else — without derailing your current session, and with a clean two-way link between quests
and the git commits that address them.

> **Status: pre-implementation.** The design is complete; the tool is not built yet.
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

A full README (quickstart, CLI/MCP reference, plugin install, configuration) ships with the
implementation.
