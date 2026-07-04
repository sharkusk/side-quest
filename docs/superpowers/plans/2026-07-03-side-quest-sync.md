# side-quest Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reconcile a diverged `refs/side-quest/quests` across clones with a domain-aware three-way merge, published automatically by a `pre-push` hook and available as `side-quest sync`.

**Architecture:** A pure, table-tested merge engine (`internal/merge`) holds every rule and touches no git. A git-plumbing layer (`internal/store/sync.go`) fetches the remote quest ref into a *tracking* ref, reads the three snapshots, calls the engine, writes a two-parent merge commit, and pushes with a fetch-merge-retry loop. The CLI/hook layer (`cmd/side-quest`) wires `side-quest sync` and the `pre-push` hook, migrates the refspecs, and bootstraps a fresh clone.

**Tech Stack:** Go (stdlib only — `crypto/sha256`, `sort`, `strings`, `time`, `flag`); the existing `internal/gitcmd`, `internal/quest`, `internal/config` packages; real `git` via subprocess. No new dependencies.

## Global Constraints

- **Design source:** `docs/superpowers/specs/2026-07-03-side-quest-sync-design.md`. Every rule below is specified there; cite the section when in doubt.
- **TDD, no exceptions:** write the failing test, watch it fail for the right reason, then the minimal code. Docs are the only TDD-exempt files.
- **No new dependencies.** stdlib + existing internal packages only.
- **Determinism is a correctness property.** Every merge rule must be a pure function of its inputs and independent of which side is "local" vs "remote," so two clones converge. Where a tie is possible (equal commit times), break it on marshaled content bytes, never on side identity.
- **Marshaling is canonical.** `quest.Marshal` emits fields in declaration order and yaml.v3 sorts map keys, so byte comparisons of marshaled quests are stable across machines. Use `quest.Marshal` for equality, tiebreaks, and the collision id.
- **Quest id is the filename**, never a serialized field (`yaml:"-"`). A rename is a new filename, not a field edit.
- **Warn, never block (hook path):** the `pre-push` hook must never fail the user's branch push; on any sync failure it warns and exits 0. The standalone `side-quest sync` exits non-zero on genuine failure so CI notices.
- **Living docs:** the doc change ships in the SAME commit as the behavior it describes (the standalone `docs/sync.md` explainer is the final task).
- **Commit trailers (every commit):**
  ```
  Quest: SQ-0031
  Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01JZYTSMds932GAvtVwWW5Ws
  ```
  Use `Quest: SQ-0031` on every task commit EXCEPT: Task 13 (pre-push) closes SQ-0032 — use `Completes: SQ-0032`; Task 14 (docs) closes the feature — use `Completes: SQ-0031`. The post-commit hook links/closes from these trailers.
- **Run the whole suite before every commit:** `go test ./...` must pass (`go vet ./...` too).

---

## Task 1: Merge engine — types and structural cases

**Files:**
- Create: `internal/merge/merge.go`
- Test: `internal/merge/merge_test.go`

**Interfaces:**
- Consumes: `internal/quest` (`quest.Quest`, `quest.Marshal`), `internal/config` (`config.Config`).
- Produces:
  - `type Side struct { Config config.Config; Quests map[string]*quest.Quest; Touch map[string]time.Time; ConfigTouch time.Time }`
  - `type Result struct { Config config.Config; Quests map[string]*quest.Quest }`
  - `type EventKind int` with `const ( Renamed EventKind = iota; Conflicted )`
  - `type Event struct { Kind EventKind; ID string; Detail string }`
  - `func Merge(base, local, remote Side) (Result, []Event)`
  - `func canonical(q *quest.Quest) []byte` (unexported; marshaled bytes, `nil` on error)
  - `func equalQuest(a, b *quest.Quest) bool` (unexported)

This task handles every per-id case EXCEPT both-sides-changed (Task 2), id collision (Task 4), and config (Task 5): add-on-one-side, identical, unchanged-on-one-side. It also locks the empty-base behavior. For now `Merge` copies `local.Config` straight through (Task 5 replaces that).

- [ ] **Step 1: Write the failing test**

```go
package merge

import (
	"testing"
	"time"

	"github.com/sharkusk/side-quest/internal/config"
	"github.com/sharkusk/side-quest/internal/quest"
)

// q is a terse quest builder for tests.
func q(id, title string, st quest.Status) *quest.Quest {
	return &quest.Quest{
		ID: id, Title: title, Status: st,
		Type: quest.TypeFeature, Priority: quest.PriorityLow,
		Created: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Commits: []string{},
	}
}

func side(qs ...*quest.Quest) Side {
	m := map[string]*quest.Quest{}
	for _, x := range qs {
		m[x.ID] = x
	}
	return Side{Config: config.Default(), Quests: m, Touch: map[string]time.Time{}}
}

func TestMergeStructuralCases(t *testing.T) {
	base := side(q("SQ-0001", "shared", quest.StatusOpen))
	// SQ-0002 added only locally; SQ-0003 added only remotely;
	// SQ-0001 unchanged locally, edited remotely -> remote wins.
	remoteEdited := q("SQ-0001", "shared edited", quest.StatusPartial)
	local := side(q("SQ-0001", "shared", quest.StatusOpen), q("SQ-0002", "local only", quest.StatusOpen))
	remote := side(remoteEdited, q("SQ-0003", "remote only", quest.StatusOpen))

	res, events := Merge(base, local, remote)

	if len(events) != 0 {
		t.Fatalf("expected no events, got %v", events)
	}
	if got := res.Quests["SQ-0001"].Title; got != "shared edited" {
		t.Errorf("SQ-0001 title = %q, want remote's %q", got, "shared edited")
	}
	if _, ok := res.Quests["SQ-0002"]; !ok {
		t.Error("SQ-0002 (local add) missing from result")
	}
	if _, ok := res.Quests["SQ-0003"]; !ok {
		t.Error("SQ-0003 (remote add) missing from result")
	}
}

func TestMergeEmptyBaseTakesBothAdds(t *testing.T) {
	// No common ancestor: base is the zero Side.
	local := side(q("SQ-0001", "a", quest.StatusOpen))
	remote := side(q("SQ-0002", "b", quest.StatusOpen))
	res, events := Merge(Side{}, local, remote)
	if len(events) != 0 {
		t.Fatalf("events: %v", events)
	}
	if len(res.Quests) != 2 {
		t.Errorf("want 2 quests, got %d", len(res.Quests))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/merge/`
Expected: FAIL — `undefined: Merge` (package doesn't compile yet).

- [ ] **Step 3: Write minimal implementation**

```go
// Package merge is the pure, git-free three-way merge engine for the quest ref
// (design spec 2026-07-03-side-quest-sync §5). Every rule here is a deterministic
// function of its inputs so that two clones merging the same divergence converge
// on byte-identical results regardless of which side each calls "local".
package merge

import (
	"bytes"
	"sort"
	"time"

	"github.com/sharkusk/side-quest/internal/config"
	"github.com/sharkusk/side-quest/internal/quest"
)

// Side is one snapshot of the store the merge needs from a single commit.
// Touch holds, per quest id, the commit time of the last commit that modified
// quests/<id>.md on this side; it is consulted only for the both-sides-changed
// case, so the plumbing layer may leave it empty for non-conflicting quests.
type Side struct {
	Config      config.Config
	Quests      map[string]*quest.Quest
	Touch       map[string]time.Time
	ConfigTouch time.Time
}

// Result is the merged snapshot: the tree the plumbing layer will commit.
type Result struct {
	Config config.Config
	Quests map[string]*quest.Quest
}

// EventKind classifies a reportable merge outcome.
type EventKind int

const (
	Renamed    EventKind = iota // a quest was re-keyed to resolve an id collision
	Conflicted                  // both sides changed a quest; one side's scalars won
)

// Event is a human-reportable note about how the merge resolved something.
type Event struct {
	Kind   EventKind
	ID     string // the resulting id (post-rename for Renamed)
	Detail string
}

// canonical returns q's marshaled bytes (id excluded, per the file format), the
// stable basis for equality, tiebreaks, and the collision id. A marshal error is
// impossible for a well-formed quest; nil bytes simply read as "not equal".
func canonical(q *quest.Quest) []byte {
	b, err := quest.Marshal(q)
	if err != nil {
		return nil
	}
	return b
}

// equalQuest reports whether a and b are the same quest content (id aside).
func equalQuest(a, b *quest.Quest) bool {
	if a == nil || b == nil {
		return a == b
	}
	return bytes.Equal(canonical(a), canonical(b))
}

// Merge performs the three-way merge. base may be the zero Side (no common
// ancestor), in which case every quest is an add on one or both sides.
func Merge(base, local, remote Side) (Result, []Event) {
	res := Result{Config: local.Config, Quests: map[string]*quest.Quest{}}
	var events []Event

	for _, id := range unionIDs(base.Quests, local.Quests, remote.Quests) {
		b, l, r := base.Quests[id], local.Quests[id], remote.Quests[id]
		switch {
		case l == nil && r == nil:
			// deleted on both (no delete API exists; harmless if hand-edited).
		case l == nil:
			res.Quests[id] = r // add on remote only
		case r == nil:
			res.Quests[id] = l // add on local only
		case equalQuest(l, r):
			res.Quests[id] = l // same content (both made the same change, or neither)
		case b != nil && equalQuest(l, b):
			res.Quests[id] = r // unchanged locally -> take remote
		case b != nil && equalQuest(r, b):
			res.Quests[id] = l // unchanged remotely -> take local
		default:
			// both changed since base (or added independently) -> Task 2 / Task 4.
			res.Quests[id] = l // placeholder until Task 2
		}
	}
	return res, events
}

// unionIDs returns the sorted union of keys across the given maps.
func unionIDs(maps ...map[string]*quest.Quest) []string {
	seen := map[string]bool{}
	for _, m := range maps {
		for id := range m {
			seen[id] = true
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/merge/`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
git add internal/merge/
git commit   # message: "feat: merge engine types and structural cases" + Quest: SQ-0031 trailers
```

---

## Task 2: Merge engine — both-sides-changed field merge

**Files:**
- Modify: `internal/merge/merge.go` (replace the `default:` placeholder; add `mergeConflict`, `laterWins`, helpers)
- Test: `internal/merge/merge_test.go` (add cases)

**Interfaces:**
- Consumes: Task 1's `Side`, `Result`, `canonical`, `equalQuest`.
- Produces: `func mergeConflict(id string, b, l, r *quest.Quest, lTouch, rTouch time.Time) *quest.Quest` (unexported); wires `Merge`'s `default:` case to call it and emit a `Conflicted` event. Body/notes are handled in Task 3 — for now the winner's body is taken verbatim.

Rule (spec §5.1): the side with the later `Touch` wins ALL scalars; equal `Touch` breaks on lexicographically larger `canonical`. `Created` = earliest of b/l/r. `Commits` = union. `Tags` = union, winner wins a conflicting key. Body = winner's (Task 3 refines).

- [ ] **Step 1: Write the failing test**

```go
func TestMergeBothChangedScalarLWW(t *testing.T) {
	base := side(q("SQ-0001", "orig", quest.StatusOpen))
	l := q("SQ-0001", "local title", quest.StatusDone)
	l.Priority = quest.PriorityHigh
	l.Commits = []string{"aaa"}
	r := q("SQ-0001", "remote title", quest.StatusPartial)
	r.Commits = []string{"bbb"}

	local := side(l)
	local.Touch["SQ-0001"] = time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC) // earlier
	remote := side(r)
	remote.Touch["SQ-0001"] = time.Date(2026, 1, 2, 11, 0, 0, 0, time.UTC) // later -> wins

	res, events := Merge(base, local, remote)
	got := res.Quests["SQ-0001"]
	if got.Title != "remote title" || got.Status != quest.StatusPartial {
		t.Errorf("scalars = (%q,%q), want remote's (remote title, partial)", got.Title, got.Status)
	}
	// commits union regardless of winner:
	if len(got.Commits) != 2 || got.Commits[0] != "aaa" || got.Commits[1] != "bbb" {
		t.Errorf("commits = %v, want [aaa bbb]", got.Commits)
	}
	if len(events) != 1 || events[0].Kind != Conflicted {
		t.Errorf("events = %v, want one Conflicted", events)
	}
}

