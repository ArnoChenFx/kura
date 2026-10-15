package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/ArnoChenFx/kura/resources"
	myui "github.com/egoist/mygo/ui"
)

// 三栏的宽度（DIP）：左栏放目录名与徽标，右栏放详情字段；中栏占满剩余。
// rootsWidth 与 detailWidth 是默认值，两侧的分割线拖动时在下面的范围里改变。
const (
	rootsWidth  = 224
	detailWidth = 320
	// cardMinWidth / cardHeight 是卡片的最小宽度与固定高度：网格按窗口宽度
	// 决定列数，列宽在最小宽度之上平分剩余空间。
	cardMinWidth = 248
	cardHeight   = 140
	// titleBarMinHeight 是自绘标题栏的高度（DIP），与 main.go 里
	// WindowOptions.TitleBarHeight 保持一致：全屏时窗口按钮藏起来、
	// c.TitleBar() 返回零，高度取这个兑底，顶栏不塌。
	titleBarMinHeight = 40
)

// 分割线的命中宽度，以及两侧栏宽的拖动范围（DIP）：左栏 168–420，详情栏
// 260–560。中栏至少留 mainMinW，窄窗口里两侧栏会一起让位（见 fitWidths）。
const (
	gripHit    = 8
	rootsMinW  = 168
	rootsMaxW  = 420
	detailMinW = 260
	detailMaxW = 560
	mainMinW   = 300
)

// appIcon 是标题栏里的应用图标（resources/icon.png，经 resources 包嵌入）。
var appIcon = mustDecodeBitmap(resources.IconPNG)

// mustDecodeBitmap 解码内嵌的位图资源。资源损坏会让进程启动时直接 panic，
// 与 icons.go 的 MustParseSVG 一样当作编译期式的校验。
func mustDecodeBitmap(data []byte) *myui.Bitmap {
	bm, err := myui.DecodeBitmap(data)
	if err != nil {
		panic(i18n.Tf("ui.panic.iconDecode", err))
	}
	return bm
}

// View 构建 kura 主窗口的一帧，顶部自绘标题栏 + 三栏结构：
//
//	┌──────────┬──────────────────────────────┬────────────┐
//	│ kura     │ 目录名 · 路径    刷新 检查 导入 │ 详情（选中   │
//	│ 技能目录  │ 🔍 搜索…   [全部|已启用|停用|回收站] │ 卡片后出现） │
//	│ ▸skills 9│ ┌──────┐ ┌──────┐ ┌──────┐     │ 名字 状态    │
//	│  other  3│ │卡片 ⬜│ │卡片 ⬜│ │卡片 ⬜│     │ 描述         │
//	│          │ └──────┘ └──────┘ └──────┘     │ 元数据       │
//	│ +添加文件夹│ 状态条                         │ 操作         │
//	└──────────┴──────────────────────────────┴────────────┘
func (a *App) View(c *myui.Context) {
	a.skin = kuraScheme.apply(c, a.themeMode)
	a.pal = a.skin.pal
	// 首帧触发一次后台初始扫描（磁盘 I/O 不在 UI 线程做）。
	if !a.loaded {
		a.loaded = true
		a.ReloadAsync()
	}
	// 左栏的选择在上一次构建的输入里改了 rootSel：在这里切换目录。
	// 以状态比较为守卫，重复构建不会重复切换。
	if a.rootSel != a.cfg.Root {
		a.SelectRoot(a.rootSel)
	}
	rows := a.visible()
	a.syncSelection(rows)
	sel := a.selected()
	// 译文模式下为选中的技能发起描述翻译（受缓存占位保护，同一原文只发一次；
	// 请求在后台，这里只读状态，见 translate.go）。
	a.ensureTranslations(sel)

	a.viewTitlebar(c)
	// 三栏占满标题栏以下的剩余高度；标题栏自身不参与收缩（见 viewTitlebar）。
	// 左右两条分割线拖动改变两侧栏宽（偏好），中栏占剩余（见 viewGrip）。
	// 实际宽度按本帧窗口宽度（c.Size）每帧夹紧：窗口变窄时两侧一起让位，不
	// 改用户的偏好，变宽后自然恢复；它是偏好与窗口宽度的纯函数，所以连续帧
	// 不会抖动。
	a.bodyW, _ = c.Size()
	a.rootsEff, a.detailEff = fitWidths(a.bodyW, a.rootsW, a.detailW, sel != nil)
	rootsHi, detailHi := a.widthLimits(sel != nil)
	body := myui.Row(c).Key("body").Grow(1).MinHeight(0).AlignItems(myui.Stretch)
	body.Children(func() {
		a.viewRoots(c)
		a.viewGrip(c, &a.rootsW, a.rootsEff, 1, rootsMinW, rootsHi)
		myui.Column(c).Grow(1).MinWidth(0).FillHeight().Background(a.pal.canvas).Children(func() {
			a.viewMainHeader(c)
			a.viewFilterBar(c)
			a.viewErrorBanner(c)
			a.viewSkills(c, rows)
			a.viewStatusBar(c)
		})
		if sel != nil {
			a.viewGrip(c, &a.detailW, a.detailEff, -1, detailMinW, detailHi)
			a.viewDetail(c, sel)
		}
	})

	// 「移入回收站」与「永久删除」共用一个确认框；最后一个按钮是默认按钮（回车触发），
	// Escape 或点按钮后 *open 置 false。
	title, message, ok := i18n.T("ui.confirm.trashTitle"),
		i18n.Tf("ui.confirm.trashMessage", a.confirm.name), i18n.T("ui.confirm.trashOK")
	if a.confirm.purge {
		title, ok = i18n.T("ui.confirm.purgeTitle"), i18n.T("ui.action.purge")
		message = i18n.Tf("ui.confirm.purgeMessage", a.confirm.name)
	}
	if n := myui.AlertDialog(c, &a.confirm.open, title, message, i18n.T("ui.common.cancel"), ok); n == 1 {
		if a.confirm.purge {
			a.PurgeAsync(a.confirm.name)
		} else {
			a.RemoveAsync(a.confirm.name)
		}
	}

	// 更新评审面板（检查更新/全部检查的结果 + 应用/取消）。
	a.viewReviewModal(c)

	// 安装技能弹窗。
	a.viewInstallModal(c)

	// 设置弹窗（翻译 API 与目标语言）。
	a.viewSettingsModal(c)
}

// viewTitlebar 是自绘标题栏：跨整个窗口宽度的一条顶栏，放在三栏之上。
// 窗口用 mygo.TitleBarHidden 隐藏了系统标题栏但保留右上角的窗口按钮，按钮
// 占掉的区域经 c.TitleBar() 拿到（Height 是顶部那条带的高度，Right 是从右
// 边缘算的宽度），内边距避开它，让标题文字不被按钮压住；全屏时窗口按钮
// 藏起来、c.TitleBar() 返回零，高度取 titleBarMinHeight 兜底，顶栏不塌。
// 整条 DragWindow()：拖动它移动窗口，双击等于标题栏的双击（最大化/还原）。
// 按钮不放在这里——可交互的元素会吃掉拖动手势，所以标题栏只放标识与名字。
func (a *App) viewTitlebar(c *myui.Context) {
	t := c.Theme()
	bar := c.TitleBar()
	myui.Row(c).FillWidth().Shrink(0).Height(max(bar.Height, titleBarMinHeight)).
		Padding(0, bar.Right+12, 0, bar.Left+12).Background(a.pal.sidebar).
		AlignItems(myui.Center).Gap(t.Space(2)).DragWindow().Children(func() {
		// 标识：项目里的应用图标（四周透明，直接放进顶栏）。
		myui.Image(c, appIcon).Size(t.Space(6), t.Space(6))
		myui.Text(c, "kura").Bold().SingleLine()
	})
}

// widthLimits 返回两侧栏宽的拖动上限（DIP）。窗口宽度减去中栏的最小宽度与两条
// 分割线，再减去另一侧栏的当前实际宽度：这样拖宽一侧时，中栏不会被挤没。
// 详情栏隐藏时左栏可以用掉全部余量。bodyW 为 0（还没有窗口宽度）时只用固定
// 上限。
func (a *App) widthLimits(detailShown bool) (rootsHi, detailHi float32) {
	rootsHi, detailHi = rootsMaxW, detailMaxW
	if a.bodyW <= 0 {
		return rootsHi, detailHi
	}
	spare := a.bodyW - mainMinW - 2*gripHit
	rootsHi = min(rootsHi, spare)
	if detailShown {
		rootsHi = min(rootsHi, spare-a.detailEff)
		detailHi = min(detailHi, spare-a.rootsEff)
	}
	return rootsHi, detailHi
}

