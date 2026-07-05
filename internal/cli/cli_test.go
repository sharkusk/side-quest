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
