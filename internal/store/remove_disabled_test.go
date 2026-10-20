package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// 启用与停用区可有同名的不同内容；每个移除入口必须只移动指定状态的
// 副本，并以这份内容写快照。Remove 仍保持 CLI 使用的启用副本契约。
func TestRemoveTargetsRequestedState(t *testing.T) {
	for _, state := range []State{StateEnabled, StateDisabled} {
		t.Run(string(state), func(t *testing.T) {
			c := newTestConfig(t)
			writeSkill(t, c.Root, "duplicate", map[string]string{"copy.txt": "enabled copy"})
			writeSkill(t, c.DisabledRoot(), "duplicate", map[string]string{"copy.txt": "disabled copy"})
			source, other, remove := c.SkillPath("duplicate"), c.DisabledPath("duplicate"), c.Remove
			if state == StateDisabled {
				source, other, remove = other, source, c.RemoveDisabled
			}
			sourceHash, err := HashDir(source)
			if err != nil {
				t.Fatal(err)
			}
			otherHash, err := HashDir(other)
			if err != nil {
				t.Fatal(err)
			}
			result, err := remove("duplicate")
			if err != nil {
				t.Fatal(err)
			}
			if result.ID != "duplicate" || result.State != StateTrashed {
				t.Fatalf("移除结果错误：%+v", result)
			}
			if _, err := os.Stat(source); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("指定副本应已移走：%v", err)
			}
			if got, err := HashDir(other); err != nil || got != otherHash {
				t.Fatalf("另一状态的同名副本必须原样保留：hash=%s err=%v", got, err)
			}
			for _, dir := range []string{result.Dir, result.SnapshotDir} {
				if got, err := HashDir(dir); err != nil || got != sourceHash {
					t.Errorf("回收站与快照应保存指定副本：dir=%s hash=%s err=%v", dir, got, err)
				}
			}
		})
	}
}

// 停用副本消失时不能回退到删除根目录中的同名技能。
func TestRemoveDisabledMissingDoesNotRemoveEnabled(t *testing.T) {
	c := newTestConfig(t)
	writeSkill(t, c.Root, "duplicate", map[string]string{"copy.txt": "keep enabled"})
	if _, err := c.RemoveDisabled("duplicate"); !errors.Is(err, ErrUnknownSkill) {
		t.Fatalf("停用副本不存在应返回 ErrUnknownSkill，得到 %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(c.SkillPath("duplicate"), "copy.txt")); err != nil || string(got) != "keep enabled" {
		t.Fatalf("启用副本应原样保留：%q err=%v", got, err)
	}
	if _, err := os.Stat(c.TrashPath("duplicate")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("不应创建回收站副本：%v", err)
	}
}
