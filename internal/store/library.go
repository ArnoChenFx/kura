package store

// Library 管理多个技能根目录：默认根目录（~/.agents/skills）总是第一个，
// 用户可以追加任意文件夹。列表持久化在 Home/config.json 的 roots 字段。
//
// 每个根目录对应一个 Config。默认根目录的 Scope 为空，disabled/trash/
// snapshots 仍在 Home 下的原有位置，已有数据无需迁移；追加的根目录各自
// 使用 Home/roots/<id>/ 存放这些状态，两个根目录里的同名技能互不干扰。

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// RootsDir 是追加根目录私有状态在 Home 下的父目录名。
const RootsDir = "roots"

var (
	// ErrRootExists 表示要添加的根目录已在列表里。
	ErrRootExists = errors.New("store: 该目录已在列表中")
	// ErrUnknownRoot 表示要移除的根目录不在列表里。
	ErrUnknownRoot = errors.New("store: 列表中没有该目录")
	// ErrDefaultRoot 表示试图移除默认根目录。
	ErrDefaultRoot = errors.New("store: 默认技能目录不能移除")
	// ErrNotDir 表示要添加的路径不存在或不是文件夹。
	ErrNotDir = errors.New("store: 路径不存在或不是文件夹")
	// ErrUnknownTheme 表示外观偏好不是 system、light 或 dark。
	ErrUnknownTheme = errors.New("store: 外观只能是 system、light 或 dark")
)

// 外观偏好的取值（config.json 的 theme）。
const (
	// ThemeSystem 跟随系统的浅色/深色设置，是默认值。
	ThemeSystem = "system"
	// ThemeLight 总是浅色。
	ThemeLight = "light"
	// ThemeDark 总是深色。
	ThemeDark = "dark"
)

// NormalizeTheme 把外观偏好规范成三个取值之一：空串是跟随系统；
// 不认识的值返回 ErrUnknownTheme。
func NormalizeTheme(mode string) (string, error) {
	switch mode {
	case "", ThemeSystem:
		return ThemeSystem, nil
	case ThemeLight, ThemeDark:
		return mode, nil
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownTheme, mode)
}

// configMu 串行化对 config.json 的「读—改—写」。窗口里改外观与增删目录是
// 各自的后台任务，不串行的话，一个任务会覆盖另一个刚写下的改动，或者读到
// 对方写了一半的文件，把它当成损坏的配置覆盖掉。
var configMu sync.Mutex

// Library 是全部技能根目录。Roots[0] 是默认根目录。
type Library struct {
	// Home 是 kura 的数据目录。
	Home string
	// Roots 是根目录的绝对路径，Roots[0] 为默认根目录。
	Roots []string
	// Theme 是读取时 config.json 里的外观偏好（已规范，缺省为 ThemeSystem）。
	// 只在启动时用来定初始外观；改外观走 SetTheme，它直接读写文件。
	Theme string
}

// LoadLibrary 以 base 的根目录为默认根目录、base.Home 为数据目录，读取
// config.json 里追加的根目录。配置文件不存在时只有默认根目录；文件损坏时
// 同样返回只含默认根目录的 Library，并附带错误，调用方可继续使用。
func LoadLibrary(base *Config) (*Library, error) {
	l := &Library{Home: base.Home, Roots: []string{filepath.Clean(base.Root)}, Theme: ThemeSystem}
	hc, _, err := LoadHomeConfig(base)
	if err != nil {
		return l, err
	}
	// 写坏或手改出的未知外观按跟随系统处理，不当作错误。
	if mode, terr := NormalizeTheme(hc.Theme); terr == nil {
		l.Theme = mode
	}
	for _, r := range hc.Roots {
		if r == "" || l.index(r) >= 0 {
			continue
		}
		l.Roots = append(l.Roots, filepath.Clean(r))
	}
	return l, nil
}

// Default 返回默认根目录。
func (l *Library) Default() string { return l.Roots[0] }

