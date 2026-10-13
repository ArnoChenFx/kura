// 视图共用的小组件与主题驱动的度量。所有尺寸都从 c.Theme() 的
// Space/FontSize/Radius 推导，避免散落的魔法数字。
package ui

import (
	"github.com/ArnoChenFx/kura/internal/store"
	myui "github.com/egoist/mygo/ui"
)

// stateText 把技能状态翻译成短标签。
func stateText(s store.State) string {
	switch s {
	case store.StateDisabled:
		return "已停用"
	case store.StateTrashed:
		return "回收站"
	default:
		return "已启用"
	}
}

// stateColor 返回状态点的颜色：启用=Success、停用=Warning、回收站=Danger。
func stateColor(c *myui.Context, s store.State) myui.Color {
	t := c.Theme()
	switch s {
	case store.StateDisabled:
		return t.Warning
	case store.StateTrashed:
		return t.Danger
	default:
		return t.Success
	}
}

// stateDot 画状态的小色点：8×8 的圆，颜色由 stateColor 决定。列表行与
// 详情头部共用，替代彩色的状态文字。
func stateDot(c *myui.Context, s store.State) {
	t := c.Theme()
	size := t.Space(2)
	myui.Box(c).Size(size, size).Shrink(0).Radius(size / 2).
		Background(stateColor(c, s)).AlignSelf(myui.Center)
}

// stateBadge 是详情头部用的状态胶囊：色点 + 状态词，浅色底圆角框，
// 比彩色文字更结构化。回收站里的技能是 Danger 描边，一眼可辨。
func stateBadge(c *myui.Context, s store.State) {
	t := c.Theme()
	fg := stateColor(c, s)
	bg := fg.Alpha(0.12)
	border := myui.Color{}
	if s == store.StateTrashed {
		// 回收站是危险区：加一圈淡淡的 Danger 描边强调。
		border = fg.Alpha(0.35)
	}
	myui.Row(c).Shrink(0).AlignItems(myui.Center).Gap(t.Space(1)).
		Padding(t.Space(0.5), t.Space(1.5)).Radius(t.Radius).
		Background(bg).Border(1, border).Children(func() {
		stateDot(c, s)
		myui.Text(c, stateText(s)).SingleLine().FontSize(t.FontSize - 1).TextColor(fg)
	})
}

// itoa 把小整数转成字符串（计数、±行数都在用）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// sourceText 概括技能的来源类型。
func sourceText(s *store.Skill) string {
	if s.Meta == nil {
		return "无元数据"
	}
	switch s.Meta.Source.Type {
	case store.SourceGitHub:
		return "GitHub"
	case store.SourceLocal:
		return "本地"
	case "":
		return "未知来源"
	default:
		return s.Meta.Source.Type
	}
}

// sourceURLText 概括元数据里的来源信息。
func sourceURLText(m *store.Meta) string {
	switch m.Source.Type {
	case store.SourceGitHub:
		return m.Source.URL
	case store.SourceLocal:
		return m.Source.Path
	default:
		if m.Source.Type == "" {
			return "未知"
		}
		return m.Source.Type
	}
}

// shortHash 缩短内容哈希便于展示。
func shortHash(h string) string {
	if h == "" {
		return "—"
	}
	if i := myIndex(h, ":"); i >= 0 && len(h) > i+13 {
		return h[:i+13] + "…"
	}
	return h
}

// myIndex 是 strings.Index 的本地小包装，避免每个调用点都导 strings。
func myIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// shortCommit 缩短 commit 哈希用于展示。
func shortCommit(s string) string {
	if len(s) > 10 {
		return s[:10]
	}
	return s
}

// metaField 是详情面板里的一行「标签：值」。放进 Form 里使用：标签右
// 对齐、与值的第一行对齐；wrap 允许长值换行。render 非空时改由它渲染值
// （目前只有「来源」用它换可点击的链接），其余字段传 nil 照旧按普通
// 文本渲染，不会意外变成链接。
func metaField(c *myui.Context, k, v string, wrap bool, render func()) {
	myui.Field(c, k, func() {
		if render != nil {
			render()
			return
		}
		t := myui.Text(c, v).TextColor(c.Theme().Text)
		if !wrap {
			t = t.SingleLine()
		}
	})
}

