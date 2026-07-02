# Contributing to side-quest

## Documentation is part of the change

side-quest keeps two kinds of docs, and they have different rules:

- **Living docs** — [`docs/architecture.md`](docs/architecture.md) and the README. These
  describe how the tool works **now** and MUST be updated in the *same commit/PR* as any change
  to the behavior they describe (the storage model, CAS, the mutation flow, id allocation, the
  CLI/MCP surface, config keys, etc.).
- **Design records** — the dated files under `docs/superpowers/specs/` and
  `docs/superpowers/plans/`. These are point-in-time snapshots of a decision. **Do not edit
  them to match later code** — they are history. If a decision changes, write a new record.

**The rule:** if you change public behavior or the architecture, update `docs/architecture.md`
(and the README concepts/glossary if the change touches them) in the same change. A reviewer
should be able to read the living docs and trust they match the code.

**git version floor:** side-quest shells out to `git`, so its minimum git version is set by the
newest git feature it uses (currently **2.13**). If you add or change a `git` command/flag,
check the version it was introduced in; if it exceeds the current floor, **raise the floor and
add a row to the version-sensitive table in [`docs/architecture.md`](docs/architecture.md) →
Dependencies** (and update the README/spec dependency lines) in the same change.

### The doc-freshness reminder (warn-only hook)

This repo ships a **warn-only** `pre-commit` hook that reminds you when you've changed Go
source under `internal/` without touching any docs. It never blocks a commit — it just nudges,
in keeping with side-quest's "assisted, not enforced" philosophy. Enable it once per clone:

```sh
git config core.hooksPath .githooks
```

(That points git at the tracked `.githooks/` directory. It's opt-in so it never surprises a
new contributor; CI is not required for it.)

If a commit legitimately changes code without needing a doc update (a pure refactor, a test-only
change), just proceed — the hook is a reminder, not a gate.

## Development

Dependencies:

- **Go ≥ 1.22**
- **`git` ≥ 2.13** (invoked as a subprocess; floor set by `rev-parse --absolute-git-dir` —
  see [`docs/architecture.md`](docs/architecture.md) → Dependencies for the full table)
- **`gopkg.in/yaml.v3`**

Standard commands:

```sh
go build ./...
go test ./... -race
go vet ./...
gofmt -l internal/        # should print nothing
```

Conventions: TDD; teaching-quality doc comments for a C/Python audience; small, single-purpose
files; the id-is-the-filename invariant; machine/`--json` output stays neutral (no voice).
