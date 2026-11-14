package store

// 链接形式的技能上的各项操作：启用 / 停用 / 移入回收站 / 恢复 / 永久删除 / 更新 / 历史快照恢复 /
// 本地安装，以及跨卷移动。共同的要求：移动的是链接本身；快照是穿透复制的真实内容；任何操作之后
// 链接指向的目标目录都不能被破坏（Purge 之后它仍在）。
//
// 测试隔离：所有目录与链接都在 t.TempDir() 里；跨卷用例只用自己建、结束即删的临时目录。

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ArnoChenFx/kura/internal/i18n"
)

// assertTargetIntact 断言链接的目标目录原封不动：哈希与 want 一致。
func assertTargetIntact(t *testing.T, target, want string) {
	t.Helper()
	if got := mustHash(t, target); got != want {
		t.Errorf("目标目录 %s 的内容被改动了：哈希 %s，想要 %s", target, got, want)
	}
}

// assertRealDir 断言 p 是一个真实目录（不是链接，也不是文件）。
func assertRealDir(t *testing.T, p string) {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatalf("Lstat %s: %v", p, err)
	}
	if isLinkMode(fi.Mode()) || !fi.IsDir() {
		t.Errorf("%s 应是真实目录，实际类型位 %v", p, fi.Mode())
	}
}

// TestDisableEnableLinkedSkill 验证停用 / 启用移动的是链接本身：链接换位置、仍是链接，目标目录不动，
// 扫描随之在根与 disabled 之间切换。
func TestDisableEnableLinkedSkill(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		c := newTestConfig(t)
		target := linkedSkillSetup(t, c, mk, "demo", map[string]string{"refs/a.md": "A"})
		want := mustHash(t, target)

		res, err := c.Disable("demo")
		if err != nil {
			t.Fatalf("Disable: %v", err)
		}
		if !isLinkPath(t, res.Dir) {
			t.Errorf("停用后 disabled 里应仍是链接：%s", res.Dir)
		}
		if _, err := os.Lstat(c.SkillPath("demo")); !os.IsNotExist(err) {
			t.Errorf("根目录里的链接应已移走，err=%v", err)
		}
		assertTargetIntact(t, target, want)
		enabled, _ := c.Scan()
		disabled, _ := c.ScanDisabled()
		if len(enabled) != 0 || len(disabled) != 1 || disabled[0].Name != "demo" || !disabled[0].HasSKILLMD {
			t.Errorf("停用后应只在 disabled 里扫到它：enabled=%v disabled=%v", skillNamesOf(enabled), skillNamesOf(disabled))
		}

		res, err = c.Enable("demo")
		if err != nil {
			t.Fatalf("Enable: %v", err)
		}
		if !isLinkPath(t, res.Dir) || res.Dir != c.SkillPath("demo") {
			t.Errorf("启用后根里应是链接：%s", res.Dir)
		}
		assertTargetIntact(t, target, want)
		if enabled, _ = c.Scan(); len(enabled) != 1 || enabled[0].Name != "demo" {
			t.Errorf("启用后应能扫到它：%v", skillNamesOf(enabled))
		}
	})
}

// TestRemoveLinkedSkillWritesRealSnapshot 验证移入回收站：回收站里是链接本身（移动，不是复制内容）；
// 快照是穿透复制的真实目录，内容与目标一致；目标目录不动。
func TestRemoveLinkedSkillWritesRealSnapshot(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		c := newTestConfig(t)
		target := linkedSkillSetup(t, c, mk, "demo", map[string]string{"refs/a.md": "A", "scripts/run.sh": "echo"})
		want := mustHash(t, target)

		res, err := c.Remove("demo")
		if err != nil {
			t.Fatalf("Remove: %v", err)
		}
		if !isLinkPath(t, res.Dir) {
			t.Errorf("回收站里应是链接本身：%s", res.Dir)
		}
		assertRealDir(t, res.SnapshotDir)
		if got := mustHash(t, res.SnapshotDir); got != want {
			t.Errorf("快照应是目标内容的真实副本：哈希 %s，想要 %s", got, want)
		}
		if _, err := os.Stat(filepath.Join(res.SnapshotDir, "refs", "a.md")); err != nil {
			t.Errorf("快照里应有穿透复制出来的文件：%v", err)
		}
		assertTargetIntact(t, target, want)
		if _, err := os.Lstat(c.SkillPath("demo")); !os.IsNotExist(err) {
			t.Errorf("根目录里的链接应已移走，err=%v", err)
		}
	})
}