// metaFieldLink 是「来源」行专用的值：GitHub 来源渲染成可点击的链接
// （myui.Link 走 host 的 openURL 在系统浏览器打开，悬停自带下划线）；
// 本地/未知来源不用 Link——Link 只认 URL，传路径会打开失败——保持
// 可选中的普通文本（所在「元数据」组已是 Selectable，见 detailGroup）。
func metaFieldLink(c *myui.Context, m *store.Meta) {
	url := sourceURLText(m)
	myui.Field(c, "来源", func() {
		if m.Source.Type == store.SourceGitHub {
			myui.Link(c, url, url).FontSize(c.Theme().FontSize)
			return
		}
		metaField(c, "", url, true, nil)
	})
}

// diffStatusText 把文件差异状态翻译成短标签。
func diffStatusText(status string) string {
	switch status {
	case store.DiffAdded:
		return "新增"
	case store.DiffRemoved:
		return "删除"
	default:
		return "修改"
	}
}

// diffStatusColor 返回差异标签的颜色：新增绿、删除红、修改琥珀。
func diffStatusColor(c *myui.Context, status string) myui.Color {
	t := c.Theme()
	switch status {
	case store.DiffAdded:
		return t.Success
	case store.DiffRemoved:
		return t.Danger
	default:
		return t.Warning
	}
}

// sectionHeader 是小节标题：全大写风格的小号灰字（中文没有大写，用
// 字号与颜色区分层级）。
func sectionHeader(c *myui.Context, title string) {
	t := c.Theme()
	myui.Text(c, title).SingleLine().FontSize(t.FontSize - 2).
		FontWeight(600).TextColor(t.TextMuted)
}

// searchBox 是自建的搜索框：放大镜图标 + 输入框。MyGo 的 SearchField 把
// 英文占位符 "Search" 写死在内部输入框上（coreSearchField 里的
// Placeholder("Search")），返回的外层元素上再设 Placeholder 落不到
// 输入框，中文界面里不合适；因此照它的结构用公开 API 自建：同样的
// Surface 底、1px 边框、圆角与文字光标，另加 Escape 清空。
// 返回外层元素，调用方用 Grow/MaxWidth 控制宽度。
func searchBox(c *myui.Context, query *string, bg myui.Color) myui.Element {
	t := c.Theme()
	row := myui.Row(c).AlignItems(myui.Center).
		Gap(t.Space(1.5)).Padding(t.Space(1.5), t.Space(2.5)).
		Radius(t.Radius).Background(bg).Border(1, t.Border).
		Cursor(myui.CursorText)
	row.Children(func() {
		myui.Icon(c, iconSearch).TextColor(t.TextMuted)
		in := myui.TextInput(c, query).Grow(1).MinWidth(t.Space(15)).
			Placeholder("搜索技能").Label("搜索技能").
			// 外框由 row 画：输入框自身不再画边框与底色，避免框中套框。
			Border(0, myui.Color{}).Background(myui.Color{}).Padding(0)
		// 焦点在输入框里时 Escape 清空（与 SearchField 的行为一致）。
		in.OnShortcut(0, myui.KeyEscape, func() { *query = "" })
	})
	return row
}

// sourceIcon 返回卡片底行的来源图标。
func sourceIcon(s *store.Skill) *myui.SVG {
	if s.Meta == nil {
		return iconUnknown
	}
	switch s.Meta.Source.Type {
	case store.SourceGitHub:
		return iconGitHub
	case store.SourceLocal:
		return iconLocal
	}
	return iconUnknown
}

// pill 是卡片右下角的小标签：浅色底 + 同色文字。
func pill(c *myui.Context, text string, color myui.Color) {
	t := c.Theme()
	myui.Box(c).Shrink(0).Padding(t.Space(0.25), t.Space(1.5)).Radius(t.Radius).
		Background(color.Alpha(0.12)).Children(func() {
		myui.Text(c, text).SingleLine().FontSize(t.FontSize - 2).TextColor(color)
	})
}

// shortTime 把 RFC3339 时间截到分钟（2026-02-02 13:34），解析不了时原样返回。
func shortTime(s string) string {
	if len(s) >= 16 && s[10] == 'T' {
		return s[:10] + " " + s[11:16]
	}
	return s
}

// cardSource 是卡片名字下的一行来源：GitHub 显示 owner/repo，本地显示
// 「本地」。插件名等细节留给详情栏。
func cardSource(s *store.Skill) string {
	if s.Meta == nil {
		if s.MetaErr != "" {
			return "元数据损坏"
		}
		return "无元数据"
	}
	src := sourceText(s)
	if s.Meta.Source.Type == store.SourceGitHub {
		if r, _, _ := parseRepo(s.Meta.Source.URL); r != "" {
			src = r
		}
	}
	return src
}
