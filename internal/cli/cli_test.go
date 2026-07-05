package cli

import (
	"path/filepath"
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