// TestRestoreLinkedSkillFromTrash 验证恢复：不给时间戳时取最新一份——回收站里时间戳层是链接，
// latestSubdir 必须认得；恢复后根里又是链接，内容可读。
func TestRestoreLinkedSkillFromTrash(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		c := newTestConfig(t)
		target := linkedSkillSetup(t, c, mk, "demo", nil)
		want := mustHash(t, target)
		if _, err := c.Remove("demo"); err != nil {
			t.Fatal(err)
		}

		res, err := c.Restore("demo", "")
		if err != nil {
			t.Fatalf("Restore: %v", err)
		}
		if !isLinkPath(t, res.Dir) || res.Dir != c.SkillPath("demo") {
			t.Errorf("恢复后根里应是链接：%s", res.Dir)
		}
		assertTargetIntact(t, target, want)
		if got := mustHash(t, c.SkillPath("demo")); got != want {
			t.Errorf("恢复后通过链接读到的内容应与目标一致")
		}
		skills, _ := c.Scan()
		if len(skills) != 1 || !skills[0].HasSKILLMD {
			t.Errorf("恢复后应能扫到它：%v", skillNamesOf(skills))
		}
		if _, err := os.Stat(filepath.Join(c.TrashRoot(), "demo")); !os.IsNotExist(err) {
			t.Errorf("恢复后回收站里的空壳应被清掉，err=%v", err)
		}
	})
}

// TestPurgeLinkedSkillKeepsTarget 验证永久删除只删链接本身：目标目录仍在、内容不变；写下的快照
// 是真实内容且与目标一致。这是「绝不能穿透删到目标」的核心用例。
func TestPurgeLinkedSkillKeepsTarget(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		c := newTestConfig(t)
		target := linkedSkillSetup(t, c, mk, "demo", map[string]string{"refs/a.md": "A"})
		want := mustHash(t, target)
		if _, err := c.Remove("demo"); err != nil {
			t.Fatal(err)
		}

		res, err := c.Purge("demo")
		if err != nil {
			t.Fatalf("Purge: %v", err)
		}
		if _, err := os.Stat(filepath.Join(c.TrashRoot(), "demo")); !os.IsNotExist(err) {
			t.Errorf("回收站里的 demo 应已删除，err=%v", err)
		}
		// 核心：目标目录与里面的文件都还在。
		if _, err := os.Stat(filepath.Join(target, "SKILL.md")); err != nil {
			t.Fatalf("Purge 把目标目录里的文件删了：%v", err)
		}
		assertTargetIntact(t, target, want)
		if res.SnapshotDir == "" {
			t.Fatal("Purge 应返回快照目录")
		}
		assertRealDir(t, res.SnapshotDir)
		if got := mustHash(t, res.SnapshotDir); got != want {
			t.Errorf("Purge 前的快照应与目标一致：%s vs %s", got, want)
		}
	})
}

// TestPurgeDanglingLinkInTrash 验证回收站里的链接后来变成断链（目标被别的程序删了）：Purge 不崩、不报错，
// 把断链摘掉；没有内容可快照，也就没有快照。
func TestPurgeDanglingLinkInTrash(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		c := newTestConfig(t)
		target := linkedSkillSetup(t, c, mk, "demo", nil)
		if _, err := c.Remove("demo"); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(target); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Purge("demo"); err != nil {
			t.Fatalf("Purge 断链不应报错：%v", err)
		}
		if _, err := os.Lstat(filepath.Join(c.TrashRoot(), "demo")); !os.IsNotExist(err) {
			t.Errorf("回收站里的 demo 应已删除，err=%v", err)
		}
	})
}

