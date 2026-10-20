package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/internal/market"
	"github.com/ArnoChenFx/kura/internal/store"
	myui "github.com/egoist/mygo/ui"
)

// newTestApp 搭建一个针对临时技能根目录的应用：store.NewConfig 用临时目录，
// 绝不触碰真实的 ~/.agents/skills。spawn 同步执行，update 记入队列，
// 由 flush 显式回放，模拟 Window.Update 的异步语义。
func newTestApp(t *testing.T) (*App, *store.Config) {
	t.Helper()
	root := t.TempDir()
	cfg, err := store.NewConfig(filepath.Join(root, "skills"), filepath.Join(root, ".kura"))
	if err != nil {
		t.Fatal(err)
	}
	return newAppFor(cfg), cfg
}

// newAppFor 为 cfg 建一个同步 spawn、手动回放 update 的应用。需要在建应用
// 之前先摆好磁盘状态（比如损坏的 config.json）时单独使用。
func newAppFor(cfg *store.Config) *App {
	a := NewApp(cfg)
	var mu sync.Mutex
	a.spawn = func(fn func()) {
		mu.Lock()
		defer mu.Unlock()
		fn()
	}
	a.update = func(fn func()) { a.pending = append(a.pending, fn) }
	// 系统调用一律换成空实现：测试绝不真的打开文件夹或改系统外观。
	// 需要检查调用的测试再换成 fakeShell（见 menus_test.go）。
	a.openPath = func(string) error { return nil }
	a.reveal = func(string) {}
	a.setSource = func(string) {}
	// 语言：生产走 mygo.App.SetLocale（见 NewApp），测试里事件循环没跑，
	// 改用 internal/i18n 的进程级覆盖；解析口径与生产一致（system → 系统
	// locale）。
	a.setLocale = func(pref string) {
		i18n.Set(i18n.Resolve(store.LangPref(pref), i18n.SystemLocale()))
	}
	// 技能市场的榜单读取：测试里绝不联网，默认报错；需要榜单内容的测试自己替换。
	a.marketFetch = func(context.Context, market.View, int) (market.Page, error) {
		return market.Page{}, errors.New("no network in tests")
	}
	a.marketDocFetch = func(context.Context, string, string) (market.Document, error) {
		return market.Document{}, errors.New("no network in tests")
	}
	// 翻译：测试里默认报错，绝不打到真实接口（需要译文的测试换成 fakeTranslator）。
	a.translate = func(context.Context, store.Settings, string) (string, error) {
		return "", errors.New("no network in tests")
	}
	// 外部链接：测试里不打开浏览器（需要检查调用的测试换成记录用的假实现）。
	a.openURL = func(string) error { return nil }
	return a
}

// flush 回放所有待执行的 update 闭包，相当于窗口处理完一帧。
func (a *App) flush() {
	for len(a.pending) > 0 {
		fns := a.pending
		a.pending = nil
		for _, fn := range fns {
			fn()
		}
	}
}

// settle 让 UI 完成「输入 → 后台任务 → update 回放 → 重绘」一整个周期，
// 直到没有遗留任务，避免依赖固定帧数。
func (a *App) settle() {
	a.flush()
}

