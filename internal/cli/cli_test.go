package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
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
	t.Setenv("USERPROFILE", home) // os.UserHomeDir() reads USERPROFILE on Windows (SQ-0086)
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
	t.Setenv("USERPROFILE", home) // os.UserHomeDir() reads USERPROFILE on Windows (SQ-0086)
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

// SQ-0074: the compiled side-quest binary embeds launcher.sh/.cmd — marker and
// all — via //go:embed, so a user's own go-installed side-quest carries the marker
// bytes deep in its data section. Detection scans only the file's prefix, so Install
// must treat such a binary as NOT ours and refuse to clobber it.
func TestInstallRefusesBinaryWithBuriedMarker(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, LauncherName())
	// A stand-in for a compiled binary: the marker appears only far past the prefix.
	fakeBinary := append(bytes.Repeat([]byte{0}, 4096), []byte(Marker)...)
	if err := os.WriteFile(target, fakeBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir() reads USERPROFILE on Windows (SQ-0086)
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("PATH", dir)

	if _, err := Install(); err == nil {
		t.Fatal("Install should refuse a binary whose marker is only buried deep (a user's own build)")
	}
	got, _ := os.ReadFile(target)
	if !bytes.Equal(got, fakeBinary) {
		t.Error("Install clobbered a user's own side-quest binary")
	}
}

// SQ-0074: Status must not report a user's own side-quest binary as an installed
// launcher just because it embeds the marker deep in its data section.
func TestStatusIgnoresBinaryWithBuriedMarker(t *testing.T) {
	home := t.TempDir()
	local := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeBinary := append(bytes.Repeat([]byte{0}, 4096), []byte(Marker)...)
	if err := os.WriteFile(filepath.Join(local, "side-quest"), fakeBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir() reads USERPROFILE on Windows (SQ-0086)
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("PATH", local)

	if st := Status(); st.Installed {
		t.Errorf("Status reported a user's own binary as installed: %+v", st)
	}
}

// SQ-0074: Uninstall must not delete a user's own side-quest binary that merely
// embeds the marker deep down — it is reported as refused, not removed.
func TestUninstallRefusesBinaryWithBuriedMarker(t *testing.T) {
	home := t.TempDir()
	local := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(local, "side-quest")
	fakeBinary := append(bytes.Repeat([]byte{0}, 4096), []byte(Marker)...)
	if err := os.WriteFile(target, fakeBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir() reads USERPROFILE on Windows (SQ-0086)
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("PATH", local)

	r, err := Uninstall()
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if len(r.Removed) != 0 {
		t.Errorf("Uninstall removed a user's own binary: %v", r.Removed)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("user's own binary was deleted: %v", err)
	}
	if len(r.Refused) != 1 || r.Refused[0] != target {
		t.Errorf("expected the binary reported in Refused, got %v", r.Refused)
	}
}

// With no candidate on PATH, Install falls back to ~/.local/bin and reports off-PATH.
func TestInstallFallbackReportsOffPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir() reads USERPROFILE on Windows (SQ-0086)
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
	t.Setenv("USERPROFILE", home) // os.UserHomeDir() reads USERPROFILE on Windows (SQ-0086)
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

// Install must refuse when it cannot READ an existing side-quest to verify our
// marker — not fall through and clobber it. A write-permitted, read-denied file
// (mode 0o200) is the exact case: the old guard only refused on a successful
// read, so a non-ENOENT read error let WriteFile destroy the user's own binary
// (D8: never clobber a file we didn't write).
func TestInstallRefusesUnreadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file-mode semantics")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses read permission, so the read never fails")
	}
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, LauncherName())
	mine := "#!/bin/sh\necho my own unreadable build\n"
	if err := os.WriteFile(target, []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o200); err != nil { // writable, NOT readable
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(target, 0o644) })
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir() reads USERPROFILE on Windows (SQ-0086)
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("PATH", dir)

	if _, err := Install(); err == nil {
		t.Fatal("Install should refuse a side-quest it cannot read to verify")
	}
	if err := os.Chmod(target, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != mine {
		t.Errorf("Install clobbered an unreadable side-quest:\n%s", got)
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
	t.Setenv("USERPROFILE", home) // os.UserHomeDir() reads USERPROFILE on Windows (SQ-0086)
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
	t.Setenv("USERPROFILE", home) // os.UserHomeDir() reads USERPROFILE on Windows (SQ-0086)
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
	t.Setenv("USERPROFILE", home) // os.UserHomeDir() reads USERPROFILE on Windows (SQ-0086)
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("PATH", "/usr/bin:/bin") // launcher dir not on PATH

	st := Status()
	if !st.Installed || !strings.HasSuffix(st.Path, filepath.Join(".local", "bin", "side-quest")) {
		t.Errorf("Status = %+v, want installed at ~/.local/bin/side-quest", st)
	}
}
