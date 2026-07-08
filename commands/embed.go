// Package commands embeds the plugin's slash-command markdown so the binary can
// reinstall it elsewhere — e.g. install-cli writing a project-level /sq command
// (SQ-0107). The .md file remains the single source: the Claude Code plugin loads
// it from this directory directly, and this embed ships the same bytes.
package commands

import _ "embed"

//go:embed sq.md
var Sq string

// ManagedMarker identifies a copy of the command that side-quest wrote and may
// refresh. install-cli overwrites a file carrying it (so plugin updates
// propagate) but leaves a file without it untouched (a user's own command). It
// must appear verbatim in Sq — guarded by a test.
const ManagedMarker = "side-quest-managed-command"