// fitWidths 把用户偏好的栏宽夹紧到本帧窗口宽度 avail（DIP）放得下的范围，返回
// 左栏与详情栏的实际宽度。偏好先夹进各自的 [下限, 上限]；两侧加起来超过
// avail 减去中栏最小宽度与两条分割线的余量时，两侧按各自高出下限的部分
// 等比例一起让位，最多让到下限（窗口窄到连下限都放不下时，中栏不再保证
// mainMinW）。详情栏隐藏时只夹左栏，它可以用掉全部余量。avail 为 0
// （还没有窗口宽度）时只做上下限夹紧。纯函数：同样的输入总是同样的结果。
func fitWidths(avail, rootsPref, detailPref float32, detailShown bool) (roots, detail float32) {
	roots = min(max(rootsPref, rootsMinW), rootsMaxW)
	detail = min(max(detailPref, detailMinW), detailMaxW)
	if avail <= 0 {
		return roots, detail
	}
	spare := avail - mainMinW - 2*gripHit
	if !detailShown {
		return min(roots, max(spare, rootsMinW)), detail
	}
	if over := roots + detail - spare; over > 0 {
		slackR, slackD := roots-rootsMinW, detail-detailMinW
		total := slackR + slackD
		if over >= total {
			return rootsMinW, detailMinW
		}
		roots -= over * slackR / total
		detail -= over * slackD / total
	}
	return roots, detail
}

// viewGrip 是两栏之间的分割线：中间一条 1px 的线，两侧共 gripHit 宽的区域用来
// 拖动，悬停或拖动时线变成强调色。size 指向它调整的那一栏的宽度偏好，cur 是
// 它本帧的实际宽度（窗口窄时可能比偏好小）；dir 为 +1 表示那一栏在分割线
// 左侧（向右拖变宽），-1 表示在右侧（向左拖变宽）。拖动量从实际宽度出发，
// 经 dragWidth 夹进 [lo, hi]；开始拖动时两栏当前被夹紧后的实际宽度先固定为
// 偏好，这样分割线跟手、另一栏不会在拖动中又被重新夹一遍。宽度变了就请求
// 重绘。
//
// 命中区用主栏底色铺满：不铺的话，它会透出后面的窗口底色（深色下是卡片色），
// 变成一道 gripHit 宽的色带，分割线看起来就粗了。
func (a *App) viewGrip(c *myui.Context, size *float32, cur, dir float32, lo, hi float32) {
	t := c.Theme()
	g := myui.Box(c).Width(gripHit).FillHeight().Shrink(0).Background(a.pal.canvas).Cursor(myui.CursorResizeEW)
	dx, _, held := g.Dragged()
	if held && dx != 0 {
		if next := dragWidth(cur, dir*dx, lo, hi); next != cur {
			a.rootsW, a.detailW = a.rootsEff, a.detailEff
			*size = next
			c.Invalidate()
		}
	}
	line := a.pal.line
	if held || g.Hovered() {
		line = t.Accent
	}
	g.Draw(func(p *myui.Painter, r myui.Rect) {
		p.Fill(myui.Rect{X: r.X + (r.W-1)/2, Y: r.Y, W: 1, H: r.H}, line, 0)
	})
}

// dragWidth 返回拖动 delta 之后的栏宽。变宽时不超过 hi，但已经比 hi 宽的栏不被
// 收窄；变窄时不低于 lo，已经比 lo 窄的栏不被放宽。
func dragWidth(cur, delta, lo, hi float32) float32 {
	next := cur + delta
	switch {
	case delta > 0:
		next = min(next, max(hi, cur))
	case delta < 0:
		next = max(next, min(lo, cur))
	}
	return next
}

// viewRoots 是左栏：技能目录列表（每项带在用技能数，追加的目录可以拖动排序，
// 默认目录总在第一个）与底部的「添加文件夹」，再往下是 viewRootsFooter 的一行。
// 底色与中栏相同，只靠右侧的分隔线区分；选中项用浅一档的底色。品牌标识在窗口
// 顶栏里，这里不再重复。
func (a *App) viewRoots(c *myui.Context) {
	t := c.Theme()
	myui.Column(c).Width(a.rootsEff).Shrink(0).FillHeight().Background(a.pal.sidebar).Children(func() {
		myui.Text(c, i18n.T("ui.roots.title")).FontSize(t.FontSize-1).FontWeight(600).TextColor(t.TextMuted).
			Padding(t.Space(4), t.Space(5), t.Space(1.5), t.Space(5))
		myui.Scroll(c).Grow(1).FillWidth().Children(func() {
			myui.Column(c).FillWidth().Padding(0, t.Space(2.5)).Gap(t.Space(0.5)).Children(func() {
				for i, root := range a.lib.Roots {
					a.rootItem(c, root, i == 0)
				}
			})
		})
		if a.libErr != "" {
			myui.Text(c, i18n.Tf("ui.roots.loadFailed", a.libErr)).MaxLines(3).
				FontSize(t.FontSize-2).TextColor(t.Danger).Padding(t.Space(1), t.Space(4))
		}
		if a.settingsErr != "" {
			myui.Text(c, i18n.Tf("ui.roots.settingsLoadFailed", a.settingsErr)).MaxLines(3).
				FontSize(t.FontSize-2).TextColor(t.Danger).Padding(t.Space(1), t.Space(4))
		}
		myui.Column(c).FillWidth().Padding(t.Space(2), t.Space(3), t.Space(3), t.Space(3)).
			Gap(t.Space(3)).Children(func() {
			ab := myui.ButtonBase(c).Label(i18n.T("ui.roots.addFolder")).FillWidth().Disabled(a.adding).
				Justify(myui.Start).Gap(t.Space(2)).Padding(t.Space(2), t.Space(3)).
				Radius(t.Radius).Border(1, t.Border).TextColor(a.pal.textMid)
			if ab.Hovered() {
				ab.Background(t.SurfaceHover).TextColor(t.Text)
			}
			ab.Children(func() {
				myui.Icon(c, iconPlus)
				myui.Text(c, i18n.T("ui.roots.addFolder"))
			})
			if ab.Clicked() {
				a.AddRootAsync()
			}
			a.viewRootsFooter(c)
		})
	})
}

// themeModes 是外观开关的三个选项，顺序即分段顺序。key 是显示名的目录 key：
// 文案每帧现取（见 viewThemeSwitch），语言一切就跟着变。
var themeModes = []struct {
	mode, key string
	icon      *myui.SVG
}{
	{store.ThemeSystem, "ui.theme.system", iconMonitor},
	{store.ThemeLight, "ui.theme.light", iconSun},
	{store.ThemeDark, "ui.theme.dark", iconMoon},
}

// themeIndex 返回外观模式在 themeModes 里的位置，不认识的当作跟随系统。
func themeIndex(mode string) int {
	for i, m := range themeModes {
		if m.mode == mode {
			return i
		}
	}
	return 0
}

// viewRootsFooter 是左栏最底下的一行：外观开关靠左，「检查更新」「设置」两个图标
// 按钮靠右。「检查更新」检查的是 kura 应用自身（区别于技能级的「检查更新」）：点击后
// 立刻在状态行给一句反馈，更新窗口由 MyGo 插件弹出；经 checkUpdates 钩子走
// selfupdate.CheckForUpdates，测试里注入假实现，绝不触发真实检查。
func (a *App) viewRootsFooter(c *myui.Context) {
	t := c.Theme()
	myui.Row(c).FillWidth().AlignItems(myui.Center).Gap(t.Space(1)).Children(func() {
		a.viewThemeSwitch(c)
		myui.Spacer(c)
		railButton(c, iconUpdate, i18n.T("ui.action.checkUpdate"), i18n.T("ui.rail.checkUpdateTip"), a.checkingUpdate, a.CheckAppUpdateAsync)
		railButton(c, iconSettings, i18n.T("ui.settings.title"), i18n.T("ui.rail.settingsTip"), false, a.OpenSettings)
	})
}

// railButton 是左栏底部的图标按钮：无底色，悬停时才出现浅色底，与 ghostButton
// 同一套样式。label 是按钮的名字（无障碍与测试查找用），tip 是悬停提示，可以
// 比名字更具体。
func railButton(c *myui.Context, icon *myui.SVG, label, tip string, disabled bool, onClick func()) {
	t := c.Theme()
	b := myui.ButtonBase(c).Label(label).Tooltip(tip).Disabled(disabled).
		Padding(t.Space(1.75)).Radius(t.Radius).TextColor(t.TextMuted)
	if b.Hovered() && !disabled {
		b.Background(t.SurfaceHover).TextColor(t.Text)
	}
	if disabled {
		b.Opacity(0.45)
	}
	b.Children(func() { myui.Icon(c, icon).FontSize(t.FontSize + 2) })
	if b.Clicked() {
		onClick()
	}
}

