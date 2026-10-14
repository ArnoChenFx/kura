package cli

// catalog.go — cli 包的用户可见文案（中文 / 英文两套）。
//
// 规则见 internal/i18n 的包注释：中文取值与改造前的输出**逐字一致**，英文
// 取值自然、简短；两种语言的占位符（%s/%d）顺序相同，整句进目录、不做拼接。
// key 形如 `<领域>.<分类>.<名字>`：cli.usage.* 是 --help 全文，cli.err.* 是
// 错误与用法提示，cli.flag.* 是各 flag 的说明，cli.doctor.* 是 doctor 的
// 明细文案，cli.list.* / cli.check.* / cli.status.* 是人类可读输出。

import (
	"github.com/ArnoChenFx/kura/internal/i18n"
)

// 中文文案：与改造前的输出逐字一致。
var catalogZH = map[string]string{
	// ---------------- cli.go：--help 全文 ----------------
	"cli.usage.text": `kura —— agent skills 管理器（无界面命令行）

用法:
  kura <命令> [参数] [flags]

命令:
  list                 列出全部技能（enabled + disabled + trashed）
  scan                 同 list（别名，习惯上配合 --root 覆盖根目录）
  disable <NAME>       禁用技能（移动到 <home>/disabled/，纯移动）
  enable <NAME>        启用技能（从 disabled/ 移回根目录）
  remove <NAME> [--stamp TS]
                       移入回收站（先写快照）；--stamp 指定回收站时间戳目录名
  restore <NAME> [--stamp TS]
                       从回收站恢复；--stamp 省略时取最新一次
  import-meta [--dry-run] [--lock PATH]
                       依据根目录同级的 .skill-lock.json 补写缺失的
                       .kura-meta.json；--dry-run 只分析不写文件；
                       只适用于默认目录，界面里追加的目录会被拒绝（退出码 1）
  add --local PATH [--name N]
                       从本地目录安装技能
  add --github owner/repo [--sub-path P] [--ref R] [--name N]
                       从 GitHub 仓库安装技能；不给 --sub-path 时自动发现
                       仓库内的技能：唯一候选直接安装，多个候选列出清单
                       并以退出码 1 失败，请用 --sub-path 指定其中一个
  update <NAME>        按元数据记录的来源重新拉取并替换内容（先快照）
  update --check <NAME>
                       预览 NAME 的更新会改什么（不修改任何文件）；
                       有待更新退出码 3，无变化退出码 0
  update --check --all
                       预览根目录全部技能的更新（每个技能一段结果）
  doctor               只读体检：缺元数据 / 元数据损坏 / 内容漂移 /
                       缺 SKILL.md / disabled 与 trash 中的残留条目
  version              打印版本

全局 flags（可写在任何子命令之后）:
  --json        输出 JSON；出错时错误信息以 {"error": "..."} 输出到 stderr
  --root PATH   技能根目录（默认 $KURA_ROOT 或 ~/.agents/skills）
  --home PATH   kura 数据目录（默认 $KURA_HOME 或 ~/.kura）
  --lang LANG   界面语言：system | zh | en（默认 $KURA_LANG，其次
                config.json 的 lang，最后跟随系统）

退出码:
  0  成功
  1  操作失败（技能不存在、同名冲突、IO 错误等）
  2  用法错误（未知命令、flag 解析失败、参数个数不对）
  3  doctor 发现问题（明细见其输出）

JSON 输出形状（--json 时）:
  list/scan   {"root": string, "skills": [{name, state, hasSkillMd, dir,
              sourceType?, sourceURL?, ref?, subPath?, pluginName?,
              updatedAt?, metaError?}]}
  变更命令     {"id": string, "state": string, "dir": string,
              "snapshotDir"?: string}
  doctor      {"root": string, "problems": [{kind, skill, detail}]}
              kind ∈ not-a-dir | no-meta | meta-parse | hash-drift |
                      no-skillmd | leftover
  import-meta {"root": string, "lockPath": string, "dryRun": bool,
              "wouldImport"?: [string], "imported"?: [string]}
  add（多候选） {"error": string, "repo": string,
              "candidates": [string]}（输出到 stderr，退出码 1）
  update --check NAME
              {"skill": string, "sourceType": string, "commit"?: string,
              "hasChanges": bool, "basis": string, "localModified": bool,
              "oldHash": string, "newHash": string,
              "files": [{path, status, added, removed, oldHash?, newHash?}],
              "unchanged": int}
              status ∈ added | changed | removed；files 只含差异文件，
              无变化时为 []。退出码：有待更新 3，无变化 0。
              basis ∈ commit | tree | content：git 来源先用 git ls-remote
              比对上游 commit（commit），commit 变了再比对该技能目录的树哈希
              （tree），这两种都不下载文件；其余情况才逐文件比较（content）。
              localModified 为 true 表示已安装内容被改过（上游没变时不算
              有更新，但应用更新会覆盖这些改动）。
  update --check --all
              {"root": string,
              "results": [{skill, error?, check?}]}
              check 的形状同上；任一技能有待更新退出码 3。同一仓库的技能
              共用一次检查与 clone。
`,
	"cli.err.badFlag":        "kura: 参数错误: %v（用 kura --help 查看用法）\n",
	"cli.err.badFlagJSON":    "参数错误: %v",
	"cli.err.badLang":        "kura: 参数错误: 无法识别的界面语言 %q（只能是 system、zh 或 en；用 kura --help 查看用法）\n",
	"cli.err.unknownCommand": "kura: 未知命令 %q\n\n",

	// ---------------- cli.go：flag 说明 ----------------
	"cli.flag.json": "以 JSON 输出（错误也以 JSON 输出到 stderr）",
	"cli.flag.root": "技能根目录（默认 $KURA_ROOT 或 ~/.agents/skills）",
	"cli.flag.home": "kura 数据目录（默认 $KURA_HOME 或 ~/.kura）",
	"cli.flag.lang": "界面语言：system | zh | en（默认 $KURA_LANG，其次 config.json 的 lang，最后跟随系统）",

	// ---------------- output.go ----------------
	"cli.err.jsonEncode": "cli: JSON 编码失败",

	// ---------------- commands.go：list ----------------
	"cli.err.listPositional": "list 不接受位置参数（得到 %q）",
	"cli.list.empty":         "（没有技能）",
	"cli.list.trashed":       "\t(回收站条目，内容在时间戳子目录中)",
	"cli.list.noSkillMD":     "\t⚠ 缺少 SKILL.md",
	"cli.list.metaBroken":    "\t⚠ 元数据损坏: %s",
	"cli.list.noMeta":        "\t(无元数据)",

	// ---------------- commands.go：disable / enable ----------------
	"cli.err.usageToggle":  "用法: kura %s <NAME>",
	"cli.err.actionFailed": "%s 失败: %v",

	// ---------------- commands.go：remove / restore ----------------
	"cli.flag.stampRemove":  "回收站/快照时间戳目录名（默认当前时间）",
	"cli.err.usageRemove":   "用法: kura remove <NAME> [--stamp TS]",
	"cli.err.removeFailed":  "remove 失败: %v",
	"cli.flag.stampRestore": "恢复回收站中指定时间戳的版本（默认最新一次）",
	"cli.err.usageRestore":  "用法: kura restore <NAME> [--stamp TS]",
	"cli.err.restoreFailed": "restore 失败: %v",

	// ---------------- commands.go：import-meta ----------------
	"cli.flag.dryRun":               "只分析将要导入哪些技能，不写文件",
	"cli.flag.lock":                 "lock 文件路径（默认 <root>/../.skill-lock.json）",
	"cli.err.importMetaPositional":  "import-meta 不接受位置参数",
	"cli.err.importMetaFailed":      "import-meta 失败: %v: %s",
	"cli.err.importMetaFailedPlain": "import-meta 失败: %v",
	"cli.importMeta.imported":       "已导入",
	"cli.importMeta.wouldImport":    "将要导入",
	"cli.importMeta.lockMissing":    "（不存在）",
	"cli.importMeta.nothing":        "没有需要导入的技能",
	"cli.importMeta.count":          "%s %d 个技能:\n",

	// ---------------- commands.go：add ----------------
	"cli.flag.addLocal":     "本地技能目录",
	"cli.flag.addGithub":    "GitHub 仓库，格式 owner/repo",
	"cli.flag.addSubPath":   "仓库内技能子目录（github 来源可选；省略时自动发现仓库内的技能，多个候选时必填其一）",
	"cli.flag.addRef":       "分支/标签/commit（github 来源可选）",
	"cli.flag.addName":      "安装后的技能名（默认取来源目录名）",
	"cli.err.addPositional": "add 不接受位置参数",
	"cli.err.addUsage":      "用法: kura add --local PATH 或 kura add --github owner/repo（必须且只能提供一个来源）",
	"cli.err.addFailed":     "add 失败: %v",

	// ---------------- commands.go：update / update --check ----------------
	"cli.flag.check":              "预览更新会改什么（不修改任何文件）；有待更新时退出码 3",
	"cli.flag.all":                "配合 --check：检查根目录全部技能",
	"cli.err.usageUpdate":         "用法: kura update <NAME>",
	"cli.usage.checkAllWithName":  "kura: 用法: kura update --check --all（不能同时给 NAME）\n",
	"cli.err.checkAllWithName":    "用法: kura update --check --all（不能同时给 NAME）",
	"cli.usage.checkNeedsName":    "kura: 用法: kura update --check <NAME> | kura update --check --all\n",
	"cli.err.checkNeedsName":      "用法: kura update --check <NAME> | kura update --check --all",
	"cli.err.updateFailed":        "update 失败: %v",
	"cli.err.checkFailed":         "check 失败: %v",
	"cli.err.scanRootFailed":      "扫描根目录失败: %v",
	"cli.checkAll.failed":         "[%s] 检查失败: %s\n",
	"cli.checkAll.summary":        "共 %d 个技能，%d 个有待更新\n",
	"cli.check.noteCommit":        "（上游 commit 未变）",
	"cli.check.noteTree":          "（上游该目录未变）",
	"cli.check.upToDate":          "%s: 已是最新%s\n",
	"cli.check.localModifiedNote": "  注意: 本地内容与安装时不一致（被改过）\n",
	"cli.check.changes":           "%s: %d 个文件有变化（新增 %d · 修改 %d · 删除 %d）\n",
	"cli.check.hash":              "  哈希: %s -> %s\n",
	"cli.check.commit":            "  上游 commit: %s\n",
	"cli.check.localModifiedWarn": "  注意: 本地内容已被改过，应用更新会覆盖这些改动（旧内容在快照中）\n",
	"cli.status.added":            "新增",
	"cli.status.changed":          "修改",
	"cli.status.removed":          "删除",

	// ---------------- doctor.go ----------------
	"cli.err.doctorPositional":        "doctor 不接受位置参数",
	"cli.term.rootDir":                "根目录",
	"cli.doctor.rootUnreadable":       "根目录不可读: %v",
	"cli.doctor.rootFileNotDir":       "根目录下存在同名普通文件，技能必须是目录",
	"cli.doctor.metaMissing":          "%s 的技能缺少 .kura-meta.json（可用 kura import-meta 从 lock 文件补齐）",
	"cli.doctor.skillMDMissing":       "%s 的技能缺少 SKILL.md",
	"cli.doctor.noContentHash":        "元数据缺少 contentHash，无法检测内容漂移",
	"cli.doctor.hashDrift":            "内容与元数据不符: 当前 %s，记录 %s（可用 kura update 刷新）",
	"cli.doctor.scanDisabledFailed":   "扫描 disabled 目录失败: %v",
	"cli.doctor.disabledLeftover":     "disabled 中有已禁用的技能（可用 kura enable %s 恢复）",
	"cli.doctor.scanTrashFailed":      "扫描 trash 目录失败: %v",
	"cli.doctor.trashEntryUnreadable": "回收站条目不可读: %v",
	"cli.doctor.trashLeftover":        "回收站中保留着 %s/%s（可用 kura restore %s --stamp %s 恢复）",
	"cli.doctor.readFailed":           "读取 %s 失败: %v",
	"cli.doctor.sideFileNotDir":       "%s 下存在同名普通文件，技能必须是目录",
	"cli.doctor.allGood":              "一切正常（没有发现问题）",
	"cli.doctor.problemCount":         "发现 %d 个问题:\n",
}

