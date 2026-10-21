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
	"os"
	"path/filepath"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/locales"
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
var ErrUnknownSkill = i18n.NewError("storeErrUnknownSkill", func() string { return locales.StoreErrUnknownSkill() })

// NewConfig 构造自定义配置；root 或 home 为空时返回错误（root 不可为空，
// home 为空时调用方应使用 DefaultConfig）。
func NewConfig(root, home string) (*Config, error) {
	if root == "" {
		return nil, i18n.NewError("storeErrEmptyRoot", func() string { return locales.StoreErrEmptyRoot() })
	}
	if home == "" {
		return nil, i18n.NewError("storeErrEmptyHome", func() string { return locales.StoreErrEmptyHome() })
	}
	return &Config{Root: root, Home: home}, nil
}

// DefaultConfig 返回默认配置：根目录 ~/.agents/skills，kura 数据目录 ~/.kura。
// HOME 无法解析时返回错误，而不是 panic。
func DefaultConfig() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, i18n.Wrapf(err, "storeErrUserHome", func() string { return locales.StoreErrUserHome() })
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
			return i18n.Wrapf(err, "storeErrCreateDir", func() string { return locales.StoreErrCreateDir(dir) })
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
		return nil, false, i18n.Wrapf(err, "storeErrReadFile", func() string { return locales.StoreErrReadFile(c.ConfigPath()) })
	}
	var hc HomeConfig
	if err := json.Unmarshal(data, &hc); err != nil {
		return nil, false, i18n.Wrapf(err, "storeErrParseJSON", func() string { return locales.StoreErrParseJSON(c.ConfigPath()) })
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
		return i18n.Wrapf(err, "storeErrMarshalConfig", func() string { return locales.StoreErrMarshalConfig() })
	}
	if err := os.WriteFile(c.ConfigPath(), data, 0o644); err != nil {
		return i18n.Wrapf(err, "storeErrWriteFile", func() string { return locales.StoreErrWriteFile(c.ConfigPath()) })
	}
	return nil
}

// HomeConfig 是 ~/.kura/config.json 的内容：技能根目录列表、外观与界面偏好、
// 应用自更新的偏好与检查时间，以及创建时间，字段保持小写驼峰。
type HomeConfig struct {
	// Roots 是用户添加的技能根目录列表（见 Library）；默认根目录总是第一个。
	Roots []string `json:"roots"`
	// Theme 是外观偏好：ThemeSystem、ThemeLight 或 ThemeDark；空串等同跟随系统。
	Theme string `json:"theme,omitempty"`
	// View 是技能区的排列模式：ViewGrid（卡片网格）或 ViewList（两行列表）；
	// 空串等同卡片网格。
	View string `json:"view,omitempty"`
	// Lang 是界面语言偏好：LangSystem（跟随系统，缺省）、LangZH 或 LangEN。
	// 空串等同跟随系统。
	Lang string `json:"lang,omitempty"`
	// AutoCheck 是「自动检查更新」偏好。nil 表示还没定过，读取时按开处理；
	// 定过之后它就是唯一真值，插件 updater.json 里的同名值只是派生的调度开关
	// （见 ResolveAutoCheck）。
	AutoCheck *bool `json:"autoCheckUpdates,omitempty"`
	// UpdateCheckedAt 是 kura 自己最近一次成功检查应用更新的时间（RFC3339）。
	// 后台检查的节奏靠它；插件的 lastCheck 只记插件自己的检查。
	UpdateCheckedAt string `json:"updateCheckedAt,omitempty"`
	// UpdateCheckedVersion 是上面那条检查时间对应的应用版本，也就是那次检查时
	// 正在运行的版本。用来判断「装完新版本后第一次启动要不要重新查一次」：记录的
	// 版本与当前运行版本不一致时，这条时间不再推迟后台检查。空串（旧版本写的
	// config.json 只有时间、没有这一项）表示版本未知，不能当成「与当前版本一致」。
	UpdateCheckedVersion string `json:"updateCheckedVersion,omitempty"`
	// Discovered 记录已经考虑过的 agent 技能目录（绝对路径）。自动发现
	// （Library.DiscoverAgentRoots）每个路径只考虑一次：用户之后从列表移除的
	// 目录记在这里，下次启动不会被再次加回来。
	Discovered []string `json:"discovered,omitempty"`
	// CreatedAt 是配置首次创建时间（RFC3339）。
	CreatedAt string `json:"createdAt,omitempty"`
}
