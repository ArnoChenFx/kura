package cli

// 链接形式的技能（Windows 联接点 / 目录符号链接）在命令行里的完整流程：list 列出、断链不显示不报错、
// doctor 的各处检查（根、disabled、trash 的时间戳层）、停用 / 启用 / 移入回收站 / 恢复，
// 并且每一步之后链接指向的目标目录都不能被破坏。
//
// 测试隔离：全部目录都在 t.TempDir() 里，链接目标也在其中。

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
)

// isLink 报告 p 本身（不跟随）是不是符号链接或联接点。
func isLink(t *testing.T, p string) bool {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatalf("Lstat %s: %v", p, err)
	}
	return fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0
}

// brokenLinkIn 在 dir 下建一个断链 name：指向一个刚被删掉的目录。
func brokenLinkIn(t *testing.T, mk linkMaker, dir, name string) {
	t.Helper()
	gone := filepath.Join(t.TempDir(), "gone-"+name)
	if err := os.MkdirAll(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mk(filepath.Join(dir, name), gone)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
}

// listNames 运行 list --json，返回「名字 -> 状态」。
func listNames(t *testing.T, e *cliEnv) map[string]string {
	t.Helper()
	code, out := e.runJSON(t, "list")
	if code != ExitOK {
		t.Fatalf("list 应退出 0，得到 %d，stderr=%s", code, e.stderr.String())
	}
	got := map[string]string{}
	for _, raw := range out["skills"].([]any) {
		s := raw.(map[string]any)
		got[s["name"].(string)] = s["state"].(string)
	}
	return got
}

// TestListShowsLinkedSkillsAndSkipsBroken 验证 list 把链接技能当技能列出，断链不显示、也不报错。
func TestListShowsLinkedSkillsAndSkipsBroken(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		e := newEnv(t)
		e.installWithMeta(t, "plain")
		e.linkedSkill(t, mk, "linked")
		brokenLinkIn(t, mk, e.root, "broken")

		got := listNames(t, e)
		if len(got) != 2 || got["plain"] != "enabled" || got["linked"] != "enabled" {
			t.Errorf("list 应只列出 plain 与 linked，得到 %v", got)
		}
	})
}

// TestDoctorIgnoresBrokenLinks 验证断链按「目录不存在」处理：根、disabled、trash 三处都不被
// doctor 报成「普通文件」，退出码为 0。
func TestDoctorIgnoresBrokenLinks(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		e := newEnv(t)
		e.installWithMeta(t, "plain")
		brokenLinkIn(t, mk, e.root, "broken-root")
		brokenLinkIn(t, mk, filepath.Join(e.home, "disabled"), "broken-disabled")
		brokenLinkIn(t, mk, filepath.Join(e.home, "trash"), "broken-trash")

		code, out := e.runJSON(t, "doctor")
		got := problemKinds(t, out)
		if code != ExitOK || len(got) != 0 {
			t.Errorf("断链不应被报告：退出码 %d，问题 %v", code, got)
		}
	})
}

// TestDoctorHashDriftThroughLink 验证 doctor 的哈希核对穿过链接读真实内容：目标目录里的内容变了，
// 链接技能就该被报告漂移。
func TestDoctorHashDriftThroughLink(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		e := newEnv(t)
		real := e.linkedSkill(t, mk, "linked")
		if code, _ := e.runJSON(t, "doctor"); code != ExitOK {
			t.Fatalf("改动前应是干净的，退出码 %d", code)
		}
		if err := os.WriteFile(filepath.Join(real, "SKILL.md"), []byte("# drifted"), 0o644); err != nil {
			t.Fatal(err)
		}
		code, out := e.runJSON(t, "doctor")
		got := problemKinds(t, out)
		if code != ExitDoctorProblem || !got["hash-drift/linked"] {
			t.Errorf("目标内容变了应报告 hash-drift/linked：退出码 %d，问题 %v", code, got)
		}
	})
}

// TestLinkedSkillLifecycleViaCLI 走一遍命令行里的完整流程：停用（doctor 报 disabled 残留）→ 启用 →
// 移入回收站（doctor 认得时间戳层的链接，报回收站残留）→ 恢复。每一步之后目标目录都不变，
// 回收站里是链接本身，快照是真实目录。
func TestLinkedSkillLifecycleViaCLI(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		e := newEnv(t)
		real := e.linkedSkill(t, mk, "linked")
		want, err := store.HashDir(real)
		if err != nil {
			t.Fatal(err)
		}
		intact := func(step string) {
			t.Helper()
			if got, err := store.HashDir(real); err != nil || got != want {
				t.Fatalf("%s之后目标目录的内容被改了：%s %v", step, got, err)
			}
		}

		// 停用：链接被移到 disabled/，doctor 只报告残留提醒。
		if code, _ := e.runJSON(t, "disable", "linked"); code != ExitOK {
			t.Fatalf("disable 应成功：%s", e.stderr.String())
		}
		if !isLink(t, filepath.Join(e.home, "disabled", "linked")) {
			t.Error("停用后 disabled 里应是链接本身")
		}
		intact("停用")
		if got := listNames(t, e); got["linked"] != "disabled" {
			t.Errorf("停用后 list 应显示 disabled：%v", got)
		}
		_, out := e.runJSON(t, "doctor")
		if got := problemKinds(t, out); len(got) != 1 || !got["leftover/linked"] {
			t.Errorf("停用的链接技能应只有一条残留提醒：%v", got)
		}

		// 启用。
		if code, _ := e.runJSON(t, "enable", "linked"); code != ExitOK {
			t.Fatalf("enable 应成功：%s", e.stderr.String())
		}
		if !isLink(t, filepath.Join(e.root, "linked")) {
			t.Error("启用后根里应是链接本身")
		}
		intact("启用")

		// 移入回收站：回收站里是链接，快照是真实目录且内容一致。
		code, out := e.runJSON(t, "remove", "linked", "--stamp", "20260101-000000")
		if code != ExitOK {
			t.Fatalf("remove 应成功：%s", e.stderr.String())
		}
		if !isLink(t, out["dir"].(string)) {
			t.Error("回收站里应是链接本身")
		}
		snap := out["snapshotDir"].(string)
		if isLink(t, snap) {
			t.Error("快照应是真实目录，不能只是一个链接")
		}
		if got, err := store.HashDir(snap); err != nil || got != want {
			t.Errorf("快照内容应与目标一致：%s %v", got, err)
		}
		intact("移入回收站")
		_, out = e.runJSON(t, "doctor")
		if got := problemKinds(t, out); len(got) != 1 || !got["leftover/linked"] {
			t.Errorf("回收站里的链接应被识别、报一条残留提醒：%v", got)
		}

		// 恢复：不给时间戳，取最新一份。
		if code, _ := e.runJSON(t, "restore", "linked"); code != ExitOK {
			t.Fatalf("restore 应成功：%s", e.stderr.String())
		}
		if !isLink(t, filepath.Join(e.root, "linked")) {
			t.Error("恢复后根里应是链接本身")
		}
		intact("恢复")
		if code, out := e.runJSON(t, "doctor"); code != ExitOK {
			t.Errorf("恢复后应干净：退出码 %d，%v", code, problemKinds(t, out))
		}
	})
}
