# kura

A desktop manager for agent skills, built with [MyGo](https://github.com/egoist/mygo)'s native `ui` package: no webview, no Node, no Bun.

[中文说明](README-zh.md)

## What it does

- **Several skill roots at once.** `~/.agents/skills` is the default and always first; any other folder can be added in the UI. Each root keeps its own disabled/trash/snapshot state, so same-named skills never interfere.
- **Installs skills** from a GitHub repository (`owner/repo` or a pasted URL, with optional subdirectory, ref and name) or a local folder. Repository skills are found automatically and several can be installed in one run; one failure does not stop the others. The default name comes from the `name` field of `SKILL.md`.
- **Enables and disables by moving**, never copying and never a flag inside the skill: the folder travels between the root and `~/.kura/disabled/`. Removing sends it to `~/.kura/trash/` instead, where it can be restored; deleting it for good asks for confirmation and writes a content snapshot first.
- **Checks for upstream changes by git object hash**, not clocks or file times, so an up-to-date skill downloads nothing. Skills from one repository share a single clone, and a locally edited skill is reported as locally modified, not "update available".
- **Shows the full skill text and can translate it.** The detail column renders `SKILL.md` as Markdown, and its paragraphs can be translated into one global target language, fenced code blocks kept whole. Translations stay in memory: `SKILL.md` is never edited and nothing is written into the skill folder.
- **Speaks Chinese and English.** The interface language follows the system on first launch (a `zh*` locale is Chinese, anything else is English) and can be pinned in the settings dialog; the CLI has `--lang` and `KURA_LANG`.
- **Three-column window** (roots, cards, details). Column widths are draggable, roots can be reordered by dragging, skills show as a card grid or a two-line list, and there are search plus All / Enabled / Disabled / Trash filters with counts. Context menus are the system's native menus; appearance follows the system or is pinned to light or dark.
- **Updates itself** through the MyGo updater: the app menu has a check for updates, a check also runs daily in the background, and a download is installed only after its Ed25519 signature has been verified.
- **Finds agent directories on this machine.** On startup kura adds the global skill directories that already exist (Claude Code, Codex, Cursor, Gemini CLI, Pi and others — see `internal/store/agentdirs.go`), and a directory removed from the list is never added back.

## Install

Download from the [Releases](https://github.com/ArnoChenFx/kura/releases) page. Release assets are named like this:

- Windows: `kura.Setup.<version>.amd64.exe` (per-user installer), `kura.exe` (portable)
- macOS: `kura.<version>.arm64.dmg`, `kura.<version>.amd64.dmg`
- Linux: `kura_<version>_{amd64,arm64}.deb`, `kura-<version>-linux-<arch>.tar.gz`, or `install.sh` (per-user install into `~/.local/kura.app`, no root)

Those assets hold the window app; the CLI is a separate binary. Build it from source (below), or take the `kura-cli-<os>-<arch>` artifact CI attaches to each run.

## Command line

`cmd/kura` is a headless CLI over the same store as the window; `kura help` prints the full usage.

```sh
kura list                  # or `scan`: every skill in the root, with its state
kura add --local PATH      # install from a folder
kura add --github owner/repo [--sub-path P] [--ref R] [--name N]
kura update NAME           # re-fetch from the recorded source (snapshot first)
kura update --check NAME   # preview the changes; --all for every skill
kura disable NAME          # also enable / remove / restore
kura import-meta           # default root only; reads ~/.agents/.skill-lock.json
kura doctor                # read-only health check
```

Exit codes: `0` success, `1` operation failed, `2` usage error, `3` `doctor` found problems or `update --check` found pending updates. Every command takes `--json` (errors go to stderr as `{"error": "..."}`) and `--root` / `--home` (or `KURA_ROOT` / `KURA_HOME`); JSON shapes: [docs/note/cli-json-shape.md](docs/note/cli-json-shape.md).

## Where files live

| Path | Contents |
| --- | --- |
| `~/.agents/skills/` | default skill root (Windows: `%USERPROFILE%\.agents\skills`) |
| `~/.kura/config.json` | added roots, appearance (`theme`), skill layout (`view`) |
| `~/.kura/disabled/`, `trash/<skill>/<stamp>/`, `snapshots/<skill>/<stamp>/` | disabled skills, removed ones (restorable), and snapshots taken before an update or a removal |
| `~/.kura/roots/<id>/` | the same three folders for each added root; `<id>` is the first 12 hex digits of the SHA-256 of the normalised root path |
| `~/.kura/settings.json` | translation API base URL, key and model, plus the global target language |
| `~/.agents/.skill-lock.json` | read-only input for `kura import-meta`; never rewritten |
| `<skill>/.kura-meta.json` | per-skill metadata: source, commit and tree hash, timestamps, content hash |

`settings.json` stores the translation API key **in plain text** (mode `0600`): a deliberate choice for a local, single-user app. Do not use it on a shared machine.

## Build from source

Needs Go 1.27 or newer (`go.mod` asks for `go 1.27.2`), and nothing else: no frontend build step, no Node, Bun or npm.

```sh
go build .            # the window app
go build ./cmd/kura   # the headless CLI
go tool mygo build    # packaged app; -platform windows/amd64 and friends to cross-build

gofmt -l .            # the checks CI runs — a change should keep these green
go vet ./...
go tool mygo vet .
go test ./...
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go build ./...
```

## Development and docs

[AGENTS.md](AGENTS.md) has this repository's engineering rules (package boundaries, the UI thread contract, verification, test isolation); [REQUIREMENTS.md](REQUIREMENTS.md) has what kura must do, edge cases and non-goals included. [CHANGELOG.md](CHANGELOG.md) is the source of release notes, and each release needs a `## <version>` heading because the updater shows that section. Development notes live in [docs/note/](docs/note/).

## Status

kura is at `0.1.x`. The window's text is Chinese only. Release builds carry no Developer ID or Authenticode signature and are not notarised, so macOS and Windows may warn; the self-update path is not verified end to end: [docs/note/updates.md](docs/note/updates.md).

## License

No license is declared in this repository.