// TestRemoveDisabledLinkedSkill 验证先停用再移入回收站：停用区里的链接同样被快照、被移动。
func TestRemoveDisabledLinkedSkill(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		c := newTestConfig(t)
		target := linkedSkillSetup(t, c, mk, "demo", nil)
		want := mustHash(t, target)
		if _, err := c.Disable("demo"); err != nil {
			t.Fatal(err)
		}
		res, err := c.RemoveDisabled("demo")
		if err != nil {
			t.Fatalf("RemoveDisabled: %v", err)
		}
		if !isLinkPath(t, res.Dir) {
			t.Errorf("回收站里应是链接：%s", res.Dir)
		}
		assertRealDir(t, res.SnapshotDir)
		assertTargetIntact(t, target, want)
	})
}

// localSourceFixture 造一个来源是本地目录的链接技能：target 是真实目录（v1 内容，含 old.txt 与元数据），
// src 是本地来源目录（v2 内容，含 new.txt）。
func localSourceFixture(t *testing.T, c *Config, mk linkMaker) (target, src string) {
	t.Helper()
	target = linkedSkillSetup(t, c, mk, "demo", map[string]string{"old.txt": "old"})
	srcRoot := t.TempDir()
	writeSkill(t, srcRoot, "demo", map[string]string{"new.txt": "new"})
	src = filepath.Join(srcRoot, "demo")
	writeFileT(t, filepath.Join(src, "SKILL.md"), "---\nname: demo\n---\n\n# v2")
	writeLocalMeta(t, target, "demo", src)
	return target, src
}

// writeLocalMeta 给技能目录 dir 写一份来源是本地目录 src 的元数据，contentHash 与当前内容一致。
func writeLocalMeta(t *testing.T, dir, name, src string) {
	t.Helper()
	m := &Meta{
		ID: name, DisplayName: name, Source: Source{Type: SourceLocal, Path: src},
		InstalledAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z", ContentHash: mustHash(t, dir),
	}
	if err := writeMeta(dir, m); err != nil {
		t.Fatal(err)
	}
}

// TestUpdateLinkedSkillActsOnRealDir 验证更新（replaceSkill）作用在链接指向的真实目录上：链接保持不动，
// 目标目录换成新内容（外面的程序仍通过链接看到新内容），元数据写进真实目录，旧内容留有真实快照，
// 不在真实目录旁留下暂存目录。
func TestUpdateLinkedSkillActsOnRealDir(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		c := newTestConfig(t)
		target, src := localSourceFixture(t, c, mk)
		oldHash := mustHash(t, target)
		link := c.SkillPath("demo")
		linkTarget, _ := os.Readlink(link)

		res, err := c.Update("demo")
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if !isLinkPath(t, link) {
			t.Fatal("更新后根里的链接应保持不动")
		}
		if got, _ := os.Readlink(link); got != linkTarget {
			t.Errorf("链接的目标被改了：%q -> %q", linkTarget, got)
		}
		if res.Dir != link {
			t.Errorf("Result.Dir = %s，想要链接路径 %s", res.Dir, link)
		}
		if got, want := mustHash(t, target), mustHash(t, src); got != want {
			t.Errorf("目标目录应换成来源的新内容：%s vs %s", got, want)
		}
		if got, _ := os.ReadFile(filepath.Join(link, "new.txt")); string(got) != "new" {
			t.Errorf("通过链接应看到新文件：%q", got)
		}
		if _, err := os.Stat(filepath.Join(target, "old.txt")); !os.IsNotExist(err) {
			t.Errorf("旧文件应已被替换掉，err=%v", err)
		}
		// 元数据写进了真实目录，哈希与新内容一致。
		m := readMeta(target)
		if m.Err != nil || !m.Found || m.Meta.ContentHash != mustHash(t, target) || m.Meta.UpdatedAt == "2026-01-01T00:00:00Z" {
			t.Errorf("真实目录里的元数据应已刷新：%+v err=%v", m.Meta, m.Err)
		}
		// 快照是旧内容的真实副本。
		assertRealDir(t, res.SnapshotDir)
		if got := mustHash(t, res.SnapshotDir); got != oldHash {
			t.Errorf("快照应是更新前的内容：%s vs %s", got, oldHash)
		}
		if _, err := os.Stat(filepath.Join(res.SnapshotDir, "old.txt")); err != nil {
			t.Errorf("快照里应有旧文件：%v", err)
		}
		// 暂存目录建在真实目录旁边，用完不能留下。
		if _, err := os.Lstat(target + ".kura-new"); !os.IsNotExist(err) {
			t.Errorf("真实目录旁不应留下暂存目录，err=%v", err)
		}
	})
}

