package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// State 是技能的三种存放状态：根目录（enabled）、disabled、trash。
type State string

// Skill 是一个技能文件夹的完整视图。
type Skill struct {
	Name        string
	Dir         string
	State       State
	HasSKILLMD  bool
	Meta        *Meta
	MetaErr     string
	ContentHash string
}

const (
	StateEnabled  State = "enabled"
	StateDisabled State = "disabled"
	StateTrashed  State = "trashed"
)

// Scan 遍历根目录，返回全部技能。目录不存在返回空列表。单个技能元数据
// 损坏不影响整体扫描：该技能 Meta 为 nil，MetaErr 记录原因。
func (c *Config) Scan() ([]*Skill, error) {
	return scanDir(c.Root, StateEnabled)
}

// ScanDisabled 遍历 disabled 目录。
func (c *Config) ScanDisabled() ([]*Skill, error) {
	return scanDir(c.DisabledRoot(), StateDisabled)
}
func (c *Config) ScanTrash() ([]*Skill, error) { return scanDir(c.TrashRoot(), StateTrashed) }

// ScanTrash 遍历 trash 目录。

// ScanAll 返回 enabled + disabled + trash 的合集。单个目录读失败不致命。
func (c *Config) ScanAll() ([]*Skill, error) {
	enabled, err := c.Scan()
	if err != nil {
		return nil, err
	}
	if disabled, derr := c.ScanDisabled(); derr == nil {
		enabled = append(enabled, disabled...)
	}
	if trashed, terr := c.ScanTrash(); terr == nil {
		enabled = append(enabled, trashed...)
	}
	return enabled, nil
}

// scanDir 列出 dir 下的一级子目录，构造 Skill 视图。目录不存在视为空。
func scanDir(dir string, state State) ([]*Skill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: 读取目录 %s 失败: %w", dir, err)
	}
	skills := make([]*Skill, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		skills = append(skills, buildSkill(filepath.Join(dir, e.Name()), e.Name(), state))
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, nil
}

// buildSkill 构造单个 Skill：读元数据（损坏时降级并记录原因），再计算内容哈希。
func buildSkill(dir, name string, state State) *Skill {
	s := &Skill{
		Name:       name,
		Dir:        dir,
		State:      state,
		HasSKILLMD: fileExists(filepath.Join(dir, "SKILL.md")),
	}
	res := readMeta(dir)
	if res.Err != nil {
		s.MetaErr = res.Err.Error()
	} else if res.Found {
		s.Meta = res.Meta
	}
	s.ContentHash, _ = HashDir(dir)
	return s
}

// fileExists 报告路径是否存在。
func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
