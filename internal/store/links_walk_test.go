package store

// walkTree 与建立在它之上的 HashDir / copyDir：要能进入「根是链接目录」与「树里含链接目录」，
// 链接指回祖先造成的环必须被跳过（不死循环、不爆栈），菱形（两个链接指向同一目录）不是环，
// 不可信来源（git clone 产物）不进入树里的链接目录。
//
// 测试隔离：所有目录与链接都在 t.TempDir() 里。

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// collectRels 用 walkTree 遍历 root，按回调顺序返回每一项的相对路径。
func collectRels(t *testing.T, root string, opt walkOpts) []string {
	t.Helper()
	var rels []string
	if err := walkTree(root, opt, func(e treeEntry) error {
		rels = append(rels, e.Rel)
		return nil
	}); err != nil {
		t.Fatalf("walkTree(%s): %v", root, err)
	}
	return rels
}

// TestHashDirThroughLinkRoot 验证根本身是链接目录时能哈希，结果与它指向的真实目录一致。
// 修复前 filepath.WalkDir 不进入联接点，只能读到 "."，报「读取 . 失败」。
func TestHashDirThroughLinkRoot(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		outside := t.TempDir()
		writeSkill(t, outside, "s", map[string]string{"a.txt": "A", "sub/b.txt": "B"})
		real := filepath.Join(outside, "s")
		link := filepath.Join(t.TempDir(), "s-link")
		mk(link, real)

		want := mustHash(t, real)
		if got := mustHash(t, link); got != want {
			t.Errorf("链接根的哈希 %s 应与真实目录 %s 一致", got, want)
		}
		empty := filepath.Join(t.TempDir(), "empty")
		mkdirAll(t, empty)
		if want == mustHash(t, empty) {
			t.Fatal("前提不成立：真实目录的哈希不该等于空目录的哈希")
		}
	})
}

// linkedSubdirFixture 造两个等价的技能：skillWithLink 里的 refs 是指向共享目录的链接，
// skillMaterialized 里的 refs 是同样内容的真实目录。两者的 SKILL.md 与其他文件完全一致。
func linkedSubdirFixture(t *testing.T, mk linkMaker) (skillWithLink, skillMaterialized string) {
	t.Helper()
	shared := filepath.Join(t.TempDir(), "shared")
	writeFileT(t, filepath.Join(shared, "ref.md"), "ref")
	writeFileT(t, filepath.Join(shared, "deep", "x.md"), "x")

	linkRoot := t.TempDir()
	writeSkill(t, linkRoot, "s", map[string]string{"a.txt": "A"})
	skillWithLink = filepath.Join(linkRoot, "s")
	mk(filepath.Join(skillWithLink, "refs"), shared)

	matRoot := t.TempDir()
	writeSkill(t, matRoot, "s", map[string]string{"a.txt": "A", "refs/ref.md": "ref", "refs/deep/x.md": "x"})
	return skillWithLink, filepath.Join(matRoot, "s")
}

// TestHashDirWithLinkedSubdir 验证树里含链接目录时，哈希与「把链接穿透成真目录」后的哈希一致。
func TestHashDirWithLinkedSubdir(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		withLink, materialized := linkedSubdirFixture(t, mk)
		if got, want := mustHash(t, withLink), mustHash(t, materialized); got != want {
			t.Errorf("含链接子目录的哈希 %s 应等于穿透后的 %s", got, want)
		}
	})
}

// TestCopyDirMaterializesLinks 验证复制把链接穿透成真实内容：根是链接、树里含链接两种情况下，
// 结果里都没有任何链接，内容完整，哈希与源一致。快照靠这一点才有保护意义。
func TestCopyDirMaterializesLinks(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		withLink, _ := linkedSubdirFixture(t, mk)
		want := mustHash(t, withLink)

		// 树里含链接目录。
		dst := filepath.Join(t.TempDir(), "copy")
		if err := copyDir(withLink, dst); err != nil {
			t.Fatal(err)
		}
		assertNoLinks(t, dst)
		if got, err := os.ReadFile(filepath.Join(dst, "refs", "deep", "x.md")); err != nil || string(got) != "x" {
			t.Errorf("链接目录里的文件应被复制成真实内容：%q %v", got, err)
		}
		if got := mustHash(t, dst); got != want {
			t.Errorf("复制结果的哈希 %s 应与源一致 %s", got, want)
		}

		// 根本身是链接目录。
		rootLink := filepath.Join(t.TempDir(), "root-link")
		mk(rootLink, withLink)
		dst2 := filepath.Join(t.TempDir(), "copy2")
		if err := copyDir(rootLink, dst2); err != nil {
			t.Fatal(err)
		}
		assertNoLinks(t, dst2)
		if got := mustHash(t, dst2); got != want {
			t.Errorf("链接根复制结果的哈希 %s 应与源一致 %s", got, want)
		}
	})
}

