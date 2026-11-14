package store

// 链接目录的判定与解析：isSkillDirEntry / realDir / DirExists / removeEntry。
// （共用的小工具也在这里；链接的创建方式见 links_test.go 的 forEachLinkKind。）
//
// 测试隔离：所有目录与链接都在 t.TempDir() 里。

import (
	"os"
	"path/filepath"
	"testing"
)

// mkdirAll 建目录（含父级），失败即终止。
func mkdirAll(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

// writeFileT 写文件（含父目录），失败即终止。
func writeFileT(t *testing.T, p, content string) {
	t.Helper()
	mkdirAll(t, filepath.Dir(p))
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// isLinkPath 报告 p 本身（不跟随）是不是链接：符号链接或联接点。
func isLinkPath(t *testing.T, p string) bool {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatalf("Lstat %s: %v", p, err)
	}
	return isLinkMode(fi.Mode())
}

// sameDirT 断言 a 与 b 是同一个目录：用 os.SameFile 比，不受大小写、短路径名的影响。
func sameDirT(t *testing.T, a, b string) {
	t.Helper()
	fa, err := os.Stat(a)
	if err != nil {
		t.Fatalf("Stat %s: %v", a, err)
	}
	fb, err := os.Stat(b)
	if err != nil {
		t.Fatalf("Stat %s: %v", b, err)
	}
	if !os.SameFile(fa, fb) {
		t.Errorf("%s 与 %s 不是同一个目录", a, b)
	}
}

// linkedSkillSetup 在 c.Root 里建一个链接形式的技能：真实目录在 c.Root 之外，返回真实目录。
func linkedSkillSetup(t *testing.T, c *Config, mk linkMaker, name string, extra map[string]string) string {
	t.Helper()
	outside := t.TempDir()
	writeSkill(t, outside, name, extra)
	mkdirAll(t, c.Root)
	mk(c.SkillPath(name), filepath.Join(outside, name))
	return filepath.Join(outside, name)
}

// mustHash 取目录哈希，失败即终止。
func mustHash(t *testing.T, dir string) string {
	t.Helper()
	h, err := HashDir(dir)
	if err != nil {
		t.Fatalf("HashDir(%s): %v", dir, err)
	}
	return h
}

// skillNamesOf 取技能名，用于报错信息。
func skillNamesOf(skills []*Skill) []string {
	var out []string
	for _, s := range skills {
		out = append(out, s.Name)
	}
	return out
}

// brokenLink 在 dir 下建一个指向「刚被删掉的目录」的链接 name，返回链接路径。
func brokenLink(t *testing.T, mk linkMaker, dir, name string) string {
	t.Helper()
	gone := filepath.Join(t.TempDir(), "gone-"+name)
	mkdirAll(t, gone)
	mkdirAll(t, dir)
	link := filepath.Join(dir, name)
	mk(link, gone)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	return link
}

// TestIsSkillDirEntry 覆盖统一判定的每个分支：普通目录、普通文件、指向目录的链接、断链，
// 以及（仅符号链接）指向文件的链接。导出版与断链判定同口径。
func TestIsSkillDirEntry(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		root, outside := t.TempDir(), t.TempDir()
		mkdirAll(t, filepath.Join(root, "real-dir"))
		writeFileT(t, filepath.Join(root, "plain-file"), "x")
		mkdirAll(t, filepath.Join(outside, "target"))
		mk(filepath.Join(root, "link-dir"), filepath.Join(outside, "target"))
		brokenLink(t, mk, root, "broken")
		want := map[string]bool{"real-dir": true, "plain-file": false, "link-dir": true, "broken": false}
		// 指向文件的链接只有符号链接能建；建不了就不测这一项。
		writeFileT(t, filepath.Join(outside, "a-file"), "x")
		if os.Symlink(filepath.Join(outside, "a-file"), filepath.Join(root, "file-link")) == nil {
			want["file-link"] = false
		}

		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}
		seen := 0
		for _, e := range entries {
			w, ok := want[e.Name()]
			if !ok {
				continue
			}
			seen++
			if got := isSkillDirEntry(root, e); got != w {
				t.Errorf("isSkillDirEntry(%s) = %v，想要 %v（类型位 %v）", e.Name(), got, w, e.Type())
			}
			if IsSkillDirEntry(root, e) != isSkillDirEntry(root, e) {
				t.Errorf("导出版 IsSkillDirEntry(%s) 与内部判定不一致", e.Name())
			}
			if broken := IsBrokenLinkEntry(root, e); broken != (e.Name() == "broken") {
				t.Errorf("IsBrokenLinkEntry(%s) = %v", e.Name(), broken)
			}
		}
		if seen != len(want) {
			t.Errorf("目录里应有 %d 个被测条目，实际遇到 %d 个", len(want), seen)
		}
	})
}

