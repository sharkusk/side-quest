package packaging

// Shared helpers for the provisioning-shim download tests. A tiny HTTP server stands
// in for the GitHub release so the shims' real download → checksum → extract → exec
// path runs in CI, pointed at it via SIDE_QUEST_RELEASE_BASE (SQ-0084). No build tag:
// used by both the POSIX (launcher_test.go) and Windows (launcher_windows_test.go)
// suites.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// serveRelease starts a test server that serves the named asset (its bytes) and a
// checksums.txt listing its SHA-256 in GoReleaser's `<hash>  <name>` format (two
// spaces, as sha256sum writes). It returns the base URL to hand a shim through
// SIDE_QUEST_RELEASE_BASE. The server is torn down at test end.
func serveRelease(t *testing.T, asset string, body []byte) string {
	t.Helper()
	sum := sha256hex(body)
	mux := http.NewServeMux()
	mux.HandleFunc("/"+asset, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", sum, asset)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}
