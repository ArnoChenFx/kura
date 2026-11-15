# Changelog

kura follows the [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) format: the version number is the only section heading (`## <version>`). When packaging, the MyGo updater extracts that version's section from this file and shows it as the release notes in the native update window. Therefore **each version's notes must start with `## <version>`** (MyGo also recognizes headings of the form `[<version>]`).

The sole source of the application version is the `version` field in `mygo.json` (injected by MyGo at pack time). The `Version` constant in `internal/cli` is the CLI binary's own version number; it is not part of self-update, and must be kept in sync with `mygo.json` by hand when releasing.

## [Unreleased]

## 0.1.23

- Skills that are links (Windows junctions, directory symlinks) now behave like ordinary skills: they appear in the list and the detail pane, count toward totals, can be hashed and snapshotted, and support enable / disable / move to trash / restore, as well as update and restore of historical versions. A snapshot is always a through-copy of the real content; permanent delete only removes the link itself; a broken link is ignored as "directory does not exist". Update and historical restore write into the real directory the link points to; the link itself is left in place.
- The left rail lists only skill directories that currently exist: a directory whose folder has disappeared no longer takes a row. If the selected directory disappears, the first existing directory is selected; if none exist, a short hint is shown. Directories are still recorded in `config.json`, and reappear when you refresh after they come back.
- The skills area can be sorted by modified time (newest first, the default), with sort by name kept as a toggleable option. Clicking the clock icon again reverses the direction. The sort order is stored as `sort` in `config.json`. The CLI `list --json` output gains a `modTime` field, but the output order is still by name.
- Upgraded MyGo to 0.9.0.

## 0.1.22

- Dependency upgrade: MyGo 0.6.3 → 0.8.3. Window, scrolling, input, and update behavior are unchanged. None of the APIs kura uses were removed or renamed, and no application code was changed.
- Scrolling back up after reaching the bottom responds immediately (from upstream 0.7.0, a reverse wheel movement is no longer absorbed by a journey that has not finished).
- Windows and macOS installers have new file names: `kura-setup-<version>-<arch>.exe`, `kura-<version>-<arch>.dmg` (the old names were `kura Setup <version> amd64.exe` and `kura <version> <arch>.dmg`). The Release also includes `install.ps1` and `SHA256SUMS`. Already-installed copies keep self-updating as before.

## 0.1.21

- kura now checks for app updates on every launch: one check about 10 seconds after startup, skipped only if the last successful check was within 10 minutes (this blocks repeated requests piled up by rapid restarts or crash restarts). Later in the same run it is still once every 24 hours. Previously the first check was scheduled as "last successful check + 24 hours", so a check earlier that day meant no further check that day, and a restart would not show the blue dot either.
- If the version has changed (a new version was just installed) or the record has no version, a check still runs immediately after launch, same as in 0.1.18.

## 0.1.20

- Upgraded MyGo to 0.6.3: on Windows, when the title bar is hidden and material is enabled, a maximized window no longer shows the system's title-bar buttons; the hidden scrollbar track also lets clicks through; and the last column of a table no longer has a draggable edge.
- On Windows, text fields exclusively handle keys, so Ctrl+V in the Edit menu pastes only once; inline transforms of large text search each paragraph only once, which makes scrolling cheaper.

## 0.1.19

- Upgraded MyGo to 0.6.2: on Linux, Chinese punctuation between Latin words is drawn with that language's font; a two-finger trackpad pinch is reported as an input event; and on Wayland a second instance can forward the activation token to the already-running window.

## 0.1.18

- Fixed a bug where the update hint never appeared after a new version was installed: after a manual check and upgrade, the plugin's freshly refreshed check time pushed the next background check 24 hours out, and a restart would not check either.
- Background checks now record the app version at check time. If the recorded version does not match the currently running version, or an older version left a record with no version, that timestamp is no longer trusted, and a check runs again about 10 seconds after launch.

## 0.1.17

- Upgraded MyGo to 0.6.0, adopting the framework's partial redraws, incremental layout, and paused painting for hidden windows.
- Every scroll area now uses MyGo's native smooth scrolling and default step size, keeping jump-to-top on search, board switching, directory scroll positions, and reduce-motion behavior.
- Scanning, installing, translation, and other background tasks uniformly use the MyGo loading plugin's circular indicator, which follows the light/dark theme and the system's reduce-motion preference.
- The Linux package no longer depends on WebKitGTK, which it does not need; only the GTK required by the native UI is kept.

