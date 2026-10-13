package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 技能来源类型常量。
const (
	SourceLocal  = "local"
	SourceGitHub = "github"
)

// AddOptions 是 Add 的来源描述：LocalPath 与 Repo 二选一。
type AddOptions struct {
	// LocalPath 是本地技能目录（内含 SKILL.md 的目录本身）。
	LocalPath string
	// Repo 是 GitHub 的 owner/repo。
	Repo string
	// SubPath 是仓库内技能目录（可选）。为空时默认用 Repo 根目录；
	// 安装路径会先做仓库内技能发现（见 discoverForAdd）：仓库根是技能
	// 或恰好只有一个候选时直接装，多个候选时报 CandidatesError 交由
	// 调用方展示，零个候选时报「来源目录缺少 SKILL.md」。
	// 显式给了 SubPath 时按用户指定的目录安装，绝不自动换别的。
	SubPath string
	// Ref 是分支/标签/commit（可选，默认仓库默认分支）。
	Ref string
	// Name 是安装后的技能名（可选）。为空时取来源目录名。
	// 元数据中的来源信息始终记录真实来源，与改名无关。
	Name string
}

// validate 校验来源参数：必须且只能提供一种来源。
func (o *AddOptions) validate() error {
	if (o.LocalPath == "") == (o.Repo == "") {
		return errors.New("store: LocalPath 与 Repo 必须且只能提供一个")
	}
	if o.LocalPath != "" {
		return nil
	}
	if strings.Count(o.Repo, "/") != 1 || strings.ContainsAny(o.Repo, " \t\\") {
		return errors.New("store: Repo 格式应为 owner/repo")
	}
	if strings.HasPrefix(o.SubPath, "/") || strings.Contains(o.SubPath, "..") {
		return errors.New("store: SubPath 不能以 / 开头或包含 ..")
	}
	return nil
}

// Add 把一个技能安装进根目录：从本地目录或 GitHub 仓库。安装时会写入
// .kura-meta.json（记录来源、安装时间与内容哈希）。已存在同名技能时报错。
func (c *Config) Add(opts *AddOptions) (*Result, error) {
	if opts == nil {
		return nil, errors.New("store: AddOptions 不能为空")
	}
	if err := opts.validate(); err != nil {
		return nil, err
	}
	if err := c.ensureDirs(); err != nil {
		return nil, err
	}
	var srcDir string
	var src Source
	var err error
	name := opts.Name
	if opts.LocalPath != "" {
		srcDir, src, err = c.resolveLocal(opts)
		if err != nil {
			return nil, err
		}
	} else {
		var u *upstream
		u, src, err = c.resolveGitHub(opts)
		if err != nil {
			return nil, err
		}
		defer u.Close()
		srcDir = u.Dir(src.SubPath)
		// GitHub 来源且未显式指定子目录：先发现仓库内的技能再决定装什么。
		// 显式给了 SubPath 时跳过（用户指定的子目录一律照装，不自作主张）。
		if opts.SubPath == "" {
			dir, sub, err := discoverForAdd(opts.Repo, u.dir)
			if err != nil {
				return nil, err
			}
			srcDir = dir
			src.SubPath = sub
			// 选定技能后才检出它的文件，并记下子目录树哈希。
			if err := c.checkoutSkill(u, &src); err != nil {
				return nil, err
			}
			if sub == "" && name == "" {
				// 仓库根本身是技能：临时目录名是随机的（kura-upstream-NNN），
				// 不能当技能名；默认取仓库名（owner/repo 的尾段）。
				name = opts.Repo[strings.LastIndex(opts.Repo, "/")+1:]
			}
		}
	}
	return c.installDir(srcDir, src, name)
}

// discoverForAdd 在 clone 好的仓库 tmp 里决定要安装的技能目录，返回
// （技能目录绝对路径，相对仓库根的 slash 路径）。subPath 会写进元数据的
// Source.SubPath，让后续 Update 能照它重新拉取；仓库根本身是技能时为空串。
// 恰好一个候选（含「仓库根本身是技能」）→ 直接返回它；多个候选 →
// 返回 *CandidatesError 把选择权交还调用方；零个 → 返回与旧路径一致
// 的「来源目录缺少 SKILL.md」错误（仓库根没有 SKILL.md 时根目录安装
// 本来就会失败，语义与显式 --sub-path 时完全相同）。
func discoverForAdd(repo, tmp string) (string, string, error) {
	dr, err := DiscoverSkills(tmp)
	if err != nil {
		return "", "", err
	}
	switch len(dr.Paths) {
	case 1:
		if dr.Paths[0] == "." {
			return tmp, "", nil // 仓库根本身就是技能。
		}
		return filepath.Join(tmp, filepath.FromSlash(dr.Paths[0])), dr.Paths[0], nil
	case 0:
		return "", "", fmt.Errorf("store: 来源目录缺少 SKILL.md: %s", tmp)
	default:
		return "", "", &CandidatesError{Repo: repo, Paths: dr.Paths}
	}
}

