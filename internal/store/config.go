// Package store 实现 kura 的核心存储层：扫描技能根目录、读写
// .kura-meta.json 元数据、计算内容哈希、从 .skill-lock.json 导入元数据，
// 以及启用/禁用/删除/恢复/添加/更新等全部变更操作。
//
// 设计要点：
//   - 根目录与 kura 数据目录全部建模为配置（root 不可为空）；多个根目录由
//     Library 管理，每个根目录对应一个 Config。
//   - 所有操作返回类型化结果或错误，绝不 panic；元数据文件损坏按零值处理。
//   - 每次修改技能内容前先写入快照（Update/Remove）。
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Config 是 kura 的全部目录建模：一个技能根目录 + kura 自有数据目录。
// 其他 agent 根目录（.claude/skills 等）留待 v2 通过增加 Root 实现。
type Config struct {
	// Root 是被管理的技能根目录，不可为空。
	Root string
	// Home 是 kura 的数据目录（默认 ~/.kura），存放 disabled/trash/snapshots/config.json。
	Home string
	// Scope 是该根目录私有状态（disabled/trash/snapshots）在 Home 下的相对
	// 子目录。默认根目录为空串，沿用 Home/disabled 等原有布局；Library 里
	// 追加的根目录为 roots/<id>，避免不同根目录的同名技能互相覆盖。
	Scope string
}

// Config 的目录约定常量。
const (
	// MetaFileName 是技能文件夹内的元数据文件名。
	MetaFileName = ".kura-meta.json"
	// DisabledDir / TrashDir / SnapshotsDir 是 Home 下的固定子目录名。
	DisabledDir  = "disabled"
	TrashDir     = "trash"
	SnapshotsDir = "snapshots"
	// ConfigFileName 是 Home 下的配置文件名。
	ConfigFileName = "config.json"
)

// ErrUnknownSkill 表示请求的技能在根目录（或对应位置）不存在。
var ErrUnknownSkill = errors.New("store: 未找到该技能")

// NewConfig 构造自定义配置；root 或 home 为空时返回错误（root 不可为空，
// home 为空时调用方应使用 DefaultConfig）。
func NewConfig(root, home string) (*Config, error) {
	if root == "" {
		return nil, errors.New("store: root 不能为空")
	}
	if home == "" {
		return nil, errors.New("store: home 不能为空（可改用 DefaultConfig）")
	}
	return &Config{Root: root, Home: home}, nil
}

// DefaultConfig 返回默认配置：根目录 ~/.agents/skills，kura 数据目录 ~/.kura。
// HOME 无法解析时返回错误，而不是 panic。
func DefaultConfig() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("store: 无法确定用户主目录: %w", err)
	}
	return &Config{
		Root: filepath.Join(home, ".agents", "skills"),
		Home: filepath.Join(home, ".kura"),
	}, nil
}

// stateHome 返回该根目录私有状态的目录（Home 或 Home/<Scope>）。
func (c *Config) stateHome() string { return filepath.Join(c.Home, c.Scope) }

// DisabledRoot 返回禁用技能的存放目录（Home/[Scope/]disabled）。
func (c *Config) DisabledRoot() string { return filepath.Join(c.stateHome(), DisabledDir) }

// TrashRoot 返回已删除技能的回收站目录（Home/[Scope/]trash）。
func (c *Config) TrashRoot() string { return filepath.Join(c.stateHome(), TrashDir) }

// SnapshotsRoot 返回内容快照目录（Home/[Scope/]snapshots）。
func (c *Config) SnapshotsRoot() string { return filepath.Join(c.stateHome(), SnapshotsDir) }

// ConfigPath 返回 Home/config.json 的路径。
func (c *Config) ConfigPath() string { return filepath.Join(c.Home, ConfigFileName) }

// SkillPath 返回根目录下名为 name 的技能文件夹路径。
func (c *Config) SkillPath(name string) string { return filepath.Join(c.Root, name) }

// DisabledPath 返回 disabled 下名为 name 的技能文件夹路径。
func (c *Config) DisabledPath(name string) string { return filepath.Join(c.DisabledRoot(), name) }

// TrashPath 返回 trash 下名为 name 的技能文件夹路径。
func (c *Config) TrashPath(name string) string { return filepath.Join(c.TrashRoot(), name) }

// SnapshotPath 返回某技能某时间戳快照的目录路径。
func (c *Config) SnapshotPath(name, stamp string) string {
	return filepath.Join(c.SnapshotsRoot(), name, stamp)
}

// ensureDirs 创建 Home 及其 disabled/trash/snapshots 子目录（幂等）。
func (c *Config) ensureDirs() error {
	for _, dir := range []string{c.Home, c.DisabledRoot(), c.TrashRoot(), c.SnapshotsRoot()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("store: 创建目录 %s 失败: %w", dir, err)
		}
	}
	return nil
}

// LoadHomeConfig 从 Home/config.json 读取配置；文件不存在时返回零值与 false，
// 文件损坏时返回错误（配置文件损坏是可修复的用户问题，不应静默忽略）。
func LoadHomeConfig(c *Config) (*HomeConfig, bool, error) {
	data, err := os.ReadFile(c.ConfigPath())
	if errors.Is(err, os.ErrNotExist) {
		return &HomeConfig{}, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("store: 读取 %s 失败: %w", c.ConfigPath(), err)
	}
	var hc HomeConfig
	if err := json.Unmarshal(data, &hc); err != nil {
		return nil, false, fmt.Errorf("store: 解析 %s 失败（JSON 已损坏）: %w", c.ConfigPath(), err)
	}
	return &hc, true, nil
}

// SaveHomeConfig 将配置写入 Home/config.json（缩进两格，便于手工查看）。
func SaveHomeConfig(c *Config, hc *HomeConfig) error {
	if err := c.ensureDirs(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(hc, "", "  ")
	if err != nil {
		return fmt.Errorf("store: 序列化配置失败: %w", err)
	}
	if err := os.WriteFile(c.ConfigPath(), data, 0o644); err != nil {
		return fmt.Errorf("store: 写入 %s 失败: %w", c.ConfigPath(), err)
	}
	return nil
}

// HomeConfig 是 ~/.kura/config.json 的内容：技能根目录列表与创建时间，
// 字段保持小写驼峰。
type HomeConfig struct {
	// Roots 是用户添加的技能根目录列表（见 Library）；默认根目录总是第一个。
	Roots []string `json:"roots"`
	// CreatedAt 是配置首次创建时间（RFC3339）。
	CreatedAt string `json:"createdAt,omitempty"`
}
