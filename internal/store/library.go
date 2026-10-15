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
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/locales"
)

// RootsDir 是追加根目录私有状态在 Home 下的父目录名。
const RootsDir = "roots"

var (
	// ErrRootExists 表示要添加的根目录已在列表里。
	ErrRootExists = i18n.NewError("storeErrRootExists", func() string { return locales.StoreErrRootExists() })
	// ErrUnknownRoot 表示要移除的根目录不在列表里。
	ErrUnknownRoot = i18n.NewError("storeErrUnknownRoot", func() string { return locales.StoreErrUnknownRoot() })
	// ErrDefaultRoot 表示试图移除默认根目录。
	ErrDefaultRoot = i18n.NewError("storeErrDefaultRoot", func() string { return locales.StoreErrDefaultRoot() })
	// ErrNotDir 表示要添加的路径不存在或不是文件夹。
	ErrNotDir = i18n.NewError("storeErrNotDir", func() string { return locales.StoreErrNotDir() })
	// ErrUnknownTheme 表示外观偏好不是 system、light 或 dark。
	ErrUnknownTheme = i18n.NewError("storeErrUnknownTheme", func() string { return locales.StoreErrUnknownTheme() })
	// ErrUnknownView 表示排列模式不是 grid 或 list。
	ErrUnknownView = i18n.NewError("storeErrUnknownView", func() string { return locales.StoreErrUnknownView() })
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

// 技能区排列模式的取值（config.json 的 view）。
const (
	// ViewGrid 是卡片网格，是默认值。
	ViewGrid = "grid"
	// ViewList 是两行列表：一个技能占两行（名字与控件、单行描述）。
	ViewList = "list"
)

// NormalizeView 把排列模式规范成两个取值之一：空串是卡片网格；
// 不认识的值返回 ErrUnknownView。
func NormalizeView(v string) (string, error) {
	switch v {
	case "", ViewGrid:
		return ViewGrid, nil
	case ViewList:
		return v, nil
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownView, v)
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
	// View 是读取时 config.json 里的排列模式（已规范，缺省为 ViewGrid）。
	// 与 Theme 一样只在启动时读；改排列模式走 SetView，它直接读写文件。
	View string
	// Lang 是读取时 config.json 里的界面语言偏好（已规范，缺省为
	// LangSystem，即跟随系统）。启动时由 main.go / CLI 解析成具体语言并
	// 交给 i18n.Set；改语言走 SetLang。
	Lang string
}

// LoadLibrary 以 base 的根目录为默认根目录、base.Home 为数据目录，读取
// config.json 里追加的根目录。配置文件不存在时只有默认根目录；文件损坏时
// 同样返回只含默认根目录的 Library，并附带错误，调用方可继续使用。
func LoadLibrary(base *Config) (*Library, error) {
	l := &Library{Home: base.Home, Roots: []string{filepath.Clean(base.Root)},
		Theme: ThemeSystem, View: ViewGrid, Lang: LangSystem}
	hc, _, err := LoadHomeConfig(base)
	if err != nil {
		return l, err
	}
	// 写坏或手改出的未知外观按跟随系统处理，不当作错误。
	if mode, terr := NormalizeTheme(hc.Theme); terr == nil {
		l.Theme = mode
	}
	// 同理：未知的排列模式按卡片网格处理，不当作错误。
	if v, verr := NormalizeView(hc.View); verr == nil {
		l.View = v
	}
	// 语言偏好同理：缺失或不认识都按跟随系统处理。
	l.Lang = langFromHome(hc)
	l.Roots = mergeRoots(base.Root, hc.Roots)
	return l, nil
}

// mergeRoots 返回默认目录加上 saved 里的追加目录：去掉空串与重复项，默认
// 目录总在第一个（即使 saved 里也写了它）。
func mergeRoots(def string, saved []string) []string {
	l := &Library{Roots: []string{filepath.Clean(def)}}
	for _, r := range saved {
		if r == "" || l.index(r) >= 0 {
			continue
		}
		l.Roots = append(l.Roots, filepath.Clean(r))
	}
	return l.Roots
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
		return i18n.Wrapf(err, "storeErrResolvePath", func() string { return locales.StoreErrResolvePath(root) })
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%w: %s", ErrNotDir, abs)
	}
	// 「已在列表里」要对 config.json 里最新的列表判，所以放进锁内的回调。
	return l.updateRoots(func(cur []string) ([]string, error) {
		if indexOf(cur, abs) >= 0 {
			return nil, fmt.Errorf("%w: %s", ErrRootExists, abs)
		}
		return append(cur, abs), nil
	})
}