## 0.1.16

- Fixed a misalignment between the left/right pane splitters and the background edges, keeping the existing drag ranges for resizing the panes.
- Changed the trash empty-state copy to "No skills in the trash".
- Cleaned up outdated development notes and task scripts, keeping the current verification, API, and release docs.

## 0.1.15

- Fixed the layout of the action buttons at the bottom of skill details, keeping clear grouping and spacing in a narrow pane and in both the Chinese and English UIs.
- Lowered the request rate of the free translation services; after being rate-limited they cool down as the service requires and prompt you to retry later. Switching skills or turning translation off stops stale requests, and completed sections are kept so you can resume.

Free translation is still subject to the service's regional/IP quota limits, so 429s cannot be guaranteed to be avoided entirely. Anonymous Microsoft translation remains experimental.

## 0.1.14

Adds key-free translation, optional batch updates, and restoring from historical snapshots, and improves long-document reading and browsing across multiple directories.

- Key-free Google Translate is provided by default, and you can also choose experimental Microsoft Translator or a custom OpenAI-compatible API. Existing custom settings are kept; switching services cancels in-flight requests and clears the cache.
- Long-document translation runs in the background in sections, shows progress, and supports stopping and resuming. Completed sections are kept in memory, and unusually large documents are size-limited at the read stage.
- Each directory separately remembers its search, filters, selected skill copy, and scroll position. Local search supports the source repository and the path.
- Batch updates support per-item checkboxes, keep success/failure results, and retry only the failed items. Locally modified skills are unchecked by default, with an explicit warning.
- Adds historical snapshots, per-file text diffs, and confirmed restore. The current version is saved before restore; if the preview is stale or the restore fails, the current content is protected.
- The Settings and History panels keep their action buttons visible in smaller windows. The small bar under the view, filter, and theme-toggle buttons is removed; the selected background is kept.

Known limitations: Google translation of short text and Markdown has been verified against the live service. The anonymous Microsoft API returned 401 in the current verification environment and has not yet been verified successfully, so it is marked experimental. On a connection failure the original text is kept, and you can manually choose another translation service.

## 0.1.13

Scrolling feels better, the top of the window takes less space, and browsing a large skill library or switching directories is faster.

- Shorter mouse-wheel steps and smoother scrolling; the trackpad keeps native continuous scrolling. Search or switching boards jumps back to the top immediately.
- The top toolbar is tightened to leave more room for skill cards. Actions and filters in a narrow window wrap automatically, and the full directory path can be viewed on hover.
- The local list loads by visible range; long names and sources no longer squeeze the action area. Search results are cached, and after switching directories the current directory's content is shown first.
- The skill market can search already-loaded entries by name and source repository; matches from "Load more" are added automatically.
- Install shows the download, parse, and write phases. An update check can be cancelled, and switching directories stops the previous check.
- Fixed a bug where a completed task from an old directory could interfere with the new directory's state or close a newly opened review panel.
- Fixed a bug on Windows where a root path that incorrectly pointed at a regular file was treated as an empty directory, and an update check was falsely reported as successful.

## 0.1.12

The skill library, skill market, and app logo are redesigned, with a graphite-black theme and a paper-white silver-gray theme, and selection and actions for same-named skill copies are fixed.

- **Updated visual hierarchy**: titles, the toolbar, filters, cards, and the detail pane are reorganized; the app logo is now silver-gray and graphite black.
- **A purpose-built light theme**: a white title and detail pane, a silver-gray canvas and sidebar, and clean white cards. Fine strokes, light shadows, and a raised selected state distinguish interaction states.
- **Subtle motion**: short transitions are added for card hover, button press, selection changes, and content entering, honoring the system's reduce-motion preference.
- **Same-named copies are no longer confused**: enabled, disabled, and trashed skills that share a name each keep their own selection, control identity, and full-text cache. After a move or restore completes, the detail pane follows the result; if you change the selection or close the detail pane in the meantime, your new choice is kept.
- **Disabled copies are removed correctly**: moving to the trash from a disabled skill's detail pane or context menu moves only that disabled copy, and a snapshot is saved first.
- **Theme switches keep the scroll position**: a long list stays at its current position when switching between dark and light themes.