// viewThemeSwitch 是左栏底部的外观开关：三个图标分段（跟随系统 / 浅色 / 深色），
// 选中的分段浮起。分段的点击在构建结束或查询响应时才写进 sel，所以要靠 Changed
// 在同一次构建里取到新选择，再交给 SetThemeAsync（幂等，重复构建不会重复写盘）；
// sel 每次构建都从 themeMode 重新推出。布局由 viewRootsFooter 负责。
func (a *App) viewThemeSwitch(c *myui.Context) {
	t := c.Theme()
	sel := themeIndex(a.themeMode)
	seg := myui.SegmentedBase(c, &sel, len(themeModes))
	track := seg.Track.Label(i18n.T("ui.theme.label")).Padding(t.Space(0.5)).Gap(t.Space(0.5)).
		Radius(t.Radius).Background(a.pal.card).Border(1, a.pal.line).Shrink(0).Children(func() {
		for i, m := range themeModes {
			b := seg.Segment(i).Label(i18n.T(m.key)).Tooltip(i18n.T(m.key)).
				Padding(t.Space(1.25), t.Space(2)).Radius(t.Radius - 2).TextColor(t.TextMuted)
			switch {
			case i == sel:
				b.Background(t.SurfacePressed).TextColor(t.Text)
			case b.Hovered():
				b.Background(t.SurfaceHover).TextColor(t.Text)
			}
			b.Children(func() { myui.Icon(c, m.icon).FontSize(t.FontSize + 1) })
		}
	})
	if track.Changed() {
		a.SetThemeAsync(themeModes[sel].mode)
	}
}

// rootRef 是拖动排序时携带的目录路径。Drag 把它交给被放下的那一行，Drop 只接收
// 这个类型，不会误收别的拖放数据。
type rootRef string

// rootItem 是左栏里的一个技能目录：图标 + 名字 + 在用技能数。选中项用浅一档的
// 底色，未选中项悬停时才有底色；点击后在下一帧开头切换目录。追加的目录可以拖动
// 排序：拖到另一个目录上，就占据那个位置（默认目录固定第一个，既不能拖也不能
// 放）。拖动时的落点边框亮起强调色。
func (a *App) rootItem(c *myui.Context, root string, isDefault bool) {
	t := c.Theme()
	b := myui.ButtonBase(c).Key(root).Label(rootLabel(root)).Tooltip(root).
		FillWidth().Justify(myui.Start).Gap(t.Space(2.5)).
		Padding(t.Space(1.75), t.Space(2.5)).Radius(t.Radius - 2).TextColor(a.pal.textMid)
	switch {
	case root == a.rootSel:
		b.Background(a.pal.sel).TextColor(t.Text)
	case b.Hovered():
		b.Background(t.SurfaceHover).TextColor(t.Text)
	}
	if !isDefault {
		b.Drag(rootRef(root))
		if _, over := myui.DragOver[rootRef](b); over {
			b.Border(1, t.Accent)
		}
		if from, ok := myui.Drop[rootRef](b); ok && string(from) != root {
			a.ReorderRootAsync(string(from), slices.Index(a.lib.Roots, root))
		}
	}
	b.Children(func() {
		icon := iconFolder
		if isDefault {
			icon = iconHome
		}
		myui.Icon(c, icon).FontSize(16)
		myui.Text(c, rootLabel(root)).SingleLine().Grow(1).MinWidth(0)
		if n, ok := a.rootCounts[root]; ok {
			myui.Text(c, itoa(n)).SingleLine().FontSize(t.FontSize - 1).TextColor(t.TextMuted)
		}
	})
	b.ContextMenu(func(m *myui.Menu) { a.rootMenu(c, m, root, isDefault) })
	if b.Clicked() {
		a.rootSel = root
	}
}

// rootMenu 是左栏目录项的右键菜单：打开文件夹、复制路径；追加的目录还能
// 从列表移除（只改 config.json，不动文件夹）。右键不切换目录。
func (a *App) rootMenu(c *myui.Context, m *myui.Menu, root string, isDefault bool) {
	if m.Item(i18n.T("ui.menu.openFolder")).Chosen() {
		a.OpenPathAsync(root)
	}
	if m.Item(i18n.T("ui.menu.copyPath")).Chosen() {
		a.copyPath(c, root)
	}
	if !isDefault {
		m.Separator()
		if m.Item(i18n.T("ui.menu.removeFromList")).Chosen() {
			a.RemoveRootAsync(root)
		}
	}
}

// copyPath 把路径放进剪贴板，状态行说一声。只在菜单项被选中的那一帧调用。
func (a *App) copyPath(c *myui.Context, path string) {
	c.WriteClipboard(path)
	a.status, a.statusErr = i18n.Tf("ui.status.pathCopied", path), false
}

// rootLabel 是目录在左栏里的名字：最后两级路径（如 .agents/skills），
// 比单独的 skills 更好分辨。
func rootLabel(root string) string {
	parent, base := filepath.Split(filepath.Clean(root))
	parent = filepath.Base(filepath.Clean(parent))
	if parent == "" || parent == "." || parent == string(filepath.Separator) || base == "" {
		return root
	}
	return parent + "/" + base
}

// viewMainHeader 是中栏头部：当前目录名与完整路径；右侧是主操作「安装
// 技能」与作用于整个目录的图标按钮（刷新、全部检查、导入元数据或从列表移除）。
func (a *App) viewMainHeader(c *myui.Context) {
	t := c.Theme()
	isDefault := a.cfg.Root == a.lib.Default()
	myui.Row(c).FillWidth().Padding(t.Space(4), t.Space(5), t.Space(2), t.Space(5)).
		Gap(t.Space(1)).AlignItems(myui.Center).Children(func() {
		myui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(1)).Children(func() {
			myui.Text(c, rootLabel(a.cfg.Root)).Bold().FontSize(t.FontSize + 6).SingleLine()
			myui.Row(c).Gap(t.Space(1.5)).AlignItems(myui.Center).Children(func() {
				myui.Icon(c, iconFolder).TextColor(t.TextMuted).FontSize(t.FontSize - 1)
				myui.Text(c, a.cfg.Root).SingleLine().MinWidth(0).FontSize(t.FontSize - 1).TextColor(t.TextMuted)
			})
		})
		if a.scanning || a.checkingAll || a.checking != "" {
			myui.Spinner(c)
		}
		ghostButton(c, iconRefresh, i18n.T("ui.action.refresh"), a.scanning, a.ReloadAsync)
		ghostButton(c, iconCheckAll, i18n.T("ui.action.checkAll"), a.checkingAll || a.checking != "", a.CheckAllAsync)
		// .skill-lock.json 只描述默认目录里的技能。
		if isDefault {
			ghostButton(c, iconImport, i18n.T("ui.action.importMeta"), false, a.ImportAsync)
		} else {
			ghostButton(c, iconUnlink, i18n.T("ui.action.removeRoot"), false, func() { a.RemoveRootAsync(a.cfg.Root) })
		}
		myui.Box(c).Width(t.Space(2))
		ib := myui.PrimaryButton(c, "").Label(i18n.T("ui.action.install"))
		ib.Children(func() {
			myui.Icon(c, iconPlus)
			myui.Text(c, i18n.T("ui.action.install")).FontWeight(600)
		})
		if ib.Clicked() {
			a.OpenInstall()
		}
	})
}

// viewFilterBar 是搜索框 + 状态筛选（带数量）。窄的时候筛选换到搜索框
// 下一行（Wrap），而不是互相挤压。
func (a *App) viewFilterBar(c *myui.Context) {
	t := c.Theme()
	en, dis, trash := a.counts()
	myui.Row(c).FillWidth().Wrap().Padding(t.Space(2), t.Space(5), t.Space(3), t.Space(5)).Gap(t.Space(2)).
		AlignItems(myui.Center).Children(func() {
		searchBox(c, &a.query, a.pal.card).Grow(1).Basis(t.Space(50)).MaxWidth(t.Space(80))
		myui.Spacer(c)
		myui.Segmented(c, &a.filter,
			i18n.Tf("ui.filter.all", en+dis), i18n.Tf("ui.filter.enabled", en),
			i18n.Tf("ui.filter.disabled", dis), i18n.Tf("ui.filter.trashed", trash)).
			Shrink(0)
		myui.Box(c).Width(t.Space(1))
		a.viewModeSwitch(c)
	})
}

// viewModes 是排列模式开关的各个分段，顺序即分段顺序。key 是显示名的目录
// key：文案每帧现取（见 viewModeSwitch），语言一切就跟着变。
var viewModes = []struct {
	mode, key string
	icon      *myui.SVG
}{
	{store.ViewGrid, "ui.view.grid", iconGrid},
	{store.ViewList, "ui.view.list", iconRows},
}

// viewIndex 返回排列模式在 viewModes 里的位置，不认识的当作卡片网格。
func viewIndex(mode string) int {
	for i, m := range viewModes {
		if m.mode == mode {
			return i
		}
	}
	return 0
}