// RemoveRoot 把 root 从列表移除并保存，只改列表，不动磁盘上的任何技能。
// 默认根目录不能移除（ErrDefaultRoot）。
func (l *Library) RemoveRoot(root string) error {
	return l.updateRoots(func(cur []string) ([]string, error) {
		i := indexOf(cur, root)
		switch {
		case i < 0:
			return nil, fmt.Errorf("%w: %s", ErrUnknownRoot, root)
		case i == 0:
			return nil, ErrDefaultRoot
		}
		return slices.Delete(cur, i, i+1), nil
	})
}

// MoveRoot 把 root 挪到列表里的第 to 个位置（从 0 数）并保存，只改列表顺序。
// 默认目录固定在第一个：它不能移动（ErrDefaultRoot），其他目录也不能挪到
// 第一个位置；root 不在列表里返回 ErrUnknownRoot，to 越界返回普通错误。
func (l *Library) MoveRoot(root string, to int) error {
	return l.updateRoots(func(cur []string) ([]string, error) {
		i := indexOf(cur, root)
		switch {
		case i < 0:
			return nil, fmt.Errorf("%w: %s", ErrUnknownRoot, root)
		case i == 0 || to == 0:
			return nil, ErrDefaultRoot
		case to < 0 || to >= len(cur):
			return nil, i18n.NewError("storeErrPosOutOfRange", func() string { return locales.StoreErrPosOutOfRange(to) })
		}
		moved := cur[i]
		return slices.Insert(slices.Delete(cur, i, i+1), to, moved), nil
	})
}

