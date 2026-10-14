package store

import (
	"fmt"
	"os"
	"time"

	"github.com/ArnoChenFx/kura/internal/i18n"
)

// Update 从记录的来源重新拉取技能并替换根目录中的内容，等价于只含一个
// 技能的 UpdateMany。
func (c *Config) Update(name string) (*Result, error) {
	r := c.UpdateMany([]string{name})[0]
	return r.Result, r.Err
}

// UpdateResult 是 UpdateMany 里单个技能的结果：成功时 Result 非 nil，
// 失败时 Err 非 nil。
type UpdateResult struct {
	Name   string
	Result *Result
	Err    error
}

// UpdateMany 依次更新 names 里的技能，结果与 names 一一对应、顺序相同。
// 流程：快照旧内容 → 获取新内容 → 原子替换 → 刷新元数据（updatedAt/commit/树哈希/内容哈希）。
// 来源为 local 时从原路径复制（等价于重装）；来源为 git 时同一仓库（URL+ref）
// 只 clone 一次，并且只稀疏检出这些技能的子目录。
// 单个技能失败不影响其他技能；拉取失败时技能保持原样，替换成功后快照保留在 snapshots/ 下。
func (c *Config) UpdateMany(names []string) []UpdateResult {
	out := make([]UpdateResult, len(names))
	groups := map[repoKey][]int{}
	var order []repoKey
	metas := make([]*Meta, len(names))
	for i, name := range names {
		out[i].Name = name
		meta, err := c.sourceMeta(name, msgMetaMissingUpdate)
		if err != nil {
			out[i].Err = err
			continue
		}
		metas[i] = meta
		if meta.Source.Type == SourceLocal {
			out[i].Result, out[i].Err = c.replaceSkill(name, meta.Source.Path, meta, "", "")
			continue
		}
		k := newRepoKey(meta.Source.URL, meta.Source.Ref)
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], i)
	}
	for _, k := range order {
		c.updateRepo(groups[k], names, metas, out)
	}
	return out
}

// updateRepo 更新同一仓库（同 URL 与 ref）里的一组技能：只 clone 一次，
// 只稀疏检出这些技能的子目录。某个技能的子目录缺失只让它自己失败。
func (c *Config) updateRepo(idx []int, names []string, metas []*Meta, out []UpdateResult) {
	fail := func(is []int, err error) {
		for _, i := range is {
			out[i].Err = err
		}
	}
	u, err := cloneUpstream(metas[idx[0]].Source.URL, metas[idx[0]].Source.Ref)
	if err != nil {
		fail(idx, err)
		return
	}
	defer u.Close()
	subs := make([]string, len(idx))
	for j, i := range idx {
		subs[j] = metas[i].Source.SubPath
	}
	trees, err := u.Trees(subs)
	if err != nil {
		fail(idx, err)
		return
	}
	var ok []int
	var okSubs []string
	for _, i := range idx {
		sub := metas[i].Source.SubPath
		if r := trees[sub]; r.Err != nil {
			out[i].Err = r.Err
			continue
		}
		ok = append(ok, i)
		okSubs = append(okSubs, sub)
	}
	if len(ok) == 0 {
		return
	}
	if err := u.Checkout(okSubs); err != nil {
		fail(ok, err)
		return
	}
	for _, i := range ok {
		sub := metas[i].Source.SubPath
		out[i].Result, out[i].Err = c.replaceSkill(names[i], u.Dir(sub), metas[i], u.Commit, trees[sub].Tree)
	}
}

// sourceMeta 读取根目录下技能 name 的元数据并校验来源字段齐全。
// 技能不存在 → ErrUnknownSkill；元数据缺失/损坏/来源不完整 → 明确报错。
// missingKey 是「元数据缺失」时的整句文案 key（更新与检查更新各一句，见
// catalog.go 的 msgMetaMissingUpdate / msgMetaMissingCheck）。
func (c *Config) sourceMeta(name, missingKey string) (*Meta, error) {
	src := c.SkillPath(name)
	if !fileExists(src) {
		return nil, fmt.Errorf("%w: %s", ErrUnknownSkill, name)
	}
	// 缺失或损坏时无法知道来源，直接报错（不 panic）。
	res := readMeta(src)
	if res.Err != nil {
		return nil, i18n.Wrapf(res.Err, "store.err.metaUnreadable", name)
	}
	if !res.Found {
		return nil, i18n.Errorf(missingKey, name)
	}
	meta := res.Meta
	switch meta.Source.Type {
	case SourceLocal:
		if meta.Source.Path == "" {
			return nil, i18n.NewError("store.err.metaNoLocalPath")
		}
		if !fileExists(meta.Source.Path) {
			return nil, i18n.Errorf("store.err.localSourceGone", meta.Source.Path)
		}
	case SourceGitHub:
		if meta.Source.URL == "" {
			return nil, i18n.NewError("store.err.metaNoGithubURL")
		}
		meta.Source.SubPath = cleanSubPath(meta.Source.SubPath)
	default:
		return nil, i18n.Errorf("store.err.unknownSourceType", meta.Source.Type)
	}
	return meta, nil
}

// replaceSkill 用 newDir 的内容替换根目录下的技能 name：先快照，再原子替换，
// 最后刷新元数据。commit/tree 是新内容对应的上游 commit 与子目录树哈希
// （local 来源为空串）。
func (c *Config) replaceSkill(name, newDir string, meta *Meta, commit, tree string) (*Result, error) {
	src := c.SkillPath(name)
	// 快照旧内容（含旧元数据文件本身，用于完整还原）。
	now := time.Now()
	snapDir, err := c.snapshotSkill(name, src, now)
	if err != nil {
		return nil, err
	}
	// 原子替换：先删旧的，再把新内容移进来（同一磁盘上 Rename 是原子的）。
	staging := src + ".kura-new"
	os.RemoveAll(staging)
	// 技能是仓库根或本地来源本身是 git 仓库时目录里带着 .git，不能装进技能目录。
	if err := copyTree(newDir, staging, true); err != nil {
		os.RemoveAll(staging)
		return nil, i18n.Wrapf(err, "store.err.prepareContent")
	}
	os.RemoveAll(src)
	if err := os.Rename(staging, src); err != nil {
		// 替换失败：从快照恢复旧内容。
		restoreErr := copyDir(snapDir, src)
		if restoreErr != nil {
			return nil, i18n.Wrapf(err, "store.err.replaceAndRestore", snapDir)
		}
		return nil, i18n.Wrapf(err, "store.err.replaceRestored")
	}
	// 刷新元数据：保留 installedAt/pluginName/notes，更新 updatedAt、commit、树哈希、内容哈希。
	meta.UpdatedAt = now.UTC().Format(time.RFC3339)
	meta.Source.Commit = commit
	meta.Source.Tree = tree
	hash, err := HashDir(src)
	if err != nil {
		return nil, err
	}
	meta.ContentHash = hash
	if err := writeMeta(src, meta); err != nil {
		return nil, i18n.Wrapf(err, "store.err.updateMeta")
	}
	return &Result{ID: name, State: StateEnabled, Dir: src, SnapshotDir: snapDir}, nil
}
