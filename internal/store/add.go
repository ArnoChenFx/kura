package store

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ArnoChenFx/kura/internal/i18n"
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
	// Name 是安装后的技能名（可选）。为空时取 SKILL.md frontmatter 的
	// name（技能自己的名字），它不能当目录名用时退回来源目录名。
	// 元数据中的来源信息始终记录真实来源，与改名无关。
	Name string
}

// validate 校验来源参数：必须且只能提供一种来源。
func (o *AddOptions) validate() error {
	if (o.LocalPath == "") == (o.Repo == "") {
		return i18n.NewError("store.err.addSource")
	}
	if o.LocalPath != "" {
		return nil
	}
	if strings.Count(o.Repo, "/") != 1 || strings.ContainsAny(o.Repo, " \t\\") {
		return i18n.NewError("store.err.badRepo")
	}
	if strings.HasPrefix(o.SubPath, "/") || strings.Contains(o.SubPath, "..") {
		return i18n.NewError("store.err.badSubPath")
	}
	return nil
}

// Add 把一个技能安装进根目录：从本地目录或 GitHub 仓库。安装时会写入
// .kura-meta.json（记录来源、安装时间与内容哈希）。已存在同名技能时报错。
func (c *Config) Add(opts *AddOptions) (*Result, error) {
	if opts == nil {
		return nil, i18n.NewError("store.err.addOptionsNil")
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
				// 不能当技能名。frontmatter 的 name 可用时由 installDir 用它，
				// 否则默认取仓库名（owner/repo 的尾段）。
				if !validSkillDirName(readSkillName(srcDir)) {
					name = repoName(opts.Repo)
				}
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
		return "", "", i18n.Errorf("store.err.sourceNoSkillMD", tmp)
	default:
		return "", "", &CandidatesError{Repo: repo, Paths: dr.Paths}
	}
}

