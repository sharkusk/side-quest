# Tips

Practical recipes that build on two side-quest primitives: **tags** — free-form
`key=value` annotations you can attach to any quest — and **`--filter`**, which
selects quests with a boolean expression over those tags (and over the built-in
`status`/`type`/`priority` values). Neither needs setup: pick a key, use it
consistently, and query it whenever you like.

## Track launch features for an upcoming release

Give every quest bound for a release the same tag — say `launch=0.1.1` — and the
tag becomes a live checklist you can query at any point in the cycle.

**Tag at capture** with `--tag key=value` (repeat it for several tags):

```
side-quest new "Warn that quests are as visible as the repo" --tag launch=0.1.1
side-quest new "Fix launcher marker clobber" --type bug --tag launch=0.1.1 --tag area=cli
```

**Tag a quest you already filed** — open it and add the tag under `tags:` in its
frontmatter:

```
side-quest edit SQ-0075
```

```yaml
tags:
  launch: 0.1.1
```

Working through an agent, just ask it to tag the quest — it calls `quest_update`
and you never touch the file.

**See what's still outstanding for the release.** A bare `list` shows only open
and partial quests, so filtering by the tag gives you exactly the unfinished
launch work — your burn-down:

```
side-quest list --tag launch=0.1.1
```

**See the whole release scope, done or not.** `--filter` takes full control of the
selection — it ignores the default open-only view — so it returns every matching
quest regardless of status:

```
side-quest list --filter "launch=0.1.1"
```

Narrow it however you like: `--filter "launch=0.1.1 and bug"` for just the bugs,
or `--filter "launch=0.1.1 and not done"` for everything left to close.

## Decide which quests go in the release notes

When you cut the release, the same tag tells you exactly what shipped. Select the
quests that are both tagged for the release *and* finished:

```
side-quest list --filter "launch=0.1.1 and done"
```

Add `--json` for a machine-readable list you can feed to a notes generator:

```
side-quest list --filter "launch=0.1.1 and done" --json
```

Each quest in the output carries its `Commits` — the hashes the `post-commit`
hook linked back when the work landed — so you can turn *what shipped* into
*which commits, for which quest* with no extra bookkeeping. After the release,
move the tag forward (`launch=0.1.2`) on the next batch and repeat.
