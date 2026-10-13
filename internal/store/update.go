package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Update 从记录的来源重新拉取技能并替换根目录中的内容。
// 流程：快照旧内容 → 重新获取 → 原子替换 → 刷新元数据（updatedAt/commit/哈希）。
// 来源为 local 时从原路径复制（等价于重装）；来源为 github 时重新 clone。
// 拉取失败时技能保持原样，仅返回错误；替换成功后快照保留在 snapshots/ 下。
func (c *Config) Update(name string) (*Result, error) {
	src := c.SkillPath(name)
	if !fileExists(src) {
		return nil, fmt.Errorf("store: %w: %s", ErrUnknownSkill, name)
	}
	// 读取已有元数据；缺失或损坏时无法知道来源，直接报错（不 panic）。
	res := readMeta(src)
	if res.Err != nil {
		return nil, fmt.Errorf("store: %s 的元数据不可读: %w", name, res.Err)
	}
	if !res.Found {
		return nil, fmt.Errorf("store: %s 缺少 .kura-meta.json，无法更新", name)
	}
	meta := res.Meta
	var newDir string
	var cleanup func()
	var err error
	switch meta.Source.Type {
	case SourceLocal:
		if meta.Source.Path == "" {
			return nil, errors.New("store: 元数据缺少 local 来源路径")
		}
		newDir = meta.Source.Path
		if !fileExists(newDir) {
			return nil, fmt.Errorf("store: 本地来源不存在: %s", meta.Source.Path)
		}
	case SourceGitHub:
		if meta.Source.URL == "" {
			return nil, errors.New("store: 元数据缺少 github 来源 URL")
		}
		newDir, _, cleanup, err = c.refetchGitHub(name, meta)
		if err != nil {
			return nil, err
		}
		defer cleanup()
	default:
		return nil, fmt.Errorf("store: 未知来源类型 %q", meta.Source.Type)
	}
	// 快照旧内容（含旧元数据文件本身，用于完整还原）。
	now := time.Now()
	snapDir, err := c.snapshotSkill(name, src, now)
	if err != nil {
		return nil, err
	}
	// 原子替换：先删旧的，再把新内容移进来（同一磁盘上 Rename 是原子的）。
	staging := src + ".kura-new"
	os.RemoveAll(staging)
	if err := copyDir(newDir, staging); err != nil {
		os.RemoveAll(staging)
		return nil, fmt.Errorf("store: 准备新内容失败: %w", err)
	}
	commit := gitHeadStaging(staging)
	os.RemoveAll(filepath.Join(staging, ".git"))
	os.RemoveAll(src)
	if err := os.Rename(staging, src); err != nil {
		// 替换失败：从快照恢复旧内容。
		restoreErr := copyDir(snapDir, src)
		if restoreErr != nil {
			return nil, fmt.Errorf("store: 替换失败且自动恢复失败（快照在 %s）: %w", snapDir, err)
		}
		return nil, fmt.Errorf("store: 替换失败（已从快照恢复）: %w", err)
	}
	// 刷新元数据：保留 installedAt/pluginName/notes，更新 updatedAt、commit、哈希。
	meta.UpdatedAt = now.UTC().Format(time.RFC3339)
	meta.Source.Commit = commit
	hash, err := HashDir(src)
	if err != nil {
		return nil, err
	}
	meta.ContentHash = hash
	if err := writeMeta(src, meta); err != nil {
		return nil, fmt.Errorf("store: 更新元数据失败: %w", err)
	}
	return &Result{ID: name, State: StateEnabled, Dir: src, SnapshotDir: snapDir}, nil
}

// refetchGitHub 从元数据记录的 URL/Ref/SubPath 重新 clone 到临时目录。
// SubPath 为空时技能目录就是仓库根目录。
func (c *Config) refetchGitHub(name string, meta *Meta) (string, Source, func(), error) {
	tmp, err := os.MkdirTemp("", "kura-update-")
	if err != nil {
		return "", Source{}, nil, fmt.Errorf("store: 创建临时目录失败: %w", err)
	}
	cleanup := func() { os.RemoveAll(tmp) }
	args := []string{"clone", "--depth", "1"}
	if meta.Source.Ref != "" {
		args = append(args, "--branch", meta.Source.Ref)
	}
	args = append(args, meta.Source.URL, tmp)
	cmd := gitCommand(args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", Source{}, cleanup, fmt.Errorf("store: git clone 失败: %w: %s", err, strings.TrimSpace(string(out)))
	}
	dir := tmp
	if meta.Source.SubPath != "" {
		dir = filepath.Join(tmp, filepath.FromSlash(meta.Source.SubPath))
	}
	if !fileExists(dir) {
		return "", Source{}, cleanup, fmt.Errorf("store: 仓库内不存在目录 %s", meta.Source.SubPath)
	}
	return dir, meta.Source, cleanup, nil
}

// gitHeadStaging 读取 staging 目录中的 git commit（替换发生在移除 .git 之后无法
// 再读，故在复制后立刻读一次）。失败返回空串。
func gitHeadStaging(dir string) string {
	cmd := gitCommand("-C", dir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
