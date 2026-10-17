// 图标资源：Lucide（ISC 许可，https://lucide.dev）风格的 24×24 线性图标，
// stroke 描边取 currentColor，因此 Icon 会跟随主题文字颜色着色。
//
// MustParseSVG 在包初始化时解析一次（不在 View 里解析）；资源损坏会让
// 进程启动时直接 panic，相当于编译期校验。MyGo 的 SVG 解析器支持 path、
// rect、circle、line、polyline 等基本形状与 currentColor，这里只用这些。
package ui

import myui "github.com/egoist/mygo/ui"

// svgIcon 用公共的 SVG 头尾包裹 Lucide 的路径内容：stroke-width 2、
// 圆头圆角、fill none，与 MyGo 自带的 searchIcon 风格一致。
func svgIcon(paths string) []byte {
	return []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + paths + `</svg>`)
}

// 图标集：刷新、移入回收站、恢复、检查/应用更新、导入元数据、
// 空态收件箱、技能根目录文件夹。
var (
	// iconRefresh（lucide refresh-cw）：工具栏的「刷新」图标按钮。
	iconRefresh = myui.MustParseSVG(svgIcon(
		`<path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/><path d="M3 21v-5h5"/>`))
	// iconTrash（lucide trash-2）：移入回收站。
	iconTrash = myui.MustParseSVG(svgIcon(
		`<path d="M3 6h18"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/><line x1="10" y1="11" x2="10" y2="17"/><line x1="14" y1="11" x2="14" y2="17"/>`))
	// iconRestore（lucide rotate-ccw）：从回收站恢复。
	iconRestore = myui.MustParseSVG(svgIcon(
		`<path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8"/><path d="M3 3v5h5"/>`))
	// iconUpdate（lucide download）：检查更新 / 应用更新。
	iconUpdate = myui.MustParseSVG(svgIcon(
		`<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/>`))
	// iconImport（lucide file-down）：导入元数据（从 .skill-lock.json 补齐）。
	iconImport = myui.MustParseSVG(svgIcon(
		`<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M12 18v-6"/><path d="m9 15 3 3 3-3"/>`))
	// iconCheckAll（lucide list-checks）：工具栏的「全部检查」——列表+勾，
	// 语义是逐个核对，区别于单技能的下载型「检查更新」。
	iconCheckAll = myui.MustParseSVG(svgIcon(
		`<path d="m3 17 2 2 4-4"/><path d="m3 7 2 2 4-4"/><path d="M13 6h8"/><path d="M13 12h8"/><path d="M13 18h8"/>`))
	// iconSearch（lucide search）：自建搜索框的放大镜（SearchField 的
	// 占位符写死为英文，这里用图标+TextInput 重建）。
	iconSearch = myui.MustParseSVG(svgIcon(
		`<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>`))
	// iconInbox（lucide inbox）：未选中技能时的空态插图。
	iconInbox = myui.MustParseSVG(svgIcon(
		`<polyline points="22 12 16 12 14 15 10 15 8 12 2 12"/><path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"/>`))
	// iconArrowLeft（lucide arrow-left）：技能市场页面的「返回」按钮。
	iconArrowLeft = myui.MustParseSVG(svgIcon(
		`<path d="m12 19-7-7 7-7"/><path d="M19 12H5"/>`))
	// iconGlobe（lucide globe）：工具栏的「技能市场」图标按钮（skills.sh 榜单）。
	iconGlobe = myui.MustParseSVG(svgIcon(
		`<circle cx="12" cy="12" r="10"/><path d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20"/><path d="M2 12h20"/>`))
	// iconFolder（lucide folder）：技能根目录路径旁的位置图标。
	iconFolder = myui.MustParseSVG(svgIcon(
		`<path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/>`))
)

