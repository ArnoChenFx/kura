package ui

// catalog.go 是 internal/ui 的界面文案目录：中文与英文两套，key 一一对应。
//
// 规则（见 internal/i18n 的包注释与 AGENTS.md）：
//   - 非测试代码里不写用户可见的中文字面量，一律 i18n.T / i18n.Tf 取用；
//   - 中文取值与改造前的界面文字逐字一致（既有测试断言的中文不能变），
//     英文取值自然、简短；两种语言的 %s/%d 占位符顺序相同；
//   - 整句进目录，不拼接（语序在中英文之间不同）。
//
// 这个文件也是本包唯一允许出现中文字面量的地方（守卫 TestNoHardcodedCJK
// 按文件名放行 catalog.go），所以下面几张「不翻译的表」也放在这里：
// 语言自称与既有测试引用的中文常量。

import "github.com/ArnoChenFx/kura/internal/i18n"

// catalogZH 是中文文案：与改造前的界面文字逐字一致。
var catalogZH = map[string]string{
	// common：多个弹窗共用的按钮。
	"ui.common.cancel":  "取消",
	"ui.common.close":   "关闭",
	"ui.common.save":    "保存",
	"ui.common.listSep": "、",

	// fileDialog：系统文件夹选择框的标题与按钮。
	"ui.fileDialog.addRootTitle":  "添加技能目录",
	"ui.fileDialog.addRootButton": "添加",

	// roots：左栏（技能目录列表）。
	"ui.roots.title":              "技能目录",
	"ui.roots.addFolder":          "添加文件夹",
	"ui.roots.loadFailed":         "目录列表读取失败：%s",
	"ui.roots.settingsLoadFailed": "设置读取失败：%s",

	// rail：左栏底部的图标按钮。
	"ui.rail.checkUpdateTip": "检查 kura 应用更新",
	"ui.rail.settingsTip":    "翻译 API 与目标语言",

	// action：按钮与菜单项的动作名。
	"ui.action.refresh":      "刷新",
	"ui.action.checkAll":     "全部检查",
	"ui.action.checkUpdate":  "检查更新",
	"ui.action.importMeta":   "导入元数据",
	"ui.action.removeRoot":   "从列表移除此目录",
	"ui.action.install":      "安装技能",
	"ui.action.restore":      "恢复",
	"ui.action.purge":        "永久删除",
	"ui.action.closeDetail":  "关闭详情",
	"ui.action.moveTrash":    "移入回收站",
	"ui.action.purgeTooltip": "永久删除回收站里的技能",

	// state：技能状态的短标签。
	"ui.state.enabled":  "已启用",
	"ui.state.disabled": "已停用",
	"ui.state.trashed":  "回收站",

	// filter：筛选栏的分段（带数量）。
	"ui.filter.all":      "全部 %d",
	"ui.filter.enabled":  "启用 %d",
	"ui.filter.disabled": "停用 %d",
	"ui.filter.trashed":  "回收站 %d",

	// search：搜索框。
	"ui.search.placeholder": "搜索技能",

	// view：技能区的排列模式开关。
	"ui.view.label": "排列",
	"ui.view.grid":  "卡片网格",
	"ui.view.list":  "列表",

	// theme：外观开关。
	"ui.theme.label":  "外观",
	"ui.theme.system": "跟随系统",
	"ui.theme.light":  "浅色",
	"ui.theme.dark":   "深色",

	// menu：右键菜单项。
	"ui.menu.openFolder":     "打开文件夹",
	"ui.menu.revealFolder":   "在文件夹中显示",
	"ui.menu.openSkillMD":    "打开 SKILL.md",
	"ui.menu.copyPath":       "复制路径",
	"ui.menu.removeFromList": "从列表移除",
	"ui.menu.moveTrash":      "移入回收站…",
	"ui.menu.disable":        "停用",
	"ui.menu.enable":         "启用",
	"ui.menu.restore":        "恢复到技能目录",
	"ui.menu.purge":          "永久删除…",

	// card：卡片与列表行上的小控件。
	"ui.card.switchLabel":   "%s 开关",
	"ui.card.noDescription": "没有描述",

	// pill：卡片右下角的状态标签。
	"ui.pill.hasUpdate": "有更新",
	"ui.pill.badMeta":   "元数据损坏",
	"ui.pill.noSkillMD": "缺少 SKILL.md",

	// empty：技能区为空时的占位。
	"ui.empty.noSkills":     "这个目录里还没有技能",
	"ui.empty.noSkillsHint": "点右上角的「安装技能」，从 GitHub 或本地文件夹安装",
	"ui.empty.noMatch":      "没有匹配「%s」的技能",
	"ui.empty.noMatchHint":  "换个关键词试试",
	"ui.empty.noEnabled":    "没有已启用的技能",
	"ui.empty.noDisabled":   "没有已停用的技能",
	"ui.empty.noTrashed":    "没有回收站的技能",

	// detail：右栏详情。
	"ui.detail.tabOverview": "概览",
	"ui.detail.tabFull":     "全文",
	"ui.detail.source":      "来源",
	"ui.detail.metaGroup":   "元数据",
	"ui.detail.location":    "位置",
	"ui.detail.metaFailed":  "元数据读取失败：%s",
	"ui.detail.noMeta":      "没有 .kura-meta.json（非 kura 安装，或尚未导入元数据）",
	"ui.detail.installedAt": "安装",
	"ui.detail.updatedAt":   "更新",
	"ui.detail.busy":        "正在操作 %s…",
	"ui.detail.trashLabel":  "详情:移入回收站",
	"ui.detail.checkLabel":  "详情:检查更新",
	"ui.detail.purgeLabel":  "详情:永久删除",
	"ui.detail.loading":     "正在读取 SKILL.md…",
	"ui.detail.noSkillMD":   "没有 SKILL.md 可显示",
	"ui.detail.readFailed":  "读取 SKILL.md 失败：%s",

	// translate：译文模式（只影响界面显示）。
	"ui.translate.label":          "翻译",
	"ui.translate.original":       "原文",
	"ui.translate.translated":     "译文",
	"ui.translate.displayLang":    "显示语言",
	"ui.translate.notConfigured":  "先在设置里填翻译 API",
	"ui.translate.openSettings":   "打开设置",
	"ui.translate.inProgress":     "翻译中…",
	"ui.translate.failed":         "翻译失败：%s（切回原文再切到译文可重试）",
	"ui.translate.fullInProgress": "翻译全文中…",
	"ui.translate.fullFailed":     "全文翻译失败：%s（切回原文再切到译文可重试）",
	"ui.translate.note":           "译文 · %s · 仅界面显示，不改动技能文件",
	"ui.translate.configChanged":  "翻译配置已变更",
	"ui.translate.emptyResult":    "翻译结果为空",
	"ui.translate.chunkFailed":    "第 %d/%d 段：%s",
	"ui.translate.chunkEmpty":     "第 %d/%d 段：翻译结果为空",

	// install：安装技能弹窗。
	"ui.install.button":              "安装",
	"ui.install.buttonSelect":        "安装选中的技能",
	"ui.install.buttonSelected":      "安装选中的 %d 个",
	"ui.install.repoRoot":            "仓库根",
	"ui.install.subtitle":            "安装到 %s",
	"ui.install.sourceGitHub":        "GitHub 仓库",
	"ui.install.sourceLocal":         "本地文件夹",
	"ui.install.folder":              "文件夹",
	"ui.install.folderPlaceholder":   "包含 SKILL.md 的文件夹",
	"ui.install.choose":              "选择…",
	"ui.install.repo":                "仓库",
	"ui.install.repoPlaceholder":     "owner/repo，或粘贴 GitHub 网址",
	"ui.install.subPath":             "子目录",
	"ui.install.subPathPlaceholder":  "可选，留空自动发现",
	"ui.install.ref":                 "分支/标签",
	"ui.install.refPlaceholder":      "可选，默认分支",
	"ui.install.name":                "安装名称",
	"ui.install.namePlaceholder":     "可选，默认取 SKILL.md 的 name",
	"ui.install.hintGitHub":          "会用本机的 git 克隆仓库；子目录留空时自动发现仓库里的技能。",
	"ui.install.hintCandidates":      "勾选要安装的技能（可多选），点下面的按钮一次装完；名字取各自 SKILL.md 里的 name（勾选一个时用上面的「安装名称」）。",
	"ui.install.confirmA11y":         "确认安装",
	"ui.install.total":               "共 %d 个技能",
	"ui.install.selectAll":           "全选",
	"ui.install.selectNone":          "全不选",
	"ui.install.failItem":            "%s：%s",
	"ui.install.noCandidates":        "store: 仓库内未找到含 SKILL.md 的技能目录",
	"ui.install.repoOrBranchChanged": "仓库或分支已改，点「安装」重新发现技能。",

	// status：底部状态行与各处的一句话反馈。
	"ui.status.rootMissing":         "技能目录不存在: %s",
	"ui.status.scanned":             "已扫描 %d 个技能",
	"ui.status.enabled":             "已启用 %s",
	"ui.status.disabled":            "已停用 %s",
	"ui.status.toggling":            "正在切换 %s…",
	"ui.status.movingToTrash":       "正在移入回收站 %s…",
	"ui.status.movedToTrash":        "已移入回收站 %s",
	"ui.status.restoring":           "正在恢复 %s…",
	"ui.status.restored":            "已恢复 %s",
	"ui.status.purging":             "正在永久删除 %s…",
	"ui.status.purged":              "已永久删除 %s",
	"ui.status.imported":            "已导入 %d 条元数据",
	"ui.status.nothingToImport":     "没有可导入的元数据（lock 文件不存在或技能已有元数据）",
	"ui.status.rootAdded":           "已添加目录 %s",
	"ui.status.rootRemoved":         "已从列表移除 %s（文件夹未改动）",
	"ui.status.pathCopied":          "已复制路径 %s",
	"ui.status.discoverRootsFailed": "自动发现技能目录失败：%s",
	"ui.status.rootsDiscovered":     "已自动加入 %d 个 agent 技能目录",
	"ui.status.openFailed":          "无法打开 %s：%s",
	"ui.status.appUpdateNotReady":   "更新功能未就绪（开发构建未挂载更新插件）",
	"ui.status.appUpdateChecking":   "正在检查 kura 更新…",
	"ui.status.themeSaveFailed":     "外观偏好没能保存：%s",
	"ui.status.viewSaveFailed":      "排列模式没能保存：%s",
	"ui.status.langSaveFailed":      "界面语言没能保存：%s",
	"ui.status.savingSettings":      "正在保存设置…",
	"ui.status.settingsSaved":       "设置已保存",
	"ui.status.settingsSavedShort":  "设置已保存，但翻译 API 还没填全，暂时不能翻译",
	"ui.status.settingsSaveFailed":  "设置没能保存：%s",
	"ui.status.checkingUpdate":      "正在检查更新 %s…",
	"ui.status.checkFailed":         "检查 %s 失败: %v",
	"ui.status.checkingAll":         "正在检查全部更新…",
	"ui.status.checkedAll":          "已检查 %d 个技能",
	"ui.status.updating":            "正在更新 %s…",
	"ui.status.updated":             "已更新 %d 个技能（旧内容在快照中）",
	"ui.status.updateFailed":        "更新失败: %s",
	"ui.status.updatedPartial":      "已更新 %d 个技能，%d 个失败: %v",
	"ui.status.checkSummary":        "%s: 新增 %d · 修改 %d · 删除 %d（共 %d 个文件）",
	"ui.status.installing":          "正在安装…",
	"ui.status.installFailed":       "安装失败",
	"ui.status.installed":           "已安装 %s",
	"ui.status.installedCount":      "已安装 %d 个技能",
	"ui.status.installedPartial":    "已安装 %d 个，%d 个失败",
	"ui.status.candidates":          "仓库里有 %d 个技能，请勾选要安装的",
	"ui.status.discovering":         "正在查找仓库里的技能…",
	"ui.status.discoveryFailed":     "查找失败",
	"ui.status.repoChanged":         "仓库或分支已改，点「安装」重新发现技能。",
	"ui.status.subPathFilled":       "已填子目录，点「安装」装它",
	"ui.status.skillFound":          "发现技能 %s",
	"ui.status.total":               "共 %d 个技能",
	"ui.status.pickFolderFailed":    "选择文件夹失败: %s",

	// review：更新评审面板。
	"ui.review.title":                    "更新 %s",
	"ui.review.nothingToCheck":           "根目录里没有可检查的技能",
	"ui.review.applyAll":                 "应用全部更新",
	"ui.review.apply":                    "应用更新",
	"ui.review.applying":                 "正在应用…",
	"ui.review.upToDate":                 "与上游一致，无需更新",
	"ui.review.localModified":            "已安装的内容与安装时不一致（本地改过）",
	"ui.review.localModifiedWithChanges": "已安装的内容与安装时不一致（本地改过），应用更新会覆盖这些改动（旧内容在快照中）",
	"ui.review.noResult":                 "没有可显示的检查结果",
	"ui.review.checkFailed":              "检查失败",
	"ui.review.latest":                   "已是最新",
	"ui.review.localChanged":             "本地有改动",
	"ui.review.counts":                   "+%d ~%d -%d（%d 个文件）",
	"ui.review.metaSource":               "来源: %s",
	"ui.review.metaCommit":               "上游 commit: %s",
	"ui.review.basisCommit":              "上游 commit 未变",
	"ui.review.basisTree":                "上游该目录未变",
	"ui.review.oldHash":                  "旧 %s",
	"ui.review.newHash":                  "新 %s",
	"ui.review.scanFailed":               "根目录扫描失败: %s",

	// source：技能的来源类型与来源值。
	"ui.source.noMeta":      "无元数据",
	"ui.source.local":       "本地",
	"ui.source.unknown":     "未知来源",
	"ui.source.unknownType": "未知",

	// diff：文件差异状态。
	"ui.diff.added":    "新增",
	"ui.diff.removed":  "删除",
	"ui.diff.modified": "修改",

	// confirm：破坏性操作的确认框。
	"ui.confirm.trashTitle":   "移入回收站？",
	"ui.confirm.trashMessage": "技能 %s 将从技能目录移入 kura 回收站，可随时恢复；移动前会自动写入内容快照。",
	"ui.confirm.trashOK":      "确认移入",
	"ui.confirm.purgeTitle":   "永久删除？",
	"ui.confirm.purgeMessage": "技能 %s 将从回收站永久删除，无法恢复。删除前会先写入一份内容快照，快照保留在 kura 的数据目录里。",

	// markdown：全文页的渲染提示。
	"ui.markdown.truncated": "（内容过长，已截断）",
	"ui.markdown.moreLines": "…（另有 %d 行未显示）",

	// settings：设置弹窗。
	"ui.settings.title":             "设置",
	"ui.settings.translateHeading":  "翻译 API（OpenAI 兼容）",
	"ui.settings.loadFailed":        "设置文件读取失败，保存会覆盖它：%s",
	"ui.settings.baseURL":           "API 地址",
	"ui.settings.apiKey":            "密钥",
	"ui.settings.model":             "模型",
	"ui.settings.targetLang":        "目标语言",
	"ui.settings.interfaceLang":     "界面语言",
	"ui.settings.langSystem":        "跟随系统",
	"ui.settings.unknownTargetLang": "已保存的目标语言无法识别，已回到%s；保存后生效。",
	"ui.settings.secretNote":        "密钥以明文保存在 ~/.kura/settings.json；译文只用于界面显示，不会改动技能文件。",
	"ui.settings.incomplete":        "API 地址、密钥、模型都填好后，才能翻译技能描述。",
	"ui.settings.saveA11y":          "确认保存",

	// panic：编程错误（资源损坏）时的 panic 文字：不是给用户的界面文案，
	// 但同样走目录，理由是它原本就是中文、且守卫不允许别处出现中文字面量。
	"ui.panic.iconDecode": "ui: 应用图标解码失败: %s",
}

