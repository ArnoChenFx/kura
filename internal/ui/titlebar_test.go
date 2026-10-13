// 自绘标题栏与「检查更新」按钮的界面测试。
//
// 窗口在 main.go 里用 mygo.TitleBarHidden 隐藏系统标题栏：headless Tester
// 没有真实窗口，经 SetTitleBar 模拟窗口按钮占掉的区域（Windows 上是高
// TitleBarHeight、右侧三个按钮的宽度），验证自绘顶栏避开按钮、不把三栏
// 挤坏；「检查更新」经 checkUpdates 钩子注入假实现，绝不触发真实更新
// 检查（生产实现 selfupdate.CheckForUpdates 依赖已挂载的更新插件，测试
// 进程没有挂载，直接调用会 panic）。
package ui

import (
	"testing"

	myui "github.com/egoist/mygo/ui"
)

// TestTitlebarAvoidsWindowButtons 验证自绘顶栏：文字不被右侧窗口按钮
// 区域压住；顶栏高度不低于 40；三栏仍占满剩余高度、底部状态条在窗口内。
func TestTitlebarAvoidsWindowButtons(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "alpha", nil)
	tt := showApp(a, 1280, 800)
	// 模拟 Windows 的窗口按钮区：高 40（与 main.go 的 TitleBarHeight 一致），
	// 右侧三个按钮各 46 宽（ui.TitleBar 单位是 DIP）。
	tt.SetTitleBar(myui.TitleBar{Height: 40, Right: 138})
	refresh(a, tt)

	// 顶栏里的标识与名字都在顶栏这条带里。
	kura, ok := tt.Find("kura")
	if !ok {
		t.Fatalf("顶栏应显示 kura，文本 %v", tt.Texts())
	}
	if kura.Y+1 > 40 {
		t.Errorf("顶栏文字应落在顶部 40px 的条带里（y=%v，窗口按钮占 0..40）", kura.Y)
	}
	if kura.X+kura.W > 1280-138 {
		t.Errorf("顶栏文字不应伸进右侧窗口按钮区（右端 %v > %d）", kura.X+kura.W, 1280-138)
	}

	// 顶栏下方三栏照旧：左栏、中栏的头部与状态条都有正的位置，状态条
	// 没有被挤出窗口底部。
	rootItem, ok := tt.Find("001/skills")
	if !ok {
		// rootLabel 是路径最后两级，名字取决于临时目录；找不到就退而
		// 检查「技能目录」小节标题。
		if _, ok := tt.Find("技能目录"); !ok {
			t.Fatalf("左栏应仍在顶栏之下，文本 %v", tt.Texts())
		}
	} else if rootItem.Y < 40 {
		t.Errorf("左栏应从顶栏（40px）之下开始，得到 y=%v", rootItem.Y)
	}
	status, ok := tt.Find("共 1 个技能")
	if !ok {
		t.Fatalf("底部状态条应在，文本 %v", tt.Texts())
	}
	if status.Y+status.H > 800 {
		t.Errorf("状态条不应被挤出窗口底部（底部 %v > 800）", status.Y+status.H)
	}
}

// TestTitlebarLayoutWithoutWindowButtons 验证：窗口没有隐藏标题栏
// （Tester 默认 TitleBar 为零，等价全屏时按钮隐藏）时顶栏仍有兜底高度，
// 布局不塌、状态条不被挤出。
func TestTitlebarLayoutWithoutWindowButtons(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "alpha", nil)
	tt := showApp(a, 1280, 800)

	kura, ok := tt.Find("kura")
	if !ok {
		t.Fatalf("顶栏应显示 kura，文本 %v", tt.Texts())
	}
	if kura.H <= 0 {
		t.Errorf("顶栏应有正高度，得到 %v", kura.H)
	}
	status, ok := tt.Find("共 1 个技能")
	if !ok {
		t.Fatalf("底部状态条应在，文本 %v", tt.Texts())
	}
	if status.Y+status.H > 800 {
		t.Errorf("状态条不应被挤出窗口底部（底部 %v > 800）", status.Y+status.H)
	}
}

// TestCheckAppUpdateButtonCallsHook 验证「检查更新」按钮：注入假实现，
// 点击后假实现被调用、状态行给出反馈；没有钩子时点击只报告错误，绝不
// panic 或触发真实检查。
func TestCheckAppUpdateButtonCallsHook(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "alpha", nil)
	tt := showApp(a, 1280, 800)

	// 默认无钩子（NewApp 不接线）：点击只报告「未就绪」，不 panic。
	click(t, a, tt, "检查更新")
	if a.statusErr == false {
		t.Errorf("无钩子时点击应报告错误，状态行 = %q", a.status)
	}

	// 注入假实现再点：钩子被调用；状态行先给「正在检查 kura 更新…」，
	// 之后的反馈交给更新窗口（假实现不改状态行）。同步回放后忙碌标记
	// 已清除，按钮恢复可用。
	calls := 0
	a.checkUpdates = func() { calls++ }
	click(t, a, tt, "检查更新")
	if calls != 1 {
		t.Fatalf("假实现应被调用 1 次，得到 %d", calls)
	}
	if a.checkingUpdate {
		t.Error("检查完成后忙碌标记应清除（测试里同步回放）")
	}
	if a.status != "正在检查 kura 更新…" || a.statusErr {
		t.Errorf("点击后状态行应是「正在检查 kura 更新…」，得到 %q err=%v", a.status, a.statusErr)
	}
}