func TestMergeEqualTouchTiebreakByBytes(t *testing.T) {
	base := side(q("SQ-0001", "orig", quest.StatusOpen))
	l := q("SQ-0001", "aaa", quest.StatusOpen)
	r := q("SQ-0001", "zzz", quest.StatusOpen)
	ts := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	local := side(l)
	local.Touch["SQ-0001"] = ts
	remote := side(r)
	remote.Touch["SQ-0001"] = ts
	// Same result whichever side is "local": larger canonical bytes win.
	res1, _ := Merge(base, local, remote)
	res2, _ := Merge(base, remote, local)
	if res1.Quests["SQ-0001"].Title != res2.Quests["SQ-0001"].Title {
		t.Fatalf("tiebreak not symmetric: %q vs %q",
			res1.Quests["SQ-0001"].Title, res2.Quests["SQ-0001"].Title)
	}
}

func TestMergeTagsUnionWinnerWinsKey(t *testing.T) {
	base := side(q("SQ-0001", "orig", quest.StatusOpen))
	l := q("SQ-0001", "l", quest.StatusOpen)
	l.Tags = map[string]string{"area": "app", "only-l": "1"}
	r := q("SQ-0001", "r", quest.StatusOpen)
	r.Tags = map[string]string{"area": "map", "only-r": "2"}
	local := side(l)
	local.Touch["SQ-0001"] = time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	remote := side(r)
	remote.Touch["SQ-0001"] = time.Date(2026, 1, 2, 11, 0, 0, 0, time.UTC) // remote wins
	res, _ := Merge(base, local, remote)
	tags := res.Quests["SQ-0001"].Tags
	if tags["area"] != "map" || tags["only-l"] != "1" || tags["only-r"] != "2" {
		t.Errorf("tags = %v, want area=map + both only-* keys", tags)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/merge/ -run TestMergeBothChanged`
Expected: FAIL — placeholder returns `l`, so title is "local title" and no event.

- [ ] **Step 3: Write minimal implementation**

Replace the `default:` case body in `Merge`:

```go
		default:
			res.Quests[id] = mergeConflict(id, b, l, r, local.Touch[id], remote.Touch[id])
			events = append(events, Event{Kind: Conflicted, ID: id,
				Detail: "both sides changed; scalars taken from the later edit"})
```

Add the helpers:

```go
// mergeConflict resolves a quest changed on both sides. The later-touched side
// wins the scalar fields (equal times break on larger canonical bytes, a
// side-independent tiebreak); Created is the earliest seen; commits and tags
// union. Body is the winner's here and is refined in Task 3.
func mergeConflict(id string, b, l, r *quest.Quest, lTouch, rTouch time.Time) *quest.Quest {
	winner := laterWins(l, r, lTouch, rTouch)
	out := *winner // copy the winning scalars (Title, Status, Type, Priority, Context, Body, Completed)
	out.ID = id
	out.Created = earliest(b, l, r)
	out.Commits = unionCommits(b, l, r)
	out.Tags = unionTags(winner, l, r)
	return &out
}

// laterWins returns whichever of l, r was touched later; on an exact tie the one
// with lexicographically larger canonical bytes wins, so the choice never
// depends on which side is "local".
func laterWins(l, r *quest.Quest, lTouch, rTouch time.Time) *quest.Quest {
	if lTouch.Equal(rTouch) {
		if bytes.Compare(canonical(r), canonical(l)) > 0 {
			return r
		}
		return l
	}
	if rTouch.After(lTouch) {
		return r
	}
	return l
}

// earliest returns the earliest non-zero Created among the present quests.
func earliest(qs ...*quest.Quest) time.Time {
	var out time.Time
	for _, x := range qs {
		if x == nil || x.Created.IsZero() {
			continue
		}
		if out.IsZero() || x.Created.Before(out) {
			out = x.Created
		}
	}
	return out
}

// unionCommits merges commit lists preserving base order, then appends shas new
// to either side (deduped, deterministic order: local's new before remote's).
func unionCommits(b, l, r *quest.Quest) []string {
	seen := map[string]bool{}
	out := []string{}
	add := func(q *quest.Quest) {
		if q == nil {
			return
		}
		for _, sha := range q.Commits {
			if !seen[sha] {
				seen[sha] = true
				out = append(out, sha)
			}
		}
	}
	add(b)
	add(l)
	add(r)
	return out
}

// unionTags unions the tag keys of l and r; a key set on both takes the winner's
// value. Nil result when no tags exist anywhere.
func unionTags(winner, l, r *quest.Quest) map[string]string {
	out := map[string]string{}
	for _, q := range []*quest.Quest{l, r} {
		for k, v := range q.Tags {
			out[k] = v
		}
	}
	for k, v := range winner.Tags {
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/merge/`
Expected: PASS (all Task 1 + Task 2 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/merge/
git commit   # "feat: merge both-changed quests with scalar last-writer-wins" + Quest: SQ-0031
```

---

## Task 3: Merge engine — body preamble + notes union

**Files:**
- Modify: `internal/merge/merge.go` (add `mergeBody`, `splitBody`, `note`; call from `mergeConflict`)
- Create: `internal/merge/body.go` (the note-parsing helpers, to keep merge.go focused)
- Test: `internal/merge/body_test.go`

**Interfaces:**
- Consumes: Task 2's `mergeConflict`.
- Produces: `func mergeBody(winner, l, r *quest.Quest) string` (unexported, in body.go); `mergeConflict` sets `out.Body = mergeBody(winner, l, r)`.

Rule (spec §5.1 Body): preamble (prose before the first `--- note <ts> ---` header) is the winner's; notes are the union of both sides' note entries, deduped by full text, ordered by timestamp. The note header format matches `store.AppendNote`: `--- note <RFC3339> ---\n\n<text>`.

- [ ] **Step 1: Write the failing test**

```go
package merge

import (
	"strings"
	"testing"
	"time"

	"github.com/sharkusk/side-quest/internal/quest"
)

func withBody(id, title, body string) *quest.Quest {
	x := q(id, title, quest.StatusOpen)
	x.Body = body
	return x
}

func TestMergeBodyUnionsNotesKeepsWinnerPreamble(t *testing.T) {
	base := side(q("SQ-0001", "orig", quest.StatusOpen))
	lBody := "local preamble\n\n--- note 2026-01-02T10:00:00Z ---\n\nfrom local\n"
	rBody := "remote preamble\n\n--- note 2026-01-02T11:00:00Z ---\n\nfrom remote\n"
	l := withBody("SQ-0001", "l", lBody)
	r := withBody("SQ-0001", "r", rBody)
	local := side(l)
	local.Touch["SQ-0001"] = time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	remote := side(r)
	remote.Touch["SQ-0001"] = time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC) // remote wins preamble

	got := mustMergeBody(t, base, local, remote)
	if !strings.HasPrefix(got, "remote preamble") {
		t.Errorf("preamble should be winner's (remote); got:\n%s", got)
	}
	if !strings.Contains(got, "from local") || !strings.Contains(got, "from remote") {
		t.Errorf("both notes should survive; got:\n%s", got)
	}
	// ordered by timestamp: local (10:00) before remote (11:00)
	if strings.Index(got, "from local") > strings.Index(got, "from remote") {
		t.Errorf("notes out of timestamp order:\n%s", got)
	}
}

func TestMergeBodyDedupesIdenticalNote(t *testing.T) {
	base := side(q("SQ-0001", "orig", quest.StatusOpen))
	shared := "--- note 2026-01-02T10:00:00Z ---\n\nsame note\n"
	l := withBody("SQ-0001", "l", "p\n\n"+shared)
	r := withBody("SQ-0001", "r", shared)
	local := side(l)
	local.Touch["SQ-0001"] = time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	remote := side(r)
	remote.Touch["SQ-0001"] = time.Date(2026, 1, 2, 11, 0, 0, 0, time.UTC)
	got := mustMergeBody(t, base, local, remote)
	if n := strings.Count(got, "same note"); n != 1 {
		t.Errorf("identical note should appear once, got %d:\n%s", n, got)
	}
}

func mustMergeBody(t *testing.T, base, local, remote Side) string {
	t.Helper()
	res, _ := Merge(base, local, remote)
	return res.Quests["SQ-0001"].Body
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/merge/ -run TestMergeBody`
Expected: FAIL — winner's body is taken verbatim, so "from local" is absent.

- [ ] **Step 3: Write minimal implementation**

Create `internal/merge/body.go`:

```go
package merge

import (
	"sort"
	"strings"

	"github.com/sharkusk/side-quest/internal/quest"
)

// noteHeader marks the start of a timestamped note block, matching the format
// store.AppendNote writes: "--- note <RFC3339> ---".
const noteHeaderPrefix = "--- note "
const noteHeaderSuffix = " ---"

// note is one parsed note block: its timestamp (for ordering) and full text
// (header + body, trimmed) for dedup.
type note struct {
	ts   string
	text string
}

// splitBody separates the preamble (prose before the first note header) from the
// ordered list of note blocks. A body with no note headers is all preamble.
func splitBody(body string) (preamble string, notes []note) {
	lines := strings.Split(body, "\n")
	// find first note header
	start := -1
	for i, ln := range lines {
		if isNoteHeader(ln) {
			start = i
			break
		}
	}
	if start < 0 {
		return strings.TrimRight(body, "\n"), nil
	}
	preamble = strings.TrimRight(strings.Join(lines[:start], "\n"), "\n")
	// walk note blocks
	var cur []string
	var curTS string
	flush := func() {
		if len(cur) == 0 {
			return
		}
		notes = append(notes, note{ts: curTS, text: strings.TrimRight(strings.Join(cur, "\n"), "\n")})
		cur = nil
	}
	for _, ln := range lines[start:] {
		if isNoteHeader(ln) {
			flush()
			curTS = noteTimestamp(ln)
		}
		cur = append(cur, ln)
	}
	flush()
	return preamble, notes
}

func isNoteHeader(line string) bool {
	line = strings.TrimSpace(line)
	return strings.HasPrefix(line, noteHeaderPrefix) && strings.HasSuffix(line, noteHeaderSuffix)
}

func noteTimestamp(line string) string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, noteHeaderPrefix)
	line = strings.TrimSuffix(line, noteHeaderSuffix)
	return strings.TrimSpace(line)
}

// mergeBody keeps the winner's preamble and unions both sides' notes, deduped by
// full text and ordered by timestamp (stable for equal timestamps).
func mergeBody(winner, l, r *quest.Quest) string {
	preamble, _ := splitBody(winner.Body)
	_, ln := splitBody(l.Body)
	_, rn := splitBody(r.Body)

	seen := map[string]bool{}
	var all []note
	for _, n := range append(append([]note{}, ln...), rn...) {
		if seen[n.text] {
			continue
		}
		seen[n.text] = true
		all = append(all, n)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].ts < all[j].ts })

	var b strings.Builder
	if preamble != "" {
		b.WriteString(preamble)
	}
	for _, n := range all {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(n.text)
	}
	return b.String()
}
```

In `merge.go`, change `mergeConflict` to set the body:

```go
	out.Body = mergeBody(winner, l, r)
```
(place this line after `out := *winner` and before `return &out`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/merge/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/merge/
git commit   # "feat: union quest notes across sides on merge" + Quest: SQ-0031
```

---

## Task 4: Merge engine — id-collision reassignment

**Files:**
- Modify: `internal/merge/merge.go` (detect collision in the `default:` case; add `reassign` pass and helpers)
- Test: `internal/merge/merge_test.go` (add cases)

**Interfaces:**
- Consumes: Task 1–3.
- Produces: `func collisionID(prefix string, q *quest.Quest, taken map[string]bool) string` (unexported); a `Renamed` event per reassignment.

Rule (spec §5.2): a collision is `b == nil`, both `l` and `r` present, `!equalQuest(l,r)` — two different quests under one id. The earlier-`Created` keeps the id (tie: larger `canonical` keeps it); the loser is re-keyed to `PREFIX-<first 6 hex of sha256(canonical(loser))>`, extended by more hex on the vanishing chance of re-collision, with a rename note appended and a `Renamed` event emitted.

- [ ] **Step 1: Write the failing test**

```go
func TestMergeIDCollisionReassignsLoser(t *testing.T) {
	// SQ-0007 minted independently by two clones (no base) for different quests.
	early := q("SQ-0007", "fix parser", quest.StatusOpen)
	early.Created = time.Date(2026, 1, 2, 14, 2, 0, 0, time.UTC)
	late := q("SQ-0007", "add dark mode", quest.StatusOpen)
	late.Created = time.Date(2026, 1, 2, 15, 30, 0, 0, time.UTC)

	local := side(early)
	remote := side(late)
	res, events := Merge(Side{}, local, remote)

	if res.Quests["SQ-0007"].Title != "fix parser" {
		t.Errorf("earlier-Created should keep SQ-0007, got %q", res.Quests["SQ-0007"].Title)
	}
	// the loser exists under a new prefix-hex id with a rename note:
	var renamed *quest.Quest
	for id, x := range res.Quests {
		if id != "SQ-0007" {
			renamed = x
		}
	}
	if renamed == nil || renamed.Title != "add dark mode" {
		t.Fatalf("loser not reassigned: %v", res.Quests)
	}
	if !strings.Contains(renamed.Body, "renamed from SQ-0007") {
		t.Errorf("rename note missing:\n%s", renamed.Body)
	}
	if len(events) != 1 || events[0].Kind != Renamed || events[0].ID != renamed.ID {
		t.Errorf("events = %v, want one Renamed for %s", events, renamed.ID)
	}
}

func TestMergeIDCollisionDeterministic(t *testing.T) {
	early := q("SQ-0007", "fix parser", quest.StatusOpen)
	early.Created = time.Date(2026, 1, 2, 14, 2, 0, 0, time.UTC)
	late := q("SQ-0007", "add dark mode", quest.StatusOpen)
	late.Created = time.Date(2026, 1, 2, 15, 30, 0, 0, time.UTC)
	// Swapping which side is "local" must yield the same reassigned id.
	res1, _ := Merge(Side{}, side(early), side(late))
	res2, _ := Merge(Side{}, side(late), side(early))
	ids1, ids2 := idsOf(res1), idsOf(res2)
	if ids1 != ids2 {
		t.Errorf("collision resolution not deterministic: %s vs %s", ids1, ids2)
	}
}

func idsOf(r Result) string {
	var ids []string
	for id := range r.Quests {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}
```

Add `"crypto/sha256"`, `"encoding/hex"`, `"fmt"`, `"sort"`, `"strings"`, `"time"` imports to the test file as needed (`sort`, `strings`, `time` already present from earlier tasks).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/merge/ -run TestMergeIDCollision`
Expected: FAIL — the `default:` case currently calls `mergeConflict` (with `b == nil` it panics on `earliest`/reads, or produces one quest, not two).

- [ ] **Step 3: Write minimal implementation**

In `Merge`, split the final case so a base-less both-present pair is a collision, and collect reassignments to run after the main pass (so `taken` sees every original id):

```go
		default:
			if b == nil {
				// same id, two different quests (added independently) -> collision.
				keep, lose := collisionKeeper(l, r)
				res.Quests[id] = keep
				pendingLosers = append(pendingLosers, lose)
				break
			}
			res.Quests[id] = mergeConflict(id, b, l, r, local.Touch[id], remote.Touch[id])
			events = append(events, Event{Kind: Conflicted, ID: id,
				Detail: "both sides changed; scalars taken from the later edit"})
```

Declare `var pendingLosers []*quest.Quest` before the loop. After the loop, assign new ids:

```go
	// Resolve id collisions deterministically. taken starts as every id that
	// exists anywhere, so a reassigned id can never shadow a real quest.
	taken := map[string]bool{}
	for id := range res.Quests {
		taken[id] = true
	}
	for _, id := range unionIDs(base.Quests, local.Quests, remote.Quests) {
		taken[id] = true
	}
	sort.SliceStable(pendingLosers, func(i, j int) bool {
		return bytes.Compare(canonical(pendingLosers[i]), canonical(pendingLosers[j])) < 0
	})
	for _, lose := range pendingLosers {
		newID := collisionID(local.Config.IDPrefix, lose, taken)
		taken[newID] = true
		old := lose.ID
		lose.ID = newID
		lose.Body = appendRenameNote(lose.Body, old)
		res.Quests[newID] = lose
		events = append(events, Event{Kind: Renamed, ID: newID,
			Detail: "renamed from " + old + " (id collision)"})
	}
	return res, events
```

Add the helpers (and the imports `crypto/sha256`, `encoding/hex`, `fmt` to merge.go):

```go
// collisionKeeper returns (keeper, loser): the earlier-Created quest keeps the
// id; an exact Created tie is broken by larger canonical bytes, so the outcome
// does not depend on which side is "local".
func collisionKeeper(l, r *quest.Quest) (keep, lose *quest.Quest) {
	if l.Created.Equal(r.Created) {
		if bytes.Compare(canonical(l), canonical(r)) >= 0 {
			return l, r
		}
		return r, l
	}
	if l.Created.Before(r.Created) {
		return l, r
	}
	return r, l
}

// collisionID derives a stable new id for a reassigned quest: prefix + the first
// 6 hex chars of sha256(canonical), widening by 2 hex chars on the astronomically
// unlikely event the id is already taken. Deterministic across clones.
func collisionID(prefix string, q *quest.Quest, taken map[string]bool) string {
	sum := sha256.Sum256(canonical(q))
	full := hex.EncodeToString(sum[:])
	for n := 6; n <= len(full); n += 2 {
		id := fmt.Sprintf("%s-%s", prefix, full[:n])
		if !taken[id] {
			return id
		}
	}
	// Exhausted the hash (practically impossible); fall back to the full digest.
	return fmt.Sprintf("%s-%s", prefix, full)
}

// appendRenameNote records the reassignment as a note so the history is visible
// in the quest itself. It reuses the note header shape (no timestamp source in
// this pure layer, so the marker is fixed and sorts before real notes).
func appendRenameNote(body, oldID string) string {
	note := "--- note (sync) ---\n\nrenamed from " + oldID + " on sync: id collision"
	if strings.TrimSpace(body) == "" {
		return note
	}
	return strings.TrimRight(body, "\n") + "\n\n" + note
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/merge/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/merge/
git commit   # "feat: resolve quest id collisions deterministically on merge" + Quest: SQ-0031
```

---

## Task 5: Merge engine — config merge

**Files:**
- Modify: `internal/merge/merge.go` (compute `res.Config` via `mergeConfig`)
- Test: `internal/merge/merge_test.go` (add cases)

**Interfaces:**
- Consumes: Task 1's `Side`, `config.Config`.
- Produces: `func mergeConfig(base, local, remote config.Config, lTouch, rTouch time.Time) config.Config` (unexported).

Rule (spec §5.3): `seq_next` = max(base, local, remote). All other fields = last-writer-wins by `ConfigTouch` (equal times break on marshaled config bytes).

- [ ] **Step 1: Write the failing test**

```go
func TestMergeConfigSeqNextMaxAndLWW(t *testing.T) {
	base := side(q("SQ-0001", "x", quest.StatusOpen))
	base.Config = config.Default() // seq_next 1

	local := side(q("SQ-0001", "x", quest.StatusOpen))
	local.Config = config.Default()
	local.Config.SeqNext = 9
	local.Config.Tone = config.TonePlain
	local.ConfigTouch = time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)

	remote := side(q("SQ-0001", "x", quest.StatusOpen))
	remote.Config = config.Default()
	remote.Config.SeqNext = 5
	remote.Config.Tone = config.ToneDCC
	remote.ConfigTouch = time.Date(2026, 1, 2, 11, 0, 0, 0, time.UTC) // later -> tone wins

	res, _ := Merge(base, local, remote)
	if res.Config.SeqNext != 9 {
		t.Errorf("seq_next = %d, want max 9", res.Config.SeqNext)
	}
	if res.Config.Tone != config.ToneDCC {
		t.Errorf("tone = %q, want later writer's dcc", res.Config.Tone)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/merge/ -run TestMergeConfig`
Expected: FAIL — `Merge` currently sets `res.Config = local.Config` (seq_next 9 passes by luck, but tone would be local's `plain`).

- [ ] **Step 3: Write minimal implementation**

In `Merge`, replace `res := Result{Config: local.Config, ...}` initialization so config is computed after the loop (leave the struct literal's `Config` as `local.Config` and overwrite before returning):

```go
	res.Config = mergeConfig(base.Config, local.Config, remote.Config, local.ConfigTouch, remote.ConfigTouch)
	return res, events
```
(place immediately before the existing `return res, events`; the collision block above stays before it.)

Add the helper:

```go
// mergeConfig merges the on-ref config: seq_next only ever moves forward (max of
// all three), and every other field is last-writer-wins by config touch time,
// with an exact tie broken by larger marshaled bytes for determinism.
func mergeConfig(base, local, remote config.Config, lTouch, rTouch time.Time) config.Config {
	out := local
	if rTouch.After(lTouch) {
		out = remote
	} else if lTouch.Equal(rTouch) {
		lb, _ := config.Marshal(local)
		rb, _ := config.Marshal(remote)
		if bytes.Compare(rb, lb) > 0 {
			out = remote
		}
	}
	out.SeqNext = maxInt(base.SeqNext, maxInt(local.SeqNext, remote.SeqNext))
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/merge/`
Expected: PASS. Also run `go vet ./internal/merge/` — expect clean.

- [ ] **Step 5: Commit**

```bash
git add internal/merge/
git commit   # "feat: merge on-ref config (seq_next max, fields last-writer-wins)" + Quest: SQ-0031
```

---

## Task 6: Store — two-parent merge commit builder

**Files:**
- Modify: `internal/store/store.go` (add `buildMergeCommit`; refactor `buildCommit` to share)
- Test: `internal/store/sync_test.go` (new file)

**Interfaces:**
- Consumes: existing `txn`, `buildCommit` internals, `gitcmd`.
- Produces: `func (s *Store) buildMergeCommit(parents []string, msg string, tx *txn) (string, error)` — commits `tx`'s full file set from an EMPTY index (so the tree is exactly `tx`, no inherited files) with the given parents.

- [ ] **Step 1: Write the failing test**

```go
package store

import (
	"strings"
	"testing"
)

func TestBuildMergeCommitHasTwoParents(t *testing.T) {
	s := newStore(t)
	// two independent root commits on the quest ref namespace to use as parents
	tx1 := newTxn()
	tx1.put("quests/SQ-0001.md", []byte("one"))
	p1, err := s.buildCommit("", "p1", tx1)
	if err != nil {
		t.Fatal(err)
	}
	tx2 := newTxn()
	tx2.put("quests/SQ-0002.md", []byte("two"))
	p2, err := s.buildCommit("", "p2", tx2)
	if err != nil {
		t.Fatal(err)
	}

	tx := newTxn()
	tx.put("quests/SQ-0003.md", []byte("merged"))
	m, err := s.buildMergeCommit([]string{p1, p2}, "merge", tx)
	if err != nil {
		t.Fatal(err)
	}

	// exactly two parents, in order
	out, err := s.git.Run("rev-list", "--parents", "-n", "1", m)
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(out)
	if len(fields) != 3 || fields[1] != p1 || fields[2] != p2 {
		t.Fatalf("parents = %v, want [%s %s]", fields, p1, p2)
	}
	// tree is exactly tx (only SQ-0003), not a union with the parents' trees
	names, err := s.git.Run("ls-tree", "--name-only", "-r", m)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(names) != "quests/SQ-0003.md" {
		t.Fatalf("tree = %q, want only quests/SQ-0003.md", names)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestBuildMergeCommit`
Expected: FAIL — `undefined: (*Store).buildMergeCommit`.

- [ ] **Step 3: Write minimal implementation**

In `store.go`, add `buildMergeCommit` (reusing the scratch-index pattern from `buildCommit`, always starting from an empty index and accepting multiple parents):

```go
// buildMergeCommit stages tx into a scratch index built from EMPTY (so the tree
// is exactly tx's files, never a union with a parent's tree) and returns a commit
// with the given parents. Used by sync to record a 3-way merge as real history.
func (s *Store) buildMergeCommit(parents []string, msg string, tx *txn) (string, error) {
	idxFile, err := os.CreateTemp(s.gitDir, "sq-index-*")
	if err != nil {
		return "", err
	}
	idxPath := idxFile.Name()
	idxFile.Close()
	defer os.Remove(idxPath)

	g := s.git.WithEnv("GIT_INDEX_FILE=" + idxPath)
	if _, err := g.Run("read-tree", "--empty"); err != nil {
		return "", err
	}
	for path, data := range tx.puts {
		blob, err := g.RunInput(string(data), "hash-object", "-w", "--stdin")
		if err != nil {
			return "", err
		}
		if _, err := g.Run("update-index", "--add", "--cacheinfo", "100644,"+blob+","+path); err != nil {
			return "", err
		}
	}
	tree, err := g.Run("write-tree")
	if err != nil {
		return "", err
	}
	args := []string{"commit-tree", tree, "-m", msg}
	for _, p := range parents {
		if p != "" {
			args = append(args, "-p", p)
		}
	}
	return g.Run(args...)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestBuildMergeCommit`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit   # "feat: two-parent merge-commit builder for sync" + Quest: SQ-0031
```

---

## Task 7: Store — read a Side at a commit + per-quest touch times

**Files:**
- Create: `internal/store/sync.go`
- Test: `internal/store/sync_test.go` (add cases)

**Interfaces:**
- Consumes: `internal/merge` (`merge.Side`), existing `readFile`, `listIDs`, `Ref`.
- Produces:
  - `const TrackingRef = "refs/side-quest-remote/quests"`
  - `func (s *Store) sideAt(commit string) (merge.Side, error)` — config + all quests at `commit` (empty `Side` when `commit == ""`); `Touch` left empty.
  - `func (s *Store) fillTouch(side *merge.Side, commit string, ids []string) error` — sets `side.Touch[id]` (and `side.ConfigTouch`) from the last commit that changed each path at/under `commit`.
  - `func (s *Store) lastTouch(commit, path string) (time.Time, error)` — committer time of the last commit touching `path`.

- [ ] **Step 1: Write the failing test**

```go
func TestSideAtReadsQuestsAndTouch(t *testing.T) {
	s := newStore(t)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	a := mustCreate(t, s) // SQ-0001
	tip, _ := s.tip()

	side, err := s.sideAt(tip)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := side.Quests[a.ID]; !ok {
		t.Fatalf("sideAt missing %s: %v", a.ID, side.Quests)
	}

	if err := s.fillTouch(&side, tip, []string{a.ID}); err != nil {
		t.Fatal(err)
	}
	if side.Touch[a.ID].IsZero() {
		t.Errorf("touch time for %s not populated", a.ID)
	}
}

func TestSideAtEmptyCommit(t *testing.T) {
	s := newStore(t)
	side, err := s.sideAt("")
	if err != nil {
		t.Fatal(err)
	}
	if len(side.Quests) != 0 {
		t.Errorf("empty commit should yield no quests, got %d", len(side.Quests))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestSideAt`
Expected: FAIL — `undefined: (*Store).sideAt`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/store/sync.go`:

```go
// Package store — sync.go adds the git plumbing for the cross-clone three-way
// merge (design spec 2026-07-03-side-quest-sync). It reads snapshots at arbitrary
// commits, drives internal/merge, records the result as a two-parent merge
// commit, and publishes it with a fetch-merge-retry loop.
package store

import (
	"fmt"
	"strings"
	"time"

	"github.com/sharkusk/side-quest/internal/config"
	"github.com/sharkusk/side-quest/internal/merge"
	"github.com/sharkusk/side-quest/internal/quest"
)

// TrackingRef is where a fetch lands the remote quest ref, kept separate from the
// live Ref so the network never clobbers local quests.
const TrackingRef = "refs/side-quest-remote/quests"

// FetchRefspec maps the remote quest ref into the local tracking ref.
const FetchRefspec = Ref + ":" + TrackingRef

// sideAt reads the config and every quest present in the tree at commit into a
// merge.Side. An empty commit yields a zero Side (used as the no-common-ancestor
// base). Touch/ConfigTouch are left empty; callers fill them for the conflict set.
func (s *Store) sideAt(commit string) (merge.Side, error) {
	side := merge.Side{Config: config.Default(), Quests: map[string]*quest.Quest{}, Touch: map[string]time.Time{}}
	if commit == "" {
		return side, nil
	}
	if raw, err := s.readFile(commit, configPath); err == nil {
		cfg, err := config.Unmarshal(raw)
		if err != nil {
			return side, err
		}
		side.Config = cfg
	}
	ids, err := s.listIDs(commit)
	if err != nil {
		return side, err
	}
	for _, id := range ids {
		raw, err := s.readFile(commit, questPath(id))
		if err != nil {
			return side, err
		}
		q, err := quest.Unmarshal(id, raw)
		if err != nil {
			return side, err
		}
		side.Quests[id] = q
	}
	return side, nil
}

// fillTouch populates side.Touch for the given ids and side.ConfigTouch, reading
// the committer time of the last commit that changed each path at/under commit.
func (s *Store) fillTouch(side *merge.Side, commit string, ids []string) error {
	for _, id := range ids {
		t, err := s.lastTouch(commit, questPath(id))
		if err != nil {
			return err
		}
		side.Touch[id] = t
	}
	if t, err := s.lastTouch(commit, configPath); err == nil {
		side.ConfigTouch = t
	}
	return nil
}

// lastTouch returns the committer time (RFC3339, parsed) of the most recent
// commit reachable from commit that modified path.
func (s *Store) lastTouch(commit, path string) (time.Time, error) {
	out, err := s.git.Run("log", "-1", "--format=%cI", commit, "--", path)
	if err != nil {
		return time.Time{}, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return time.Time{}, fmt.Errorf("no commit touched %s", path)
	}
	return time.Parse(time.RFC3339, out)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestSideAt`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit   # "feat: read a merge Side and per-quest touch times at a commit" + Quest: SQ-0031
```

---

## Task 8: Store — local reconcile against the tracking ref

**Files:**
- Modify: `internal/store/sync.go` (add `reconcile`, `relationship`, `isAncestor`, `mergeBase`, `trackingTip`, `writeMerge`)
- Test: `internal/store/sync_test.go` (add a two-store convergence test with a shared file origin)

**Interfaces:**
- Consumes: Task 6 (`buildMergeCommit`), Task 7 (`sideAt`, `fillTouch`), `merge.Merge`.
- Produces:
  - `type SyncResult struct { Merged int; Renamed int; Pushed bool; UpToDate bool }`
  - `func (s *Store) trackingTip() (string, error)`
  - `func (s *Store) reconcile(dryRun bool) (SyncResult, error)` — reconciles the live Ref against TrackingRef locally (fast-forward or two-parent merge commit); no network. Moves the live ref via CAS unless `dryRun`.

**Test setup note:** to exercise this without a server, create a *bare* repo as a file remote, and two working clones that fetch/push the quest ref to it. Add a harness helper:

```go
// newOrigin returns (originDir) for a bare repo usable as a file remote.
func newOrigin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := gitcmd.New(dir).Run("init", "--bare", "-q"); err != nil {
		t.Fatal(err)
	}
	return dir
}

// clone makes a working repo with origin set to originDir and an identity.
func clone(t *testing.T, originDir string) *Store {
	t.Helper()
	dir := t.TempDir()
	g := gitcmd.New(dir)
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "Tester"},
		{"remote", "add", "origin", originDir},
	} {
		if _, err := g.Run(args...); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
```
(Place these in `sync_test.go`; import `gitcmd`.)

- [ ] **Step 1: Write the failing test**

```go
func TestReconcileFastForward(t *testing.T) {
	origin := newOrigin(t)
	a := clone(t, origin)
	if err := a.Init(); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, a) // SQ-0001
	if _, err := a.git.Run("push", origin, Ref); err != nil {
		t.Fatal(err)
	}

	b := clone(t, origin)
	// b has no quests yet; fetch origin into its tracking ref, then reconcile.
	if _, err := b.git.Run("fetch", origin, FetchRefspec); err != nil {
		t.Fatal(err)
	}
	res, err := b.reconcile(false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.UpToDate && res.Merged == 0 {
		t.Errorf("expected b to adopt remote quests, got %+v", res)
	}
	got, err := b.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("b should have 1 quest after reconcile, got %d", len(got))
	}
}

func TestReconcileDivergedConverges(t *testing.T) {
	origin := newOrigin(t)
	a := clone(t, origin)
	if err := a.Init(); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, a) // SQ-0001 (shared base)
	if _, err := a.git.Run("push", origin, Ref); err != nil {
		t.Fatal(err)
	}
	b := clone(t, origin)
	if _, err := b.git.Run("fetch", origin, FetchRefspec); err != nil {
		t.Fatal(err)
	}
	if _, err := b.reconcile(false); err != nil { // b adopts SQ-0001
		t.Fatal(err)
	}

	// Diverge: a adds SQ-0002, b adds SQ-0003 (random strategy avoids id clash;
	// both are sequential here but different content, so ids differ by counter).
	if _, err := a.Create("a work", "", "", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.git.Run("push", origin, Ref); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Create("b work", "", "", "", nil); err != nil {
		t.Fatal(err)
	}
	// b fetches a's advance and reconciles -> merge commit containing all three.
	if _, err := b.git.Run("fetch", origin, FetchRefspec); err != nil {
		t.Fatal(err)
	}
	res, err := b.reconcile(false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Merged == 0 {
		t.Errorf("expected a merge, got %+v", res)
	}
	got, _ := b.List()
	if len(got) != 3 {
		t.Fatalf("b should have 3 quests after merge, got %d", len(got))
	}
	// the merge commit has two parents
	tip, _ := b.tip()
	parents, _ := b.git.Run("rev-list", "--parents", "-n", "1", tip)
	if len(strings.Fields(parents)) != 3 {
		t.Errorf("merge tip should have 2 parents: %q", parents)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestReconcile`
Expected: FAIL — `undefined: (*Store).reconcile`.

- [ ] **Step 3: Write minimal implementation**

Add to `sync.go`:

```go
// SyncResult summarizes what a sync did, for reporting.
type SyncResult struct {
	Merged   int  // quests newly integrated from the remote (adopted or field-merged)
	Renamed  int  // id collisions reassigned
	Pushed   bool // a push to the remote succeeded
	UpToDate bool // nothing to do
}

func (s *Store) trackingTip() (string, error) {
	out, err := s.git.Run("for-each-ref", "--format=%(objectname)", TrackingRef)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// isAncestor reports whether a is an ancestor of b (a == b counts as false here;
// callers test equality separately).
func (s *Store) isAncestor(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	_, err := s.git.Run("merge-base", "--is-ancestor", a, b)
	return err == nil
}

// mergeBase returns the common ancestor of a and b, or "" if there is none.
func (s *Store) mergeBase(a, b string) string {
	out, err := s.git.Run("merge-base", a, b)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// reconcile brings the live Ref into agreement with TrackingRef using the domain
// merge, with no network I/O. It fast-forwards when possible, otherwise writes a
// two-parent merge commit. With dryRun it computes counts but writes nothing.
func (s *Store) reconcile(dryRun bool) (SyncResult, error) {
	local, err := s.tip()
	if err != nil {
		return SyncResult{}, err
	}
	remote, err := s.trackingTip()
	if err != nil {
		return SyncResult{}, err
	}
	switch {
	case remote == "":
		return SyncResult{UpToDate: true}, nil // no remote data
	case local == remote:
		return SyncResult{UpToDate: true}, nil
	case local == "":
		// fresh: adopt remote wholesale
		if !dryRun {
			if _, err := s.git.Run("update-ref", Ref, remote); err != nil {
				return SyncResult{}, err
			}
		}
		rs, _ := s.sideAt(remote)
		return SyncResult{Merged: len(rs.Quests)}, nil
	case s.isAncestor(local, remote):
		// local strictly behind -> fast-forward
		if !dryRun {
			if _, err := s.git.Run("update-ref", Ref, remote, local); err != nil {
				return SyncResult{}, err
			}
		}
		ls, _ := s.sideAt(local)
		rs, _ := s.sideAt(remote)
		return SyncResult{Merged: countNew(ls, rs)}, nil
	case s.isAncestor(remote, local):
		return SyncResult{}, nil // local ahead: nothing to merge (push will publish)
	default:
		return s.writeMerge(local, remote, dryRun)
	}
}

// writeMerge performs the true 3-way merge of local vs remote and records it as a
// two-parent merge commit (unless dryRun).
func (s *Store) writeMerge(local, remote string, dryRun bool) (SyncResult, error) {
	baseCommit := s.mergeBase(local, remote)
	base, err := s.sideAt(baseCommit)
	if err != nil {
		return SyncResult{}, err
	}
	lSide, err := s.sideAt(local)
	if err != nil {
		return SyncResult{}, err
	}
	rSide, err := s.sideAt(remote)
	if err != nil {
		return SyncResult{}, err
	}
	// Touch is needed only where both sides have the id; fill that set.
	var both []string
	for id := range lSide.Quests {
		if _, ok := rSide.Quests[id]; ok {
			both = append(both, id)
		}
	}
	if err := s.fillTouch(&lSide, local, both); err != nil {
		return SyncResult{}, err
	}
	if err := s.fillTouch(&rSide, remote, both); err != nil {
		return SyncResult{}, err
	}

	result, events := merge.Merge(base, lSide, rSide)
	renamed := 0
	for _, e := range events {
		if e.Kind == merge.Renamed {
			renamed++
		}
	}
	res := SyncResult{Merged: countNew(lSide, mergeSideOf(result)), Renamed: renamed}
	if dryRun {
		return res, nil
	}

	tx := newTxn()
	cfgBytes, err := config.Marshal(result.Config)
	if err != nil {
		return SyncResult{}, err
	}
	tx.put(configPath, cfgBytes)
	for id, q := range result.Quests {
		data, err := quest.Marshal(q)
		if err != nil {
			return SyncResult{}, err
		}
		tx.put(questPath(id), data)
	}
	commit, err := s.buildMergeCommit([]string{local, remote}, "side-quest: sync merge", tx)
	if err != nil {
		return SyncResult{}, err
	}
	if _, err := s.git.Run("update-ref", Ref, commit, local); err != nil {
		return SyncResult{}, err
	}
	return res, nil
}

// countNew counts ids in `to` that are absent from or differ from `from`.
func countNew(from, to merge.Side) int {
	n := 0
	for id, q := range to.Quests {
		old, ok := from.Quests[id]
		if !ok {
			n++
			continue
		}
		ob, _ := quest.Marshal(old)
		nb, _ := quest.Marshal(q)
		if string(ob) != string(nb) {
			n++
		}
	}
	return n
}

// mergeSideOf wraps a Result as a Side for countNew.
func mergeSideOf(r merge.Result) merge.Side {
	return merge.Side{Config: r.Config, Quests: r.Quests}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestReconcile`
Expected: PASS (both reconcile tests).

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit   # "feat: local reconcile of quest ref against the tracking ref" + Quest: SQ-0031
```

---

## Task 9: Store — Sync with fetch-merge-retry push

**Files:**
- Modify: `internal/store/sync.go` (add `Sync`, `SyncOptions`, `push`, `isNonFastForward`)
- Test: `internal/store/sync_test.go` (add end-to-end convergence + inheritance test)

**Interfaces:**
- Consumes: Task 8 (`reconcile`, `trackingTip`).
- Produces:
  - `type SyncOptions struct { DryRun bool; NoVerify bool }`
  - `func (s *Store) Sync(remote string, opts SyncOptions) (SyncResult, error)`

Loop (spec §6): fetch tracking → reconcile → if nothing to push, done → push `Ref` (`--no-verify` when `opts.NoVerify`) → on non-fast-forward, retry; on other error, return it.

- [ ] **Step 1: Write the failing test**

```go
func TestSyncConvergesAndInherits(t *testing.T) {
	origin := newOrigin(t)
	a := clone(t, origin)
	if err := a.Init(); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, a)
	if _, err := a.Sync("origin", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	b := clone(t, origin)
	if _, err := b.Sync("origin", SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	// diverge
	if _, err := a.Create("a work", "", "", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Sync("origin", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Create("b work", "", "", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Sync("origin", SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	// a syncs again and must converge to b's merged tree
	if _, err := a.Sync("origin", SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	al, _ := a.List()
	bl, _ := b.List()
	if len(al) != len(bl) || len(al) != 3 {
		t.Fatalf("did not converge: a=%d b=%d (want 3 each)", len(al), len(bl))
	}

	// inheritance: a second sync on a settled clone does nothing (no new commit)
	before, _ := a.tip()
	res, err := a.Sync("origin", SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := a.tip()
	if before != after || !res.UpToDate {
		t.Errorf("settled clone re-merged: before=%s after=%s res=%+v", before, after, res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestSyncConverges`
Expected: FAIL — `undefined: (*Store).Sync`.

- [ ] **Step 3: Write minimal implementation**

Add to `sync.go`:

```go
// SyncOptions tunes a sync. DryRun computes and reports without writing/pushing;
// NoVerify skips hooks on the internal quest-ref push (set by the pre-push hook to
// avoid re-entering itself).
type SyncOptions struct {
	DryRun   bool
	NoVerify bool
}

const maxSyncTries = 10

// Sync fetches the remote quest ref into the tracking ref, reconciles the live
// ref with a domain merge, and publishes the result, retrying on a lost push
// race. It never mutates the remote in DryRun.
func (s *Store) Sync(remote string, opts SyncOptions) (SyncResult, error) {
	var last SyncResult
	for try := 0; try < maxSyncTries; try++ {
		if _, err := s.git.Run("fetch", remote, FetchRefspec); err != nil {
			return SyncResult{}, fmt.Errorf("fetch %s: %w", remote, err)
		}
		res, err := s.reconcile(opts.DryRun)
		if err != nil {
			return SyncResult{}, err
		}
		last = res

		local, err := s.tip()
		if err != nil {
			return SyncResult{}, err
		}
		remoteTip, err := s.trackingTip()
		if err != nil {
			return SyncResult{}, err
		}
		// Nothing to publish if the remote already contains local.
		if local == "" || local == remoteTip || s.isAncestor(local, remoteTip) {
			last.UpToDate = last.UpToDate || (res.Merged == 0 && res.Renamed == 0)
			return last, nil
		}
		if opts.DryRun {
			return last, nil
		}

		if err := s.push(remote, opts.NoVerify); err == nil {
			last.Pushed = true
			return last, nil
		} else if !isNonFastForward(err) {
			return SyncResult{}, err
		}
		// lost the race: loop, re-fetch, re-merge.
	}
	return SyncResult{}, fmt.Errorf("sync: %s stayed contended after %d tries", Ref, maxSyncTries)
}

// push publishes the live quest ref to remote. noVerify skips hooks so the
// pre-push hook's own publish does not re-enter the hook.
func (s *Store) push(remote string, noVerify bool) error {
	args := []string{"push"}
	if noVerify {
		args = append(args, "--no-verify")
	}
	args = append(args, remote, Ref+":"+Ref)
	_, err := s.git.Run(args...)
	return err
}

// isNonFastForward reports whether err is git's rejection of a diverged push (the
// retryable case). gitcmd pins LC_ALL=C, so the English text is stable.
func isNonFastForward(err error) bool {
	if err == nil {
		return false
	}
	m := err.Error()
	return strings.Contains(m, "non-fast-forward") ||
		strings.Contains(m, "fetch first") ||
		strings.Contains(m, "rejected")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/`
Expected: PASS (all store tests). Also `go vet ./internal/store/`.

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit   # "feat: Sync fetch-merge-retry push loop for the quest ref" + Quest: SQ-0031
```

---

## Task 10: Store — bootstrap the live ref from the tracking ref

**Files:**
- Modify: `internal/store/sync.go` (add `BootstrapFromTracking`)
- Test: `internal/store/sync_test.go` (add case)

**Interfaces:**
- Consumes: Task 8 (`trackingTip`, `isAncestor`).
- Produces: `func (s *Store) BootstrapFromTracking() error` — if the tracking ref exists and the live ref is absent or a strict ancestor of it, fast-forward the live ref to it. Local-only, no network. Safe to call on every command.

- [ ] **Step 1: Write the failing test**

```go
func TestBootstrapAdoptsTrackingWhenLiveAbsent(t *testing.T) {
	origin := newOrigin(t)
	a := clone(t, origin)
	if err := a.Init(); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, a)
	if _, err := a.Sync("origin", SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	b := clone(t, origin)
	// simulate a fetch having populated only the tracking ref (no live ref yet)
	if _, err := b.git.Run("fetch", origin, FetchRefspec); err != nil {
		t.Fatal(err)
	}
	if tip, _ := b.tip(); tip != "" {
		t.Fatalf("precondition: live ref should be absent, got %s", tip)
	}
	if err := b.BootstrapFromTracking(); err != nil {
		t.Fatal(err)
	}
	got, _ := b.List()
	if len(got) != 1 {
		t.Fatalf("bootstrap should adopt 1 quest, got %d", len(got))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestBootstrap`
Expected: FAIL — `undefined: (*Store).BootstrapFromTracking`.

- [ ] **Step 3: Write minimal implementation**

```go
// BootstrapFromTracking fast-forwards the live Ref to the tracking ref when the
// live ref is absent or a strict ancestor of it — the fresh-clone case, where
// quests should appear without a full sync. It never touches a diverged or ahead
// live ref and does no network I/O, so it is safe to call on every command.
func (s *Store) BootstrapFromTracking() error {
	remote, err := s.trackingTip()
	if err != nil || remote == "" {
		return nil
	}
	local, err := s.tip()
	if err != nil {
		return err
	}
	switch {
	case local == "":
		_, err := s.git.Run("update-ref", Ref, remote)
		return err
	case local != remote && s.isAncestor(local, remote):
		_, err := s.git.Run("update-ref", Ref, remote, local)
		return err
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit   # "feat: bootstrap the live quest ref from the tracking ref" + Quest: SQ-0031
```

---

## Task 11: Refspec migration to the tracking ref

**Files:**
- Modify: `cmd/side-quest/hooks.go` (rewrite `addRefspec`; add `unsetConfigValue`)
- Modify: `docs/manual-setup.md` (the refspec section)
- Test: `cmd/side-quest/hooks_test.go` (add case)

**Interfaces:**
- Consumes: `store.FetchRefspec`, existing `ensureConfigContains`.
- Produces: an `addRefspec` that migrates old installs: removes the old `refs/side-quest/*:refs/side-quest/*` fetch AND push entries, adds `store.FetchRefspec` as fetch, keeps `HEAD` as push, and does NOT add a quest push refspec.

- [ ] **Step 1: Write the failing test**

```go
func TestAddRefspecMigratesOldConfig(t *testing.T) {
	dir := t.TempDir()
	g := gitcmd.New(dir)
	for _, args := range [][]string{
		{"init", "-q"},
		{"remote", "add", "origin", "https://example.com/x.git"},
		// simulate a pre-sync install:
		{"config", "--add", "remote.origin.fetch", "refs/side-quest/*:refs/side-quest/*"},
		{"config", "--add", "remote.origin.push", "HEAD"},
		{"config", "--add", "remote.origin.push", "refs/side-quest/*:refs/side-quest/*"},
	} {
		if _, err := g.Run(args...); err != nil {
			t.Fatal(err)
		}
	}

	addRefspec(g)

	fetch, _ := g.Run("config", "--get-all", "remote.origin.fetch")
	if strings.Contains(fetch, "refs/side-quest/*:refs/side-quest/*") {
		t.Errorf("old fetch refspec not removed:\n%s", fetch)
	}
	if !strings.Contains(fetch, store.FetchRefspec) {
		t.Errorf("new fetch refspec missing:\n%s", fetch)
	}
	push, _ := g.Run("config", "--get-all", "remote.origin.push")
	if strings.Contains(push, "refs/side-quest/*:refs/side-quest/*") {
		t.Errorf("old quest push refspec not removed:\n%s", push)
	}
	if !strings.Contains(push, "HEAD") {
		t.Errorf("HEAD push refspec should remain:\n%s", push)
	}

	// idempotent: a second call does not duplicate or re-add the old ones
	addRefspec(g)
	fetch2, _ := g.Run("config", "--get-all", "remote.origin.fetch")
	if strings.Count(fetch2, store.FetchRefspec) != 1 {
		t.Errorf("fetch refspec duplicated:\n%s", fetch2)
	}
}
```
Ensure `hooks_test.go` imports `strings`, `gitcmd`, and `store`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/side-quest/ -run TestAddRefspecMigrates`
Expected: FAIL — old refspec still present (current `addRefspec` only adds).

- [ ] **Step 3: Write minimal implementation**

Rewrite `addRefspec` in `hooks.go` and add `unsetConfigValue` (add `store` to the imports):

```go
// addRefspec migrates origin's refspecs to the sync model: the quest ref is
// fetched into a SEPARATE tracking ref (never clobbering the live ref), and is no
// longer pushed by git's refspec — the pre-push hook publishes it. Best-effort:
// no origin -> a note, no error. Idempotent, and it removes the pre-sync
// refspecs so upgrades converge.
func addRefspec(g *gitcmd.Git) {
	if _, err := g.Run("remote", "get-url", "origin"); err != nil {
		fmt.Println("side-quest: no 'origin' remote — skipped refspec (add it later or use sync).")
		return
	}
	const oldRefspec = "refs/side-quest/*:refs/side-quest/*"
	unsetConfigValue(g, "remote.origin.fetch", oldRefspec)
	unsetConfigValue(g, "remote.origin.push", oldRefspec)
	// Fetch the remote quest ref into the tracking ref sync merges from.
	ensureConfigContains(g, "remote.origin.fetch", store.FetchRefspec)
	// Keep pushing the current branch (a configured push refspec disables
	// push.default, so we restore current-branch push explicitly). The quest ref
	// is intentionally NOT pushed here — the pre-push hook publishes it.
	ensureConfigContains(g, "remote.origin.push", "HEAD")
}

// unsetConfigValue removes every occurrence of an exact value from a multi-valued
// git config key. git matches --unset-all against a value regex, so we anchor and
// escape the value. A "key/value not found" (exit 5) is fine.
func unsetConfigValue(g *gitcmd.Git, key, value string) {
	re := "^" + regexp.QuoteMeta(value) + "$"
	_, _ = g.Run("config", "--unset-all", key, re)
}
```
Add `"regexp"` and `"github.com/sharkusk/side-quest/internal/store"` to `hooks.go` imports.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/side-quest/ -run TestAddRefspec`
Expected: PASS.

- [ ] **Step 5: Update the doc (living-doc rule)**

In `docs/manual-setup.md`, replace the refspec instructions with:

```
# Fetch the remote quest ref into a local tracking ref (never clobbers your
# live quests — sync merges from it):
git config --add remote.origin.fetch 'refs/side-quest/quests:refs/side-quest-remote/quests'

# Keep pushing your current branch. Do NOT add a quest push refspec — the
# side-quest pre-push hook publishes refs/side-quest/quests for you.
git config --add remote.origin.push HEAD
```

- [ ] **Step 6: Commit**

```bash
git add cmd/side-quest/hooks.go cmd/side-quest/hooks_test.go docs/manual-setup.md
git commit   # "feat: migrate refspecs to the sync tracking ref" + Quest: SQ-0031
```

---

## Task 12: `side-quest sync` command + fresh-clone bootstrap wiring

**Files:**
- Create: `cmd/side-quest/sync.go`
- Modify: `cmd/side-quest/main.go` (dispatch `sync`; usage line; call `BootstrapFromTracking` in `openStore`)
- Test: `cmd/side-quest/sync_test.go`

**Interfaces:**
- Consumes: `store.Sync`, `store.SyncOptions`, `store.SyncResult`, `openStore`, existing `setUsage`/`helpRequested`.
- Produces: `func cmdSync(args []string) error`, `func resolveRemote(s *store.Store, flag string) (string, error)`.

- [ ] **Step 1: Write the failing test**

```go
func TestSyncCommandPublishesAndDryRun(t *testing.T) {
	bin := buildBinary(t)
	origin := t.TempDir()
	if _, err := gitcmd.New(origin).Run("init", "--bare", "-q"); err != nil {
		t.Fatal(err)
	}
	dir, s := newRepo(t)
	if _, err := gitcmd.New(dir).Run("remote", "add", "origin", origin); err != nil {
		t.Fatal(err)
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("first", "", "", "", nil); err != nil {
		t.Fatal(err)
	}

	// dry-run writes/pushes nothing
	out, code := runBin(t, bin, dir, "sync", "--dry-run")
	if code != 0 {
		t.Fatalf("sync --dry-run exit=%d out=%s", code, out)
	}
	if remoteHasQuestRef(t, origin) {
		t.Errorf("dry-run must not push; origin has the quest ref")
	}

	// real sync publishes
	if _, code := runBin(t, bin, dir, "sync"); code != 0 {
		t.Fatalf("sync exit=%d", code)
	}
	if !remoteHasQuestRef(t, origin) {
		t.Errorf("sync should have published the quest ref")
	}
}

func remoteHasQuestRef(t *testing.T, originDir string) bool {
	t.Helper()
	out, err := gitcmd.New(originDir).Run("for-each-ref", "--format=%(refname)", store.Ref)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(out) != ""
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/side-quest/ -run TestSyncCommand`
Expected: FAIL — `unknown command "sync"` (exit 1) / undefined `cmdSync`.

- [ ] **Step 3: Write minimal implementation**

Create `cmd/side-quest/sync.go`:

```go
package main

import (
	"flag"
	"fmt"

	"github.com/sharkusk/side-quest/internal/store"
)

// cmdSync reconciles this clone's quest ref with a remote: fetch into the tracking
// ref, three-way merge, push. --dry-run reports the plan without writing/pushing.
func cmdSync(args []string) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	dry := fs.Bool("dry-run", false, "show what would merge/push without writing anything")
	remote := fs.String("remote", "", "remote to sync with (default: origin, or the sole remote)")
	setUsage(fs, "sync [--dry-run] [--remote <name>]")
	if err := fs.Parse(args); err != nil {
		if helpRequested(err) {
			return nil
		}
		return err
	}
	s, err := openStore()
	if err != nil {
		return err
	}
	rem, err := resolveRemote(s, *remote)
	if err != nil {
		return err
	}
	res, err := s.Sync(rem, store.SyncOptions{DryRun: *dry})
	if err != nil {
		return err
	}
	prefix := "synced"
	if *dry {
		prefix = "dry-run"
	}
	if res.UpToDate && res.Merged == 0 && res.Renamed == 0 {
		fmt.Printf("side-quest: %s %s: already up to date.\n", prefix, rem)
		return nil
	}
	fmt.Printf("side-quest: %s %s: merged %d, renamed %d, pushed %t.\n",
		prefix, rem, res.Merged, res.Renamed, res.Pushed)
	return nil
}

// resolveRemote picks the remote to sync with: the flag if set, else "origin" if
// it exists, else the sole configured remote, else an error.
func resolveRemote(s *store.Store, flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	names, err := s.Remotes()
	if err != nil {
		return "", err
	}
	for _, n := range names {
		if n == "origin" {
			return "origin", nil
		}
	}
	if len(names) == 1 {
		return names[0], nil
	}
	return "", fmt.Errorf("no remote to sync with (configure 'origin' or pass --remote)")
}
```

Add a `Remotes` accessor to `internal/store/sync.go` (small, tested indirectly here):

```go
// Remotes returns the configured remote names (empty when there are none).
func (s *Store) Remotes() ([]string, error) {
	out, err := s.git.Run("remote")
	if err != nil {
		return nil, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}
```

In `main.go`: add the dispatch case and usage line, and wire bootstrap into `openStore`:

```go
	case "sync":
		return cmdSync(args)
```
```go
  sync [--dry-run] [--remote <name>]  reconcile quests with a remote (fetch+merge+push)
```
In `openStore`, after `store.Open(...)` succeeds and before returning, best-effort bootstrap:

```go
	s, err := store.Open(cwd)
	if err != nil {
		return nil, err
	}
	_ = s.BootstrapFromTracking() // fresh-clone convenience; local-only, safe to ignore
	return s, nil
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/side-quest/ -run TestSyncCommand`
Expected: PASS. Then `go test ./...` and `go vet ./...`.

- [ ] **Step 5: Commit**

```bash
git add cmd/side-quest/sync.go cmd/side-quest/main.go cmd/side-quest/sync_test.go internal/store/sync.go
git commit   # "feat: side-quest sync command + fresh-clone bootstrap" + Quest: SQ-0031
```

---

## Task 13: `pre-push` hook — auto-sync, warn-never-block (closes SQ-0032)

**Files:**
- Modify: `cmd/side-quest/sync.go` (add `cmdPrePushHook`)
- Modify: `cmd/side-quest/main.go` (dispatch `pre-push`; usage line)
- Modify: `cmd/side-quest/hooks.go` (add the `pre-push` shim to the `shims` slice)
- Test: `cmd/side-quest/sync_test.go` (add cases)

**Interfaces:**
- Consumes: `store.Sync` with `SyncOptions{NoVerify: true}`.
- Produces: `func cmdPrePushHook(args []string) error` — `args[0]` is the remote name (from git's hook argv); ignores stdin; on any error warns to stderr and returns nil (exit 0).

- [ ] **Step 1: Write the failing test**

```go
func TestPrePushHookPublishesQuests(t *testing.T) {
	bin := buildBinary(t)
	origin := t.TempDir()
	if _, err := gitcmd.New(origin).Run("init", "--bare", "-q"); err != nil {
		t.Fatal(err)
	}
	dir, s := newRepo(t)
	g := gitcmd.New(dir)
	if _, err := g.Run("remote", "add", "origin", origin); err != nil {
		t.Fatal(err)
	}
	// install hooks (adds pre-push shim + refspecs) via the real binary
	if _, code := runBin(t, bin, dir, "install-hooks"); code != 0 {
		t.Fatalf("install-hooks exit=%d", code)
	}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("hooked", "", "", "", nil); err != nil {
		t.Fatal(err)
	}
	// make a branch commit so there is something to push
	writeFile(t, dir, "f.txt", "x")
	if _, err := g.Run("add", "f.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Run("commit", "-q", "-m", "work"); err != nil {
		t.Fatal(err)
	}

	// a bare `git push` fires the pre-push hook, which syncs the quest ref
	if _, err := g.Run("push", "origin", "HEAD"); err != nil {
		t.Fatalf("push: %v", err)
	}
	if !remoteHasQuestRef(t, origin) {
		t.Errorf("pre-push hook should have published the quest ref")
	}
}

func TestPrePushHookOfflineWarnsExitsZero(t *testing.T) {
	bin := buildBinary(t)
	dir, _ := newRepo(t)
	// origin points at a nonexistent path -> fetch fails (offline analogue)
	if _, err := gitcmd.New(dir).Run("remote", "add", "origin", filepath.Join(dir, "nope.git")); err != nil {
		t.Fatal(err)
	}
	out, code := runBin(t, bin, dir, "pre-push", "origin", "file://nope")
	if code != 0 {
		t.Fatalf("pre-push must exit 0 even when sync fails; got %d out=%s", code, out)
	}
	if !strings.Contains(out, "couldn't publish quests") {
		t.Errorf("expected a warning; got %q", out)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
```
Ensure `sync_test.go` imports `os`, `path/filepath`, `strings`, `gitcmd`, `store`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/side-quest/ -run TestPrePushHook`
Expected: FAIL — `unknown command "pre-push"` / no pre-push shim installed.

- [ ] **Step 3: Write minimal implementation**

Add to `sync.go`:

```go
// cmdPrePushHook is the git pre-push hook entry point. git passes the remote name
// as args[0] (and URL as args[1]); we ignore the hook's stdin, since git omits a
// non-fast-forward ref from it (see SQ-0032). It syncs the quest ref out-of-band
// and NEVER blocks the user's branch push: any failure is a warning + exit 0.
func cmdPrePushHook(args []string) error {
	remote := "origin"
	if len(args) > 0 && args[0] != "" {
		remote = args[0]
	}
	s, err := openStore()
	if err != nil {
		return nil // not a side-quest repo state we can act on; let the push proceed
	}
	if _, err := s.Sync(remote, store.SyncOptions{NoVerify: true}); err != nil {
		fmt.Fprintf(stderr, "warning (side-quest): couldn't publish quests to %s: %v\n", remote, err)
		fmt.Fprintln(stderr, "                     run `side-quest sync` when back online.")
	}
	return nil
}
```
Use the package's existing stderr handle if one exists; otherwise add `var stderr = os.Stderr` at the top of `sync.go` and import `os`. (Check `cli.go` — if it already prints to `os.Stderr` directly, use `os.Stderr` here too for consistency.)

In `main.go`, add dispatch + usage:

```go
	case "pre-push":
		return cmdPrePushHook(args)
```
```go
  pre-push [<remote> <url>]        (hook) auto-sync quests on git push
```

In `hooks.go`, add the shim to the `shims` slice (the `|| true` guarantees the hook can never fail the push):

```go
		{"pre-push", q + ` pre-push "$@" || true`},
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/side-quest/ -run TestPrePushHook`
Expected: PASS. Then `go test ./...`.

- [ ] **Step 5: Commit** (this closes SQ-0032)

```bash
git add cmd/side-quest/sync.go cmd/side-quest/main.go cmd/side-quest/hooks.go cmd/side-quest/sync_test.go
git commit   # "feat: auto-sync quests via pre-push hook (warn-never-block)"
             # trailers: Completes: SQ-0032  +  Quest: SQ-0031  + Co-Authored-By + Claude-Session
```

---

## Task 14: Documentation — `docs/sync.md` explainer + living-doc updates (closes SQ-0031)

**Files:**
- Create: `docs/sync.md`
- Modify: `docs/architecture.md` (add a Sync section cross-linking `docs/sync.md`; note the two-parent merge-commit helper and refspec change)
- Modify: `README.md` (a short "Working with others" note pointing at `docs/sync.md`)

This is a docs-only task (TDD-exempt). Prose must be teaching-quality (the audience is new to Go/Rust, fluent in C/Python).

- [ ] **Step 1: Write `docs/sync.md`**

Cover, in order, with worked examples:
1. **Why the quest ref diverges** — two clones each commit to `refs/side-quest/quests`; a plain `git fetch`/`git push` can't reconcile two quest histories.
2. **The tracking ref** — `refs/side-quest-remote/quests`; the fetch refspec change; why the live ref is never clobbered.
3. **The three-way merge** — the per-id table (add/identical/unchanged/both-changed), scalar last-writer-wins by commit time, commits/tags/notes union, the id-collision rule, config `seq_next = max`. Reuse the spec's worked `SQ-0007` example.
4. **Convergence via merge commits** — the two-parent argument: a resolution becomes inherited history, so other clones don't re-derive it; determinism as the backstop.
5. **Automatic on push** — the `pre-push` hook flow; warn-never-block; that the hook is the publish path and `side-quest sync` is the manual fallback.
6. **Recovering manually** — `side-quest sync`, `--dry-run`, `--remote`.

- [ ] **Step 2: Update `docs/architecture.md`**

Add a "Sync" section: link `docs/sync.md`, state that `internal/merge` is pure and `internal/store/sync.go` does the plumbing, record the `buildMergeCommit` two-parent helper and the refspec change (fetch → tracking ref, quest ref off the push refspec).

- [ ] **Step 3: Update `README.md`**

Add a brief "Working with others" subsection: quests sync automatically on `git push`; point at `docs/sync.md` for the model.

- [ ] **Step 4: Verify docs render and links resolve**

Run: `go test ./...` (unchanged — sanity that nothing broke) and manually confirm the three files reference each other correctly (`grep -n "sync.md" README.md docs/architecture.md`).

- [ ] **Step 5: Commit** (this closes SQ-0031)

```bash
git add docs/sync.md docs/architecture.md README.md
git commit   # "docs: explain the sync model (docs/sync.md) + living-doc updates"
             # trailers: Completes: SQ-0031  + Co-Authored-By + Claude-Session
```

---

## Self-Review (completed by plan author)

**Spec coverage** — every spec section maps to a task:
- §3 architecture (three layers) → Tasks 1–5 (merge), 6–10 (store), 11–13 (cmd).
- §4 tracking ref + refspec + migration + bootstrap → Tasks 11 (migration), 10 (bootstrap), 12 (bootstrap wiring).
- §5 merge rules → Task 1 (structural), 2 (scalar LWW), 3 (body/notes), 4 (collision), 5 (config).
- §6 reconcile/commit/push loop → Tasks 8 (reconcile), 9 (Sync), 6 (merge commit).
- §7 CLI + hook → Tasks 12 (`sync`), 13 (`pre-push`).
- §8 error handling (warn vs fail asymmetry) → Task 13 (hook exit 0), Task 12 (command surfaces errors).
- §9 testing → each task's tests; two-repo convergence in Tasks 8–9.
- §10 docs → Task 14 (+ living-doc updates folded into Task 11).
- §11 accepted risks → no code; nothing to implement.

**Placeholder scan** — no "TBD"/"handle errors"/"similar to"; every code step carries full code. The one soft reference (Task 13's "use the package's existing stderr handle if one exists") includes the concrete fallback (`var stderr = os.Stderr`).

**Type consistency** — `merge.Side`/`Result`/`Event`/`Merge` are defined in Task 1 and consumed unchanged after; `SyncResult`/`SyncOptions`/`Sync`/`reconcile`/`sideAt`/`fillTouch`/`buildMergeCommit`/`BootstrapFromTracking`/`Remotes` signatures match across producer and consumer tasks; `store.FetchRefspec`/`store.Ref`/`store.TrackingRef` used consistently in Tasks 9/11/12.
