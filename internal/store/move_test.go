package store

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

// volumeOf 返回路径的卷标识：Windows 取盘符（如 "E:"，统一大写），其余平台
// 恒返回 ""（所有路径视为同卷，跨卷测试会据此跳过）。
func volumeOf(path string) string {
	if runtime.GOOS != "windows" {
		return ""
	}
	return strings.ToUpper(filepath.VolumeName(path))
}

// sameVolume 判断两个路径是否在同一卷上。
func sameVolume(a, b string) bool {
	return volumeOf(a) == volumeOf(b)
}

// dirManifest 收集目录下全部文件的「相对路径(/分隔) → 内容」映射，用于比对
// 两个目录树完全一致（含子目录与文件内容）。
func dirManifest(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("遍历 %s 失败: %v", dir, err)
	}
	return files
}

// mustNotExist 断言路径不存在（不存在即通过）。
func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s 应不存在，实际 err=%v", path, err)
	}
}

// writeMoveFixture 建一个含子目录与多个文件的技能目录（模拟真实技能布局）。
func writeMoveFixture(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"SKILL.md":               "---\nname: demo\n---\n\n# demo",
		"references/usage.md":    "# 用法\n\n跨卷移动后内容必须一致。",
		"scripts/run.sh":         "#!/bin/sh\necho hi",
		"deep/nested/level3.txt": "最深一层",
		"empty-dir/.keep":        "",
	}
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestMoveDirSameVolume 覆盖常规路径：同卷移动直接走 os.Rename，成功且不留
// staging 目录。
func TestMoveDirSameVolume(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "skill-a")
	writeMoveFixture(t, src)
	before := dirManifest(t, src)

	dst := filepath.Join(base, "skill-b")
	if err := moveDir(src, dst); err != nil {
		t.Fatalf("moveDir: %v", err)
	}
	mustNotExist(t, src)
	if got := dirManifest(t, dst); len(got) == 0 || !mapsEqualDir(before, got) {
		t.Fatalf("移动后内容不一致: before=%d 项, after=%d 项", len(before), len(got))
	}
	// 不允许留下 .kura-move-tmp 残骸。
	mustNotExist(t, dst+moveTmpSuffix)
}

// mapsEqualDir 比较两个目录清单是否完全一致（键集合与内容都相同）。
func mapsEqualDir(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// TestMoveDirDstExists 覆盖错误路径：目标已存在同名目录时必须报错，且
// 源目录原样保留、目标内容不被覆盖。
func TestMoveDirDstExists(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "skill-a")
	writeMoveFixture(t, src)
	before := dirManifest(t, src)

	dst := filepath.Join(base, "skill-b")
	writeMoveFixture(t, dst)
	dstBefore := dirManifest(t, dst)

	if err := moveDir(src, dst); err == nil {
		t.Fatal("目标已存在时 moveDir 应报错")
	}
	// 源目录必须原样保留。
	if got := dirManifest(t, src); !mapsEqualDir(before, got) {
		t.Fatal("失败的移动不得改动源目录")
	}
	// 目标内容不得被覆盖。
	if got := dirManifest(t, dst); !mapsEqualDir(dstBefore, got) {
		t.Fatal("失败的移动不得改动目标目录")
	}
}

// TestIsCrossDeviceDetectsNotSameDevice 验证 Windows 跨卷错误码的判定：
// WinError 17（ERROR_NOT_SAME_DEVICE）算跨设备，其他错误（如 ACCESS_DENIED）
// 不算。注意 syscall.EXDEV 在 Windows 不是这个含义，绝不能用。
func TestIsCrossDeviceDetectsNotSameDevice(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("ERROR_NOT_SAME_DEVICE 仅在 Windows 上出现")
	}
	err := &os.LinkError{Op: "rename", Old: `C:\a`, New: `E:\b`, Err: syscall.Errno(17)}
	if !isCrossDevice(err) {
		t.Fatal("WinError 17 应被判定为跨设备错误")
	}
	if isCrossDevice(&os.LinkError{Op: "rename", Old: "a", New: "b", Err: syscall.Errno(5)}) {
		t.Fatal("ACCESS_DENIED 不应被判定为跨设备错误")
	}
	if isCrossDevice(os.ErrNotExist) || isCrossDevice(nil) {
		t.Fatal("普通错误与 nil 不应被判定为跨设备错误")
	}
}