// resolveLocal 处理本地目录来源。
func (c *Config) resolveLocal(opts *AddOptions) (string, Source, error) {
	abs, err := filepath.Abs(opts.LocalPath)
	if err != nil {
		return "", Source{}, i18n.Wrapf(err, "store.err.resolveLocalPath")
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", Source{}, i18n.Wrapf(err, "store.err.sourceUnreadable")
	}
	if !info.IsDir() {
		return "", Source{}, i18n.Errorf("store.err.sourceNotDir", abs)
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
// 技能名优先用 nameOverride（AddOptions.Name，UI 的「安装名称」/CLI 的
// --name），否则取技能自己声明的名字（见 skillDirName）；重名或名字
// 非法时报错。元数据里的来源始终是真实来源（srcDir/src），与改名无关。
func (c *Config) installDir(srcDir string, src Source, nameOverride string) (*Result, error) {
	name := nameOverride
	if name == "" {
		name = skillDirName(srcDir, filepath.Base(srcDir))
	}
	if name == "." || name == string(filepath.Separator) || strings.ContainsAny(name, `\/`) {
		return nil, i18n.Errorf("store.err.deriveSkillName", srcDir)
	}
	dst := c.SkillPath(name)
	if fileExists(dst) {
		return nil, i18n.Errorf("store.err.skillExists", name)
	}
	if !fileExists(filepath.Join(srcDir, "SKILL.md")) {
		return nil, i18n.Errorf("store.err.sourceNoSkillMD", srcDir)
	}
	// GitHub 来源的仓库根技能目录里带着 clone 的 .git，不能装进技能根目录，
	// 复制时就跳过它。
	if err := copyTree(srcDir, dst, src.Type == SourceGitHub); err != nil {
		return nil, i18n.Wrapf(err, "store.err.installSkill", name)
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

// skillDirName 推导安装后的默认技能名：优先 SKILL.md frontmatter 里的 name
// （技能自己的名字，比来源目录名权威——仓库里 skills/engineering/tdd 的
// 目录名与技能名一致只是巧合）；它不能当目录名用时退回 base（来源目录名），
// 与改动前一致。显式给了名字时不会走到这里（installDir 里 nameOverride
// 优先）。
func skillDirName(dir, base string) string {
	if name := readSkillName(dir); validSkillDirName(name) {
		return name
	}
	return base
}

// validSkillDirName 判断一个名字能不能当技能目录名用（frontmatter 的 name
// 来自仓库，不讲究时不能让安装失败，调用方退回来源目录名）。
//
// 为什么不复用 cli_support.go 的 ValidName：那个函数守的是「名字拼进路径
// 不会越界」（Enable/Disable/Remove 等破坏性操作），只拦空、"."、".."、
// 路径分隔符与 ".."，放得过 Windows 上根本建不出目录的名字（尾随的空格或
// 点、控制字符、`<>:"|?*`、CON 这类设备名）。这里要的是「这个名字能不能
// 变成一个真实目录」，判据更强；反过来它也不做 ValidName 的
// filepath.Base(name) == name（那一条在 Windows 上放得过 "a:b" 与尾随
// 空格，正是这里要拦的）。
func validSkillDirName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	// 路径分隔符与 ".."：名字只能是单层目录，不能越界。
	if strings.ContainsAny(name, `\/`) || strings.Contains(name, "..") {
		return false
	}
	// Windows 上文件名的非法字符，外加控制字符。
	if strings.ContainsAny(name, `<>:"|?*`) {
		return false
	}
	for _, r := range name {
		if r < 0x20 {
			return false
		}
	}
	// Windows 上以空格或点结尾的名字建不出目录。
	if strings.HasSuffix(name, " ") || strings.HasSuffix(name, ".") {
		return false
	}
	// Windows 的设备名：不分大小写，带扩展名同样是保留名（"con.md" 也不行）。
	base := name
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return false
	}
	return true
}

// repoName 取 owner/repo 里的仓库名（尾段），仓库根本身是技能时的默认
// 技能名回退（临时的 clone 目录名是随机的，不能当技能名）。
func repoName(repo string) string {
	return repo[strings.LastIndex(repo, "/")+1:]
}

// AddSpec 是 AddMany 里单个技能的请求。
//
// 与 AddOptions 的关系：AddOptions 描述「来源」，AddSpec 描述「这个来源里的
// 哪一个技能、装成什么名字」。
type AddSpec struct {
	// SubPath 是仓库内的技能目录（相对仓库根的 slash 路径）。空串或 "."
	// 表示技能就是仓库根（DiscoverSkills 用 "." 表示这一情形）。
	SubPath string
	// Name 是安装后的技能名（可选）。为空时取该技能 SKILL.md frontmatter
	// 的 name，再退回来源目录名（仓库根时退回仓库名）。与 AddOptions.Name
	// 同义，只是按技能逐个给。
	Name string
}

// AddResult 是 AddMany 里单个技能的结果：SubPath 与入参一一对应（原样
// 回填），Name 是实际装成的技能名（失败时为空），成功时 Result 非 nil，
// 失败时 Err 非 nil。
//
// 与 UpdateResult 同形：批量调用方可以只遍历结果，不必自己配对入参。
type AddResult struct {
	SubPath string
	Name    string
	Result  *Result
	Err     error
}

// AddMany 从同一个 GitHub 仓库（同一个 ref）一次安装多个技能。
//
// 「只 clone 一次」：同一批技能都在同一个仓库里，cloneUpstream 只跑一次，
// 子目录树哈希用一次 Trees（一个 git 进程）取全，再一次 Checkout 把选中的
// 目录一起稀疏检出（对比逐个 Add 会把仓库克隆 N 遍）。
//
// 结果与 specs 一一对应、顺序相同；单个技能失败不影响其他技能（比如目标
// 目录已存在同名技能、某个子目录在上游不存在），失败的 Err 里带上它的
// 子目录，调用方能看出是哪一个失败。
//
// 同一个 SubPath 给两次不去重（这是调用方的问题，不替它猜）：两项会各走一遍
// 安装，第二次装到同一个目标名就报「根目录已存在同名技能」；给了不同的 Name
// 时就是「同一份内容装成两个技能」，这是合法用法，按路径去重会把这种用法一起
// 丢掉。能安全合并的只是 git 那边的查询与检出（uniqueSubs 已经做了）。
//
// 与 Add 的关系：Add 是「一个来源、自动发现或显式指定一个技能」，语义与
// 错误文案都不变；AddMany 只做批量，要求调用方给出明确的子目录列表，
// 不做自动发现，也不会返回 *CandidatesError。
func (c *Config) AddMany(repo, ref string, specs []AddSpec) []AddResult {
	out := make([]AddResult, len(specs))
	for i, spec := range specs {
		out[i].SubPath = spec.SubPath
	}
	// 来源校验复用 Add 的那一套（owner/repo 格式）。
	opts := &AddOptions{Repo: repo, Ref: ref}
	if err := opts.validate(); err != nil {
		failAllAdds(out, err)
		return out
	}
	if len(specs) == 0 {
		return out
	}
	if err := c.ensureDirs(); err != nil {
		failAllAdds(out, err)
		return out
	}
	// 逐项校验子目录（非法的项只让自己失败）：要装的就是这些技能。
	items := make([]addItem, 0, len(specs))
	for i, spec := range specs {
		sub := cleanSubPath(spec.SubPath)
		if strings.HasPrefix(spec.SubPath, "/") || strings.Contains(sub, "..") {
			out[i].Err = i18n.Errorf("store.err.badSkillSubPath", spec.SubPath)
			continue
		}
		if sub == "." {
			sub = "" // DiscoverSkills 用 "." 表示仓库根本身是技能。
		}
		items = append(items, addItem{index: i, sub: sub, name: spec.Name})
	}
	if len(items) == 0 {
		return out
	}
	url := githubURL(repo)
	u, err := cloneUpstream(url, ref)
	if err != nil {
		failItems(out, items, err)
		return out
	}
	defer u.Close()
	// 一次取全部子目录的树哈希（只读树对象，不下载文件）。某个目录不
	// 存在只让它自己失败。
	trees, err := u.Trees(uniqueSubs(items))
	if err != nil {
		failItems(out, items, err)
		return out
	}
	var todo []addItem
	for _, it := range items {
		if r := trees[it.sub]; r.Err != nil {
			out[it.index].Err = i18n.Wrapf(r.Err, "store.err.installSkill", subLabel(it.sub))
			continue
		}
		todo = append(todo, it)
	}
	if len(todo) == 0 {
		return out
	}
	// 一次检出选中的全部目录（同一批只跑一次 git）。
	if err := u.Checkout(uniqueSubs(todo)); err != nil {
		failItems(out, todo, err)
		return out
	}
	for _, it := range todo {
		src := Source{
			Type: SourceGitHub, URL: url, Ref: ref, SubPath: it.sub,
			Commit: u.Commit, Tree: trees[it.sub].Tree,
		}
		name := it.name
		if name == "" && it.sub == "" {
			// 仓库根本身是技能：临时目录名是随机的，不能当技能名；
			// frontmatter 的 name 可用时由 installDir 用它。
			if !validSkillDirName(readSkillName(u.Dir(it.sub))) {
				name = repoName(repo)
			}
		}
		res, err := c.installDir(u.Dir(it.sub), src, name)
		if err != nil {
			out[it.index].Err = i18n.Wrapf(err, "store.err.installSkill", subLabel(it.sub))
			continue
		}
		out[it.index].Name = res.ID
		out[it.index].Result = res
	}
	return out
}

// addItem 是 AddMany 内部的条目：index 是它在结果里的位置，sub 是规范化
// 后的子目录（空串 = 仓库根），name 是调用方给的安装名（可空）。
// 位置与名字都记下来，后面的循环就不必再按 index 回查入参。
type addItem struct {
	index int
	sub   string
	name  string
}

// uniqueSubs 返回 items 里出现过的子目录（去重，保留首次出现的顺序）：
// git 那边每种子目录信息只查一次、只写一条稀疏检出模式。
func uniqueSubs(items []addItem) []string {
	seen := map[string]bool{}
	var out []string
	for _, it := range items {
		if seen[it.sub] {
			continue
		}
		seen[it.sub] = true
		out = append(out, it.sub)
	}
	return out
}

// failAllAdds 把同一个错误写进全部结果项。
func failAllAdds(out []AddResult, err error) {
	for i := range out {
		out[i].Err = err
	}
}

// failItems 把错误写进给定条目对应的结果项。
func failItems(out []AddResult, items []addItem, err error) {
	for _, it := range items {
		out[it.index].Err = err
	}
}

// subLabel 是错误信息里给用户看的技能标识：子目录为空表示技能就是仓库根
// （标识本身也是界面文案，两种语言下都显示「仓库根」/“the repository root”）。
func subLabel(sub string) string {
	if sub == "" {
		return i18n.T("store.word.repoRoot")
	}
	return sub
}