// Config 返回根目录 root 对应的 Config。默认根目录与不在列表里的路径
// 使用原有布局（Scope 为空）；追加的根目录使用 roots/<id>。
func (l *Library) Config(root string) *Config {
	root = filepath.Clean(root)
	c := &Config{Root: root, Home: l.Home}
	if i := l.index(root); i > 0 {
		c.Scope = filepath.Join(RootsDir, RootID(root))
	}
	return c
}

// Configs 按列表顺序返回每个根目录的 Config。
func (l *Library) Configs() []*Config {
	out := make([]*Config, len(l.Roots))
	for i, r := range l.Roots {
		out[i] = l.Config(r)
	}
	return out
}

// AddRoot 把文件夹 root 追加到列表末尾并保存。root 必须是已存在的文件夹；
// 已在列表里时返回 ErrRootExists。
func (l *Library) AddRoot(root string) error {
	abs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("store: 无法解析路径 %s: %w", root, err)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%w: %s", ErrNotDir, abs)
	}
	if l.index(abs) >= 0 {
		return fmt.Errorf("%w: %s", ErrRootExists, abs)
	}
	next := append(append([]string(nil), l.Roots...), abs)
	if err := l.save(next); err != nil {
		return err
	}
	l.Roots = next
	return nil
}

// RemoveRoot 把 root 从列表移除并保存，只改列表，不动磁盘上的任何技能。
// 默认根目录不能移除（ErrDefaultRoot）。
func (l *Library) RemoveRoot(root string) error {
	i := l.index(root)
	switch {
	case i < 0:
		return fmt.Errorf("%w: %s", ErrUnknownRoot, root)
	case i == 0:
		return ErrDefaultRoot
	}
	next := append(append([]string(nil), l.Roots[:i]...), l.Roots[i+1:]...)
	if err := l.save(next); err != nil {
		return err
	}
	l.Roots = next
	return nil
}

// SetTheme 把外观偏好写进 config.json（其余内容原样保留）。mode 不是
// system、light、dark 时返回 ErrUnknownTheme，不写盘。config.json 损坏时
// 它被只含这一项偏好的新文件覆盖，与 save 对根目录列表的处理一致。
func (l *Library) SetTheme(mode string) error {
	mode, err := NormalizeTheme(mode)
	if err != nil {
		return err
	}
	configMu.Lock()
	defer configMu.Unlock()
	c := &Config{Root: l.Roots[0], Home: l.Home}
	hc, _, err := LoadHomeConfig(c)
	if err != nil {
		hc = &HomeConfig{Roots: append([]string(nil), l.Roots...)}
	}
	if hc.CreatedAt == "" {
		hc.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	hc.Theme = mode
	if err := SaveHomeConfig(c, hc); err != nil {
		return err
	}
	l.Theme = mode
	return nil
}

// save 把根目录列表写回 config.json，保留已有的 createdAt 与外观偏好。
func (l *Library) save(roots []string) error {
	configMu.Lock()
	defer configMu.Unlock()
	c := &Config{Root: roots[0], Home: l.Home}
	hc, _, err := LoadHomeConfig(c)
	if err != nil {
		// 损坏的配置文件被新列表覆盖；本次运行里设过的外观偏好带过去。
		hc = &HomeConfig{}
		if l.Theme != ThemeSystem {
			hc.Theme = l.Theme
		}
	}
	if hc.CreatedAt == "" {
		hc.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	hc.Roots = roots
	return SaveHomeConfig(c, hc)
}

// index 返回 root 在列表里的位置，不在时返回 -1。
func (l *Library) index(root string) int {
	key := rootKey(root)
	for i, r := range l.Roots {
		if rootKey(r) == key {
			return i
		}
	}
	return -1
}

// RootID 是根目录的稳定短标识：规范化路径的 SHA-256 前 12 位十六进制。
func RootID(root string) string {
	sum := sha256.Sum256([]byte(rootKey(root)))
	return hex.EncodeToString(sum[:])[:12]
}

// rootKey 规范化路径用于比较：Clean，Windows 下忽略大小写。
func rootKey(root string) string {
	root = filepath.Clean(root)
	if runtime.GOOS == "windows" {
		root = strings.ToLower(root)
	}
	return root
}