// writableTempOnVolume 在卷 volRoot 上创建一个本次测试独占的临时目录；
// 卷不存在、只读或无权限时返回空串。创建的目录由调用方负责清理。
func writableTempOnVolume(t *testing.T, volRoot string) string {
	t.Helper()
	for _, base := range []string{volRoot, filepath.Join(volRoot, "Windows", "Temp"), filepath.Join(volRoot, "Temp")} {
		dir, err := os.MkdirTemp(base, "kura-crossdisk-")
		if err == nil {
			return dir
		}
	}
	return ""
}

// TestDisableEnableAcrossVolumes 是缺陷 1 的真机回归：技能根目录与
// ~/.kura 数据目录在不同卷上时，Disable/Enable（os.Rename 跨卷必失败）
// 仍必须成功，且内容完整（文件数量与内容一致）。
//
// 测试隔离：只使用测试自己创建、结束即删的临时目录，绝不触碰用户真实目录。
// 环境里找不出第二个可写卷（非 Windows、或 CI 只挂一块盘）时跳过，不算失败。
func TestDisableEnableAcrossVolumes(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("跨卷移动是 Windows 专属缺陷（EXDEV 回退），非 Windows 跳过")
	}
	// 在不同卷上各找一个可写临时目录：volA 当技能根目录，volB 当数据目录。
	var volA, volB string
	for _, vol := range []string{`C:\`, `D:\`, `E:\`} {
		dir := writableTempOnVolume(t, vol)
		if dir == "" {
			continue
		}
		cleanupDir := dir
		t.Cleanup(func() { os.RemoveAll(cleanupDir) })
		if volA == "" {
			volA = dir
			continue
		}
		if !sameVolume(volA, dir) {
			volB = dir
		}
	}
	if volA == "" || volB == "" {
		t.Skipf("找不到两个可写卷（volA=%q volB=%q），无法做跨卷回归", volA, volB)
	}
	// 复现用户布局：技能根目录与 kura 数据目录分属两块盘。
	root := filepath.Join(volA, "skills-root")
	home := filepath.Join(volB, "kura-home")
	c, err := NewConfig(root, home)
	if err != nil {
		t.Fatal(err)
	}
	if sameVolume(root, c.DisabledRoot()) {
		t.Fatalf("测试前提不成立：root(%s) 与 disabled(%s) 应在不同卷", root, c.DisabledRoot())
	}

	writeSkill(t, root, "grill-me", map[string]string{
		"references/usage.md":    "# 用法\n\n跨卷移动后内容必须一致。",
		"scripts/run.sh":         "#!/bin/sh\necho hi",
		"deep/nested/level3.txt": "最深一层",
	})
	before := dirManifest(t, c.SkillPath("grill-me"))

	// Disable：E: 卷 → C: 卷（缺陷复现方向）。
	res, err := c.Disable("grill-me")
	if err != nil {
		t.Fatalf("跨卷 Disable 失败: %v", err)
	}
	if got := dirManifest(t, res.Dir); !mapsEqualDir(before, got) {
		t.Fatalf("Disable 后内容不一致: before=%d 项, after=%d 项", len(before), len(got))
	}
	mustNotExist(t, c.SkillPath("grill-me"))
	mustNotExist(t, res.Dir+moveTmpSuffix)

	// Enable：C: 卷 → E: 卷（反向同样要通）。
	res2, err := c.Enable("grill-me")
	if err != nil {
		t.Fatalf("跨卷 Enable 失败: %v", err)
	}
	if got := dirManifest(t, res2.Dir); !mapsEqualDir(before, got) {
		t.Fatalf("Enable 后内容不一致: before=%d 项, after=%d 项", len(before), len(got))
	}
	mustNotExist(t, c.DisabledPath("grill-me"))
	mustNotExist(t, res2.Dir+moveTmpSuffix)
}
