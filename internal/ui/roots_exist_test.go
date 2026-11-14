package ui

// 左栏只显示「当前存在且是目录」的技能目录，以及链接形式的技能在窗口里的呈现。
//
// 存在与否不在每帧里现查（每帧 Stat 十几次太贵），而是随扫描一起在后台判定、缓存在
// App.rootExists 里（见 App.setRootExists / rootAvailable）。这些用例都走真实的扫描流程：
// 先在磁盘上删掉或建出目录，再点「刷新」，看左栏跟着变。
//
// 测试隔离：全部目录与链接都在 t.TempDir() 里，绝不触碰真实的 ~/.agents/skills 与 ~/.kura。

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ArnoChenFx/kura/locales"
)

// linkMaker 在 link 处建一个指向目录 target 的链接。
type linkMaker func(link, target string)

// forEachLinkKind 对每一种可用的目录链接各跑一遍 fn：Windows 上是联接点（mklink /J，不需要
// 管理员权限）与目录符号链接，其余平台只有符号链接；建不了的种类会被跳过。
func forEachLinkKind(t *testing.T, fn func(t *testing.T, mk linkMaker)) {
	t.Helper()
	t.Run("junction", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("联接点只存在于 Windows")
		}
		fn(t, func(link, target string) {
			if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
				t.Fatalf("mklink /J 失败: %v\n%s", err, out)
			}
		})
	})
	t.Run("symlink", func(t *testing.T) {
		if err := os.Symlink(t.TempDir(), filepath.Join(t.TempDir(), "probe")); err != nil {
			t.Skipf("当前环境不能创建符号链接: %v", err)
		}
		fn(t, func(link, target string) {
			if err := os.Symlink(target, link); err != nil {
				t.Fatalf("符号链接创建失败: %v", err)
			}
		})
	})
}

// TestRailHidesRootsThatDoNotExist 验证左栏只列出存在的目录：目录在磁盘上消失后刷新一次，
// 它的条目与计数徽标一起消失；目录回来后再刷新，条目与计数又出现。
func TestRailHidesRootsThatDoNotExist(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "base", nil)
	other := makeRoot(t, "extra", "x1", "x2")
	if err := a.lib.AddRoot(other); err != nil {
		t.Fatal(err)
	}
	tt := showApp(a, 1280, 800)
	for _, root := range []string{cfg.Root, other} {
		if _, ok := tt.Find(rootLabel(root)); !ok {
			t.Fatalf("起初左栏应列出 %s，文本 %v", rootLabel(root), tt.Texts())
		}
	}
	if a.rootCounts[other] != 2 {
		t.Fatalf("起初 %s 的计数应是 2：%v", rootLabel(other), a.rootCounts)
	}

	// 用 Rename 而不是 RemoveAll：稍后要把目录原样放回来。
	parked := other + ".parked"
	if err := os.Rename(other, parked); err != nil {
		t.Fatal(err)
	}
	click(t, a, tt, "刷新")
	if _, ok := tt.Find(rootLabel(other)); ok {
		t.Errorf("目录消失后左栏不应再列出它，文本 %v", tt.Texts())
	}
	if _, ok := tt.Find(rootLabel(cfg.Root)); !ok {
		t.Errorf("存在的默认目录仍应列出，文本 %v", tt.Texts())
	}
	if _, ok := a.rootCounts[other]; ok {
		t.Errorf("不存在的目录不应留下计数徽标：%v", a.rootCounts)
	}
	if tt.HasText(locales.UiRootsNoneAvailable()) {
		t.Errorf("还有目录可用时不应出现「没有可用目录」的提示，文本 %v", tt.Texts())
	}

	if err := os.Rename(parked, other); err != nil {
		t.Fatal(err)
	}
	click(t, a, tt, "刷新")
	if _, ok := tt.Find(rootLabel(other)); !ok {
		t.Errorf("目录回来并刷新后左栏应再列出它，文本 %v", tt.Texts())
	}
	if a.rootCounts[other] != 2 {
		t.Errorf("目录回来后计数应恢复为 2：%v", a.rootCounts)
	}
}

