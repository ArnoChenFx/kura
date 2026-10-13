package ui

// kura 的配色。取自参考截图的深色界面：近中性的深炭灰底（带一丝冷紫调）、
// 比底色浅一档的卡片加 1px 细边框、三级灰度的文字，强调色是低饱和的雾蓝
// 紫（periwinkle），只用在主按钮、选中边框与「有更新」这类小面积元素上；
// 开关「开」用薄荷绿，对应「已启用」的状态色。浅色皮肤沿用同一套色相。

import (
	"hash/fnv"
	"time"

	myui "github.com/egoist/mygo/ui"
)

// palette 是主题之外、kura 自己用的颜色。
type palette struct {
	sidebar myui.Color // 左栏底色
	canvas  myui.Color // 中栏与详情栏的底色
	card    myui.Color // 卡片、详情分组与搜索框的底色（比底色浅一档）
	line    myui.Color // 卡片与分隔线的描边
	shadow  myui.Color // 卡片投影（深色皮肤里为透明）
	sel     myui.Color // 左栏选中项的底色
	textMid myui.Color // 左栏未选中项的文字（介于正文与弱化色之间）
	toggle  myui.Color // 开关「开」的颜色
	// avatars 是头像色板：低饱和的暖色与青绿，刻意不含蓝紫，
	// 不和强调色抢戏。
	avatars []myui.Color
}

// skin 是某个外观（浅/深）下的全部颜色。
type skin struct {
	theme *myui.Theme
	pal   palette
}

// scheme 是浅色与深色两个皮肤。
type scheme struct {
	light, dark *skin
}

// apply 让本帧使用与系统外观一致的皮肤，返回对应的表面色。
func (s *scheme) apply(c *myui.Context) palette {
	sk := s.light
	if c.Theme().Dark {
		sk = s.dark
	}
	c.SetTheme(sk.theme)
	return sk.pal
}

// tone 是一个皮肤的颜色清单（十六进制），newSkin 把它换成主题与表面色。
type tone struct {
	accent, accentHover, accentPressed, accentText string
	toggle                                         string
	success, warning, danger                       string
	// bg 是弹窗等「浮起」的面板色，和卡片同档。
	bg, surface, surfaceHover, surfacePressed string
	border, text, muted, textMid              string
	sidebar, canvas, card, line, sel          string
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
	t.Radius = 8

	avatars := make([]myui.Color, len(k.avatars))
	for i, a := range k.avatars {
		avatars[i] = hx(a)
	}
	return &skin{theme: t, pal: palette{
		sidebar: hx(k.sidebar), canvas: hx(k.canvas), card: hx(k.card), line: hx(k.line),
		shadow: k.shadow, sel: hx(k.sel), textMid: hx(k.textMid), toggle: hx(k.toggle),
		avatars: avatars,
	}}
}

// kuraScheme 是应用的配色。
var kuraScheme = &scheme{
	light: newSkin(false, tone{
		accent: "#525fa3", accentHover: "#46528f", accentPressed: "#3c477c", accentText: "#ffffff",
		toggle:  "#36a874",
		success: "#2f9e6a", warning: "#b7791f", danger: "#d64550",
		bg: "#ffffff", surface: "#f0f0f4", surfaceHover: "#e7e7ed", surfacePressed: "#dddde5",
		border: "#dedee4", text: "#1a1a1e", muted: "#6b6b75", textMid: "#45454d",
		sidebar: "#f5f5f7", canvas: "#f5f5f7", card: "#ffffff", line: "#e2e2e8", sel: "#e9e9ef",
		shadow:  myui.RGBA(20, 20, 40, 0.05),
		avatars: []string{"#2a9d8f", "#c98a1b", "#d6455d", "#d9622b", "#5b6577", "#6f9a2a", "#c2418b", "#8a5a35"},
	}),
	dark: newSkin(true, tone{
		accent: "#818bbf", accentHover: "#939cca", accentPressed: "#727bae", accentText: "#1a1a1e",
		toggle:  "#62bf89",
		success: "#6fd99b", warning: "#e5b567", danger: "#e5707a",
		bg: "#232327", surface: "#232327", surfaceHover: "#2a2a2f", surfacePressed: "#303036",
		border: "#333339", text: "#ededf1", muted: "#8b8b94", textMid: "#b9b9c1",
		sidebar: "#1a1a1e", canvas: "#1a1a1e", card: "#232327", line: "#333339", sel: "#27272c",
		shadow:  myui.RGBA(0, 0, 0, 0),
		avatars: []string{"#6fcdbd", "#e5b567", "#e58f9b", "#e8a074", "#9ea8bd", "#b9d577", "#df93c4", "#cfa883"},
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
		Background(col.Alpha(0.14)).Center()
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

// kuraSwitch 是开关：与 MyGo 的 Switch 同尺寸、同动画与同样的白色圆钮，
// 只是「开」的颜色由 onColor 指定，不必与主题的强调色相同。
func kuraSwitch(c *myui.Context, on *bool, onColor myui.Color) myui.Element {
	t := c.Theme()
	sw := myui.SwitchBase(c, on).Size(t.Space(9), t.Space(5)).Radius(t.Space(2.5))
	target := float32(0)
	if *on {
		target = 1
	}
	pos := sw.Animate("knob", target, 140*time.Millisecond)
	off := t.Border.Mix(t.Text, 0.15)
	sw.Background(off.Mix(onColor, pos))
	sw.Draw(func(p *myui.Painter, r myui.Rect) {
		in := t.Space(0.5)
		d := r.H - 2*in
		knob := myui.Rect{X: r.X + in + pos*(r.W-r.H), Y: r.Y + in, W: d, H: d}
		p.Shadow(knob, d/2, 0, 1, 3, 0, myui.RGBA(0, 0, 0, 0.25))
		p.Fill(knob, myui.RGB(255, 255, 255), d/2)
	})
	return sw
}
