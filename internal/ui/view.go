package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/ArnoChenFx/kura/locales"
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
	cardHeight   = 168
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
		panic(locales.UiPanicIconDecode(err.Error()))
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
	a.ensureCurrentTranslations()

	a.viewTitlebar(c)
	// 三栏占满标题栏以下的剩余高度；标题栏自身不参与收缩（见 viewTitlebar）。
	// 左右两条分割线拖动改变两侧栏宽（偏好），中栏占剩余（见 viewGrip）。
	// 实际宽度按本帧窗口宽度（c.Size）每帧夹紧：窗口变窄时两侧一起让位，不
	// 改用户的偏好，变宽后自然恢复；它是偏好与窗口宽度的纯函数，所以连续帧
	// 不会抖动。
	a.bodyW, _ = c.Size()
	a.rootsEff, a.detailEff = fitWidths(a.bodyW, a.rootsW, a.detailW, sel != nil)
	rootsHi, detailHi := a.widthLimits(sel != nil)
	if a.market.screen != marketNone {
		// 技能市场页面替换三栏的内容区（标题栏仍在，见 market.go）。
		a.viewMarketPage(c)
	} else {
		body := myui.Row(c.Key("body")).Grow(1).MinHeight(0).AlignItems(myui.Stretch)
		body.Children(func() {
			a.viewRoots(c)
			a.viewGrip(c, &a.rootsW, a.rootsEff, 1, rootsMinW, rootsHi)
			myui.Column(c).Grow(1).MinWidth(0).FillHeight().Background(a.pal.canvas).Children(func() {
				a.viewMainHeader(c)
				a.viewFilterBar(c)
				var separator, gap float32
				if !c.Theme().Dark {
					separator, gap = 1, c.Theme().Space(2)
				}
				// 白色工具区与银灰画布留出间距；深色保留零高节点，
				// 避免主题切换改变后续列表的身份、丢失滚动位置。
				myui.Box(c).FillWidth().Height(separator).Shrink(0).Background(a.pal.line)
				myui.Box(c).Height(gap).Shrink(0)
				a.viewErrorBanner(c)
				a.viewSkills(c, rows)
				a.viewStatusBar(c)
			})
			if sel != nil {
				a.viewGrip(c, &a.detailW, a.detailEff, -1, detailMinW, detailHi)
				a.viewDetail(c, sel)
			}
		})
	}

	// 「移入回收站」与「永久删除」共用一个确认框；最后一个按钮是默认按钮（回车触发），
	// Escape 或点按钮后 *open 置 false。
	title, message, ok := locales.UiConfirmTrashTitle(),
		locales.UiConfirmTrashMessage(a.confirm.name), locales.UiConfirmTrashOK()
	if a.confirm.purge {
		title, ok = locales.UiConfirmPurgeTitle(), locales.UiActionPurge()
		message = locales.UiConfirmPurgeMessage(a.confirm.name)
	}
	if n := myui.AlertDialog(c, &a.confirm.open, title, message, locales.UiCommonCancel(), ok); n == 1 {
		if a.confirm.purge {
			a.PurgeAsync(a.confirm.name)
		} else {
			a.RemoveSkillAsync(a.confirm.name, a.confirm.state)
		}
	}

	// 更新评审面板（检查更新/全部检查的结果 + 应用/取消）。
	a.viewReviewModal(c)
	a.viewSnapshotsModal(c)

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
		Padding(0, bar.Right+16, 0, bar.Left+16).Background(a.pal.sidebar).
		AlignItems(myui.Center).Gap(t.Space(2)).DragWindow().Children(func() {
		// 标识：项目里的应用图标（四周透明，直接放进顶栏）。
		myui.Image(c, appIcon).Size(22, 22)
		myui.Text(c, "kura").FontWeight(600).SingleLine()
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
// 浅色导航用银灰底与白色选中项，深色沿用石墨层次。品牌标识在窗口顶栏里，
// 这里不再重复。
func (a *App) viewRoots(c *myui.Context) {
	t := c.Theme()
	myui.Column(c).Width(a.rootsEff).Shrink(0).FillHeight().Background(a.pal.sidebar).Children(func() {
		myui.Text(c, locales.UiRootsTitle()).FontSize(t.FontSize-2).FontWeight(600).TextColor(t.TextMuted).
			Padding(t.Space(6), t.Space(5), t.Space(3), t.Space(5))
		smoothScroll(myui.Scroll(c)).Grow(1).FillWidth().Children(func() {
			myui.Column(c).FillWidth().Padding(0, t.Space(3)).Gap(t.Space(1.5)).Children(func() {
				for i, root := range a.lib.Roots {
					a.rootItem(c, root, i == 0)
				}
			})
		})
		if a.libErr != "" {
			myui.Text(c, locales.UiRootsLoadFailed(a.libErr)).MaxLines(3).
				FontSize(t.FontSize-2).TextColor(t.Danger).Padding(t.Space(1), t.Space(4))
		}
		if a.settingsErr != "" {
			myui.Text(c, locales.UiRootsSettingsLoadFailed(a.settingsErr)).MaxLines(3).
				FontSize(t.FontSize-2).TextColor(t.Danger).Padding(t.Space(1), t.Space(4))
		}
		myui.Column(c).FillWidth().Padding(t.Space(2), t.Space(3), t.Space(3), t.Space(3)).
			Gap(t.Space(3)).Children(func() {
			ab := myui.ButtonBase(c).Label(locales.UiRootsAddFolder()).FillWidth().Disabled(a.adding).
				Justify(myui.Start).Gap(t.Space(2)).Padding(t.Space(2), t.Space(3)).
				Radius(t.Radius).Border(1, t.Border).TextColor(a.pal.textMid)
			buttonMotion(c, ab, !a.adding)
			if ab.Hovered() {
				ab.Background(t.SurfaceHover).TextColor(t.Text)
			}
			ab.Children(func() {
				myui.Icon(c, iconPlus)
				myui.Text(c, locales.UiRootsAddFolder())
			})
			if ab.Clicked() {
				a.AddRootAsync()
			}
			myui.Divider(c)
			a.viewRootsFooter(c)
		})
	})
}

// themeModes 是外观开关的三个选项，顺序即分段顺序。label 是显示名的取值
// 函数（locales）：每帧现取（见 viewThemeSwitch），语言一切就跟着变。
var themeModes = []struct {
	mode  string
	label func() string
	icon  *myui.SVG
}{
	{store.ThemeSystem, locales.UiThemeSystem, iconMonitor},
	{store.ThemeLight, locales.UiThemeLight, iconSun},
	{store.ThemeDark, locales.UiThemeDark, iconMoon},
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
//
// 后台查到新版本时（a.updateFound，由 selfupdate.Background 经 SetUpdateFound
// 发布），「检查更新」按钮右上角出现一个小蓝点；悬停提示也换成
// 一句「有新版本可用」。
func (a *App) viewRootsFooter(c *myui.Context) {
	t := c.Theme()
	myui.Row(c).FillWidth().AlignItems(myui.Center).Gap(t.Space(1)).Children(func() {
		a.viewThemeSwitch(c)
		myui.Spacer(c)
		tip := railUpdateTip(a.updateFound)
		// 「检查更新」按钮在后台查到新版本时右上角带一颗小蓝点（a.pal.notify，
		// Transparent 表示不带）。
		dot := myui.Transparent
		if a.updateFound {
			dot = a.pal.notify
		}
		a.railButton(c, iconUpdate, locales.UiActionCheckUpdate(), tip, a.checkingUpdate, dot, a.CheckAppUpdateAsync)
		a.railButton(c, iconSettings, locales.UiSettingsTitle(), locales.UiRailSettingsTip(), false, myui.Transparent, a.OpenSettings)
	})
}

// railUpdateTip 是左栏「检查更新」按钮的悬停提示：平时说明它是检查 kura 应用
// 自身的更新，后台查到新版本时换成「有新版本可用」。
func railUpdateTip(found bool) string {
	if found {
		return locales.UiRailUpdateAvailableTip()
	}
	return locales.UiRailCheckUpdateTip()
}

// railButton 是左栏底部的图标按钮：无底色，悬停时才出现浅色底，与 ghostButton
// 同一套样式。label 是按钮的名字（无障碍与测试查找用），tip 是悬停提示，可以
// 比名字更具体。dot 不是透明色时在按钮右上角画一颗这种颜色的小圆点（见
// notifyDot）：它是绝对定位的子元素，不改变按钮的位置与尺寸，也不吃点击。
//
// 是 App 的方法而不是自由函数：小圆点的描边要用左栏底色（a.pal.sidebar）
// 盖住下面的图标。
func (a *App) railButton(c *myui.Context, icon *myui.SVG, label, tip string, disabled bool, dot myui.Color, onClick func()) {
	t := c.Theme()
	b := myui.ButtonBase(c).Label(label).Tooltip(tip).Disabled(disabled).
		Padding(t.Space(1.75)).Radius(t.Radius).TextColor(t.TextMuted)
	hover := b.Hovered() && !disabled
	h := b.AnimateTo("hover", boolF(hover), interactionMotion)
	b.Background(t.SurfaceHover.Alpha(h)).TextColor(t.TextMuted.Mix(t.Text, h))
	buttonMotion(c, b, !disabled)
	if disabled {
		b.Opacity(0.45)
	}
	b.Children(func() {
		if dot.A != 0 {
			// 描边与按钮此刻的底色同色（悬停时是悬停底），否则会露出一圈不对色的边。
			ring := a.pal.sidebar
			if hover {
				ring = t.SurfaceHover
			}
			notifyDot(c, dot, ring)
		}
		myui.Icon(c, icon).FontSize(t.FontSize + 2)
	})
	if b.Clicked() {
		onClick()
	}
}

// notifyDot 是「有新版本」的小蓝点：画在所在按钮的右上角。它摆在按钮的
// **内边距盒**的右上角（Absolute 的 Top/Right 就相对该盒，按钮没有边框，
// 所以就是按钮自己的右上角），整个圆点都在按钮里，描边与悬停底因此自洽。
//
// 它是绝对定位的子元素，因此不参与父元素的尺寸计算——加上它前后，按钮的矩形
// 逐像素一致（update_dot_test.go 守着这一点）。PassThrough 让它不吃点击（用户的
// 点击必须落到按钮上），描一圈与按钮底色同色（ring）的细边把它与下面的图标分开。
func notifyDot(c *myui.Context, col, ring myui.Color) {
	// 圆点直径取 2 个间距单位（默认 8px），放在按钮右上角那块空出来的角上：
	// 图标居中、只占中间的 16px，所以小圆点盖不到它。
	d := c.Theme().Space(2)
	myui.Box(c.Key("update-dot")).Size(d, d).Radius(d/2).Background(col).
		Border(1, ring).Absolute().Top(0).Right(0).PassThrough()
}

// viewThemeSwitch 是左栏底部的外观开关：三个图标分段（跟随系统 / 浅色 / 深色），
// 选中的分段浮起。分段的点击在构建结束或查询响应时才写进 sel，所以要靠 Changed
// 在同一次构建里取到新选择，再交给 SetThemeAsync（幂等，重复构建不会重复写盘）；
// sel 每次构建都从 themeMode 重新推出。布局由 viewRootsFooter 负责。
func (a *App) viewThemeSwitch(c *myui.Context) {
	t := c.Theme()
	sel := themeIndex(a.themeMode)
	seg := myui.SegmentedBase(c, &sel, len(themeModes))
	track := seg.Track.Label(locales.UiThemeLabel()).Padding(t.Space(0.5)).Gap(t.Space(0.5)).
		Radius(t.Radius).Background(a.pal.card).Border(1, a.pal.line).Shrink(0)
	if !t.Dark {
		track.Background(t.Surface)
	}
	track.Children(func() {
		for i, m := range themeModes {
			b := seg.Segment(i).Label(m.label()).Tooltip(m.label()).
				Padding(t.Space(1.25), t.Space(2)).Radius(t.Radius - 2).TextColor(t.TextMuted)
			segmentSurface(b, t, i == sel)
			b.Children(func() {
				segmentIndicator(c, track.ID(), i == sel)
				myui.Icon(c, m.icon).FontSize(t.FontSize + 1)
			})
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
	b := myui.ButtonBase(c.Key(root)).Label(rootLabel(root)).Tooltip(root).
		FillWidth().Justify(myui.Start).Gap(t.Space(2)).
		Padding(t.Space(2), t.Space(2.5)).Radius(t.Radius).TextColor(a.pal.textMid)
	chosen := b.AnimateTo("chosen", boolF(root == a.rootSel), interactionMotion)
	hover := b.AnimateTo("hover", boolF(b.Hovered()), interactionMotion)
	b.Background(a.pal.sidebar.Mix(t.SurfaceHover, hover).Mix(a.pal.sel, chosen)).
		TextColor(a.pal.textMid.Mix(t.Text, chosen))
	if !t.Dark {
		b.Border(1, a.pal.line.Alpha(chosen)).
			Shadow(0, 2, 7, 0, a.pal.shadow.Mix(myui.Transparent, 1-chosen))
	}
	buttonMotion(c, b, true)
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
		if !t.Dark {
			// 标记只绘制，不改变目录行的点击、拖放和右键行为。
			myui.Box(c.Key("root-marker")).Absolute().Left(0).Top(12).Bottom(12).
				Width(3).Radius(1.5).Background(t.Accent.Alpha(chosen)).PassThrough()
		}
		icon := iconFolder
		if isDefault {
			icon = iconHome
		}
		myui.Box(c).Size(26, 26).Shrink(0).Radius(7).
			Background(t.Accent.Alpha(0.06 + 0.08*chosen)).Center().Children(func() {
			myui.Icon(c, icon).FontSize(14).TextColor(a.pal.textMid.Mix(t.Accent, chosen))
		})
		myui.Text(c, rootLabel(root)).SingleLine().Grow(1).MinWidth(0)
		if n, ok := a.rootCounts[root]; ok {
			n := myui.Text(c, itoa(n)).SingleLine().FontSize(t.FontSize-2).
				TextColor(t.TextMuted).Padding(2, 6).Radius(6).Background(a.pal.card.Alpha(0.6))
			countMotion(c, n)
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
	if m.Item(locales.UiMenuOpenFolder()).Chosen() {
		a.OpenPathAsync(root)
	}
	if m.Item(locales.UiMenuCopyPath()).Chosen() {
		a.copyPath(c, root)
	}
	if !isDefault {
		m.Separator()
		if m.Item(locales.UiMenuRemoveFromList()).Chosen() {
			a.RemoveRootAsync(root)
		}
	}
}

// copyPath 把路径放进剪贴板，状态行说一声。只在菜单项被选中的那一帧调用。
func (a *App) copyPath(c *myui.Context, path string) {
	c.WriteClipboard(path)
	a.status, a.statusErr = locales.UiStatusPathCopied(path), false
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

// viewMainHeader 把目录标题和操作收在同一行，完整路径保留在标题的悬停
// 提示里。窄窗口允许操作组整组换行，不压缩按钮或隐藏已有动作。
func (a *App) viewMainHeader(c *myui.Context) {
	t := c.Theme()
	isDefault := a.cfg.Root == a.lib.Default()
	myui.Row(c.Key("main-header")).FillWidth().Shrink(0).Wrap().
		Padding(t.Space(3), t.Space(5), t.Space(1), t.Space(5)).
		Background(a.pal.header).Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
		myui.Text(c, rootLabel(a.cfg.Root)).FontWeight(600).FontSize(t.FontSize + 4).
			SingleLine().Tooltip(a.cfg.Root).Grow(1).Basis(120).MinWidth(0)
		myui.Row(c).Shrink(0).Gap(t.Space(1)).AlignItems(myui.Center).Children(func() {
			if a.scanning {
				myui.Spinner(c)
			}
			ghostButton(c.Key("refresh"), iconRefresh, locales.UiActionRefresh(), a.scanning, a.ReloadAsync)
			if a.checkingAll || a.checking != "" {
				ghostButton(c.Key("cancel-check"), iconClose, locales.UiActionCancelCheck(), false, a.CancelCheck)
			} else {
				ghostButton(c.Key("check-all"), iconCheckAll, locales.UiActionCheckAll(), a.hasCheckTarget(), a.CheckAllAsync)
			}
			if isDefault {
				ghostButton(c.Key("root-action"), iconImport, locales.UiActionImportMeta(), false, a.ImportAsync)
			} else {
				ghostButton(c.Key("root-action"), iconUnlink, locales.UiActionRemoveRoot(), false, func() { a.RemoveRootAsync(a.cfg.Root) })
			}
			ghostButton(c.Key("market"), iconGlobe, locales.UiActionMarket(), false, a.OpenMarket)
			ib := myui.PrimaryButton(c.Key("install"), "").Label(locales.UiActionInstall()).Shrink(0).
				Padding(t.Space(2), t.Space(3))
			buttonMotion(c, ib, true)
			ib.Children(func() {
				myui.Icon(c, iconPlus).FontSize(t.FontSize)
				myui.Text(c, locales.UiActionInstall()).FontWeight(600)
			})
			if ib.Clicked() {
				a.OpenInstall()
			}
		})
	})
}

// viewFilterBar 把排列开关与搜索框放在同组，状态筛选可整组换到下一行。
// 最窄中栏也只需要两行，不会为排列按钮再占一整行；缩放时控件身份不变。
func (a *App) viewFilterBar(c *myui.Context) {
	t := c.Theme()
	en, dis, trash := a.counts()
	myui.Row(c.Key("filter-bar")).FillWidth().Wrap().
		Padding(t.Space(1), t.Space(5), t.Space(3), t.Space(5)).Gap(t.Space(2)).
		Background(a.pal.header).Justify(myui.SpaceBetween).AlignItems(myui.Center).Children(func() {
		myui.Row(c.Key("search-controls")).Grow(1).Basis(240).MaxWidth(440).MinWidth(0).
			Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
			searchBox(c.Key("search"), &a.query, a.pal.card).Grow(1).MinWidth(0)
			a.viewModeSwitch(c.Key("view-mode"))
		})
		filterTabs(c.Key("state-filter"), &a.filter,
			locales.UiFilterAll(en+dis), locales.UiFilterEnabled(en),
			locales.UiFilterDisabled(dis), locales.UiFilterTrashed(trash)).
			Shrink(1).MinWidth(0).MaxWidthPercent(100).Wrap()
	})
}

// viewModes 是排列模式开关的各个分段，顺序即分段顺序。label 是显示名的
// 取值函数（locales）：每帧现取（见 viewModeSwitch），语言一切就跟着变。
var viewModes = []struct {
	mode  string
	label func() string
	icon  *myui.SVG
}{
	{store.ViewGrid, locales.UiViewGrid, iconGrid},
	{store.ViewList, locales.UiViewList, iconRows},
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

// viewModeSwitch 是搜索框旁的排列开关：两个图标分段（卡片网格 / 列表），
// 选中的分段浮起，写法同 viewThemeSwitch。分段的点击在构建结束或查询响应时
// 才写进 sel，所以靠 Changed 在同一次构建里取到新选择再交给 SetViewAsync
// （幂等）；sel 每次构建都从 viewMode 重新推出。Shrink(0)：窄窗口里筛选栏
// 换行时它不被挤扁。
func (a *App) viewModeSwitch(c *myui.Context) {
	t := c.Theme()
	sel := viewIndex(a.viewMode)
	seg := myui.SegmentedBase(c, &sel, len(viewModes))
	track := seg.Track.Label(locales.UiViewLabel()).Padding(t.Space(0.5)).Gap(t.Space(0.5)).
		Radius(t.Radius).Background(a.pal.card).Border(1, a.pal.line).Shrink(0)
	if !t.Dark {
		track.Background(t.Surface)
	}
	track.Children(func() {
		for i, m := range viewModes {
			b := seg.Segment(i).Label(m.label()).Tooltip(m.label()).
				Padding(t.Space(1.5), t.Space(2)).Radius(t.Radius - 2).TextColor(t.TextMuted)
			segmentSurface(b, t, i == sel)
			b.Children(func() {
				segmentIndicator(c, track.ID(), i == sel)
				myui.Icon(c, m.icon).FontSize(t.FontSize + 1)
			})
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

// viewList 只构建可见的技能与少量缓冲行，由原生 List 管理可变行高与滚动
// 锚点。技能身份包含状态，同名的启用/停用副本不共用焦点或行内控件。
// 行按钮继续负责选择、右键和开关，不再给 List 绑定第二份选择状态。
func (a *App) viewList(c *myui.Context, rows []*store.Skill) {
	if len(rows) == 0 {
		a.viewGridEmpty(c)
		return
	}
	t := c.Theme()
	smoothScroll(myui.List(c.Key("local-skills:"+a.cfg.Root), a.list, len(rows)).
		Grow(1).FillWidth().MinHeight(0).Gap(t.Space(1.5)).
		Padding(0, t.Space(5), t.Space(3), t.Space(5)).
		ItemKey(func(i int) any { return skillKey(rows[i]) }).
		Rows(func(row myui.ListRow) {
			s := rows[row.Index]
			a.viewRow(row.Context, s, a.isSelected(s))
		}), uint64(a.gen))
}

// viewRow 是列表里的一个技能：头像、名称与描述占主区，右侧操作固定宽度；
// 来源与状态放在下方的可换行信息区，窄窗口不会把开关挤出行边界。
//
//	[头像] 名字                     [开关 / 恢复 / 转圈]
//	       描述（单行，超出省略）
//	       来源                    [状态标签，可换行]
//
// 行为与卡片一致：左键点击选中、右键只弹同一份菜单（不改选中）；停用与回收站的名字
// 变淡，回收站加删除线并把开关换成「恢复」。行自己画选中与悬停（同一圈边框）。
func (a *App) viewRow(c *myui.Context, s *store.Skill, selected bool) {
	t := c.Theme()
	busy := a.isBusy(s.Name)
	dim := s.State != store.StateEnabled
	row := myui.ButtonBase(c.Key(skillKey(s))).FillWidth().Justify(myui.Start).
		Padding(t.Space(2.5), t.Space(3)).Radius(t.Radius + 2)
	surfaceMotion(c, row, a.pal, selected, false)
	// 只有左键会选中这一行。右键只弹菜单（同卡片，见 viewCard）：菜单动作以
	// s 为准，不改选中。
	row.ContextMenu(func(m *myui.Menu) { a.cardMenu(c, m, s) })
	row.Children(func() {
		myui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(2)).Children(func() {
			myui.Row(c).FillWidth().Gap(t.Space(3)).AlignItems(myui.Start).Children(func() {
				avatar(c, a.pal, s.Name, t.Space(9), dim)
				myui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(1)).Children(func() {
					name := myui.Text(c, s.Name).SingleLine().FontWeight(600).Tooltip(s.Name)
					if dim {
						name.TextColor(t.TextMuted)
					}
					if s.State == store.StateTrashed {
						name.Strikethrough()
					}
					desc, color := a.descText(translateCardPrefix, s), t.TextMuted
					if desc == "" {
						desc, color = locales.UiCardNoDescription(), t.TextMuted.Alpha(0.6)
					}
					myui.Text(c, desc).SingleLine().FontSize(t.FontSize - 1).TextColor(color)
				})
				myui.Box(c).Width(t.Space(9)).Height(t.Space(9)).Shrink(0).Center().Children(func() {
					switch {
					case busy:
						myui.Spinner(c)
					case s.State == store.StateTrashed:
						ghostButton(c, iconRestore, locales.UiActionRestore(), false, func() { a.RestoreAsync(s.Name) })
					default:
						on := s.State == store.StateEnabled
						sw := kuraSwitch(c, &on, a.pal.toggle).Label(locales.UiCardSwitchLabel(s.Name))
						if sw.Changed() {
							a.ToggleAsync(s.Name, on)
						}
					}
				})
			})
			myui.Row(c).FillWidth().Children(func() {
				myui.Box(c).Width(t.Space(12)).Shrink(0)
				myui.Row(c).Grow(1).MinWidth(0).Wrap().Gap(t.Space(1.5)).AlignItems(myui.Center).Children(func() {
					myui.Row(c).Grow(1).Basis(140).MinWidth(0).Gap(t.Space(1)).AlignItems(myui.Center).Children(func() {
						myui.Icon(c, sourceIcon(s)).TextColor(t.TextMuted).FontSize(t.FontSize - 2).Shrink(0)
						myui.Text(c, cardSource(s)).SingleLine().Grow(1).MinWidth(0).
							FontSize(t.FontSize - 2).TextColor(t.TextMuted).Tooltip(cardSource(s))
					})
					if a.updates[s.Name] {
						pill(c, locales.UiPillHasUpdate(), t.Accent)
					}
					switch s.State {
					case store.StateDisabled:
						pill(c, locales.UiStateDisabled(), t.Warning)
					case store.StateTrashed:
						pill(c, locales.UiStateTrashed(), t.Danger)
					}
					if s.MetaErr != "" {
						pill(c, locales.UiPillBadMeta(), t.Danger)
					} else if !s.HasSKILLMD && s.State != store.StateTrashed {
						pill(c, locales.UiPillNoSkillMD(), t.Warning)
					}
				})
			})
		})
	})
	if row.Clicked() && !a.isSelected(s) {
		a.selectSkill(s)
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
	smoothScroll(myui.GridView(c.Key("local-grid:"+a.cfg.Root), a.grid, len(rows), cardMinWidth, cardHeight, func(i int) {
		c.SetTheme(t)
		a.viewCard(c, rows[i], i == a.selIdx)
		c.SetTheme(a.skin.cell)
	}), uint64(a.gen)).Grow(1).FillWidth().Padding(0, t.Space(4), t.Space(2), t.Space(4))
	c.SetTheme(t)
}

// viewGridEmpty 是没有卡片时的占位：区分「搜索无结果」「筛选下为空」与
// 「目录里没有技能」。
func (a *App) viewGridEmpty(c *myui.Context) {
	t := c.Theme()
	msg, hint := locales.UiEmptyNoSkills(), locales.UiEmptyNoSkillsHint()
	switch {
	case a.query != "":
		msg, hint = locales.UiEmptyNoMatch(a.query), locales.UiEmptyNoMatchHint()
	case a.filter != filterAll:
		// 三种筛选各一条完整文案（不拿状态标签拼接：英文里句子中间不该插
		// 一个大写开头的标签）。
		hint = ""
		switch a.filter {
		case filterEnabled:
			msg = locales.UiEmptyNoEnabled()
		case filterDisabled:
			msg = locales.UiEmptyNoDisabled()
		default:
			msg = locales.UiEmptyNoTrashed()
		}
	}
	empty := myui.Box(c.Key("empty-skills")).Grow(1).FillWidth().Center().Padding(t.Space(5))
	enterMotion(c, empty)
	empty.Children(func() {
		myui.Column(c).Gap(t.Space(3)).AlignItems(myui.Center).Children(func() {
			myui.Box(c).Size(t.Space(18), t.Space(18)).Radius(t.Space(5)).
				Background(a.pal.card).Border(1, a.pal.line).
				Shadow(0, 4, 12, 0, a.pal.shadow).Center().Children(func() {
				myui.Icon(c, iconInbox).TextColor(t.Accent).FontSize(t.FontSize * 2)
			})
			myui.Text(c, msg).FontWeight(600).FontSize(t.FontSize + 3).MaxWidth(280)
			if hint != "" {
				myui.Text(c, hint).TextColor(t.TextMuted).MaxWidth(280)
			}
			if a.query != "" {
				if myui.Button(c, locales.UiSearchClear()).Clicked() {
					a.query = ""
				}
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
	card := myui.Column(c).Fill().Padding(t.Space(4)).Gap(t.Space(3)).
		Radius(t.Radius + 4)
	// 选中与悬停都由卡片自己画：同一圈边框、同一个圆角（网格的单元格不画，
	// 见 skin.cell）。选中优先于悬停。
	surfaceMotion(c, card, a.pal, selected, true)
	// 只有左键会选中这张卡片（走网格单元格的点击，见 viewGrid）。右键只弹这
	// 张卡片自己的菜单：菜单里的动作都以 s 为准，与右栏在看哪个技能无关，所
	// 以右键不改选中，右栏保持原样。
	card.ContextMenu(func(m *myui.Menu) { a.cardMenu(c, m, s) })
	card.Children(func() {
		myui.Row(c).FillWidth().Shrink(0).Gap(t.Space(3)).AlignItems(myui.Center).Children(func() {
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
				ghostButton(c, iconRestore, locales.UiActionRestore(), false, func() { a.RestoreAsync(s.Name) })
			default:
				// Switch 的输入在构建时应用：Changed 返回 true 的这次构建里，
				// on 已是切换后的目标状态。
				on := s.State == store.StateEnabled
				sw := kuraSwitch(c, &on, a.pal.toggle).Label(locales.UiCardSwitchLabel(s.Name))
				if sw.Changed() {
					a.ToggleAsync(s.Name, on)
				}
			}
		})
		// 译文模式下，选中技能的卡片显示译文（其余卡片仍是原文，见 translate.go）。
		desc, color := a.descText(translateCardPrefix, s), t.TextMuted
		if desc == "" {
			desc, color = locales.UiCardNoDescription(), t.TextMuted.Alpha(0.6)
		}
		myui.Text(c, desc).MaxLines(2).FontSize(t.FontSize - 1).TextColor(color).Grow(1).MinHeight(0)
		myui.Row(c).FillWidth().Height(20).Shrink(0).Gap(t.Space(1.5)).AlignItems(myui.Center).Children(func() {
			if a.updates[s.Name] {
				pill(c, locales.UiPillHasUpdate(), t.Accent)
			}
			switch s.State {
			case store.StateDisabled:
				pill(c, locales.UiStateDisabled(), t.Warning)
			case store.StateTrashed:
				pill(c, locales.UiStateTrashed(), t.Danger)
			}
			if s.MetaErr != "" {
				pill(c, locales.UiPillBadMeta(), t.Danger)
			} else if !s.HasSKILLMD && s.State != store.StateTrashed {
				pill(c, locales.UiPillNoSkillMD(), t.Warning)
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
	if m.Item(locales.UiMenuOpenFolder()).Chosen() {
		a.OpenPathAsync(s.Dir)
	}
	if m.Item(locales.UiMenuRevealFolder()).Chosen() {
		a.RevealAsync(s.Dir)
	}
	if s.State != store.StateTrashed {
		if m.Item(locales.UiMenuOpenSkillMD()).Disabled(!s.HasSKILLMD).Chosen() {
			a.OpenPathAsync(filepath.Join(s.Dir, "SKILL.md"))
		}
	}
	if m.Item(locales.UiMenuCopyPath()).Chosen() {
		a.copyPath(c, s.Dir)
	}
	m.Separator()
	trash := func() {
		if m.Item(locales.UiMenuMoveTrash()).Disabled(busy).Chosen() {
			a.confirm = confirmState{open: true, name: s.Name, state: s.State}
		}
	}
	switch s.State {
	case store.StateEnabled:
		if m.Item(locales.UiMenuDisable()).Disabled(busy).Chosen() {
			a.ToggleAsync(s.Name, false)
		}
		if m.Item(locales.UiActionCheckUpdate()).Disabled(busy || a.hasCheckTarget()).Chosen() {
			a.CheckAsync(s.Name)
		}
		m.Separator()
		trash()
	case store.StateDisabled:
		if m.Item(locales.UiMenuEnable()).Disabled(busy).Chosen() {
			a.ToggleAsync(s.Name, true)
		}
		m.Separator()
		trash()
	case store.StateTrashed:
		if m.Item(locales.UiMenuRestore()).Disabled(busy).Chosen() {
			a.RestoreAsync(s.Name)
		}
		m.Separator()
		if m.Item(locales.UiMenuPurge()).Disabled(busy).Chosen() {
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
	if a.detailSel != skillKey(s) {
		a.detailSel = skillKey(s)
		a.detailTab = 0
	}
	panel := myui.Column(c.Key("detail-pane")).Width(a.detailEff).Shrink(0).FillHeight().Background(a.pal.panel)
	detailMotion(c, panel)
	panel.Children(func() {
		smoothScroll(myui.Scroll(c)).Grow(1).FillWidth().Children(func() {
			myui.Column(c).FillWidth().Padding(t.Space(5), t.Space(5)).Gap(t.Space(4)).Children(func() {
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
								pill(c, locales.UiPillHasUpdate(), t.Accent)
							}
						})
					})
					ghostButton(c, iconClose, locales.UiActionCloseDetail(), false, a.clearSelection)
				})
				a.viewTranslateBar(c)
				// 概览/全文 tab：绑定 a.detailTab。点选在下一次构建生效，
				// 两个页面都按 a.detailTab 分支，本帧内的切换下一帧生效。
				myui.Tabs(c, &a.detailTab, locales.UiDetailTabOverview(), locales.UiDetailTabFull()).FillWidth()
				a.detailBody(c, s)
			})
		})
		myui.Divider(c)
		myui.Box(c).FillWidth().Padding(t.Space(3), t.Space(5)).Background(a.pal.card).Children(func() {
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
		myui.Text(c, locales.UiPillNoSkillMD()).TextColor(t.Warning)
	}
	a.detailGroup(c, locales.UiDetailMetaGroup(), true, func() { a.viewDetailMeta(c, s) })
	a.detailGroup(c, locales.UiDetailLocation(), true, func() {
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
			myui.Text(c, locales.UiTranslateLabel()).SingleLine().FontSize(t.FontSize - 1).TextColor(t.TextMuted)
			myui.Spacer(c)
			sel := 0
			if a.trans.on {
				sel = 1
			}
			if myui.Segmented(c, &sel, locales.UiTranslateOriginal(), locales.UiTranslateTranslated()).
				Label(locales.UiTranslateDisplayLang()).Shrink(0).Changed() {
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
					myui.Text(c, locales.UiTranslateNotConfigured()).TextColor(t.Warning)
					myui.Row(c).FillWidth().Children(func() {
						if myui.Button(c, locales.UiTranslateOpenSettings()).Clicked() {
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
			myui.Text(c, locales.UiTranslateInProgress()).FontSize(t.FontSize - 1).TextColor(t.TextMuted)
		})
	case e.err != "":
		myui.Text(c, locales.UiTranslateFailed(e.err)).MaxLines(4).
			FontSize(t.FontSize - 1).TextColor(t.Danger)
		if myui.Button(c, locales.UiTranslateRetry()).Clicked() {
			delete(a.trans.items, detailDescKey(s.Name))
			delete(a.trans.items, cardDescKey(s.Name))
			a.ensureTranslations(s)
		}
	default:
		myui.Text(c, locales.UiTranslateNote(a.settings.LangLabel())).
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
	doc := a.skillDocs[skillKey(s)]
	switch {
	case doc.loading:
		myui.Text(c, locales.UiDetailLoading()).TextColor(c.Theme().TextMuted)
	case doc.noMD:
		myui.Text(c, locales.UiDetailNoSkillMD()).TextColor(c.Theme().TextMuted)
	case doc.err != nil:
		myui.Text(c, doc.err.Error()).TextColor(c.Theme().Danger)
	default:
		// 渲染进外层已有的 Scroll，宽度铺满（官方视图自己处理块内布局）。
		if doc.truncated {
			myui.Text(c, locales.UiMarkdownTruncated()).TextColor(c.Theme().TextMuted)
		}
		a.viewFullTextBody(c, s, doc.src)
	}
}

// viewFullTextBody 渲染全文正文。原文模式直接渲染 SKILL.md。译文模式下：译文还在
// 路上时先显示原文、顶上一行「翻译全文中…」，译文到了替换成译文；失败时说明原因，
// 仍显示原文。翻译只读缓存，发起请求只经 ensureDocTranslation（受缓存占位保护，
// 同一原文只发一次）。翻译 API 未配好时不发请求，显示原文（引导在开关条里）。
func (a *App) viewFullTextBody(c *myui.Context, s *store.Skill, src string) {
	a.viewDocTranslation(c, docTransKey(s.Dir), src)
}

// fullTextDoc 是一个技能的 SKILL.md 读盘结果，三种状态互斥：
// src 非空是读到了正文；loading 为真是在后台读；noMD 为真是没有 SKILL.md；
// err 非空是读盘失败。后三项存的是语言无关的状态而不是文案：后台任务跑完时
// 语言可能已经变了，文案（含错误里的原因）在 viewDetailFullText 里现取。
type fullTextDoc struct {
	src       string
	truncated bool
	loading   bool
	noMD      bool
	err       error
}

// startFullTextLoad 确保技能的 SKILL.md 有一份缓存：没有就去后台读
// （key 包含状态和技能名，启用副本与回收站里的同名副本彼此独立）。
// 任务发出前先占位「正在读取」，避免重复发任务；读结果回写前检查
// docGen，扫描已刷新过缓存的任务直接丢弃。startFullTextLoad 只在
// viewDetailFullText（UI 线程）里调用。
func (a *App) startFullTextLoad(s *store.Skill) {
	if a.skillDocs == nil {
		a.skillDocs = map[string]fullTextDoc{}
	}
	key := skillKey(s)
	if _, ok := a.skillDocs[key]; ok {
		return
	}
	a.skillDocs[key] = fullTextDoc{loading: true}
	dir, hasMD, gen := s.Dir, s.HasSKILLMD, a.docGen
	a.spawn(func() {
		doc := fullTextDoc{noMD: true}
		if hasMD {
			data, truncated, err := store.ReadSkillDocument(dir, fullTextMaxSource)
			if err != nil {
				// 错误文字随语言变（i18n.Errorf 到显示时才取文案），
				// 所以这里存错误本身，不存字符串。
				doc = fullTextDoc{err: i18n.NewError("uiDetailReadFailed", func() string { return locales.UiDetailReadFailed(err.Error()) })}
			} else {
				doc = fullTextDoc{src: data, truncated: truncated}
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
			a.skillDocs[key] = doc
		})
	})
}

// detailGroup 是详情里的一个信息块：浅色用银灰内嵌面，深色用提亮的圆角框。
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
			Radius(t.Radius).Background(a.pal.group).Border(1, a.pal.groupLine)
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
		myui.Text(c, locales.UiDetailMetaFailed(s.MetaErr)).TextColor(t.Danger)
		return
	default:
		myui.Text(c, locales.UiDetailNoMeta()).TextColor(t.TextMuted)
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
				metaField(c, locales.UiDetailInstalledAt(), shortTime(m.InstalledAt), false, nil)
			}
			if m.UpdatedAt != "" {
				metaField(c, locales.UiDetailUpdatedAt(), shortTime(m.UpdatedAt), false, nil)
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
			myui.Text(c, locales.UiDetailBusy(s.Name)).SingleLine().TextColor(t.TextMuted)
		})
		return
	}
	if s.State == store.StateEnabled || s.State == store.StateDisabled {
		myui.Row(c).FillWidth().Children(func() {
			if myui.Button(c, locales.UiSnapshotHistory()).Clicked() {
				a.OpenSnapshots(s)
			}
		})
	}
	// trashButton 是「移入回收站」：Label 保持「详情:移入回收站」供测试与无障碍。
	trashButton := func() {
		myui.Spacer(c)
		b := myui.ButtonBase(c).Label(locales.UiDetailTrashLabel()).Tooltip(locales.UiActionMoveTrash()).
			Padding(t.Space(1.75)).Radius(t.Radius).TextColor(t.Danger)
		if b.Hovered() {
			b.Background(t.Danger.Alpha(0.1))
		}
		b.Children(func() { myui.Icon(c, iconTrash).FontSize(t.FontSize + 2) })
		if b.Clicked() {
			a.confirm = confirmState{open: true, name: s.Name, state: s.State}
		}
	}
	myui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(myui.Center).Children(func() {
		switch s.State {
		case store.StateEnabled:
			// 检查更新：预览上游差异，弹出评审面板（不直接改盘）。
			if a.checkingAll || a.checking == s.Name {
				if myui.Button(c, locales.UiActionCancelCheck()).Clicked() {
					a.CancelCheck()
				}
			} else {
				ub := myui.PrimaryButton(c, "").Label(locales.UiDetailCheckLabel()).Disabled(a.hasCheckTarget())
				ub.Children(func() {
					myui.Icon(c, iconUpdate)
					myui.Text(c, locales.UiActionCheckUpdate())
				})
				if ub.Clicked() {
					a.CheckAsync(s.Name)
				}
			}
			if myui.Button(c, locales.UiMenuDisable()).Clicked() {
				a.ToggleAsync(s.Name, false)
			}
			trashButton()
		case store.StateDisabled:
			if myui.PrimaryButton(c, locales.UiMenuEnable()).Clicked() {
				a.ToggleAsync(s.Name, true)
			}
			trashButton()
		case store.StateTrashed:
			rb := myui.PrimaryButton(c, "").Label(locales.UiMenuRestore())
			rb.Children(func() {
				myui.Icon(c, iconRestore)
				myui.Text(c, locales.UiMenuRestore())
			})
			if rb.Clicked() {
				a.RestoreAsync(s.Name)
			}
			// 永久删除：危险色的文字按钮，靠右放；走确认框，删除前 store 先快照。
			myui.Spacer(c)
			pb := myui.ButtonBase(c).Label(locales.UiDetailPurgeLabel()).Tooltip(locales.UiActionPurgeTooltip()).
				Padding(t.Space(1.75), t.Space(2.5)).Radius(t.Radius).TextColor(t.Danger)
			if pb.Hovered() {
				pb.Background(t.Danger.Alpha(0.1))
			}
			pb.Children(func() {
				myui.Icon(c, iconTrash).FontSize(t.FontSize + 1)
				myui.Text(c, locales.UiActionPurge())
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
		myui.Text(c, locales.UiStatusTotal(en+dis)).
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
				myui.Text(c, locales.UiActionCheckAll()).FontSize(16).Bold()
				if len(r.entries) == 0 {
					myui.Text(c, locales.UiReviewNothingToCheck()).TextColor(t.TextMuted)
				}
				smoothScroll(myui.Scroll(c)).Grow(1).Height(340).Children(func() {
					myui.Column(c).FillWidth().Gap(10).Children(func() {
						for i := range r.entries {
							a.viewReviewEntry(c.Key(r.entries[i].skill), &r.entries[i])
						}
					})
				})
				myui.Row(c).Gap(8).Justify(myui.End).Children(func() {
					if myui.Button(c, locales.UiCommonClose()).Clicked() {
						a.CloseReview()
					}
					if r.failedUpdateCount() > 0 {
						if myui.Button(c, locales.UiReviewRetryFailed()).Disabled(r.applyBusy).Clicked() {
							a.RetryFailedUpdates()
						}
					}
					label := locales.UiReviewApplySelected(r.selectedUpdateCount())
					if r.applyBusy {
						label = locales.UiReviewApplying()
					}
					if myui.PrimaryButton(c, label).Disabled(r.applyBusy || r.selectedUpdateCount() == 0).Clicked() {
						a.ApplyAsync()
					}
				})
			case r.preview != nil:
				p := r.preview
				myui.Text(c, locales.UiReviewTitle(r.skill)).FontSize(16).Bold()
				myui.Row(c).Gap(8).AlignItems(myui.Center).Children(func() {
					myui.Text(c, previewMetaText(p)).SingleLine().TextColor(t.TextMuted)
				})
				if !p.HasChanges() {
					myui.Text(c, locales.UiReviewUpToDate()).TextColor(t.Success)
				}
				if p.LocalModified {
					// 上游没变时只是提醒；有变化时提示应用会覆盖本地改动。
					// 两种情形各一条整句：句子中段拼接会译歪。
					msg := locales.UiReviewLocalModified()
					if p.HasChanges() {
						msg = locales.UiReviewLocalModifiedWithChanges()
					}
					myui.Text(c, msg).TextColor(t.Warning)
				}
				smoothScroll(myui.Scroll(c)).Grow(1).Height(320).Children(func() {
					myui.Column(c).FillWidth().Gap(2).Children(func() {
						for _, f := range p.Files {
							a.viewDiffRow(c, f)
						}
					})
				})
				myui.Row(c).Gap(8).Justify(myui.End).Children(func() {
					if myui.Button(c, locales.UiCommonCancel()).Clicked() {
						a.CloseReview()
					}
					label := locales.UiReviewApply()
					if r.applyBusy {
						label = locales.UiReviewApplying()
					}
					// 无变化时禁用应用（ApplyAsync 对空名单也会直接返回）。
					if myui.PrimaryButton(c, label).Disabled(r.applyBusy || !p.HasChanges()).Clicked() {
						a.ApplyAsync()
					}
				})
			default:
				// 理论不可达：open 但无数据。给占位并允许关闭，绝不 panic。
				myui.Text(c, locales.UiReviewNoResult()).TextColor(t.TextMuted)
				if myui.Button(c, locales.UiCommonClose()).Clicked() {
					a.CloseReview()
				}
			}
		})
	})
}

// viewReviewEntry 渲染全部检查汇总里的一个技能小节。
func (a *App) viewReviewEntry(c *myui.Context, e *skillReviewEntry) {
	t := c.Theme()
	myui.Column(c).FillWidth().Gap(4).Children(func() {
		myui.Row(c).FillWidth().Gap(8).AlignItems(myui.Center).Children(func() {
			if e.canApply() {
				myui.Checkbox(c, &e.selected, "").Label(locales.UiReviewSelectSkill(e.skill)).Disabled(a.review.applyBusy)
			}
			myui.Text(c, e.skill).Bold()
			switch {
			case e.applied:
				myui.Text(c, locales.UiReviewUpdated()).TextColor(t.Success)
			case e.err != "":
				myui.Text(c, locales.UiReviewCheckFailed()).SingleLine().TextColor(t.Danger)
			case !e.preview.HasChanges():
				myui.Text(c, locales.UiReviewLatest()).SingleLine().TextColor(t.Success)
				if e.preview.LocalModified {
					myui.Text(c, locales.UiReviewLocalChanged()).SingleLine().TextColor(t.Warning)
				}
			default:
				added, changed, removed := e.preview.Counts()
				myui.Text(c, locales.UiReviewCounts(added, changed, removed, len(e.preview.Files))).
					SingleLine().TextColor(t.Warning)
			}
		})
		// 错误详情或变更文件摘要（每个文件一行）。
		if e.err != "" {
			myui.Text(c, e.err).TextColor(t.Danger)
			return
		}
		if e.applyErr != "" {
			myui.Text(c, e.applyErr).TextColor(t.Danger)
		}
		if !e.applied && e.preview.LocalModified && e.preview.HasChanges() {
			myui.Text(c, locales.UiReviewLocalModifiedWithChanges()).TextColor(t.Warning)
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
// 这是纯函数（没有 App/Context），文案直接用 locales.Xxx() 现取。
func previewMetaText(p *store.UpdatePreview) string {
	parts := []string{locales.UiReviewMetaSource(p.SourceType)}
	if p.Commit != "" {
		parts = append(parts, locales.UiReviewMetaCommit(shortCommit(p.Commit)))
	}
	switch p.Basis {
	case store.BasisCommit:
		parts = append(parts, locales.UiReviewBasisCommit())
	case store.BasisTree:
		parts = append(parts, locales.UiReviewBasisTree())
	}
	if p.Basis == store.BasisContent || p.LocalModified {
		parts = append(parts, locales.UiReviewOldHash(shortHash(p.OldHash)),
			locales.UiReviewNewHash(shortHash(p.NewHash)))
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