// viewModeSwitch 是筛选栏右端的排列开关：两个图标分段（卡片网格 / 列表），
// 选中的分段浮起，写法同 viewThemeSwitch。分段的点击在构建结束或查询响应时
// 才写进 sel，所以靠 Changed 在同一次构建里取到新选择再交给 SetViewAsync
// （幂等）；sel 每次构建都从 viewMode 重新推出。Shrink(0)：窄窗口里筛选栏
// 换行时它不被挤扁。
func (a *App) viewModeSwitch(c *myui.Context) {
	t := c.Theme()
	sel := viewIndex(a.viewMode)
	seg := myui.SegmentedBase(c, &sel, len(viewModes))
	track := seg.Track.Label(i18n.T("ui.view.label")).Padding(t.Space(0.5)).Gap(t.Space(0.5)).
		Radius(t.Radius).Background(a.pal.card).Border(1, a.pal.line).Shrink(0).Children(func() {
		for i, m := range viewModes {
			b := seg.Segment(i).Label(i18n.T(m.key)).Tooltip(i18n.T(m.key)).
				Padding(t.Space(1.5), t.Space(2)).Radius(t.Radius - 2).TextColor(t.TextMuted)
			switch {
			case i == sel:
				b.Background(t.SurfacePressed).TextColor(t.Text)
			case b.Hovered():
				b.Background(t.SurfaceHover).TextColor(t.Text)
			}
			b.Children(func() { myui.Icon(c, m.icon).FontSize(t.FontSize + 1) })
		}
	})
	if track.Changed() {
		a.SetViewAsync(viewModes[sel].mode)
	}
}

// viewErrorBanner 是全局错误横幅：目录缺失或扫描失败时出现在卡片上方。
func (a *App) viewErrorBanner(c *myui.Context) {
	if a.loadErr == "" {
		return
	}
	t := c.Theme()
	myui.Box(c).FillWidth().Margin(0, t.Space(5), t.Space(2), t.Space(5)).
		Padding(t.Space(2), t.Space(3)).Radius(t.Radius).
		Background(t.Danger.Alpha(0.08)).Border(1, t.Danger.Alpha(0.3)).
		Children(func() {
			myui.Text(c, a.loadErr).SingleLine().TextColor(t.Danger)
		})
}

// viewSkills 按排列模式画技能区：卡片网格（默认）或两行列表。两种排列共用
// 同一份筛选后的 rows 与选中状态（selName），切换模式不丢选中。
func (a *App) viewSkills(c *myui.Context, rows []*store.Skill) {
	if a.viewMode == store.ViewList {
		a.viewList(c, rows)
		return
	}
	a.viewGrid(c, rows)
}

// viewList 是两行列表：每个技能一行（viewRow），整体可滚动。选中直接以
// selName 为准，不引入网格的下标选中状态。没有技能时复用网格的空态。
func (a *App) viewList(c *myui.Context, rows []*store.Skill) {
	if len(rows) == 0 {
		a.viewGridEmpty(c)
		return
	}
	t := c.Theme()
	myui.Scroll(c).Grow(1).FillWidth().Children(func() {
		myui.Column(c).FillWidth().Gap(t.Space(1.5)).
			Padding(0, t.Space(5), t.Space(3), t.Space(5)).Children(func() {
			for _, s := range rows {
				a.viewRow(c, s, s.Name == a.selName)
			}
		})
	})
}

// viewRow 是列表里的一个技能，占两行：
//
//	[头像] 名字 · 来源 …… [标签] [开关 / 恢复 / 转圈]
//	       描述（单行，超出省略）
//
// 行为与卡片一致：点击选中、右键先选中再弹同一份菜单；停用与回收站的名字
// 变淡，回收站加删除线并把开关换成「恢复」。行自己画选中与悬停（同一圈边框）。
func (a *App) viewRow(c *myui.Context, s *store.Skill, selected bool) {
	t := c.Theme()
	busy := a.isBusy(s.Name)
	dim := s.State != store.StateEnabled
	row := myui.ButtonBase(c).Key(s.Name).FillWidth().Justify(myui.Start).
		Padding(t.Space(2.5), t.Space(3)).Radius(t.Radius + 2)
	bg, border := a.pal.card, a.pal.line
	switch {
	case selected:
		bg, border = a.pal.cardSel, t.Accent
	case row.Hovered():
		bg, border = a.pal.cardHover, a.pal.lineHover
	}
	row.Background(bg).Border(1, border)
	// 右键先选中这一行：菜单作用的就是右栏正显示的技能（同卡片，放在菜单的
	// 构建函数里）。
	row.ContextMenu(func(m *myui.Menu) {
		if a.selName != s.Name {
			a.selName = s.Name
			c.Invalidate()
		}
		a.cardMenu(c, m, s)
	})
	row.Children(func() {
		myui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(1.5)).Children(func() {
			// 第 1 行：头像、名字、来源、标签 + 右侧控件。
			myui.Row(c).FillWidth().Gap(t.Space(3)).AlignItems(myui.Center).Children(func() {
				avatar(c, a.pal, s.Name, t.Space(7), dim)
				name := myui.Text(c, s.Name).SingleLine().FontWeight(600).MinWidth(0)
				if dim {
					name.TextColor(t.TextMuted)
				}
				if s.State == store.StateTrashed {
					name.Strikethrough()
				}
				myui.Row(c).Grow(1).MinWidth(0).Gap(t.Space(1)).AlignItems(myui.Center).Children(func() {
					myui.Icon(c, sourceIcon(s)).TextColor(t.TextMuted).FontSize(t.FontSize - 2).Shrink(0)
					myui.Text(c, cardSource(s)).SingleLine().MinWidth(0).
						FontSize(t.FontSize - 2).TextColor(t.TextMuted)
				})
				// 标签放在控件左侧，不挤压名字：只在有话要说时出现。
				if a.updates[s.Name] {
					pill(c, i18n.T("ui.pill.hasUpdate"), t.Accent)
				}
				switch s.State {
				case store.StateDisabled:
					pill(c, i18n.T("ui.state.disabled"), t.Warning)
				case store.StateTrashed:
					pill(c, i18n.T("ui.state.trashed"), t.Danger)
				}
				if s.MetaErr != "" {
					pill(c, i18n.T("ui.pill.badMeta"), t.Danger)
				} else if !s.HasSKILLMD && s.State != store.StateTrashed {
					pill(c, i18n.T("ui.pill.noSkillMD"), t.Warning)
				}
				switch {
				case busy:
					myui.Spinner(c)
				case s.State == store.StateTrashed:
					ghostButton(c, iconRestore, i18n.T("ui.action.restore"), false, func() { a.RestoreAsync(s.Name) })
				default:
					// 同卡片：Changed 返回 true 的这次构建里，on 已是目标状态。
					on := s.State == store.StateEnabled
					sw := kuraSwitch(c, &on, a.pal.toggle).Label(i18n.Tf("ui.card.switchLabel", s.Name))
					if sw.Changed() {
						a.ToggleAsync(s.Name, on)
					}
				}
			})
			// 第 2 行：描述，单行省略；左缩进与名字对齐（头像宽 + 间距）。
			desc, color := a.descText(translateCardPrefix, s), t.TextMuted
			if desc == "" {
				desc, color = i18n.T("ui.card.noDescription"), t.TextMuted.Alpha(0.6)
			}
			// 缩进用占位盒子而不是外边距：FillWidth 加外边距会比父容器宽出缩进，
			// 省略号就画到行边框之外。
			myui.Row(c).FillWidth().Children(func() {
				myui.Box(c).Width(t.Space(10)).Shrink(0)
				myui.Text(c, desc).SingleLine().FontSize(t.FontSize - 1).TextColor(color).
					Grow(1).MinWidth(0)
			})
		})
	})
	if row.Clicked() && a.selName != s.Name {
		a.selName = s.Name
		c.Invalidate()
	}
}

// viewGrid 是技能卡片网格：按宽度自动换列，只构建可见的行。
func (a *App) viewGrid(c *myui.Context, rows []*store.Skill) {
	t := c.Theme()
	if len(rows) == 0 {
		a.viewGridEmpty(c)
		return
	}
	// 网格用 skin.cell 主题构建：它的单元格不画任何选中框与悬停底（原因见
	// newSkin），选中与悬停全由卡片自己画，边框只有一圈。卡片里要换回真正的
	// 主题，建完再切回 cell 主题：下一个单元格在这之后构建，读的是 c 此刻的
	// 主题。
	c.SetTheme(a.skin.cell)
	myui.GridView(c, &a.grid, len(rows), cardMinWidth, cardHeight, func(i int) {
		c.SetTheme(t)
		a.viewCard(c, rows[i], i == a.selIdx)
		c.SetTheme(a.skin.cell)
	}).Grow(1).FillWidth().Padding(0, t.Space(4), t.Space(2), t.Space(4))
	c.SetTheme(t)
}

