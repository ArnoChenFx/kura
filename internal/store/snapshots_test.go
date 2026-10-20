package store

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func snapshotFixture(t *testing.T) (*Config, string) {
	t.Helper()
	c := newTestConfig(t)
	writeSkill(t, c.Root, "alpha", map[string]string{"notes.txt": "current\nkeep\n"})
	stamp := "20260101-120000"
	writeSkill(t, filepath.Join(c.SnapshotsRoot(), "alpha"), stamp, map[string]string{"notes.txt": "historic\nkeep\n"})
	return c, stamp
}

func snapshotRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSnapshotRestorePreservesCurrentAndOtherCopies(t *testing.T) {
	c, stamp := snapshotFixture(t)
	writeSkill(t, c.DisabledRoot(), "alpha", map[string]string{"notes.txt": "disabled\n"})
	other := *c
	other.Root = filepath.Join(t.TempDir(), "other")
	other.Scope = filepath.Join("roots", "other")
	writeSkill(t, other.Root, "alpha", map[string]string{"notes.txt": "other root\n"})
	before := snapshotRead(t, filepath.Join(c.SkillPath("alpha"), "SKILL.md"))
	p, err := c.PreviewSnapshot("alpha", StateEnabled, stamp)
	if err != nil {
		t.Fatal(err)
	}
	if !p.HasChanges() {
		t.Fatal("应发现历史内容差异")
	}
	items, err := c.ListSnapshots("alpha")
	if err != nil || len(items) != 1 || items[0].Err != nil {
		t.Fatalf("快照列表: %+v %v", items, err)
	}
	if _, err := other.RestoreSnapshot(p); !errors.Is(err, ErrSnapshotChanged) {
		t.Fatalf("其他目录不能使用预览: %v", err)
	}
	r, err := c.RestoreSnapshot(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshotRead(t, filepath.Join(r.Dir, "notes.txt")); got != "historic\nkeep\n" {
		t.Fatalf("恢复内容: %q", got)
	}
	if got := snapshotRead(t, filepath.Join(r.SnapshotDir, "SKILL.md")); got != before {
		t.Fatal("恢复前当前版本未保存")
	}
	if got := snapshotRead(t, filepath.Join(c.DisabledPath("alpha"), "notes.txt")); got != "disabled\n" {
		t.Fatal("误改停用副本")
	}
	if got := snapshotRead(t, filepath.Join(other.SkillPath("alpha"), "notes.txt")); got != "other root\n" {
		t.Fatal("误改另一目录")
	}
	// 新快照可以反向恢复，实现撤销本次恢复；快照目录名称允许同秒多份。
	undo, err := c.PreviewSnapshot("alpha", StateEnabled, filepath.Base(r.SnapshotDir))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.RestoreSnapshot(undo); err != nil {
		t.Fatal(err)
	}
	if got := snapshotRead(t, filepath.Join(c.SkillPath("alpha"), "notes.txt")); got != "current\nkeep\n" {
		t.Fatal("无法恢复刚保存的当前版本")
	}
}

func TestSnapshotDisabledRestoreDoesNotEnable(t *testing.T) {
	c, stamp := snapshotFixture(t)
	writeSkill(t, c.DisabledRoot(), "alpha", map[string]string{"notes.txt": "disabled\n"})
	p, err := c.PreviewSnapshot("alpha", StateDisabled, stamp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.RestoreSnapshot(p); err != nil {
		t.Fatal(err)
	}
	if got := snapshotRead(t, filepath.Join(c.DisabledPath("alpha"), "notes.txt")); got != "historic\nkeep\n" {
		t.Fatal("停用副本未恢复")
	}
	if got := snapshotRead(t, filepath.Join(c.SkillPath("alpha"), "notes.txt")); got != "current\nkeep\n" {
		t.Fatal("误改已启用副本")
	}
}

func TestSnapshotRejectsStalePreviewIncludingMetadata(t *testing.T) {
	for _, which := range []string{"current", "snapshot", "metadata"} {
		t.Run(which, func(t *testing.T) {
			c, stamp := snapshotFixture(t)
			p, err := c.PreviewSnapshot("alpha", StateEnabled, stamp)
			if err != nil {
				t.Fatal(err)
			}
			dir := c.SkillPath("alpha")
			if which == "snapshot" {
				dir = c.SnapshotPath("alpha", stamp)
			}
			path := filepath.Join(dir, "notes.txt")
			text := "changed after preview"
			if which == "metadata" {
				path = filepath.Join(dir, MetaFileName)
				text = `{"metaVersion":1,"id":"alpha","notes":"changed"}`
			}
			if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
			before, _ := hashTreeExcluding(c.SkillPath("alpha"), nil)
			if _, err := c.RestoreSnapshot(p); !errors.Is(err, ErrSnapshotChanged) {
				t.Fatalf("应拒绝过期预览: %v", err)
			}
			after, _ := hashTreeExcluding(c.SkillPath("alpha"), nil)
			if before != after {
				t.Fatal("过期预览改变了当前内容")
			}
		})
	}
}

func TestSnapshotRejectsUnsafeAndIncompleteSources(t *testing.T) {
	for _, kind := range []string{"traversal", "missing-md", "bad-meta", "symlink", "parent-link", "state"} {
		t.Run(kind, func(t *testing.T) {
			c, stamp := snapshotFixture(t)
			dir := c.SnapshotPath("alpha", stamp)
			state := StateEnabled
			switch kind {
			case "traversal":
				stamp = "../../alpha"
			case "missing-md":
				if err := os.Remove(filepath.Join(dir, "SKILL.md")); err != nil {
					t.Fatal(err)
				}
			case "bad-meta":
				if err := os.WriteFile(filepath.Join(dir, MetaFileName), []byte("{broken"), 0o644); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := filepath.Join(t.TempDir(), "secret")
				os.WriteFile(target, []byte("outside"), 0o600)
				if err := os.Symlink(target, filepath.Join(dir, "outside")); err != nil {
					t.Skipf("平台不支持符号链接: %v", err)
				}
			case "parent-link":
				moved := filepath.Join(t.TempDir(), "moved")
				if err := os.Rename(filepath.Dir(dir), moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(moved, filepath.Dir(dir)); err != nil {
					t.Skipf("平台不支持符号链接: %v", err)
				}
			case "state":
				state = StateTrashed
			}
			before, _ := hashTreeExcluding(c.SkillPath("alpha"), nil)
			if _, err := c.PreviewSnapshot("alpha", state, stamp); err == nil {
				t.Fatal("应拒绝不安全或损坏快照")
			}
			after, _ := hashTreeExcluding(c.SkillPath("alpha"), nil)
			if before != after {
				t.Fatal("失败改变当前内容")
			}
		})
	}
}

func TestSnapshotRenameFailureRollsBack(t *testing.T) {
	c, stamp := snapshotFixture(t)
	p, err := c.PreviewSnapshot("alpha", StateEnabled, stamp)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := hashTreeExcluding(c.SkillPath("alpha"), nil)
	calls := 0
	_, err = c.restoreSnapshot(p, func(old, new string) error {
		calls++
		if calls == 2 {
			return errors.New("injected replacement failure")
		}
		return os.Rename(old, new)
	})
	if err == nil || calls != 3 {
		t.Fatalf("应尝试回滚: calls=%d err=%v", calls, err)
	}
	after, _ := hashTreeExcluding(c.SkillPath("alpha"), nil)
	if before != after {
		t.Fatal("失败未恢复当前内容")
	}
	entries, err := os.ReadDir(c.Root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "alpha" {
		t.Fatalf("临时目录未清理: %v", entries)
	}
}

func TestSnapshotDiffShowsRealLinesAndBounds(t *testing.T) {
	c, stamp := snapshotFixture(t)
	p, err := c.PreviewSnapshot("alpha", StateEnabled, stamp)
	if err != nil {
		t.Fatal(err)
	}
	d, err := c.SnapshotFileDiff(p, "notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, line := range d.Lines {
		got[line.Kind+line.Text] = true
	}
	if !got["-current"] || !got["+historic"] || !got[" keep"] {
		t.Fatalf("逐行差异不完整: %+v", d.Lines)
	}
	if _, err := c.SnapshotFileDiff(p, "../../elsewhere"); err == nil {
		t.Fatal("不能读未在预览中的路径")
	}
	if err := os.WriteFile(filepath.Join(c.SnapshotPath("alpha", stamp), "large.txt"), []byte(strings.Repeat("line\n", 20000)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.SnapshotPath("alpha", stamp), "binary.bin"), []byte{1, 0, 2}, 0o644); err != nil {
		t.Fatal(err)
	}
	p, err = c.PreviewSnapshot("alpha", StateEnabled, stamp)
	if err != nil {
		t.Fatal(err)
	}
	d, err = c.SnapshotFileDiff(p, "large.txt")
	if err != nil || !d.Truncated || len(d.Lines) > 400 {
		t.Fatalf("差异必须有界: %+v %v", d, err)
	}
	d, err = c.SnapshotFileDiff(p, "binary.bin")
	if err != nil || !d.Binary {
		t.Fatalf("二进制必须明确标记: %+v %v", d, err)
	}
}

func TestSnapshotRestoresMetadataAndExecutableFiles(t *testing.T) {
	c, stamp := snapshotFixture(t)
	dir := c.SnapshotPath("alpha", stamp)
	meta := &Meta{MetaVersion: MetaVersion, ID: "alpha", Notes: "historical metadata"}
	if err := writeMeta(dir, meta); err != nil {
		t.Fatal(err)
	}
	// 当前元数据损坏仍可从有效历史恢复；损坏原件同样会被先保存。
	if err := os.WriteFile(filepath.Join(c.SkillPath("alpha"), MetaFileName), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := c.PreviewSnapshot("alpha", StateEnabled, stamp)
	if err != nil {
		t.Fatal(err)
	}
	if !p.MetadataChanged {
		t.Fatal("元数据恢复必须提示")
	}
	files := map[string]bool{}
	for _, f := range p.Files {
		files[f.Path] = true
	}
	if !files[MetaFileName] || !files[".git/HEAD"] {
		t.Fatal("恢复的元数据和隐藏内容必须列入差异")
	}
	r, err := c.RestoreSnapshot(p)
	if err != nil {
		t.Fatal(err)
	}
	if m := readMeta(r.Dir); m.Err != nil || m.Meta == nil || m.Meta.Notes != "historical metadata" {
		t.Fatal("未完整恢复元数据")
	}
	if got := snapshotRead(t, filepath.Join(r.SnapshotDir, MetaFileName)); got != "{broken" {
		t.Fatal("未保存原始损坏元数据")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(r.Dir, "run.sh"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Fatal("恢复丢失脚本可执行权限")
		}
	}
}

func TestSnapshotFileDirectoryTransitions(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(map[bool]string{false: "file-to-directory", true: "directory-to-file"}[reverse], func(t *testing.T) {
			c, stamp := snapshotFixture(t)
			fileDir, treeDir := c.SkillPath("alpha"), c.SnapshotPath("alpha", stamp)
			if reverse {
				fileDir, treeDir = treeDir, fileDir
			}
			if err := os.WriteFile(filepath.Join(fileDir, "a"), []byte("flat\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(treeDir, "a"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(treeDir, "a", "config.json"), []byte("{\"nested\":true}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			p, err := c.PreviewSnapshot("alpha", StateEnabled, stamp)
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"a", "a/config.json"} {
				diff, err := c.SnapshotFileDiff(p, path)
				if err != nil || len(diff.Lines) == 0 {
					t.Fatalf("文件/目录转换应能预览 %q: %v %+v", path, err, diff)
				}
			}
			if _, err := c.RestoreSnapshot(p); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(filepath.Join(c.SkillPath("alpha"), "a"))
			if err != nil {
				t.Fatal(err)
			}
			if info.IsDir() == reverse {
				t.Fatal("文件/目录转换未恢复")
			}
		})
	}
}

func TestSnapshotLargeFilesHashBeyondPreviewLimit(t *testing.T) {
	c, stamp := snapshotFixture(t)
	common := strings.Repeat("large payload\n", 10000)
	for dir, tail := range map[string]string{c.SkillPath("alpha"): "old tail", c.SnapshotPath("alpha", stamp): "new tail"} {
		if err := os.WriteFile(filepath.Join(dir, "large.txt"), []byte(common+tail), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s, err := summarizeSnapshotFile(filepath.Join(c.SkillPath("alpha"), "large.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !s.truncated || len(s.sample) > 64<<10 {
		t.Fatal("大文件样本未限制")
	}
	p, err := c.PreviewSnapshot("alpha", StateEnabled, stamp)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range p.Files {
		if f.Path == "large.txt" {
			found = true
			if f.Added != -1 || f.Removed != -1 {
				t.Fatal("截断样本不能冒充完整行统计")
			}
		}
	}
	if !found {
		t.Fatal("预览上限之后的差异被遗漏")
	}
	if _, err := c.RestoreSnapshot(p); err != nil {
		t.Fatal(err)
	}
	got := snapshotRead(t, filepath.Join(c.SkillPath("alpha"), "large.txt"))
	if got != common+"new tail" {
		t.Fatal("恢复必须使用完整快照")
	}
}
