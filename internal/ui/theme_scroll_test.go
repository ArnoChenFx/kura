package ui

import (
	"fmt"
	"math"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/ArnoChenFx/kura/locales"
	myui "github.com/egoist/mygo/ui"
)

// 长列表滚动到中部后，切换主题只应改变工具栏留白，不应更换 Scroll 的
// 身份而回到顶部。用实际首行布局校准两种主题的位置差，不复制主题尺寸。
func TestThemeSwitchKeepsLongListScrollPosition(t *testing.T) {
	for _, initial := range []string{store.ThemeLight, store.ThemeDark} {
		t.Run(initial, func(t *testing.T) {
			a, cfg := newTestApp(t)
			var names []string
			for i := 0; i < 30; i++ {
				name := fmt.Sprintf("scroll-skill-%02d", i)
				names = append(names, name)
				seedDescribed(t, cfg, name, "A skill in the long list", nil)
			}
			a.viewMode, a.themeMode = store.ViewList, initial
			tt := showApp(a, 1280, 800)
			tt.SetPreferences(myui.Preferences{ReduceMotion: true})
			refresh(a, tt)

			switchTheme := func(mode string) {
				t.Helper()
				label := locales.UiThemeLight()
				if mode == store.ThemeDark {
					label = locales.UiThemeDark()
				}
				click(t, a, tt, label)
				if a.themeMode != mode {
					t.Fatalf("主题按钮未切换到 %s：得到 %s", mode, a.themeMode)
				}
			}
			firstY := map[string]float32{}
			for _, mode := range []string{store.ThemeLight, store.ThemeDark} {
				switchTheme(mode)
				first, ok := tt.Find(names[0])
				if !ok || first.H <= 0 {
					t.Fatalf("滚动前 %s 主题应显示首行", mode)
				}
				firstY[mode] = first.Y
			}
			switchTheme(initial)
			first, _ := tt.Find(names[0])
			tt.Scroll(first.X+first.W/2, first.Y+first.H/2, 0, 900)
			refresh(a, tt)
			if r, ok := tt.Find(names[0]); ok && r.H > 0 {
				t.Fatal("滚动后首行仍可见，未实际进入长列表中部")
			}

			// 选窗口中部的一行作锚点，避免顶部/底部的裁切影响几何断言。
			var anchor string
			var before myui.Rect
			for _, name := range names[1:] {
				if r, ok := tt.Find(name); ok && r.H > 0 && r.Y >= 360 && r.Y+r.H <= 560 {
					anchor, before = name, r
					break
				}
			}
			if anchor == "" {
				t.Fatal("滚动后没有可见的中部行")
			}
			other := store.ThemeDark
			if initial == store.ThemeDark {
				other = store.ThemeLight
			}
			for _, mode := range []string{other, initial} {
				switchTheme(mode)
				for frame := 0; frame < 2; frame++ {
					tt.Frame()
					after, ok := tt.Find(anchor)
					if !ok || after.H <= 0 {
						t.Fatalf("切到 %s 后第 %d 帧丢失滚动锚点 %q，列表可能回到了顶部", mode, frame, anchor)
					}
					wantY := before.Y + firstY[mode] - firstY[initial]
					if math.Abs(float64(after.Y-wantY)) > 0.5 {
						t.Fatalf("切到 %s 后列表滚动位置改变：锚点 %q Y=%g，按实际工具栏布局应为 %g", mode, anchor, after.Y, wantY)
					}
				}
			}
		})
	}
}
