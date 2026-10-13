package ui

// 右键菜单的界面测试：技能卡片与左栏的目录项。系统调用（用默认方式打开、
// 在文件管理器里显示）一律换成 fakeShell 记录，绝不真的打开任何窗口，剪贴板
// 用 Tester 自带的；全部目录都在 t.TempDir() 里。原生菜单不会出现在截图里，
// 所以靠 Tester 的 Menu / ChooseMenuItem 检查。

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
	myui "github.com/egoist/mygo/ui"
)

// fakeShell 记录「打开」与「在文件夹中显示」的调用，代替真正的系统外壳。
type fakeShell struct {
	mu       sync.Mutex
	opened   []string
	revealed []string
	err      error // openPath 返回的错误
}

// install 把 a 的系统调用换成记录用的假实现，并返回 f。
func (f *fakeShell) install(a *App) *fakeShell {
	a.openPath = func(path string) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.opened = append(f.opened, path)
		return f.err
	}
	a.reveal = func(path string) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.revealed = append(f.revealed, path)
	}
	return f
}

// openMenu 右键点击 text，让界面跟上，返回弹出的菜单项（"-" 是分隔线）。
func openMenu(t *testing.T, a *App, tt *myui.Tester, text string) []string {
	t.Helper()
	if err := tt.RightClick(text); err != nil {
		t.Fatal(err)
	}
	refresh(a, tt)
	return tt.Menu()
}

// choose 选一个菜单项（菜单会随之关闭）并让界面跟上。
func choose(t *testing.T, a *App, tt *myui.Tester, path ...string) {
	t.Helper()
	if err := tt.ChooseMenuItem(path...); err != nil {
		t.Fatal(err)
	}
	refresh(a, tt)
}

