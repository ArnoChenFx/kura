package store

// catalog.go — store 包的用户可见文案（中文 / 英文两套）。
//
// 规则见 internal/i18n 的包注释：中文取值与改造前的界面文字**逐字一致**，
// 英文取值自然、简短；两种语言的占位符（%s/%d）顺序相同，整句进目录、不做
// 拼接。key 形如 `<领域>.<分类>.<名字>`。
//
// 少见的例外（有意留在代码里，不进目录）：
//   - translate.go 的 systemPrompt 是发给模型的指令，不是界面文案：它必须
//     稳定、可测试，不能随界面语言变。
//   - translate.go 里拼在 systemPrompt 后面的「目标语言：」同属该指令。

import (
	"github.com/ArnoChenFx/kura/internal/i18n"
)

// 中文文案：与改造前的界面文字逐字一致。
var catalogZH = map[string]string{
	// ---------------- config.go ----------------
	"store.err.unknownSkill":  "store: 未找到该技能",
	"store.err.emptyRoot":     "store: root 不能为空",
	"store.err.emptyHome":     "store: home 不能为空（可改用 DefaultConfig）",
	"store.err.userHome":      "store: 无法确定用户主目录",
	"store.err.createDir":     "store: 创建目录 %s 失败",
	"store.err.readFile":      "store: 读取 %s 失败",
	"store.err.parseJSON":     "store: 解析 %s 失败（JSON 已损坏）",
	"store.err.marshalConfig": "store: 序列化配置失败",
	"store.err.writeFile":     "store: 写入 %s 失败",

	// ---------------- library.go ----------------
	"store.err.rootExists":    "store: 该目录已在列表中",
	"store.err.unknownRoot":   "store: 列表中没有该目录",
	"store.err.defaultRoot":   "store: 默认技能目录不能移除",
	"store.err.notDir":        "store: 路径不存在或不是文件夹",
	"store.err.unknownTheme":  "store: 外观只能是 system、light 或 dark",
	"store.err.unknownView":   "store: 排列模式只能是 grid 或 list",
	"store.err.resolvePath":   "store: 无法解析路径 %s",
	"store.err.posOutOfRange": "store: 位置 %d 超出列表范围",

	// ---------------- lang.go ----------------
	"store.err.unknownUILang": "store: 界面语言只能是 system、zh 或 en",

	// ---------------- cli_support.go ----------------
	"store.err.invalidName":    "store: 非法的技能名",
	"store.err.snapshot":       "store: 快照 %s 失败",
	"store.err.invalidStamp":   "store: 非法的时间戳目录名 %q",
	"store.err.trashExists":    "store: 回收站中已存在 %s/%s",
	"store.err.createTrashDir": "store: 创建回收站目录失败",
	"store.err.moveToTrash":    "store: 移入回收站失败 (%s)",

	// ---------------- ops.go ----------------
	"store.err.enableFailed":        "store: 启用 %s 失败",
	"store.err.disableFailed":       "store: 禁用 %s 失败",
	"store.err.skillExists":         "store: 根目录已存在同名技能 %s",
	"store.err.disabledExists":      "store: disabled 中已存在同名技能 %s",
	"store.err.createRootDir":       "store: 创建根目录失败",
	"store.err.restoreFailed":       "store: 恢复 %s 失败",
	"store.err.unknownSkillInTrash": "store: 未找到该技能: %s（回收站中不存在）",
	"store.err.invalidStampName":    "store: 非法的技能名: 时间戳目录名 %q",
	"store.err.escapesTrash":        "store: 非法的技能名: %q 解析后不在回收站内，未删除",
	"store.err.trashRead":           "store: 读取回收站中的 %s 失败",
	"store.err.purgeFailed":         "store: 永久删除 %s 失败",
	"store.err.hashTrashCopy":       "store: 计算回收站副本 %s 的内容哈希失败，未删除",
	"store.err.snapshotIsSymlink":   "store: 快照位置 %s 是符号链接，不能当作快照，未删除 %s",
	"store.err.snapshotIsFile":      "store: 快照位置 %s 被文件占用，未删除 %s",
	"store.err.snapshotCheck":       "store: 检查快照位置 %s 失败，未删除 %s",
	"store.err.snapshotBeforePurge": "store: 永久删除前快照 %s 失败，未删除",
	"store.err.snapshotVerify":      "store: 永久删除前快照 %s 校验失败，未删除",
	"store.err.noSnapshotPath":      "store: 找不到可用的快照位置 (%s/%s)，未删除",
	"store.err.noSubdir":            "store: 目录下没有子目录",

	// ---------------- add.go ----------------
	"store.err.addSource":        "store: LocalPath 与 Repo 必须且只能提供一个",
	"store.err.badRepo":          "store: Repo 格式应为 owner/repo",
	"store.err.badSubPath":       "store: SubPath 不能以 / 开头或包含 ..",
	"store.err.addOptionsNil":    "store: AddOptions 不能为空",
	"store.err.sourceNoSkillMD":  "store: 来源目录缺少 SKILL.md: %s",
	"store.err.resolveLocalPath": "store: 解析路径失败",
	"store.err.sourceUnreadable": "store: 来源目录不可读",
	"store.err.sourceNotDir":     "store: 来源不是目录: %s",
	"store.err.deriveSkillName":  "store: 无法从来源推导技能名: %s",
	"store.err.installSkill":     "store: 安装 %s 失败",
	"store.err.badSkillSubPath":  "store: 技能 %s 的子目录不能以 / 开头或包含 ..",
	"store.word.repoRoot":        "仓库根",

	// ---------------- update.go / preview.go ----------------
	// 缺元数据的整句文案：更新与检查更新各一句（见 sourceMeta）。
	"store.err.metaMissingUpdate":  "store: %s 缺少 .kura-meta.json，无法更新",
	"store.err.metaMissingCheck":   "store: %s 缺少 .kura-meta.json，无法检查更新",
	"store.err.metaUnreadable":     "store: %s 的元数据不可读",
	"store.err.metaNoLocalPath":    "store: 元数据缺少 local 来源路径",
	"store.err.localSourceGone":    "store: 本地来源不存在: %s",
	"store.err.metaNoGithubURL":    "store: 元数据缺少 github 来源 URL",
	"store.err.unknownSourceType":  "store: 未知来源类型 %q",
	"store.err.prepareContent":     "store: 准备新内容失败",
	"store.err.replaceAndRestore":  "store: 替换失败且自动恢复失败（快照在 %s）",
	"store.err.replaceRestored":    "store: 替换失败（已从快照恢复）",
	"store.err.updateMeta":         "store: 更新元数据失败",
	"store.err.upstreamDirMissing": "store: 上游目录不存在: %s",

	// ---------------- upstream.go ----------------
	"store.err.gitTimeout":      "store: git 操作超时",
	"store.err.gitTimeoutAfter": "store: git 操作超时（%s 超过 %s）",
	"store.err.gitFailed":       "store: git %s 失败: %s",
	"store.err.noDefaultBranch": "store: 远端 %s 没有默认分支",
	"store.err.noSuchRef":       "store: 远端 %s 不存在分支或标签 %s",
	"store.err.tempDir":         "store: 创建临时目录失败",
	"store.err.noDirInRepo":     "store: 仓库内不存在目录 %s",
	"store.err.sparseCheckout":  "store: 准备稀疏检出失败",

	// ---------------- discover.go ----------------
	"store.err.tooManySkills":      "store: 仓库内技能候选过多",
	"store.err.tooManySkillsLimit": "store: 仓库内技能候选过多（上限 %d 个）",
	"store.err.tooManyCandidates":  "store: 仓库 %s 里有 %d 个技能，请用 --sub-path 指定其中一个：",
	"store.err.repoDirMissing":     "store: 仓库目录不存在: %s",
	"store.term.truncated":         "…（已截断）",

	// ---------------- meta.go ----------------
	"store.err.parseMeta":   "store: 解析 %s 失败（JSON 损坏）",
	"store.err.emptyMeta":   "store: 不能写入空元数据",
	"store.err.marshalMeta": "store: 序列化元数据失败",

	// ---------------- importmeta.go ----------------
	"store.err.parseFile":         "store: 解析 %s 失败",
	"store.err.importDefaultOnly": "store: .skill-lock.json 只描述默认技能目录，导入元数据只适用于默认目录",
	"store.err.importMetaWrite":   "store: 写入 %s 的元数据失败",

	// ---------------- move.go ----------------
	"store.err.cleanStaging":      "store: 清理跨卷移动的临时目录 %s 失败",
	"store.err.crossVolumeCopy":   "store: 跨卷移动 %s：复制到 %s 失败（原目录未改动）",
	"store.err.crossVolumeRename": "store: 跨卷移动 %s：落位 %s 失败（原目录未改动）",
	"store.err.crossVolumeSource": "store: 跨卷移动完成，但删除源目录 %s 失败（内容已在 %s）",

	// ---------------- hash.go / scan.go / fsutil.go / preview.go ----------------
	"store.err.walkDir":  "store: 遍历 %s 失败",
	"store.err.readDir":  "store: 读取目录 %s 失败",
	"store.err.copyFile": "store: 复制 %s 失败",

	// ---------------- settings.go ----------------
	"store.err.unknownLang":     "store: 不支持的翻译目标语言",
	"store.err.notConfigured":   "store: 翻译未配置：请先在设置里填写 API 地址、密钥与模型",
	"store.err.noSettings":      "store: 尚未保存任何设置",
	"store.err.emptySettings":   "store: 设置不能为空",
	"store.err.marshalSettings": "store: 序列化设置失败",

	// ---------------- translate.go ----------------
	"store.err.translateRequest":   "store: 构造翻译请求失败",
	"store.err.translateEndpoint":  "store: 构造翻译请求失败（请检查 API 地址 %s）",
	"store.err.translateDo":        "store: 翻译请求失败",
	"store.err.translateRead":      "store: 读取翻译响应失败",
	"store.err.translateStatus":    "store: 翻译接口返回 %d: %s",
	"store.err.translateParse":     "store: 解析翻译响应失败（不是合法 JSON）",
	"store.err.translateNoChoices": "store: 翻译响应里没有 choices（服务端未返回译文）",
	"store.err.translateEmpty":     "store: 翻译响应内容为空",

	// ---------------- 目标语言的自称（两种界面语言下都一样，见 langLabel）----
	"store.langName.en":   "English",
	"store.langName.zhCN": "简体中文",
	"store.langName.enUS": "English (US)",
	"store.langName.enGB": "English (UK)",
	"store.langName.ja":   "日本語",
	"store.langName.ko":   "한국어",
	"store.langName.fr":   "Français",
	"store.langName.de":   "Deutsch",
	"store.langName.es":   "Español",
	"store.langName.pt":   "Português",
	"store.langName.ru":   "Русский",
}

