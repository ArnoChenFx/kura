package ui

// 外观开关的测试：跟随系统 / 浅色 / 深色。皮肤立刻换，偏好写进 config.json，
// 原生外观（标题栏、系统菜单）经 setSource 同步——测试里换成记录用的假实现，
// 绝不改真实系统的外观；config.json 在 t.TempDir() 里。

import (
	"os"
	"reflect"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
	myui "github.com/egoist/mygo/ui"
)

// recordSource 让 a 把外观同步记到返回的切片里。
func recordSource(a *App) *[]string {
	var synced []string
	a.setSource = func(mode string) { synced = append(synced, mode) }
	return &synced
}

// canvasIs 检查窗口里一块没有卡片的空白处，真实渲染出来的颜色是 want。
func canvasIs(t *testing.T, tt *myui.Tester, want myui.Color, why string) {
	t.Helper()
	if got := tt.Image().RGBAAt(1100, 720); !near(got, want, 2) {
		t.Errorf("%s：空白处渲染成 %v，想要 %v", why, got, want)
	}
}

// savedTheme 读回 config.json 里的外观偏好。
func savedTheme(t *testing.T, cfg *store.Config) string {
	t.Helper()
	lib, err := store.LoadLibrary(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return lib.Theme
}

func TestThemeIndex(t *testing.T) {
	for mode, want := range map[string]int{
		store.ThemeSystem: 0, store.ThemeLight: 1, store.ThemeDark: 2, "": 0, "sepia": 0,
	} {
		if got := themeIndex(mode); got != want {
			t.Errorf("themeIndex(%q) = %d，想要 %d", mode, got, want)
		}
	}
}

// TestThemeSwitchChangesSkillAndPersists 验证点外观开关：皮肤立刻换成所选的，
// 不随系统外观变；「跟随系统」又跟着系统走。每次切换原生外观只同步一次，偏好
// 写进 config.json，重复点同一项什么也不做；切换不影响当前选中的技能。
func TestThemeSwitchChangesSkillAndPersists(t *testing.T) {
	a, cfg := newTestApp(t)
	synced := recordSource(a)
	seedDescribed(t, cfg, "alpha", "第一个技能", nil)
	tt := showApp(a, 1280, 800)
	light, dark := kuraScheme.light.pal.canvas, kuraScheme.dark.pal.canvas

	// 起初跟随系统：系统浅色，就是浅色皮肤。
	tt.SetDark(false)
	refresh(a, tt)
	if a.themeMode != store.ThemeSystem {
		t.Fatalf("起初应跟随系统，得到 %q", a.themeMode)
	}
	canvasIs(t, tt, light, "跟随系统（系统浅色）")
	click(t, a, tt, "alpha")
	if a.selName != "alpha" {
		t.Fatalf("selName = %q，想要 alpha", a.selName)
	}

	// 选深色：系统仍是浅色，界面是深色。
	click(t, a, tt, "深色")
	if a.themeMode != store.ThemeDark || a.pal.canvas != dark {
		t.Fatalf("选深色后 themeMode=%q canvas=%v", a.themeMode, a.pal.canvas)
	}
	canvasIs(t, tt, dark, "选深色（系统浅色）")
	if want := []string{store.ThemeDark}; !reflect.DeepEqual(*synced, want) {
		t.Errorf("同步给原生的外观 = %q，想要 %q", *synced, want)
	}
	if got := savedTheme(t, cfg); got != store.ThemeDark {
		t.Errorf("config.json 里的外观 = %q，想要 dark", got)
	}
	if a.selName != "alpha" || !tt.HasText("关闭详情") {
		t.Error("切换外观不应清掉选中的技能")
	}

	// 选浅色：系统一变深，界面仍是浅色。
	click(t, a, tt, "浅色")
	tt.SetDark(true)
	refresh(a, tt)
	canvasIs(t, tt, light, "选浅色（系统深色）")
	if got := savedTheme(t, cfg); got != store.ThemeLight {
		t.Errorf("config.json 里的外观 = %q，想要 light", got)
	}

	// 回到跟随系统：系统深色就是深色，系统切回浅色界面跟着变。
	click(t, a, tt, "跟随系统")
	canvasIs(t, tt, dark, "跟随系统（系统深色）")
	tt.SetDark(false)
	refresh(a, tt)
	canvasIs(t, tt, light, "跟随系统（系统换成浅色）")
	if got := savedTheme(t, cfg); got != store.ThemeSystem {
		t.Errorf("config.json 里的外观 = %q，想要 system", got)
	}

	// 重复点同一项：不再同步、不再写盘。
	click(t, a, tt, "跟随系统")
	if want := []string{store.ThemeDark, store.ThemeLight, store.ThemeSystem}; !reflect.DeepEqual(*synced, want) {
		t.Errorf("同步给原生的外观 = %q，想要 %q（每次切换一次，重复点不再同步）", *synced, want)
	}
}

// TestThemeSyncOnlyLatestWins 验证连续切换时，排队的后台任务只有最新一次会
// 执行：旧任务不会把原生外观与 config.json 写回旧值。
func TestThemeSyncOnlyLatestWins(t *testing.T) {
	a, cfg := newTestApp(t)
	synced := recordSource(a)
	var queue []func()
	a.spawn = func(fn func()) { queue = append(queue, fn) }

	a.SetThemeAsync(store.ThemeLight)
	a.SetThemeAsync(store.ThemeDark)
	if a.themeMode != store.ThemeDark {
		t.Fatalf("界面应立刻是最新的选择，得到 %q", a.themeMode)
	}
	if len(*synced) != 0 {
		t.Fatal("同步在后台做：任务排队期间原生外观还没动")
	}
	for len(queue) > 0 {
		fn := queue[0]
		queue = queue[1:]
		fn()
	}
	if want := []string{store.ThemeDark}; !reflect.DeepEqual(*synced, want) {
		t.Errorf("只应同步最新的外观，得到 %q", *synced)
	}
	if got := savedTheme(t, cfg); got != store.ThemeDark {
		t.Errorf("config.json 里的外观 = %q，想要 dark", got)
	}
}

// TestThemeSaveFailureIsReported 验证写不进 config.json 时：界面照样换了皮肤，
// 状态行报告偏好没存上，应用继续可用。
func TestThemeSaveFailureIsReported(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "alpha", nil)
	tt := showApp(a, 1280, 800)
	// 让 config.json 的位置被一个文件夹占着，写不进去。
	if err := os.MkdirAll(cfg.ConfigPath(), 0o755); err != nil {
		t.Fatal(err)
	}

	click(t, a, tt, "深色")
	if a.themeMode != store.ThemeDark {
		t.Errorf("存盘失败也不应回滚已换的皮肤，themeMode=%q", a.themeMode)
	}
	canvasIs(t, tt, kuraScheme.dark.pal.canvas, "存盘失败后仍是深色")
	if !a.statusErr || !tt.HasText("外观偏好没能保存") {
		t.Errorf("状态行应报告没存上，statusErr=%v 文本 %v", a.statusErr, tt.Texts())
	}
	if !tt.HasText("alpha 开关") {
		t.Error("存盘失败后界面应仍可用")
	}
}