## 0.1.11

Background checks no longer open the update window on their own: when a new version is found, only a blue dot lights up on the "Check for updates" button in the left rail. The update window opens only when you click the button or use the app menu.

- **A background check that finds a new version only lights the blue dot**: previously, when the plugin found a new version in the background (and "Automatically download and install updates in the future" was not checked) it would open the update window itself, stealing focus and flashing the taskbar. The plugin's scheduler is now turned off, and background checks are handled by kura: one check about 10 seconds after launch, then once a day. When a new version is found, the blue dot at the top-right of the left-rail "Check for updates" button lights up, and the hover tip is "A new version of kura is available".
- **Opening the window clears the blue dot**: clicking "Check for updates" in the left rail or "Check for Updates…" in the app menu opens the update window as usual (Install Update / Remind Me Later / Skip This Version), and the blue dot is cleared at the same time. The blue dot is also cleared when a background check finds that you are already up to date.
- **A skipped version no longer lights the blue dot**: after you choose "Skip This Version" in the update window, that same version no longer lights the blue dot; newer versions are still announced as usual.
- **The "Check for updates automatically" preference moves to `config.json`**: stored as `autoCheckUpdates` (on by default); the switch in Settings is unchanged. On the first launch after upgrading, the old `automaticChecks` value in the plugin's `updater.json` is migrated over, and `config.json` is the source of truth after that.
- **"Automatically download and install updates in the future" still works as before**: once it is checked, the plugin takes over background checks, downloads and installs silently, and the update takes effect at the next launch; this mode does not light the blue dot.

## 0.1.10

Skill market: browse skills.sh rankings inside kura and install a skill into the current skills directory.

- **Skill market page**: the globe button on the right of the title bar replaces the three-pane view with the market page; "Back to skills" returns to the three panes. Three boards (**All time / Hot / Trending**) switch in a segmented control, each with its own pagination ("Load more") and its own cache (switching back does not refetch). If a load fails, the reason is kept and it can be retried. All time is the default board.
- **Skill detail page**: avatar, name, source, install count, the description from the SKILL.md frontmatter, two external links (opened in the system browser in the background, without blocking the UI), the file list inside the skill, and the rendered full text of SKILL.md. The "Original" / "Translated" switch shares the same global switch, translation settings, and cache rules as the local detail page; market translation keys are prefixed with `market:` / `mdoc:`, so a local skill of the same name does not share translations.
- **Install from the market**: goes through the new `store.AddSkillByName`: the repository is cloned only once, and the skill is found by name using the same rules as `npx skills add --skill` (case-insensitive: the `name` in SKILL.md wins; if there is no `name`, the directory name is used; a skill at the repository root uses the repository name). The metadata written on install (source, commit, subdirectory tree hash) is identical to what the "Install skill" dialog writes, so later "Check for updates" works as usual. Only one install runs at a time; if you switch directories mid-install, the old result does not change the selection.
- **Some ranking entries cannot be installed**: some board entries have a source that is not in `owner/repo` shape (for example `open.feishu.cn`). They are not GitHub repositories, and the market skips them rather than showing them as installable. 28 of the 200 entries on the first All time page are of this kind.
- **Nothing is written to disk**: both the boards and the skill snapshots live in memory only; they are not written into any skill directory, and no cache file is kept.
- **Fix**: `frontmatterDescription` previously treated only `>` and `|` as block indicators, so `>-` / `|-` / `|+` were taken as literal values (the detail-page description showed up as ">-"). In real data `>-` is the most common; it is now treated as a block indicator.
- **Fix**: the market page applied left/right padding outside the scroll container, so content ran to the right edge of the scroll area and was covered by the scrollbar drawn in the rightmost 12 DIP. Padding is now applied to the scroll content (same as the local detail page), 61px on each side, and the scrollbar sits in the padding gutter.

## 0.1.9

Three UI fixes and a self-update preference, plus a mygo upgrade to v0.5.5.

