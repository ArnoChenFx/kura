package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Result 是所有变更操作的返回值：技能 id + 操作后的新状态。
type Result struct {
	ID          string
	State       State
	Dir         string
	SnapshotDir string
}

// stamp 生成快照/回收站目录名的时间戳（本地时间，文件名安全）。
func stamp(t time.Time) string {
	return t.Format("20060102-150405")
}

// snapshotSkill 把技能文件夹复制到 snapshots/<name>/<stamp>。复制而非移动：
// 即使后续操作失败，原位置与快照都完好。
func (c *Config) snapshotSkill(name, dir string, now time.Time) (string, error) {
	if err := c.ensureDirs(); err != nil {
		return "", err
	}
	dst := c.SnapshotPath(name, stamp(now))
	if err := copyDir(dir, dst); err != nil {
		return "", fmt.Errorf("store: 快照 %s 失败: %w", name, err)
	}
	return dst, nil
}

// Enable 把 disabled/<name> 移回根目录。根目录已有同名技能时返回冲突错误。
func (c *Config) Enable(name string) (*Result, error) {
	src := c.DisabledPath(name)
	if !fileExists(src) {
		return nil, fmt.Errorf("store: %w: %s", ErrUnknownSkill, name)
	}
	dst := c.SkillPath(name)
	if fileExists(dst) {
		return nil, fmt.Errorf("store: 根目录已存在同名技能 %s", name)
	}
	if err := os.MkdirAll(c.Root, 0o755); err != nil {
		return nil, fmt.Errorf("store: 创建根目录失败: %w", err)
	}
	if err := moveDir(src, dst); err != nil {
		return nil, fmt.Errorf("store: 启用 %s 失败: %w", name, err)
	}
	return &Result{ID: name, State: StateEnabled, Dir: dst}, nil
}

// Disable 把根目录的 <name> 移入 disabled/（纯移动，不复制不改内容）。
func (c *Config) Disable(name string) (*Result, error) {
	src := c.SkillPath(name)
	if !fileExists(src) {
		return nil, fmt.Errorf("store: %w: %s", ErrUnknownSkill, name)
	}
	dst := c.DisabledPath(name)
	if fileExists(dst) {
		return nil, fmt.Errorf("store: disabled 中已存在同名技能 %s", name)
	}
	if err := c.ensureDirs(); err != nil {
		return nil, err
	}
	if err := moveDir(src, dst); err != nil {
		return nil, fmt.Errorf("store: 禁用 %s 失败: %w", name, err)
	}
	return &Result{ID: name, State: StateDisabled, Dir: dst}, nil
}

// Remove 把根目录的技能移入 trash/<name>/<stamp>，移动前先做内容快照。
// 快照与回收站是两个动作：快照永远留在 snapshots/ 里，trash 里的副本用于恢复。
func (c *Config) Remove(name string) (*Result, error) {
	src := c.SkillPath(name)
	if !fileExists(src) {
		return nil, fmt.Errorf("store: %w: %s", ErrUnknownSkill, name)
	}
	now := time.Now()
	snapDir, err := c.snapshotSkill(name, src, now)
	if err != nil {
		return nil, err
	}
	if err := c.ensureDirs(); err != nil {
		return nil, err
	}
	dst := filepath.Join(c.TrashRoot(), name, stamp(now))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return nil, fmt.Errorf("store: 创建回收站目录失败: %w", err)
	}
	if err := moveDir(src, dst); err != nil {
		return nil, fmt.Errorf("store: 移入回收站失败 (%s): %w", name, err)
	}
	return &Result{ID: name, State: StateTrashed, Dir: dst, SnapshotDir: snapDir}, nil
}

// Restore 把 trash/<name>/<stamp> 移回根目录。stamp 为空时取最新一次。
// 根目录已有同名技能时返回冲突错误。
func (c *Config) Restore(name, stampName string) (*Result, error) {
	trashDir := filepath.Join(c.TrashRoot(), name)
	if !fileExists(trashDir) {
		return nil, fmt.Errorf("store: %w: %s（回收站中不存在）", ErrUnknownSkill, name)
	}
	if stampName == "" {
		latest, err := latestSubdir(trashDir)
		if err != nil {
			return nil, err
		}
		stampName = latest
	}
	src := filepath.Join(trashDir, stampName)
	dst := c.SkillPath(name)
	if fileExists(dst) {
		return nil, fmt.Errorf("store: 根目录已存在同名技能 %s", name)
	}
	if err := os.MkdirAll(c.Root, 0o755); err != nil {
		return nil, fmt.Errorf("store: 创建根目录失败: %w", err)
	}
	if err := moveDir(src, dst); err != nil {
		return nil, fmt.Errorf("store: 恢复 %s 失败: %w", name, err)
	}
	// 清理 trash/<name> 空壳目录：os.Remove 只能删空目录，若同一技能还有
	// 其他时间戳副本则会失败并忽略（保留它们是正确的）。
	os.Remove(trashDir)
	return &Result{ID: name, State: StateEnabled, Dir: dst}, nil
}

// Purge 把回收站里的技能 name 永久删除（它的全部时间戳副本）。删除前每份副本
// 都先快照到 snapshots/<name>/<stamp>：移入回收站时已有同名快照的沿用，不覆盖；
// 快照任何一份失败都不删除，并清掉写到一半的快照。快照本身不删，符合「破坏性
// 操作前先快照」的约定。回收站里没有该技能时返回 ErrUnknownSkill。
func (c *Config) Purge(name string) (*Result, error) {
	trashDir := filepath.Join(c.TrashRoot(), name)
	if !fileExists(trashDir) {
		return nil, fmt.Errorf("store: %w: %s（回收站中不存在）", ErrUnknownSkill, name)
	}
	entries, err := os.ReadDir(trashDir)
	if err != nil {
		return nil, fmt.Errorf("store: 读取回收站中的 %s 失败: %w", name, err)
	}
	if err := c.ensureDirs(); err != nil {
		return nil, err
	}
	var snapDir string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		src := filepath.Join(trashDir, e.Name())
		dst := c.SnapshotPath(name, e.Name())
		info, err := os.Stat(dst)
		switch {
		case err == nil && info.IsDir():
			// 移入回收站时已写下的同名快照与副本内容一致，沿用它。
		case err == nil:
			return nil, fmt.Errorf("store: 快照位置 %s 被文件占用，未删除 %s", dst, name)
		default:
			if err := copyDir(src, dst); err != nil {
				// dst 之前不存在，这里只清掉写到一半的部分副本。
				os.RemoveAll(dst)
				return nil, fmt.Errorf("store: 永久删除前快照 %s 失败，未删除: %w", name, err)
			}
		}
		snapDir = dst
	}
	if err := os.RemoveAll(trashDir); err != nil {
		return nil, fmt.Errorf("store: 永久删除 %s 失败: %w", name, err)
	}
	return &Result{ID: name, SnapshotDir: snapDir}, nil
}

// latestSubdir 返回 dir 下按名字排序最大的子目录名（时间戳目录名可排序）。
func latestSubdir(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("store: 读取 %s 失败: %w", dir, err)
	}
	var best string
	for _, e := range entries {
		if e.IsDir() && e.Name() > best {
			best = e.Name()
		}
	}
	if best == "" {
		return "", errors.New("store: 目录下没有子目录")
	}
	return best, nil
}
