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
	idxFile, err := os.CreateTemp(s.gitDir, "sq-index-*")
	if err != nil {
		return "", err
	}
	idxPath := idxFile.Name()
	idxFile.Close()
	defer os.Remove(idxPath) // defer runs on return, like a Python finally block

	g := s.git.WithEnv("GIT_INDEX_FILE=" + idxPath)

	if parent != "" {
		if _, err := g.Run("read-tree", parent); err != nil {
			return "", err
		}
	} else {
		if _, err := g.Run("read-tree", "--empty"); err != nil {
			return "", err
		}
	}
	for path, data := range tx.puts {
		blob, err := g.RunInput(string(data), "hash-object", "-w", "--stdin")
		if err != nil {
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
	tree, err := g.Run("write-tree")
	if err != nil {
		return "", err
	}
	args := []string{"commit-tree", tree, "-m", msg}
	if parent != "" {
		args = []string{"commit-tree", tree, "-p", parent, "-m", msg}
	}
	return g.Run(args...)
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
// with nonexistent object ..." — verified against git's messages.
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
	cfgBytes, err := config.Marshal(config.Default())
	if err != nil {
		return err
	}
	return s.mutate("side-quest: init", func(snap *Snapshot, tx *txn) error {
		if snap.Tip != "" {
			return errors.New("side-quest already initialized")
		}
		tx.put(configPath, cfgBytes)
		return nil
	})
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
func (s *Store) Create(title, context string, tags map[string]string) (*quest.Quest, error) {
	now := time.Now().UTC().Truncate(time.Second)
	var created *quest.Quest
	err := s.mutate("side-quest: new quest", func(snap *Snapshot, tx *txn) error {
		id, cfg, err := allocID(snap)
		if err != nil {
			return err
		}
		q := &quest.Quest{
			ID:      id,
			Title:   title,
			Status:  quest.StatusOpen,
			Created: now,
			Commits: []string{},
			Context: context,
			Tags:    tags,
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