// skillByName 在当前目录的技能里找 name。
func skillByName(t *testing.T, a *App, name string) *store.Skill {
	t.Helper()
	for _, s := range a.skills {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("当前目录里没有技能 %q", name)
	return nil
}

// TestCardMenuItemsFollowState 验证卡片右键菜单的内容随技能状态变化：
// 只看不改的几项总有；启用的可停用、检查更新，停用的可启用，回收站里的只能
// 恢复；缺 SKILL.md 的技能不能「打开 SKILL.md」。
func TestCardMenuItemsFollowState(t *testing.T) {
	a, cfg := newTestApp(t)
	seedDescribed(t, cfg, "enabled-one", "启用的技能", nil)
	seedDescribed(t, cfg, "disabled-one", "停用的技能", nil)
	seedSkill(t, cfg, "trashed-one", nil)
	if err := os.MkdirAll(cfg.SkillPath("no-md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.SkillPath("no-md"), "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Disable("disabled-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.Remove("trashed-one"); err != nil {
		t.Fatal(err)
	}
	tt := showApp(a, 1280, 800)

	look := []string{"打开文件夹", "在文件夹中显示", "打开 SKILL.md", "复制路径", "-"}
	with := func(rest ...string) []string { return append(append([]string(nil), look...), rest...) }

	for _, c := range []struct {
		skill string
		want  []string
	}{
		{"enabled-one", with("停用", "检查更新", "-", "移入回收站…")},
		{"no-md", with("停用", "检查更新", "-", "移入回收站…")},
		{"disabled-one", with("启用", "-", "移入回收站…")},
	} {
		if got := openMenu(t, a, tt, c.skill); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s 的菜单 = %q，想要 %q", c.skill, got, c.want)
		}
		tt.CloseMenu()
	}

	// 缺 SKILL.md：那一项灰着，选不了；其余照常。
	openMenu(t, a, tt, "no-md")
	if err := tt.ChooseMenuItem("打开 SKILL.md"); err == nil {
		t.Error("没有 SKILL.md 的技能不应能选「打开 SKILL.md」")
	}
	tt.CloseMenu()
	openMenu(t, a, tt, "enabled-one")
	if err := tt.ChooseMenuItem("打开 SKILL.md"); err != nil {
		t.Errorf("有 SKILL.md 的技能应能打开它: %v", err)
	}
	tt.CloseMenu()

	// 回收站里的：没有「打开 SKILL.md」，只有恢复与永久删除（中间用分隔线隔开）。
	click(t, a, tt, "回收站 1")
	want := []string{"打开文件夹", "在文件夹中显示", "复制路径", "-", "恢复到技能目录", "-", "永久删除…"}
	if got := openMenu(t, a, tt, "trashed-one"); !reflect.DeepEqual(got, want) {
		t.Errorf("回收站里的菜单 = %q，想要 %q", got, want)
	}
}

// TestCardMenuOpensFolderAndFiles 验证只看不改的几项：用系统打开卡片的文件夹
// 与 SKILL.md，在文件管理器里显示，复制路径；打开失败时状态行报错。
func TestCardMenuOpensFolderAndFiles(t *testing.T) {
	a, cfg := newTestApp(t)
	shell := (&fakeShell{}).install(a)
	seedDescribed(t, cfg, "alpha", "第一个技能", nil)
	tt := showApp(a, 1280, 800)
	dir := skillByName(t, a, "alpha").Dir

	openMenu(t, a, tt, "alpha")
	choose(t, a, tt, "打开文件夹")
	if want := []string{dir}; !reflect.DeepEqual(shell.opened, want) {
		t.Errorf("打开文件夹：opened = %q，想要 %q", shell.opened, want)
	}

	openMenu(t, a, tt, "alpha")
	choose(t, a, tt, "在文件夹中显示")
	if want := []string{dir}; !reflect.DeepEqual(shell.revealed, want) {
		t.Errorf("在文件夹中显示：revealed = %q，想要 %q", shell.revealed, want)
	}

	openMenu(t, a, tt, "alpha")
	choose(t, a, tt, "打开 SKILL.md")
	if want := []string{dir, filepath.Join(dir, "SKILL.md")}; !reflect.DeepEqual(shell.opened, want) {
		t.Errorf("打开 SKILL.md：opened = %q，想要 %q", shell.opened, want)
	}

	openMenu(t, a, tt, "alpha")
	choose(t, a, tt, "复制路径")
	if got := tt.Clipboard(); got != dir {
		t.Errorf("剪贴板 = %q，想要 %q", got, dir)
	}
	if !tt.HasText("已复制路径 "+dir) || a.statusErr {
		t.Errorf("状态行应报告已复制，文本 %v", tt.Texts())
	}

	// 这些都只读：技能原样还在，也没有因为菜单而去动磁盘。
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		t.Errorf("菜单里只看不改的几项不应改动技能: %v", err)
	}

	// 系统打不开：状态行报错，界面照常可用。
	shell.err = errors.New("没有关联的程序")
	openMenu(t, a, tt, "alpha")
	choose(t, a, tt, "打开文件夹")
	if !a.statusErr || !tt.HasText("无法打开 "+dir+"：没有关联的程序") {
		t.Errorf("打开失败应在状态行报错，statusErr=%v 文本 %v", a.statusErr, tt.Texts())
	}
	if !tt.HasText("alpha 开关") {
		t.Error("打开失败后界面应仍可用")
	}
}

// TestCardMenuChangesState 验证改状态的几项走与按钮、开关同样的流程：停用、
// 启用直接移动文件夹；移入回收站先弹确认框，确认后才动，恢复把它放回；检查
// 更新打开评审面板。
func TestCardMenuChangesState(t *testing.T) {
	a, cfg := newTestApp(t)
	seedDescribed(t, cfg, "gamma", "会被停用再启用", nil)
	seedDescribed(t, cfg, "delta", "会被移入回收站再恢复", nil)
	seedWithLocalSource(t, cfg, "rev",
		"---\nname: rev\n---\n\ninstalled\n",
		"---\nname: rev\n---\n\nupstream\n")
	tt := showApp(a, 1280, 800)

	// 停用 → 启用。
	openMenu(t, a, tt, "gamma")
	choose(t, a, tt, "停用")
	if _, err := os.Stat(cfg.DisabledPath("gamma")); err != nil {
		t.Errorf("停用后 disabled/gamma 应存在: %v", err)
	}
	if _, err := os.Stat(cfg.SkillPath("gamma")); !os.IsNotExist(err) {
		t.Errorf("停用后根目录的 gamma 应已移走 (err=%v)", err)
	}
	openMenu(t, a, tt, "gamma")
	choose(t, a, tt, "启用")
	if _, err := os.Stat(cfg.SkillPath("gamma")); err != nil {
		t.Errorf("启用后根目录的 gamma 应存在: %v", err)
	}

	// 移入回收站：先确认。
	openMenu(t, a, tt, "delta")
	choose(t, a, tt, "移入回收站…")
	if !a.confirm.open || a.confirm.name != "delta" {
		t.Fatalf("应弹出对 delta 的确认框，得到 %+v", a.confirm)
	}
	if _, err := os.Stat(cfg.SkillPath("delta")); err != nil {
		t.Fatalf("确认前 delta 不应移动: %v", err)
	}
	click(t, a, tt, "确认移入")
	if _, err := os.Stat(cfg.TrashPath("delta")); err != nil {
		t.Errorf("确认后 trash/delta 应存在: %v", err)
	}
	click(t, a, tt, "回收站 1")
	openMenu(t, a, tt, "delta")
	choose(t, a, tt, "恢复到技能目录")
	if _, err := os.Stat(cfg.SkillPath("delta")); err != nil {
		t.Errorf("恢复后根目录的 delta 应存在: %v", err)
	}
	click(t, a, tt, "全部 3")

	// 检查更新：评审面板打开。
	openMenu(t, a, tt, "rev")
	choose(t, a, tt, "检查更新")
	if !a.review.open || a.review.skill != "rev" {
		t.Errorf("检查更新后评审面板应打开并指向 rev，得到 %+v", a.review)
	}
}

// TestCardMenuBusyDisablesChanges 验证技能正忙（后台在处理它）时，菜单里改
// 状态的项灰着，只看不改的照常可用；有检查在跑时「检查更新」也灰着。
func TestCardMenuBusyDisablesChanges(t *testing.T) {
	a, cfg := newTestApp(t)
	shell := (&fakeShell{}).install(a)
	seedDescribed(t, cfg, "alpha", "忙着", nil)
	seedDescribed(t, cfg, "beta", "不忙", nil)
	tt := showApp(a, 1280, 800)

	a.busy[a.busyKey("alpha")] = true
	refresh(a, tt)
	openMenu(t, a, tt, "alpha")
	for _, item := range []string{"停用", "检查更新", "移入回收站…"} {
		if err := tt.ChooseMenuItem(item); err == nil {
			t.Errorf("忙的技能不应能选 %q", item)
		}
	}
	if err := tt.ChooseMenuItem("打开文件夹"); err != nil {
		t.Errorf("忙的技能仍应能打开文件夹: %v", err)
	}
	refresh(a, tt)
	if len(shell.opened) != 1 {
		t.Errorf("打开文件夹应执行一次，opened = %q", shell.opened)
	}
	if _, err := os.Stat(cfg.SkillPath("alpha")); err != nil {
		t.Errorf("被拒绝的操作不应动 alpha: %v", err)
	}
	delete(a.busy, a.busyKey("alpha"))

	// 别的技能在检查更新：「检查更新」灰着，停用仍可用。
	a.checking = "beta"
	refresh(a, tt)
	openMenu(t, a, tt, "alpha")
	if err := tt.ChooseMenuItem("检查更新"); err == nil {
		t.Error("有检查在进行时不应能再发起一次")
	}
	if err := tt.ChooseMenuItem("停用"); err != nil {
		t.Errorf("检查进行时停用仍应可用: %v", err)
	}
}

// TestRightClickSelectsCard 验证右键点卡片会先选中它：右栏换成这张卡片的详情，
// 菜单作用的就是右栏正显示的技能；右键不影响之后的左键点击选择。
func TestRightClickSelectsCard(t *testing.T) {
	a, cfg := newTestApp(t)
	seedDescribed(t, cfg, "alpha", "第一个技能的描述", nil)
	seedDescribed(t, cfg, "beta", "第二个技能的描述", nil)
	tt := showApp(a, 1280, 800)

	click(t, a, tt, "alpha")
	if a.selName != "alpha" {
		t.Fatalf("selName = %q，想要 alpha", a.selName)
	}
	openMenu(t, a, tt, "beta")
	if a.selName != "beta" {
		t.Errorf("右键 beta 后 selName = %q，想要 beta", a.selName)
	}
	if got := countText(tt, "第二个技能的描述"); got != 2 {
		t.Errorf("右栏应换成 beta 的详情，其描述应出现 2 份，得到 %d；文本 %v", got, tt.Texts())
	}
	tt.CloseMenu()
	refresh(a, tt)
	if a.selName != "beta" {
		t.Errorf("关掉菜单后选中应保持，selName = %q", a.selName)
	}

	// 右键之后左键照常选择。
	click(t, a, tt, "alpha")
	if a.selName != "alpha" {
		t.Errorf("右键之后左键点 alpha，selName = %q，想要 alpha", a.selName)
	}
}

// TestRootMenu 验证左栏目录项的右键菜单：都能打开文件夹、复制路径；追加的
// 目录还能从列表移除（只改 config.json，不动文件夹），默认目录没有这一项。
// 右键不会切换目录；移除的若不是正在看的目录，当前目录不变。
func TestRootMenu(t *testing.T) {
	a, cfg := newTestApp(t)
	shell := (&fakeShell{}).install(a)
	seedSkill(t, cfg, "base", nil)
	extra := makeRoot(t, "extra", "x1")
	other := makeRoot(t, "other", "o1")
	for _, r := range []string{extra, other} {
		if err := a.lib.AddRoot(r); err != nil {
			t.Fatal(err)
		}
	}
	tt := showApp(a, 1280, 800)

	// 默认目录：没有「从列表移除」。
	if got, want := openMenu(t, a, tt, rootLabel(cfg.Root)), []string{"打开文件夹", "复制路径"}; !reflect.DeepEqual(got, want) {
		t.Errorf("默认目录的菜单 = %q，想要 %q", got, want)
	}
	tt.CloseMenu()
	// 追加的目录：多一项。右键不切换目录。
	if got, want := openMenu(t, a, tt, rootLabel(extra)), []string{"打开文件夹", "复制路径", "-", "从列表移除"}; !reflect.DeepEqual(got, want) {
		t.Errorf("追加目录的菜单 = %q，想要 %q", got, want)
	}
	if a.cfg.Root != cfg.Root || a.rootSel != cfg.Root {
		t.Errorf("右键不应切换目录：cfg.Root=%s rootSel=%s", a.cfg.Root, a.rootSel)
	}

	choose(t, a, tt, "打开文件夹")
	if want := []string{extra}; !reflect.DeepEqual(shell.opened, want) {
		t.Errorf("opened = %q，想要 %q", shell.opened, want)
	}
	openMenu(t, a, tt, rootLabel(extra))
	choose(t, a, tt, "复制路径")
	if got := tt.Clipboard(); got != extra {
		t.Errorf("剪贴板 = %q，想要 %q", got, extra)
	}

	// 先切到 other，再移除 extra：被移除的不是正在看的目录，所以仍停在 other；
	// 列表与 config.json 少一项，文件夹还在。
	click(t, a, tt, rootLabel(other))
	if a.cfg.Root != other {
		t.Fatalf("应已切到 %s，得到 %s", other, a.cfg.Root)
	}
	openMenu(t, a, tt, rootLabel(extra))
	choose(t, a, tt, "从列表移除")
	if want := []string{cfg.Root, other}; !reflect.DeepEqual(a.lib.Roots, want) {
		t.Errorf("移除后 Roots = %v，想要 %v", a.lib.Roots, want)
	}
	if a.cfg.Root != other || a.rootSel != other {
		t.Errorf("移除别的目录不应换掉正在看的目录：cfg.Root=%s rootSel=%s，想要 %s", a.cfg.Root, a.rootSel, other)
	}
	if !tt.HasText("o1 开关") {
		t.Errorf("仍应显示 other 里的技能，文本 %v", tt.Texts())
	}
	if _, err := os.Stat(filepath.Join(extra, "x1", "SKILL.md")); err != nil {
		t.Errorf("从列表移除不应动磁盘上的技能: %v", err)
	}
	if lib, err := store.LoadLibrary(cfg); err != nil || !reflect.DeepEqual(lib.Roots, a.lib.Roots) {
		t.Errorf("config.json 应与界面一致：Roots=%v err=%v", lib.Roots, err)
	}
	if _, ok := tt.Find(rootLabel(extra)); ok {
		t.Errorf("左栏不应再列出被移除的目录，文本 %v", tt.Texts())
	}

	// 移除正在看的目录：回到默认目录。
	openMenu(t, a, tt, rootLabel(other))
	choose(t, a, tt, "从列表移除")
	if a.cfg.Root != cfg.Root || len(a.lib.Roots) != 1 {
		t.Errorf("移除正在看的目录后应回到默认目录：cfg.Root=%s Roots=%v", a.cfg.Root, a.lib.Roots)
	}
	if _, err := os.Stat(filepath.Join(other, "o1", "SKILL.md")); err != nil {
		t.Errorf("从列表移除不应动磁盘上的技能: %v", err)
	}
}