// seedSkill 在根目录写一个最小可用的技能文件夹（SKILL.md + 可选元数据）。
func seedSkill(t *testing.T, cfg *store.Config, name string, meta *store.Meta) {
	t.Helper()
	dir := cfg.SkillPath(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\n---\n\n# "+name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if meta != nil {
		data, err := json.MarshalIndent(meta, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, store.MetaFileName), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// waitBusy 等待所有后台任务结束并回放 update（同步 spawn 下即为回放）。
func (a *App) waitBusy() {
	a.flush()
}

// newTester 启动 Tester 驱动 a.View。
func newTester(a *App, w, h int) *myui.Tester {
	return myui.NewTester(a.View, w, h)
}

// TestListShowsSkillsFromTempRoot 验证：临时根目录里的技能出现在列表中，
// 且详情面板显示元数据字段与根目录路径。
func TestListShowsSkillsFromTempRoot(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "alpha", &store.Meta{
		ID:          "alpha",
		DisplayName: "alpha",
		Source:      store.Source{Type: store.SourceGitHub, URL: "https://github.com/me/alpha.git", Ref: "main", Commit: "abc1234"},
		InstalledAt: "2026-01-01T00:00:00Z",
		UpdatedAt:   "2026-02-02T00:00:00Z",
		ContentHash: "sha256:deadbeefdeadbeefdeadbeef",
	})
	seedSkill(t, cfg, "beta", nil)

	tt := newTester(a, 980, 640)
	a.settle() // 回放首帧触发的初始扫描
	tt.Frame()
	a.settle()
	tt.Frame()

	for _, name := range []string{"alpha", "beta"} {
		if !tt.HasText(name) {
			t.Errorf("列表缺少 %q，文本 %v", name, tt.Texts())
		}
	}
	// 点击 beta 行选中，详情面板显示元数据与根目录。
	if err := tt.Click("beta"); err != nil {
		t.Fatal(err)
	}
	a.settle()
	tt.Frame()
	if !tt.HasText("https://github.com/me/alpha.git") && tt.HasText("alpha") {
		// alpha 未选中，详情应是 beta 的：无元数据提示。
		if !tt.HasText("没有 .kura-meta.json") {
			t.Errorf("beta 详情应提示没有元数据，文本 %v", tt.Texts())
		}
	}
	if !tt.HasText(cfg.Root) {
		t.Errorf("页脚应显示根目录 %q", cfg.Root)
	}
	// 选中 alpha 后详情显示来源 URL 与 Ref。
	if err := tt.Click("alpha"); err != nil {
		t.Fatal(err)
	}
	a.settle()
	tt.Frame()
	for _, want := range []string{"https://github.com/me/alpha.git", "main", "abc1234"} {
		if !tt.HasText(want) {
			t.Errorf("alpha 详情缺少 %q，文本 %v", want, tt.Texts())
		}
	}
}

// TestToggleMovesFolder 验证：点击开关（经详情面板的停用/启用按钮，
// 语义与开关一致）后文件夹在根目录与 disabled/ 之间移动。
func TestToggleMovesFolder(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "gamma", nil)

	tt := newTester(a, 980, 640)
	a.settle()
	tt.Frame()
	a.settle()
	tt.Frame()

	// 选中 gamma。
	if err := tt.Click("gamma"); err != nil {
		t.Fatal(err)
	}
	a.settle()
	tt.Frame()

	// 停用：点击详情面板的「停用」按钮。
	if err := tt.Click("停用"); err != nil {
		t.Fatal(err)
	}
	a.waitBusy()
	tt.Frame()
	if _, err := os.Stat(cfg.DisabledPath("gamma")); err != nil {
		t.Errorf("停用后 disabled/gamma 应存在: %v", err)
	}
	if _, err := os.Stat(cfg.SkillPath("gamma")); !os.IsNotExist(err) {
		t.Errorf("停用后根目录的 gamma 应已移走 (err=%v)", err)
	}
	if !tt.HasText("已停用 gamma") {
		t.Errorf("状态行应显示已停用，文本 %v", tt.Texts())
	}
	a.settle()
	tt.Frame()

	// 重新启用：点击「启用」。
	if err := tt.Click("启用"); err != nil {
		t.Fatal(err)
	}
	a.waitBusy()
	tt.Frame()
	if _, err := os.Stat(cfg.SkillPath("gamma")); err != nil {
		t.Errorf("启用后根目录的 gamma 应存在: %v", err)
	}
	if _, err := os.Stat(cfg.DisabledPath("gamma")); !os.IsNotExist(err) {
		t.Errorf("启用后 disabled/gamma 应已移走 (err=%v)", err)
	}
	if !tt.HasText("已启用 gamma") {
		t.Errorf("状态行应显示已启用，文本 %v", tt.Texts())
	}
}