// 英文文案：key 与中文一一对应（含同样的占位符）。
var catalogEN = map[string]string{
	// ---------------- config.go ----------------
	"store.err.unknownSkill":  "store: skill not found",
	"store.err.emptyRoot":     "store: root must not be empty",
	"store.err.emptyHome":     "store: home must not be empty (use DefaultConfig)",
	"store.err.userHome":      "store: cannot locate the user home folder",
	"store.err.createDir":     "store: failed to create folder %s",
	"store.err.readFile":      "store: failed to read %s",
	"store.err.parseJSON":     "store: failed to parse %s (corrupt JSON)",
	"store.err.marshalConfig": "store: failed to encode the config",
	"store.err.writeFile":     "store: failed to write %s",

	// ---------------- library.go ----------------
	"store.err.rootExists":    "store: that folder is already in the list",
	"store.err.unknownRoot":   "store: that folder is not in the list",
	"store.err.defaultRoot":   "store: the default skills folder cannot be removed",
	"store.err.notDir":        "store: the path does not exist or is not a folder",
	"store.err.unknownTheme":  "store: the theme must be system, light or dark",
	"store.err.unknownView":   "store: the view must be grid or list",
	"store.err.resolvePath":   "store: failed to resolve the path %s",
	"store.err.posOutOfRange": "store: position %d is out of range",

	// ---------------- lang.go ----------------
	"store.err.unknownUILang": "store: the UI language must be system, zh or en",

	// ---------------- cli_support.go ----------------
	"store.err.invalidName":    "store: invalid skill name",
	"store.err.snapshot":       "store: failed to snapshot %s",
	"store.err.invalidStamp":   "store: invalid timestamp folder name %q",
	"store.err.trashExists":    "store: the trash already contains %s/%s",
	"store.err.createTrashDir": "store: failed to create the trash folder",
	"store.err.moveToTrash":    "store: failed to move (%s) to the trash",

	// ---------------- ops.go ----------------
	"store.err.enableFailed":        "store: failed to enable %s",
	"store.err.disableFailed":       "store: failed to disable %s",
	"store.err.skillExists":         "store: a skill named %s already exists in the root folder",
	"store.err.disabledExists":      "store: a skill named %s already exists in disabled",
	"store.err.createRootDir":       "store: failed to create the root folder",
	"store.err.restoreFailed":       "store: failed to restore %s",
	"store.err.unknownSkillInTrash": "store: skill not found: %s (not in the trash)",
	"store.err.invalidStampName":    "store: invalid skill name: timestamp folder name %q",
	"store.err.escapesTrash":        "store: invalid skill name: %q resolves outside the trash, nothing deleted",
	"store.err.trashRead":           "store: failed to read %s from the trash",
	"store.err.purgeFailed":         "store: failed to delete %s permanently",
	"store.err.hashTrashCopy":       "store: failed to hash the trash copy %s, nothing deleted",
	"store.err.snapshotIsSymlink":   "store: the snapshot path %s is a symlink, so it cannot serve as a snapshot; %s not deleted",
	"store.err.snapshotIsFile":      "store: the snapshot path %s is taken by a file; %s not deleted",
	"store.err.snapshotCheck":       "store: failed to check the snapshot path %s; %s not deleted",
	"store.err.snapshotBeforePurge": "store: failed to snapshot %s before deleting, nothing deleted",
	"store.err.snapshotVerify":      "store: the snapshot of %s failed verification, nothing deleted",
	"store.err.noSnapshotPath":      "store: no free snapshot path (%s/%s), nothing deleted",
	"store.err.noSubdir":            "store: the folder has no subfolders",

	// ---------------- add.go ----------------
	"store.err.addSource":        "store: provide exactly one of LocalPath or Repo",
	"store.err.badRepo":          "store: Repo must look like owner/repo",
	"store.err.badSubPath":       "store: SubPath must not start with / or contain ..",
	"store.err.addOptionsNil":    "store: AddOptions must not be nil",
	"store.err.sourceNoSkillMD":  "store: the source folder has no SKILL.md: %s",
	"store.err.resolveLocalPath": "store: failed to resolve the path",
	"store.err.sourceUnreadable": "store: the source folder is not readable",
	"store.err.sourceNotDir":     "store: the source is not a folder: %s",
	"store.err.deriveSkillName":  "store: cannot derive a skill name from the source: %s",
	"store.err.installSkill":     "store: failed to install %s",
	"store.err.badSkillSubPath":  "store: the subfolder of skill %s must not start with / or contain ..",
	"store.word.repoRoot":        "the repository root",

	// ---------------- update.go / preview.go ----------------
	"store.err.metaMissingUpdate":  "store: %s has no .kura-meta.json, cannot update",
	"store.err.metaMissingCheck":   "store: %s has no .kura-meta.json, cannot check for updates",
	"store.err.metaUnreadable":     "store: the metadata of %s is unreadable",
	"store.err.metaNoLocalPath":    "store: the metadata has no local source path",
	"store.err.localSourceGone":    "store: the local source is gone: %s",
	"store.err.metaNoGithubURL":    "store: the metadata has no github source URL",
	"store.err.unknownSourceType":  "store: unknown source type %q",
	"store.err.prepareContent":     "store: failed to prepare the new content",
	"store.err.replaceAndRestore":  "store: the swap failed and the automatic restore failed too (snapshot at %s)",
	"store.err.replaceRestored":    "store: the swap failed (restored from the snapshot)",
	"store.err.updateMeta":         "store: failed to update the metadata",
	"store.err.upstreamDirMissing": "store: no such upstream folder: %s",

	// ---------------- upstream.go ----------------
	"store.err.gitTimeout":      "store: git operation timed out",
	"store.err.gitTimeoutAfter": "store: git operation timed out (%s took longer than %s)",
	"store.err.gitFailed":       "store: git %s failed: %s",
	"store.err.noDefaultBranch": "store: the remote %s has no default branch",
	"store.err.noSuchRef":       "store: the remote %s has no branch or tag %s",
	"store.err.tempDir":         "store: failed to create a temp folder",
	"store.err.noDirInRepo":     "store: no such folder in the repository: %s",
	"store.err.sparseCheckout":  "store: failed to set up the sparse checkout",

	// ---------------- discover.go ----------------
	"store.err.tooManySkills":      "store: too many skill candidates in the repository",
	"store.err.tooManySkillsLimit": "store: too many skill candidates in the repository (limit %d)",
	"store.err.tooManyCandidates":  "store: the repository %s holds %d skills; pick one with --sub-path:",
	"store.err.repoDirMissing":     "store: no such repository folder: %s",
	"store.term.truncated":         "… (truncated)",

	// ---------------- meta.go ----------------
	"store.err.parseMeta":   "store: failed to parse %s (corrupt JSON)",
	"store.err.emptyMeta":   "store: cannot write empty metadata",
	"store.err.marshalMeta": "store: failed to encode the metadata",

	// ---------------- importmeta.go ----------------
	"store.err.parseFile":         "store: failed to parse %s",
	"store.err.importDefaultOnly": "store: .skill-lock.json only describes the default skills folder, so importing metadata is limited to that folder",
	"store.err.importMetaWrite":   "store: failed to write the metadata of %s",

	// ---------------- move.go ----------------
	"store.err.cleanStaging":      "store: failed to clean the temp folder of the cross-volume move: %s",
	"store.err.crossVolumeCopy":   "store: cross-volume move of %s: failed to copy to %s (source untouched)",
	"store.err.crossVolumeRename": "store: cross-volume move of %s: failed to place %s (source untouched)",
	"store.err.crossVolumeSource": "store: cross-volume move done, but deleting the source folder %s failed (the content is at %s)",

	// ---------------- hash.go / scan.go / fsutil.go / preview.go ----------------
	"store.err.walkDir":  "store: failed to walk %s",
	"store.err.readDir":  "store: failed to read folder %s",
	"store.err.copyFile": "store: failed to copy %s",

	// ---------------- settings.go ----------------
	"store.err.unknownLang":     "store: unsupported translation target language",
	"store.err.notConfigured":   "store: translation is not configured: fill in the API address, key and model first",
	"store.err.noSettings":      "store: no settings have been saved yet",
	"store.err.emptySettings":   "store: settings must not be nil",
	"store.err.marshalSettings": "store: failed to encode the settings",

	// ---------------- translate.go ----------------
	"store.err.translateRequest":   "store: failed to build the translation request",
	"store.err.translateEndpoint":  "store: failed to build the translation request (check the API address %s)",
	"store.err.translateDo":        "store: the translation request failed",
	"store.err.translateRead":      "store: failed to read the translation response",
	"store.err.translateStatus":    "store: the translation API returned %d: %s",
	"store.err.translateParse":     "store: failed to parse the translation response (not valid JSON)",
	"store.err.translateNoChoices": "store: the translation response has no choices (the server returned no translation)",
	"store.err.translateEmpty":     "store: the translation response is empty",

	// ---------------- 目标语言的自称 ----------------
	"store.langName.en":   "English",
	"store.langName.zhCN": "简体中文",
	"store.langName.enUS": "English (US)",
	"store.langName.enGB": "English (UK)",
	"store.langName.ja":   "日本語",
	"store.langName.ko":   "한국어",
	"store.langName.fr":   "Français",
	"store.langName.de":   "Deutsch",
	"store.langName.es":   "Español",
	"store.langName.pt":   "Português",
	"store.langName.ru":   "Русский",
}

// 缺元数据的整句文案 key：sourceMeta 的两个调用点（更新、检查更新）各用一句。
const (
	msgMetaMissingUpdate = "store.err.metaMissingUpdate"
	msgMetaMissingCheck  = "store.err.metaMissingCheck"
)

func init() { i18n.RegisterZH(catalogZH); i18n.RegisterEN(catalogEN) }

// sentinelErr 造一个「整句来自目录、身份来自哨兵」的错误：文字随语言变，
// errors.Is(err, base) 照样成立。
//
// 用在「哨兵自带的文字不够、还要补上下文（文件名、位置）」而整句又要完整进
// 目录的场合（如「store: 未找到该技能: x（回收站中不存在）」）。
func sentinelErr(base error, key string, args ...any) error {
	return &withSentinel{base: base, key: key, args: args}
}

// withSentinel 见 sentinelErr。
type withSentinel struct {
	base error
	key  string
	args []any
}

// Error 实现 error：每次渲染都按当前语言取文案。
func (e *withSentinel) Error() string { return i18n.Tf(e.key, e.args...) }

// Unwrap 让 errors.Is / errors.As 穿透到哨兵（身份不受语言影响）。
func (e *withSentinel) Unwrap() error { return e.base }
