package store

// 回收站里的技能永久删除（Purge）：删除前快照、快照失败不删、删除后不可恢复。

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPurgeDeletesTrashAndKeepsSnapshot(t *testing.T) {
	base, _ := newLibraryFixture(t)
	writeSkill(t, base.Root, "gone", map[string]string{"notes.md": "keep me"})
	if _, err := base.Remove("gone"); err != nil {
		t.Fatal(err)
	}
	if !fileExists(base.TrashPath("gone")) {
		t.Fatal("移入回收站后应有回收站副本")
	}

	res, err := base.Purge("gone")
	if err != nil {
		t.Fatal(err)
	}
	if fileExists(base.TrashPath("gone")) {
		t.Error("永久删除后回收站里不应再有该技能")
	}
	if res.SnapshotDir == "" || !fileExists(filepath.Join(res.SnapshotDir, "notes.md")) {
		t.Errorf("删除前应有快照可找回内容，SnapshotDir=%q", res.SnapshotDir)
	}
	if _, err := base.Restore("gone", ""); !errors.Is(err, ErrUnknownSkill) {
		t.Errorf("永久删除后不能再恢复，得到 %v", err)
	}
	if _, err := base.Purge("gone"); !errors.Is(err, ErrUnknownSkill) {
		t.Errorf("再次删除应返回 ErrUnknownSkill，得到 %v", err)
	}
}

func TestPurgeLeavesEnabledAndDisabledAlone(t *testing.T) {
	base, _ := newLibraryFixture(t)
	writeSkill(t, base.Root, "live", nil)
	writeSkill(t, base.Root, "off", nil)
	if _, err := base.Disable("off"); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, base.Root, "trashed", nil)
	if _, err := base.Remove("trashed"); err != nil {
		t.Fatal(err)
	}
	if _, err := base.Purge("trashed"); err != nil {
		t.Fatal(err)
	}
	if !fileExists(base.SkillPath("live")) || !fileExists(base.DisabledPath("off")) {
		t.Error("永久删除回收站里的技能不应动到在用或停用的技能")
	}
}

// TestPurgeSnapshotFailureKeepsTrash 验证快照位置被文件占用时，删除中止，回收站
// 副本原样保留。
func TestPurgeSnapshotFailureKeepsTrash(t *testing.T) {
	base, _ := newLibraryFixture(t)
	writeSkill(t, base.Root, "keep", map[string]string{"a.md": "x"})
	if _, err := base.Remove("keep"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(base.TrashPath("keep"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("回收站副本异常: %v %v", entries, err)
	}
	// 把 Remove 写下的快照目录换成一个文件：它不是快照，不能被当成已快照。
	snap := base.SnapshotPath("keep", entries[0].Name())
	if err := os.RemoveAll(snap); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snap, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := base.Purge("keep"); err == nil {
		t.Fatal("快照位置被文件占用时应返回错误")
	}
	if !fileExists(filepath.Join(base.TrashPath("keep"), entries[0].Name(), "a.md")) {
		t.Error("快照失败时回收站副本必须保留")
	}
}