// TestSwitchOnRowToggles 验证：直接点击行内开关也会触发同样的移动。
func TestSwitchOnRowToggles(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "delta", nil)

	tt := newTester(a, 980, 640)
	a.settle()
	tt.Frame()
	a.settle()
	tt.Frame()

	// 行内开关的辅助标签是「<名> 开关」。
	if err := tt.Click("delta 开关"); err != nil {
		t.Fatal(err)
	}
	a.waitBusy()
	tt.Frame()
	if _, err := os.Stat(cfg.DisabledPath("delta")); err != nil {
		t.Errorf("点开关后 disabled/delta 应存在: %v", err)
	}
	a.settle()
	tt.Frame()

	// 再点一次：开关目前禁用（busy 刚清除、行仍是停用态的开关处于禁用），
	// 停用的技能在行内用「恢复」之外没有开关，因此行内开关消失。
	if tt.HasText("delta 开关") {
		// 仍显示（列表刷新后 delta 回到 enabled）？不应发生：busy 已清。
		if _, err := os.Stat(cfg.SkillPath("delta")); err == nil {
			t.Errorf("delta 不应自动回到根目录")
		}
	}
}

// TestSearchFilters 验证：搜索框输入即时过滤列表。
func TestSearchFilters(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "notes-api", nil)
	seedSkill(t, cfg, "pdf-tools", nil)

	tt := newTester(a, 980, 640)
	a.settle()
	tt.Frame()
	a.settle()
	tt.Frame()

	// 点搜索框并输入。
	if err := tt.Click("搜索技能"); err != nil {
		t.Fatal(err)
	}
	tt.Type("notes")
	a.settle()
	tt.Frame()

	if tt.HasText("pdf-tools") {
		t.Errorf("搜索 notes 后不应显示 pdf-tools，文本 %v", tt.Texts())
	}
	if !tt.HasText("notes-api") {
		t.Errorf("搜索 notes 后应显示 notes-api，文本 %v", tt.Texts())
	}

	// 清空搜索（Ctrl+A 全选后 Backspace）恢复全部。
	tt.Key(myui.Ctrl, myui.KeyA)
	tt.Key(0, myui.KeyBackspace)
	a.settle()
	tt.Frame()
	if !tt.HasText("pdf-tools") {
		t.Errorf("清空搜索后应恢复显示 pdf-tools，文本 %v", tt.Texts())
	}
}

// TestRemoveConfirmAndRestore 验证：删除需确认，确认后进入回收站（只在
// 「回收站」筛选下可见），卡片上的「恢复」把它放回根目录。
func TestRemoveConfirmAndRestore(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "eps", nil)

	tt := newTester(a, 980, 640)
	a.settle()
	tt.Frame()
	a.settle()
	tt.Frame()

	if err := tt.Click("eps"); err != nil {
		t.Fatal(err)
	}
	a.settle()
	tt.Frame()

	// 点「移入回收站」→ 弹确认框。
	if err := tt.Click("详情:移入回收站"); err != nil {
		t.Fatal(err)
	}
	a.flush()
	tt.Frame()
	if !a.confirm.open {
		t.Fatal("点删除后确认框应打开")
	}
	// 文件夹还没动。
	if _, err := os.Stat(cfg.SkillPath("eps")); err != nil {
		t.Fatalf("确认前 eps 不应移动: %v", err)
	}

	// 点确认按钮。
	if err := tt.Click("确认移入"); err != nil {
		t.Fatal(err)
	}
	a.waitBusy()
	tt.Frame()
	if _, err := os.Stat(cfg.TrashPath("eps")); err != nil {
		t.Errorf("确认后 trash/eps 应存在: %v", err)
	}
	if _, err := os.Stat(cfg.SkillPath("eps")); !os.IsNotExist(err) {
		t.Errorf("确认后根目录的 eps 应已移走 (err=%v)", err)
	}
	a.settle()
	tt.Frame()

	// 「全部」下不再有它的卡片（详情栏仍停在它身上，方便直接恢复）。
	if tt.HasText("eps 开关") {
		t.Errorf("回收站里的技能不应出现在「全部」的卡片里，文本 %v", tt.Texts())
	}
	// 切到「回收站」筛选：卡片上是「恢复」。
	if err := tt.Click("回收站 1"); err != nil {
		t.Fatal(err)
	}
	a.settle()
	tt.Frame()
	if err := tt.Click("恢复"); err != nil {
		t.Fatal(err)
	}
	a.waitBusy()
	tt.Frame()
	if _, err := os.Stat(cfg.SkillPath("eps")); err != nil {
		t.Errorf("恢复后根目录的 eps 应存在: %v", err)
	}
	if !tt.HasText("已恢复 eps") {
		t.Errorf("状态行应显示已恢复，文本 %v", tt.Texts())
	}
}

