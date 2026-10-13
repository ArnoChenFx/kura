# kura — v1 requirements

A desktop manager for agent skills, built with [MyGo](https://github.com/egoist/mygo)
(native `ui` package, no webview).

## Scope of v1

Manage the skills in one or more **skill roots**:

- the default root: `~/.agents/skills` (Windows: `C:\Users\ArnoChen\.agents\skills`);
  it is always present and always first in the list
- any other folder the user adds in the UI (for example a project's
  `.claude/skills`); the list lives in `~/.kura/config.json` under `roots`
- one directory per skill, containing `SKILL.md` plus optional files

Roots are configuration, never constants. Every root keeps its own
disabled/trash/snapshots state, so same-named skills in different roots never
interfere. Removing a root from the list only edits `config.json`: the folder
and the skills in it are left untouched. Copying or syncing skills *between*
roots is out of scope.

## Commands (UI surfaces later; core first)

Every action works on the selected root.

| Action | Meaning |
| --- | --- |
| List | Every skill in the root, with enabled/disabled state, description and source metadata |
| Add | Install a skill into the root: from a local directory, or from a GitHub source (`owner/repo`, optional subdirectory and ref) |
| Update | Re-fetch a skill from its recorded source and replace its content (snapshotting the old content first) |
| Remove | Take a skill out of the root into kura's own trash directory (recoverable) |
| Disable | Move a skill's folder from the root into `~/.kura/disabled/`; the UI shows a toggle (off) |
| Enable | Move it back from `~/.kura/disabled/` into the root |
| Restore | Put a trashed (removed) skill back into the root |
| Add root / Remove root | Add any folder to the list of roots, or take a non-default root out of the list (never touches the folder) |

Disable/enable is a **move**, never a copy or a flag inside the skill.

## Metadata: `.kura-meta.json`

Written inside each managed skill folder, next to `SKILL.md`:

```json
{
  "metaVersion": 1,
  "id": "yixin-api",
  "displayName": "yixin-api",
  "source": {
    "type": "github",
    "url": "https://github.com/billionsintelligence/yixin-api.git",
    "ref": "main",
    "subPath": "skills/yixin-api",
    "commit": "abc1234"
  },
  "pluginName": "mattpocock-skills",
  "installedAt": "2026-06-21T04:42:05.890Z",
  "updatedAt": "2026-09-06T13:34:08.150Z",
  "contentHash": "sha256:...",
  "notes": ""
}
```

`source.type` is `local` (with `path`) or `github`. `pluginName` is imported from
the sibling `~/.agents/.skill-lock.json` when the skill is listed there; the lock
file is read and never rewritten by kura v1. It only describes the default root,
so "import metadata" is offered for the default root only: the window hides the
action for other roots, and `store.Config.ImportMeta` and `kura import-meta`
refuse an added root (exit code 1).

`contentHash` is a SHA-256 over the skill folder's files, with paths sorted and
`.kura-meta.json` itself excluded, so an unchanged skill keeps a stable hash and
drift can be detected.

## kura's own directories

```
~/.kura/
  disabled/<skill>/     # disabled skills, folder + .kura-meta.json
  trash/<skill>/        # removed skills with a timestamp
  snapshots/<skill>/<timestamp>/  # pre-update and pre-remove content snapshots
  roots/<id>/           # the same three directories, for each added (non-default) root
  config.json           # roots, preferences (theme: system | light | dark)
```

The default root keeps the layout above, so existing data needs no migration.
`<id>` is the first 12 hex digits of the SHA-256 of the normalised root path.
The CLI's `--root` reaches the same state when it points at a root from the list.

## Window

Three columns:

1. **Skill roots** (left): the default root plus every folder the user added,
   each with its enabled-skill count, and "Add folder" (system folder picker).
2. **Skill cards** (middle): a responsive grid with several cards per row. A card
   shows the name, the source (`owner/repo` or local), the two-line
   `description` from `SKILL.md`, tags (update available, disabled, broken
   metadata, missing `SKILL.md`) and the enable switch. Above the grid: search
   (name and description) and filter tabs All / Enabled / Disabled / Trash, each
   with its count ("All" does not include the trash). Header actions: refresh,
   check all for updates, import metadata (default root) or remove from list
   (other roots), and **Install skill**.
3. **Detail** (right, only while a card is selected): name, state, description,
   metadata, location and the actions (check update, disable/enable, trash or
   restore).

**Context menus** are the system's native menus, opened with the secondary mouse
button (they do not show up in screenshots):

- a skill card: open the folder, show it in the file manager, open `SKILL.md`
  (greyed out when the skill has none), copy the path; below a separator the
  actions that fit the card's state: an enabled skill can be disabled, checked for
  updates or moved to the trash, a disabled one enabled or moved to the trash, a
  trashed one restored. Right-clicking a card selects it first, so the menu always
  acts on the skill the detail column shows. The state-changing items are greyed
  out while the skill is busy; moving to the trash still asks for confirmation.
- a root in the left column: open the folder, copy the path and, for an added root
  only, remove it from the list (never touches the folder). Right-clicking a root
  does not switch to it.

**Appearance** is a switch at the foot of the left column: follow the system
(default) / light / dark. The choice applies at once, is saved as `theme` in
`~/.kura/config.json` (`system`, `light` or `dark`; a missing or unknown value
means `system`) and is also given to MyGo, so the native title bar and system
menus match.

**Install skill** is a dialog: a GitHub repository (`owner/repo` or a pasted URL,
optional subdirectory, ref and name) or a local folder (system picker, optional
name). It installs into the selected root.

Look: a dark, low-contrast palette taken from the reference screenshot (near
neutral charcoal base, slightly lighter bordered cards, three grey text levels,
mint toggles) with one muted periwinkle accent used sparingly; the light skin
keeps the same hues, with the accent darkened just enough for contrast and kept
as desaturated as the dark one. Saturated indigo/violet is explicitly unwanted.
All colours live in `internal/ui/theme.go`.

Interaction feedback: a card is ringed once, by its own border (the grid draws
nothing around it), and brightens on hover; the toggle's track tints and its knob
gets a halo on hover, stretches while pressed, and rings on keyboard focus; the
pointer becomes a hand over a toggle.

## Non-goals for v1

- Editing `SKILL.md` text inside kura
- Syncing to `.claude/skills` or other mirrors (see open question 2)
- Uploading a skill back to GitHub
- Skill dependency resolution

## Milestones

1. **Core store** — scan/parse/write meta, hashes, config, enable/disable/remove/restore/add/update as a package with tests. No UI.
2. **List UI** — native window listing skills with toggle + search.
3. **Actions UI** — add/update/remove flows, progress, errors.
4. **Diff + polish** — show upstream diff (github.com/egoist/godiff), icons, packaging.