// TestUpdateRelativeSymlinkKeepsLink 验证相对目标的符号链接：更新必须解析到真实目录，而不是把
// 链接本身当成目录替换掉。（只有符号链接能有相对目标。）
func TestUpdateRelativeSymlinkKeepsLink(t *testing.T) {
	c := newTestConfig(t)
	outside := t.TempDir()
	writeSkill(t, outside, "demo", map[string]string{"old.txt": "old"})
	target := filepath.Join(outside, "demo")
	mkdirAll(t, c.Root)
	rel, err := filepath.Rel(c.Root, target)
	if err != nil {
		t.Skipf("无法算出相对路径（不同卷）: %v", err)
	}
	if err := os.Symlink(rel, c.SkillPath("demo")); err != nil {
		t.Skipf("当前环境不能创建符号链接: %v", err)
	}
	srcRoot := t.TempDir()
	writeSkill(t, srcRoot, "demo", map[string]string{"new.txt": "new"})
	src := filepath.Join(srcRoot, "demo")
	writeLocalMeta(t, target, "demo", src)

	if _, err := c.Update("demo"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !isLinkPath(t, c.SkillPath("demo")) {
		t.Fatal("相对符号链接被替换成了真目录")
	}
	if got, _ := os.Readlink(c.SkillPath("demo")); got != rel {
		t.Errorf("链接的目标被改了：%q -> %q", rel, got)
	}
	if _, err := os.Stat(filepath.Join(target, "new.txt")); err != nil {
		t.Errorf("真实目录应有新文件：%v", err)
	}
}

// TestUpdateLinkedSkillFailureRestoresFromSnapshot 验证「失败从快照恢复」对真实目录同样成立：
// 用被占用（不允许删除）的文件让删除旧内容失败，更新必须返回错误，目录内容回到更新前，链接不动，
// 不留暂存目录。占用文件只有 Windows 上才能做到（Go 打开文件时不带 FILE_SHARE_DELETE），其他平台跳过。
func TestUpdateLinkedSkillFailureRestoresFromSnapshot(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("用被占用的文件模拟删除失败，仅 Windows 可行")
	}
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		c := newTestConfig(t)
		target, _ := localSourceFixture(t, c, mk)
		want := mustHash(t, target)
		locked, err := os.Open(filepath.Join(target, "old.txt"))
		if err != nil {
			t.Fatal(err)
		}
		defer locked.Close()

		if _, err := c.Update("demo"); err == nil {
			t.Fatal("旧内容删不掉时更新应当失败")
		}
		locked.Close()
		if !isLinkPath(t, c.SkillPath("demo")) {
			t.Error("失败后链接应保持不动")
		}
		assertTargetIntact(t, target, want)
		if _, err := os.Stat(filepath.Join(target, "new.txt")); !os.IsNotExist(err) {
			t.Errorf("失败后不应出现新文件，err=%v", err)
		}
		if _, err := os.Lstat(target + ".kura-new"); !os.IsNotExist(err) {
			t.Errorf("恢复成功后不应留下暂存目录，err=%v", err)
		}
	})
}

