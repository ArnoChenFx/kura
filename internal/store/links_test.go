package store

// 链接目录形式的技能（Windows 联接点 junction / 目录符号链接）的测试。
//
// 背景：CherryStudio 等程序会把技能以联接点的形式建到 ~/.agents/skills 下。Go 在
// Windows 上把联接点的目录项报成 ModeIrregular（既不是 ModeDir 也不是 ModeSymlink），
// filepath.WalkDir 也不会进入它，所以过去它们既扫不到、也无法哈希或复制。
//
// 测试隔离：所有目录都在 t.TempDir() 里，链接目标也在其中，绝不触碰真实的
// ~/.agents/skills 与 ~/.kura。

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// linkMaker 在 link 处建一个指向目录 target 的链接。
type linkMaker func(link, target string)

// makeJunction 建一个 Windows 联接点（mklink /J，不需要管理员权限）。
// 非 Windows 平台没有联接点，调用方应已跳过。
func makeJunction(t *testing.T, link, target string) {
	t.Helper()
	out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Fatalf("mklink /J 失败: %v\n%s", err, out)
	}
}

// makeSymlink 建一个目录符号链接。Windows 上没有权限（未开开发者模式）时返回 false，
// 调用方据此跳过，不算失败。
func makeSymlink(t *testing.T, link, target string) bool {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Logf("无法创建符号链接，跳过：%v", err)
		return false
	}
	return true
}

// forEachLinkKind 对每一种可用的目录链接各跑一遍 fn：Windows 上是联接点与目录符号链接，
// 其余平台只有符号链接。创建不了的种类会被跳过。
func forEachLinkKind(t *testing.T, fn func(t *testing.T, mk linkMaker)) {
	t.Helper()
	t.Run("junction", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("联接点只存在于 Windows")
		}
		fn(t, func(link, target string) { makeJunction(t, link, target) })
	})
	t.Run("symlink", func(t *testing.T) {
		probe := filepath.Join(t.TempDir(), "probe")
		if !makeSymlink(t, probe, t.TempDir()) {
			t.Skip("当前环境不能创建符号链接")
		}
		fn(t, func(link, target string) {
			if !makeSymlink(t, link, target) {
				t.Fatal("符号链接创建失败")
			}
		})
	})
}

// removeLinkOnly 只摘掉链接本身（不穿透到目标），测试收尾时用。
func removeLinkOnly(link string) { os.Remove(link) }

// TestScanLinkOnlyRoot 是任务书要求的最小复现：根目录里只有一个链接形式的技能。
// 修复前 Scan() 返回 0 条（目录项被 e.IsDir() 丢掉），修复后返回 1 条且 HasSKILLMD=true。
func TestScanLinkOnlyRoot(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		c := newTestConfig(t)
		outside := t.TempDir()
		writeSkill(t, outside, "only-linked", nil)
		if err := os.MkdirAll(c.Root, 0o755); err != nil {
			t.Fatal(err)
		}
		mk(filepath.Join(c.Root, "only-linked"), filepath.Join(outside, "only-linked"))

		got, err := c.Scan()
		if err != nil {
			t.Fatal(err)
		}
		hasMD := len(got) > 0 && got[0].HasSKILLMD
		t.Logf("Scan() 返回 %d 条, 第一条 HasSKILLMD=%v", len(got), hasMD)
		if len(got) != 1 || !hasMD {
			t.Fatalf("Scan() 返回 %d 条（HasSKILLMD=%v），想要 1 条且 HasSKILLMD=true", len(got), hasMD)
		}
	})
}

// TestScanFindsLinkedSkills 是复现用例：根目录里有一个普通技能和一个链接形式的技能，
// Scan 与 CountEnabled 必须两个都算。修复前联接点那条被 e.IsDir() 丢掉，只返回 1 条。
func TestScanFindsLinkedSkills(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		c := newTestConfig(t)
		outside := t.TempDir() // 链接目标在技能根目录之外
		writeSkill(t, outside, "linked-demo", map[string]string{"references/guide.md": "guide"})
		writeSkill(t, c.Root, "plain", nil)
		link := filepath.Join(c.Root, "linked-demo")
		mk(link, filepath.Join(outside, "linked-demo"))

		got, err := c.Scan()
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, s := range got {
			names = append(names, s.Name)
		}
		t.Logf("Scan() 返回 %d 条: %v", len(got), names)
		if len(got) != 2 {
			t.Fatalf("Scan() 返回 %d 条，想要 2 条（plain + linked-demo）：%v", len(got), names)
		}
		var linked *Skill
		for _, s := range got {
			if s.Name == "linked-demo" {
				linked = s
			}
		}
		if linked == nil {
			t.Fatalf("没有扫到链接技能 linked-demo：%v", names)
		}
		if !linked.HasSKILLMD {
			t.Errorf("链接技能应能读到 SKILL.md，HasSKILLMD=false")
		}
		if linked.Dir != link {
			t.Errorf("Skill.Dir = %q，想要链接自己的路径 %q", linked.Dir, link)
		}
		if linked.State != StateEnabled {
			t.Errorf("State = %q，想要 enabled", linked.State)
		}
		if n := c.CountEnabled(); n != 2 {
			t.Errorf("CountEnabled() = %d，想要 2", n)
		}
	})
}
