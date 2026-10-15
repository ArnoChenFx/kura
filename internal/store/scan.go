package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ArnoChenFx/kura/internal/i18n"
)

// State 是技能的三种存放状态：根目录（enabled）、disabled、trash。
type State string

// Skill 是一个技能文件夹的完整视图。
type Skill struct {
	Name       string
	Dir        string
	State      State
	HasSKILLMD bool
	// Description 是 SKILL.md frontmatter 的 description，没有时为空串。
	Description string
	Meta        *Meta
	MetaErr     string
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

// CountEnabled 返回根目录里技能文件夹的数量，只列目录、不读元数据也不算
// 哈希，供界面的目录徽标使用。目录不存在或读失败时返回 0。
func (c *Config) CountEnabled() int {
	entries, err := os.ReadDir(c.Root)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			n++
		}
	}
	return n
}

// scanDir 列出 dir 下的一级子目录，构造 Skill 视图。目录不存在视为空。
func scanDir(dir string, state State) ([]*Skill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, i18n.Wrapf(err, "store.err.readDir", dir)
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

// buildSkill 构造单个 Skill：读元数据（损坏时降级并记录原因）。不在这里算内容
// 哈希：它要读完技能目录里的每个文件，列表扫描用不到；需要核对漂移的调用方
// （kura doctor）自己调 HashDir。
func buildSkill(dir, name string, state State) *Skill {
	s := &Skill{
		Name:       name,
		Dir:        dir,
		State:      state,
		HasSKILLMD: fileExists(filepath.Join(dir, "SKILL.md")),
	}
	if s.HasSKILLMD {
		s.Description = readDescription(dir)
	}
	res := readMeta(dir)
	if res.Err != nil {
		s.MetaErr = res.Err.Error()
	} else if res.Found {
		s.Meta = res.Meta
	}
	return s
}

// fileExists 报告路径是否存在。
func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
