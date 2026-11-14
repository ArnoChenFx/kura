package cli

// 链接目录形式的技能（Windows 联接点 / 目录符号链接）在命令行里的行为：list 要列出，
// doctor 不能把它们误报成「根目录下存在同名普通文件」，断链按「目录不存在」处理。
//
// 测试隔离：全部目录都在 t.TempDir() 里，链接目标也在其中。

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
)

// linkMaker 在 link 处建一个指向目录 target 的链接。
type linkMaker func(link, target string)

// forEachLinkKind 对每一种可用的目录链接各跑一遍 fn：Windows 上是联接点（mklink /J，
// 不需要管理员权限）与目录符号链接，其余平台只有符号链接；建不了的种类会被跳过。
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
		probe := filepath.Join(t.TempDir(), "probe")
		if err := os.Symlink(t.TempDir(), probe); err != nil {
			t.Skipf("当前环境不能创建符号链接: %v", err)
		}
		fn(t, func(link, target string) {
			if err := os.Symlink(target, link); err != nil {
				t.Fatalf("符号链接创建失败: %v", err)
			}
		})
	})
}

// linkedSkill 在 root 之外建一个带完整元数据的真实技能目录，再在 root 里建一个指向它的
// 链接，返回真实目录。元数据里的 contentHash 与内容一致，所以链接技能本身是「干净」的。
func (e *cliEnv) linkedSkill(t *testing.T, mk linkMaker, name string) string {
	t.Helper()
	real := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "SKILL.md"), []byte("---\nname: "+name+"\n---\n\n# "+name), 0o644); err != nil {
		t.Fatal(err)
	}
	hash, err := store.HashDir(real)
	if err != nil {
		t.Fatal(err)
	}
	m := &store.Meta{
		ID: name, DisplayName: name,
		Source:      store.Source{Type: store.SourceLocal, Path: real},
		InstalledAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
		ContentHash: hash,
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(real, store.MetaFileName), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(e.root, 0o755); err != nil {
		t.Fatal(err)
	}
	mk(filepath.Join(e.root, name), real)
	return real
}

// problemKinds 把 doctor 的 JSON 输出整理成「kind/skill」键的集合。
func problemKinds(t *testing.T, out map[string]any) map[string]bool {
	t.Helper()
	got := map[string]bool{}
	probs, _ := out["problems"].([]any)
	for _, raw := range probs {
		p := raw.(map[string]any)
		got[p["kind"].(string)+"/"+p["skill"].(string)] = true
	}
	return got
}

// TestDoctorLinkedSkillIsNotAFile 是 doctor 的复现用例：根目录里的链接技能是正经的技能目录，
// doctor 不能把它报成 not-a-dir（「根目录下存在同名普通文件」）。修复前联接点会被误报。
func TestDoctorLinkedSkillIsNotAFile(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		e := newEnv(t)
		e.installWithMeta(t, "plain")
		e.linkedSkill(t, mk, "linked")

		code, out := e.runJSON(t, "doctor")
		got := problemKinds(t, out)
		t.Logf("doctor 退出码 %d，问题 %v", code, got)
		if got["not-a-dir/linked"] {
			t.Errorf("链接技能被误报成普通文件: %v", got)
		}
		if code != ExitOK || len(got) != 0 {
			t.Errorf("干净的 plain + 链接技能应退出 0 且无问题，得到 %d %v", code, got)
		}
	})
}
