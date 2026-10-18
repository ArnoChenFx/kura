# kura — requirements

kura is a desktop manager for agent skills, built with [MyGo](https://github.com/egoist/mygo) (native `ui` package, no webview; no Node, Bun or npm).

This file is the requirement of record: what kura must do, the edge cases it must handle, and what it deliberately does not do. `AGENTS.md` holds the engineering conventions (package boundaries, UI threading, verification commands, test isolation); this file wins on questions of behaviour.

Everything below is implemented unless it is listed in [Not implemented, not verified, or limited](#14-not-implemented-not-verified-or-limited).

## 1. Skill roots

kura manages one or more **skill roots**:

- the default root: `~/.agents/skills` (Windows: `%USERPROFILE%\.agents\skills`). It is always in the list and always first; it can be neither removed nor moved.
- any other folder the user adds in the UI (for example a project's `.claude/skills`). The list lives in `~/.kura/config.json` under `roots`.
- one directory per skill, containing `SKILL.md` plus optional files.

Roots are configuration, never constants.

- A root must be an existing folder (`ErrNotDir`); adding one that is already in the list is `ErrRootExists`. Both are sentinel errors, judgeable with `errors.Is`.
- Roots other than the default can be reordered by dragging in the UI. Moving the default root, or moving any root to position 0, is `ErrDefaultRoot`; a root or position that is not in the list is `ErrUnknownRoot`.
- **Removing a root only edits `config.json`**: the folder and the skills in it are left untouched. Copying or syncing skills *between* roots is out of scope.
- Every root keeps its own disabled/trash/snapshots state, so same-named skills in different roots never interfere:

  | Root | Disabled / trash / snapshots |
  | --- | --- |
  | default (`~/.agents/skills`) | `~/.kura/disabled/`, `~/.kura/trash/`, `~/.kura/snapshots/` (existing layout, no migration) |
  | an added root | `~/.kura/roots/<id>/disabled/`, `.../trash/`, `.../snapshots/` |

  `<id>` is the first 12 hex digits of the SHA-256 of the normalised root path (`store.RootID`). Two different roots with the same skill name stay apart.
- The CLI's `--root` reaches the same state when it points at a root from the list: `cli.resolveConfig` goes through `store.Library.Config`.
- **`config.json` is read-modify-written under `configMu`.** Appearance, view mode and the root list are written by separate background tasks; without the lock they overwrite each other (an auto-discovered root could be dropped for good). Any new writer of `config.json` must take the same lock (`Library.SetTheme`, `SetView`, `updateRoots`, `DiscoverAgentRoots` already do).
- A corrupt `config.json` must not break the app: `LoadLibrary` returns a list containing only the default root plus an error; the window reports it and stays usable, and the CLI keeps working on the default layout. A hand-edited `theme` or `view` with an unknown value is **not** an error: it falls back to `system` and `grid`.

## 2. Automatic discovery of agent skill directories

- On startup kura adds the agent skill directories that already exist on the machine, without asking per directory. The candidate list is `store.AgentSkillDirs` (`internal/store/agentdirs.go`), relative to the user's home directory, in table order: `.claude/skills` (Claude Code), `.codex/skills`, `.cursor/skills`, `.config/opencode/skills`, `.gemini/skills`, `.gemini/antigravity/skills`, `.copilot/skills`, `.pi/agent/skills`, `.kiro/skills`, `.qwen/skills`, `.codeium/windsurf/skills`, `.roo/skills`, `.trae/skills`, `.config/goose/skills`, `.factory/skills`, `.augment/skills`, `.config/agents/skills`. The default root is not in the list: it is already first.
- Only paths that exist and are directories are considered. The window shows how many directories were added; finding none stays silent.
- **Every candidate path is considered exactly once.** The ones seen are recorded in `config.json`'s `discovered`, so a directory the user removed from the list is never added back.
- A candidate that is already in the list, or resolves (through symlinks) to a directory already in the list, is recorded but not added twice.
- Nothing new means no write to `config.json`. A corrupt `config.json` makes discovery fail with an error and **no** write: user-edited content is not clobbered.

## 3. Installing skills

A skill is installed into the selected root, from one of two sources:

- a local directory, optionally with a name;
- a GitHub repository (`owner/repo` or a pasted URL) with an optional subdirectory, ref and name.

Discovery inside a repository (when the user gave no explicit subdirectory):

1. `SKILL.md` at the repository root → the whole repository is one skill.
2. Otherwise the `skills` array of `.claude-plugin/plugin.json`, when present (`./skills/<category>/<name>` style) — the author's explicit list wins. Entries pointing at missing paths are ignored rather than failing the whole install; a corrupt manifest falls back to scanning.
3. Otherwise a recursive scan: every directory with a `SKILL.md` is a candidate. `.git`, `node_modules` and build output are skipped; depth is capped at 6 and the candidate count at 64 (`ErrTooManySkills` above that — an explicit error beats silently dropping candidates). The scan does not descend into a directory that already matched.

Installing several skills from one repository at once (`store.AddMany`):

- one clone per batch, one tree lookup for all subdirectories, one sparse checkout — not one per skill;
- a failure in one item (name clash, subdirectory not in the repository, invalid subdirectory) fails only that item; the error text names the subdirectory;
- results correspond one-to-one with the requested items;
- the same subdirectory twice is not deduplicated: the second item fails with a name clash, unless a different name is given (installing the same content under two names is allowed);
- the UI shows the candidates as a check list (nothing selected by default, with a select-all/none toggle, in a scrolling area capped at 8 rows) and the main button counts the selection. A candidate list belongs to the `owner/repo`+ref it was discovered for: if the user edits the repository field, the stale list is dropped instead of being installed into a different repository, and a discovery result that arrives after the form changed is discarded whole (no implicit install).

Default naming priority: an explicit name (UI field or `--name`) → the `name` field of `SKILL.md`'s frontmatter → the source directory name (the repository name when the repository root is the skill, so a random temporary clone directory name never leaks). A name that cannot be used as a directory name is silently skipped in favour of the next candidate: non-empty, not `.` or `..`, no `/` or `\`, no `..`, none of `<>:"|?*` or control characters, not ending in a space or a dot, and not a Windows device name (`con`, `CON.md`, …). Metadata `id` and `displayName` follow the installed name; an existing skill with that name in the root is a conflict error.

### Skill market (skills.sh)

- A **market page** replaces the three columns while it is open; the title bar stays. The header's globe button opens it, and "Back to skills" returns to the selected root.
- Three rankings behind one segmented control: **All time** (default), **Hot** and **Trending**. Each ranking is read one page at a time with "Load more". A failed read keeps its reason and can be retried. A ranking that was read is not read again when the user switches back to it. Rankings are held in memory only.
- Rankings come from skills.sh's public JSON routes without an API key (`internal/market`). A record without an `owner/repo` source or a name is skipped; a malformed page is an error, shown on the page with a retry.
- A row shows the avatar, the name, `owner/repo` and the install count. Clicking the name opens the **detail page**: the description from SKILL.md's frontmatter, two external links (opened in the system browser, in the background), the list of files in the skill, and the rendered SKILL.md. The detail is read per skill and cached in memory; a failed read can be retried.
- Installing from a row or the detail page calls `store.AddSkillByName`: one clone of the repository, the skill matched by name the way `npx skills add --skill` matches it (SKILL.md `name` first, directory name only when there is no `name`, case-insensitive). Zero matches is an error; several matches are a candidate list the page shows and does not choose from. The installed skill has the same metadata as an install from GitHub, so updates work. A skill whose name is already an enabled skill in the current root is shown as installed, with no button.
- Only one install runs at a time. The page stays open after an install and reports failures in place. An install that finishes after the root was switched does not change the selection (the `gen` guard).
- The page follows the skill area's list/grid preference (`view` in `config.json`, switched from the page's own toolbar). The grid shows cards with the name, source, install count and install button, and the number of columns follows the window width. Both layouts are virtualised, so long rankings stay responsive.
- On a detail page the global 原文 / 译文 switch translates the description and SKILL.md through the translation settings, with the same rules as the local detail: display only, held in memory, nothing written to a skill folder, and no request until the settings are complete. Cache keys carry a `market:` / `mdoc:` prefix, so a local skill with the same name never shares a translation. Rankings and cards carry no descriptions, so nothing there is translated.
- Nothing from the market is written to disk or into a skill folder. Tests use a fake reader and a local mirror repository; they make no network calls.

## 4. Skill state: enable, disable, remove, restore, purge

Disable/enable is a **move**, never a copy and never a flag inside the skill: the folder travels between the root and `~/.kura/disabled/`. A name clash at the destination is an error, on both directions.

Moving between volumes (for example a root on another drive than `~/.kura` on Windows) must work: the move prefers a same-volume rename and falls back to "copy into a temporary directory on the target volume → rename into place → delete the source"; on failure the temporary directory is cleaned up and the source is left untouched.

| Action | Meaning |
| --- | --- |
| Remove | snapshot the content, then move the folder to `trash/<name>/<stamp>/` (recoverable) |
| Restore | move `trash/<name>/<stamp>/` back into the root; without a stamp the newest copy is taken. The emptied `trash/<name>/` container is removed; other stamps of the same skill are kept |
| Purge | delete every timestamped copy in the trash for good |

`Purge` is the only irreversible action and has extra requirements:

- it must write a **valid** snapshot for each trash copy before deleting (`snapshots/<name>/<stamp>/`). A snapshot is valid only if it is a real directory (not a symlink or junction), is not empty, and its `HashDir` matches the trash copy. An empty or wrong snapshot is rewritten; a non-empty mismatching one is left alone and the new one goes to `<stamp>-N`. If the snapshot cannot be written, the purge aborts and the trash copy survives; a partially written snapshot is cleaned up.
- the name must be a single directory name (`ErrInvalidName`), and after resolving symlinks the deletion target must still be inside the trash directory. Otherwise nothing is deleted.
- snapshots themselves are never deleted: "snapshot before any destructive operation" is the rule, and the snapshot of a purge stays for the record.

## 5. Metadata: `.kura-meta.json`

Written inside each managed skill folder, next to `SKILL.md`:

```json
{
  "metaVersion": 1,
  "id": "notes-api",
  "displayName": "notes-api",
  "source": {
    "type": "github",
    "url": "https://github.com/example/notes-skills.git",
    "ref": "main",
    "subPath": "skills/notes-api",
    "commit": "abc1234",
    "tree": "def5678"
  },
  "pluginName": "example-skills",
  "installedAt": "2026-01-01T00:00:00.000Z",
  "updatedAt": "2026-01-02T00:00:00.000Z",
  "contentHash": "sha256:...",
  "notes": ""
}
```

- `source.type` is `local` (with `path`) or `github`; when the type is `github`, `subPath` is empty if the skill is the repository root itself.
- `source.commit` and `source.tree` record the upstream commit and the git tree hash of the skill's subdirectory (the repository root tree when there is no `subPath`) at install or update time. They are what makes checking for updates cheap. Metadata written by older versions, or imported from the lock file, has no `tree` and falls back to a per-file comparison — no migration is needed.
- `pluginName` is imported from the sibling `~/.agents/.skill-lock.json` when the skill is listed there.
- **`~/.agents/.skill-lock.json` is read-only for kura: read in, never rewritten.** It only describes the default root, so "import metadata" is offered for the default root only: the window hides the action for other roots, and `store.Config.ImportMeta` / `kura import-meta` refuse an added root with `ErrImportDefaultOnly` (CLI exit code 1, including `--dry-run` and `--lock`). Importing only fills in missing metadata: a skill that already has `.kura-meta.json` is skipped, never overwritten.
- `contentHash` is a SHA-256 over the skill folder's files, with paths sorted and `.kura-meta.json` itself excluded, so an unchanged skill keeps a stable hash and drift can be detected.
- Broken metadata must never crash the app or a scan: the skill is reported with `metaError`/`MetaErr` and the UI, `doctor` and the CLI stay usable.

## 6. Checking for updates and applying them

The check never relies on local clocks or file times; it compares git object hashes, cheapest first, and stops as soon as it can decide:

1. `git ls-remote` for the recorded `ref` (the default branch when empty). Same commit as `source.commit` → up to date, nothing is downloaded.
2. Otherwise one treeless shallow clone per repository (`--depth 1 --filter=blob:none --no-checkout`): the commit and trees, no file contents. Same tree hash for the skill's subdirectory as `source.tree` → up to date (the repository changed elsewhere), still no file contents.
3. Otherwise (or when the metadata has no `commit`/`tree`) a sparse checkout of just the skill subdirectories and a per-file comparison with the installed content.

- Skills from the same repository (same URL and ref) share one `ls-remote`, one clone and one sparse checkout, both when checking and when applying updates (`PreviewUpdates` / `UpdateMany`; never loop over `PreviewUpdate` / `Update`).
- A `local` source is compared file by file against the recorded path.
- Steps 1 and 2 compare upstream against what was installed, so a locally edited skill is not reported as "has update": the check flags it as locally modified (`localModified`) instead. Applying an update still overwrites those edits, after a snapshot.
- `PreviewUpdate` is strictly read-only: no snapshot, no change to the skill folder or metadata. The window shows the preview as a review panel with one line per changed file (status, path, ±lines, new hash prefix) and applies it only on request; `kura update --check` exits 3 when something would change.
- Applying an update: snapshot the old content → fetch → replace atomically → refresh `updatedAt`, `commit`, `tree` and `contentHash`. A fetch failure leaves the skill as it was; a successful replacement keeps the snapshot.
- Every git invocation goes through `runGit` / `gitOutput` (timeout, `GIT_TERMINAL_PROMPT=0`, abort below 1 KB/s for 30 s) and through `gitCommandContext` (`exec_cmd_windows.go` / `exec_cmd_other.go`), which starts git without a console window on Windows. This is the only git entry point; non-test code must not call `exec.Command("git", …)` directly.

## 7. Translations

Translation exists to make skill text readable in the UI. It is **display only**.

- Settings live in `~/.kura/settings.json` (`baseUrl`, `apiKey`, `model`, `targetLang`), read and written only through `store.LoadSettings` / `SaveSettings`. The API key is stored **in plain text** with file permissions `0600`: the user's explicit choice for a local single-user app — no system credential manager, no encryption. The key must never appear in logs, error messages, test fixtures or any exported file.
- There is one global target language. `store.NormalizeTargetLang` maps known inputs to canonical codes (`zh`→`zh-CN`, `en-US`, `ja`, `fr`, …); an unknown value is `ErrUnknownLang` and is **not** guessed. An empty value means `zh-CN`.
- No request is made unless `Settings.Ready()` (base URL, key and model all set); otherwise the UI guides the user to the settings dialog and `ErrNotConfigured` is returned. The HTTP client is OpenAI-compatible (`/chat/completions`), standard library only, with the caller's timeout and cancellation; responses are capped (10 MB) and non-2xx bodies are truncated to 512 bytes in the error.
- `SKILL.md` is translated paragraph by paragraph: `SplitForTranslation` splits at blank lines and never inside a fenced code block (``` or ~~~, CommonMark rules — an indented code block is not a fence). The system prompt requires fenced code, front matter and identifiers to be kept verbatim.
- **Translations live in memory only** (`internal/ui/translate.go`'s `translateState.items`, keys such as `card:<skill>:description`). Nothing is written into a skill folder: no `SKILL.md` edit, no `.kura-meta.json` change, no cache file on disk. The UI never goes blank: while a translation is missing it shows the original text.
- Changing the translation settings bumps `translateState.epoch` and cancels the context of in-flight requests; results that come back afterwards are discarded, never mixed with the new settings.

## 8. The window

Three columns:

1. **Skill roots** (left): the default root plus every folder the user added, each with its enabled-skill count, plus "add folder" (system folder picker). Roots can be dragged to reorder (the default stays first).
2. **Skill cards** (middle): the skills of the selected root. A card shows the name, the source (`owner/repo` or local), the two-line `description` from `SKILL.md`, tags (update available, disabled, trashed, broken metadata, missing `SKILL.md`) and the enable switch. Above the grid: search (name, description and display name), filter tabs All / Enabled / Disabled / Trash with counts ("All" means enabled + disabled and never includes the trash), and the layout switch. Header actions: refresh, check all for updates, import metadata (default root) or remove from list (added roots), install skill.
3. **Detail** (right, only while a skill is selected): name, state, description, metadata, location and the actions. Two tabs: overview and full text. The full text renders the `SKILL.md` Markdown (headings, lists, fenced code, quotes, rules and inline code/bold/italic/strikethrough/links; unknown structures degrade to a paragraph; the source is capped and never makes the UI panic). The text is loaded in the background and cached per skill name; a scan invalidates the cache.

Requirements:

- Skills are sorted by name, case-insensitively; disabled skills are mixed in rather than pushed to the bottom, because state is expressed by the card's switch and tags.
- The selection is tracked **by skill name** (the grid uses an index only as a view detail). Hiding the selected skill with a filter leaves the detail column as it is.
- Column widths are draggable (left 168–420, detail 260–560 DIP, middle at least 300). When the window is too narrow the two side columns give way together without changing the user's preference, and the dragged widths come back when the window grows.
- Card grid or two-line list, chosen by the user and remembered in `config.json` as `view` (`grid` default).
- **Context menus are the system's native menus.** A skill card: open the folder, show it in the file manager, open `SKILL.md` (greyed out when there is none), copy the path, then the actions that fit the card's state (disable, check for updates, move to the trash, enable, restore). Right-clicking a card selects it first, so the menu always acts on the skill the detail column shows. A root: open the folder, copy the path and — for added roots only — remove it from the list; right-clicking a root does not switch to it. State-changing items are greyed out while the skill is busy; moving to the trash and purging ask for confirmation.
- **Appearance** is follow the system (default) / light / dark, applied at once, saved as `theme` in `config.json` and given to MyGo so the native title bar and system menus match. Switching goes through `SetThemeAsync`: the skin changes immediately, the write and `mygo.Theme.SetSource` happen in the background, and a failure is only reported in the status line (the skin is not rolled back). Consecutive switches keep only the newest. `main.go` calls `app.SyncTheme()` before `App.Run`.
- The view switch (`SetViewAsync`) follows the same pattern: change now, write in the background, report failures in the status line only, do not roll back.
- **The window has no system title bar.** kura draws its own bar, 40 DIP tall (the larger of `c.TitleBar().Height` and 40, so the bar does not collapse when the window buttons are hidden), with padding from `c.TitleBar()`'s `Left`/`Right` so it does not collide with the system window buttons. The whole bar drags the window (double-click = maximise/restore) and contains **no interactive elements**, because they would swallow the drag gesture.
- Text the user may want to select and copy lives in a `Selectable()` container, and controls inside such a container must be `Unselectable()`.
- All colours come from `internal/ui/theme.go` (`kuraScheme` / `palette`); view code contains no hex literals. The palette is a near-neutral charcoal base with bordered cards, three grey text levels, mint toggles and one muted periwinkle accent. Saturated indigo/violet is explicitly unwanted.
- **The interface speaks Chinese and English.** The language is a preference (`lang` in `config.json`: `system` | `zh` | `en`), changed from the settings dialog and applied at once; `system` follows the operating system, where any `zh*` locale is Chinese and every other locale (including an empty one) is English. The CLI takes `--lang` and `KURA_LANG` (flag, then environment, then `config.json`, then the system). Texts live in per-package `catalog.go` files and are read with `internal/i18n`; errors carry a catalog key, so `errors.Is` survives a language switch, and a guard test fails while any user-visible Go file still holds a hardcoded Chinese literal. User data (skill names, descriptions, paths, URLs) and language self-names (`简体中文`, `日本語`, …) are never translated.

### UI thread contract (the easiest thing to get wrong)

- Views may be rebuilt several times per event: side effects belong to a handled action or an explicitly guarded task, never to an unconditional render path.
- Slow I/O and heavy computation never run on the UI thread; results are published back with `Window.Update`.
- Elements and widgets are invalid after the next build: store app data, widget state and `ui.Handle`s — never elements.
- After a state change, request a redraw with `Invalidate` instead of writing the model and querying it in the same build.
- A background task records `App.gen` when it starts and, when it publishes, drops the result if `gen != a.gen` (the root changed meanwhile). Every new background path must do the same (`publishAfterWork`, `reload`).
- A widget's bound value is only written at the end of a build or when its response is queried; segment controls use `Track.Changed()` and switches use `Changed()` to see the new value in the same build.

## 9. Command line

`cmd/kura` is a headless binary over the same `internal/store`. `cli.Run` returns an exit code and **never** calls `os.Exit`; `cmd/kura/main.go` owns the exit.

| Command | Behaviour |
| --- | --- |
| `list` / `scan` | every skill of the root (enabled, disabled and trashed) with state, description and metadata |
| `disable NAME` / `enable NAME` | move between the root and `disabled/` |
| `remove NAME [--stamp TS]` / `restore NAME [--stamp TS]` | move into / out of the trash; `--stamp` picks the timestamped copy |
| `import-meta [--dry-run] [--lock PATH]` | fill missing metadata from `.skill-lock.json`; default root only |
| `add --local PATH [--name N]` / `add --github owner/repo [--sub-path P] [--ref R] [--name N]` | install one skill |
| `update NAME` | re-fetch the recorded source and replace the content (snapshot first) |
| `update --check NAME` / `update --check --all` | preview the changes without touching anything; exit 3 when something would change |
| `doctor` | read-only health check |
| `version`, `help` | version, usage |

- Exit codes are fixed: `0` success, `1` operation failed (unknown skill, name clash, IO error, …), `2` usage error (unknown command, flag parse failure, wrong argument count), `3` `doctor` found problems or `update --check` found pending updates.
- `doctor` kinds (a stable contract): `not-a-dir`, `no-meta`, `meta-parse`, `hash-drift`, `no-skillmd`, `leftover`.
- Every command accepts `--json`: machine-readable output on stdout, errors as `{"error": "..."}` on stderr. Fields are only ever added — never renamed, never removed — and the shapes are documented in [docs/note/cli-json-shape.md](docs/note/cli-json-shape.md).
- `--root` / `--home` (and the environment variables `KURA_ROOT` / `KURA_HOME`) may appear before or after the subcommand; flags win over the environment. A `--root` that is an added root uses that root's private state.
- Multiple candidates from one repository are a failure for `add` (exit 1) with the candidate list in the output (`--json`: `{"error", "repo", "candidates"}`); `--sub-path` installs exactly that one.

## 10. Application self-update

kura does not implement downloading, verifying or replacing itself; MyGo's updater plugin does (native update window, no webview).

- `internal/selfupdate.Install()` mounts `native.Plugin` before `mygo.App.Run()` (once per process) with the plugin's own automatic checks turned off (`DisableAutomaticChecks`). kura owns the background check (`internal/selfupdate/watch.go`): about ten seconds after launch and then once a day it asks `mygo.Updater.Check`. A newer version only lights a dot on the check-for-updates button in the left rail; it never opens the update window. The window opens only from that button or the app menu, and either one clears the dot. The preference is `autoCheckUpdates` in `~/.kura/config.json` (default on). A value left in the plugin's `updater.json` by an earlier version is migrated once, before the plugin starts. Turning on the plugin's "automatically download and install updates" option hands scheduling back to the plugin, which then installs silently with neither a window nor a dot.
- `internal/selfupdate.AppMenu()` (from the `WhenReady` callback, on the main thread) builds the app menu: the kura submenu (About, "check for updates", quit) plus the standard Edit and Window roles so the usual shortcuts survive.
- Updates are verified with Ed25519 against the public key compiled in from `mygo.json`'s `updates.publicKey`; the private key exists only as the CI secret `MYGO_UPDATER_PRIVATE_KEY` and must never enter the repository.
- **`mygo.json`'s `version` is the only source of the application version.** `internal/cli` carries its own `Version` constant for the separate CLI binary; it is not part of self-update and is kept in sync by hand. Release tags are `v` + the application version, and `.github/workflows/release.yml` fails when the tag and the version disagree. A release also needs a `## <version>` section in `CHANGELOG.md`, which becomes the release notes shown in the update window.
- The release workflow builds every platform with `go tool mygo build -platform <target> -upload`: installers, update archives, deltas and the `update-<target>.json` manifests, which carry a signature for the archive and for every delta. The manifests go last, so the app never reads a manifest pointing at a missing file. Deltas are built against the last three published versions (`updates.deltas`; `0` disables).
- Development builds and package-manager copies do not self-update; the update window explains why. `internal/cli.Version` is the CLI's own version and is not part of self-update; it is kept in sync with `mygo.json` by hand.

## 11. kura's own directories

```
~/.kura/
  disabled/<skill>/              # disabled skills (folder + .kura-meta.json)
  trash/<skill>/<stamp>/         # removed skills, recoverable
  snapshots/<skill>/<stamp>/     # content snapshots from before an update or a removal
  roots/<id>/                    # disabled/ trash/ snapshots/ for each added root
  config.json                    # roots, preferences (theme: system|light|dark; view: grid|list)
  settings.json                  # translation API and global target language (plaintext key, 0600)
```

Plus, outside `~/.kura`:

- `~/.agents/skills/` — the default root (Windows: `%USERPROFILE%\.agents\skills`);
- `~/.agents/.skill-lock.json` — read-only input for `kura import-meta`;
- `<skill>/.kura-meta.json` — per-skill metadata.

## 12. Verification and acceptance

A change is done only when these are green (the exact commands are in `AGENTS.md`): `gofmt -l .` (empty), `go vet ./...`, `go tool mygo vet .`, `go test ./...`, `CGO_ENABLED=0 go test ./...`, `CGO_ENABLED=0 go build ./...` and `go tool mygo build -platform windows/amd64`.

- Shared state changes are additionally tested with `-race`.
- A layout or platform-behaviour change must be checked in a **real window** (`tools/capture-window.ps1`), not only in tests.
- UI failures are reproduced with `ui.NewTester`, including the extra frames after an element disappears.
- Test isolation is mandatory: tests use roots created with `t.TempDir()` and must never touch the real `~/.agents/skills` or `~/.kura`; git-dependent tests pin `GIT_CONFIG_GLOBAL` and friends; no test may use the network (a GitHub path is tested by pointing `PATH` at an empty directory, or with a local repository rewritten through `insteadOf`).
- In UI tests `App.pickFolder` is a fake and never opens a system dialog, and the three system calls `App.openPath` / `reveal` / `setSource` are replaced with no-ops (`newAppFor`); tests that need to observe them inject `fakeShell` / `recordSource`. Never really open a window or change the system appearance.
- Reports state honestly what was verified and what was not.

## 13. Non-goals

- Editing `SKILL.md` text inside kura
- Syncing to `.claude/skills` or other mirrors
- Uploading a skill back to GitHub
- Skill dependency resolution
- Copying or syncing skills between roots (adding and managing several roots is supported)

## 14. Not implemented, not verified, or limited

- **End-to-end self-update is verified once, on Windows only**: an installed copy went from 0.1.0 to 0.1.1 on 2026-10-13 (manifest discovery, download, Ed25519 verification, replacement, restart). The macOS and Linux copies have not been exercised ([docs/note/updates.md](docs/note/updates.md)).
- **Delta updates are not verified in use**: delta files are produced and published (`kura-<from>-to-<to>-<platform>.delta`), but whether an installed app actually picks a delta over the full archive has not been observed.
- **macOS builds are unsigned and not notarised.** `mygo.json` sets no `macos` section, so MyGo falls back to an ad-hoc signature (`-`) and runs no notary step; no Developer ID signing happens in CI either. macOS will therefore warn before the first launch. Windows builds carry no Authenticode signature, so SmartScreen may warn as well.
- The update preview's line counts do not account for git's `autocrlf`: when the cloned content and the local content differ in line endings, a file is shown as fully changed ([docs/note/m4-preview-notes.md](docs/note/m4-preview-notes.md)).
- The multi-skill install candidate list has only been checked with a 12-candidate headless render and a 3-candidate real window, not with a real repository of ~38 skills ([docs/note/multi-select-install.md](docs/note/multi-select-install.md)).
- **The skills.sh market has no search box yet.** Its routes were checked against the live service on 2026-10-17: `all-time`, `hot` and `trending` all answer 200 with the documented shape, as does `/api/download/{owner}/{repo}/{slug}` (see [docs/note/skills-market.md](docs/note/skills-market.md) for the results and sources). A ranking can hold records that are not `owner/repo` on GitHub (for example `open.feishu.cn`); those are skipped rather than shown as installable.
- Windows device-name handling in `validSkillDirName` is deliberately conservative: on other platforms it falls back to the directory name more often than strictly necessary, and it has not been exercised on other platforms.
