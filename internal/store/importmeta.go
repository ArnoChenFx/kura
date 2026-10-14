package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ArnoChenFx/kura/internal/i18n"
)

// LockEntry 是 .skill-lock.json 里一个技能条目的字段（v3 格式）。
type LockEntry struct {
	Source          string `json:"source"`          // 如 "owner/repo" 或本地路径
	SourceType      string `json:"sourceType"`      // github | local
	SourceURL       string `json:"sourceUrl"`       // 完整 clone 地址
	SkillPath       string `json:"skillPath"`       // 仓库内路径，指向 SKILL.md 或技能目录
	SkillFolderHash string `json:"skillFolderHash"` // 安装器自己的哈希，kura 不使用
	PluginName      string `json:"pluginName,omitempty"`
	InstalledAt     string `json:"installedAt"`
	UpdatedAt       string `json:"updatedAt"`
}

// LockFile 是 ~/.agents/.skill-lock.json 的顶层结构。
type LockFile struct {
	Version int                  `json:"version"`
	Skills  map[string]LockEntry `json:"skills"`
}

// ReadLockFile 读取并解析 .skill-lock.json（只读，绝不改写）。文件不存在
// 返回 (nil, false, nil)。
func ReadLockFile(path string) (*LockFile, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, i18n.Wrapf(err, "store.err.readFile", path)
	}
	var lf LockFile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, false, i18n.Wrapf(err, "store.err.parseFile", path)
	}
	return &lf, true, nil
}

// DefaultLockPath 返回 ~/.agents/.skill-lock.json 的默认路径。
func DefaultLockPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", i18n.Wrapf(err, "store.err.userHome")
	}
	return filepath.Join(home, ".agents", ".skill-lock.json"), nil
}

// lockSubPath 把 lock 条目的 skillPath（指向 SKILL.md 或技能目录）换算为
// 技能文件夹在仓库内的相对路径：父目录即技能文件夹；skillPath 就是目录时原样返回。
func lockSubPath(e LockEntry) string {
	p := filepath.ToSlash(e.SkillPath)
	if p == "" {
		return ""
	}
	if strings.HasSuffix(p, "/SKILL.md") || p == "SKILL.md" {
		return strings.TrimSuffix(strings.TrimSuffix(p, "SKILL.md"), "/")
	}
	return strings.TrimSuffix(p, "/")
}

// ErrImportDefaultOnly 表示对追加的根目录做「导入元数据」：.skill-lock.json
// 只描述默认根目录，别的目录不能用它导入。
var ErrImportDefaultOnly = i18n.NewError("store.err.importDefaultOnly")

// CanImportMeta 报告该根目录能否「导入元数据」：只有默认根目录可以。
// 追加的根目录（Library.Config 给它们非空的 Scope）不行。界面与命令行
// 都以此为准，不各自判断。
func (c *Config) CanImportMeta() bool { return c.Scope == "" }

// ImportMeta 用 .skill-lock.json 补齐根目录下缺失的 .kura-meta.json。
// 只处理 lock 文件中列出的技能；已有元数据的技能一律跳过（绝不覆盖）。
// 追加的根目录返回 ErrImportDefaultOnly。返回本次导入的技能 id 列表。
func (c *Config) ImportMeta(lockPath string) ([]string, error) {
	if !c.CanImportMeta() {
		return nil, fmt.Errorf("%w: %s", ErrImportDefaultOnly, c.Root)
	}
	if lockPath == "" {
		var err error
		if lockPath, err = DefaultLockPath(); err != nil {
			return nil, err
		}
	}
	lf, found, err := ReadLockFile(lockPath)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	skills, err := c.Scan()
	if err != nil {
		return nil, err
	}
	var imported []string
	for _, s := range skills {
		if s.Meta != nil {
			continue // 已有元数据，不覆盖。
		}
		e, ok := lf.Skills[s.Name]
		if !ok {
			continue // lock 文件未列出该技能。
		}
		m := &Meta{
			ID:          s.Name,
			DisplayName: s.Name,
			PluginName:  e.PluginName,
			InstalledAt: e.InstalledAt,
			UpdatedAt:   e.UpdatedAt,
		}
		switch e.SourceType {
		case "github":
			m.Source = Source{Type: "github", URL: e.SourceURL, SubPath: lockSubPath(e)}
		case "local":
			m.Source = Source{Type: "local", Path: e.Source}
		default:
			// 未知来源类型：跳过，避免写入无法追溯来源的元数据。
			continue
		}
		if err := writeMeta(s.Dir, m); err != nil {
			return imported, i18n.Wrapf(err, "store.err.importMetaWrite", s.Name)
		}
		imported = append(imported, s.Name)
	}
	return imported, nil
}
