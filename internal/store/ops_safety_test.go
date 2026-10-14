package store

// 破坏性操作的安全性（PR #4 评审缺陷 2、3）：技能名必须是单一目录名，
// 永久删除前必须确实有一份与回收站副本一致的有效快照。

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestValidName 验证名字校验的边界。
func TestValidName(t *testing.T) {
	for _, ok := range []string{"alpha", "my-skill", "a.b", "技能", "x_1"} {
		if !ValidName(ok) {
			t.Errorf("%q 应是合法名字", ok)
		}
	}
	for _, bad := range []string{"", ".", "..", "../x", "a/b", `a\b`, "..\\x", "a..b", "/abs"} {
		if ValidName(bad) {
			t.Errorf("%q 应是非法名字", bad)
		}
	}
}

// TestInvalidNameRejectedAndOutsideUntouched 验证：所有按名字操作的入口都对
// 非法名字返回 ErrInvalidName（带 store: 前缀与非法名字），且回收站之外的
// 目录原样存在。修复前 Purge("../outside") 会把它整个删掉。
func TestInvalidNameRejectedAndOutsideUntouched(t *testing.T) {
	base, _ := newLibraryFixture(t)
	// <home>/outside 与 <home>/trash 同级：Purge("../outside") 会指到它。
	outside := filepath.Join(base.Home, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(marker, []byte("must survive"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Remove("../x") 要找的 <root>/../x 也造出来。
	sibling := filepath.Join(filepath.Dir(base.Root), "x")
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}

	calls := map[string]func(name string) error{
		"Enable":  func(n string) error { _, err := base.Enable(n); return err },
		"Disable": func(n string) error { _, err := base.Disable(n); return err },
		"Remove":  func(n string) error { _, err := base.Remove(n); return err },
		"Restore": func(n string) error { _, err := base.Restore(n, ""); return err },
		"Purge":   func(n string) error { _, err := base.Purge(n); return err },
		"RemoveWithStamp": func(n string) error {
			_, err := base.RemoveWithStamp(n, "20240101-000000")
			return err
		},
	}
	for op, call := range calls {
		for _, name := range []string{"../outside", "../x", "a/b", ".."} {
			err := call(name)
			if !errors.Is(err, ErrInvalidName) {
				t.Errorf("%s(%q) = %v，想要 ErrInvalidName", op, name, err)
				continue
			}
			if msg := err.Error(); len(msg) < 7 || msg[:7] != "store: " {
				t.Errorf("%s(%q) 的错误信息应以 store:  开头，得到 %q", op, name, msg)
			}
		}
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "must survive" {
		t.Errorf("回收站之外的目录必须原样存在：%v", err)
	}
	if !fileExists(sibling) {
		t.Error("根目录之外的同级目录必须原样存在")
	}
}

// TestRestoreRejectsTraversalStamp 验证 Restore 的时间戳目录名同样不能越界。
func TestRestoreRejectsTraversalStamp(t *testing.T) {
	base, _ := newLibraryFixture(t)
	writeSkill(t, base.Root, "gone", nil)
	if _, err := base.Remove("gone"); err != nil {
		t.Fatal(err)
	}
	if _, err := base.Restore("gone", "../../.."); !errors.Is(err, ErrInvalidName) {
		t.Errorf("越界的时间戳应返回 ErrInvalidName，得到 %v", err)
	}
}

// TestLegalNamesStillWork 验证合法名字的完整生命周期不受校验影响。
func TestLegalNamesStillWork(t *testing.T) {
	base, _ := newLibraryFixture(t)
	writeSkill(t, base.Root, "my-skill.v2", map[string]string{"a.md": "x"})
	if _, err := base.Disable("my-skill.v2"); err != nil {
		t.Fatal(err)
	}
	if _, err := base.Enable("my-skill.v2"); err != nil {
		t.Fatal(err)
	}
	if _, err := base.Remove("my-skill.v2"); err != nil {
		t.Fatal(err)
	}
	if _, err := base.Restore("my-skill.v2", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := base.Remove("my-skill.v2"); err != nil {
		t.Fatal(err)
	}
	if _, err := base.Purge("my-skill.v2"); err != nil {
		t.Fatal(err)
	}
}

// trashedWithSnapshot 造一个已移入回收站、带原始快照的技能，返回时间戳目录名、
// 回收站副本路径与快照路径。
func trashedWithSnapshot(t *testing.T, base *Config, name string) (stampName, trashCopy, snap string) {
	t.Helper()
	writeSkill(t, base.Root, name, map[string]string{"notes.md": "precious", "sub/deep.txt": "deep"})
	if _, err := base.Remove(name); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(base.TrashPath(name))
	if err != nil || len(entries) != 1 {
		t.Fatalf("回收站副本异常: %v %v", entries, err)
	}
	stampName = entries[0].Name()
	return stampName, filepath.Join(base.TrashPath(name), stampName), base.SnapshotPath(name, stampName)
}

// TestPurgeRewritesEmptiedSnapshot 验证变体 B：快照目录还在但被清空，不能当作
// 已快照；应重新写一份，内容在快照里，然后才删。
func TestPurgeRewritesEmptiedSnapshot(t *testing.T) {
	base, _ := newLibraryFixture(t)
	_, _, snap := trashedWithSnapshot(t, base, "emptied")
	if err := os.RemoveAll(snap); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(snap, 0o755); err != nil { // 目录在，里面空了
		t.Fatal(err)
	}

	res, err := base.Purge("emptied")
	if err != nil {
		t.Fatal(err)
	}
	if fileExists(base.TrashPath("emptied")) {
		t.Error("快照重写成功后应删除回收站副本")
	}
	for _, rel := range []string{"notes.md", filepath.Join("sub", "deep.txt")} {
		if !fileExists(filepath.Join(snap, rel)) {
			t.Errorf("被清空的快照应被重新写入，缺 %s", rel)
		}
	}
	if res.SnapshotDir != snap {
		t.Errorf("SnapshotDir = %q，想要 %q", res.SnapshotDir, snap)
	}
}

// TestPurgeRejectsSymlinkSnapshot 验证变体 A：快照位置是指向别处的符号链接，
// 不能当作快照：报错、不写穿链接、不删回收站副本。
func TestPurgeRejectsSymlinkSnapshot(t *testing.T) {
	base, _ := newLibraryFixture(t)
	_, trashCopy, snap := trashedWithSnapshot(t, base, "linked")
	elsewhere := t.TempDir()
	if err := os.RemoveAll(snap); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, snap); err != nil {
		t.Skipf("当前环境不能建符号链接（Windows 需要开发者模式）: %v", err)
	}

	if _, err := base.Purge("linked"); err == nil {
		t.Fatal("快照位置是符号链接时应返回错误")
	}
	if !fileExists(filepath.Join(trashCopy, "notes.md")) {
		t.Error("快照无效时回收站副本必须保留")
	}
	if entries, _ := os.ReadDir(elsewhere); len(entries) != 0 {
		t.Errorf("不能写穿符号链接，链接目标里出现了 %v", entries)
	}
}

// TestPurgeSnapshotMismatchWritesNewOne 验证：快照目录非空但内容与回收站副本
// 对不上时，不能拿旧快照顶替：重新写一份（旧的原样保留，快照只增不删）。
func TestPurgeSnapshotMismatchWritesNewOne(t *testing.T) {
	base, _ := newLibraryFixture(t)
	stampName, _, snap := trashedWithSnapshot(t, base, "stale")
	if err := os.WriteFile(filepath.Join(snap, "notes.md"), []byte("an older revision"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := base.Purge("stale")
	if err != nil {
		t.Fatal(err)
	}
	if fileExists(base.TrashPath("stale")) {
		t.Error("快照写好后应删除回收站副本")
	}
	if data, _ := os.ReadFile(filepath.Join(snap, "notes.md")); string(data) != "an older revision" {
		t.Errorf("旧快照不能被改写，得到 %q", data)
	}
	if res.SnapshotDir == snap || filepath.Dir(res.SnapshotDir) != filepath.Dir(snap) {
		t.Fatalf("应写到同级的新目录，SnapshotDir = %q（旧快照 %q）", res.SnapshotDir, snap)
	}
	if data, err := os.ReadFile(filepath.Join(res.SnapshotDir, "notes.md")); err != nil || string(data) != "precious" {
		t.Errorf("新快照应含被删技能的内容，得到 %q, %v（stamp %s）", data, err, stampName)
	}
}

// TestPurgeNormalPathKeepsContentInSnapshot 验证正常路径：沿用 Remove 时写下的
// 快照，里面确实有被删技能的内容；回收站副本删除。
func TestPurgeNormalPathKeepsContentInSnapshot(t *testing.T) {
	base, _ := newLibraryFixture(t)
	_, _, snap := trashedWithSnapshot(t, base, "fine")
	res, err := base.Purge("fine")
	if err != nil {
		t.Fatal(err)
	}
	if res.SnapshotDir != snap {
		t.Errorf("应沿用 Remove 写下的快照 %q，得到 %q", snap, res.SnapshotDir)
	}
	if data, err := os.ReadFile(filepath.Join(snap, "notes.md")); err != nil || string(data) != "precious" {
		t.Errorf("快照里应有被删技能的内容，得到 %q, %v", data, err)
	}
	if fileExists(base.TrashPath("fine")) {
		t.Error("回收站副本应已删除")
	}
}

// TestWithinResolvesBothSides 验证 within 的越界判断。
func TestWithinResolvesBothSides(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "a")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	if !within(root, inside) {
		t.Error("子目录应在根之内")
	}
	if within(root, root) {
		t.Error("根自身不算在根之内")
	}
	if within(root, filepath.Join(root, "..", "elsewhere")) {
		t.Error("越出根的路径不应在根之内")
	}
}