// assertNoLinks 断言 dir 之下（含 dir 本身）没有任何符号链接或联接点。
func assertNoLinks(t *testing.T, dir string) {
	t.Helper()
	if isLinkPath(t, dir) {
		t.Errorf("%s 本身不应是链接", dir)
	}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if isLinkMode(d.Type()) {
			t.Errorf("复制结果里不应有链接：%s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestWalkTreeSkipsCycles 验证链接指回祖先造成的环被跳过：不死循环、不爆栈，环上的链接不回调
// 也不进入；指向更上层目录的链接可以进入，但上层里再碰到正在遍历的目录时同样被跳过。
// 哈希同样终止，结果等于没有这些链接时的哈希。
func TestWalkTreeSkipsCycles(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		base := t.TempDir()
		writeSkill(t, base, "skill", map[string]string{"a/b.txt": "b"})
		skill := filepath.Join(base, "skill")
		mk(filepath.Join(skill, "loop"), skill)   // 指回自己
		mk(filepath.Join(skill, "a", "up"), base) // 指向更上层，上层里含有 skill 本身

		got := collectRels(t, skill, walkOpts{})
		want := []string{".", "SKILL.md", "a", "a/b.txt", "a/up"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("遍历结果 %v，想要 %v", got, want)
		}

		clean := t.TempDir()
		writeSkill(t, clean, "skill", map[string]string{"a/b.txt": "b"})
		if h, w := mustHash(t, skill), mustHash(t, filepath.Join(clean, "skill")); h != w {
			t.Errorf("带环链接的哈希 %s 应等于没有链接时的 %s", h, w)
		}
		// 复制同样终止，并且不把环物化出来。
		dst := filepath.Join(t.TempDir(), "copy")
		if err := copyDir(skill, dst); err != nil {
			t.Fatal(err)
		}
		if h, w := mustHash(t, dst), mustHash(t, filepath.Join(clean, "skill")); h != w {
			t.Errorf("带环链接的复制结果哈希 %s 应等于 %s", h, w)
		}
	})
}

// TestWalkTreeCycleThroughLinkRoot 验证根本身是链接、且树里又有链接指回这个真实目录时同样不死循环。
func TestWalkTreeCycleThroughLinkRoot(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		real := filepath.Join(t.TempDir(), "real")
		writeFileT(t, filepath.Join(real, "SKILL.md"), "# s")
		rootLink := filepath.Join(t.TempDir(), "root-link")
		mk(rootLink, real)
		mk(filepath.Join(real, "back"), rootLink) // 指回「根链接」，真实路径就是根自己

		got := collectRels(t, rootLink, walkOpts{})
		if want := []string{".", "SKILL.md"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("遍历结果 %v，想要 %v", got, want)
		}
	})
}

// TestWalkTreeDiamondIsNotACycle 验证两个链接指向同一个目录（菱形）不是环：两处都要遍历，
// 否则复制出来的快照会悄悄少一份内容。
func TestWalkTreeDiamondIsNotACycle(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		shared := filepath.Join(t.TempDir(), "shared")
		writeFileT(t, filepath.Join(shared, "f.txt"), "f")
		base := t.TempDir()
		writeSkill(t, base, "skill", nil)
		skill := filepath.Join(base, "skill")
		mk(filepath.Join(skill, "x"), shared)
		mk(filepath.Join(skill, "y"), shared)

		got := collectRels(t, skill, walkOpts{})
		want := []string{".", "SKILL.md", "x", "x/f.txt", "y", "y/f.txt"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("遍历结果 %v，想要 %v", got, want)
		}
		dst := filepath.Join(t.TempDir(), "copy")
		if err := copyDir(skill, dst); err != nil {
			t.Fatal(err)
		}
		for _, rel := range []string{"x/f.txt", "y/f.txt"} {
			if _, err := os.Stat(filepath.Join(dst, filepath.FromSlash(rel))); err != nil {
				t.Errorf("菱形的两条路径都应被复制：%v", err)
			}
		}
	})
}