// TestImportMetaAction 验证：「导入元数据」读取 .skill-lock.json 并补齐
// 缺失的 .kura-meta.json。
func TestImportMetaAction(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "from-lock", nil)

	// 在临时 .agents 下伪造 lock 文件。
	agents := filepath.Join(filepath.Dir(cfg.Root), ".agents")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(agents, ".skill-lock.json")
	lockJSON := fmt.Sprintf(`{"version":3,"skills":{"from-lock":{"source":"me/repo","sourceType":"github","sourceUrl":"https://github.com/me/repo.git","skillPath":"skills/from-lock/SKILL.md","pluginName":"my-plugin","installedAt":"2026-03-01T00:00:00Z","updatedAt":"2026-03-02T00:00:00Z"}}}`)
	if err := os.WriteFile(lock, []byte(lockJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	a.SetLockPath(lock)

	tt := newTester(a, 980, 640)
	a.settle()
	tt.Frame()
	a.settle()
	tt.Frame()

	if err := tt.Click("导入元数据"); err != nil {
		t.Fatal(err)
	}
	a.waitBusy()
	tt.Frame()

	if _, err := os.Stat(filepath.Join(cfg.SkillPath("from-lock"), store.MetaFileName)); err != nil {
		t.Errorf("导入后应写入 .kura-meta.json: %v", err)
	}
	if !tt.HasText("已导入 1 条元数据") {
		t.Errorf("状态行应显示导入成功，文本 %v", tt.Texts())
	}
	// 详情里能看到 pluginName。
	if err := tt.Click("from-lock"); err != nil {
		t.Fatal(err)
	}
	a.settle()
	tt.Frame()
	if !tt.HasText("my-plugin") {
		t.Errorf("详情应显示 pluginName，文本 %v", tt.Texts())
	}
}

// TestMissingRootKeepsAppUsable 验证：根目录不存在时显示错误、界面可用、
// 不 panic；后续创建目录后刷新即恢复。
func TestMissingRootKeepsAppUsable(t *testing.T) {
	a, cfg := newTestApp(t)
	// 故意不创建 cfg.Root。

	tt := newTester(a, 980, 640)
	a.settle()
	tt.Frame()
	a.settle()
	tt.Frame()

	if !tt.HasText("技能目录不存在") {
		t.Errorf("应显示目录缺失的提示，文本 %v", tt.Texts())
	}
	if !tt.HasText("这个目录里还没有技能") {
		t.Errorf("空列表应显示占位文本，文本 %v", tt.Texts())
	}
	// 刷新按钮仍然可用。
	if err := tt.Click("刷新"); err != nil {
		t.Fatal(err)
	}
	a.waitBusy()
	tt.Frame()

	// 创建根目录后刷新恢复。
	if err := os.MkdirAll(cfg.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("刷新"); err != nil {
		t.Fatal(err)
	}
	a.waitBusy()
	tt.Frame()
	if a.loadErr != "" {
		t.Errorf("根目录创建后 loadErr 应清空，得到 %q", a.loadErr)
	}
	// 计数显示在筛选栏的每个标签里。
	if !tt.HasText("启用 0") || !tt.HasText("停用 0") {
		t.Errorf("筛选栏应显示各状态的数量，文本 %v", tt.Texts())
	}
	if tt.HasText("技能目录不存在") {
		t.Errorf("目录创建后缺失提示应消失，文本 %v", tt.Texts())
	}
}

// TestCorruptSkillFolderDoesNotPanic 验证：损坏的技能文件夹（元数据坏、
// 缺 SKILL.md）只显示降级信息，扫描与应用都不崩。
func TestCorruptSkillFolderDoesNotPanic(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "good", nil)
	// 元数据损坏的技能。
	bad := cfg.SkillPath("bad")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, store.MetaFileName), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	tt := newTester(a, 980, 640)
	a.settle()
	tt.Frame()
	a.settle()
	tt.Frame()

	if !tt.HasText("bad") {
		t.Errorf("损坏的技能仍应出现在列表，文本 %v", tt.Texts())
	}
	if err := tt.Click("bad"); err != nil {
		t.Fatal(err)
	}
	a.settle()
	tt.Frame()
	if !tt.HasText("元数据读取失败") {
		t.Errorf("损坏元数据应显示降级信息，文本 %v", tt.Texts())
	}
	// 好技能不受影响。
	if !tt.HasText("good") {
		t.Errorf("good 应仍在列表，文本 %v", tt.Texts())
	}
}

