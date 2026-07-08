// Package store persists quests on the orphan ref refs/side-quest/quests.
//
// It never checks the ref out into the working tree. Reads use `cat-file` /
// `ls-tree`; writes build a new commit through a SCRATCH index
// (read-tree -> hash-object -> update-index -> write-tree -> commit-tree) and
// move the ref with `update-ref` compare-and-swap, retrying on a lost race so
// concurrent worktree lanes need no lock (spec §5).
package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/sharkusk/side-quest/internal/config"
	"github.com/sharkusk/side-quest/internal/gitcmd"
	"github.com/sharkusk/side-quest/internal/quest"
)

const (
	Ref        = "refs/side-quest/quests"
	configPath = "_config.yaml"
	questDir   = "quests"
)

// ErrNotFound is returned when a quest id has no file on the ref.
var ErrNotFound = errors.New("quest not found")

// ErrAlreadyInitialized is returned by Init when the ref already exists, so
// callers like `onboard` can treat a re-init as a no-op rather than a failure.
var ErrAlreadyInitialized = errors.New("side-quest already initialized")

// Store is bound to one git repository.
type Store struct {
	git    *gitcmd.Git
	gitDir string // absolute .git dir, where scratch index files are created
}

// Open finds the git repo containing dir and returns a Store for it.
func Open(dir string) (*Store, error) {
	probe := gitcmd.New(dir)
	top, err := probe.Run("rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("not a git repository: %w", err)
	}
	gitDir, err := probe.Run("rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, err
	}
	return &Store{git: gitcmd.New(top), gitDir: gitDir}, nil
}

func questPath(id string) string { return questDir + "/" + id + ".md" }

