package ui

// 左栏（技能目录列表）的界面测试：列出、切换、添加、移除，以及各目录
// 状态互相隔离。文件夹选择框用假实现替换；全部目录都在 t.TempDir() 里。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
)

// TestRootsColumnListsRootsAndCounts 验证左栏列出每个目录与其在用技能数。
func TestRootsColumnListsRootsAndCounts(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "a1", nil)
	seedSkill(t, cfg, "a2", nil)
	seedSkill(t, cfg, "a3", nil)
	if _, err := cfg.Disable("a3"); err != nil {
		t.Fatal(err)
	}
	other := makeRoot(t, "extra", "x1", "x2", "x3", "x4")
	if err := a.lib.AddRoot(other); err != nil {
		t.Fatal(err)
	}
	tt := showApp(a, 1280, 800)

	// 在用技能数不含停用与回收站；另一个目录的数量由后台统计。
	if a.rootCounts[cfg.Root] != 2 || a.rootCounts[other] != 4 {
		t.Errorf("rootCounts = %v，想要 默认=2 其他=4", a.rootCounts)
	}
	for _, root := range []string{cfg.Root, other} {
		if _, ok := tt.Find(rootLabel(root)); !ok {
			t.Errorf("左栏应列出 %s，文本 %v", rootLabel(root), tt.Texts())
		}
	}
	if countText(tt, "2") != 1 || countText(tt, "4") != 1 {
		t.Errorf("左栏应显示两个目录的技能数 2 和 4，文本 %v", tt.Texts())
	}
}

// TestSwitchRootShowsThatRootsSkills 验证点击左栏的目录后，中栏换成那个
// 目录的技能，头部的操作也跟着换（默认目录可导入元数据，其他目录可移除）。
func TestSwitchRootShowsThatRootsSkills(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "in-default", nil)
	other := makeRoot(t, "extra", "in-extra")
	if err := a.lib.AddRoot(other); err != nil {
		t.Fatal(err)
	}
	tt := showApp(a, 1280, 800)

	if !tt.HasText("in-default 开关") || tt.HasText("in-extra") {
		t.Fatalf("起初应只显示默认目录的技能，文本 %v", tt.Texts())
	}
	if _, ok := tt.Find("导入元数据"); !ok {
		t.Error("默认目录的头部应有「导入元数据」")
	}
	if _, ok := tt.Find("从列表移除此目录"); ok {
		t.Error("默认目录不能被移除，头部不应有移除按钮")
	}

	click(t, a, tt, rootLabel(other))
	if a.cfg.Root != other {
		t.Fatalf("cfg.Root = %s，想要 %s", a.cfg.Root, other)
	}
	if !tt.HasText("in-extra 开关") || tt.HasText("in-default") {
		t.Errorf("切换后应只显示另一个目录的技能，文本 %v", tt.Texts())
	}
	if countText(tt, rootLabel(other)) != 3 {
		t.Errorf("侧栏和紧凑头部应显示另一个目录的名称，文本 %v", tt.Texts())
	}
	if _, ok := tt.Find("从列表移除此目录"); !ok {
		t.Errorf("非默认目录的头部应有移除按钮，文本 %v", tt.Texts())
	}
	if _, ok := tt.Find("导入元数据"); ok {
		t.Error("导入元数据只描述默认目录，其他目录不应出现")
	}

	// 切回默认目录。
	click(t, a, tt, rootLabel(cfg.Root))
	if a.cfg.Root != cfg.Root || !tt.HasText("in-default 开关") {
		t.Errorf("应切回默认目录，cfg.Root=%s 文本 %v", a.cfg.Root, tt.Texts())
	}
}

// TestSwitchRootClearsSelection 验证切换目录会收起右栏：所选技能属于旧目录。
func TestSwitchRootClearsSelection(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "in-default", nil)
	other := makeRoot(t, "extra", "in-extra")
	if err := a.lib.AddRoot(other); err != nil {
		t.Fatal(err)
	}
	tt := showApp(a, 1280, 800)
	click(t, a, tt, "in-default")
	if _, ok := tt.Find("关闭详情"); !ok {
		t.Fatal("选中后应有右栏")
	}

	click(t, a, tt, rootLabel(other))
	if _, ok := tt.Find("关闭详情"); ok || a.selName != "" {
		t.Errorf("切换目录后应收起右栏，selName=%q 文本 %v", a.selName, tt.Texts())
	}
}