// TestDisabledRowSwitchReenables 验证：停用技能的行内开关显示为关，
// 点击它重新启用（文件夹移回根目录）。
func TestDisabledRowSwitchReenables(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "off-skill", nil)
	// 先手动停用。
	if _, err := cfg.Disable("off-skill"); err != nil {
		t.Fatal(err)
	}

	tt := newTester(a, 980, 640)
	a.settle()
	tt.Frame()
	a.settle()
	tt.Frame()

	if !tt.HasText("已停用") {
		t.Errorf("停用技能应显示状态标签，文本 %v", tt.Texts())
	}
	// 点击关着的开关 → 重新启用。
	if err := tt.Click("off-skill 开关"); err != nil {
		t.Fatalf("行内开关应存在: %v", err)
	}
	a.waitBusy()
	tt.Frame()
	if _, err := os.Stat(cfg.SkillPath("off-skill")); err != nil {
		t.Errorf("点击关闭的开关后技能应回到根目录: %v", err)
	}
	if _, err := os.Stat(cfg.DisabledPath("off-skill")); !os.IsNotExist(err) {
		t.Errorf("disabled/off-skill 应已移走 (err=%v)", err)
	}
}

// TestHeaderCountsAndDetailOnSelect 验证三栏结构的两块核心：
//  1. 中栏头部显示目录名与完整路径，筛选栏的每个标签带数量；
//  2. 没选中技能时不占出右栏，选中后右栏出现元数据与位置。
func TestHeaderCountsAndDetailOnSelect(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "alpha", &store.Meta{
		ID: "alpha", DisplayName: "alpha",
		Source: store.Source{Type: store.SourceGitHub, URL: "https://github.com/me/alpha.git"},
	})
	seedSkill(t, cfg, "off", nil)
	if _, err := cfg.Disable("off"); err != nil {
		t.Fatal(err)
	}

	tt := newTester(a, 980, 640)
	a.settle()
	tt.Frame()
	a.settle()
	tt.Frame()

	// 紧凑头部：目录名称，完整路径在悬停提示；筛选栏仍显示各状态数量。
	if countText(tt, rootLabel(cfg.Root)) != 3 {
		t.Errorf("侧栏与头部应显示当前目录名称，文本 %v", tt.Texts())
	}
	for _, want := range []string{"全部 2", "启用 1", "停用 1", "回收站 0"} {
		if _, ok := tt.Find(want); !ok {
			t.Errorf("筛选栏应有 %q，文本 %v", want, tt.Texts())
		}
	}
	// 未选中：没有右栏。
	for _, absent := range []string{"元数据", "位置", "关闭详情"} {
		if _, ok := tt.Find(absent); ok {
			t.Errorf("未选中时不应出现 %q，文本 %v", absent, tt.Texts())
		}
	}

	// 选中 alpha：元数据字段与位置出现，来源 URL 也在。
	if err := tt.Click("alpha"); err != nil {
		t.Fatal(err)
	}
	a.settle()
	tt.Frame()
	for _, want := range []string{"元数据", "位置", "来源", "https://github.com/me/alpha.git", cfg.SkillPath("alpha")} {
		if !tt.HasText(want) {
			t.Errorf("选中后右栏应显示 %q，文本 %v", want, tt.Texts())
		}
	}

	// 关闭详情：右栏收起，卡片仍在。
	if err := tt.Click("关闭详情"); err != nil {
		t.Fatal(err)
	}
	a.settle()
	tt.Frame()
	if _, ok := tt.Find("元数据"); ok {
		t.Errorf("关闭详情后不应再有元数据小节，文本 %v", tt.Texts())
	}
	if !tt.HasText("alpha 开关") {
		t.Errorf("关闭详情后卡片应仍在，文本 %v", tt.Texts())
	}
}