// TestUpdateRefusesUnsafeLinkTarget 验证链接的目标把 kura 自己的技能根目录包含在内（链接指回了上级目录）时
// 拒绝更新：替换会先删光目标，会连带毁掉整个技能目录。什么都不能被删。
func TestUpdateRefusesUnsafeLinkTarget(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		base := t.TempDir()
		c, err := NewConfig(filepath.Join(base, "skills"), filepath.Join(t.TempDir(), "kura"))
		if err != nil {
			t.Fatal(err)
		}
		writeSkill(t, c.Root, "bystander", nil) // 一个不相干的技能，更新出事时它也会被毁
		src := filepath.Join(t.TempDir(), "src")
		writeFileT(t, filepath.Join(src, "SKILL.md"), "# src")
		// 链接 demo 指向 base —— base 里就有 skills 根目录本身。
		mk(c.SkillPath("demo"), base)
		writeLocalMeta(t, base, "demo", src)

		_, err = c.Update("demo")
		if err == nil {
			t.Fatal("目标包含技能根目录时应拒绝更新")
		}
		if !strings.Contains(err.Error(), base) {
			t.Errorf("错误信息应说明目标路径：%v", err)
		}
		if _, err := os.Stat(filepath.Join(c.Root, "bystander", "SKILL.md")); err != nil {
			t.Fatalf("拒绝更新后技能根目录里的内容必须原样保留：%v", err)
		}
		if _, err := os.Stat(filepath.Join(base, MetaFileName)); err != nil {
			t.Errorf("目标里的文件必须原样保留：%v", err)
		}
		if !isLinkPath(t, c.SkillPath("demo")) {
			t.Error("链接本身也必须保留")
		}
		if _, err := os.Stat(base + ".kura-new"); !os.IsNotExist(err) {
			t.Errorf("拒绝后不应留下暂存目录，err=%v", err)
		}
	})
}

// TestPreviewUpdateLinkedSkill 验证检查更新（只读预览）对链接技能同样可用：旧内容一侧穿过链接读取，
// 本地来源是链接目录时新内容一侧也能读。
func TestPreviewUpdateLinkedSkill(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		c := newTestConfig(t)
		target, src := localSourceFixture(t, c, mk)
		before := mustHash(t, target)

		// 本地来源本身也换成链接目录。
		srcLink := filepath.Join(t.TempDir(), "src-link")
		mk(srcLink, src)
		writeLocalMeta(t, target, "demo", srcLink)

		p, err := c.PreviewUpdate("demo")
		if err != nil {
			t.Fatalf("PreviewUpdate: %v", err)
		}
		added, changed, removed := p.Counts()
		if added != 1 || changed != 1 || removed != 1 {
			t.Errorf("应是新增 new.txt、变更 SKILL.md、删除 old.txt：added=%d changed=%d removed=%d files=%+v", added, changed, removed, p.Files)
		}
		if p.OldHash != before || p.LocalModified {
			t.Errorf("旧哈希应是当前内容、且没被改过：old=%s want=%s modified=%v", p.OldHash, before, p.LocalModified)
		}
		if got := mustHash(t, src); p.NewHash != got {
			t.Errorf("新哈希应是来源内容的哈希：%s vs %s", p.NewHash, got)
		}
		assertTargetIntact(t, target, before) // 只读：什么都不能动
	})
}

// TestAddLocalFromLinkedDirectory 验证从链接目录安装本地技能：装进根目录的是穿透复制的真实目录，
// 来源路径如实记录为链接路径。
func TestAddLocalFromLinkedDirectory(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		c := newTestConfig(t)
		realSrc := filepath.Join(t.TempDir(), "demo")
		writeSkill(t, filepath.Dir(realSrc), "demo", map[string]string{"refs/a.md": "A"})
		srcLink := filepath.Join(t.TempDir(), "demo")
		mk(srcLink, realSrc)

		res, err := c.Add(&AddOptions{LocalPath: srcLink})
		if err != nil {
			t.Fatalf("Add: %v", err)
		}
		assertRealDir(t, res.Dir)
		if _, err := os.Stat(filepath.Join(res.Dir, "refs", "a.md")); err != nil {
			t.Errorf("应复制出链接目录里的内容：%v", err)
		}
		m := readMeta(res.Dir)
		if m.Err != nil || !m.Found || m.Meta.Source.Path != srcLink {
			t.Errorf("元数据应如实记录来源路径 %s：%+v err=%v", srcLink, m.Meta, m.Err)
		}
	})
}

