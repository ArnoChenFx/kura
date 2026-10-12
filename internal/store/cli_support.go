package store

// store 包为 CLI 层（internal/cli）补充的少量导出辅助：它们都是对内部
// 既有能力的薄封装，保持 store 的封装边界（元数据读写、快照、目录约定
// 仍然只在这里发生）。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidStampName 校验用户指定的时间戳目录名是否安全：非空、不含路径分隔符
// 与 ".."（防止构造出 trash/<name>/../../ 之类的越界路径）。
func ValidStampName(s string) bool {
	if s == "" || strings.ContainsAny(s, `/\`) || strings.Contains(s, "..") {
		return false
	}
	return s != "." && s != string(filepath.Separator)
}

// snapshotSkillAt 把技能文件夹复制到 snapshots/<name>/<stampName>（自定义
// 时间戳版本，供 CLI 的 remove --stamp 使用）。目标已存在时先清空再复制。
func (c *Config) snapshotSkillAt(name, dir, stampName string) (string, error) {
	dst := c.SnapshotPath(name, stampName)
	if err := copyDir(dir, dst); err != nil {
		return "", fmt.Errorf("store: 快照 %s 失败: %w", name, err)
	}
	return dst, nil
}

// RemoveWithStamp 与 Remove 相同（快照 → 移入回收站），但回收站与快照目录
// 使用调用方指定的时间戳名 stampName，而不是当前时间。回收站中已存在同名
// 时间戳目录时报错，绝不覆盖。
func (c *Config) RemoveWithStamp(name, stampName string) (*Result, error) {
	if !ValidStampName(stampName) {
		return nil, fmt.Errorf("store: 非法的时间戳目录名 %q", stampName)
	}
	src := c.SkillPath(name)
	if !fileExists(src) {
		return nil, fmt.Errorf("store: %w: %s", ErrUnknownSkill, name)
	}
	if err := c.ensureDirs(); err != nil {
		return nil, err
	}
	snapDir, err := c.snapshotSkillAt(name, src, stampName)
	if err != nil {
		return nil, err
	}
	dst := filepath.Join(c.TrashRoot(), name, stampName)
	if fileExists(dst) {
		return nil, fmt.Errorf("store: 回收站中已存在 %s/%s", name, stampName)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return nil, fmt.Errorf("store: 创建回收站目录失败: %w", err)
	}
	if err := os.Rename(src, dst); err != nil {
		return nil, fmt.Errorf("store: 移入回收站失败 (%s): %w", name, err)
	}
	return &Result{ID: name, State: StateTrashed, Dir: dst, SnapshotDir: snapDir}, nil
}
