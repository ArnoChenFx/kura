package ui

// 「后台查到新版本」的小蓝点的像素级测试（同 switch_test.go / cardchrome_test.go
// 的做法：从 tt.Image() 读真实渲染出来的像素）。
//
// 边界：这里绝不挂更新插件、绝不联网——SetUpdateFound 只改一个布尔值，检查钩子
// 注入成计数用的假实现。

import (
	"testing"

	"github.com/ArnoChenFx/kura/locales"
	myui "github.com/egoist/mygo/ui"
)

// dotRadius 是小圆点的半径：notifyDot 用 2 个间距单位作直径（默认间距 4，即 8px）。
const dotRadius = 4

// dotCenter 返回左栏「检查更新」按钮右上角小圆点的中心（圆点整个贴在按钮的
// 内边距盒右上角，圆心因此在距右边缘、上边缘各一个半径处）。
func dotCenter(tt *myui.Tester, label string) (x, y int, ok bool) {
	r, found := tt.Find(label)
	if !found {
		return 0, 0, false
	}
	return int(r.X + r.W - dotRadius), int(r.Y + dotRadius), true
}

// dotIsBlue 报告按钮右上角有没有那颗小蓝点。
func dotIsBlue(tt *myui.Tester, pal palette) bool {
	x, y, ok := dotCenter(tt, locales.UiActionCheckUpdate())
	if !ok {
		return false
	}
	return near(tt.Image().RGBAAt(x, y), pal.notify, 6)
}

// TestUpdateDotAppearsAndClears 验证小蓝点：后台查到新版本（SetUpdateFound(true)）
// 时「检查更新」按钮右上角出现调色板里的蓝点，后台说没有了（SetUpdateFound(false)）
// 就消失；点按钮本身也立刻清掉它。浅色与深色皮肤都跑。
func TestUpdateDotAppearsAndClears(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			a, _ := newTestApp(t)
			clicks := 0
			a.SetCheckUpdates(func() { clicks++ })
			tt := showApp(a, 1280, 800)
			tt.SetDark(dark)
			refresh(a, tt)

			// 没有新版本：没有蓝点。
			if dotIsBlue(tt, a.pal) {
				t.Fatal("还没发现新版本时不应有蓝点")
			}

			// 后台发布「发现了新版本」：经 a.publish 回主线程后出现蓝点。
			a.SetUpdateFound(true)
			refresh(a, tt)
			if !a.updateFound {
				t.Fatal("SetUpdateFound(true) 后 updateFound 应为真")
			}
			if !dotIsBlue(tt, a.pal) {
				t.Errorf("发现新版本后按钮右上角应有 %v 的蓝点", a.pal.notify)
			}

			// 后台说没有了（例如新一轮检查发现已是最新）：蓝点消失。
			a.SetUpdateFound(false)
			refresh(a, tt)
			if a.updateFound {
				t.Fatal("SetUpdateFound(false) 后 updateFound 应为假")
			}
			if dotIsBlue(tt, a.pal) {
				t.Error("已是最新时蓝点应消失")
			}

			// 有蓝点时点按钮：蓝点立刻清掉，钩子仍被调用一次。
			a.SetUpdateFound(true)
			refresh(a, tt)
			click(t, a, tt, locales.UiActionCheckUpdate())
			if a.updateFound {
				t.Error("点「检查更新」后蓝点应被清掉")
			}
			if clicks != 1 {
				t.Errorf("点「检查更新」应经钩子打开更新窗口一次，得到 %d", clicks)
			}
			tt.Frame()
			if dotIsBlue(tt, a.pal) {
				t.Error("点过之后蓝点不应再出现")
			}
		})
	}
}

// TestUpdateDotKeepsButtonLayout 验证小蓝点不改变按钮的位置与尺寸：加上蓝点
// 前后，两个底部按钮的矩形逐像素一致；蓝点本身也不吃点击（在蓝点上按下松开
// 仍然触发按钮）。
func TestUpdateDotKeepsButtonLayout(t *testing.T) {
	a, _ := newTestApp(t)
	clicks := 0
	a.SetCheckUpdates(func() { clicks++ })
	tt := showApp(a, 1280, 800)

	// 蓝点是绝对定位的子元素，不参与布局：加上它前后矩形应完全一致。
	before := map[string][4]float32{}
	for _, label := range []string{locales.UiActionCheckUpdate(), locales.UiSettingsTitle()} {
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("找不到按钮 %q", label)
		}
		before[label] = [4]float32{r.X, r.Y, r.W, r.H}
	}

	a.SetUpdateFound(true)
	refresh(a, tt)
	for label, want := range before {
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("加上蓝点后找不到按钮 %q", label)
		}
		if got := [4]float32{r.X, r.Y, r.W, r.H}; got != want {
			t.Errorf("%q 的矩形被蓝点改变了：%v → %v", label, want, got)
		}
	}

	// 蓝点压在按钮上，点它必须落到按钮上——蓝点是个普通的非交互 Box（没有 Label、
	// 不是按钮），本来就不会成为指针的目标；notifyDot 另外加的 PassThrough 是给将来
	// 万一给它加了 Label 之类的东西留的保险。这条断言守的是「按钮仍能被点到」，
	// 它本身分辨不出有没有 PassThrough（去掉 PassThrough 这条也会过）。
	x, y, ok := dotCenter(tt, locales.UiActionCheckUpdate())
	if !ok {
		t.Fatal("找不到「检查更新」按钮")
	}
	tt.ClickAt(float32(x), float32(y))
	refresh(a, tt)
	if clicks != 1 {
		t.Errorf("点在蓝点上应触发按钮，钩子调用次数 = %d，想要 1", clicks)
	}
}

// TestRailUpdateTipFollowsUpdateFound 验证按钮的悬停提示随「有没有新版本」
// 二选一：平时是「检查 kura 应用更新」，发现新版本后换成「有一条新版本可用」。
// 提示是原生的、不出现在截图里，所以直接测选提示的那个函数（生产代码里就是它）。
func TestRailUpdateTipFollowsUpdateFound(t *testing.T) {
	if got := railUpdateTip(false); got != locales.UiRailCheckUpdateTip() {
		t.Errorf("平时应用「%s」，得到 %q", locales.UiRailCheckUpdateTip(), got)
	}
	if got := railUpdateTip(true); got != locales.UiRailUpdateAvailableTip() {
		t.Errorf("发现新版本后应用「%s」，得到 %q", locales.UiRailUpdateAvailableTip(), got)
	}
	if railUpdateTip(false) == railUpdateTip(true) {
		t.Error("两种状态的提示应不同")
	}
}