// TestMoveDirLinkSameVolume 验证同卷移动：os.Rename 移动的是链接本身，结果仍是链接，目标不动。
func TestMoveDirLinkSameVolume(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		base := t.TempDir()
		target := filepath.Join(base, "target")
		writeFileT(t, filepath.Join(target, "SKILL.md"), "# t")
		want := mustHash(t, target)
		src := filepath.Join(base, "a")
		mk(src, target)
		dst := filepath.Join(base, "b")

		if err := moveDir(src, dst); err != nil {
			t.Fatal(err)
		}
		if !isLinkPath(t, dst) {
			t.Error("移动后应仍是链接")
		}
		if _, err := os.Lstat(src); !os.IsNotExist(err) {
			t.Errorf("源链接应已移走，err=%v", err)
		}
		assertTargetIntact(t, target, want)
	})
}

// twoVolumeDirs 在两个不同的卷上各建一个本次测试独占的临时目录。找不到第二个可写卷（非 Windows、
// 只挂一块盘）时跳过，不算失败。
func twoVolumeDirs(t *testing.T) (a, b string) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("跨卷用例只在 Windows 上能稳定构造")
	}
	for _, vol := range []string{`C:\`, `D:\`, `E:\`} {
		dir := writableTempOnVolume(t, vol)
		if dir == "" {
			continue
		}
		cleanup := dir
		t.Cleanup(func() { os.RemoveAll(cleanup) })
		if a == "" {
			a = dir
		} else if !sameVolume(a, dir) && b == "" {
			b = dir
		}
	}
	if a == "" || b == "" {
		t.Skipf("找不到两个可写卷（a=%q b=%q）", a, b)
	}
	return a, b
}

// TestMoveLinkAcrossVolumes 验证跨卷移动链接不会被悄悄展开成真目录（那会把「移动」变成「复制内容」）：
//   - 联接点：标准库造不出来，返回明确错误，源链接与目标原样保留，目的地什么都不留；
//   - 符号链接：在目的地用同一个目标重建，再摘掉源链接，目标目录不动。
func TestMoveLinkAcrossVolumes(t *testing.T) {
	volA, volB := twoVolumeDirs(t)
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		kind := strings.ReplaceAll(t.Name(), "/", "_")
		target := filepath.Join(volA, "target-"+kind)
		writeFileT(t, filepath.Join(target, "SKILL.md"), "# t")
		t.Cleanup(func() { os.RemoveAll(target) })
		want := mustHash(t, target)
		srcDir, dstDir := filepath.Join(volA, "src-"+kind), filepath.Join(volB, "dst-"+kind)
		mkdirAll(t, srcDir)
		mkdirAll(t, dstDir)
		t.Cleanup(func() { os.Remove(filepath.Join(srcDir, "demo")); os.RemoveAll(srcDir) })
		t.Cleanup(func() { os.Remove(filepath.Join(dstDir, "demo")); os.RemoveAll(dstDir) })
		src, dst := filepath.Join(srcDir, "demo"), filepath.Join(dstDir, "demo")
		mk(src, target)

		err := moveDir(src, dst)
		assertTargetIntact(t, target, want)
		if strings.HasSuffix(kind, "junction") {
			if !errors.Is(err, i18n.NewError("storeErrLinkCrossVolume")) {
				t.Fatalf("联接点跨卷应返回明确错误，得到 %v", err)
			}
			if !isLinkPath(t, src) {
				t.Error("失败后源链接必须原样保留")
			}
			if _, err := os.Lstat(dst); !os.IsNotExist(err) {
				t.Errorf("失败后目的地不应留下任何东西，err=%v", err)
			}
			if _, err := os.Lstat(dst + moveTmpSuffix); !os.IsNotExist(err) {
				t.Errorf("不应留下跨卷暂存目录，err=%v", err)
			}
			return
		}
		if err != nil {
			t.Fatalf("符号链接跨卷应在目的地重建：%v", err)
		}
		if !isLinkPath(t, dst) {
			t.Error("目的地应是符号链接，而不是展开后的真目录")
		}
		sameDirT(t, dst, target)
		if _, err := os.Lstat(src); !os.IsNotExist(err) {
			t.Errorf("源链接应已摘掉，err=%v", err)
		}
	})
}

// TestDisableLinkedSkillAcrossVolumes 把上面的约束放进真实的停用流程：技能根目录与 kura 数据目录在不同卷，
// 链接形式的技能停用时不能被展开复制成真目录——联接点干脆停用失败（技能留在原处）。
func TestDisableLinkedSkillAcrossVolumes(t *testing.T) {
	volA, volB := twoVolumeDirs(t)
	t.Run("junction", func(t *testing.T) {
		c, err := NewConfig(filepath.Join(volA, "root-j"), filepath.Join(volB, "home-j"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Remove(c.SkillPath("demo")); os.RemoveAll(c.Root); os.RemoveAll(c.Home) })
		target := linkedSkillSetup(t, c, func(link, target string) { makeJunction(t, link, target) }, "demo", nil)
		want := mustHash(t, target)

		if _, err := c.Disable("demo"); err == nil {
			t.Fatal("联接点跨卷停用应当失败，而不是悄悄复制内容")
		}
		if !isLinkPath(t, c.SkillPath("demo")) {
			t.Error("停用失败后根里的链接必须原样保留")
		}
		if _, err := os.Lstat(c.DisabledPath("demo")); !os.IsNotExist(err) {
			t.Errorf("disabled 里不应留下任何东西，err=%v", err)
		}
		assertTargetIntact(t, target, want)
	})
}

// TestSnapshotHistoryOnLinkedSkill 验证历史快照的预览与恢复对链接形式的当前技能可用：恢复作用在真实
// 目录上，链接保持不动；恢复前的当前版本以真实内容保存；不在真实目录旁留下暂存目录。
func TestSnapshotHistoryOnLinkedSkill(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		c, stamp := snapshotFixture(t)
		target := filepath.Join(t.TempDir(), "alpha")
		if err := os.Rename(c.SkillPath("alpha"), target); err != nil {
			t.Fatal(err)
		}
		mk(c.SkillPath("alpha"), target)
		currentHash := mustHash(t, target)

		p, err := c.PreviewSnapshot("alpha", StateEnabled, stamp)
		if err != nil {
			t.Fatalf("PreviewSnapshot 对链接技能应可用：%v", err)
		}
		if !p.HasChanges() {
			t.Fatal("应发现历史内容差异")
		}
		res, err := c.RestoreSnapshot(p)
		if err != nil {
			t.Fatalf("RestoreSnapshot: %v", err)
		}
		if !isLinkPath(t, c.SkillPath("alpha")) {
			t.Fatal("恢复后链接应保持不动")
		}
		if res.Dir != c.SkillPath("alpha") {
			t.Errorf("Result.Dir = %s，想要链接路径", res.Dir)
		}
		if got := snapshotRead(t, filepath.Join(target, "notes.txt")); got != "historic\nkeep\n" {
			t.Errorf("真实目录应恢复成历史内容：%q", got)
		}
		assertRealDir(t, res.SnapshotDir)
		if got := mustHash(t, res.SnapshotDir); got != currentHash {
			t.Errorf("恢复前的当前版本应以真实内容保存：%s vs %s", got, currentHash)
		}
		left, _ := os.ReadDir(filepath.Dir(target))
		for _, e := range left {
			if e.Name() != "alpha" {
				t.Errorf("真实目录旁不应留下暂存目录：%s", e.Name())
			}
		}
	})
}