// catalogEN 是英文文案：key 与中文一一对应（含同样的占位符）。
var catalogEN = map[string]string{
	// common
	"ui.common.cancel":  "Cancel",
	"ui.common.close":   "Close",
	"ui.common.save":    "Save",
	"ui.common.listSep": ", ",

	// fileDialog
	"ui.fileDialog.addRootTitle":  "Add skill directory",
	"ui.fileDialog.addRootButton": "Add",

	// roots
	"ui.roots.title":              "Skill directories",
	"ui.roots.addFolder":          "Add folder",
	"ui.roots.loadFailed":         "Could not read the directory list: %s",
	"ui.roots.settingsLoadFailed": "Could not read the settings: %s",

	// rail
	"ui.rail.checkUpdateTip": "Check for kura app updates",
	"ui.rail.settingsTip":    "Translation API and target language",

	// action
	"ui.action.refresh":      "Refresh",
	"ui.action.checkAll":     "Check all",
	"ui.action.checkUpdate":  "Check for updates",
	"ui.action.importMeta":   "Import metadata",
	"ui.action.removeRoot":   "Remove this directory from the list",
	"ui.action.install":      "Install skill",
	"ui.action.restore":      "Restore",
	"ui.action.purge":        "Delete permanently",
	"ui.action.closeDetail":  "Close details",
	"ui.action.moveTrash":    "Move to trash",
	"ui.action.purgeTooltip": "Delete a trashed skill permanently",

	// state
	"ui.state.enabled":  "Enabled",
	"ui.state.disabled": "Disabled",
	"ui.state.trashed":  "Trash",

	// filter
	"ui.filter.all":      "All %d",
	"ui.filter.enabled":  "Enabled %d",
	"ui.filter.disabled": "Disabled %d",
	"ui.filter.trashed":  "Trash %d",

	// search
	"ui.search.placeholder": "Search skills",

	// view
	"ui.view.label": "Layout",
	"ui.view.grid":  "Grid",
	"ui.view.list":  "List",

	// theme：外观开关的英文用 Auto，避免与「界面语言」的 System 撞名（同一
	// 窗口里两处文字相同时，按文字查找的测试会点错）。
	"ui.theme.label":  "Appearance",
	"ui.theme.system": "Auto",
	"ui.theme.light":  "Light",
	"ui.theme.dark":   "Dark",

	// menu
	"ui.menu.openFolder":     "Open folder",
	"ui.menu.revealFolder":   "Show in folder",
	"ui.menu.openSkillMD":    "Open SKILL.md",
	"ui.menu.copyPath":       "Copy path",
	"ui.menu.removeFromList": "Remove from list",
	"ui.menu.moveTrash":      "Move to trash…",
	"ui.menu.disable":        "Disable",
	"ui.menu.enable":         "Enable",
	"ui.menu.restore":        "Restore to skill directory",
	"ui.menu.purge":          "Delete permanently…",

	// card
	"ui.card.switchLabel":   "%s switch",
	"ui.card.noDescription": "No description",

	// pill
	"ui.pill.hasUpdate": "Update available",
	"ui.pill.badMeta":   "Corrupt metadata",
	"ui.pill.noSkillMD": "Missing SKILL.md",

	// empty
	"ui.empty.noSkills":     "No skills in this directory yet",
	"ui.empty.noSkillsHint": "Click “Install skill” at the top right to install from GitHub or a local folder",
	"ui.empty.noMatch":      "No skills match “%s”",
	"ui.empty.noMatchHint":  "Try another keyword",
	"ui.empty.noEnabled":    "No enabled skills",
	"ui.empty.noDisabled":   "No disabled skills",
	"ui.empty.noTrashed":    "No skills in the trash",

	// detail
	"ui.detail.tabOverview": "Overview",
	"ui.detail.tabFull":     "Full text",
	"ui.detail.source":      "Source",
	"ui.detail.metaGroup":   "Metadata",
	"ui.detail.location":    "Location",
	"ui.detail.metaFailed":  "Could not read the metadata: %s",
	"ui.detail.noMeta":      "No .kura-meta.json (not installed by kura, or the metadata was never imported)",
	"ui.detail.installedAt": "Installed",
	"ui.detail.updatedAt":   "Updated",
	"ui.detail.busy":        "Working on %s…",
	"ui.detail.trashLabel":  "Detail: move to trash",
	"ui.detail.checkLabel":  "Detail: check for updates",
	"ui.detail.purgeLabel":  "Detail: delete permanently",
	"ui.detail.loading":     "Reading SKILL.md…",
	"ui.detail.noSkillMD":   "No SKILL.md to show",
	"ui.detail.readFailed":  "Could not read SKILL.md: %s",

	// translate
	"ui.translate.label":          "Translation",
	"ui.translate.original":       "Original",
	"ui.translate.translated":     "Translated",
	"ui.translate.displayLang":    "Display language",
	"ui.translate.notConfigured":  "Set up the translation API in Settings first",
	"ui.translate.openSettings":   "Open settings",
	"ui.translate.inProgress":     "Translating…",
	"ui.translate.failed":         "Translation failed: %s (switch back to Original and then to Translated to retry)",
	"ui.translate.fullInProgress": "Translating the full text…",
	"ui.translate.fullFailed":     "Full-text translation failed: %s (switch back to Original and then to Translated to retry)",
	"ui.translate.note":           "Translated · %s · shown in the interface only; skill files are not modified",
	"ui.translate.configChanged":  "The translation settings changed",
	"ui.translate.emptyResult":    "The translation was empty",
	"ui.translate.chunkFailed":    "part %d/%d: %s",
	"ui.translate.chunkEmpty":     "part %d/%d: the translation was empty",

	// install
	"ui.install.button":              "Install",
	"ui.install.buttonSelect":        "Install selected skills",
	"ui.install.buttonSelected":      "Install %d selected",
	"ui.install.repoRoot":            "Repository root",
	"ui.install.subtitle":            "Installing into %s",
	"ui.install.sourceGitHub":        "GitHub repository",
	"ui.install.sourceLocal":         "Local folder",
	"ui.install.folder":              "Folder",
	"ui.install.folderPlaceholder":   "Folder containing SKILL.md",
	"ui.install.choose":              "Choose…",
	"ui.install.repo":                "Repository",
	"ui.install.repoPlaceholder":     "owner/repo, or paste a GitHub URL",
	"ui.install.subPath":             "Subdirectory",
	"ui.install.subPathPlaceholder":  "Optional; leave empty to discover automatically",
	"ui.install.ref":                 "Branch/tag",
	"ui.install.refPlaceholder":      "Optional; uses the default branch",
	"ui.install.name":                "Install name",
	"ui.install.namePlaceholder":     "Optional; defaults to the name in SKILL.md",
	"ui.install.hintGitHub":          "Clones the repository with your local git; when the subdirectory is empty, skills in the repository are discovered automatically.",
	"ui.install.hintCandidates":      "Select the skills to install (multiple allowed) and use the button below to install them at once; each name comes from its own SKILL.md (when you select one, the “Install name” above is used).",
	"ui.install.confirmA11y":         "Confirm install",
	"ui.install.total":               "Skills: %d",
	"ui.install.selectAll":           "Select all",
	"ui.install.selectNone":          "Deselect all",
	"ui.install.failItem":            "%s: %s",
	"ui.install.noCandidates":        "store: no skill directory containing SKILL.md was found in the repository",
	"ui.install.repoOrBranchChanged": "The repository or branch changed; click “Install” to discover skills again.",

	// status
	"ui.status.rootMissing":         "The skill directory does not exist: %s",
	"ui.status.scanned":             "Scanned: %d",
	"ui.status.enabled":             "Enabled %s",
	"ui.status.disabled":            "Disabled %s",
	"ui.status.toggling":            "Switching %s…",
	"ui.status.movingToTrash":       "Moving %s to the trash…",
	"ui.status.movedToTrash":        "Moved %s to the trash",
	"ui.status.restoring":           "Restoring %s…",
	"ui.status.restored":            "Restored %s",
	"ui.status.purging":             "Deleting %s permanently…",
	"ui.status.purged":              "Deleted %s permanently",
	"ui.status.imported":            "Imported metadata: %d",
	"ui.status.nothingToImport":     "Nothing to import (no lock file, or every skill already has metadata)",
	"ui.status.rootAdded":           "Added directory %s",
	"ui.status.rootRemoved":         "Removed %s from the list (the folder is untouched)",
	"ui.status.pathCopied":          "Copied path %s",
	"ui.status.discoverRootsFailed": "Could not discover agent skill directories: %s",
	"ui.status.rootsDiscovered":     "Auto-added skill directories: %d",
	"ui.status.openFailed":          "Could not open %s: %s",
	"ui.status.appUpdateNotReady":   "Updates are not available (this development build has no update plugin)",
	"ui.status.appUpdateChecking":   "Checking for kura updates…",
	"ui.status.themeSaveFailed":     "Could not save the appearance preference: %s",
	"ui.status.viewSaveFailed":      "Could not save the layout mode: %s",
	"ui.status.langSaveFailed":      "Could not save the interface language: %s",
	"ui.status.savingSettings":      "Saving settings…",
	"ui.status.settingsSaved":       "Settings saved",
	"ui.status.settingsSavedShort":  "Settings saved, but the translation API is incomplete, so translation is unavailable for now",
	"ui.status.settingsSaveFailed":  "Could not save the settings: %s",
	"ui.status.checkingUpdate":      "Checking %s for updates…",
	"ui.status.checkFailed":         "Check failed for %s: %v",
	"ui.status.checkingAll":         "Checking all skills for updates…",
	"ui.status.checkedAll":          "Checked: %d",
	"ui.status.updating":            "Updating %s…",
	"ui.status.updated":             "Updated: %d (old contents are in snapshots)",
	"ui.status.updateFailed":        "Update failed: %s",
	"ui.status.updatedPartial":      "%d updated, %d failed: %v",
	"ui.status.checkSummary":        "%s: %d added · %d modified · %d removed (%d file(s))",
	"ui.status.installing":          "Installing…",
	"ui.status.installFailed":       "Install failed",
	"ui.status.installed":           "Installed %s",
	"ui.status.installedCount":      "Installed %d skills",
	"ui.status.installedPartial":    "Installed %d, %d failed",
	"ui.status.candidates":          "The repository has %d skills; select the ones to install",
	"ui.status.discovering":         "Looking for skills in the repository…",
	"ui.status.discoveryFailed":     "Discovery failed",
	"ui.status.repoChanged":         "The repository or branch changed; click “Install” to discover skills again.",
	"ui.status.subPathFilled":       "The subdirectory is filled in; click “Install” to install it",
	"ui.status.skillFound":          "Found skill %s",
	"ui.status.total":               "Skills: %d",
	"ui.status.pickFolderFailed":    "Could not choose a folder: %s",

	// review
	"ui.review.title":                    "Update %s",
	"ui.review.nothingToCheck":           "No skills to check in this directory",
	"ui.review.applyAll":                 "Apply all updates",
	"ui.review.apply":                    "Apply update",
	"ui.review.applying":                 "Applying…",
	"ui.review.upToDate":                 "Matches upstream; nothing to update",
	"ui.review.localModified":            "The installed content differs from what was installed (modified locally)",
	"ui.review.localModifiedWithChanges": "The installed content differs from what was installed (modified locally), and applying the update will overwrite these changes (old contents are in a snapshot)",
	"ui.review.noResult":                 "No check results to show",
	"ui.review.checkFailed":              "Check failed",
	"ui.review.latest":                   "Up to date",
	"ui.review.localChanged":             "Changed locally",
	"ui.review.counts":                   "+%d ~%d -%d (%d file(s))",
	"ui.review.metaSource":               "Source: %s",
	"ui.review.metaCommit":               "upstream commit: %s",
	"ui.review.basisCommit":              "upstream commit unchanged",
	"ui.review.basisTree":                "upstream directory unchanged",
	"ui.review.oldHash":                  "old %s",
	"ui.review.newHash":                  "new %s",
	"ui.review.scanFailed":               "Scan failed: %s",

	// source
	"ui.source.noMeta":      "No metadata",
	"ui.source.local":       "Local",
	"ui.source.unknown":     "Unknown source",
	"ui.source.unknownType": "Unknown",

	// diff
	"ui.diff.added":    "Added",
	"ui.diff.removed":  "Removed",
	"ui.diff.modified": "Modified",

	// confirm
	"ui.confirm.trashTitle":   "Move to the trash?",
	"ui.confirm.trashMessage": "Skill %s will be moved from the skill directory to the kura trash and can be restored at any time. A content snapshot is written before the move.",
	"ui.confirm.trashOK":      "Move to trash",
	"ui.confirm.purgeTitle":   "Delete permanently?",
	"ui.confirm.purgeMessage": "Skill %s will be deleted from the trash permanently and cannot be restored. A content snapshot is written first and kept in the kura data directory.",

	// markdown
	"ui.markdown.truncated": "(content too long; truncated)",
	"ui.markdown.moreLines": "… (%d more lines not shown)",

	// settings
	"ui.settings.title":             "Settings",
	"ui.settings.translateHeading":  "Translation API (OpenAI-compatible)",
	"ui.settings.loadFailed":        "Could not read the settings file; saving will overwrite it: %s",
	"ui.settings.baseURL":           "API URL",
	"ui.settings.apiKey":            "API key",
	"ui.settings.model":             "Model",
	"ui.settings.targetLang":        "Target language",
	"ui.settings.interfaceLang":     "Language",
	"ui.settings.langSystem":        "System",
	"ui.settings.unknownTargetLang": "The saved target language was not recognized; reset to %s. Save to apply.",
	"ui.settings.secretNote":        "The key is stored in plain text at ~/.kura/settings.json. Translations are shown in the interface only; skill files are never modified.",
	"ui.settings.incomplete":        "Fill in the API URL, the key and the model to translate skill descriptions.",
	"ui.settings.saveA11y":          "Confirm save",

	// panic
	"ui.panic.iconDecode": "ui: could not decode the app icon: %s",
}

