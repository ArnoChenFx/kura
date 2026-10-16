package ui

// 设置弹窗里「自动检查更新」开关的界面测试。
//
// 偏好的真值在更新器插件的 updater.json（生产经 selfupdate.AutomaticChecks /
// SetAutomaticChecks 读写），这里注入假钩子：绝不挂插件、绝不联网。开关的
// 「开/关」用真实渲染出来的像素判断（旋钮在哪一侧），与 switch_test.go 同一套
// 做法。

import (
	"testing"

	"github.com/ArnoChenFx/kura/locales"
	myui "github.com/egoist/mygo/ui"
)

// fakeAutoChecks 是「自动检查更新」偏好的假实现：get 返回当前值，set 记下每次
// 写入。它模拟插件的 updater.json——不碰任何磁盘或网络。
type fakeAutoChecks struct {
	on     bool
	writes []bool
}

// install 把钩子接到 a 上（同 main.go 的 SetAutoChecks 调用点）。
func (f *fakeAutoChecks) install(a *App) *fakeAutoChecks {
	a.SetAutoChecks(func() bool { return f.on }, func(on bool) {
		f.on = on
		f.writes = append(f.writes, on)
	})
	return f
}

// switchIsOn 读开关旋钮的位置：开时旋钮在右端，关时在左端（见 kuraSwitch）。
func switchIsOn(tt *myui.Tester, label string) bool {
	r, ok := tt.Find(label)
	if !ok {
		return false
	}
	// 旋钮直径是轨道内缘高度，两端各留 2px 内边距；取离两端各 10px 处，
	// 落在旋钮中心附近、又避开轨道里侧的圆角。
	onAt := tt.Image().RGBAAt(int(r.X+r.W-10), int(r.Y+r.H/2))
	offAt := tt.Image().RGBAAt(int(r.X+10), int(r.Y+r.H/2))
	return luma([4]uint8{onAt.R, onAt.G, onAt.B, onAt.A}) > luma([4]uint8{offAt.R, offAt.G, offAt.B, offAt.A})
}

// TestSettingsAutoCheckSwitch 验证设置弹窗里的「自动检查更新」开关：
//
//   - 弹窗里有这一项；
//   - 钩子为 nil 时点击不 panic（只在状态行报告「更新功能未就绪」）；
//   - 注入的 get 返回真时默认勾上；
//   - 点一下 → 假 set 收到 false、a.autoCheck 变假、界面重绘后是关；
//   - 再点回来 → true，且写盘次数与点击次数一致。
func TestSettingsAutoCheckSwitch(t *testing.T) {
	t.Run("no-hooks", func(t *testing.T) {
		a, _ := newTestApp(t)
		tt := showApp(a, 1280, 800)
		click(t, a, tt, "设置")
		if !detHasText(tt, "自动检查更新") {
			t.Fatalf("设置弹窗应有「自动检查更新」一项，文本 %v", tt.Texts())
		}
		// 默认勾上（与更新器插件的默认一致），此时还没接钩子。
		if !a.autoCheck {
			t.Fatal("默认应勾上")
		}
		// 钩子为 nil：点一下不 panic，只在状态行报告；没有地方可写，所以开关
		// 保持原样（不假装已经关掉）。
		click(t, a, tt, "自动检查更新开关")
		if !a.statusErr || !detHasText(tt, "更新功能未就绪") {
			t.Errorf("未接线时应在状态行报告更新功能未就绪，得到 %q (err=%v)", a.status, a.statusErr)
		}
		if !a.autoCheck {
			t.Error("没有地方可写时不应改开关的值")
		}
	})

	t.Run("toggle", func(t *testing.T) {
		for _, dark := range []bool{false, true} {
			a, _ := newTestApp(t)
			f := (&fakeAutoChecks{on: true}).install(a)
			tt := showApp(a, 1280, 800)
			// 关掉动画，截图才稳定（同 switch_test.go）。
			tt.SetPreferences(myui.Preferences{ReduceMotion: true})
			tt.SetDark(dark)
			refresh(a, tt)
			click(t, a, tt, "设置")

			if !a.autoCheck {
				t.Fatal("注入的 get 返回真时开关应默认勾上")
			}
			if !switchIsOn(tt, "自动检查更新开关") {
				t.Errorf("dark=%v：开关应显示为开", dark)
			}

			// 关掉：假 set 收到 false，界面重绘后显示为关。
			click(t, a, tt, "自动检查更新开关")
			a.settle()
			if len(f.writes) != 1 || f.writes[0] != false {
				t.Fatalf("关掉开关应写入一次 false，得到 %v", f.writes)
			}
			if a.autoCheck {
				t.Error("关掉后 a.autoCheck 应为假")
			}
			tt.Frame()
			if switchIsOn(tt, "自动检查更新开关") {
				t.Errorf("dark=%v：关掉后开关应显示为关", dark)
			}

			// 再点回来。
			click(t, a, tt, "自动检查更新开关")
			a.settle()
			if len(f.writes) != 2 || f.writes[1] != true {
				t.Fatalf("再点回来应写入 true，得到 %v", f.writes)
			}
			if !a.autoCheck {
				t.Error("再点回来后 a.autoCheck 应为真")
			}
			tt.Frame()
			if !switchIsOn(tt, "自动检查更新开关") {
				t.Errorf("dark=%v：再点回来后开关应显示为开", dark)
			}
		}
	})
}

