package ui

// 开关（kuraSwitch）的交互反馈测试：悬停、按下、键盘聚焦都有可见的变化。
// 关掉动画（ReduceMotion 让 Animate 直接到位），截图才稳定；颜色从真实渲染
// 出来的像素里读。

import (
	"image"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
	myui "github.com/egoist/mygo/ui"
)

// luma 是像素的亮度（0–255），只用来比较「更亮」「更暗」。
func luma(p [4]uint8) int { return (299*int(p[0]) + 587*int(p[1]) + 114*int(p[2])) / 1000 }

// switchTrack 读开关轨道的颜色：在轨道里离旋钮最远的一端取样（开时旋钮在右，
// 取左端；关时相反），避开旋钮和它周围的光晕。
func switchTrack(tt *myui.Tester, label string, on bool) [4]uint8 {
	r, ok := tt.Find(label)
	if !ok {
		return [4]uint8{}
	}
	x := r.X + 4
	if !on {
		x = r.X + r.W - 4
	}
	c := tt.Image().RGBAAt(int(x), int(r.Y+r.H/2))
	return [4]uint8{c.R, c.G, c.B, c.A}
}

// snapshot 复制 tt 最后一帧的像素：Image 返回的缓冲会被下一帧复用。
func snapshot(tt *myui.Tester) *image.RGBA {
	img := tt.Image()
	out := image.NewRGBA(img.Bounds())
	copy(out.Pix, img.Pix)
	return out
}

// TestSwitchHoverPressFeedback 验证开关悬停时轨道向正文色靠一点并变成手形
// 指针，按下时靠得更多；移开后恢复，且按下后移到外面松开不会误触切换。开与关
// 两种状态、深色与浅色皮肤都一样。
func TestSwitchHoverPressFeedback(t *testing.T) {
	for _, dark := range []bool{true, false} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		a, cfg := newTestApp(t)
		seedDescribed(t, cfg, "alpha", "开着", nil)
		seedDescribed(t, cfg, "beta", "关着", nil)
		if _, err := cfg.Disable("beta"); err != nil {
			t.Fatal(err)
		}
		tt := showApp(a, 1280, 800)
		tt.SetPreferences(myui.Preferences{ReduceMotion: true})
		tt.SetDark(dark)
		refresh(a, tt)

		for _, c := range []struct {
			label string
			on    bool
		}{{"alpha 开关", true}, {"beta 开关", false}} {
			t.Run(mode+"/"+c.label, func(t *testing.T) {
				// 向正文色靠：深色里变亮，浅色里变暗。
				sign := 1
				if !dark {
					sign = -1
				}
				tt.Move(5, 5)
				refresh(a, tt)
				rest := luma(switchTrack(tt, c.label, c.on))
				if tt.Cursor() == myui.CursorPointer {
					t.Error("指针不在开关上时不应是手形")
				}

				r, _ := tt.Find(c.label)
				cx, cy := r.X+r.W/2, r.Y+r.H/2
				tt.Move(cx, cy)
				refresh(a, tt)
				hover := luma(switchTrack(tt, c.label, c.on))
				if tt.Cursor() != myui.CursorPointer {
					t.Errorf("悬停在开关上时指针应是手形，得到 %v", tt.Cursor())
				}
				if sign*(hover-rest) < 4 {
					t.Errorf("悬停时轨道应向正文色靠：静止亮度 %d，悬停亮度 %d", rest, hover)
				}

				tt.Press(cx, cy)
				refresh(a, tt)
				press := luma(switchTrack(tt, c.label, c.on))
				if sign*(press-hover) < 2 {
					t.Errorf("按下时轨道应比悬停时靠得更多：悬停 %d，按下 %d", hover, press)
				}

				// 按住后移到外面松开：不算点击，开关状态不变，反馈消失。
				tt.Move(5, 5)
				tt.Release(5, 5)
				refresh(a, tt)
				if got := luma(switchTrack(tt, c.label, c.on)); got != rest {
					t.Errorf("移开后轨道应恢复：静止 %d，现在 %d", rest, got)
				}
				name := c.label[:len(c.label)-len(" 开关")]
				if s := skillByName(t, a, name); (s.State == store.StateEnabled) != c.on {
					t.Errorf("在开关外松开不应切换 %s，状态 %v", name, s.State)
				}
			})
		}
	}
}

// TestSwitchKeyboardFocusRing 验证用键盘 Tab 到开关上时，轨道外面出现聚焦环。
// Focused 对祖先也为真（网格容器先拿到焦点），所以用两张卡片区分：只有 alpha
// 的开关自己拿到焦点时，alpha 为真而 beta 为假。
func TestSwitchKeyboardFocusRing(t *testing.T) {
	a, cfg := newTestApp(t)
	seedDescribed(t, cfg, "alpha", "开着", nil)
	seedDescribed(t, cfg, "beta", "也开着", nil)
	tt := showApp(a, 1280, 800)
	tt.SetPreferences(myui.Preferences{ReduceMotion: true})
	tt.Move(5, 5)
	refresh(a, tt)

	const label = "alpha 开关"
	before := snapshot(tt)
	for i := 0; i < 40 && !(tt.Focused(label) && !tt.Focused("beta 开关")); i++ {
		tt.Key(0, myui.KeyTab)
	}
	if !tt.Focused(label) || tt.Focused("beta 开关") {
		t.Fatalf("Tab 应能走到 %q 上", label)
	}
	after := tt.Image()

	// 聚焦环画在轨道外侧几个像素内：比较那一圈与聚焦前的差异。
	r, _ := tt.Find(label)
	outer := image.Rect(int(r.X)-4, int(r.Y)-4, int(r.X+r.W)+4, int(r.Y+r.H)+4)
	inner := image.Rect(int(r.X), int(r.Y), int(r.X+r.W), int(r.Y+r.H))
	changed := 0
	for y := outer.Min.Y; y < outer.Max.Y; y++ {
		for x := outer.Min.X; x < outer.Max.X; x++ {
			if image.Pt(x, y).In(inner) {
				continue
			}
			if after.RGBAAt(x, y) != before.RGBAAt(x, y) {
				changed++
			}
		}
	}
	if changed < 100 {
		t.Errorf("键盘聚焦后开关外侧应出现聚焦环，只有 %d 个像素变了", changed)
	}
}