// TestAddRootViaPicker 验证「添加文件夹」：选择→写入 config.json→切换过去；
// 重复添加只是切换；取消不改变任何东西；无效路径报错。
func TestAddRootViaPicker(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "base", nil)
	dir := makeRoot(t, "projects", "one", "two")
	picked := dir
	a.pickFolder = func() (string, error) { return picked, nil }
	tt := showApp(a, 1280, 800)

	click(t, a, tt, "添加文件夹")
	if a.cfg.Root != dir || len(a.lib.Roots) != 2 {
		t.Fatalf("添加后应切换到新目录：cfg.Root=%s Roots=%v", a.cfg.Root, a.lib.Roots)
	}
	for _, want := range []string{"one 开关", "two 开关"} {
		if !tt.HasText(want) {
			t.Errorf("应显示新目录的技能 %q，文本 %v", want, tt.Texts())
		}
	}
	if tt.HasText("base 开关") {
		t.Error("不应再显示默认目录的技能")
	}
	if !tt.HasText("已添加目录 " + dir) {
		t.Errorf("状态行应报告添加成功，文本 %v", tt.Texts())
	}
	// 写进了 config.json：重新读取仍有这个目录。
	lib, err := store.LoadLibrary(cfg)
	if err != nil || len(lib.Roots) != 2 || lib.Roots[1] != dir {
		t.Errorf("config.json 应记录新目录：Roots=%v err=%v", lib.Roots, err)
	}

	// 重复添加：不报错，只是切换过去。
	click(t, a, tt, rootLabel(cfg.Root))
	click(t, a, tt, "添加文件夹")
	if a.cfg.Root != dir || len(a.lib.Roots) != 2 || a.statusErr {
		t.Errorf("重复添加应只切换：cfg.Root=%s Roots=%v statusErr=%v", a.cfg.Root, a.lib.Roots, a.statusErr)
	}

	// 取消选择框：什么都不变。
	click(t, a, tt, rootLabel(cfg.Root))
	picked = ""
	status := a.status
	click(t, a, tt, "添加文件夹")
	if a.cfg.Root != cfg.Root || len(a.lib.Roots) != 2 || a.status != status || a.statusErr {
		t.Errorf("取消不应有任何变化：cfg.Root=%s status=%q", a.cfg.Root, a.status)
	}

	// 无效路径：报错，目录列表不变，界面仍可用。
	picked = filepath.Join(dir, "nope")
	click(t, a, tt, "添加文件夹")
	if !a.statusErr || !tt.HasText("路径不存在或不是文件夹") || len(a.lib.Roots) != 2 {
		t.Errorf("无效路径应报错：statusErr=%v Roots=%v 文本 %v", a.statusErr, a.lib.Roots, tt.Texts())
	}
	if a.adding {
		t.Error("失败后「添加文件夹」应恢复可用")
	}
}

// TestRemoveRootKeepsFilesAndGoesBack 验证「从列表移除此目录」只改列表：
// 文件夹与技能原样保留，回到默认目录。
func TestRemoveRootKeepsFilesAndGoesBack(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "base", nil)
	other := makeRoot(t, "extra", "keep-me")
	if err := a.lib.AddRoot(other); err != nil {
		t.Fatal(err)
	}
	tt := showApp(a, 1280, 800)
	click(t, a, tt, rootLabel(other))

	click(t, a, tt, "从列表移除此目录")
	if a.cfg.Root != cfg.Root || len(a.lib.Roots) != 1 {
		t.Fatalf("移除后应回到默认目录：cfg.Root=%s Roots=%v", a.cfg.Root, a.lib.Roots)
	}
	if _, err := os.Stat(filepath.Join(other, "keep-me", "SKILL.md")); err != nil {
		t.Errorf("移除目录不应动磁盘上的技能: %v", err)
	}
	if _, ok := tt.Find(rootLabel(other)); ok {
		t.Errorf("左栏不应再列出被移除的目录，文本 %v", tt.Texts())
	}
	if !tt.HasText("文件夹未改动") || a.statusErr {
		t.Errorf("状态行应说明文件夹未改动，文本 %v", tt.Texts())
	}
	if _, ok := a.rootCounts[other]; ok {
		t.Error("rootCounts 里不应再有被移除的目录")
	}
	lib, err := store.LoadLibrary(cfg)
	if err != nil || len(lib.Roots) != 1 {
		t.Errorf("config.json 应不再记录该目录：Roots=%v err=%v", lib.Roots, err)
	}
}