// TestSelectedRootThatVanishedFallsBackToFirstExisting 验证选中的目录消失后切到列表里第一个存在的
// 目录，默认目录优先；默认目录也不在时，退到列表里下一个存在的。
func TestSelectedRootThatVanishedFallsBackToFirstExisting(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "base", nil)
	first := makeRoot(t, "first", "in-first")
	second := makeRoot(t, "second", "in-second")
	for _, r := range []string{first, second} {
		if err := a.lib.AddRoot(r); err != nil {
			t.Fatal(err)
		}
	}
	tt := showApp(a, 1280, 800)

	// 选中最后一个目录，删掉它：回到默认目录（列表里排第一）。
	click(t, a, tt, rootLabel(second))
	if err := os.RemoveAll(second); err != nil {
		t.Fatal(err)
	}
	click(t, a, tt, "刷新")
	if a.rootSel != cfg.Root || a.cfg.Root != cfg.Root {
		t.Fatalf("选中目录消失后应回到默认目录：rootSel=%s cfg.Root=%s", a.rootSel, a.cfg.Root)
	}

	// 现在默认目录也消失，而正在看的就是它：退到列表里下一个存在的目录 first。
	if err := os.RemoveAll(cfg.Root); err != nil {
		t.Fatal(err)
	}
	click(t, a, tt, "刷新")
	if a.rootSel != first || a.cfg.Root != first {
		t.Fatalf("默认目录也消失后应退到下一个存在的目录：rootSel=%s cfg.Root=%s 想要 %s", a.rootSel, a.cfg.Root, first)
	}
	if !tt.HasText("in-first 开关") {
		t.Errorf("应显示 first 目录里的技能，文本 %v", tt.Texts())
	}
	for _, gone := range []string{cfg.Root, second} {
		if _, ok := tt.Find(rootLabel(gone)); ok {
			t.Errorf("已消失的 %s 不应出现在左栏，文本 %v", rootLabel(gone), tt.Texts())
		}
	}
}

// TestAllRootsMissingShowsHintAndStaysUsable 验证一个目录都不存在时：给一句提示，保留现状（选中项
// 不变），界面照常渲染（添加文件夹等入口仍在），不崩。
func TestAllRootsMissingShowsHintAndStaysUsable(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "base", nil)
	other := makeRoot(t, "extra", "x1")
	if err := a.lib.AddRoot(other); err != nil {
		t.Fatal(err)
	}
	tt := showApp(a, 1280, 800)
	click(t, a, tt, rootLabel(other))
	// 选中目录的名字在「左栏条目 + 中栏头部」里出现若干次；左栏条目消失后只剩头部那一处。
	selectedBefore := countText(tt, rootLabel(other))

	for _, r := range []string{cfg.Root, other} {
		if err := os.RemoveAll(r); err != nil {
			t.Fatal(err)
		}
	}
	click(t, a, tt, "刷新")
	if !tt.HasText(locales.UiRootsNoneAvailable()) {
		t.Errorf("没有可用目录时左栏应给提示，文本 %v", tt.Texts())
	}
	if n := countText(tt, rootLabel(cfg.Root)); n != 0 {
		t.Errorf("没被选中的缺失目录 %s 不应出现在界面上（出现 %d 次），文本 %v", rootLabel(cfg.Root), n, tt.Texts())
	}
	if n := countText(tt, rootLabel(other)); n >= selectedBefore || n == 0 {
		t.Errorf("被选中的缺失目录只该剩中栏头部那一处：之前 %d 次，现在 %d 次，文本 %v", selectedBefore, n, tt.Texts())
	}
	if a.rootSel != other || a.cfg.Root != other {
		t.Errorf("没有地方可切时应保留现状：rootSel=%s cfg.Root=%s", a.rootSel, a.cfg.Root)
	}
	if _, ok := tt.Find("添加文件夹"); !ok {
		t.Errorf("「添加文件夹」入口应仍可用，文本 %v", tt.Texts())
	}
	// 再来一次刷新也不能崩。
	click(t, a, tt, "刷新")

	// 有一个目录回来了：提示消失，它出现在左栏。
	if err := os.MkdirAll(cfg.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	click(t, a, tt, "刷新")
	if tt.HasText(locales.UiRootsNoneAvailable()) {
		t.Errorf("目录回来后提示应消失，文本 %v", tt.Texts())
	}
	if _, ok := tt.Find(rootLabel(cfg.Root)); !ok {
		t.Errorf("回来的默认目录应出现在左栏，文本 %v", tt.Texts())
	}
	if a.rootSel != cfg.Root {
		t.Errorf("选中的目录消失后又有目录回来，应切过去：rootSel=%s", a.rootSel)
	}
}

