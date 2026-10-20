package ui

// kura 的配色：中性石墨灰黑与纸白，导航、画布、检查器和卡片逐级分层。
// 主操作用银白或墨灰，薄荷绿表示启用；避免大面积的蓝紫色倾向。

import (
	"hash/fnv"
	"time"

	"github.com/ArnoChenFx/kura/internal/store"
	myui "github.com/egoist/mygo/ui"
)

// palette 是主题之外、kura 自己用的颜色。
type palette struct {
	sidebar myui.Color // 左栏底色
	canvas  myui.Color // 中栏底色
	panel   myui.Color // 详情检查器的底色
	card    myui.Color // 卡片、详情分组与搜索框的底色（比底色浅一档）
	line    myui.Color // 卡片与分隔线的描边
	shadow  myui.Color // 卡片的轻投影
	sel     myui.Color // 左栏选中项的底色
	textMid myui.Color // 左栏未选中项的文字（介于正文与弱化色之间）
	toggle  myui.Color // 开关「开」的颜色
	// notify 是「有新版本」的小圆点的颜色：一个真正的蓝（色相近 210°），
	// 与中性的主操作色明显不同，但不刺眼。
	notify myui.Color
	// 卡片的三种交互态：悬停时底色与描边都亮（深色）/暗（浅色）一点，
	// 选中时描边是强调色、底色带一丝强调色。
	cardHover, lineHover, cardSel myui.Color
	// avatars 是头像色板：低饱和的暖色与青绿，刻意不含蓝紫，
	// 不和强调色抢戏。
	avatars []myui.Color
}

// skin 是某个外观（浅/深）下的全部颜色。
type skin struct {
	theme *myui.Theme
	pal   palette
	// cell 是构建卡片网格时用的主题：与 theme 相同，只是 GridView 给每个
	// 单元格画选中框与悬停底用的四个颜色（见 newSkin）都是透明的。
	cell *myui.Theme
}

// scheme 是浅色与深色两个皮肤。
type scheme struct {
	light, dark *skin
}

// apply 让本帧使用 mode 指定的皮肤并返回它：store.ThemeLight、ThemeDark
// 固定用浅/深色，其余（跟随系统）看系统当前的外观。每帧开头 c.Theme()
// 是系统的默认主题，所以读 Dark 得到的是系统外观，不是上一帧设的皮肤。
func (s *scheme) apply(c *myui.Context, mode string) *skin {
	dark := c.Theme().Dark
	switch mode {
	case store.ThemeLight:
		dark = false
	case store.ThemeDark:
		dark = true
	}
	sk := s.light
	if dark {
		sk = s.dark
	}
	c.SetTheme(sk.theme)
	return sk
}

// tone 是一个皮肤的颜色清单（十六进制），newSkin 把它换成主题与表面色。
type tone struct {
	accent, accentHover, accentPressed, accentText string
	toggle                                         string
	// notify 是「有新版本」圆点的颜色（见 palette.notify）。
	notify                   string
	success, warning, danger string
	// bg 是弹窗等「浮起」的面板色，和卡片同档。
	bg, surface, surfaceHover, surfacePressed string
	border, text, muted, textMid              string
	sidebar, canvas, panel, card, line, sel   string
	shadow                                    myui.Color
	avatars                                   []string
}