// TestScanSkipsBrokenLink 验证断链按「目录不存在」处理：Scan / CountEnabled / ScanDisabled /
// ScanAll 都不列出它，也不报错。
func TestScanSkipsBrokenLink(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		c := newTestConfig(t)
		writeSkill(t, c.Root, "ok", nil)
		brokenLink(t, mk, c.Root, "broken")
		brokenLink(t, mk, c.DisabledRoot(), "broken")

		got, err := c.Scan()
		if err != nil || len(got) != 1 || got[0].Name != "ok" {
			t.Fatalf("Scan() = %v, err %v，想要只有 ok", skillNamesOf(got), err)
		}
		if n := c.CountEnabled(); n != 1 {
			t.Errorf("CountEnabled() = %d，想要 1", n)
		}
		dis, err := c.ScanDisabled()
		if err != nil || len(dis) != 0 {
			t.Errorf("ScanDisabled() = %v, err %v，想要空", skillNamesOf(dis), err)
		}
		all, err := c.ScanAll()
		if err != nil || len(all) != 1 {
			t.Errorf("ScanAll() = %v, err %v，想要只有 ok", skillNamesOf(all), err)
		}
	})
}

// TestRealDir 覆盖解析：普通目录原样返回；链接解析到目标；链接指向链接也能解析到底；断链与
// 指向文件的链接解析失败。
func TestRealDir(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		base := t.TempDir()
		target := filepath.Join(base, "target")
		plain := filepath.Join(base, "plain")
		mkdirAll(t, target)
		mkdirAll(t, plain)
		link := filepath.Join(base, "link")
		mk(link, target)
		chain := filepath.Join(base, "chain")
		mk(chain, link) // 链接指向链接

		if got, ok := realDir(plain); !ok || got != plain {
			t.Errorf("普通目录应原样返回：%q %v", got, ok)
		}
		for _, p := range []string{link, chain} {
			got, ok := realDir(p)
			if !ok {
				t.Errorf("realDir(%s) 应成功", filepath.Base(p))
				continue
			}
			sameDirT(t, got, target)
			if isLinkPath(t, got) {
				t.Errorf("realDir(%s) 返回的仍是链接：%s", filepath.Base(p), got)
			}
		}

		if _, ok := realDir(brokenLink(t, mk, base, "broken")); ok {
			t.Error("断链不应解析成功")
		}
		if _, ok := realDir(filepath.Join(base, "missing")); ok {
			t.Error("不存在的路径不应解析成功")
		}
		writeFileT(t, filepath.Join(base, "a-file"), "x")
		if os.Symlink(filepath.Join(base, "a-file"), filepath.Join(base, "file-link")) == nil {
			if _, ok := realDir(filepath.Join(base, "file-link")); ok {
				t.Error("指向文件的链接不是目录，不应解析成功")
			}
		}
	})
}

// TestRealDirResolvesRelativeSymlink 验证相对目标的符号链接按链接所在目录换算：更新技能时要靠它
// 找到真实目录，而不是把链接本身当目录替换掉。
func TestRealDirResolvesRelativeSymlink(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	mkdirAll(t, target)
	mkdirAll(t, filepath.Join(base, "sub"))
	link := filepath.Join(base, "sub", "rel-link")
	if err := os.Symlink(filepath.Join("..", "target"), link); err != nil {
		t.Skipf("当前环境不能创建符号链接: %v", err)
	}
	got, ok := realDir(link)
	if !ok {
		t.Fatal("相对符号链接应解析成功")
	}
	sameDirT(t, got, target)
}

// TestDirExists 覆盖界面用来过滤左栏的存在性判断：普通目录、链接目录算存在；不存在、文件、断链不算。
func TestDirExists(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		base := t.TempDir()
		real := filepath.Join(base, "real")
		mkdirAll(t, real)
		link := filepath.Join(base, "link")
		mk(link, real)
		writeFileT(t, filepath.Join(base, "file"), "x")
		broken := brokenLink(t, mk, base, "broken")
		for p, want := range map[string]bool{
			real: true, link: true,
			filepath.Join(base, "file"): false, broken: false, filepath.Join(base, "missing"): false,
		} {
			if got := DirExists(p); got != want {
				t.Errorf("DirExists(%s) = %v，想要 %v", filepath.Base(p), got, want)
			}
		}
	})
}

// TestRemoveEntryNeverFollowsLinks 验证 removeEntry：链接只摘链接，目标原封不动；含链接的目录整个
// 删除时同样不穿透；路径不存在不算错误。
func TestRemoveEntryNeverFollowsLinks(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		base := t.TempDir()
		target := filepath.Join(base, "target")
		writeFileT(t, filepath.Join(target, "keep.txt"), "keep")
		link := filepath.Join(base, "link")
		mk(link, target)
		if err := removeEntry(link); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(link); !os.IsNotExist(err) {
			t.Errorf("链接应已被摘掉，err=%v", err)
		}
		if got, err := os.ReadFile(filepath.Join(target, "keep.txt")); err != nil || string(got) != "keep" {
			t.Fatalf("目标目录里的文件不能受影响：%q %v", got, err)
		}

		// 含链接的容器目录整个删除（Purge 里 trash/<name> 的情形）。
		container := filepath.Join(base, "container")
		mkdirAll(t, container)
		mk(filepath.Join(container, "stamp"), target)
		if err := removeEntry(container); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(container); !os.IsNotExist(err) {
			t.Errorf("容器目录应已删除，err=%v", err)
		}
		if got, err := os.ReadFile(filepath.Join(target, "keep.txt")); err != nil || string(got) != "keep" {
			t.Fatalf("删除含链接的容器不能穿透到目标：%q %v", got, err)
		}
		if err := removeEntry(filepath.Join(base, "missing")); err != nil {
			t.Errorf("路径不存在不算错误：%v", err)
		}
	})
}