// TestThemeRejectsUnknownMode 验证不认识的外观值被拒绝：不换皮肤、不写盘，
// 状态行报错。
func TestThemeRejectsUnknownMode(t *testing.T) {
	a, cfg := newTestApp(t)
	synced := recordSource(a)
	a.SetThemeAsync("sepia")
	if a.themeMode != store.ThemeSystem || !a.statusErr || len(*synced) != 0 {
		t.Errorf("未知外观应被拒绝：themeMode=%q statusErr=%v synced=%q", a.themeMode, a.statusErr, *synced)
	}
	if _, err := os.Stat(cfg.ConfigPath()); !os.IsNotExist(err) {
		t.Errorf("被拒绝的外观不应写盘 (err=%v)", err)
	}
}

// TestThemePreferenceAppliesAtStartup 验证 config.json 里存的外观在启动时
// 生效：界面直接是所存的外观（不管系统怎样），SyncTheme 把它同步给原生外观。
func TestThemePreferenceAppliesAtStartup(t *testing.T) {
	a, cfg := newTestApp(t)
	lib, err := store.LoadLibrary(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.SetTheme(store.ThemeDark); err != nil {
		t.Fatal(err)
	}

	b := newAppFor(cfg) // 模拟重启：从磁盘重新读
	synced := recordSource(b)
	if b.themeMode != store.ThemeDark {
		t.Fatalf("重启后 themeMode = %q，想要 dark", b.themeMode)
	}
	b.SyncTheme()
	if want := []string{store.ThemeDark}; !reflect.DeepEqual(*synced, want) {
		t.Errorf("SyncTheme 应把所存的外观同步给原生，得到 %q", *synced)
	}
	tt := showApp(b, 1280, 800)
	tt.SetDark(false) // 系统是浅色，所存的深色优先
	refresh(b, tt)
	canvasIs(t, tt, kuraScheme.dark.pal.canvas, "启动时按所存的深色渲染")

	// 第一个应用没碰过：它仍按默认的跟随系统。
	if a.themeMode != store.ThemeSystem {
		t.Errorf("没存过外观的应用应跟随系统，得到 %q", a.themeMode)
	}
}
