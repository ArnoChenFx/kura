package ui

import (
	"fmt"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
	myui "github.com/egoist/mygo/ui"
)

// 线必须恰好位于两块背景之间；透明命中区不能把浅色工具区涂成画布色，
// 也不能在详情正文和底部动作区旁留出另一条色带。
func TestSplittersFollowPanelBackgroundEdges(t *testing.T) {
	for _, mode := range []string{store.ThemeLight, store.ThemeDark} {
		t.Run(mode, func(t *testing.T) {
			a, cfg := newTestApp(t)
			a.themeMode = mode
			seedSkill(t, cfg, "alpha", nil)
			tt := showApp(a, 1280, 800)
			tt.SetPreferences(myui.Preferences{ReduceMotion: true})
			click(t, a, tt, "alpha")
			tt.Move(500, 600)
			refresh(a, tt)

			for _, sample := range []struct {
				name          string
				y             int
				main, details myui.Color
			}{
				{"header", 55, a.pal.header, a.pal.panel},
				{"content", 500, a.pal.canvas, a.pal.panel},
				{"footer", 785, a.pal.canvas, a.pal.card},
			} {
				t.Run(sample.name, func(t *testing.T) {
					img := tt.Image()
					assertPixel := func(x int, want myui.Color) {
						t.Helper()
						if got := img.RGBAAt(x, sample.y); !near(got, want, 1) {
							t.Errorf("边界像素 (%d,%d) = %v，想要 %v", x, sample.y, got, want)
						}
					}
					// 两侧都检查紧邻线的像素与外侧像素，不能以宽色带冒充细线。
					left := int(a.rootsEff)
					for _, dx := range []int{-4, -1} {
						assertPixel(left+dx, a.pal.sidebar)
					}
					assertPixel(left, a.pal.line)
					for _, dx := range []int{1, 4} {
						assertPixel(left+dx, sample.main)
					}
					right := 1280 - int(a.detailEff) - 1
					for _, dx := range []int{-4, -1} {
						assertPixel(right+dx, sample.main)
					}
					assertPixel(right, a.pal.line)
					for _, dx := range []int{1, 4} {
						assertPixel(right+dx, sample.details)
					}
				})
			}
		})
	}
}

// 命中层必须覆盖细线两侧；若将它放入细线占位的子树，后绘制的面板会遮住
// 右半。边界外的指针则不能误拖侧栏。
func TestSplitterKeepsEightDIPHitAreaOnBothSides(t *testing.T) {
	for _, side := range []string{"roots", "detail"} {
		for _, offset := range []float32{-4.25, -3.75, 3.75, 4.25} {
			t.Run(fmt.Sprintf("%s/%g", side, offset), func(t *testing.T) {
				a, cfg := newTestApp(t)
				seedSkill(t, cfg, "alpha", nil)
				tt := showApp(a, 1280, 800)
				tt.SetPreferences(myui.Preferences{ReduceMotion: true})
				click(t, a, tt, "alpha")
				center, before, direction := a.rootsEff+dividerWidth/2.0, a.rootsW, float32(1)
				if side == "detail" {
					center, before, direction = 1280-a.detailEff-dividerWidth/2.0, a.detailW, -1
				}
				inside := offset > -gripHit/2.0 && offset < gripHit/2.0
				tt.Move(center+offset, 550)
				tt.Frame()
				if got := tt.Cursor() == myui.CursorResizeEW; got != inside {
					t.Fatalf("8DIP 命中范围错误：offset=%g resizeCursor=%v", offset, got)
				}
				dragH(a, tt, center+offset, 550, 24)
				after := a.rootsW
				if side == "detail" {
					after = a.detailW
				}
				want := before
				if inside {
					want += direction * 24
				}
				if after != want {
					t.Errorf("拖动结果错误：before=%g after=%g want=%g", before, after, want)
				}
			})
		}
	}
}

// 透明命中层不能吞掉紧邻主栏右边的滚动条；拖滚动条只滚列表，不改栏宽。
func TestDetailSplitterLeavesMainScrollbarDraggable(t *testing.T) {
	a, cfg := newTestApp(t)
	for i := range 80 {
		seedSkill(t, cfg, fmt.Sprintf("scroll-%02d", i), nil)
	}
	a.viewMode = store.ViewList
	tt := showApp(a, 1280, 800)
	tt.SetPreferences(myui.Preferences{ReduceMotion: true})
	click(t, a, tt, "scroll-00")
	first, ok := tt.Find("scroll-00")
	if !ok {
		t.Fatal("滚动前应有首行")
	}
	rootBefore, detailBefore := a.rootsW, a.detailW
	x, y := 1280-a.detailEff-dividerWidth-6, first.Y+3
	tt.Press(x, y)
	tt.Frame()
	tt.Move(x, y+200)
	tt.Frame()
	tt.Release(x, y+200)
	refresh(a, tt)
	start, _ := a.list.Visible()
	if start <= 0 {
		t.Fatalf("拖动主栏滚动条应离开首行，visibleStart=%d", start)
	}
	if a.rootsW != rootBefore || a.detailW != detailBefore {
		t.Fatalf("滚动条不能拖动侧栏：roots=%g detail=%g", a.rootsW, a.detailW)
	}
}
