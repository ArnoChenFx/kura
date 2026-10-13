package ui

// kura 的主题：在系统的浅色/深色主题上换一套更有层次的配色——
// 左栏、内容画布、卡片三种底色由浅到深分层，强调色用靛蓝，圆角放大。
// 主题在包初始化时构建一次，View 每帧按系统外观选用。

import (
	"hash/fnv"

	myui "github.com/egoist/mygo/ui"
)

// palette 是主题之外、kura 自己用的几种表面色。
type palette struct {
	sidebar myui.Color // 左栏底色
	canvas  myui.Color // 中栏画布底色（卡片落在它上面）
	card    myui.Color // 卡片、详情栏与搜索框的底色
	line    myui.Color // 卡片描边
	shadow  myui.Color // 卡片投影
}

var (
	lightTheme, darkTheme = kuraTheme(false), kuraTheme(true)
	lightPal              = palette{
		sidebar: myui.Hex("#f3f3f6"), canvas: myui.Hex("#f8f8fa"), card: myui.Hex("#ffffff"),
		line: myui.Hex("#e6e6eb"), shadow: myui.RGBA(24, 24, 40, 0.06),
	}
	darkPal = palette{
		sidebar: myui.Hex("#131316"), canvas: myui.Hex("#18181b"), card: myui.Hex("#212125"),
		line: myui.Hex("#2e2e34"), shadow: myui.RGBA(0, 0, 0, 0.35),
	}
)

// kuraTheme 基于 MyGo 的浅色/深色主题，替换强调色、边框与圆角。
func kuraTheme(dark bool) *myui.Theme {
	if dark {
		t := myui.DarkTheme()
		t.Background = myui.Hex("#1c1c20")
		t.Surface = myui.Hex("#26262b")
		t.SurfaceHover = myui.Hex("#2f2f35")
		t.SurfacePressed = myui.Hex("#38383f")
		t.Border = myui.Hex("#34343b")
		t.Accent = myui.Hex("#6366f1")
		t.AccentHover = myui.Hex("#818cf8")
		t.AccentPressed = myui.Hex("#4f46e5")
		t.Focus = myui.RGBA(129, 140, 248, 0.6)
		t.Selection = myui.RGBA(99, 102, 241, 0.4)
		t.Radius = 8
		return t
	}
	t := myui.LightTheme()
	t.Surface = myui.Hex("#f4f4f7")
	t.SurfaceHover = myui.Hex("#ebebf0")
	t.SurfacePressed = myui.Hex("#e1e1e8")
	t.Border = myui.Hex("#dedee5")
	t.TextMuted = myui.Hex("#6b6b76")
	t.Accent = myui.Hex("#4f46e5")
	t.AccentHover = myui.Hex("#4338ca")
	t.AccentPressed = myui.Hex("#3730a3")
	t.Focus = myui.RGBA(79, 70, 229, 0.5)
	t.Selection = myui.RGBA(79, 70, 229, 0.22)
	t.Radius = 8
	return t
}

// applyTheme 让本帧使用 kura 主题（跟随系统外观），返回对应的表面色。
func applyTheme(c *myui.Context) palette {
	if c.Theme().Dark {
		c.SetTheme(darkTheme)
		return darkPal
	}
	c.SetTheme(lightTheme)
	return lightPal
}

// avatarColors 是技能头像的色板：按名字哈希取色，同名技能颜色稳定。
var avatarColors = []myui.Color{
	myui.Hex("#4f46e5"), myui.Hex("#0284c7"), myui.Hex("#059669"), myui.Hex("#d97706"),
	myui.Hex("#e11d48"), myui.Hex("#7c3aed"), myui.Hex("#0d9488"), myui.Hex("#ea580c"),
}

// avatarColor 返回技能名对应的头像颜色。
func avatarColor(name string) myui.Color {
	h := fnv.New32a()
	h.Write([]byte(name))
	return avatarColors[h.Sum32()%uint32(len(avatarColors))]
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
func avatar(c *myui.Context, name string, size float32, dim bool) {
	t := c.Theme()
	col := avatarColor(name)
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