- **Right-click only opens the menu, and no longer also switches the right pane**: right-clicking a skill card or list row now only opens that item's own context menu, and no longer replaces the right pane with its details. Actions in the menu already target the skill that was right-clicked, regardless of which skill the right pane is showing, so changing the selection had no purpose; "right-click once and the right pane jumps there" was a side effect nobody asked for. Selection is still decided only by a left click (grid cells and list rows).

- **New "Check for updates automatically" switch in Settings (on by default)**: the preference's source of truth lives in **the updater plugin's `updater.json`** (not `config.json`): read via `updater.AutomaticChecks`, written via `updater.SetAutomaticChecks`, so the default is naturally "on", and it stays consistent with the plugin's own scheduling: after it is turned off, the plugin no longer checks in the background. The dialog re-reads the source of truth every time it opens, and does not use a possibly stale in-memory copy.

- **When a background check finds a new version, a small blue dot appears at the top-right of the "Check for updates" button in the left rail**: the plugin has no "new version found" callback, and no mode that "the background found a new version but only hints, without opening a window", so after every plugin check kura asks once more itself (`mygo.Updater.Check`, 30-second timeout): it deduplicates with `LastCheck`, runs only one query at a time, and on query failure **leaves** the blue-dot state alone (a single network blip must not take back a hint the user has already seen). The dot is an absolutely positioned child; it does not change the button's position or size, and it does not eat clicks. Its color comes from kura's palette (Light `#2f7dd1` / Dark `#5aa9f0`). Clicking "Check for updates" clears the blue dot, and the hover tip also changes to "A new version of kura is available".

- **"Updates" is its own group in the settings dialog**: a separator line divides it off, and the translation group's messages (the API-key note, and the "incomplete" reminder) stay above the separator so they are not read as talking about automatic update checks.

- **Dependencies**: mygo is upgraded to v0.5.5 (previously v0.5.1). The four packages kura uses (`ui`, `ui/markdown`, `i18n`, `plugins/updater`) only add exported API, never remove it, so no application code changes are needed; new APIs such as `ui.SplitPercent` and `Element.Blur` are unused for now.

## 0.1.8

Two parts of the UI are rebuilt the official MyGo way: Markdown rendering on the Full text page, and all UI copy.

- **Full text page uses the official Markdown view**: the "Full text" tab on the detail page no longer uses the handwritten renderer (the old `internal/ui/markdown.go`, 1066 lines) and switches to `ui/markdown` from MyGo v0.5.1 (full CommonMark 0.31.2 + GFM). Tables are no longer "a pile of vertical bars", task lists render as real checkboxes, code blocks have syntax highlighting and a "Copy" button, and inline code and links sit correctly on one line; footnotes, formulas, and image descriptions are also readable. kura keeps only the two things the official view does not do, and that we need: skipping the YAML frontmatter at the start of `SKILL.md`, and a truncation cap for very large sources. The render theme is rebuilt only when the window appearance (Light / Dark / Auto) changes, not on every frame.

- **Copy moved to official MyGo catalogs**: all UI copy moves from the custom `internal/i18n` plus each of the four packages' own `catalog.go` into `locales/en.json` and `locales/zh-Hans.json` (ARB + ICU MessageFormat), with typed accessor functions generated by `go tool mygo generate`. All 421 strings were migrated one-to-one; **the Chinese and English wording is unchanged, character for character** (a regression script guards this). Errors are still identified by key (`errors.Is` holds across languages), while the text is fetched in the current language each time, so an error produced by a background task and shown only after you switch language is also a sentence in the new language.

- **Incidental gains and fixes**: MyGo's own widgets, menus, and folder pickers now follow the interface language as well (Chinese users see the Chinese labels instead of "Done"/"Cancel"), which the custom setup could not do. Three other fixes: on Traditional Chinese systems (`zh-TW`/`zh-Hant`/`zh-HK`) the window language incorrectly fell back to English (while the CLI on the same machine was Chinese); the "no hardcoded Chinese in non-test code" guard was half idle (the directory walk treated the root entry as a hidden directory and skipped it, so not a single file was checked); and a dead string retired with the old renderer ("N more lines not shown").

- **Dependencies**: mygo is upgraded to v0.5.1 (previously v0.4.8). CI adds `go tool mygo i18n check`.