// 三栏布局新增的图标：默认目录、添加文件夹、移除目录、关闭详情、来源。
var (
	// iconHome（lucide house）：左栏里的默认技能目录。
	iconHome = myui.MustParseSVG(svgIcon(
		`<path d="M15 21v-8a1 1 0 0 0-1-1h-4a1 1 0 0 0-1 1v8"/><path d="M3 10a2 2 0 0 1 .709-1.528l7-5.999a2 2 0 0 1 2.582 0l7 5.999A2 2 0 0 1 21 10v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>`))
	// iconPlus（lucide plus）：添加文件夹。
	iconPlus = myui.MustParseSVG(svgIcon(`<path d="M5 12h14"/><path d="M12 5v14"/>`))
	// iconUnlink（lucide folder-minus）：把目录从列表移除（不删文件夹）。
	iconUnlink = myui.MustParseSVG(svgIcon(
		`<path d="M9 13h6"/><path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/>`))
	// iconClose（lucide x）：关闭详情栏。
	iconClose = myui.MustParseSVG(svgIcon(`<path d="M18 6 6 18"/><path d="m6 6 12 12"/>`))
	// iconGitHub（lucide git-branch）：GitHub 来源。
	iconGitHub = myui.MustParseSVG(svgIcon(
		`<line x1="6" y1="3" x2="6" y2="15"/><circle cx="18" cy="6" r="3"/><circle cx="6" cy="18" r="3"/><path d="M18 9a9 9 0 0 1-9 9"/>`))
	// iconLocal（lucide hard-drive）：本地来源。
	iconLocal = myui.MustParseSVG(svgIcon(
		`<line x1="22" y1="12" x2="2" y2="12"/><path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"/><line x1="6" y1="16" x2="6.01" y2="16"/>`))
	// iconUnknown（lucide circle-help）：没有元数据或元数据损坏。
	iconUnknown = myui.MustParseSVG(svgIcon(
		`<circle cx="12" cy="12" r="10"/><path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/><path d="M12 17h.01"/>`))
)

// 外观开关与右键菜单用到的图标。
var (
	// iconSun（lucide sun）：浅色外观。
	iconSun = myui.MustParseSVG(svgIcon(
		`<circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.93 4.93 1.41 1.41"/><path d="m17.66 17.66 1.41 1.41"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.34 17.66-1.41 1.41"/><path d="m19.07 4.93-1.41 1.41"/>`))
	// iconMoon（lucide moon）：深色外观。
	iconMoon = myui.MustParseSVG(svgIcon(`<path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/>`))
	// iconMonitor（lucide monitor）：外观跟随系统。
	iconMonitor = myui.MustParseSVG(svgIcon(
		`<rect width="20" height="14" x="2" y="3" rx="2"/><line x1="8" x2="16" y1="21" y2="21"/><line x1="12" x2="12" y1="17" y2="21"/>`))
)

// 排列模式开关用到的图标。
var (
	// iconGrid（lucide layout-grid）：卡片网格。
	iconGrid = myui.MustParseSVG(svgIcon(
		`<rect width="7" height="7" x="3" y="3" rx="1"/><rect width="7" height="7" x="14" y="3" rx="1"/><rect width="7" height="7" x="14" y="14" rx="1"/><rect width="7" height="7" x="3" y="14" rx="1"/>`))
	// iconRows（lucide rows-3）：两行列表。
	iconRows = myui.MustParseSVG(svgIcon(
		`<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M21 9H3"/><path d="M21 15H3"/>`))
)

// 设置与翻译用到的图标。
var (
	// iconSettings（lucide settings）：左栏的「设置」入口（齿轮）。
	iconSettings = myui.MustParseSVG(svgIcon(
		`<path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/><circle cx="12" cy="12" r="3"/>`))
	// iconTranslate（lucide languages）：详情栏「原文/译文」开关旁的标识。
	iconTranslate = myui.MustParseSVG(svgIcon(
		`<path d="m5 8 6 6"/><path d="m4 14 6-6 2-3"/><path d="M2 5h12"/><path d="M7 2h1"/><path d="m22 22-5-10-5 10"/><path d="M14 18h6"/>`))
)
