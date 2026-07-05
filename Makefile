# Development helpers for working on side-quest — and dogfooding it on itself.
#
# The MCP server is not a separate artifact: `side-quest serve` IS the binary, so
# updating the binary updates the server. The installed Claude plugin registers its
# server via .claude-plugin/plugin.json (`${CLAUDE_PLUGIN_ROOT}/bin/side-quest`); a
# non-plugin user gets a project .mcp.json from `side-quest onboard`. This repo's own
# .mcp.json is git-ignored dogfooding config (bare `side-quest` -> HEAD on PATH),
# written by `make dev` below — never shipped, so a plugin install sees no stray
# PATH-resolved server (SQ-0080).

BIN := side-quest
GOBIN := $(shell go env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell go env GOPATH)/bin
endif

# Stamp dev builds with the git-describe version so `side-quest version` and the
# MCP-advertised ServerInfo report the actual build commit (e.g. 590a5ae, or
# v0.1.0-6-g590a5ae once tagged, -dirty for an uncommitted tree) instead of an
# opaque "dev" — making a stale dogfood binary/server legible at a glance
# (SQ-0050). Falls back to "dev" outside a git repo. Releases don't use this: the
# GoReleaser workflow stamps main.version from the tag.
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build install test vet dev

# build a local ./side-quest from HEAD (gitignored; handy for ad-hoc runs).
build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/side-quest

# install HEAD to $(GOBIN) — the binary that bare `side-quest` (the .mcp.json MCP
# server) and the installed git-hook shims resolve to.
install:
	go install -ldflags "$(LDFLAGS)" ./cmd/side-quest

test:
	go test ./...

vet:
	go vet ./...

# dev wires up dogfooding side-quest on itself in one shot:
#   1. install HEAD to the PATH binary the MCP server and hooks use,
#   2. (re)point the git-hook shims at that binary (idempotent),
#   3. link the plugin's /sq command into the local .claude/commands.
# Re-run after code changes, then RESTART the MCP server so it reloads HEAD.
# (For a tight loop where the hook path is unchanged, a bare `make install` +
# server restart is enough — the shims already point at $(GOBIN)/side-quest.)
dev: install
	"$(GOBIN)/$(BIN)" install-hooks
	mkdir -p .claude/commands
	ln -sfn ../../commands/sq.md .claude/commands/sq.md
	@test -f .mcp.json || { printf '{\n  "mcpServers": {\n    "side-quest": { "command": "side-quest", "args": ["serve"] }\n  }\n}\n' > .mcp.json; echo "side-quest: wrote dogfood .mcp.json (bare side-quest -> your HEAD on PATH)."; }
	@echo "side-quest: dogfood ready — restart your MCP server so it reloads the HEAD binary."