## 0.1.7

Listing no longer computes content hashes, and repositories whose root is the skill can now be installed.

- **Faster scans**: skills with a large number of files in a "skill-collection repository" no longer slow down list refreshes: scanning reads only metadata, and no longer hashes the entire directory for every skill (including disabled and trashed ones). `kura doctor` still computes the hash on demand when checking for content drift; that behavior is unchanged.
- **Fix: disabled skills were falsely reported as missing metadata**: a disabled skill whose metadata is intact is no longer reported by `kura doctor` as "the metadata has no contentHash" (a false positive with exit code 3).
- **Fix: repositories whose root is the skill could not be installed**: for repositories that put `SKILL.md` at the repository root (such as `dualface/ste-zh`), clicking "Install" in the UI used to report "store: no such folder in the repository: .". A subpath of `.` is now treated the same as leaving it empty (check out the whole repository); the CLI's `--sub-path .` works the same way.

## 0.1.6

The detail page no longer shows a content hash.

- **Removed the "Hash" row from the detail pane**: that hash in the right-pane metadata was an internal fingerprint and had no value for use (version comparison looks at source / Commit / time; content drift is reported by `kura doctor` and the "Check for updates" review panel). The whole row is removed, in both the Chinese and English UIs. The "Changed locally" warning on the review panel is kept.
- **Docs updated**: outdated wording in the README and `docs/note/updates.md` is updated (the self-update path has been verified end-to-end on Windows; the mygo version number is aligned), and the corresponding items in `REQUIREMENTS.md` are updated.

## 0.1.5

The interface is bilingual (Chinese and English): the first launch follows the system language, using Chinese when the system is Chinese and English otherwise.

- **Bilingual UI**: both the window and the command line support Chinese and English. The interface language is `lang` in `~/.kura/config.json` (`system` | `zh` | `en`, default `system`). It can be switched at any time from "Language" in the settings dialog, and **takes effect as soon as it is selected** ("Save" only applies to the translation-API group). `system` means: use Chinese when the system locale starts with `zh`, and English for everything else (including empty, `C`, `POSIX`, `ja`, `de`, ...).
- **CLI**: `kura` gains `--lang system|zh|en` and the `KURA_LANG` environment variable (precedence: flag → environment variable → `config.json` → system locale). `--help`, lists, doctor, and every error message follow the language.
- **Copy centralized**: all UI copy has moved into each package's `catalog.go` (Chinese and English, with matching keys) and is read through the new `internal/i18n`. The Chinese values are **character-for-character identical** to what they were before the change: the Chinese interface you already know has not changed by a single character. Errors are now "errors with a key": the wording changes after you switch language, but `errors.Is` checks still work (this also fixes the duplicated `store: store:` prefix in older error messages).
- **Not translated**: user data such as skill names, descriptions, paths, and URLs, as well as a language's own name (the endonyms for Simplified Chinese, Japanese, English, ...), is always shown as-is.
- **Regression guards**: a new test fails if any hardcoded Chinese UI copy remains in a non-test source file, listing each line number; another test ensures the Chinese and English catalogs have exactly the same keys.
- **Fix**: the window tests in `internal/selfupdate` previously asserted the Chinese UI without pinning the language, so they would fail in an English environment after bilingual support landed. They now pin the language the same way the other packages do.

## 0.1.4

Install several skills from one repository at once, documentation is brought up to date, and MyGo is upgraded to v0.4.8.

