package ui

// 排列模式（卡片网格 / 两行列表）的测试：规范化、默认值、切换、持久化、
// 列表里的选中/右键/开关/恢复。全部目录都在 t.TempDir() 里，绝不碰真实的
// ~/.agents/skills 与 ~/.kura；系统调用沿用 newAppFor 的空实现。

import (
	"errors"
	"os"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
	myui "github.com/egoist/mygo/ui"
)

// savedView 读回 config.json 里的排列模式。
func savedView(t *testing.T, cfg *store.Config) string {
	t.Helper()
	lib, err := store.LoadLibrary(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return lib.View
}

// seedViewApp 造一个有两个技能的应用并画出首帧：alpha 与 beta 都有描述。
func seedViewApp(t *testing.T) (*App, *store.Config, *myui.Tester) {
	t.Helper()
	a, cfg := newTestApp(t)
	seedDescribed(t, cfg, "alpha", "第一个技能的描述", nil)
	seedDescribed(t, cfg, "beta", "第二个技能的描述", nil)
	return a, cfg, showApp(a, 1280, 800)
}

func TestNormalizeView(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr error
	}{
		{"", store.ViewGrid, nil},
		{"grid", store.ViewGrid, nil},
		{"list", store.ViewList, nil},
		{"weird", "", store.ErrUnknownView},
		{"LIST", "", store.ErrUnknownView},
	}
	for _, c := range cases {
		got, err := store.NormalizeView(c.in)
		if got != c.want || !errors.Is(err, c.wantErr) {
			t.Errorf("NormalizeView(%q) = %q, %v；想要 %q, %v", c.in, got, err, c.want, c.wantErr)
		}
	}
}

func TestViewIndex(t *testing.T) {
	for mode, want := range map[string]int{store.ViewGrid: 0, store.ViewList: 1, "": 0, "weird": 0} {
		if got := viewIndex(mode); got != want {
			t.Errorf("viewIndex(%q) = %d，想要 %d", mode, got, want)
		}
	}
}

// TestDefaultViewIsGrid 验证新应用默认是卡片网格，卡片里有名字与描述。
func TestDefaultViewIsGrid(t *testing.T) {
	a, _, tt := seedViewApp(t)
	if a.viewMode != store.ViewGrid {
		t.Fatalf("默认排列应是网格，得到 %q", a.viewMode)
	}
	for _, want := range []string{"alpha", "第一个技能的描述", "beta", "第二个技能的描述"} {
		if countText(tt, want) != 1 {
			t.Errorf("网格里 %q 应恰好出现一次，文本 %v", want, tt.Texts())
		}
	}
	// 排列开关的两个分段都在筛选栏里。
	for _, want := range []string{"卡片网格", "列表"} {
		if !tt.HasText(want) {
			t.Errorf("筛选栏缺少排列分段 %q", want)
		}
	}
}

// TestSwitchToListMode 验证点「列表」分段后换成两行列表：技能名与描述仍各
// 出现一次（不重复渲染），行内开关在；点回「卡片网格」又换回去。
func TestSwitchToListMode(t *testing.T) {
	a, cfg, tt := seedViewApp(t)

	click(t, a, tt, "列表")
	if a.viewMode != store.ViewList {
		t.Fatalf("点「列表」后 viewMode = %q", a.viewMode)
	}
	for _, want := range []string{"alpha", "第一个技能的描述", "beta", "第二个技能的描述"} {
		if n := countText(tt, want); n != 1 {
			t.Errorf("列表里 %q 应恰好出现一次，得到 %d，文本 %v", want, n, tt.Texts())
		}
	}
	if !tt.HasText("alpha 开关") || !tt.HasText("beta 开关") {
		t.Errorf("列表行应有开关，文本 %v", tt.Texts())
	}
	if got := savedView(t, cfg); got != store.ViewList {
		t.Errorf("config.json 里的排列 = %q，想要 list", got)
	}

	click(t, a, tt, "卡片网格")
	if a.viewMode != store.ViewGrid {
		t.Fatalf("点「卡片网格」后 viewMode = %q", a.viewMode)
	}
	if n := countText(tt, "alpha"); n != 1 {
		t.Errorf("换回网格后 alpha 应恰好出现一次，得到 %d", n)
	}
	if got := savedView(t, cfg); got != store.ViewGrid {
		t.Errorf("config.json 里的排列 = %q，想要 grid", got)
	}
}

