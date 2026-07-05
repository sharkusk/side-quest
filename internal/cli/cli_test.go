package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChooseInstallDir(t *testing.T) {
	home := "/home/dev"
	cands := InstallDirCandidates(home, "/home/dev/.xbin")
	// InstallDirCandidates order: [$XDG_BIN_HOME, ~/.local/bin, ~/bin, ~/go/bin].
	local := filepath.Join(home, ".local", "bin")
	gobin := filepath.Join(home, "go", "bin")

	cases := []struct {
		name     string
		pathDirs []string
		want     string
	}{
		{"xdg preferred when on path", []string{"/home/dev/.xbin", local}, "/home/dev/.xbin"},
		{"falls to ~/.local/bin", []string{"/usr/bin", local}, local},
		{"skips off-path, picks go/bin", []string{gobin}, gobin},
		{"none on path -> empty", []string{"/usr/bin", "/bin"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ChooseInstallDir(cands, c.pathDirs); got != c.want {
				t.Errorf("ChooseInstallDir = %q, want %q", got, c.want)
			}
		})
	}
}

// When XDG_BIN_HOME is unset, InstallDirCandidates still returns a "" slot at
// index 0 (the plan's filter-at-consumer design for spec D7), so pin that the
// empty slot is never chosen and ~/bin can still win.
func TestChooseInstallDirEmptyXDG(t *testing.T) {
	home := "/home/dev"
	cands := InstallDirCandidates(home, "")
	local := filepath.Join(home, ".local", "bin")
	hbin := filepath.Join(home, "bin")

	cases := []struct {
		name     string
		pathDirs []string
		want     string
	}{
		{"empty xdg never chosen; falls to ~/.local/bin", []string{"", local}, local},
		{"picks ~/bin when it is the first candidate on path", []string{hbin}, hbin},
		{"only empties on path -> empty", []string{"", ""}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ChooseInstallDir(cands, c.pathDirs); got != c.want {
				t.Errorf("ChooseInstallDir = %q, want %q", got, c.want)
			}
		})
	}
}

// Install writes a marked launcher into an on-PATH conventional dir and reports it.
func TestInstallWritesMarkedLauncher(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("PATH", dir)

	r, err := Install()
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !r.OnPath {
		t.Errorf("chosen dir %s is on PATH; OnPath should be true", r.Dir)
	}
	b, err := os.ReadFile(filepath.Join(dir, LauncherName()))
	if err != nil {
		t.Fatalf("launcher not written: %v", err)
	}
	if !bytes.Contains(b, []byte(Marker)) {
		t.Error("written launcher is missing the marker")
	}
}

// Install refuses to overwrite a side-quest it did not install (no marker).
func TestInstallRefusesUnmarked(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mine := "#!/bin/sh\necho my own build\n"
	if err := os.WriteFile(filepath.Join(dir, LauncherName()), []byte(mine), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("PATH", dir)

	if _, err := Install(); err == nil {
		t.Fatal("Install should refuse to clobber an unmarked side-quest")
	}
	got, _ := os.ReadFile(filepath.Join(dir, LauncherName()))
	if string(got) != mine {
		t.Errorf("Install clobbered the user's own side-quest:\n%s", got)
	}
}

// With no candidate on PATH, Install falls back to ~/.local/bin and reports off-PATH.
func TestInstallFallbackReportsOffPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("PATH", "/usr/bin:/bin") // none of our candidates is on PATH

	r, err := Install()
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if r.OnPath {
		t.Error("no candidate was on PATH; OnPath should be false")
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "bin", LauncherName())); err != nil {
		t.Errorf("fallback did not write ~/.local/bin/%s: %v", LauncherName(), err)
	}
}

// Install is idempotent over its OWN launcher: re-installing over a marked
// launcher (e.g. re-enabling after the plugin shim was deleted) succeeds rather
// than tripping the D8 unmarked-clobber guard, and leaves a marked launcher in
// place. This is the path SQ-0066's cli_install relies on.
func TestInstallOverwritesOwnMarkedLauncher(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("PATH", dir)

	if _, err := Install(); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	if _, err := Install(); err != nil {
		t.Fatalf("re-Install over own marked launcher should succeed, not refuse: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, LauncherName()))
	if err != nil {
		t.Fatalf("launcher gone after re-install: %v", err)
	}
	if !bytes.Contains(b, []byte(Marker)) {
		t.Error("re-installed launcher is missing the marker")
	}
}

// Uninstall removes a marked launcher and leaves an unmarked side-quest untouched;
// it scans candidate dirs even when they are not on $PATH (the MCP-server case).
func TestUninstallRemovesOnlyMarked(t *testing.T) {
	home := t.TempDir()
	// The marked launcher lives in ~/.local/bin, which we deliberately keep OFF
	// $PATH to prove Uninstall scans the candidate dirs too (spec D7).
	local := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "side-quest"),
		[]byte("#!/bin/sh\n# "+Marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ownDir := t.TempDir()
	own := filepath.Join(ownDir, "side-quest")
	if err := os.WriteFile(own, []byte("#!/bin/sh\necho mine\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("PATH", ownDir) // ~/.local/bin intentionally NOT on PATH

	r, err := Uninstall()
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if len(r.Removed) != 1 || !strings.HasSuffix(r.Removed[0], filepath.Join(".local", "bin", "side-quest")) {
		t.Errorf("expected the marked launcher removed, got %v", r.Removed)
	}
	if _, err := os.Stat(filepath.Join(local, "side-quest")); !os.IsNotExist(err) {
		t.Errorf("marked launcher not removed (err=%v)", err)
	}
	if _, err := os.Stat(own); err != nil {
		t.Errorf("Uninstall removed the user's own side-quest: %v", err)
	}
	// D8's other half: the unmarked file must be reported in Refused, not silently
	// skipped — SQ-0066's cli_uninstall surfaces this list to the user.
	if len(r.Refused) != 1 || r.Refused[0] != own {
		t.Errorf("expected the unmarked side-quest reported in Refused, got %v", r.Refused)
	}
}

// Uninstall reports nothing removed and nothing refused when no launcher exists.
func TestUninstallEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("PATH", t.TempDir())
	r, err := Uninstall()
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if len(r.Removed) != 0 || len(r.Refused) != 0 {
		t.Errorf("expected empty result, got removed=%v refused=%v", r.Removed, r.Refused)
	}
}

// Status finds a marked launcher in a candidate dir that is off $PATH.
func TestStatusFindsMarkedOffPath(t *testing.T) {
	home := t.TempDir()
	local := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "side-quest"),
		[]byte("#!/bin/sh\n# "+Marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("PATH", "/usr/bin:/bin") // launcher dir not on PATH

	st := Status()
	if !st.Installed || !strings.HasSuffix(st.Path, filepath.Join(".local", "bin", "side-quest")) {
		t.Errorf("Status = %+v, want installed at ~/.local/bin/side-quest", st)
	}
}