// viewGridEmpty 是没有卡片时的占位：区分「搜索无结果」「筛选下为空」与
// 「目录里没有技能」。
func (a *App) viewGridEmpty(c *myui.Context) {
	t := c.Theme()
	msg, hint := i18n.T("ui.empty.noSkills"), i18n.T("ui.empty.noSkillsHint")
	switch {
	case a.query != "":
		msg, hint = i18n.Tf("ui.empty.noMatch", a.query), i18n.T("ui.empty.noMatchHint")
	case a.filter != filterAll:
		// 三种筛选各一条完整文案（不拿状态标签拼接：英文里句子中间不该插
		// 一个大写开头的标签）。
		hint = ""
		switch a.filter {
		case filterEnabled:
			msg = i18n.T("ui.empty.noEnabled")
		case filterDisabled:
			msg = i18n.T("ui.empty.noDisabled")
		default:
			msg = i18n.T("ui.empty.noTrashed")
		}
	}
	myui.Box(c).Grow(1).FillWidth().Center().Children(func() {
		myui.Column(c).Gap(t.Space(3)).AlignItems(myui.Center).Children(func() {
			myui.Box(c).Size(t.Space(16), t.Space(16)).Radius(t.Space(8)).
				Background(t.Accent.Alpha(0.08)).Center().Children(func() {
				myui.Icon(c, iconInbox).TextColor(t.Accent).FontSize(t.FontSize * 2)
			})
			myui.Text(c, msg).Bold().FontSize(t.FontSize + 1)
			if hint != "" {
				myui.Text(c, hint).TextColor(t.TextMuted)
			}
		})
	})
}

// viewCard 是一张技能卡片：
//
//	[头像] [名字           ] [开关]
//	       [来源 · 插件    ]
//	[描述，最多两行                ]
//	[标签：有更新 / 已停用 / 缺 SKILL.md]
//
// 停用的卡片头像与名字变淡，回收站卡片加删除线并把开关换成「恢复」。
func (a *App) viewCard(c *myui.Context, s *store.Skill, selected bool) {
	t := c.Theme()
	busy := a.isBusy(s.Name)
	dim := s.State != store.StateEnabled
	card := myui.Column(c).Fill().Padding(t.Space(3.5)).Gap(t.Space(2.5)).
		Radius(t.Radius+4).Shadow(0, 1, 3, 0, a.pal.shadow)
	// 选中与悬停都由卡片自己画：同一圈边框、同一个圆角（网格的单元格不画，
	// 见 skin.cell）。选中优先于悬停。
	bg, border := a.pal.card, a.pal.line
	switch {
	case selected:
		bg, border = a.pal.cardSel, t.Accent
	case card.Hovered():
		bg, border = a.pal.cardHover, a.pal.lineHover
	}
	card.Background(bg).Border(1, border)
	// 右键先选中这张卡片：菜单作用的就是右栏正显示的技能。选中放在菜单的构建
	// 函数里（它只在菜单打开时与选了菜单项后各跑一次），不用 RightClicked：
	// 那会让卡片变成可点击元素，吞掉网格单元格要收的左键点击。
	card.ContextMenu(func(m *myui.Menu) {
		if a.selName != s.Name {
			a.selName = s.Name
			c.Invalidate()
		}
		a.cardMenu(c, m, s)
	})
	card.Children(func() {
		myui.Row(c).FillWidth().Gap(t.Space(3)).AlignItems(myui.Center).Children(func() {
			avatar(c, a.pal, s.Name, t.Space(9), dim)
			myui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(0.5)).Children(func() {
				name := myui.Text(c, s.Name).SingleLine().FontWeight(600)
				if dim {
					name.TextColor(t.TextMuted)
				}
				if s.State == store.StateTrashed {
					name.Strikethrough()
				}
				myui.Row(c).Gap(t.Space(1)).AlignItems(myui.Center).Children(func() {
					myui.Icon(c, sourceIcon(s)).TextColor(t.TextMuted).FontSize(t.FontSize - 2)
					myui.Text(c, cardSource(s)).SingleLine().MinWidth(0).
						FontSize(t.FontSize - 2).TextColor(t.TextMuted)
				})
			})
			switch {
			case busy:
				myui.Spinner(c)
			case s.State == store.StateTrashed:
				ghostButton(c, iconRestore, i18n.T("ui.action.restore"), false, func() { a.RestoreAsync(s.Name) })
			default:
				// Switch 的输入在构建时应用：Changed 返回 true 的这次构建里，
				// on 已是切换后的目标状态。
				on := s.State == store.StateEnabled
				sw := kuraSwitch(c, &on, a.pal.toggle).Label(i18n.Tf("ui.card.switchLabel", s.Name))
				if sw.Changed() {
					a.ToggleAsync(s.Name, on)
				}
			}
		})
		// 译文模式下，选中技能的卡片显示译文（其余卡片仍是原文，见 translate.go）。
		desc, color := a.descText(translateCardPrefix, s), t.TextMuted
		if desc == "" {
			desc, color = i18n.T("ui.card.noDescription"), t.TextMuted.Alpha(0.6)
		}
		myui.Text(c, desc).MaxLines(2).FontSize(t.FontSize - 1).TextColor(color).Grow(1)
		myui.Row(c).FillWidth().Gap(t.Space(1.5)).AlignItems(myui.Center).Children(func() {
			if a.updates[s.Name] {
				pill(c, i18n.T("ui.pill.hasUpdate"), t.Accent)
			}
			switch s.State {
			case store.StateDisabled:
				pill(c, i18n.T("ui.state.disabled"), t.Warning)
			case store.StateTrashed:
				pill(c, i18n.T("ui.state.trashed"), t.Danger)
			}
			if s.MetaErr != "" {
				pill(c, i18n.T("ui.pill.badMeta"), t.Danger)
			} else if !s.HasSKILLMD && s.State != store.StateTrashed {
				pill(c, i18n.T("ui.pill.noSkillMD"), t.Warning)
			}
		})
	})
}

// cardMenu 是卡片的右键菜单。上半是只看不改的：打开文件夹、在文件夹中显示、
// 打开 SKILL.md、复制路径；下半是改状态的操作，随技能状态变化：启用的有
// 停用 / 检查更新 / 移入回收站，停用的有启用 / 移入回收站，回收站里的只有
// 恢复。技能正忙时改状态的项置灰。移入回收站仍走确认框。
func (a *App) cardMenu(c *myui.Context, m *myui.Menu, s *store.Skill) {
	busy := a.isBusy(s.Name)
	if m.Item(i18n.T("ui.menu.openFolder")).Chosen() {
		a.OpenPathAsync(s.Dir)
	}
	if m.Item(i18n.T("ui.menu.revealFolder")).Chosen() {
		a.RevealAsync(s.Dir)
	}
	if s.State != store.StateTrashed {
		if m.Item(i18n.T("ui.menu.openSkillMD")).Disabled(!s.HasSKILLMD).Chosen() {
			a.OpenPathAsync(filepath.Join(s.Dir, "SKILL.md"))
		}
	}
	if m.Item(i18n.T("ui.menu.copyPath")).Chosen() {
		a.copyPath(c, s.Dir)
	}
	m.Separator()
	trash := func() {
		if m.Item(i18n.T("ui.menu.moveTrash")).Disabled(busy).Chosen() {
			a.confirm = confirmState{open: true, name: s.Name}
		}
	}
	switch s.State {
	case store.StateEnabled:
		if m.Item(i18n.T("ui.menu.disable")).Disabled(busy).Chosen() {
			a.ToggleAsync(s.Name, false)
		}
		if m.Item(i18n.T("ui.action.checkUpdate")).Disabled(busy || a.hasCheckTarget()).Chosen() {
			a.CheckAsync(s.Name)
		}
		m.Separator()
		trash()
	case store.StateDisabled:
		if m.Item(i18n.T("ui.menu.enable")).Disabled(busy).Chosen() {
			a.ToggleAsync(s.Name, true)
		}
		m.Separator()
		trash()
	case store.StateTrashed:
		if m.Item(i18n.T("ui.menu.restore")).Disabled(busy).Chosen() {
			a.RestoreAsync(s.Name)
		}
		m.Separator()
		if m.Item(i18n.T("ui.menu.purge")).Disabled(busy).Chosen() {
			a.confirm = confirmState{open: true, name: s.Name, purge: true}
		}
	}
}

