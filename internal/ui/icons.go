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