// TestWalkTreeNoSubLinks 验证 noSubLinks：根自己的链接仍然解析，树里的链接目录当文件类条目、不进入。
func TestWalkTreeNoSubLinks(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		withLink, _ := linkedSubdirFixture(t, mk)
		opt := walkOpts{noSubLinks: true}

		var refsIsDir *bool
		var rels []string
		if err := walkTree(withLink, opt, func(e treeEntry) error {
			rels = append(rels, e.Rel)
			if e.Rel == "refs" {
				v := e.IsDir
				refsIsDir = &v
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if refsIsDir == nil || *refsIsDir {
			t.Errorf("noSubLinks 时 refs 应作为非目录项出现：%v", rels)
		}
		for _, rel := range rels {
			if rel == "refs/ref.md" || rel == "refs/deep" {
				t.Errorf("noSubLinks 时不应进入链接目录：%v", rels)
			}
		}

		// 根自己是链接：照样解析。
		rootLink := filepath.Join(t.TempDir(), "root-link")
		mk(rootLink, withLink)
		if got := collectRels(t, rootLink, opt); len(got) < 3 || got[1] != "SKILL.md" {
			t.Errorf("noSubLinks 仍应解析根自己的链接：%v", got)
		}
	})
}

// TestWalkTreeSkipDirAndSkipAll 验证回调的 SkipDir / SkipAll 约定与 filepath.WalkDir 一致。
func TestWalkTreeSkipDirAndSkipAll(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		withLink, _ := linkedSubdirFixture(t, mk)

		var rels []string
		if err := walkTree(withLink, walkOpts{}, func(e treeEntry) error {
			rels = append(rels, e.Rel)
			if e.Rel == "refs" {
				return filepath.SkipDir
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		for _, rel := range rels {
			if rel == "refs/ref.md" {
				t.Errorf("对链接目录返回 SkipDir 应跳过它的内容：%v", rels)
			}
		}

		count := 0
		if err := walkTree(withLink, walkOpts{}, func(treeEntry) error {
			count++
			return fs.SkipAll
		}); err != nil {
			t.Fatalf("SkipAll 应当正常结束：%v", err)
		}
		if count != 1 {
			t.Errorf("SkipAll 之后不应再回调，回调了 %d 次", count)
		}

		boom := errors.New("boom")
		if err := walkTree(withLink, walkOpts{}, func(treeEntry) error { return boom }); !errors.Is(err, boom) {
			t.Errorf("回调的错误应原样返回，得到 %v", err)
		}
	})
}

// TestWalkTreeRootEdgeCases 覆盖遍历根的几种特殊情况：不存在报错；普通文件与断链只回调一次、非目录。
func TestWalkTreeRootEdgeCases(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		base := t.TempDir()
		err := walkTree(filepath.Join(base, "missing"), walkOpts{}, func(treeEntry) error { return nil })
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("根不存在应返回 ErrNotExist，得到 %v", err)
		}

		writeFileT(t, filepath.Join(base, "file"), "x")
		for name, p := range map[string]string{"普通文件": filepath.Join(base, "file"), "断链": brokenLink(t, mk, base, "broken")} {
			n := 0
			if err := walkTree(p, walkOpts{}, func(e treeEntry) error {
				n++
				if e.IsDir || e.Rel != "." {
					t.Errorf("%s作为根应只回调一次非目录的 \".\"：%+v", name, e)
				}
				return nil
			}); err != nil || n != 1 {
				t.Errorf("%s作为根：回调 %d 次，err %v", name, n, err)
			}
		}
	})
}

// TestCopyTreeUntrustedDoesNotFollowSubLinks 验证不可信来源（git clone 产物）里的链接目录不会被进入：
// 恶意仓库放一个指向别处的链接，不能借复制把那里的内容拖进技能目录；操作以读取失败收场（旧行为）。
// 对照：可信来源（用户自己的目录）同样的结构会被穿透复制。
func TestCopyTreeUntrustedDoesNotFollowSubLinks(t *testing.T) {
	forEachLinkKind(t, func(t *testing.T, mk linkMaker) {
		secret := filepath.Join(t.TempDir(), "secret")
		writeFileT(t, filepath.Join(secret, "secret.txt"), "TOP SECRET")
		base := t.TempDir()
		writeSkill(t, base, "s", nil)
		skill := filepath.Join(base, "s")
		mk(filepath.Join(skill, "evil"), secret)

		dst := filepath.Join(t.TempDir(), "untrusted")
		if err := copyTree(skill, dst, false, true); err == nil {
			t.Error("不可信来源遇到链接目录应当失败，而不是穿透复制")
		}
		filepath.WalkDir(dst, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.Name() == "secret.txt" {
				t.Errorf("不可信来源的链接目录被穿透了：%s", p)
			}
			return nil
		})

		trusted := filepath.Join(t.TempDir(), "trusted")
		if err := copyTree(skill, trusted, false, false); err != nil {
			t.Fatalf("可信来源应穿透复制：%v", err)
		}
		if got, err := os.ReadFile(filepath.Join(trusted, "evil", "secret.txt")); err != nil || string(got) != "TOP SECRET" {
			t.Errorf("可信来源的链接目录应被物化：%q %v", got, err)
		}
	})
}

// TestHashDirFileSymlinkReadsContent 验证指向文件的符号链接保持旧语义：按文件读内容，不会被当成
// 目录；目标文件内容变了，哈希也变。（只有符号链接能指向文件。）
func TestHashDirFileSymlinkReadsContent(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "real.txt")
	writeFileT(t, outside, "payload-1")
	base := t.TempDir()
	writeSkill(t, base, "s", nil)
	skill := filepath.Join(base, "s")
	if err := os.Symlink(outside, filepath.Join(skill, "linked.txt")); err != nil {
		t.Skipf("当前环境不能创建符号链接: %v", err)
	}
	h1 := mustHash(t, skill)

	copyDst := filepath.Join(t.TempDir(), "copy")
	if err := copyDir(skill, copyDst); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(copyDst, "linked.txt")); err != nil || string(got) != "payload-1" {
		t.Errorf("文件符号链接应按文件复制内容：%q %v", got, err)
	}
	if mustHash(t, copyDst) != h1 {
		t.Error("复制结果的哈希应与源一致")
	}
	writeFileT(t, outside, "payload-2")
	if mustHash(t, skill) == h1 {
		t.Error("目标文件内容变了，哈希应当变")
	}
}
