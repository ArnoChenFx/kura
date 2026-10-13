package ui

// kura 的配色方案。一套方案 = 浅色与深色两个「皮肤」，每个皮肤包含 MyGo
// 主题（控件配色）、表面色（左栏/画布/卡片三档底色）与头像色板。
//
// 强调色（accent）只用在主按钮、选中态与品牌标识上；开关「开」的颜色
// （toggle）与「有更新」标签的颜色（signal）单独设定，所以石墨方案里主按钮
// 可以是近黑色，开关仍是醒目的绿色。

import (
	"hash/fnv"
	"time"

	myui "github.com/egoist/mygo/ui"
)

// palette 是主题之外、kura 自己用的颜色。
type palette struct {
	sidebar myui.Color // 左栏底色
	canvas  myui.Color // 中栏画布底色（卡片落在它上面）
	card    myui.Color // 卡片、详情栏与搜索框的底色
	line    myui.Color // 卡片描边
	shadow  myui.Color // 卡片投影
	toggle  myui.Color // 开关「开」的颜色
	signal  myui.Color // 「有更新」等提示标签的颜色
	// avatars 是头像色板：浅色皮肤用 600 档、深色皮肤用 400 档，
	// 避免蓝紫色，和各方案的强调色互不抢戏。
	avatars []myui.Color
}

// skin 是一套方案在某个外观（浅/深）下的全部颜色。
type skin struct {
	theme *myui.Theme
	pal   palette
}

// scheme 是一套配色方案。
type scheme struct {
	name        string
	light, dark *skin
}

// apply 让本帧使用方案里与系统外观一致的皮肤，返回对应的表面色。
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
	toggle, signal                                 string
	bg, surface, surfaceHover, surfacePressed      string
	border, text, muted                            string
	sidebar, canvas, card, line                    string
	shadow                                         myui.Color
	avatars                                        []string
}

// newSkin 以 MyGo 的浅色/深色主题为底，按 k 替换颜色并放大圆角。
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
	t.Focus = hx(k.accent).Alpha(0.5)
	t.Selection = hx(k.accent).Alpha(0.25)
	t.Radius = 8

	avatars := make([]myui.Color, len(k.avatars))
	for i, a := range k.avatars {
		avatars[i] = hx(a)
	}
	return &skin{theme: t, pal: palette{
		sidebar: hx(k.sidebar), canvas: hx(k.canvas), card: hx(k.card),
		line: hx(k.line), shadow: k.shadow,
		toggle: hx(k.toggle), signal: hx(k.signal), avatars: avatars,
	}}
}

// 头像色板：teal/amber/rose/orange/slate/lime/pink/brown 八种色相，刻意不含
// 蓝色与紫色，也只留一个绿色系（teal），不与绿色的开关混在一起。
var (
	avatarsLight = []string{"#0d9488", "#d97706", "#e11d48", "#ea580c", "#475569", "#65a30d", "#db2777", "#92400e"}
	avatarsDark  = []string{"#2dd4bf", "#fbbf24", "#fb7185", "#fb923c", "#94a3b8", "#a3e635", "#f472b6", "#d4a373"}
)

// 三套候选方案。
var (
	// schemeForest 森林绿：清爽的绿色强调，底色带一点点绿调的灰。
	schemeForest = &scheme{name: "forest",
		light: newSkin(false, tone{
			accent: "#15803d", accentHover: "#166534", accentPressed: "#14532d", accentText: "#ffffff",
			toggle: "#16a34a", signal: "#15803d",
			bg: "#ffffff", surface: "#f2f5f3", surfaceHover: "#e8eeea", surfacePressed: "#dde5e0",
			border: "#dae2dd", text: "#171c19", muted: "#5d6862",
			sidebar: "#eff3f0", canvas: "#f6f8f6", card: "#ffffff", line: "#e2e9e4",
			shadow: myui.RGBA(16, 40, 28, 0.06), avatars: avatarsLight,
		}),
		dark: newSkin(true, tone{
			accent: "#22c55e", accentHover: "#4ade80", accentPressed: "#16a34a", accentText: "#052e16",
			toggle: "#22c55e", signal: "#4ade80",
			bg: "#181c1a", surface: "#222825", surfaceHover: "#2b322e", surfacePressed: "#343c37",
			border: "#313a35", text: "#ecf1ee", muted: "#9aa7a0",
			sidebar: "#0f1311", canvas: "#141816", card: "#1b201d", line: "#2a312d",
			shadow: myui.RGBA(0, 0, 0, 0.4), avatars: avatarsDark,
		}),
	}
	// schemeClay 陶土橙：暖橙强调，底色是偏暖的奶白。
	schemeClay = &scheme{name: "clay",
		light: newSkin(false, tone{
			accent: "#c2410c", accentHover: "#9a3412", accentPressed: "#7c2d12", accentText: "#ffffff",
			toggle: "#ea580c", signal: "#c2410c",
			bg: "#ffffff", surface: "#f5f2ec", surfaceHover: "#ece7de", surfacePressed: "#e2dcd1",
			border: "#e0d9ce", text: "#201b16", muted: "#6f665b",
			sidebar: "#f4f0ea", canvas: "#faf8f5", card: "#ffffff", line: "#e8e2d9",
			shadow: myui.RGBA(60, 40, 20, 0.07), avatars: avatarsLight,
		}),
		dark: newSkin(true, tone{
			accent: "#f97316", accentHover: "#fb923c", accentPressed: "#ea580c", accentText: "#1f0d03",
			toggle: "#f97316", signal: "#fb923c",
			bg: "#1d1a17", surface: "#282420", surfaceHover: "#322d28", surfacePressed: "#3c362f",
			border: "#3a342d", text: "#f3eee8", muted: "#a39a8e",
			sidebar: "#12100e", canvas: "#171512", card: "#201d19", line: "#302b25",
			shadow: myui.RGBA(0, 0, 0, 0.4), avatars: avatarsDark,
		}),
	}
	// schemeGraphite 石墨：中性灰黑，只有「状态」带颜色（开关绿、标签、头像）。
	schemeGraphite = &scheme{name: "graphite",
		light: newSkin(false, tone{
			accent: "#27272a", accentHover: "#3f3f46", accentPressed: "#18181b", accentText: "#fafafa",
			toggle: "#16a34a", signal: "#15803d",
			bg: "#ffffff", surface: "#f5f5f5", surfaceHover: "#ebebeb", surfacePressed: "#e0e0e0",
			border: "#e0e0e0", text: "#171717", muted: "#737373",
			sidebar: "#f3f3f3", canvas: "#fafafa", card: "#ffffff", line: "#e8e8e8",
			shadow: myui.RGBA(0, 0, 0, 0.05), avatars: avatarsLight,
		}),
		dark: newSkin(true, tone{
			accent: "#e5e5e5", accentHover: "#fafafa", accentPressed: "#d4d4d4", accentText: "#171717",
			toggle: "#22c55e", signal: "#4ade80",
			bg: "#1a1a1a", surface: "#262626", surfaceHover: "#303030", surfacePressed: "#3a3a3a",
			border: "#383838", text: "#f5f5f5", muted: "#a3a3a3",
			sidebar: "#0f0f0f", canvas: "#141414", card: "#1c1c1c", line: "#2b2b2b",
			shadow: myui.RGBA(0, 0, 0, 0.4), avatars: avatarsDark,
		}),
	}

	// defaultScheme 是应用使用的方案。
	defaultScheme = schemeGraphite
)

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