// TestStatusBarShowsTotal 验证状态条显示技能总数（共 N 个技能），
// 与筛选栏的「全部」一致：回收站里的技能不计入。
func TestStatusBarShowsTotal(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "one", nil)
	seedSkill(t, cfg, "two", nil)
	seedSkill(t, cfg, "three", nil)
	if _, err := cfg.Remove("three"); err != nil {
		t.Fatal(err)
	}

	tt := newTester(a, 980, 640)
	a.settle()
	tt.Frame()
	a.settle()
	tt.Frame()

	if !tt.HasText("共 2 个技能") {
		t.Errorf("状态条应显示总数（不含回收站），文本 %v", tt.Texts())
	}
	if _, ok := tt.Find("全部 2"); !ok {
		t.Errorf("筛选栏的「全部」应与状态条一致，文本 %v", tt.Texts())
	}
	if !tt.HasText("已扫描 2 个技能") {
		t.Errorf("扫描完成的提示也不应把回收站算进去，文本 %v", tt.Texts())
	}
}

// TestTrashedCardLooksDifferent 验证回收站卡片的降级外观：只在「回收站」
// 筛选下出现，带「回收站」标签，卡片上是「恢复」而不是开关。
func TestTrashedCardLooksDifferent(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "gone", nil)
	if _, err := cfg.Remove("gone"); err != nil {
		t.Fatal(err)
	}

	tt := newTester(a, 980, 640)
	a.settle()
	tt.Frame()
	a.settle()
	tt.Frame()

	if _, ok := tt.Find("回收站 1"); !ok {
		t.Errorf("筛选栏应统计回收站技能，文本 %v", tt.Texts())
	}
	// 「全部」下看不到它。
	if tt.HasText("gone") {
		t.Errorf("「全部」不应显示回收站技能，文本 %v", tt.Texts())
	}

	if err := tt.Click("回收站 1"); err != nil {
		t.Fatal(err)
	}
	a.settle()
	tt.Frame()
	if !tt.HasText("gone") {
		t.Fatalf("「回收站」筛选下应显示 gone，文本 %v", tt.Texts())
	}
	if _, ok := tt.Find("回收站"); !ok {
		t.Errorf("回收站卡片应带「回收站」标签，文本 %v", tt.Texts())
	}
	// 没有开关（Label 是「gone 开关」），只有恢复按钮。
	if tt.HasText("gone 开关") {
		t.Error("回收站技能不应显示启用开关")
	}
	if err := tt.Click("恢复"); err != nil {
		t.Fatalf("回收站卡片应有恢复按钮: %v", err)
	}
	a.waitBusy()
	tt.Frame()
	if _, err := os.Stat(cfg.SkillPath("gone")); err != nil {
		t.Errorf("恢复后根目录应有 gone: %v", err)
	}
	// 回到「全部」：开关回来了。
	if err := tt.Click("全部 1"); err != nil {
		t.Fatal(err)
	}
	a.settle()
	tt.Frame()
	if !tt.HasText("gone 开关") {
		t.Errorf("恢复后卡片上应出现开关，文本 %v", tt.Texts())
	}
}