func init() { i18n.RegisterZH(catalogZH); i18n.RegisterEN(catalogEN) }

// targetLang 是一个翻译目标语言：规范语言码与界面显示名。
type targetLang struct {
	code  string // 规范码，必须是 store.NormalizeTargetLang 认得（且规范化后不变）的
	label string // 界面显示名
}

// targetLangs 是设置里可选的目标语言，显式列在 UI 侧：store 的显示名表是
// 未导出的，且「选项顺序」是界面决策。每个 code 必须能被
// store.NormalizeTargetLang 原样认回，否则保存会被拒（TestTargetLangsAreNormalized 守着）。
//
// 这些 label 是**语言的自称**（简体中文、日本語、Français…）：说中文的人找
// 「简体中文」，说日语的人找「日本語」，所以两种界面语言下都显示自称、不进
// 目录、不翻译。放在 catalog.go 是因为守卫只允许这个文件出现中文字面量。
var targetLangs = []targetLang{
	{"zh-CN", "简体中文"},
	{"en", "English"},
	{"en-US", "English (US)"},
	{"en-GB", "English (UK)"},
	{"ja", "日本語"},
	{"ko", "한국어"},
	{"fr", "Français"},
	{"de", "Deutsch"},
	{"es", "Español"},
	{"pt", "Português"},
	{"ru", "Русский"},
}

