# kura — v1 requirements

A desktop manager for agent skills, built with [MyGo](https://github.com/egoist/mygo)
(native `ui` package, no webview).

## Scope of v1

Manage the skills in one root directory:

- root: `~/.agents/skills` (Windows: `C:\Users\ArnoChen\.agents\skills`)
- one directory per skill, containing `SKILL.md` plus optional files

Other agent roots (`.claude/skills`, `.codex/skills`, arbitrary directories) are
**out of scope for v1**, but every root must be modeled as configuration, not a
constant, so v2 can add more.

## Commands (UI surfaces later; core first)

| Action | Meaning |
| --- | --- |
| List | Every skill in the root, with enabled/disabled state and source metadata |
| Add | Install a skill into the root: from a local directory, or from a GitHub source (`owner/repo`, optional subdirectory and ref) |
| Update | Re-fetch a skill from its recorded source and replace its content (snapshotting the old content first) |
| Remove | Take a skill out of the root into kura's own trash directory (recoverable) |
| Disable | Move a skill's folder from the root into `~/.kura/disabled/`; the UI shows a toggle (off) |
| Enable | Move it back from `~/.kura/disabled/` into the root |
| Restore | Put a trashed (removed) skill back into the root |

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
file is read and never rewritten by kura v1.

`contentHash` is a SHA-256 over the skill folder's files, with paths sorted and
`.kura-meta.json` itself excluded, so an unchanged skill keeps a stable hash and
drift can be detected.

## kura's own directories

```
~/.kura/
  disabled/<skill>/     # disabled skills, folder + .kura-meta.json
  trash/<skill>/        # removed skills with a timestamp
  snapshots/<skill>/<timestamp>/  # pre-update and pre-remove content snapshots
  config.json           # roots, preferences
```

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
