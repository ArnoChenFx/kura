package ui

// 设置弹窗里「自动检查更新」开关的界面测试。
//
// 偏好的真值在 ~/.kura/config.json 的 autoCheckUpdates（经 store.Library 读写）；后台检查的
// 运行时钩子（生产实现是 selfupdate.Background.SetAutomaticChecks）这里注入假实现：绝不挂
// 插件、绝不联网。开关的「开/关」用真实渲染出来的像素判断（旋钮在哪一侧），与 switch_test.go
// 同一套做法。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/ArnoChenFx/kura/locales"
	myui "github.com/egoist/mygo/ui"
)

// fakeAutoChecks 是后台检查钩子的假实现：记下每一次收到的通知。
type fakeAutoChecks struct {
	writes []bool
}

// install 把钩子接到 a 上（同 main.go 的 SetAutoChecks 调用点）。
func (f *fakeAutoChecks) install(a *App) *fakeAutoChecks {
	a.SetAutoChecks(func(on bool) { f.writes = append(f.writes, on) })
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

// persistedAutoCheck 读回 config.json 里的偏好（重新加载，不看内存副本）。
func persistedAutoCheck(t *testing.T, cfg *store.Config) bool {
	t.Helper()
	lib, err := store.LoadLibrary(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return lib.AutoCheck
}

// TestSettingsAutoCheckSwitch 验证设置弹窗里的「自动检查更新」开关：
//
//   - 弹窗里有这一项；
//   - 没有后台钩子时，点击照常改开关并写进 config.json（偏好的真值在 config.json，
//     与后台是否接线无关）；
//   - 点一下 → 钩子收到 false、a.autoCheck 变假、config.json 里是关、界面重绘后是关；
//   - 再点回来 → true，且通知次数与点击次数一致。
func TestSettingsAutoCheckSwitch(t *testing.T) {
	t.Run("no-hook", func(t *testing.T) {
		a, cfg := newTestApp(t)
		tt := showApp(a, 1280, 800)
		click(t, a, tt, "设置")
		if !detHasText(tt, "自动检查更新") {
			t.Fatalf("设置弹窗应有「自动检查更新」一项，文本 %v", tt.Texts())
		}
		if !a.autoCheck {
			t.Fatal("没有配置文件时默认应勾上")
		}
		click(t, a, tt, "自动检查更新开关")
		if a.autoCheck {
			t.Error("点击后 a.autoCheck 应为假")
		}
		if persistedAutoCheck(t, cfg) {
			t.Error("点击后 config.json 里应记下关")
		}
	})

	t.Run("toggle", func(t *testing.T) {
		for _, dark := range []bool{false, true} {
			a, cfg := newTestApp(t)
			f := (&fakeAutoChecks{}).install(a)
			tt := showApp(a, 1280, 800)
			// 关掉动画，截图才稳定（同 switch_test.go）。
			tt.SetPreferences(myui.Preferences{ReduceMotion: true})
			tt.SetDark(dark)
			refresh(a, tt)
			click(t, a, tt, "设置")

			if !switchIsOn(tt, "自动检查更新开关") {
				t.Errorf("dark=%v：开关应显示为开", dark)
			}

			// 关掉：钩子收到 false，config.json 里是关，界面重绘后显示为关。
			click(t, a, tt, "自动检查更新开关")
			a.settle()
			if len(f.writes) != 1 || f.writes[0] != false {
				t.Fatalf("关掉开关应通知一次 false，得到 %v", f.writes)
			}
			if a.autoCheck || persistedAutoCheck(t, cfg) {
				t.Error("关掉后界面与 config.json 都应为关")
			}
			tt.Frame()
			if switchIsOn(tt, "自动检查更新开关") {
				t.Errorf("dark=%v：关掉后开关应显示为关", dark)
			}

			// 再点回来。
			click(t, a, tt, "自动检查更新开关")
			a.settle()
			if len(f.writes) != 2 || f.writes[1] != true {
				t.Fatalf("再点回来应通知 true，得到 %v", f.writes)
			}
			if !a.autoCheck || !persistedAutoCheck(t, cfg) {
				t.Error("再点回来后界面与 config.json 都应为开")
			}
			tt.Frame()
			if !switchIsOn(tt, "自动检查更新开关") {
				t.Errorf("dark=%v：再点回来后开关应显示为开", dark)
			}
		}
	})
}

// TestAutoCheckStartsFromConfig 验证「自动检查更新」的初始值取自 config.json：存的是关，
// 启动后开关就是关；打开设置弹窗不改动它。
func TestAutoCheckStartsFromConfig(t *testing.T) {
	root := t.TempDir()
	cfg, err := store.NewConfig(filepath.Join(root, "skills"), filepath.Join(root, ".kura"))
	if err != nil {
		t.Fatal(err)
	}
	lib, err := store.LoadLibrary(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.SetAutoCheck(false); err != nil {
		t.Fatal(err)
	}

	a := newAppFor(cfg)
	if a.autoCheck {
		t.Fatal("config.json 里是关，启动后应为关")
	}
	a.OpenSettings()
	if a.autoCheck {
		t.Error("打开设置弹窗不应改动偏好")
	}
}

// TestSetAutoCheckAsyncIsIdempotent 验证 SetAutoCheckAsync 幂等：值没变时不排后台任务；
// 连续切换时只有最新一次真的写盘与通知（用队列模拟异步，同 viewmode_test.go 的
// TestSetViewAsync），旧任务的序号召回时直接放弃。
func TestSetAutoCheckAsyncIsIdempotent(t *testing.T) {
	a, cfg := newTestApp(t)
	f := (&fakeAutoChecks{}).install(a)
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
		t.Errorf("连续切换只应通知最新一次（false），得到 %v", f.writes)
	}
	if persistedAutoCheck(t, cfg) {
		t.Error("最新的偏好为关，config.json 里应是关")
	}
	if a.autoCheck {
		t.Error("最新一次是关，a.autoCheck 应为假")
	}
}

// TestAutoCheckSaveFailureIsReported 验证写 config.json 失败时：开关照常改（本次运行里生效），
// 状态行报告保存失败，而不是静默地假装已经存好。config 的 Home 指向一个普通文件，写入必然失败。
func TestAutoCheckSaveFailureIsReported(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := store.NewConfig(filepath.Join(root, "skills"), blocker)
	if err != nil {
		t.Fatal(err)
	}
	a := newAppFor(cfg)
	f := (&fakeAutoChecks{}).install(a)

	a.SetAutoCheckAsync(false)
	a.settle()
	if !a.statusErr || !strings.HasPrefix(a.status, locales.UiStatusAutoCheckSaveFailed("")) {
		t.Errorf("保存失败应在状态行报告，得到 %q (err=%v)", a.status, a.statusErr)
	}
	if a.autoCheck {
		t.Error("保存失败不回滚开关：本次运行里仍然是关")
	}
	if len(f.writes) != 1 || f.writes[0] != false {
		t.Errorf("后台仍应收到本次的切换，得到 %v", f.writes)
	}
}

// TestSettingsModalGroupsUpdateBelowTranslation 验证设置弹窗的分组成员关系：「没填全」的
// 橙色提醒属于翻译那一组，必须排在「更新」分隔线**之前**；排在分隔线之后就会读成在说
// 「自动检查更新」（这正是评审抓到的那处错位）。
//
// 判据是两者的纵向顺序：翻译组的所有消息都应在「更新」标题之上。
func TestSettingsModalGroupsUpdateBelowTranslation(t *testing.T) {
	a, _ := newTestApp(t)
	a.settings.Provider = store.TranslationOpenAI
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