// TestOpenSettingsRereadsAutoCheck 验证打开设置弹窗时按钩子现读一次「自动检查
// 更新」：用户可能在插件自己的更新窗口里关过它，也可能手改了 updater.json，
// 弹窗不该显示一个过时的内存副本。
func TestOpenSettingsRereadsAutoCheck(t *testing.T) {
	a, _ := newTestApp(t)
	f := (&fakeAutoChecks{on: true}).install(a)
	if !a.autoCheck {
		t.Fatal("注入 get 返回真后应初始化成真")
	}
	// 插件那边把它关掉了（假装用户改的）。
	f.on = false
	a.OpenSettings()
	if a.autoCheck {
		t.Error("打开设置弹窗应现读钩子，拿到插件里的关")
	}
	// 再打开一次，改成开：也要跟。
	f.on = true
	a.OpenSettings()
	if !a.autoCheck {
		t.Error("再打开应读到新值")
	}
}

// TestSetAutoCheckAsyncIsIdempotent 验证 SetAutoCheckAsync 幂等：值没变时
// 不排后台任务；连续切换时只有最新一次真的写（用队列模拟异步，同
// viewmode_test.go 的 TestSetViewAsync），旧任务的序号召回时直接放弃。
func TestSetAutoCheckAsyncIsIdempotent(t *testing.T) {
	a, _ := newTestApp(t)
	f := (&fakeAutoChecks{on: true}).install(a)
	var queue []func()
	a.spawn = func(fn func()) { queue = append(queue, fn) }

	a.SetAutoCheckAsync(true) // 与当前相同：不排任务
	if len(queue) != 0 {
		t.Fatalf("值没变不应排后台任务，得到 %d", len(queue))
	}

	a.SetAutoCheckAsync(false)
	a.SetAutoCheckAsync(true)
	a.SetAutoCheckAsync(false)
	if len(queue) != 3 {
		t.Fatalf("三次切换应排三个任务，得到 %d", len(queue))
	}
	for _, fn := range queue {
		fn()
	}
	if len(f.writes) != 1 || f.writes[0] != false {
		t.Errorf("连续切换只应写最新一次（false），得到 %v", f.writes)
	}
	if f.on {
		t.Error("最新的偏好应为关")
	}
	if a.autoCheck {
		t.Error("最新一次是关，a.autoCheck 应为假")
	}
}

// TestSettingsModalGroupsUpdateBelowTranslation 验证设置弹窗的分组成员关系：
// 「没填全」的橙色提醒属于翻译那一组，必须排在「更新」分隔线**之前**；排在
// 分隔线之后就会读成在说「自动检查更新」（这正是评审抓到的那处错位）。
//
// 判据是两者的纵向顺序：翻译组的所有消息都应在「更新」标题之上。
func TestSettingsModalGroupsUpdateBelowTranslation(t *testing.T) {
	a, _ := newTestApp(t)
	tt := showApp(a, 1280, 800)
	click(t, a, tt, locales.UiSettingsTitle())

	heading, ok := tt.Find(locales.UiSettingsUpdateHeading())
	if !ok {
		t.Fatalf("设置弹窗应有「更新」分组标题，文本 %v", tt.Texts())
	}
	// 翻译那一组的两条消息：密钥说明与「没填全」提醒。
	for _, msg := range []string{locales.UiSettingsSecretNote(), locales.UiSettingsIncomplete()} {
		r, found := tt.Find(msg)
		if !found {
			t.Fatalf("设置弹窗应有 %q，文本 %v", msg, tt.Texts())
		}
		if r.Y >= heading.Y {
			t.Errorf("翻译组的消息 %q 排在了「更新」标题之下（y=%v >= %v）：会被读成在说自动检查更新",
				msg, r.Y, heading.Y)
		}
	}
	// 「自动检查更新」这一项确实在「更新」标题之下。
	item, ok := tt.Find(locales.UiSettingsAutoCheckUpdate())
	if !ok {
		t.Fatalf("设置弹窗应有「自动检查更新」，文本 %v", tt.Texts())
	}
	if item.Y <= heading.Y {
		t.Errorf("「自动检查更新」应在「更新」标题之下（y=%v <= %v）", item.Y, heading.Y)
	}
}