// TestRootMissingAtStartupIsHidden 验证启动时 config.json 里记着的目录已经不存在：第一次扫描之后
// 左栏就不显示它，其余目录照常。
func TestRootMissingAtStartupIsHidden(t *testing.T) {
	_, cfg := newTestApp(t)
	seedSkill(t, cfg, "base", nil)
	ghost := filepath.Join(t.TempDir(), "ghost-skills") // 从未建出来
	real := makeRoot(t, "real", "r1")
	if err := os.MkdirAll(cfg.Home, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"roots": []string{ghost, real}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.ConfigPath(), data, 0o644); err != nil {
		t.Fatal(err)
	}

	a := newAppFor(cfg)
	if len(a.lib.Roots) != 3 {
		t.Fatalf("前提：config.json 的目录都应载入，Roots=%v", a.lib.Roots)
	}
	tt := showApp(a, 1280, 800)
	if _, ok := tt.Find(rootLabel(ghost)); ok {
		t.Errorf("启动时就不存在的目录不应出现在左栏，文本 %v", tt.Texts())
	}
	for _, r := range []string{cfg.Root, real} {
		if _, ok := tt.Find(rootLabel(r)); !ok {
			t.Errorf("存在的目录 %s 应出现在左栏，文本 %v", rootLabel(r), tt.Texts())
		}
	}
}

// TestLinkedSkillsAppearInTheWindow 验证窗口里链接形式的技能（联接点、目录符号链接）和普通技能一样
// 显示、计数；断链不显示。这是「10 个真目录 + 3 个联接点显示 13 个」的缩小版。
func TestLinkedSkillsAppearInTheWindow(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		a, cfg := newTestApp(t)
		seedSkill(t, cfg, "plain-a", nil)
		seedSkill(t, cfg, "plain-b", nil)
		outside := makeRoot(t, "outside", "linked-one", "linked-two", "soon-broken")
		for _, name := range []string{"linked-one", "linked-two", "soon-broken"} {
			mk(filepath.Join(cfg.Root, name), filepath.Join(outside, name))
		}
		// 断链：目标被删掉，链接还在。
		if err := os.RemoveAll(filepath.Join(outside, "soon-broken")); err != nil {
			t.Fatal(err)
		}

		tt := showApp(a, 1280, 800)
		for _, name := range []string{"plain-a", "plain-b", "linked-one", "linked-two"} {
			if !tt.HasText(name + " 开关") {
				t.Errorf("窗口应显示 %s，文本 %v", name, tt.Texts())
			}
		}
		if tt.HasText("soon-broken 开关") {
			t.Errorf("断链不应显示，文本 %v", tt.Texts())
		}
		if got := skillNames(a.skills); len(got) != 4 {
			t.Errorf("应扫到 4 个技能（2 普通 + 2 链接），得到 %v", got)
		}
		if a.rootCounts[cfg.Root] != 4 {
			t.Errorf("左栏徽标应是 4：%v", a.rootCounts)
		}
		// 链接技能能像普通技能一样被选中、看详情。
		click(t, a, tt, "linked-one")
		if a.selName != "linked-one" || !tt.HasText(filepath.Join(cfg.Root, "linked-one")) {
			t.Errorf("选中链接技能后详情应显示它的路径，selName=%q 文本 %v", a.selName, tt.Texts())
		}

		// 摘掉链接（只摘链接本身），收尾。
		for _, name := range []string{"linked-one", "linked-two", "soon-broken"} {
			os.Remove(filepath.Join(cfg.Root, name))
		}
	})
}
