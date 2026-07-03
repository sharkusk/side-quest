# Installing the side-quest binary

side-quest is a single, statically-linked Go binary whose only runtime
dependency is the system `git`. Pick whichever of the three routes below fits
your setup — you need just one. Once `side-quest` is on your `PATH`, continue
with [manual setup](manual-setup.md) or the [Claude Code plugin](plugin.md).

## Prebuilt binary (no toolchain)

Download the archive for your platform from the
[Releases](https://github.com/sharkusk/side-quest/releases) page, extract the
`side-quest` binary, and put it on your `PATH`.

| Platform | Where to put it | Notes |
|---|---|---|
| macOS | `/usr/local/bin` or `~/.local/bin` | `chmod +x side-quest`; first run may be blocked by Gatekeeper (unsigned) — clear it with `xattr -d com.apple.quarantine side-quest` |
| Linux | `~/.local/bin` (often on `PATH`) or `/usr/local/bin` (sudo) | `chmod +x side-quest` |
| Windows | a folder you add to `Path`, e.g. `%LOCALAPPDATA%\Programs\side-quest\` | use `side-quest.exe` |

## `go install` (needs Go ≥ 1.25)

```
go install github.com/sharkusk/side-quest/cmd/side-quest@latest
```

This installs to `~/go/bin` (`%USERPROFILE%\go\bin` on Windows), which is **not on
`PATH` by default** — add it:

- macOS/Linux: `export PATH="$HOME/go/bin:$PATH"` in your shell profile.
- Windows: add `%USERPROFILE%\go\bin` to your user `Path` environment variable.

## Build from source (needs Go ≥ 1.25)

```
git clone https://github.com/sharkusk/side-quest && cd side-quest
go build -o side-quest ./cmd/side-quest
```