// tip returns the commit the ref points at, or "" if the ref does not exist.
// `for-each-ref` exits 0 and prints nothing for a missing ref, which is how we
// distinguish "empty store" from a real error.
func (s *Store) tip() (string, error) {
	out, err := s.git.Run("for-each-ref", "--format=%(objectname)", Ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// readFile returns the bytes of path in the tree at commit tip.
func (s *Store) readFile(tip, path string) ([]byte, error) {
	return s.git.RunRaw("cat-file", "-p", tip+":"+path)
}

// CommitMessage returns a linked commit's abbreviated SHA and its message, for
// the `show` command. full=false returns the subject line as text; full=true
// returns the complete message. ok is false when sha no longer resolves to a
// commit (rebased or gc'd) — the caller renders a placeholder rather than
// failing the whole command.
func (s *Store) CommitMessage(sha string, full bool) (short, text string, ok bool) {
	format := "%h%x00%s"
	if full {
		format = "%h%x00%B"
	}
	out, err := s.git.Run("show", "-s", "--format="+format, sha)
	if err != nil {
		return "", "", false
	}
	short, msg, found := strings.Cut(out, "\x00")
	if !found {
		return "", "", false
	}
	return short, strings.TrimRight(msg, "\n"), true
}

// listIDs returns the sorted quest ids present at tip (filenames minus ".md").
func (s *Store) listIDs(tip string) ([]string, error) {
	out, err := s.git.Run("ls-tree", "--name-only", tip+":"+questDir)
	if err != nil {
		// Missing quests/ directory => no quests yet.
		return nil, nil
	}
	var ids []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasSuffix(line, ".md") {
			ids = append(ids, strings.TrimSuffix(line, ".md"))
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// Snapshot is a read-only view of the store at a specific ref tip.
type Snapshot struct {
	Tip    string // "" when the ref does not exist yet
	Config config.Config
	IDs    []string
}

func (s *Store) snapshot() (*Snapshot, error) {
	tip, err := s.tip()
	if err != nil {
		return nil, err
	}
	snap := &Snapshot{Tip: tip, Config: config.Default()}
	if tip == "" {
		return snap, nil
	}
	if raw, err := s.readFile(tip, configPath); err == nil {
		cfg, err := config.Unmarshal(raw)
		if err != nil {
			return nil, err
		}
		snap.Config = cfg
	}
	ids, err := s.listIDs(tip)
	if err != nil {
		return nil, err
	}
	snap.IDs = ids
	return snap, nil
}

// --- mutation transaction -------------------------------------------------

// txn accumulates the file changes for one commit.
type txn struct {
	puts    map[string][]byte
	deletes map[string]bool
}

func newTxn() *txn {
	return &txn{puts: map[string][]byte{}, deletes: map[string]bool{}}
}

func (t *txn) put(path string, data []byte) {
	t.puts[path] = data
	delete(t.deletes, path)
}

func (t *txn) del(path string) {
	t.deletes[path] = true
	delete(t.puts, path)
}

// mutate runs build against the current snapshot, commits the staged changes,
// and moves the ref via CAS. If another writer advanced the ref first, it
// retries build against the fresh snapshot. build MUST be deterministic given
// the snapshot (it may run several times).
func (s *Store) mutate(msg string, build func(snap *Snapshot, tx *txn) error) error {
	const maxTries = 10
	for try := 0; try < maxTries; try++ {
		snap, err := s.snapshot()
		if err != nil {
			return err
		}
		tx := newTxn()
		if err := build(snap, tx); err != nil {
			return err
		}
		commit, err := s.buildCommit(snap.Tip, msg, tx)
		if err != nil {
			return err
		}
		ok, err := s.cas(snap.Tip, commit)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		// Lost the race: loop and rebuild against the new tip.
	}
	return fmt.Errorf("store: ref %s stayed contended after %d tries", Ref, maxTries)
}

// buildCommit stages tx into a scratch index and returns a new commit whose
// parent is `parent` ("" for the first, parentless commit).
func (s *Store) buildCommit(parent, msg string, tx *txn) (string, error) {
	var parents []string
	if parent != "" {
		parents = []string{parent}
	}
	return s.commitTx(parent, parents, msg, tx)
}

// buildMergeCommit stages tx into a scratch index built from EMPTY (so the
// tree is exactly tx's files, never a union with a parent's tree) and returns
// a commit with the given parents. Used by sync to record a 3-way merge as
// real two-parent history.
func (s *Store) buildMergeCommit(parents []string, msg string, tx *txn) (string, error) {
	return s.commitTx("", parents, msg, tx)
}

// commitTx stages tx into a fresh scratch index — seeded from readFrom's tree,
// or empty when readFrom is "" — and returns a commit with the given parents.
func (s *Store) commitTx(readFrom string, parents []string, msg string, tx *txn) (string, error) {
	idxFile, err := os.CreateTemp(s.gitDir, "sq-index-*")
	if err != nil {
		return "", err
	}
	idxPath := idxFile.Name()
	idxFile.Close()
	defer os.Remove(idxPath) // defer runs on return, like a Python finally block

	g := s.git.WithEnv("GIT_INDEX_FILE=" + idxPath)

	if readFrom != "" {
		if _, err := g.Run("read-tree", readFrom); err != nil {
			return "", err
		}
	} else {
		if _, err := g.Run("read-tree", "--empty"); err != nil {
			return "", err
		}
	}
	// hash-object, write-tree and commit-tree all create loose objects and so can
	// hit Windows' concurrent-write contention; retryTransient rides it out (the
	// writes are content-addressed and idempotent). update-index only stages into
	// the per-call scratch index, so it never contends.
	for path, data := range tx.puts {
		var blob string
		if err := retryTransient(func() error {
			var e error
			blob, e = g.RunInput(string(data), "hash-object", "-w", "--stdin")
			return e
		}); err != nil {
			return "", err
		}
		if _, err := g.Run("update-index", "--add", "--cacheinfo",
			"100644,"+blob+","+path); err != nil {
			return "", err
		}
	}
	for path := range tx.deletes {
		if _, err := g.Run("update-index", "--force-remove", path); err != nil {
			return "", err
		}
	}
	var tree string
	if err := retryTransient(func() error {
		var e error
		tree, e = g.Run("write-tree")
		return e
	}); err != nil {
		return "", err
	}
	args := []string{"commit-tree", tree, "-m", msg}
	for _, p := range parents {
		if p != "" {
			args = append(args, "-p", p)
		}
	}
	var commit string
	if err := retryTransient(func() error {
		var e error
		commit, e = g.Run(args...)
		return e
	}); err != nil {
		return "", err
	}
	return commit, nil
}

const transientMaxTries = 8

// transientSleep is the backoff between retries of a transient loose-object
// write; a package var so tests can stub it to a no-op.
var transientSleep = time.Sleep

// isTransientGitWrite reports whether err is a retryable loose-object write
// failure rather than a real fault. Git writes a loose object as a temp file it
// then renames into place; on Windows, several processes creating the SAME
// content-addressed object concurrently race on that rename and one fails with
// "Permission denied" (or "Unable to create"). Because the object is
// content-addressed, the write is idempotent — a retry after the winner
// finishes simply finds/writes the identical object. gitcmd pins LC_ALL=C, so
// git's message is stable English and this substring match is locale-safe.
//
// This is deliberately narrower than the CAS discriminator's "cannot lock ref"
// (a ref-level race, handled by mutate's retry) — those must not be conflated.
func isTransientGitWrite(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "Permission denied") ||
		strings.Contains(s, "Unable to create") ||
		strings.Contains(s, "unable to create") ||
		strings.Contains(s, "unable to write")
}

// retryTransient runs fn, retrying only a transient loose-object write
// (isTransientGitWrite) up to transientMaxTries with a short growing backoff.
// Success or any non-transient error returns immediately. Safe because git
// object writes are content-addressed and idempotent — this never masks a real
// fault, it just rides out Windows file-lock contention (SQ-0088).
func retryTransient(fn func() error) error {
	var err error
	for try := 0; try < transientMaxTries; try++ {
		if err = fn(); err == nil || !isTransientGitWrite(err) {
			return err
		}
		transientSleep(time.Duration(try+1) * 5 * time.Millisecond)
	}
	return err
}

// cas points the ref at newCommit only if it currently equals oldTip (or, when
// oldTip is "", only if the ref does not yet exist). A ref-lock failure means a
// concurrent writer violated that precondition — a retryable lost race, so we
// return (false, nil). Any OTHER update-ref failure is a genuine error we must
// surface rather than silently retry into the cap (spec §5.3).
//
// git distinguishes these in stderr: a precondition/lock failure says
// "cannot lock ref ..." (oldvalue mismatch, "reference already exists", or
// "unable to resolve reference"); a real fault says "cannot update ref ...
// with nonexistent object ..." — verified against git's messages. This string
// match is locale-independent because gitcmd pins LC_ALL=C on every git
// subprocess, so git's stderr is always stable English.
func (s *Store) cas(oldTip, newCommit string) (bool, error) {
	if _, err := s.git.Run("update-ref", Ref, newCommit, oldTip); err != nil {
		if strings.Contains(err.Error(), "cannot lock ref") {
			return false, nil // lost race — caller retries
		}
		return false, err // genuine failure — surface it
	}
	return true, nil
}

// Init creates the ref with a default config. It errors if the store is already
// initialized. The guard lives INSIDE the mutate closure so it re-checks the
// fresh snapshot on every CAS retry: if a concurrent Init wins the race, this
// one's retry sees a non-empty tip and returns the error instead of committing
// a second init on top (spec §5.3 CAS safety).
func (s *Store) Init() error {
	cfg := config.Default()
	// A configured remote signals a shared/team workflow, where the sequential
	// strategy collides across offline clones (two clones both mint SQ-0007 → same
	// filename, different content). Default to random ids in that case so filenames
	// never clash; a solo repo (no remote) keeps the tidy sequential default. The
	// user can always override with `config set id_strategy` (spec §7, SQ-0030).
	if s.hasRemote() {
		cfg.IDStrategy = config.Random
	}
	cfgBytes, err := config.Marshal(cfg)
	if err != nil {
		return err
	}
	return s.mutate("side-quest: init", func(snap *Snapshot, tx *txn) error {
		if snap.Tip != "" {
			return ErrAlreadyInitialized
		}
		tx.put(configPath, cfgBytes)
		return nil
	})
}

// hasRemote reports whether the repository has any configured remote. `git
// remote` exits 0 and prints nothing when there are none.
func (s *Store) hasRemote() bool {
	out, err := s.git.Run("remote")
	return err == nil && strings.TrimSpace(out) != ""
}

// allocID picks the next free id for the snapshot's strategy and returns it
// together with the config to persist (seq_next advanced, for sequential). The
// existence check guarantees the id collides with no current file — so even a
// fluke all-numeric random id can never clash with a sequential one (spec §7).
func allocID(snap *Snapshot) (string, config.Config, error) {
	cfg := snap.Config
	existing := make(map[string]bool, len(snap.IDs))
	for _, id := range snap.IDs {
		existing[id] = true
	}
	switch cfg.IDStrategy {
	case config.Random:
		for i := 0; i < 100; i++ {
			id, err := randomID(cfg.IDPrefix)
			if err != nil {
				return "", cfg, err
			}
			if !existing[id] {
				return id, cfg, nil
			}
		}
		return "", cfg, errors.New("could not find a free random id in 100 tries")
	default: // sequential
		n := cfg.SeqNext
		for {
			id := fmt.Sprintf("%s-%0*d", cfg.IDPrefix, cfg.SeqWidth, n)
			if !existing[id] {
				cfg.SeqNext = n + 1
				return id, cfg, nil
			}
			n++
		}
	}
}

// randomID returns prefix + "-" + 6 lowercase hex chars (3 random bytes).
func randomID(prefix string) (string, error) {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return prefix + "-" + hex.EncodeToString(b[:]), nil
}

// Create allocates an id and writes a new open quest. The quest file and the
// (possibly advanced) config are written in the SAME commit, so id allocation
// is atomic under the CAS loop: two racing lanes can never mint the same id —
// the loser's CAS fails and its rebuild sees the advanced counter / new files.
//
// typ and prio are required-with-defaults: an empty value is coerced to the
// package default; a non-empty but invalid value is rejected (nothing is
// written). This keeps quick capture a one-liner while guaranteeing every
// persisted quest carries a valid type and priority.
func (s *Store) Create(title, context string, typ quest.Type, prio quest.Priority, tags map[string]string) (*quest.Quest, error) {
	if typ == "" {
		typ = quest.DefaultType
	}
	if !typ.Valid() {
		return nil, fmt.Errorf("invalid type %q", typ)
	}
	if prio == "" {
		prio = quest.DefaultPriority
	}
	if !prio.Valid() {
		return nil, fmt.Errorf("invalid priority %q", prio)
	}
	now := time.Now().UTC().Truncate(time.Second)
	var created *quest.Quest
	err := s.mutate("side-quest: new quest", func(snap *Snapshot, tx *txn) error {
		id, cfg, err := allocID(snap)
		if err != nil {
			return err
		}
		q := &quest.Quest{
			ID:       id,
			Title:    title,
			Status:   quest.StatusOpen,
			Type:     typ,
			Priority: prio,
			Created:  now,
			Commits:  []string{},
			Context:  context,
			Tags:     tags,
		}
		data, err := quest.Marshal(q)
		if err != nil {
			return err
		}
		tx.put(questPath(id), data)
		cfgBytes, err := config.Marshal(cfg)
		if err != nil {
			return err
		}
		tx.put(configPath, cfgBytes)
		created = q
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// canonicalID normalizes a user-supplied id to its canonical form using the
// store's configured prefix and width, so shorthand like "11" or "0011" resolves
// to "SQ-0011". It is the single point both frontends pass through (every
// id-taking store method calls it), which keeps the CLI and MCP identical.
func (s *Store) canonicalID(id string) (string, error) {
	cfg, err := s.Config()
	if err != nil {
		return "", err
	}
	return quest.NormalizeID(cfg.IDPrefix, cfg.SeqWidth, id), nil
}

// Get loads one quest by id.
func (s *Store) Get(id string) (*quest.Quest, error) {
	id, err := s.canonicalID(id)
	if err != nil {
		return nil, err
	}
	tip, err := s.tip()
	if err != nil {
		return nil, err
	}
	if tip == "" {
		return nil, ErrNotFound
	}
	raw, err := s.readFile(tip, questPath(id))
	if err != nil {
		return nil, ErrNotFound
	}
	return quest.Unmarshal(id, raw)
}

// List returns all quests, sorted by id.
func (s *Store) List() ([]*quest.Quest, error) {
	snap, err := s.snapshot()
	if err != nil {
		return nil, err
	}
	out := make([]*quest.Quest, 0, len(snap.IDs))
	for _, id := range snap.IDs {
		raw, err := s.readFile(snap.Tip, questPath(id))
		if err != nil {
			return nil, err
		}
		q, err := quest.Unmarshal(id, raw)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, nil
}

// Update loads a quest, applies apply, and writes it back under the CAS loop.
// apply may run more than once (on CAS retry), so it must be a pure function of
// its argument.
func (s *Store) Update(id string, apply func(*quest.Quest)) error {
	id, err := s.canonicalID(id)
	if err != nil {
		return err
	}
	return s.mutate("side-quest: update "+id, func(snap *Snapshot, tx *txn) error {
		if snap.Tip == "" {
			return ErrNotFound
		}
		raw, err := s.readFile(snap.Tip, questPath(id))
		if err != nil {
			return ErrNotFound
		}
		q, err := quest.Unmarshal(id, raw)
		if err != nil {
			return err
		}
		apply(q)
		data, err := quest.Marshal(q)
		if err != nil {
			return err
		}
		tx.put(questPath(id), data)
		return nil
	})
}

// SetStatus sets a quest's status, stamping Completed when moving to done.
func (s *Store) SetStatus(id string, st quest.Status) error {
	if !st.Valid() {
		return fmt.Errorf("invalid status %q", st)
	}
	return s.Update(id, func(q *quest.Quest) {
		q.Status = st
		if st == quest.StatusDone && q.Completed == nil {
			t := time.Now().UTC().Truncate(time.Second)
			q.Completed = &t
		}
	})
}

// Reclassify sets a quest's type and/or priority in a SINGLE commit. An empty
// typ or prio leaves that field unchanged; a non-empty invalid value is
// rejected before any write, so the change is atomic — a caller can never land
// a new type but fail to apply the priority.
func (s *Store) Reclassify(id string, typ quest.Type, prio quest.Priority) error {
	if typ != "" && !typ.Valid() {
		return fmt.Errorf("invalid type %q", typ)
	}
	if prio != "" && !prio.Valid() {
		return fmt.Errorf("invalid priority %q", prio)
	}
	return s.Update(id, func(q *quest.Quest) {
		if typ != "" {
			q.Type = typ
		}
		if prio != "" {
			q.Priority = prio
		}
	})
}

// AppendNote appends text to a quest's body as a new, UTC-timestamped entry,
// leaving any existing body intact. The Update closure may run more than once
// under CAS, so it recomputes from the freshly-read body each time.
func (s *Store) AppendNote(id, text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("note text is empty")
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	return s.Update(id, func(q *quest.Quest) {
		var b strings.Builder
		b.WriteString(q.Body)
		if q.Body != "" {
			if !strings.HasSuffix(q.Body, "\n") {
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "--- note %s ---\n\n%s\n", ts, strings.TrimRight(text, "\n"))
		q.Body = b.String()
	})
}

// Modify sets a quest's title and/or merges tags in a SINGLE commit. An empty
// title leaves the title unchanged; a whitespace-only title is rejected before
// any write. Tags follow merge semantics: a non-empty value sets/overwrites the
// key, an empty value deletes it. Combining the two keeps the change atomic.
func (s *Store) Modify(id, title string, tags map[string]string) error {
	if title != "" && strings.TrimSpace(title) == "" {
		return fmt.Errorf("title is empty")
	}
	return s.Update(id, func(q *quest.Quest) {
		if title != "" {
			q.Title = title
		}
		for k, v := range tags {
			if v == "" {
				delete(q.Tags, k)
				continue
			}
			if q.Tags == nil {
				q.Tags = map[string]string{}
			}
			q.Tags[k] = v
		}
	})
}

// Replace overwrites a quest's entire content — everything but its id, which is
// the filename — with edited, in a single commit. The core fields are validated
// at this write boundary (a blank title or an unknown status/type/priority is
// rejected before anything is written), consistent with every other mutation.
//
// It is deliberately last-write-wins: unlike Update it does not merge concurrent
// changes, matching the mental model of the `edit` command, where the user opens
// a snapshot in $EDITOR and saves it back whole.
func (s *Store) Replace(id string, edited *quest.Quest) error {
	if strings.TrimSpace(edited.Title) == "" {
		return fmt.Errorf("title is empty")
	}
	if !edited.Status.Valid() {
		return fmt.Errorf("invalid status %q", edited.Status)
	}
	if !edited.Type.Valid() {
		return fmt.Errorf("invalid type %q", edited.Type)
	}
	if !edited.Priority.Valid() {
		return fmt.Errorf("invalid priority %q", edited.Priority)
	}
	id, err := s.canonicalID(id)
	if err != nil {
		return err
	}
	return s.mutate("side-quest: edit "+id, func(snap *Snapshot, tx *txn) error {
		if snap.Tip == "" {
			return ErrNotFound
		}
		if _, err := s.readFile(snap.Tip, questPath(id)); err != nil {
			return ErrNotFound
		}
		edited.ID = id // id is the filename; keep it authoritative, never from the buffer
		data, err := quest.Marshal(edited)
		if err != nil {
			return err
		}
		tx.put(questPath(id), data)
		return nil
	})
}

// AddCommit appends sha to a quest's commit list (deduped) and advances its status
// to reflect the work: a completing link (Completes: trailer) closes the quest, while
// any other link on an untouched open quest promotes it to partial — "work has
// started" — so it reads as in-progress rather than untouched (SQ-0094). Only open is
// promoted, so a partial/deferred/done quest is never churned or resurrected.
func (s *Store) AddCommit(id, sha string, complete bool) error {
	return s.Update(id, func(q *quest.Quest) {
		if !contains(q.Commits, sha) {
			q.Commits = append(q.Commits, sha)
		}
		switch {
		case complete && q.Status != quest.StatusDone:
			q.Status = quest.StatusDone
			t := time.Now().UTC().Truncate(time.Second)
			q.Completed = &t
		case !complete && q.Status == quest.StatusOpen:
			q.Status = quest.StatusPartial
		}
	})
}

// SetStrategy switches the id strategy, preserving seq_next so a later switch
// back to sequential resumes the counter (spec §7).
func (s *Store) SetStrategy(st config.Strategy) error {
	return s.mutate("side-quest: set id strategy "+string(st), func(snap *Snapshot, tx *txn) error {
		cfg := snap.Config
		cfg.IDStrategy = st
		data, err := config.Marshal(cfg)
		if err != nil {
			return err
		}
		tx.put(configPath, data)
		return nil
	})
}

// SetTone persists the human-facing voice tone in the on-ref config.
func (s *Store) SetTone(t config.Tone) error {
	return s.mutate("side-quest: set tone "+string(t), func(snap *Snapshot, tx *txn) error {
		cfg := snap.Config
		cfg.Tone = t
		data, err := config.Marshal(cfg)
		if err != nil {
			return err
		}
		tx.put(configPath, data)
		return nil
	})
}

// Config returns the on-ref configuration, or Default() when the store is empty.
func (s *Store) Config() (config.Config, error) {
	snap, err := s.snapshot()
	if err != nil {
		return config.Config{}, err
	}
	return snap.Config, nil
}

// SetRequireQuest flips the require_quest enforcement flag on the ref.
func (s *Store) SetRequireQuest(v bool) error {
	return s.mutate("side-quest: set require_quest", func(snap *Snapshot, tx *txn) error {
		cfg := snap.Config
		cfg.RequireQuest = v
		data, err := config.Marshal(cfg)
		if err != nil {
			return err
		}
		tx.put(configPath, data)
		return nil
	})
}

// SetAutoTrailer flips the auto_trailer flag on the ref (controls whether the
// prepare-commit-msg hook injects the current-quest trailer).
func (s *Store) SetAutoTrailer(v bool) error {
	return s.mutate("side-quest: set auto_trailer", func(snap *Snapshot, tx *txn) error {
		cfg := snap.Config
		cfg.AutoTrailer = v
		data, err := config.Marshal(cfg)
		if err != nil {
			return err
		}
		tx.put(configPath, data)
		return nil
	})
}

// SetLocalOnly flips the local_only flag on the ref. When true, Sync (and the
// pre-push hook) becomes a no-op so quest data never leaves this clone.
func (s *Store) SetLocalOnly(v bool) error {
	return s.mutate("side-quest: set local_only", func(snap *Snapshot, tx *txn) error {
		cfg := snap.Config
		cfg.LocalOnly = v
		data, err := config.Marshal(cfg)
		if err != nil {
			return err
		}
		tx.put(configPath, data)
		return nil
	})
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
