package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sharkusk/side-quest/internal/quest"
)

// cmdExport writes every quest to <dir> as its native SQ-<id>.md file — the same
// frontmatter+body that lives on the ref, so the export round-trips back into a
// store. The directory (and parents) is created if missing; existing files are
// overwritten but nothing else in the directory is touched (SQ-0101).
func cmdExport(args []string) error {
	fs := newFlagSet("export")
	setUsage(fs, "usage: side-quest export <dir>\nwrite every quest to <dir> as a native SQ-<id>.md file")
	rest, err := parseInterspersed(fs, args)
	if helpRequested(err) {
		return nil
	}
	if err != nil {
		return &usageErr{err.Error()}
	}
	if len(rest) != 1 {
		return &usageErr{"export needs exactly one <dir>"}
	}
	dir := rest[0]

	s, err := openStore()
	if err != nil {
		return err
	}
	quests, err := s.List()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, q := range quests {
		data, err := quest.Marshal(q)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, q.ID+".md"), data, 0o644); err != nil {
			return err
		}
	}
	fmt.Println(voiceFor(s).Exported(len(quests), dir))
	return nil
}