// viewDetail 是右栏：所选技能的详情。头部是头像、名字与状态；下面是
// 「概览 / 全文」tab：概览是描述与分组的信息块（来源、元数据、位置），
// 全文是渲染出的 SKILL.md（后台读取，见 detailBody）；底部固定操作栏。
func (a *App) viewDetail(c *myui.Context, s *store.Skill) {
	t := c.Theme()
	busy := a.isBusy(s.Name)
	// 换技能时把 tab 重置回概览（detailSel 记录 tab 属于哪个技能），
	// 避免看到上一个技能的全文。
	if a.detailSel != s.Name {
		a.detailSel = s.Name
		a.detailTab = 0
	}
	myui.Column(c).Width(a.detailEff).Shrink(0).FillHeight().Background(a.pal.canvas).Children(func() {
		myui.Scroll(c).Grow(1).FillWidth().Children(func() {
			myui.Column(c).FillWidth().Padding(t.Space(4), t.Space(5)).Gap(t.Space(4)).Children(func() {
				myui.Row(c).FillWidth().Gap(t.Space(3)).AlignItems(myui.Start).Children(func() {
					avatar(c, a.pal, s.Name, t.Space(12), s.State != store.StateEnabled)
					myui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(1.5)).Children(func() {
						name := myui.Text(c, s.Name).Bold().FontSize(t.FontSize + 4)
						if s.State == store.StateTrashed {
							name.Strikethrough()
						}
						myui.Row(c).Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
							stateBadge(c, s.State)
							if a.updates[s.Name] {
								pill(c, i18n.T("ui.pill.hasUpdate"), t.Accent)
							}
						})
					})
					ghostButton(c, iconClose, i18n.T("ui.action.closeDetail"), false, func() { a.selName = "" })
				})
				a.viewTranslateBar(c)
				// 概览/全文 tab：绑定 a.detailTab。点选在下一次构建生效，
				// 两个页面都按 a.detailTab 分支，本帧内的切换下一帧生效。
				myui.Tabs(c, &a.detailTab, i18n.T("ui.detail.tabOverview"), i18n.T("ui.detail.tabFull")).FillWidth()
				a.detailBody(c, s)
			})
		})
		myui.Divider(c)
		myui.Box(c).FillWidth().Padding(t.Space(3), t.Space(5)).Children(func() {
			a.viewDetailActions(c, s, busy)
		})
	})
}

// detailBody 按 detailTab 渲染详情正文：概览页是原有的描述与信息块，
// 全文页是后台读出并渲染的 SKILL.md。
func (a *App) detailBody(c *myui.Context, s *store.Skill) {
	if a.detailTab != 0 {
		a.viewDetailFullText(c, s)
		return
	}
	a.viewDetailOverview(c, s)
}

// viewDetailOverview 是概览页：描述、缺 SKILL.md 提示、元数据与位置。
// 这两组信息加 Selectable（理由见 detailGroup）。
func (a *App) viewDetailOverview(c *myui.Context, s *store.Skill) {
	t := c.Theme()
	if s.Description != "" {
		myui.Text(c, a.descText(translateDetailPrefix, s)).TextColor(t.Text.Alpha(0.85))
		a.viewTranslateNote(c, s)
	}
	if !s.HasSKILLMD && s.State != store.StateTrashed {
		myui.Text(c, i18n.T("ui.pill.noSkillMD")).TextColor(t.Warning)
	}
	a.detailGroup(c, i18n.T("ui.detail.metaGroup"), true, func() { a.viewDetailMeta(c, s) })
	a.detailGroup(c, i18n.T("ui.detail.location"), true, func() {
		myui.Text(c, s.Dir).FontSize(t.FontSize - 1).TextColor(t.TextMuted)
	})
}

// viewTranslateBar 是详情头部下方的「原文 / 译文」开关条。目标语言是全局
// 一个（设置里选），开关也是全局的：切到译文后，选中技能的描述与「全文」页的
// SKILL.md 都显示译文（全文逐段翻译，见 viewFullTextBody）。译文模式但翻译 API
// 还没配好时，给出引导与「打开设置」入口，不发请求。
//
// 分段控件的点击在构建结束或查询响应时才写进 sel，所以靠 Changed 在同一次
// 构建里取到新选择再交给 SetTranslateOn；sel 每次构建都从 trans.on 推出。
func (a *App) viewTranslateBar(c *myui.Context) {
	t := c.Theme()
	myui.Column(c).FillWidth().Gap(t.Space(1.5)).Children(func() {
		myui.Row(c).FillWidth().AlignItems(myui.Center).Gap(t.Space(2)).Children(func() {
			myui.Icon(c, iconTranslate).TextColor(t.TextMuted).FontSize(t.FontSize)
			myui.Text(c, i18n.T("ui.translate.label")).SingleLine().FontSize(t.FontSize - 1).TextColor(t.TextMuted)
			myui.Spacer(c)
			sel := 0
			if a.trans.on {
				sel = 1
			}
			if myui.Segmented(c, &sel, i18n.T("ui.translate.original"), i18n.T("ui.translate.translated")).
				Label(i18n.T("ui.translate.displayLang")).Shrink(0).Changed() {
				a.SetTranslateOn(sel == 1)
				c.Invalidate()
			}
		})
		if !a.trans.on {
			return
		}
		if !a.settings.Ready() {
			myui.Box(c).FillWidth().Padding(t.Space(2), t.Space(3)).Radius(t.Radius).
				Background(t.Warning.Alpha(0.10)).Border(1, t.Warning.Alpha(0.4)).Children(func() {
				myui.Column(c).FillWidth().Gap(t.Space(1.5)).Children(func() {
					myui.Text(c, i18n.T("ui.translate.notConfigured")).TextColor(t.Warning)
					myui.Row(c).FillWidth().Children(func() {
						if myui.Button(c, i18n.T("ui.translate.openSettings")).Clicked() {
							a.OpenSettings()
						}
					})
				})
			})
		}
	})
}

// viewTranslateNote 是概览页描述下方的一行小字：译文模式下说明翻译的状态
// （翻译中 / 失败原因 / 当前译文的语言）。失败时原文仍在上方，不被遮挡。
// 翻译 API 未配置时不说话（引导在开关条里）。
func (a *App) viewTranslateNote(c *myui.Context, s *store.Skill) {
	if !a.trans.on || !a.settings.Ready() {
		return
	}
	t := c.Theme()
	e, ok := a.trans.get(detailDescKey(s.Name), s.Description)
	switch {
	case !ok || e.pending:
		myui.Row(c).Gap(t.Space(1.5)).AlignItems(myui.Center).Children(func() {
			myui.Spinner(c)
			myui.Text(c, i18n.T("ui.translate.inProgress")).FontSize(t.FontSize - 1).TextColor(t.TextMuted)
		})
	case e.err != "":
		myui.Text(c, i18n.Tf("ui.translate.failed", e.err)).MaxLines(4).
			FontSize(t.FontSize - 1).TextColor(t.Danger)
	default:
		myui.Text(c, i18n.Tf("ui.translate.note", a.settings.LangLabel())).
			FontSize(t.FontSize - 2).TextColor(t.TextMuted)
	}
}

// viewDetailFullText 是全文页：渲染技能的 SKILL.md。
//
// 正文在后台读取（startFullTextLoad 经 spawn 进 goroutine，不在 UI 线程做磁盘
// I/O），读出后按技能名缓存进 a.skillDocs：切 tab、来回切技能不必反复读盘；扫描
// 后缓存整体失效（applyScan），docGen 递增让还在路上的旧读盘结果作废。加载中
// 显示提示；读失败或没有 SKILL.md（回收站里的技能可能没有）显示明确原因，绝不
// 留白，绝不 panic。译文模式下正文由 viewFullTextBody 决定显示原文还是译文。
//
// 读盘结果是语言无关的（loading/noMD/err 三种状态），文案在这里现取：后台
// 任务跑完时语言可能已经变了（i18n.Error 的文字也是每次 Error() 现取的）。
func (a *App) viewDetailFullText(c *myui.Context, s *store.Skill) {
	a.startFullTextLoad(s)
	doc := a.skillDocs[s.Name]
	switch {
	case doc.loading:
		myui.Text(c, i18n.T("ui.detail.loading")).TextColor(c.Theme().TextMuted)
	case doc.noMD:
		myui.Text(c, i18n.T("ui.detail.noSkillMD")).TextColor(c.Theme().TextMuted)
	case doc.err != nil:
		myui.Text(c, doc.err.Error()).TextColor(c.Theme().Danger)
	default:
		// 渲染进外层已有的 Scroll，宽度铺满（官方视图自己处理块内布局）。
		a.viewFullTextBody(c, s, doc.src)
	}
}