// TestViewModePersistsAndKeepsOtherPrefs 验证 SetView 写盘后重新加载读得回，
// 且不抹掉已有的外观与根目录列表；之后再增删根目录、改外观也不抹掉排列。
func TestViewModePersistsAndKeepsOtherPrefs(t *testing.T) {
	_, cfg := newTestApp(t)
	lib, err := store.LoadLibrary(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if lib.View != store.ViewGrid {
		t.Fatalf("没存过排列时应是 grid，得到 %q", lib.View)
	}
	extra := t.TempDir()
	if err := lib.AddRoot(extra); err != nil {
		t.Fatal(err)
	}
	if err := lib.SetTheme(store.ThemeDark); err != nil {
		t.Fatal(err)
	}
	if err := lib.SetView(store.ViewList); err != nil {
		t.Fatal(err)
	}

	again, err := store.LoadLibrary(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if again.View != store.ViewList {
		t.Errorf("重新加载后 View = %q，想要 list", again.View)
	}
	if again.Theme != store.ThemeDark {
		t.Errorf("SetView 不应抹掉外观，得到 %q", again.Theme)
	}
	if len(again.Roots) != 2 || again.Roots[1] != extra {
		t.Errorf("SetView 不应抹掉根目录列表，得到 %v", again.Roots)
	}

	// 反过来：改外观、改根目录也不抹掉排列。
	if err := again.SetTheme(store.ThemeLight); err != nil {
		t.Fatal(err)
	}
	if err := again.RemoveRoot(extra); err != nil {
		t.Fatal(err)
	}
	last, err := store.LoadLibrary(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if last.View != store.ViewList || last.Theme != store.ThemeLight {
		t.Errorf("改外观/根目录后 View=%q Theme=%q，想要 list/light", last.View, last.Theme)
	}

	// 不认识的值被拒绝，且不写盘。
	if err := last.SetView("weird"); !errors.Is(err, store.ErrUnknownView) {
		t.Errorf("SetView(weird) = %v，想要 ErrUnknownView", err)
	}
	if got := savedView(t, cfg); got != store.ViewList {
		t.Errorf("被拒绝的值不应写盘，config.json 里是 %q", got)
	}
}

// TestUnknownViewInConfigFallsBackToGrid 验证手改出的未知排列值按网格处理，
// 不算错误。
func TestUnknownViewInConfigFallsBackToGrid(t *testing.T) {
	_, cfg := newTestApp(t)
	if err := store.SaveHomeConfig(cfg, &store.HomeConfig{View: "tiles"}); err != nil {
		t.Fatal(err)
	}
	b := newAppFor(cfg)
	if b.libErr != "" || b.viewMode != store.ViewGrid {
		t.Errorf("未知排列应按网格处理：libErr=%q viewMode=%q", b.libErr, b.viewMode)
	}
}

// TestSetViewAsync 验证 SetViewAsync：viewMode 立刻变、状态行无错误、后台
// 写盘；重复调用幂等；连续切换只有最新一次写盘；未知值被拒绝并在状态行
// 报错；重启后仍是所选的模式。
func TestSetViewAsync(t *testing.T) {
	a, cfg := newTestApp(t)
	var queue []func()
	a.spawn = func(fn func()) { queue = append(queue, fn) }

	a.SetViewAsync(store.ViewList)
	if a.viewMode != store.ViewList {
		t.Fatalf("viewMode 应立刻变成 list，得到 %q", a.viewMode)
	}
	if a.statusErr {
		t.Errorf("状态行不应有错误：%q", a.status)
	}
	if len(queue) != 1 {
		t.Fatalf("应排一个后台写盘任务，得到 %d", len(queue))
	}
	if got := savedView(t, cfg); got != store.ViewGrid {
		t.Errorf("任务排队期间不应已写盘，得到 %q", got)
	}
	a.SetViewAsync(store.ViewList) // 幂等
	if len(queue) != 1 {
		t.Errorf("重复设置同一模式不应再排任务，得到 %d", len(queue))
	}
	for _, fn := range queue {
		fn()
	}
	if got := savedView(t, cfg); got != store.ViewList {
		t.Errorf("config.json 里的排列 = %q，想要 list", got)
	}
	if b := newAppFor(cfg); b.viewMode != store.ViewList {
		t.Errorf("重启后 viewMode = %q，想要 list", b.viewMode)
	}

	// 连续切换：旧任务发现序号已变就什么也不写，最终落盘的是最新一次。
	queue = nil
	a.SetViewAsync(store.ViewGrid)
	a.SetViewAsync(store.ViewList)
	a.SetViewAsync(store.ViewGrid)
	for _, fn := range queue {
		fn()
	}
	if got := savedView(t, cfg); got != store.ViewGrid {
		t.Errorf("连续切换后 config.json 里的排列 = %q，想要 grid", got)
	}

	// 未知值：不换排列，状态行报错。
	a.SetViewAsync("weird")
	if a.viewMode != store.ViewGrid || !a.statusErr {
		t.Errorf("未知排列应被拒绝：viewMode=%q statusErr=%v", a.viewMode, a.statusErr)
	}
}

// TestViewSaveFailureIsReported 验证写不进 config.json 时：排列照样换了，
// 状态行报告没存上，界面继续可用。
func TestViewSaveFailureIsReported(t *testing.T) {
	a, cfg, tt := seedViewApp(t)
	// 让 config.json 的位置被一个文件夹占着，写不进去。
	if err := os.MkdirAll(cfg.ConfigPath(), 0o755); err != nil {
		t.Fatal(err)
	}
	click(t, a, tt, "列表")
	if a.viewMode != store.ViewList {
		t.Errorf("存盘失败也不应回滚排列，viewMode=%q", a.viewMode)
	}
	if !a.statusErr || !tt.HasText("排列模式没能保存") {
		t.Errorf("状态行应报告没存上，statusErr=%v 文本 %v", a.statusErr, tt.Texts())
	}
	if !tt.HasText("alpha 开关") {
		t.Error("存盘失败后界面应仍可用")
	}
}

// TestListModeStartsFromSavedPreference 验证存下的列表模式在启动时直接生效。
func TestListModeStartsFromSavedPreference(t *testing.T) {
	_, cfg := newTestApp(t)
	seedDescribed(t, cfg, "alpha", "第一个技能的描述", nil)
	lib, err := store.LoadLibrary(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.SetView(store.ViewList); err != nil {
		t.Fatal(err)
	}
	b := newAppFor(cfg) // 模拟重启：从磁盘重新读
	if b.viewMode != store.ViewList {
		t.Fatalf("重启后 viewMode = %q，想要 list", b.viewMode)
	}
	tt := showApp(b, 1280, 800)
	if countText(tt, "alpha") != 1 || !tt.HasText("alpha 开关") {
		t.Errorf("启动即列表：应有 alpha 与它的开关，文本 %v", tt.Texts())
	}
}

// TestListRowSelectionAndMenu 验证列表行：左键点击选中、右键先选中再弹与卡片
// 相同的菜单；切换排列模式不丢选中。
func TestListRowSelectionAndMenu(t *testing.T) {
	a, _, tt := seedViewApp(t)
	click(t, a, tt, "列表")

	click(t, a, tt, "alpha")
	if a.selName != "alpha" || !tt.HasText("关闭详情") {
		t.Fatalf("点列表行应选中并出现详情：selName=%q", a.selName)
	}
	// 点描述也算点这一行。
	click(t, a, tt, "第二个技能的描述")
	if a.selName != "beta" {
		t.Errorf("点描述应选中 beta，得到 %q", a.selName)
	}

	// 右键 alpha：先选中，再弹出与卡片相同的菜单。
	items := openMenu(t, a, tt, "alpha")
	if a.selName != "alpha" {
		t.Errorf("右键应先选中 alpha，得到 %q", a.selName)
	}
	for _, want := range []string{"打开文件夹", "在文件夹中显示", "打开 SKILL.md", "复制路径", "停用", "移入回收站…"} {
		found := false
		for _, it := range items {
			if it == want {
				found = true
			}
		}
		if !found {
			t.Errorf("右键菜单缺少 %q，菜单 %v", want, items)
		}
	}
	choose(t, a, tt, "停用")
	a.waitBusy()
	refresh(a, tt)
	if !tt.HasText("已停用") {
		t.Errorf("右键停用后行上应有「已停用」标签，文本 %v", tt.Texts())
	}

	// 切回网格：选中不丢，右栏还是 alpha。
	click(t, a, tt, "卡片网格")
	if a.selName != "alpha" || !tt.HasText("关闭详情") {
		t.Errorf("换排列不应清掉选中：selName=%q", a.selName)
	}
}

// TestListRowSwitchAndRestore 验证列表里的开关能停用/启用技能，回收站里的
// 行是「恢复」按钮而不是开关。
func TestListRowSwitchAndRestore(t *testing.T) {
	a, cfg, tt := seedViewApp(t)
	click(t, a, tt, "列表")

	// 点行内开关：alpha 被移到 disabled/，行上出现「已停用」标签。
	click(t, a, tt, "alpha 开关")
	a.waitBusy()
	refresh(a, tt)
	if _, err := os.Stat(cfg.DisabledPath("alpha")); err != nil {
		t.Fatalf("点开关后 disabled/alpha 应存在: %v", err)
	}
	if !tt.HasText("已停用") {
		t.Errorf("停用后行上应有「已停用」标签，文本 %v", tt.Texts())
	}

	// 再点一次：重新启用。
	click(t, a, tt, "alpha 开关")
	a.waitBusy()
	refresh(a, tt)
	if _, err := os.Stat(cfg.SkillPath("alpha")); err != nil {
		t.Errorf("再点开关后 alpha 应回到根目录: %v", err)
	}

	// 移入回收站（右键菜单 + 确认框），回收站筛选下是「恢复」按钮。
	openMenu(t, a, tt, "beta")
	choose(t, a, tt, "移入回收站…")
	click(t, a, tt, "确认移入")
	a.waitBusy()
	refresh(a, tt)
	if _, err := os.Stat(cfg.SkillPath("beta")); !os.IsNotExist(err) {
		t.Fatalf("移入回收站后 beta 应已离开根目录 (err=%v)", err)
	}
	a.filter = filterTrashed
	refresh(a, tt)
	if !tt.HasText("恢复") || tt.HasText("beta 开关") {
		t.Fatalf("回收站行应是「恢复」按钮而不是开关，文本 %v", tt.Texts())
	}
	click(t, a, tt, "恢复")
	a.waitBusy()
	refresh(a, tt)
	if _, err := os.Stat(cfg.SkillPath("beta")); err != nil {
		t.Errorf("恢复后 beta 应回到根目录: %v", err)
	}
}

// TestListEmptyReusesGridEmptyState 验证列表模式没有技能时复用网格的空态。
func TestListEmptyReusesGridEmptyState(t *testing.T) {
	a, _ := newTestApp(t)
	tt := showApp(a, 1280, 800)
	click(t, a, tt, "列表")
	if !tt.HasText("这个目录里还没有技能") {
		t.Errorf("列表模式的空目录应显示空态，文本 %v", tt.Texts())
	}
}

// TestRenderViewModeShots 把两种排列各渲染成 PNG（深色、浅色）供人工看图；
// 只在设置了 KURA_UI_SHOTS=<输出目录> 时运行，CI 不受影响。
func TestRenderViewModeShots(t *testing.T) {
	out := os.Getenv("KURA_UI_SHOTS")
	if out == "" {
		t.Skip("未设置 KURA_UI_SHOTS，跳过排列模式截图")
	}
	a := seedDesignApp(t)
	render := func(name string, w, h int, mode string) {
		a.SetViewAsync(mode)
		a.selName = "tdd"
		for _, dark := range []bool{true, false} {
			tt := newTester(a, w, h)
			tt.SetPreferences(myui.Preferences{ReduceMotion: true})
			tt.SetDark(dark)
			a.settle()
			tt.Frame()
			a.settle()
			tt.Frame()
			suffix := "light"
			if dark {
				suffix = "dark"
			}
			saveShot(t, out, name+"-"+suffix, tt)
		}
	}
	render("view-grid", 1280, 800, store.ViewGrid)
	render("view-list", 1280, 800, store.ViewList)
	render("view-list-narrow", 900, 640, store.ViewList)
	a.filter = filterTrashed
	render("view-list-trash", 1280, 800, store.ViewList)
}