// resolveLocal 处理本地目录来源。
func (c *Config) resolveLocal(opts *AddOptions) (string, Source, error) {
	abs, err := filepath.Abs(opts.LocalPath)
	if err != nil {
		return "", Source{}, fmt.Errorf("store: 解析路径失败: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", Source{}, fmt.Errorf("store: 来源目录不可读: %w", err)
	}
	if !info.IsDir() {
		return "", Source{}, fmt.Errorf("store: 来源不是目录: %s", abs)
	}
	return abs, Source{Type: SourceLocal, Path: abs}, nil
}

// resolveGitHub 浅 clone owner/repo（只取树，不取文件），再按需稀疏检出：
// 仓库里与技能无关的文件不下载。返回 clone（调用方 defer Close）与来源。
//   - 显式给了 SubPath：只检出该目录，来源里记下 SubPath、commit 与子目录树哈希；
//   - 没给 SubPath：只检出发现技能要用的文件（各级 SKILL.md 与插件清单，见
//     CheckoutDiscovery），来源里 SubPath/Tree 留空，由 Add 经 discoverForAdd
//     选定技能后补全；DiscoverRepoSkills 直接在这份检出上做发现。
//
// 这里保持单一职责：只负责克隆与按显式 SubPath 定位。
func (c *Config) resolveGitHub(opts *AddOptions) (*upstream, Source, error) {
	url := githubURL(opts.Repo)
	sub := cleanSubPath(opts.SubPath)
	u, err := cloneUpstream(url, opts.Ref)
	if err != nil {
		return nil, Source{}, err
	}
	// 记录 commit 与子目录树哈希，检查更新时据此判断上游是否变化。
	src := Source{Type: SourceGitHub, URL: url, Ref: opts.Ref, SubPath: sub, Commit: u.Commit}
	if sub == "" {
		if err := u.CheckoutDiscovery(); err != nil {
			u.Close()
			return nil, Source{}, err
		}
		return u, src, nil
	}
	if err := c.checkoutSkill(u, &src); err != nil {
		u.Close()
		return nil, Source{}, err
	}
	return u, src, nil
}

// checkoutSkill 检出 src.SubPath 指向的技能目录，并把它的树哈希写进 src.Tree。
// 目录在上游不存在（或不是目录）时报错。
func (c *Config) checkoutSkill(u *upstream, src *Source) error {
	tree, err := u.Tree(src.SubPath)
	if err != nil {
		return err
	}
	if err := u.Checkout([]string{src.SubPath}); err != nil {
		return err
	}
	src.Tree = tree
	return nil
}

// installDir 把 srcDir 复制为根目录下的技能文件夹并写入元数据。
// 技能名优先用 nameOverride（AddOptions.Name），否则取来源目录名；
// 重名或名字非法时报错。元数据里的来源始终是真实来源（srcDir/src），
// 与改名无关。
func (c *Config) installDir(srcDir string, src Source, nameOverride string) (*Result, error) {
	name := nameOverride
	if name == "" {
		name = filepath.Base(srcDir)
	}
	if name == "." || name == string(filepath.Separator) || strings.ContainsAny(name, `\/`) {
		return nil, fmt.Errorf("store: 无法从来源推导技能名: %s", srcDir)
	}
	dst := c.SkillPath(name)
	if fileExists(dst) {
		return nil, fmt.Errorf("store: 根目录已存在同名技能 %s", name)
	}
	if !fileExists(filepath.Join(srcDir, "SKILL.md")) {
		return nil, fmt.Errorf("store: 来源目录缺少 SKILL.md: %s", srcDir)
	}
	// GitHub 来源的仓库根技能目录里带着 clone 的 .git，不能装进技能根目录，
	// 复制时就跳过它。
	if err := copyTree(srcDir, dst, src.Type == SourceGitHub); err != nil {
		return nil, fmt.Errorf("store: 安装 %s 失败: %w", name, err)
	}
	hash, err := HashDir(dst)
	if err != nil {
		os.RemoveAll(dst)
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	meta := &Meta{
		ID:          name,
		DisplayName: name,
		Source:      src,
		InstalledAt: now,
		UpdatedAt:   now,
		ContentHash: hash,
	}
	if err := writeMeta(dst, meta); err != nil {
		os.RemoveAll(dst)
		return nil, err
	}
	return &Result{ID: name, State: StateEnabled, Dir: dst}, nil
}