// viewFullTextBody 渲染全文正文。原文模式直接渲染 SKILL.md。译文模式下：译文还在
// 路上时先显示原文、顶上一行「翻译全文中…」，译文到了替换成译文；失败时说明原因，
// 仍显示原文。翻译只读缓存，发起请求只经 ensureDocTranslation（受缓存占位保护，
// 同一原文只发一次）。翻译 API 未配好时不发请求，显示原文（引导在开关条里）。
func (a *App) viewFullTextBody(c *myui.Context, s *store.Skill, src string) {
	t := c.Theme()
	if !a.trans.on || !a.settings.Ready() || strings.TrimSpace(src) == "" {
		a.viewMarkdown(c, src)
		return
	}
	a.ensureDocTranslation(s.Name, src)
	e, ok := a.trans.get(docTransKey(s.Name), src)
	switch {
	case !ok || e.pending:
		myui.Row(c).Gap(t.Space(1.5)).AlignItems(myui.Center).Children(func() {
			myui.Spinner(c)
			myui.Text(c, i18n.T("ui.translate.fullInProgress")).FontSize(t.FontSize - 1).TextColor(t.TextMuted)
		})
		a.viewMarkdown(c, src)
	case e.err != "":
		myui.Text(c, i18n.Tf("ui.translate.fullFailed", e.err)).MaxLines(4).
			FontSize(t.FontSize - 1).TextColor(t.Danger)
		a.viewMarkdown(c, src)
	default:
		myui.Text(c, i18n.Tf("ui.translate.note", a.settings.LangLabel())).
			FontSize(t.FontSize - 2).TextColor(t.TextMuted)
		a.viewMarkdown(c, e.dst)
	}
}

// fullTextDoc 是一个技能的 SKILL.md 读盘结果，三种状态互斥：
// src 非空是读到了正文；loading 为真是在后台读；noMD 为真是没有 SKILL.md；
// err 非空是读盘失败。后三项存的是语言无关的状态而不是文案：后台任务跑完时
// 语言可能已经变了，文案（含错误里的原因）在 viewDetailFullText 里现取。
type fullTextDoc struct {
	src     string
	loading bool
	noMD    bool
	err     error
}

// startFullTextLoad 确保技能的 SKILL.md 有一份缓存：没有就去后台读
// （key 是技能名：同名技能只可能处于启用/停用/回收站之一，名字是稳定
// 键）。任务发出前先占位「正在读取」，避免重复发任务；读结果回写前检查
// docGen，扫描已刷新过缓存的任务直接丢弃。startFullTextLoad 只在
// viewDetailFullText（UI 线程）里调用。
func (a *App) startFullTextLoad(s *store.Skill) {
	if a.skillDocs == nil {
		a.skillDocs = map[string]fullTextDoc{}
	}
	if _, ok := a.skillDocs[s.Name]; ok {
		return
	}
	a.skillDocs[s.Name] = fullTextDoc{loading: true}
	name, dir, hasMD, gen := s.Name, s.Dir, s.HasSKILLMD, a.docGen
	a.spawn(func() {
		doc := fullTextDoc{noMD: true}
		if hasMD {
			data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
			if err != nil {
				// 错误文字随语言变（i18n.Errorf 到显示时才取文案），
				// 所以这里存错误本身，不存字符串。
				doc = fullTextDoc{err: i18n.Errorf("ui.detail.readFailed", err)}
			} else {
				doc = fullTextDoc{src: string(data)}
			}
		}
		a.publish(func() {
			if gen != a.docGen {
				// 读取期间扫过描（缓存已重置），这份旧结果不要。
				return
			}
			if a.skillDocs == nil {
				a.skillDocs = map[string]fullTextDoc{}
			}
			a.skillDocs[name] = doc
		})
	})
}

// detailGroup 是详情里的一个信息块：小标题 + 比底色浅一档的圆角框。
// selectable 为 true 时内层容器加 Selectable：组内文字可拖选、双击选词、
// Ctrl+C 拷贝（MyGo 容器上的 Selectable 让内部 Text/RichText 共享一次
// 选择）。只给「元数据」「位置」这两组开：组里现在没有控件，将来若有
// 按钮/输入框，必须给它们 .Unselectable()，否则控件交互和选择打架；
// 其余组（如即将出现的详情正文）保持普通，不为加选择改变排版。
func (a *App) detailGroup(c *myui.Context, title string, selectable bool, fn func()) {
	t := c.Theme()
	myui.Column(c).FillWidth().Gap(t.Space(1.5)).Children(func() {
		sectionHeader(c, title)
		inner := myui.Column(c).FillWidth().Padding(t.Space(3)).Gap(t.Space(1.5)).
			Radius(t.Radius).Background(a.pal.card).Border(1, a.pal.line)
		if selectable {
			inner.Selectable()
		}
		inner.Children(fn)
	})
}

// viewDetailMeta 是元数据字段；缺失或损坏时给出原因，绝不 panic。
func (a *App) viewDetailMeta(c *myui.Context, s *store.Skill) {
	t := c.Theme()
	m := s.Meta
	switch {
	case m != nil:
	case s.MetaErr != "":
		myui.Text(c, i18n.Tf("ui.detail.metaFailed", s.MetaErr)).TextColor(t.Danger)
		return
	default:
		myui.Text(c, i18n.T("ui.detail.noMeta")).TextColor(t.TextMuted)
		return
	}
	// 标签用弱化色，值在 metaField 里单独设为正文色，主次分明。
	myui.Column(c).FillWidth().TextColor(t.TextMuted).Children(func() {
		myui.Form(c, func() {
			// 来源行：GitHub 渲染成可点击链接（metaFieldLink，在系统浏览器
			// 打开），本地保持可选中的普通文本（Link 只认 URL，不收路径）。
			metaFieldLink(c, m)
			if m.Source.Type == store.SourceGitHub {
				if m.Source.Ref != "" {
					metaField(c, "Ref", m.Source.Ref, false, nil)
				}
				if m.Source.SubPath != "" {
					metaField(c, "SubPath", m.Source.SubPath, true, nil)
				}
				if m.Source.Commit != "" {
					metaField(c, "Commit", shortCommit(m.Source.Commit), false, nil)
				}
			}
			if m.PluginName != "" {
				metaField(c, "Plugin", m.PluginName, false, nil)
			}
			if m.InstalledAt != "" {
				metaField(c, i18n.T("ui.detail.installedAt"), shortTime(m.InstalledAt), false, nil)
			}
			if m.UpdatedAt != "" {
				metaField(c, i18n.T("ui.detail.updatedAt"), shortTime(m.UpdatedAt), false, nil)
			}
		})
	})
}

// viewDetailActions 是详情底部的操作行，按技能状态分组：
// 启用 → 检查更新（主）+ 停用 + 移入回收站（图标）；
// 停用 → 启用（主）+ 移入回收站（图标）；
// 回收站 → 恢复到技能目录（主）。
func (a *App) viewDetailActions(c *myui.Context, s *store.Skill, busy bool) {
	t := c.Theme()
	if busy {
		myui.Row(c).Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
			myui.Spinner(c)
			myui.Text(c, i18n.Tf("ui.detail.busy", s.Name)).SingleLine().TextColor(t.TextMuted)
		})
		return
	}
	// trashButton 是「移入回收站」：Label 保持「详情:移入回收站」供测试与无障碍。
	trashButton := func() {
		myui.Spacer(c)
		b := myui.ButtonBase(c).Label(i18n.T("ui.detail.trashLabel")).Tooltip(i18n.T("ui.action.moveTrash")).
			Padding(t.Space(1.75)).Radius(t.Radius).TextColor(t.Danger)
		if b.Hovered() {
			b.Background(t.Danger.Alpha(0.1))
		}
		b.Children(func() { myui.Icon(c, iconTrash).FontSize(t.FontSize + 2) })
		if b.Clicked() {
			a.confirm = confirmState{open: true, name: s.Name}
		}
	}
	myui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
		switch s.State {
		case store.StateEnabled:
			// 检查更新：预览上游差异，弹出评审面板（不直接改盘）。
			ub := myui.PrimaryButton(c, "").Label(i18n.T("ui.detail.checkLabel")).Disabled(a.hasCheckTarget())
			ub.Children(func() {
				myui.Icon(c, iconUpdate)
				myui.Text(c, i18n.T("ui.action.checkUpdate"))
			})
			if ub.Clicked() {
				a.CheckAsync(s.Name)
			}
			if myui.Button(c, i18n.T("ui.menu.disable")).Clicked() {
				a.ToggleAsync(s.Name, false)
			}
			trashButton()
		case store.StateDisabled:
			if myui.PrimaryButton(c, i18n.T("ui.menu.enable")).Clicked() {
				a.ToggleAsync(s.Name, true)
			}
			trashButton()
		case store.StateTrashed:
			rb := myui.PrimaryButton(c, "").Label(i18n.T("ui.menu.restore"))
			rb.Children(func() {
				myui.Icon(c, iconRestore)
				myui.Text(c, i18n.T("ui.menu.restore"))
			})
			if rb.Clicked() {
				a.RestoreAsync(s.Name)
			}
			// 永久删除：危险色的文字按钮，靠右放；走确认框，删除前 store 先快照。
			myui.Spacer(c)
			pb := myui.ButtonBase(c).Label(i18n.T("ui.detail.purgeLabel")).Tooltip(i18n.T("ui.action.purgeTooltip")).
				Padding(t.Space(1.75), t.Space(2.5)).Radius(t.Radius).TextColor(t.Danger)
			if pb.Hovered() {
				pb.Background(t.Danger.Alpha(0.1))
			}
			pb.Children(func() {
				myui.Icon(c, iconTrash).FontSize(t.FontSize + 1)
				myui.Text(c, i18n.T("ui.action.purge"))
			})
			if pb.Clicked() {
				a.confirm = confirmState{open: true, name: s.Name, purge: true}
			}
		}
	})
}

