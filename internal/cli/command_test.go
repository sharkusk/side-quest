package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sharkusk/side-quest/commands"
	"github.com/sharkusk/side-quest/internal/gitcmd"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := gitcmd.New(dir).Run("init", "-q"); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestInstallCommandWritesWhenAbsent(t *testing.T) {
	res, err := InstallCommand(gitRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != CmdInstalled {
		t.Fatalf("outcome=%q, want installed", res.Outcome)
	}
	if !strings.HasSuffix(res.Path, filepath.Join(".claude", "commands", "sq.md")) {
		t.Errorf("unexpected path %q", res.Path)
	}
	b, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatalf("not written: %v", err)
	}
	if string(b) != commands.Sq {
		t.Error("written content is not the embedded command")
	}
}

func TestInstallCommandUpToDateOnRerun(t *testing.T) {
	repo := gitRepo(t)
	if _, err := InstallCommand(repo); err != nil {
		t.Fatal(err)
	}
	res, err := InstallCommand(repo)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != CmdUpToDate {
		t.Fatalf("outcome=%q, want up_to_date", res.Outcome)
	}
}

func TestInstallCommandLeavesUnmarkedFile(t *testing.T) {
	repo := gitRepo(t)
	dir := filepath.Join(repo, ".claude", "commands")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const custom = "MY OWN /sq\n"
	if err := os.WriteFile(filepath.Join(dir, "sq.md"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := InstallCommand(repo)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != CmdLeftCustom {
		t.Fatalf("outcome=%q, want left_custom", res.Outcome)
	}
	b, _ := os.ReadFile(res.Path)
	if string(b) != custom {
		t.Error("clobbered a user's customized command")
	}
}

func TestInstallCommandRefreshesMarkedStale(t *testing.T) {
	repo := gitRepo(t)
	dir := filepath.Join(repo, ".claude", "commands")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := "---\nname: x\n---\n<!-- " + commands.ManagedMarker + " -->\nOLD\n"
	if err := os.WriteFile(filepath.Join(dir, "sq.md"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := InstallCommand(repo)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != CmdRefreshed {
		t.Fatalf("outcome=%q, want refreshed", res.Outcome)
	}
	b, _ := os.ReadFile(res.Path)
	if string(b) != commands.Sq {
		t.Error("marked-stale command not refreshed")
	}
}

func TestInstallCommandSkipsOutsideRepo(t *testing.T) {
	res, err := InstallCommand(t.TempDir()) // not a git repo
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != CmdSkippedNoRepo {
		t.Fatalf("outcome=%q, want skipped_no_repo", res.Outcome)
	}
}
