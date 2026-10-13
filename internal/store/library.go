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
)

// Library 是全部技能根目录。Roots[0] 是默认根目录。
type Library struct {
	// Home 是 kura 的数据目录。
	Home string
	// Roots 是根目录的绝对路径，Roots[0] 为默认根目录。
	Roots []string
}

// LoadLibrary 以 base 的根目录为默认根目录、base.Home 为数据目录，读取
// config.json 里追加的根目录。配置文件不存在时只有默认根目录；文件损坏时
// 同样返回只含默认根目录的 Library，并附带错误，调用方可继续使用。
func LoadLibrary(base *Config) (*Library, error) {
	l := &Library{Home: base.Home, Roots: []string{filepath.Clean(base.Root)}}
	hc, _, err := LoadHomeConfig(base)
	if err != nil {
		return l, err
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

// save 把根目录列表写回 config.json，保留已有的 createdAt。
func (l *Library) save(roots []string) error {
	c := &Config{Root: roots[0], Home: l.Home}
	hc, _, err := LoadHomeConfig(c)
	if err != nil {
		// 损坏的配置文件被新列表覆盖：根目录列表是它唯一有意义的内容。
		hc = &HomeConfig{}
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