// newSkin 以 MyGo 的浅色/深色主题为底，按 k 替换颜色并设定圆角。
func newSkin(dark bool, k tone) *skin {
	t := myui.LightTheme()
	if dark {
		t = myui.DarkTheme()
	}
	hx := myui.Hex
	t.Background, t.Surface, t.SurfaceHover, t.SurfacePressed, t.Border =
		hx(k.bg), hx(k.surface), hx(k.surfaceHover), hx(k.surfacePressed), hx(k.border)
	t.Text, t.TextMuted = hx(k.text), hx(k.muted)
	t.Accent, t.AccentHover, t.AccentPressed, t.AccentText =
		hx(k.accent), hx(k.accentHover), hx(k.accentPressed), hx(k.accentText)
	t.Success, t.Warning, t.Danger = hx(k.success), hx(k.warning), hx(k.danger)
	t.Focus = hx(k.accent).Alpha(0.5)
	t.Selection = hx(k.accent).Alpha(0.25)
	t.Radius = 10

	avatars := make([]myui.Color, len(k.avatars))
	for i, a := range k.avatars {
		avatars[i] = hx(a)
	}
	pal := palette{
		sidebar: hx(k.sidebar), canvas: hx(k.canvas), panel: hx(k.panel), card: hx(k.card), line: hx(k.line),
		shadow: k.shadow, sel: hx(k.sel), textMid: hx(k.textMid), toggle: hx(k.toggle),
		notify: hx(k.notify), avatars: avatars,
	}
	// 向正文色混一点：深色里变亮、浅色里变暗，两个皮肤用同一条规则。
	pal.cardHover = pal.card.Mix(t.Text, 0.035)
	pal.lineHover = pal.line.Mix(t.Text, 0.22)
	pal.cardSel = pal.card.Mix(t.Accent, 0.07)

	// GridView 给每个单元格自己画一层选中框与悬停底：选中（网格有焦点）时是
	// 2px 的 Accent 边加 Accent 淡底，选中（无焦点）时是 SurfacePressed 底加
	// 2px 的 Border 边，悬停时是 SurfaceHover 底，圆角是主题的 Radius。卡片
	// 自己的圆角更大、边框更细，两层叠在一起就是「高亮框和卡片边框对不齐」。
	// 单元格的颜色取自构建网格时的主题，所以给网格一份把这四个颜色换成透明
	// 的主题，单元格什么也不画，选中与悬停全由卡片自己画。
	cell := *t
	cell.Accent, cell.SurfacePressed, cell.SurfaceHover, cell.Border =
		myui.Transparent, myui.Transparent, myui.Transparent, myui.Transparent
	return &skin{theme: t, pal: pal, cell: &cell}
}

// kuraScheme 是应用的配色。
var kuraScheme = &scheme{
	light: newSkin(false, tone{
		accent: "#3e3e3a", accentHover: "#30302d", accentPressed: "#232320", accentText: "#ffffff",
		toggle:  "#36a874",
		notify:  "#2f7dd1",
		success: "#2f9e6a", warning: "#b7791f", danger: "#d64550",
		bg: "#ffffff", surface: "#efefec", surfaceHover: "#e7e7e3", surfacePressed: "#deded9",
		border: "#deded9", text: "#272724", muted: "#74746e", textMid: "#4d4d47",
		sidebar: "#efefec", canvas: "#f6f6f4", panel: "#fbfbf9", card: "#ffffff", line: "#e1e1dd", sel: "#dfdfd9",
		shadow:  myui.RGBA(25, 26, 23, 0.045),
		avatars: []string{"#488779", "#a08038", "#b36870", "#b47850", "#737870", "#7a8b4c", "#a16c84", "#8a7055"},
	}),
	dark: newSkin(true, tone{
		accent: "#d4d4cf", accentHover: "#ebebe5", accentPressed: "#babab3", accentText: "#242421",
		toggle:  "#62bf89",
		notify:  "#5aa9f0",
		success: "#6fd99b", warning: "#e5b567", danger: "#e5707a",
		bg: "#262626", surface: "#292929", surfaceHover: "#323232", surfacePressed: "#3b3b39",
		border: "#3d3d3a", text: "#f0f0ed", muted: "#989891", textMid: "#c0c0b9",
		sidebar: "#171717", canvas: "#1e1e1e", panel: "#222222", card: "#282828", line: "#3a3a38", sel: "#333332",
		shadow:  myui.RGBA(0, 0, 0, 0.10),
		avatars: []string{"#94bcae", "#c8b184", "#ca9da1", "#cfaa8b", "#adb3a5", "#b4bd8e", "#bca0b0", "#c4af94"},
	}),
}

// avatarColor 返回技能名在色板 pal 里对应的头像颜色，同名技能颜色稳定。
func avatarColor(pal palette, name string) myui.Color {
	h := fnv.New32a()
	h.Write([]byte(name))
	// FNV 的低位混得不够匀，再过一遍整数混合，颜色才分布得开。
	x := h.Sum32()
	x ^= x >> 16
	x *= 0x7feb352d
	x ^= x >> 15
	x *= 0x846ca68b
	x ^= x >> 16
	return pal.avatars[x%uint32(len(pal.avatars))]
}