// updateRoots 是增、删、排序共用的「读—改—写」：持 configMu，以锁内刚从
// config.json 读到的列表为基础交给 mutate 计算新列表，再写回并同步内存里的
// l.Roots。基于内存副本算新列表会把别处（另一份 Library、DiscoverAgentRoots）
// 刚写下的目录覆盖掉，所以既有的校验（已存在、默认根、越界）也在 mutate 里、
// 对最新的列表重新判一遍。mutate 拿到的是可随意修改的拷贝；返回错误时不写盘。
//
// config.json 不存在时从默认根目录起算；损坏时以内存里的列表为基础，新列表
// 覆盖它（与 DiscoverAgentRoots 相反：这里是用户明确要求的修改），本次运行里
// 设过的外观与排列偏好带过去。
func (l *Library) updateRoots(mutate func(cur []string) ([]string, error)) error {
	configMu.Lock()
	defer configMu.Unlock()
	c := &Config{Root: l.Roots[0], Home: l.Home}
	var cur []string
	hc, _, err := LoadHomeConfig(c)
	if err != nil {
		hc = &HomeConfig{}
		if l.Theme != ThemeSystem {
			hc.Theme = l.Theme
		}
		if l.View != "" && l.View != ViewGrid {
			hc.View = l.View
		}
		cur = append([]string(nil), l.Roots...)
	} else {
		cur = mergeRoots(l.Roots[0], hc.Roots)
	}
	next, err := mutate(cur)
	if err != nil {
		return err
	}
	if hc.CreatedAt == "" {
		hc.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	hc.Roots = next
	if err := SaveHomeConfig(c, hc); err != nil {
		return err
	}
	// 写盘成功后才更新内存，保证两边一致；next 可能含别处刚加入的目录。
	l.Roots = next
	return nil
}

// DiscoverAgentRoots 把本机已有的 agent 技能目录（见 AgentSkillDirs）直接加入
// 列表并写回 config.json，不逐个确认。每个路径只考虑一次：考虑过的路径记入
// discovered，之后用户即使从列表移除了它，下次也不会再加回来。已在列表里、
// 或与列表里某个目录指向同一处（符号链接、与默认目录相同）的路径只记入
// discovered，不重复加入。返回本次新加入的目录；没有新的路径时不写盘。
//
// 读—改—写以 config.json 为准（不用内存里的副本），并持 configMu；config.json
// 损坏时返回错误且不写盘，不覆盖用户手改的内容。
func (l *Library) DiscoverAgentRoots(home string) ([]string, error) {
	candidates := AgentSkillDirs(home)
	configMu.Lock()
	defer configMu.Unlock()
	c := &Config{Root: l.Roots[0], Home: l.Home}
	hc, _, err := LoadHomeConfig(c)
	if err != nil {
		return nil, err
	}
	roots := mergeRoots(l.Roots[0], hc.Roots)
	considered := make(map[string]bool, len(hc.Discovered))
	for _, p := range hc.Discovered {
		considered[rootKey(p)] = true
	}
	var added []string
	changed := false
	for _, dir := range candidates {
		if considered[rootKey(dir)] {
			continue
		}
		considered[rootKey(dir)] = true
		hc.Discovered = append(hc.Discovered, dir)
		changed = true
		if sameDir(dir, roots) {
			continue
		}
		roots = append(roots, dir)
		added = append(added, dir)
	}
	if !changed {
		return nil, nil
	}
	hc.Roots = roots
	if hc.CreatedAt == "" {
		hc.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if err := SaveHomeConfig(c, hc); err != nil {
		return nil, err
	}
	l.Roots = roots
	return added, nil
}

// sameDir 报告 dir 是否已经是 roots 里的某个目录：路径相同，或解析符号链接
// 后指向同一处。
func sameDir(dir string, roots []string) bool {
	target := realPath(dir)
	for _, r := range roots {
		if rootKey(r) == rootKey(dir) || realPath(r) == target {
			return true
		}
	}
	return false
}

// realPath 返回解析符号链接之后的规范化绝对路径；解析失败（路径不存在等）时
// 退回 rootKey(p)。
func realPath(p string) string {
	if target, err := filepath.EvalSymlinks(p); err == nil {
		if abs, err := filepath.Abs(target); err == nil {
			return rootKey(abs)
		}
		return rootKey(target)
	}
	return rootKey(p)
}

// SetTheme 把外观偏好写进 config.json（其余内容原样保留）。mode 不是
// system、light、dark 时返回 ErrUnknownTheme，不写盘。config.json 损坏时
// 它被只含这一项偏好的新文件覆盖，与 updateRoots 对根目录列表的处理一致。
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
		if l.View != "" && l.View != ViewGrid {
			hc.View = l.View
		}
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

// SetView 把排列模式写进 config.json（其余内容原样保留）。v 不是 grid、
// list 时返回 ErrUnknownView，不写盘。与 SetTheme 一样是持 configMu 的
// 「读—改—写」；config.json 损坏时它被只含根目录列表、外观与这一项偏好的
// 新文件覆盖。
func (l *Library) SetView(v string) error {
	v, err := NormalizeView(v)
	if err != nil {
		return err
	}
	configMu.Lock()
	defer configMu.Unlock()
	c := &Config{Root: l.Roots[0], Home: l.Home}
	hc, _, err := LoadHomeConfig(c)
	if err != nil {
		hc = &HomeConfig{Roots: append([]string(nil), l.Roots...)}
		if l.Theme != ThemeSystem {
			hc.Theme = l.Theme
		}
	}
	if hc.CreatedAt == "" {
		hc.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	hc.View = v
	if err := SaveHomeConfig(c, hc); err != nil {
		return err
	}
	l.View = v
	return nil
}

// index 返回 root 在列表里的位置，不在时返回 -1。
func (l *Library) index(root string) int { return indexOf(l.Roots, root) }

// indexOf 返回 root 在 roots 里的位置（按 rootKey 比较），不在时返回 -1。
func indexOf(roots []string, root string) int {
	key := rootKey(root)
	for i, r := range roots {
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