- **Install several skills from one repository in one go**: when a repository contains multiple skills, the install dialog lists the candidates as a checklist (none selected by default, with Select all / Deselect all; it scrolls beyond 8 rows). The primary button shows "Install N selected". The same batch clones once, takes the tree once, and does one sparse checkout; if one item fails (name conflict, the directory is missing upstream, or the subdirectory is invalid) only that item fails, and the rest still install.
- **Default install name comes from the `name` field in `SKILL.md`**: the priority is "the name you typed → the `name` in the frontmatter → the source directory name". When the repository root is the skill, it falls back to the repository name (random clone-directory names no longer appear). If the name cannot be used as a directory name (contains `<>:"|?*`, control characters, a trailing space or dot, a Windows device name, and so on) the next candidate is substituted silently.
- **Old candidates are invalidated when the repository changes**: the candidate list belongs to the `owner/repo`+branch that discovered it; changing the repository field discards the old list. If a discovery result arrives late and the form has already changed, the whole result is discarded, so the wrong repository is never installed quietly.
- **Bilingual README and a rewritten REQUIREMENTS**: added `README.md` (English) and `README-zh.md` (Chinese); `REQUIREMENTS.md` is rewritten as 14 sections, covering multiple roots, auto-discovery, batch install, permanent deletion, translation, self-update, the CLI, and verification requirements, with unimplemented / unverified / limited parts listed in a section of their own.
- **Upgraded MyGo v0.4.6 → v0.4.8** (v0.4.7/v0.4.8 are incremental-only changes).
- **Docs and comments no longer contain machine-local paths**: `C:\Users\<name>`, `E:\Project`, and machine-specific tool names are replaced with generic forms such as `~`, `%USERPROFILE%`, and `<repo-root>`; real repository names in examples are also replaced with generic placeholders.
- **`tools/` keeps only reusable scripts**: the screenshot script, `demo-discover`, the watch script, and the review brief are kept, and `tools/README.md` is added. One-off logs, screenshots, patches, and backups are deleted after use; `.gitignore` ignores `tools/*.log` and `tools/**/*.png`.
- **Docs no longer use hard line wraps**: every Markdown file is one paragraph or one list item per line (they used to be hard-wrapped at column 80, which inserted an extra space into Chinese sentences that should have stayed joined).

## 0.1.3

The first round of UI feedback is in: pane widths are draggable, roots can be reordered, full text can be translated, and trash items can be deleted permanently.

- **Draggable pane widths**: the splitters between the left rail and the detail pane can now be dragged to resize them (left rail 168–420, detail pane 260–560). When the window is too narrow, both side panes yield together to the available width; when the window grows again, the widths you dragged to are restored automatically.
- **Left-rail footer collapsed to one row**: the appearance switch and icon buttons sit side by side.
- **Title-bar icon**: the top-left of the window now uses kura's own icon.
- **Skills sorted by name**: cards are always ordered by name; disabled skills no longer sink to the bottom of the list.
- **Trash items can be "Delete permanently…"**: with a confirmation dialog. A content snapshot is always written first; if the snapshot is invalid (emptied, the path is a symlink, or the content does not match), it is rewritten, and if it still cannot be written the delete is aborted and the trash copy is kept.
- **Full text can be translated section by section**: the "Full text" tab in the detail pane can translate SKILL.md section by section into the target language. Code blocks are kept intact and are not split in the middle. Translations live in memory only and **are not written** to any skill file.
- **Root list can be reordered by dragging**: the default root stays first.
- **Agent skill directories on this machine are discovered automatically at startup** (Claude Code, Codex, Cursor, and so on) and added to the list. Directories you have removed are not added back later.
- **Fixes (six found in review)**: concurrent directory operations could overwrite each other's `config.json` (auto-discovered directories could be dropped permanently); "Delete permanently" could escape the trash and delete a directory outside it when the skill name contained `../` or similar; a snapshot that had been emptied or replaced with a symlink could still be treated as already snapshotted; indented Markdown code blocks were mistaken for fenced ones, which degraded segmentation; after translation settings changed, old tasks kept sending requests and mixed old and new settings; when the window narrowed, already-widened side panes were not clamped, which squeezed the middle pane away. All six have regression tests.

## 0.1.2

Update checks are faster and use less bandwidth, and both the UI and the CLI now report "changed locally".

