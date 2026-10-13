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

// countChip 是头部的一枚计数：状态点 + 「状态 数量」一个文本节点。
// 单节点让无障碍与测试都能读到完整的「已启用 3」；颜色统一用弱化色，
// 状态由色点表达。
func countChip(c *myui.Context, s store.State, n int) {
	t := c.Theme()
	myui.Row(c).Shrink(0).AlignItems(myui.Center).Gap(t.Space(1.5)).
		Children(func() {
			stateDot(c, s)
			myui.Text(c, stateText(s)+" "+itoa(n)).SingleLine().
				FontSize(t.FontSize - 1).TextColor(t.TextMuted)
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
// 对齐、与值的第一行对齐；wrap 允许长值换行。
func metaField(c *myui.Context, k, v string, wrap bool) {
	myui.Field(c, k, func() {
		t := myui.Text(c, v)
		if !wrap {
			t = t.SingleLine()
		}
	})
}

// rowCaption 生成列表行第二行的文案：来源 + 补充说明（缺 SKILL.md /
// 元数据损坏），用统一的 TextMuted 弱化色。元数据损坏时来源是推断值，
// 不再并列展示以免自相矛盾。
func rowCaption(s *store.Skill) string {
	if s.MetaErr != "" {
		return "元数据损坏"
	}
	cap := sourceText(s)
	if !s.HasSKILLMD && s.State != store.StateTrashed {
		cap += " · 缺少 SKILL.md"
	}
	return cap
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
func searchBox(c *myui.Context, query *string) myui.Element {
	t := c.Theme()
	row := myui.Row(c).AlignItems(myui.Center).
		Gap(t.Space(1.5)).Padding(t.Space(1.5), t.Space(2.5)).
		Radius(t.Radius).Background(t.Surface).Border(1, t.Border).
		Cursor(myui.CursorText)
	row.Children(func() {
		myui.Icon(c, iconSearch).TextColor(t.TextMuted)
		in := myui.TextInput(c, query).Grow(1).MinWidth(t.Space(15)).
			Placeholder("搜索技能").Label("搜索技能")
		// 焦点在输入框里时 Escape 清空（与 SearchField 的行为一致）。
		in.OnShortcut(0, myui.KeyEscape, func() { *query = "" })
	})
	return row
}