// ifaceLang 是「界面语言」下拉的一个选项：config.json 的 lang 偏好值 + 显示名。
type ifaceLang struct {
	pref string // store.LangSystem / LangZH / LangEN
	key  string // 显示名的目录 key：非空时随界面语言变（「跟随系统」/System）
	name string // 语言自称：两种界面语言下都一样（理由同 targetLangs）
}

// ifaceLangs 是「界面语言」下拉的选项与顺序。简体中文 / English 显示自称：
// 界面语言正是用当前语言命名的最容易认不清的一处，自称让任何语言的使用者都
// 认得；只有「跟随系统」是随界面语言变的普通文案。
var ifaceLangs = []ifaceLang{
	{pref: "system", key: "ui.settings.langSystem"},
	{pref: "zh", name: "简体中文"},
	{pref: "en", name: "English"},
}

// ifaceLangLabel 返回偏好在本帧语言下的显示名；不认识的偏好按跟随系统。
func ifaceLangLabel(pref string) string {
	for _, o := range ifaceLangs {
		if o.pref == pref {
			return o.label()
		}
	}
	return ifaceLangs[0].label()
}

// ifaceLangLabels 返回下拉在本帧语言下的全部选项（顺序同 ifaceLangs）。
func ifaceLangLabels() []string {
	out := make([]string, len(ifaceLangs))
	for i, o := range ifaceLangs {
		out[i] = o.label()
	}
	return out
}

// ifaceLangPref 把下拉里的显示名换回偏好值：认不出来（不在表里）时按
// 跟随系统，不猜——下拉只会给出表里的名字。
func ifaceLangPref(label string) string {
	for _, o := range ifaceLangs {
		if o.label() == label {
			return o.pref
		}
	}
	return ifaceLangs[0].pref
}

// label 返回选项在本帧语言下的显示名。
func (o ifaceLang) label() string {
	if o.key != "" {
		return i18n.T(o.key)
	}
	return o.name
}

// detailTab 是详情页「概览 / 全文」tab 的标签下标（detailTab 状态字段的取值）。
// 下面两个中文常量保留原样：既有测试用它们点击 tab（测试跑在中文下，见
// catalog_test.go 的 TestMain），渲染时仍走目录，随界面语言变
// （TestDetailTabConstantsMatchCatalog 守着两者一致）。
const (
	detailTabOverview = "概览"
	detailTabFull     = "全文"
)

// mdTruncatedNotice 是 markdown 被截断时追加的提示的中文取值：markdown_test.go
// 直接引用它，所以它留在这里；渲染时走目录（ui.markdown.truncated）。
const mdTruncatedNotice = "（内容过长，已截断）"