// 英文文案：key 与中文一一对应（含同样的占位符）。
var catalogEN = map[string]string{
	// ---------------- cli.go：--help 全文 ----------------
	"cli.usage.text": `kura — a manager for agent skills (headless command line)

Usage:
  kura <command> [args] [flags]

Commands:
  list                 List all skills (enabled + disabled + trashed)
  scan                 Same as list (alias; handy with --root to override the root)
  disable <NAME>       Disable a skill (move it to <home>/disabled/, a pure move)
  enable <NAME>        Enable a skill (move it back from disabled/ to the root)
  remove <NAME> [--stamp TS]
                       Move to the trash (snapshot first); --stamp names the
                       trash timestamp folder
  restore <NAME> [--stamp TS]
                       Restore from the trash; --stamp defaults to the newest
  import-meta [--dry-run] [--lock PATH]
                       Fill in missing .kura-meta.json from the
                       .skill-lock.json next to the root; --dry-run only
                       reports, it writes nothing; default folder only, added
                       folders are rejected (exit 1)
  add --local PATH [--name N]
                       Install a skill from a local folder
  add --github owner/repo [--sub-path P] [--ref R] [--name N]
                       Install a skill from a GitHub repository; without
                       --sub-path the skills inside are discovered: a single
                       candidate installs directly, several candidates are
                       listed and fail with exit 1, pick one with --sub-path
  update <NAME>        Pull the recorded source again and replace the content
                       (snapshot first)
  update --check <NAME>
                       Preview what updating NAME would change (writes nothing);
                       exit 3 when an update is pending, 0 when up to date
  update --check --all
                       Preview every skill in the root (one block per skill)
  doctor               Read-only check: missing metadata / corrupt metadata /
                       content drift / missing SKILL.md / leftovers in disabled
                       and trash
  version              Print the version

Global flags (may follow any subcommand):
  --json        Output JSON; on errors the message goes to stderr as {"error": "..."}
  --root PATH   Skills root folder (default $KURA_ROOT or ~/.agents/skills)
  --home PATH   kura data folder (default $KURA_HOME or ~/.kura)
  --lang LANG   Interface language: system | zh | en (default $KURA_LANG, then
                config.json, then the system locale)

Exit codes:
  0  success
  1  operation failed (skill not found, name conflict, I/O error, ...)
  2  usage error (unknown command, bad flag, wrong number of arguments)
  3  doctor found problems (see its output)

JSON shapes (with --json):
  list/scan   {"root": string, "skills": [{name, state, hasSkillMd, dir,
              sourceType?, sourceURL?, ref?, subPath?, pluginName?,
              updatedAt?, metaError?}]}
  changes     {"id": string, "state": string, "dir": string,
              "snapshotDir"?: string}
  doctor      {"root": string, "problems": [{kind, skill, detail}]}
              kind in not-a-dir | no-meta | meta-parse | hash-drift |
                      no-skillmd | leftover
  import-meta {"root": string, "lockPath": string, "dryRun": bool,
              "wouldImport"?: [string], "imported"?: [string]}
  add (many candidates) {"error": string, "repo": string,
              "candidates": [string]} (stderr, exit 1)
  update --check NAME
              {"skill": string, "sourceType": string, "commit"?: string,
              "hasChanges": bool, "basis": string, "localModified": bool,
              "oldHash": string, "newHash": string,
              "files": [{path, status, added, removed, oldHash?, newHash?}],
              "unchanged": int}
              status in added | changed | removed; files holds only the
              differing files, [] when there is nothing to do. Exit code: 3
              when an update is pending, 0 when up to date.
              basis in commit | tree | content: git sources compare the upstream
              commit with git ls-remote (commit), when it changed the tree hash
              of the skill folder (tree); neither downloads files, anything else
              compares file by file (content). localModified true means the
              installed content was edited (an unchanged upstream is not an
              update, but applying one overwrites those edits).
  update --check --all
              {"root": string,
              "results": [{skill, error?, check?}]}
              check has the shape above; exit 3 if any skill has an update.
              Skills from the same repository share one check and clone.
`,
	"cli.err.badFlag":        "kura: argument error: %v (see kura --help)\n",
	"cli.err.badFlagJSON":    "argument error: %v",
	"cli.err.badLang":        "kura: argument error: unknown interface language %q (use system, zh or en; see kura --help)\n",
	"cli.err.unknownCommand": "kura: unknown command %q\n\n",

	// ---------------- cli.go：flag 说明 ----------------
	"cli.flag.json": "Output JSON (errors go to stderr as JSON too)",
	"cli.flag.root": "Skills root folder (default $KURA_ROOT or ~/.agents/skills)",
	"cli.flag.home": "kura data folder (default $KURA_HOME or ~/.kura)",
	"cli.flag.lang": "Interface language: system | zh | en (default $KURA_LANG, then config.json, then the system locale)",

	// ---------------- output.go ----------------
	"cli.err.jsonEncode": "cli: failed to encode JSON",

	// ---------------- commands.go：list ----------------
	"cli.err.listPositional": "list takes no positional arguments (got %q)",
	"cli.list.empty":         "(no skills)",
	"cli.list.trashed":       "\t(trash entry, its content lives in the timestamp subfolder)",
	"cli.list.noSkillMD":     "\t⚠ SKILL.md missing",
	"cli.list.metaBroken":    "\t⚠ corrupt metadata: %s",
	"cli.list.noMeta":        "\t(no metadata)",

	// ---------------- commands.go：disable / enable ----------------
	"cli.err.usageToggle":  "usage: kura %s <NAME>",
	"cli.err.actionFailed": "%s failed: %v",

	// ---------------- commands.go：remove / restore ----------------
	"cli.flag.stampRemove":  "Timestamp folder name for the trash and the snapshot (defaults to now)",
	"cli.err.usageRemove":   "usage: kura remove <NAME> [--stamp TS]",
	"cli.err.removeFailed":  "remove failed: %v",
	"cli.flag.stampRestore": "Restore the trash copy with this timestamp (defaults to the newest)",
	"cli.err.usageRestore":  "usage: kura restore <NAME> [--stamp TS]",
	"cli.err.restoreFailed": "restore failed: %v",

	// ---------------- commands.go：import-meta ----------------
	"cli.flag.dryRun":               "Only report which skills would be imported, write nothing",
	"cli.flag.lock":                 "Path of the lock file (default <root>/../.skill-lock.json)",
	"cli.err.importMetaPositional":  "import-meta takes no positional arguments",
	"cli.err.importMetaFailed":      "import-meta failed: %v: %s",
	"cli.err.importMetaFailedPlain": "import-meta failed: %v",
	"cli.importMeta.imported":       "imported",
	"cli.importMeta.wouldImport":    "would import",
	"cli.importMeta.lockMissing":    " (missing)",
	"cli.importMeta.nothing":        "no skills to import",
	"cli.importMeta.count":          "%s %d skills:\n",

	// ---------------- commands.go：add ----------------
	"cli.flag.addLocal":     "Local skill folder",
	"cli.flag.addGithub":    "GitHub repository, owner/repo",
	"cli.flag.addSubPath":   "Skill folder inside the repository (optional for github; when omitted the skills are discovered, and one must be picked if there are several)",
	"cli.flag.addRef":       "Branch/tag/commit (optional for github)",
	"cli.flag.addName":      "Skill name after installing (defaults to the source folder name)",
	"cli.err.addPositional": "add takes no positional arguments",
	"cli.err.addUsage":      "usage: kura add --local PATH or kura add --github owner/repo (exactly one source must be given)",
	"cli.err.addFailed":     "add failed: %v",

	// ---------------- commands.go：update / update --check ----------------
	"cli.flag.check":              "Preview what an update would change (writes nothing); exit code 3 when an update is pending",
	"cli.flag.all":                "With --check: check every skill in the root folder",
	"cli.err.usageUpdate":         "usage: kura update <NAME>",
	"cli.usage.checkAllWithName":  "kura: usage: kura update --check --all (NAME cannot be given here)\n",
	"cli.err.checkAllWithName":    "usage: kura update --check --all (NAME cannot be given here)",
	"cli.usage.checkNeedsName":    "kura: usage: kura update --check <NAME> | kura update --check --all\n",
	"cli.err.checkNeedsName":      "usage: kura update --check <NAME> | kura update --check --all",
	"cli.err.updateFailed":        "update failed: %v",
	"cli.err.checkFailed":         "check failed: %v",
	"cli.err.scanRootFailed":      "failed to scan the root folder: %v",
	"cli.checkAll.failed":         "[%s] check failed: %s\n",
	"cli.checkAll.summary":        "%d skill(s) in total, %d with an update pending\n",
	"cli.check.noteCommit":        " (upstream commit unchanged)",
	"cli.check.noteTree":          " (upstream folder unchanged)",
	"cli.check.upToDate":          "%s: up to date%s\n",
	"cli.check.localModifiedNote": "  note: the local content differs from the installed one (edited)\n",
	"cli.check.changes":           "%s: %d files changed (added %d · modified %d · removed %d)\n",
	"cli.check.hash":              "  hash: %s -> %s\n",
	"cli.check.commit":            "  upstream commit: %s\n",
	"cli.check.localModifiedWarn": "  note: the local content was edited; applying the update overwrites those edits (the old content is in the snapshot)\n",
	"cli.status.added":            "added",
	"cli.status.changed":          "modified",
	"cli.status.removed":          "removed",

	// ---------------- doctor.go ----------------
	"cli.err.doctorPositional":        "doctor takes no positional arguments",
	"cli.term.rootDir":                "the root folder",
	"cli.doctor.rootUnreadable":       "the root folder is unreadable: %v",
	"cli.doctor.rootFileNotDir":       "a plain file shares the name of a skill; skills must be folders",
	"cli.doctor.metaMissing":          "the skill in %s has no .kura-meta.json (kura import-meta can fill it in from the lock file)",
	"cli.doctor.skillMDMissing":       "the skill in %s has no SKILL.md",
	"cli.doctor.noContentHash":        "the metadata has no contentHash, so content drift cannot be detected",
	"cli.doctor.hashDrift":            "the content does not match the metadata: now %s, recorded %s (kura update refreshes it)",
	"cli.doctor.scanDisabledFailed":   "failed to scan the disabled folder: %v",
	"cli.doctor.disabledLeftover":     "disabled holds a disabled skill (kura enable %s brings it back)",
	"cli.doctor.scanTrashFailed":      "failed to scan the trash folder: %v",
	"cli.doctor.trashEntryUnreadable": "the trash entry is unreadable: %v",
	"cli.doctor.trashLeftover":        "the trash still holds %s/%s (kura restore %s --stamp %s brings it back)",
	"cli.doctor.readFailed":           "failed to read %s: %v",
	"cli.doctor.sideFileNotDir":       "a plain file under %s shares the name of a skill; skills must be folders",
	"cli.doctor.allGood":              "all good (nothing to report)",
	"cli.doctor.problemCount":         "%d problem(s) found:\n",
}

func init() { i18n.RegisterZH(catalogZH); i18n.RegisterEN(catalogEN) }