- **Update checks use git object hashes**: they no longer depend on the local clock or file times. Checks go coarse-to-fine and stop as soon as a verdict is possible. First, `git ls-remote` compares the upstream commit; only if it changed is a tree-only shallow clone (`--depth 1 --filter=blob:none --no-checkout`, no file contents) used to compare the skill directory's tree hash; only if that still differs is the directory sparsely checked out and compared file by file. When upstream has not changed, **no files are downloaded**.
- **One check shared per repository**: `update --check --all`, "Check for updates" on the review panel, and batch updates all group by repository (URL + ref) and share one `ls-remote`, one clone, and one sparse checkout, instead of repeating that work for every skill.
- **Local-edit notice**: when installed content does not match the hash recorded at install/update time, the check is marked "Changed locally" instead of being misreported as an update. Applying an update still overwrites those edits; a snapshot is taken first, as before.
- **New metadata field `source.tree`**: records the tree hash of the skill directory at install time. Old metadata (and metadata imported from `.skill-lock.json`) that lacks this field automatically falls back to a file-by-file comparison; **no migration is required**.
- **Fix**: after an update, skills with a `subPath` had `source.commit` in their metadata written as an empty string, which made the next check fall back to a file-by-file comparison.
- **CLI**: JSON from `update --check` now includes `basis` (`commit` / `tree` / `content`, explaining what the verdict is based on) and `localModified`. Text output notes the reason after "up to date", and reports local edits separately.

## 0.1.1

A UI overhaul and skill-text translation, plus fixes for three real defects on Windows.

- **Cross-drive move fix**: on Windows, disabling a skill or moving it to the trash failed outright with `The system cannot move the file to a different disk drive` when the skills root and `~/.kura` were on different drives (for example skills on E: and kura data on C:). Moves now prefer a same-drive rename, and fall back across drives to "copy into a temporary directory on the destination volume → rename into place on that volume → delete the source". On failure the temporary directory is cleaned up and the source is left untouched.
- **Install and update no longer flash a console window**: calling git on Windows no longer opens a new command-line window (`CREATE_NO_WINDOW`).
- **Auto-discovery of multi-skill repositories**: when installing a skill from a GitHub repository, if the repository root has no `SKILL.md` (for example repositories like `mattpocock/skills` that keep skills under `skills/<category>/<skill>/`), kura now discovers every skill in the repository. A single candidate is installed directly; several candidates are listed for you to pick from (CLI exit code 1, `--json` returns a structured `candidates` list).
- **Custom title bar**: the window no longer uses the system title bar. kura draws its own draggable top bar that maximizes on double-click, while keeping the system's minimize/maximize/close buttons. "Check for updates" moves to the bottom-left of the left rail.
- **Detail pane**: metadata and location text can be selected and copied; a GitHub source can be clicked to open it in the browser. Two new tabs, "Overview" / "Full text", are added; the Full text page renders the Markdown of `SKILL.md` (headings, lists, code blocks, block quotes, links).
- **Layout mode**: the skills area can switch between a card grid and a two-line list; the choice is remembered.
- **Settings and translation**: a new settings dialog configures an OpenAI-compatible translation API and a global target language. Detail and card descriptions can switch between "Original" / "Translated". Translations are for display only, kept in memory, and **never modify any file in the skill directory**; the API key is stored in plain text at `~/.kura/settings.json` (file mode 0600).

## 0.1.0

kura's first release: a desktop app and companion CLI for managing agent skills (Skills), built with a MyGo native UI (no webview).

- **Skill library**: scans the skills root and shows each skill's name, description, and enabled state; filter by searching name or description.
- **Enable / disable**: moves the skill folder between the root and `~/.kura/disabled/`. State is location; nothing is ever copied.
- **Remove / restore**: moves the skill into `~/.kura/trash/` (restorable), automatically snapshotting to `~/.kura/snapshots/` first.
- **Import metadata**: reads `~/.agents/.skill-lock.json` (read-only, never rewritten) and records the install source (a GitHub repository and the path inside it, or a local path), the installer plugin name, and the install time in the skill's `.kura-meta.json`; skills that already have metadata are always skipped.
- **Update preview**: for skills with an install source, pulls upstream again and shows a file-level diff of what would change; review file by file before writing. A snapshot is taken first, as with updates.
- **Doctor**: `kura doctor` checks the root, the lock file, and metadata for common problems.
- **CLI**: a headless entry point with the same capabilities as the desktop app (`kura list / add / update / disable / enable / remove / restore / import-meta / doctor`), emitting machine-readable JSON.
- **Self-update** (app-level): the app menu provides "Check for Updates…"; a background check also runs once a day. Download, Ed25519 signature verification, replacement, and relaunch are all handled by the native MyGo update window. Updates are published as GitHub Releases tagged `vX.Y.Z`; see [docs/note/updates.md](docs/note/updates.md).