// avatarInitial 是头像里的字：名字的第一个字母或汉字（大写）。
func avatarInitial(name string) string {
	for _, r := range name {
		if r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		return string(r)
	}
	return "?"
}

// avatar 画技能头像：圆角方块，淡色底 + 同色首字母。dim 时降低不透明度
// （停用/回收站）。
func avatar(c *myui.Context, pal palette, name string, size float32, dim bool) {
	t := c.Theme()
	col := avatarColor(pal, name)
	box := myui.Box(c).Size(size, size).Shrink(0).Radius(t.Radius).
		Background(col.Alpha(0.12)).Border(1, col.Alpha(0.16)).Center()
	if dim {
		box.Opacity(0.55)
	}
	box.Children(func() {
		myui.Text(c, avatarInitial(name)).Bold().FontSize(size * 0.45).TextColor(col)
	})
}

// ghostButton 是无底色的图标按钮：悬停时才出现浅色底。Label 与 Tooltip
// 都用 label，供无障碍、悬停提示与测试。
func ghostButton(c *myui.Context, icon *myui.SVG, label string, disabled bool, onClick func()) {
	t := c.Theme()
	b := myui.ButtonBase(c).Label(label).Tooltip(label).Disabled(disabled).
		Padding(t.Space(1.75)).Radius(t.Radius).TextColor(t.TextMuted)
	hover := b.AnimateTo("hover", boolF(b.Hovered() && !disabled), interactionMotion)
	b.Background(t.SurfaceHover.Alpha(hover)).TextColor(t.TextMuted.Mix(t.Text, hover))
	buttonMotion(c, b, !disabled)
	if disabled {
		b.Opacity(0.45)
	}
	b.Children(func() { myui.Icon(c, icon).FontSize(t.FontSize + 2) })
	if b.Clicked() {
		onClick()
	}
}

// kuraSwitch 是开关：尺寸、动画与白色圆钮和 MyGo 的 Switch 一致，「开」的
// 颜色由 onColor 指定，不必与主题的强调色相同。交互反馈：
//   - 悬停：指针变手形，轨道向正文色靠一点（深色里变亮、浅色里变暗），旋钮
//     周围出现一圈淡淡的光晕；
//   - 按下：轨道再靠一点，旋钮朝行进方向拉长；
//   - 键盘聚焦：MyGo 默认的聚焦环。
//
// 反馈都用短动画过渡；系统要求减少动效时直接到位。
func kuraSwitch(c *myui.Context, on *bool, onColor myui.Color) myui.Element {
	t := c.Theme()
	sw := myui.SwitchBase(c, on).Size(t.Space(9), t.Space(5)).Radius(t.Space(2.5)).
		Cursor(myui.CursorPointer)
	pos := sw.Animate("knob", boolF(*on), 140*time.Millisecond)
	hover := sw.Animate("hover", boolF(sw.Hovered()), 120*time.Millisecond)
	press := sw.Animate("press", boolF(sw.Pressed()), 90*time.Millisecond)
	off := t.Border.Mix(t.Text, 0.15)
	sw.Background(off.Mix(onColor, pos).Mix(t.Text, 0.09*hover+0.07*press))
	sw.Draw(func(p *myui.Painter, r myui.Rect) {
		in := t.Space(0.5)
		d := r.H - 2*in
		// 按下时旋钮拉长；位置按拉长后的宽度算，两端都贴着轨道内缘。
		w := d + t.Space(1)*press
		knob := myui.Rect{X: r.X + in + pos*(r.W-2*in-w), Y: r.Y + in, W: w, H: d}
		if hover > 0 {
			// 光晕：和轨道等高的胶囊，罩在旋钮外面，不超出轨道。
			halo := myui.Rect{X: knob.X - in, Y: r.Y, W: knob.W + 2*in, H: r.H}
			p.Fill(halo, t.Text.Alpha(0.12*hover), halo.H/2)
		}
		p.Shadow(knob, d/2, 0, 1, 3, 0, myui.RGBA(0, 0, 0, 0.25))
		p.Fill(knob, myui.RGB(255, 255, 255), d/2)
	})
	return sw
}

// boolF 把 bool 换成动画的目标值：true 为 1，false 为 0。
func boolF(b bool) float32 {
	if b {
		return 1
	}
	return 0
}
