package store

// add_by_name.go — 按技能名从 GitHub 仓库安装（技能市场的安装入口）。
//
// 与 Add 的区别：Add 要么显式给了子目录，要么按仓库结构自动发现；这里只有
// owner/repo 与技能名（市场榜单给出的就是这两样），由仓库里的技能按名字对应
// 出子目录。clone 只做一次：发现与检出共用同一份浅 clone，安装后的元数据
// （来源、commit、子目录树哈希）与 Add 写出来的完全一样，之后的检查更新照常。

import (
	"path/filepath"
	"strings"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/locales"
)

// ErrSkillNotInRepo 表示仓库里没有要找的技能。它是哨兵：报错处用 sentinelErr
// 补上仓库与名字，errors.Is(err, ErrSkillNotInRepo) 对两种文字都成立。
var ErrSkillNotInRepo = i18n.NewError("storeErrNoSkillInRepo", func() string { return locales.StoreErrNoSkillInRepo() })

// AddSkillByName 从 owner/repo 的默认分支安装名为 name 的技能，装进根目录。
//
// 名字与仓库里技能的对应规则与 `npx skills add --skill` 一致（不分大小写）：
// 先看 SKILL.md 的 name，只有没写 name 的技能才看目录名（仓库根的技能按仓库名）。
// 恰好一个命中就装它；没有命中报 ErrSkillNotInRepo；命中多个报 *CandidatesError，
// 把选择权交还调用方。
//
// 安装名取 name（市场上显示的名字）；它不能当目录名用时退回 installDir 的
// 默认命名（SKILL.md 的 name，再退回目录名）。
func (c *Config) AddSkillByName(repo, name string) (*Result, error) {
	return c.AddSkillByNameWithProgress(repo, name, nil)
}

// AddSkillByNameWithProgress 为市场安装报告实际阶段，保持单次 clone 与原安装语义。
func (c *Config) AddSkillByNameWithProgress(repo, name string, progress InstallProgress) (*Result, error) {
	opts := &AddOptions{Repo: repo}
	if err := opts.validate(); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrSkillNotInRepo
	}
	if err := c.ensureDirs(); err != nil {
		return nil, err
	}
	url := githubURL(repo)
	progress.report(InstallDownload)
	u, err := cloneUpstream(url, "")
	if err != nil {
		return nil, err
	}
	defer u.Close()
	// 发现只需要各级 SKILL.md 与插件清单（同 Add 的自动发现）。
	if err := u.CheckoutDiscovery(); err != nil {
		return nil, err
	}
	progress.report(InstallInspect)
	dr, err := DiscoverSkills(u.dir)
	if err != nil {
		return nil, err
	}
	matches := matchSkillPaths(u.dir, repo, dr.Paths, name)
	switch len(matches) {
	case 0:
		return nil, sentinelErr(ErrSkillNotInRepo, "storeErrNoSkillNamedInRepo", func() string {
			return locales.StoreErrNoSkillNamedInRepo(name, repo)
		})
	case 1:
	default:
		return nil, &CandidatesError{Repo: repo, Paths: matches}
	}
	sub := matches[0]
	if sub == "." {
		sub = "" // 仓库根本身是技能。
	}
	src := Source{Type: SourceGitHub, URL: url, SubPath: sub, Commit: u.Commit}
	progress.report(InstallDownload)
	if err := c.checkoutSkill(u, &src); err != nil {
		return nil, err
	}
	progress.report(InstallInspect)
	installName := name
	if !validSkillDirName(installName) {
		installName = ""
	}
	return c.installDir(u.Dir(sub), src, installName, progress)
}

// matchSkillPaths 在候选 paths（相对仓库根，"." 为仓库根）里找名为 name 的技能。
//
// SKILL.md 写了 name 的技能只按 name 比（与 CLI 的 filterSkills 一致，目录名
// 不参与，免得同名目录把不相干的技能也拉进来）；没写 name 的技能按目录名比，
// 仓库根的技能目录名取仓库名（临时 clone 目录名是随机的）。
func matchSkillPaths(root, repo string, paths []string, name string) []string {
	var byMeta, byDir []string
	for _, p := range paths {
		dir := filepath.Join(root, filepath.FromSlash(p))
		if meta := readSkillName(dir); meta != "" {
			if strings.EqualFold(meta, name) {
				byMeta = append(byMeta, p)
			}
			continue
		}
		base := filepath.Base(dir)
		if p == "." {
			base = repoName(repo)
		}
		if strings.EqualFold(base, name) {
			byDir = append(byDir, p)
		}
	}
	if len(byMeta) > 0 {
		return byMeta
	}
	return byDir
}
