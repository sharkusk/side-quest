// Package sidequest holds assets baked into the binary. The embed directive
// must sit beside the file it embeds, so keeping it at the module root lets us
// bake the canonical AGENTS.md guidance in verbatim — `side-quest agents-md`
// and `onboard` then print the exact block the repo documents, with no second
// copy that could drift.
package sidequest

import _ "embed"

// AgentsGuidance is the repo's AGENTS.md: the canonical agent-guidance block a
// user pastes into their own project's AGENTS.md.
//
//go:embed AGENTS.md
var AgentsGuidance string