// TestSameNameSkillsInDifferentRootsAreIsolated 验证两个目录里的同名技能互
// 不干扰：在另一个目录停用 dup，默认目录的 dup 不动，且状态存在各自的
// 作用域里。
func TestSameNameSkillsInDifferentRootsAreIsolated(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "dup", nil)
	other := makeRoot(t, "extra", "dup")
	if err := a.lib.AddRoot(other); err != nil {
		t.Fatal(err)
	}
	tt := showApp(a, 1280, 800)
	click(t, a, tt, rootLabel(other))

	click(t, a, tt, "dup 开关")
	a.waitBusy()
	tt.Frame()

	otherCfg := a.lib.Config(other)
	if !strings.Contains(otherCfg.DisabledPath("dup"), filepath.Join(store.RootsDir, store.RootID(other))) {
		t.Fatalf("另一个目录的状态应在 roots/<id> 下，得到 %s", otherCfg.DisabledPath("dup"))
	}
	if _, err := os.Stat(otherCfg.DisabledPath("dup")); err != nil {
		t.Errorf("另一个目录的 dup 应已停用: %v", err)
	}
	if _, err := os.Stat(filepath.Join(other, "dup")); !os.IsNotExist(err) {
		t.Errorf("另一个目录里的 dup 应已移走 (err=%v)", err)
	}
	if _, err := os.Stat(cfg.SkillPath("dup")); err != nil {
		t.Errorf("默认目录的 dup 不应受影响: %v", err)
	}
	if _, err := os.Stat(cfg.DisabledPath("dup")); !os.IsNotExist(err) {
		t.Errorf("默认目录不应出现停用的 dup (err=%v)", err)
	}

	// 回到默认目录：dup 仍是启用的。
	click(t, a, tt, rootLabel(cfg.Root))
	if len(a.skills) != 1 || a.skills[0].State != store.StateEnabled {
		t.Errorf("默认目录的 dup 应仍是启用，得到 %+v", a.skills)
	}
}

// TestCorruptRootsListKeepsDefaultUsable 验证 config.json 损坏时应用仍可用：
// 只显示默认目录并报告错误；之后添加目录会覆盖掉损坏的文件。
func TestCorruptRootsListKeepsDefaultUsable(t *testing.T) {
	_, cfg := newTestApp(t)
	seedSkill(t, cfg, "base", nil)
	if err := os.MkdirAll(cfg.Home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.ConfigPath(), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newAppFor(cfg)
	dir := makeRoot(t, "projects", "one")
	a.pickFolder = func() (string, error) { return dir, nil }
	tt := showApp(a, 1280, 800)

	if a.libErr == "" || len(a.lib.Roots) != 1 {
		t.Fatalf("损坏的 config.json 应只剩默认目录并记录错误：libErr=%q Roots=%v", a.libErr, a.lib.Roots)
	}
	if !tt.HasText("目录列表读取失败") || !tt.HasText("base 开关") {
		t.Errorf("应报告读取失败且默认目录仍可用，文本 %v", tt.Texts())
	}

	click(t, a, tt, "添加文件夹")
	if a.libErr != "" || a.cfg.Root != dir {
		t.Errorf("添加目录后应恢复：libErr=%q cfg.Root=%s status=%q", a.libErr, a.cfg.Root, a.status)
	}
	if lib, err := store.LoadLibrary(cfg); err != nil || len(lib.Roots) != 2 {
		t.Errorf("config.json 应被新列表覆盖：Roots=%v err=%v", lib.Roots, err)
	}
}

// TestMissingExtraRootShowsBannerAndCanBeRemoved 验证已添加的目录后来在磁盘
// 上消失时：显示提示，但仍可以从列表里移除。
func TestMissingExtraRootShowsBannerAndCanBeRemoved(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "base", nil)
	other := makeRoot(t, "extra", "gone-soon")
	if err := a.lib.AddRoot(other); err != nil {
		t.Fatal(err)
	}
	tt := showApp(a, 1280, 800)
	if err := os.RemoveAll(other); err != nil {
		t.Fatal(err)
	}

	click(t, a, tt, rootLabel(other))
	if !tt.HasText("技能目录不存在") {
		t.Errorf("目录消失后应提示，文本 %v", tt.Texts())
	}
	click(t, a, tt, "从列表移除此目录")
	if a.cfg.Root != cfg.Root || tt.HasText("技能目录不存在") {
		t.Errorf("移除后应回到默认目录且提示消失：cfg.Root=%s 文本 %v", a.cfg.Root, tt.Texts())
	}
}
