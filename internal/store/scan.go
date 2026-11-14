package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/locales"
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
	// ModTime 是这个技能「最近什么时候动过」，界面按它排序（默认新的在前）。
	// 取值口径见 skillModTime：技能目录自身、目录里 SKILL.md、目录里
	// .kura-meta.json 三个 mtime 里最大的一个，任一取不到就跳过它；三个都取不到时
	// 为零值（time.Time{}）。扫描时算一次放进来，之后和 Skill 的其他字段一样是只读
	// 快照——不要在每次构建界面时现算（那是磁盘 I/O，不能放在 UI 线程）。
	ModTime time.Time
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
// 链接目录（联接点、目录符号链接）与 Scan 同一口径：指向存在的目录才算（isSkillDirEntry）。
func (c *Config) CountEnabled() int {
	entries, err := os.ReadDir(c.Root)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") && isSkillDirEntry(c.Root, e) {
			n++
		}
	}
	return n
}

// scanDir 列出 dir 下的一级子目录，构造 Skill 视图。目录不存在视为空。
// 「子目录」按 isSkillDirEntry 判定：普通目录，以及指向存在目录的链接（Windows 联接点、
// 目录符号链接）；断链不显示。
func scanDir(dir string, state State) ([]*Skill, error) {
	f, err := os.Open(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, i18n.Wrapf(err, "storeErrReadDir", func() string { return locales.StoreErrReadDir(dir) })
	}
	defer f.Close()
	// Windows 的目录枚举可能把普通文件当作不存在或空目录。先核对同一
	// 句柄的类型，再从它枚举，避免路径在核对与读取之间被替换而误判。
	info, err := f.Stat()
	if err == nil && !info.IsDir() {
		err = ErrNotDir
	}
	if err != nil {
		return nil, i18n.Wrapf(err, "storeErrReadDir", func() string { return locales.StoreErrReadDir(dir) })
	}
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, i18n.Wrapf(err, "storeErrReadDir", func() string { return locales.StoreErrReadDir(dir) })
	}
	skills := make([]*Skill, 0, len(entries))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") || !isSkillDirEntry(dir, e) {
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
		ModTime:    skillModTime(dir),
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

// skillModTime 返回技能「最近动过」的时间：max(技能目录自身的 mtime、目录里 SKILL.md
// 的 mtime、目录里 .kura-meta.json 的 mtime)。任一取不到（不存在、无权限）就跳过
// 它；三个都取不到时返回零值。
//
// 为什么是这三个：
//   - 目录的 mtime 只在这个目录「直接增删条目」时才变（新增、删除、改名子目录或文件，
//     编辑器用「写临时文件再改名」保存 SKILL.md 也算），改一个已有文件的内容不会动它；
//   - 直接改 SKILL.md 的正文只动文件自己的 mtime，不动目录的；
//   - 安装、更新、导入元数据都会重写 .kura-meta.json。
//
// 三者取最大，才等于用户心里的「这个技能什么时候动过」。
//
// 为什么不递归整棵树取最大：那要给每个技能把所有文件都 Stat 一遍，kura list 与
// 界面扫描在带大 assets（图片、模型、数据集）的技能上会明显变慢；而 assets 内部的
// 改动并不影响用户对卡片顺序的直觉——SKILL.md 与元数据才是技能本身。所以这里只看
// 这三个路径，刻意不往下走。
//
// 一律用 os.Stat（跟随链接）而不是 os.Lstat：技能目录可能是联接点或目录符号链接，
// 用户关心的是链接指向的真实内容什么时候动过；链接本身的 mtime 只是建链的那一刻，
// 没有意义。目录里的 SKILL.md 与元数据路径经由链接解析，同样落在目标上。
//
// 回收站里的条目是 trash/<name> 这个容器目录（SKILL.md 与元数据在它的时间戳子目录
// 里），后两个路径取不到，只剩容器自己的 mtime，约等于它被移入回收站的时间。
func skillModTime(dir string) time.Time {
	var latest time.Time
	for _, p := range [...]string{dir, filepath.Join(dir, "SKILL.md"), filepath.Join(dir, MetaFileName)} {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if mt := info.ModTime(); mt.After(latest) {
			latest = mt
		}
	}
	return latest
}

// fileExists 报告路径是否存在。
func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
