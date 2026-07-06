//go:build !windows

// These tests exercise the POSIX provisioning hook (scripts/provision.sh): they run it
// with a staged plugin root and a local fixture release server, asserting it lands the
// native binary at the fixed data-dir path the MCP command spawns. The provision path is
// what VERSION=dev otherwise skips — the coverage gap that let SQ-0082/0083/0085 ship
// (SQ-0084/0089). The Windows arm (scripts/provision.ps1) is covered in
// launcher_windows_test.go. #!/bin/sh stand-ins don't run under Windows' test runner.
package packaging

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// makeTarGz builds a .tar.gz holding a single executable named `side-quest` — the shape
// of a side-quest release archive, whose sole member provision.sh extracts.
func makeTarGz(t *testing.T, name, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// stageProvision copies scripts/provision.sh into a fresh plugin root stamped with the
// given VERSION (provision.sh reads VERSION from <root>/VERSION), returning its path.
func stageProvision(t *testing.T, version string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte(version+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scripts := filepath.Join(root, "scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile("../../scripts/provision.sh")
	if err != nil {
		t.Fatalf("read provision.sh: %v", err)
	}
	p := filepath.Join(scripts, "provision.sh")
	if err := os.WriteFile(p, src, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func provisionEnv(data, base string) []string {
	env := []string{"PATH=/usr/bin:/bin", "HOME=" + data, "CLAUDE_PLUGIN_DATA=" + data}
	if base != "" {
		env = append(env, "SIDE_QUEST_RELEASE_BASE="+base)
	}
	return env
}

// End to end: the hook downloads the release asset from SIDE_QUEST_RELEASE_BASE, verifies
// its SHA-256 against checksums.txt, extracts the tar.gz, and writes the binary to the
// fixed path the MCP command spawns (<data>/bin/side-quest.exe) plus a version marker.
func TestProvisionDownloadsBinary(t *testing.T) {
	const ver = "9.9.9"
	asset := fmt.Sprintf("side-quest_%s_%s_%s.tar.gz", ver, runtime.GOOS, runtime.GOARCH)
	base := serveRelease(t, asset, makeTarGz(t, "side-quest", "#!/bin/sh\necho DOWNLOADED \"$@\"\n"))
	data := t.TempDir()

	cmd := exec.Command(stageProvision(t, ver))
	cmd.Env = provisionEnv(data, base)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("provision: %v\n%s", err, out)
	}

	target := filepath.Join(data, "bin", "side-quest.exe")
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("binary not provisioned at %s: %v", target, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("provisioned binary is not executable (mode %v)", info.Mode())
	}
	out, err := exec.Command(target, "serve").CombinedOutput()
	if err != nil {
		t.Fatalf("run provisioned binary: %v\n%s", err, out)
	}
	if !strings.HasPrefix(string(out), "DOWNLOADED serve") {
		t.Errorf("provisioned binary output = %q, want the archived binary", out)
	}
	marker, _ := os.ReadFile(filepath.Join(data, "bin", ".provisioned-version"))
	if strings.TrimSpace(string(marker)) != ver {
		t.Errorf("version marker = %q, want %q", marker, ver)
	}
}

// Idempotent: with the marker already at this VERSION the hook returns before any
// download. Proven by pointing at a dead base — a re-download would fail — and asserting
// the pre-planted sentinel binary survives untouched.
func TestProvisionIdempotent(t *testing.T) {
	const ver = "9.9.9"
	data := t.TempDir()
	bin := filepath.Join(data, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(bin, "side-quest.exe")
	writeExec(t, target, "#!/bin/sh\necho SENTINEL\n")
	if err := os.WriteFile(filepath.Join(bin, ".provisioned-version"), []byte(ver), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(stageProvision(t, ver))
	cmd.Env = provisionEnv(data, "http://127.0.0.1:1") // any download would fail
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("provision (idempotent): %v\n%s", err, out)
	}
	got, _ := os.ReadFile(target)
	if !strings.Contains(string(got), "SENTINEL") {
		t.Error("provision overwrote an already-current binary; want the sentinel untouched")
	}
}

// A dev checkout (VERSION=dev) has no release to download and must no-op cleanly — exit 0,
// no binary — rather than error out and disturb session start.
func TestProvisionDevIsNoop(t *testing.T) {
	data := t.TempDir()
	cmd := exec.Command(stageProvision(t, "dev"))
	cmd.Env = provisionEnv(data, "")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dev provision should exit 0: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(data, "bin", "side-quest.exe")); !os.IsNotExist(err) {
		t.Errorf("dev provision unexpectedly created a binary (err=%v)", err)
	}
}

// A failed download must be non-fatal (exit 0) and leave no partial binary, so a bad
// network or a missing asset never blocks session start (SQ-0089: the hook feeds the
// MCP spawn, so a nonzero exit here could disrupt startup).
func TestProvisionDownloadFailureIsNonFatal(t *testing.T) {
	data := t.TempDir()
	cmd := exec.Command(stageProvision(t, "9.9.9"))
	cmd.Env = provisionEnv(data, "http://127.0.0.1:1") // nothing listening
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed provision must still exit 0 (non-fatal): %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(data, "bin", "side-quest.exe")); !os.IsNotExist(err) {
		t.Errorf("failed provision left a binary behind (err=%v)", err)
	}
}