// viewStatusBar 是中栏底部的状态条：最近一次操作的结果 + 当前目录的总数。
// 总数与筛选栏的「全部」一致，不含回收站里的技能。
func (a *App) viewStatusBar(c *myui.Context) {
	t := c.Theme()
	en, dis, _ := a.counts()
	myui.Row(c).FillWidth().Padding(t.Space(2), t.Space(5)).
		Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
		if a.status != "" {
			color := t.TextMuted
			if a.statusErr {
				color = t.Danger
			}
			myui.Box(c).Size(t.Space(1.5), t.Space(1.5)).Radius(t.Space(0.75)).Shrink(0).
				Background(color)
			myui.Text(c, a.status).SingleLine().Grow(1).MinWidth(0).
				FontSize(t.FontSize - 1).TextColor(color)
		} else {
			myui.Spacer(c)
		}
		myui.Text(c, i18n.Tf("ui.status.total", en+dis)).
			SingleLine().Shrink(0).FontSize(t.FontSize - 1).TextColor(t.TextMuted)
	})
}

// viewReviewModal 渲染更新评审面板（Modal）：
//
//   - 单技能模式：标题为「更新 <skill>」；副行显示来源类型/上游 commit 与
//     目录级新旧哈希；正文逐行列出每个变更文件（状态、路径、±行数、新哈希）；
//     无变化时提示「与上游一致」；底部 应用更新 / 取消。
//   - 全部检查模式：标题「全部检查」；正文每个技能一小节，有变化的列出
//     文件摘要行，无变化的显示「已是最新」，失败的显示错误；底部
//     「应用全部更新」（逐个 Update 有变化的技能）/ 关闭。
//
// Apply 走 ApplyAsync（后台执行，完成后重扫并关闭面板）；取消仅关面板，
// 不动磁盘。Escape/点击外部关闭由 Modal 自带，语义与取消相同。
func (a *App) viewReviewModal(c *myui.Context) {
	r := &a.review
	t := c.Theme()
	myui.Modal(c, &r.open, func() {
		myui.Column(c).Width(560).Gap(12).Children(func() {
			switch {
			case r.all:
				myui.Text(c, i18n.T("ui.action.checkAll")).FontSize(16).Bold()
				if len(r.entries) == 0 {
					myui.Text(c, i18n.T("ui.review.nothingToCheck")).TextColor(t.TextMuted)
				}
				myui.Scroll(c).Grow(1).Height(340).Children(func() {
					myui.Column(c).FillWidth().Gap(10).Children(func() {
						for _, e := range r.entries {
							a.viewReviewEntry(c, e)
						}
					})
				})
				myui.Row(c).Gap(8).Justify(myui.End).Children(func() {
					if myui.Button(c, i18n.T("ui.common.close")).Clicked() {
						a.CloseReview()
					}
					label := i18n.T("ui.review.applyAll")
					if r.applyBusy {
						label = i18n.T("ui.review.applying")
					}
					if myui.PrimaryButton(c, label).Disabled(r.applyBusy || !reviewHasChanges(r.entries)).Clicked() {
						a.ApplyAsync()
					}
				})
			case r.preview != nil:
				p := r.preview
				myui.Text(c, i18n.Tf("ui.review.title", r.skill)).FontSize(16).Bold()
				myui.Row(c).Gap(8).AlignItems(myui.Center).Children(func() {
					myui.Text(c, previewMetaText(p)).SingleLine().TextColor(t.TextMuted)
				})
				if !p.HasChanges() {
					myui.Text(c, i18n.T("ui.review.upToDate")).TextColor(t.Success)
				}
				if p.LocalModified {
					// 上游没变时只是提醒；有变化时提示应用会覆盖本地改动。
					// 两种情形各一条整句：句子中段拼接会译歪。
					msg := i18n.T("ui.review.localModified")
					if p.HasChanges() {
						msg = i18n.T("ui.review.localModifiedWithChanges")
					}
					myui.Text(c, msg).TextColor(t.Warning)
				}
				myui.Scroll(c).Grow(1).Height(320).Children(func() {
					myui.Column(c).FillWidth().Gap(2).Children(func() {
						for _, f := range p.Files {
							a.viewDiffRow(c, f)
						}
					})
				})
				myui.Row(c).Gap(8).Justify(myui.End).Children(func() {
					if myui.Button(c, i18n.T("ui.common.cancel")).Clicked() {
						a.CloseReview()
					}
					label := i18n.T("ui.review.apply")
					if r.applyBusy {
						label = i18n.T("ui.review.applying")
					}
					// 无变化时禁用应用（ApplyAsync 对空名单也会直接返回）。
					if myui.PrimaryButton(c, label).Disabled(r.applyBusy || !p.HasChanges()).Clicked() {
						a.ApplyAsync()
					}
				})
			default:
				// 理论不可达：open 但无数据。给占位并允许关闭，绝不 panic。
				myui.Text(c, i18n.T("ui.review.noResult")).TextColor(t.TextMuted)
				if myui.Button(c, i18n.T("ui.common.close")).Clicked() {
					a.CloseReview()
				}
			}
		})
	})
}

// viewReviewEntry 渲染全部检查汇总里的一个技能小节。
func (a *App) viewReviewEntry(c *myui.Context, e skillReviewEntry) {
	t := c.Theme()
	myui.Column(c).FillWidth().Gap(4).Children(func() {
		myui.Row(c).FillWidth().Gap(8).AlignItems(myui.Center).Children(func() {
			myui.Text(c, e.skill).Bold()
			switch {
			case e.err != "":
				myui.Text(c, i18n.T("ui.review.checkFailed")).SingleLine().TextColor(t.Danger)
			case !e.preview.HasChanges():
				myui.Text(c, i18n.T("ui.review.latest")).SingleLine().TextColor(t.Success)
				if e.preview.LocalModified {
					myui.Text(c, i18n.T("ui.review.localChanged")).SingleLine().TextColor(t.Warning)
				}
			default:
				added, changed, removed := e.preview.Counts()
				myui.Text(c, i18n.Tf("ui.review.counts", added, changed, removed, len(e.preview.Files))).
					SingleLine().TextColor(t.Warning)
			}
		})
		// 错误详情或变更文件摘要（每个文件一行）。
		if e.err != "" {
			myui.Text(c, e.err).TextColor(t.Danger)
			return
		}
		for _, f := range e.preview.Files {
			a.viewDiffRow(c, f)
		}
	})
}

// viewDiffRow 渲染一个变更文件行：状态标签 + 路径 + ±行数 + 新内容哈希。
func (a *App) viewDiffRow(c *myui.Context, f store.FileDiff) {
	t := c.Theme()
	myui.Row(c).FillWidth().Padding(2, 8).Gap(8).AlignItems(myui.Center).Children(func() {
		myui.Text(c, diffStatusText(f.Status)).SingleLine().Width(diffStatusWidth()).
			TextColor(diffStatusColor(c, f.Status))
		myui.Text(c, f.Path).SingleLine().Grow(1)
		counts := fmt.Sprintf("+%d/-%d", f.Added, f.Removed)
		myui.Text(c, counts).SingleLine().TextColor(t.TextMuted)
		if f.NewHash != "" {
			myui.Text(c, f.NewHash[:8]).SingleLine().TextColor(t.TextMuted)
		}
	})
}

// previewMetaText 生成单技能评审面板的元信息行。上游没变（凭 commit 或
// 目录树哈希判定）时没有逐文件比较，只在本地被改过时才显示新旧哈希。
// 这是纯函数（没有 App/Context），文案直接用 i18n.T 现取。
func previewMetaText(p *store.UpdatePreview) string {
	parts := []string{i18n.Tf("ui.review.metaSource", p.SourceType)}
	if p.Commit != "" {
		parts = append(parts, i18n.Tf("ui.review.metaCommit", shortCommit(p.Commit)))
	}
	switch p.Basis {
	case store.BasisCommit:
		parts = append(parts, i18n.T("ui.review.basisCommit"))
	case store.BasisTree:
		parts = append(parts, i18n.T("ui.review.basisTree"))
	}
	if p.Basis == store.BasisContent || p.LocalModified {
		parts = append(parts, i18n.Tf("ui.review.oldHash", shortHash(p.OldHash)),
			i18n.Tf("ui.review.newHash", shortHash(p.NewHash)))
	}
	return strings.Join(parts, " · ")
}

// reviewHasChanges 报告全部检查汇总里是否存在任何可应用的变更。
func reviewHasChanges(entries []skillReviewEntry) bool {
	for _, e := range entries {
		if e.preview != nil && e.preview.HasChanges() {
			return true
		}
	}
	return false
}
