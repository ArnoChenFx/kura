package ui

// 卡片外框的像素级测试：选中、悬停时卡片只有一圈边框，且与卡片本身对齐。
//
// 起因：GridView 给每个单元格也画一层选中框与悬停底（圆角 8、边 2px），叠在
// 卡片自己的边框（圆角 12、边 1px）外面，截图里就是「高亮框和卡片边框对不齐」。
// 修法见 newSkin 里的 cell 主题；这里用真实渲染出来的像素守住它，网格有焦点、
// 焦点在别处、鼠标悬停三种情形，浅色与深色各一遍。

import (
	"image"
	"image/color"
	"testing"

	myui "github.com/egoist/mygo/ui"
)

// near 报告 got 与 want 每个通道相差都不超过 tol。
func near(got color.RGBA, want myui.Color, tol int) bool {
	d := func(a uint8, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	return d(got.R, want.R) <= tol && d(got.G, want.G) <= tol && d(got.B, want.B) <= tol
}

// edgeGroups 在 region 里找颜色为 col 的长线：长横线所在的行、长竖线所在的列，
// 相邻的行（列）归为一组，返回每组的 [首, 末]。一圈 1px 的矩形边框恰好是
// 两组行（上下边）与两组列（左右边），每组宽 1；叠了第二圈就会出现更粗的组。
func edgeGroups(img *image.RGBA, region image.Rectangle, col myui.Color) (rows, cols [][2]int) {
	const minRun = 60
	longest := func(from, to int, at func(i int) bool) int {
		best, run := 0, 0
		for i := from; i < to; i++ {
			if at(i) {
				run++
				best = max(best, run)
			} else {
				run = 0
			}
		}
		return best
	}
	add := func(groups [][2]int, v int) [][2]int {
		if n := len(groups); n > 0 && groups[n-1][1] == v-1 {
			groups[n-1][1] = v
			return groups
		}
		return append(groups, [2]int{v, v})
	}
	for y := region.Min.Y; y < region.Max.Y; y++ {
		if longest(region.Min.X, region.Max.X, func(x int) bool { return near(img.RGBAAt(x, y), col, 2) }) >= minRun {
			rows = add(rows, y)
		}
	}
	for x := region.Min.X; x < region.Max.X; x++ {
		if longest(region.Min.Y, region.Max.Y, func(y int) bool { return near(img.RGBAAt(x, y), col, 2) }) >= minRun {
			cols = add(cols, x)
		}
	}
	return rows, cols
}

// checkCardRing 检查截图里唯一一张卡片的外框：恰好一圈 1px 的 border 色边，
// 紧贴它外侧三个像素都是底色（没有第二圈、没有单元格的悬停底），内侧是 bg。
func checkCardRing(t *testing.T, tt *myui.Tester, sk *skin, w int, bg, border myui.Color, dark bool) {
	t.Helper()
	img := tt.Image()
	region := image.Rect(rootsWidth+2, 118, w-detailWidth-2, 770)
	rows, cols := edgeGroups(img, region, border)
	one := func(g [][2]int) bool { return len(g) == 2 && g[0][0] == g[0][1] && g[1][0] == g[1][1] }
	if !one(rows) || !one(cols) {
		t.Fatalf("卡片应恰好有一圈 1px 的边框（横线行 %v，竖线列 %v 各应是两组、每组一行/一列）", rows, cols)
	}
	top, bottom, left, right := rows[0][0], rows[1][0], cols[0][0], cols[1][0]
	if bottom-top < cardHeight-8 || bottom-top > cardHeight {
		t.Errorf("边框的高度 %d 应接近卡片高度 %d：圈出来的不是卡片", bottom-top+1, cardHeight)
	}
	midX, midY := (left+right)/2, (top+bottom)/2

	// 浅色皮肤的卡片有一层很淡的投影，紧贴边框处最多比底色暗 7 左右。
	tol := 3
	if !dark {
		tol = 9
	}
	for d := 1; d <= 3; d++ {
		for _, p := range []image.Point{{left - d, midY}, {right + d, midY}, {midX, top - d}, {midX, bottom + d}} {
			if got := img.RGBAAt(p.X, p.Y); !near(got, sk.pal.canvas, tol) {
				t.Errorf("边框外侧 %dpx 处 %v 的颜色 %v 不是底色 %v：卡片外多画了一层", d, p, got, sk.pal.canvas)
			}
		}
	}
	for _, p := range []image.Point{{left + 2, midY}, {right - 2, midY}, {midX, top + 2}, {midX, bottom - 2}} {
		if got := img.RGBAAt(p.X, p.Y); !near(got, bg, 2) {
			t.Errorf("边框内侧 %v 的颜色 %v 不是卡片的底色 %v", p, got, bg)
		}
	}
}

// TestCardChromeIsOneAlignedRing 验证卡片在各种交互态下只有自己画的那一圈
// 边框：平时、悬停、选中（网格有焦点）、选中（焦点在别处）。
func TestCardChromeIsOneAlignedRing(t *testing.T) {
	const w, h = 1280, 800
	for _, dark := range []bool{true, false} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		cases := []struct {
			name string
			// act 把界面带到该状态；返回卡片的底色与边框色（取自皮肤）。
			act func(t *testing.T, a *App, tt *myui.Tester) (bg, border myui.Color)
		}{
			{"idle", func(t *testing.T, a *App, tt *myui.Tester) (myui.Color, myui.Color) {
				return a.pal.card, a.pal.line
			}},
			{"hover", func(t *testing.T, a *App, tt *myui.Tester) (myui.Color, myui.Color) {
				r, _ := tt.Find("alpha")
				tt.Move(r.X+r.W/2, r.Y+r.H+30)
				refresh(a, tt)
				return a.pal.cardHover, a.pal.lineHover
			}},
			{"selected-grid-focused", func(t *testing.T, a *App, tt *myui.Tester) (myui.Color, myui.Color) {
				click(t, a, tt, "alpha")
				return a.pal.cardSel, a.skin.theme.Accent
			}},
			{"selected-pointer-away", func(t *testing.T, a *App, tt *myui.Tester) (myui.Color, myui.Color) {
				click(t, a, tt, "alpha")
				tt.Move(700, 700)
				refresh(a, tt)
				return a.pal.cardSel, a.skin.theme.Accent
			}},
			{"selected-focus-elsewhere", func(t *testing.T, a *App, tt *myui.Tester) (myui.Color, myui.Color) {
				click(t, a, tt, "alpha")
				click(t, a, tt, "刷新")
				tt.Move(700, 700)
				refresh(a, tt)
				return a.pal.cardSel, a.skin.theme.Accent
			}},
		}
		for _, c := range cases {
			t.Run(mode+"/"+c.name, func(t *testing.T) {
				a, cfg := newTestApp(t)
				seedDescribed(t, cfg, "alpha", "唯一的一张卡片", nil)
				tt := showApp(a, w, h)
				tt.SetDark(dark)
				refresh(a, tt)
				bg, border := c.act(t, a, tt)
				if c.name != "idle" && c.name != "hover" && a.selName != "alpha" {
					t.Fatalf("selName = %q，想要 alpha", a.selName)
				}
				checkCardRing(t, tt, a.skin, w, bg, border, dark)
			})
		}
	}
}
