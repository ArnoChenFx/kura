package store

// PreviewUpdate（里程碑 4）：在真正执行 Update 之前回答「更新会改什么」。
//
// 设计要点：
//   - 只读操作：除临时目录外绝不修改磁盘上的任何内容（不写快照、不动技能
//     目录、不改元数据）。git 来源先 ls-remote、再只取树、最后才稀疏检出
//     技能子目录（见 upstream.go），临时目录在返回前清理；local 来源直接只读原路径。
//   - v1 不产出 unified hunk（godiff 是整个 GUI 应用而非库，无法复用其
//     diff 核心），只产出文件级清单：每个文件标记 added/removed/changed，
//     changed 附带新增/删除行数（LCS 最小编辑距离，超大文件退化为多重集差）。
//   - 目录级 OldHash/NewHash 与 Meta.ContentHash 同口径（HashDir 算法），
//     NewHash 额外排除 .git——Update 在替换前会删掉 clone 带来的 .git，
//     因此预览的 NewHash 与更新完成后 HashDir 的结果一致，可直接对比。

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/locales"
)

// 文件差异状态常量（CLI JSON、UI 文案共用的稳定词汇）。
const (
	DiffAdded   = "added"   // 上游新增的文件
	DiffRemoved = "removed" // 上游已删除的文件
	DiffChanged = "changed" // 内容有变化的文件
)

// FileDiff 是单个文件在「已安装内容」与「上游新内容」之间的差异。
// OldHash/NewHash 是文件内容的 sha256（十六进制，无前缀）；added 文件的
// OldHash 与 removed 文件的 NewHash 为空串。Added/Removed 仅对 changed
// 有意义（added 的 Removed 与 removed 的 Added 恒为 0）。
type FileDiff struct {
	Path    string
	Status  string
	Added   int
	Removed int
	OldHash string
	NewHash string
}

// UpdatePreview 是 PreviewUpdate 的结果：一次「更新将改变什么」的只读快照。
type UpdatePreview struct {
	// ID 是技能名。
	ID string
	// SourceType 是元数据记录的来源类型（local/github）。
	SourceType string
	// Commit 是上游最新 commit（github 来源成功 clone 后读取；local 来源
	// 为空串）。
	Commit string
	// OldHash/NewHash 是目录级内容哈希（与 Meta.ContentHash 同格式
	// "sha256:hex"），分别对应已安装内容与更新后的内容。上游没有变化、
	// 没有下载内容时（Basis 为 commit/tree）NewHash 取元数据记录的
	// ContentHash（更新后的内容就是安装时那份）。
	OldHash string
	NewHash string
	// Files 只包含有差异的文件（added/removed/changed），按路径排序；
	// 未变化的文件数放在 Unchanged。这样评审面板天然「每行一个变更文件」，
	// 无差异技能的 Files 为空数组。
	Files []FileDiff
	// Unchanged 是两侧内容完全一致的文件数。Basis 不是 content 时没有逐文件
	// 比较：本地没被改过取已安装的文件数，被改过则为 0（不知道哪些文件被改）。
	Unchanged int
	// Basis 说明这次是凭什么判定的：commit / tree / content（见 PreviewUpdates）。
	Basis string
	// LocalModified 表示已安装内容与安装/更新时记录的 contentHash 不一致
	// （被手改过）。应用更新会覆盖这些改动（旧内容在快照中）。
	LocalModified bool
}

// 判定依据常量（CLI JSON、UI 文案共用的稳定词汇）。
const (
	BasisCommit  = "commit"  // 上游 commit 与安装时相同，未下载任何内容
	BasisTree    = "tree"    // 上游技能子目录的树哈希与安装时相同，未下载文件
	BasisContent = "content" // 逐文件比较了已安装内容与上游内容
)

// HasChanges 报告预览是否发现了任何文件差异。
func (p *UpdatePreview) HasChanges() bool { return len(p.Files) > 0 }

// Counts 返回 added/changed/removed 三类文件的个数，便于汇总展示。
func (p *UpdatePreview) Counts() (added, changed, removed int) {
	for _, f := range p.Files {
		switch f.Status {
		case DiffAdded:
			added++
		case DiffChanged:
			changed++
		case DiffRemoved:
			removed++
		}
	}
	return
}

// PreviewUpdate 检查单个技能的更新，等价于只含一个技能的 PreviewUpdates。
func (c *Config) PreviewUpdate(name string) (*UpdatePreview, error) {
	r := c.PreviewUpdates([]string{name})[0]
	return r.Preview, r.Err
}

// PreviewResult 是 PreviewUpdates 里单个技能的结果：成功时 Preview 非 nil，
// 失败时 Err 非 nil。
type PreviewResult struct {
	Name    string
	Preview *UpdatePreview
	Err     error
}

// PreviewUpdates 检查 names 里每个技能是否有更新，结果与 names 一一对应。
// 它不修改任何磁盘内容（clone 发生在临时目录中并自动清理），也不创建
// 快照——那是 Update 的职责。
//
// git 来源按仓库（URL+ref）分组，逐级判断，能早停就早停：
//  1. git ls-remote 取 ref 当前 commit；与元数据记录的 commit 相同 → 最新
//     （BasisCommit），什么都不下载；
//  2. 否则对该仓库做一次只取树的浅 clone，比较技能子目录的树哈希；与元数据
//     记录的相同 → 最新（BasisTree），仍不下载文件；
//  3. 树变了，或元数据里没有树哈希（旧版本安装、导入的元数据）→ 稀疏检出
//     这些技能的子目录，逐文件比较（BasisContent）。
//
// 前两级比的是「上游相对安装时是否变化」，本地手改不算有更新（见
// LocalModified）；第三级比的是已安装内容与上游内容，本地手改也会显示为差异。
// local 来源直接只读原路径逐文件比较（BasisContent）。
//
// 错误语义与 Update 对齐：技能不存在 → ErrUnknownSkill；元数据缺失或
// 损坏 → 明确报错；git 失败/超时/来源路径消失 → 记入对应技能，不影响其他技能。
func (c *Config) PreviewUpdates(names []string) []PreviewResult {
	out := make([]PreviewResult, len(names))
	metas := make([]*Meta, len(names))
	groups := map[repoKey][]int{}
	var order []repoKey
	for i, name := range names {
		out[i].Name = name
		meta, err := c.sourceMeta(name, i18n.NewError("storeErrMetaMissingCheck", func() string { return locales.StoreErrMetaMissingCheck(name) }))
		if err != nil {
			out[i].Err = err
			continue
		}
		metas[i] = meta
		if meta.Source.Type == SourceLocal {
			out[i].Preview, out[i].Err = c.contentPreview(name, meta, meta.Source.Path, "")
			continue
		}
		k := newRepoKey(meta.Source.URL, meta.Source.Ref)
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], i)
	}
	for _, k := range order {
		c.previewRepo(groups[k], names, metas, out)
	}
	return out
}

// previewRepo 检查同一仓库（同 URL 与 ref）里的一组技能（idx 是它们在 names
// 里的下标），结果写回 out。整个仓库最多一次 ls-remote、一次只取树的 clone、
// 一次稀疏检出。
func (c *Config) previewRepo(idx []int, names []string, metas []*Meta, out []PreviewResult) {
	url, ref := metas[idx[0]].Source.URL, metas[idx[0]].Source.Ref
	fail := func(is []int, err error) {
		for _, i := range is {
			out[i].Err = err
		}
	}
	head, err := remoteHead(url, ref)
	if err != nil {
		fail(idx, err)
		return
	}
	// 第一级：仓库 commit 没变。
	var pending []int
	for _, i := range idx {
		if m := metas[i]; m.Source.Commit != "" && m.Source.Commit == head {
			out[i].Preview, out[i].Err = c.upToDatePreview(names[i], m, head, BasisCommit)
			continue
		}
		pending = append(pending, i)
	}
	if len(pending) == 0 {
		return
	}
	u, err := cloneUpstream(url, ref)
	if err != nil {
		fail(pending, err)
		return
	}
	defer u.Close()
	// 第二级：技能子目录的树没变。
	subs := make([]string, len(pending))
	for j, i := range pending {
		subs[j] = metas[i].Source.SubPath
	}
	trees, err := u.Trees(subs)
	if err != nil {
		fail(pending, err)
		return
	}
	var need []int
	var needSubs []string
	for _, i := range pending {
		m := metas[i]
		r := trees[m.Source.SubPath]
		if r.Err != nil {
			out[i].Err = r.Err
			continue
		}
		if m.Source.Tree != "" && m.Source.Tree == r.Tree {
			out[i].Preview, out[i].Err = c.upToDatePreview(names[i], m, u.Commit, BasisTree)
			continue
		}
		need = append(need, i)
		needSubs = append(needSubs, m.Source.SubPath)
	}
	if len(need) == 0 {
		return
	}
	// 第三级：只检出需要的子目录，逐文件比较。
	if err := u.Checkout(needSubs); err != nil {
		fail(need, err)
		return
	}
	for _, i := range need {
		out[i].Preview, out[i].Err = c.contentPreview(names[i], metas[i], u.Dir(metas[i].Source.SubPath), u.Commit)
	}
}

// upToDatePreview 构造「上游没有变化」的预览：Files 为空。上游没变，意味着
// 更新后的内容就是安装/更新时记录的那份，所以 NewHash 取元数据里的
// ContentHash；本地没被改过时，已安装的文件都算「一致」。
func (c *Config) upToDatePreview(name string, meta *Meta, commit, basis string) (*UpdatePreview, error) {
	p := &UpdatePreview{ID: name, SourceType: meta.Source.Type, Commit: commit, Basis: basis,
		Files: []FileDiff{}, NewHash: meta.ContentHash}
	if err := c.fillLocalState(p, meta); err != nil {
		return nil, err
	}
	if !p.LocalModified {
		files, err := listFiles(c.SkillPath(name))
		if err != nil {
			return nil, err
		}
		p.Unchanged = len(files)
	}
	return p, nil
}

// contentPreview 逐文件比较已安装内容与上游目录 newDir。
func (c *Config) contentPreview(name string, meta *Meta, newDir, commit string) (*UpdatePreview, error) {
	if !fileExists(newDir) {
		return nil, i18n.NewError("storeErrUpstreamDirMissing", func() string { return locales.StoreErrUpstreamDirMissing(newDir) })
	}
	p, err := diffTree(c.SkillPath(name), newDir)
	if err != nil {
		return nil, err
	}
	p.ID = name
	p.SourceType = meta.Source.Type
	p.Commit = commit
	p.Basis = BasisContent
	if err := c.fillLocalState(p, meta); err != nil {
		return nil, err
	}
	// 新哈希排除 .git 与元数据文件，与「Update 完成后 HashDir 的结果」一致。
	p.NewHash, err = hashTreeExcluding(newDir, skipUpstream)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// fillLocalState 填写已安装一侧的目录哈希，以及它是否偏离安装时记录的哈希。
func (c *Config) fillLocalState(p *UpdatePreview, meta *Meta) error {
	// 旧哈希与 Meta.ContentHash/doctor 同口径（HashDir），便于展示漂移。
	h, err := HashDir(c.SkillPath(p.ID))
	if err != nil {
		return err
	}
	p.OldHash = h
	p.LocalModified = meta.ContentHash != "" && meta.ContentHash != h
	return nil
}

// skipUpstream 是上游内容树（clone 目录或本地来源目录）的排除谓词：
// 根级 .kura-meta.json（kura 自管，不来自上游）与 .git（clone 产物，
// Update 替换前会删除）。排除 rel 为精确路径或目录前缀。
func skipUpstream(rel string) bool {
	return rel == MetaFileName || rel == ".git" || strings.HasPrefix(rel, ".git/")
}

// hashTreeExcluding 按 HashDir 的算法计算目录内容哈希，但跳过满足
// exclude 的相对路径（目录命中时整棵剪枝）。实现与 hash.go 保持一致：
// 相对路径 / 分隔、字节序排序、内容按「路径\0内容\0」拼接后取 sha256。
func hashTreeExcluding(dir string, exclude func(rel string) bool) (string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if exclude != nil && exclude(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return "", i18n.Wrapf(err, "storeErrWalkDir", func() string { return locales.StoreErrWalkDir(dir) })
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, rel := range paths {
		_, _ = h.Write([]byte(rel))
		h.Write([]byte{0})
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return "", i18n.Wrapf(err, "storeErrReadFile", func() string { return locales.StoreErrReadFile(rel) })
		}
		h.Write(data)
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// diffTree 对比两个目录的文件树，产出文件级差异（两侧均只读）。
// 已安装一侧的 .git 同样跳过：正常安装流程不会把 .git 带进技能目录
// （Add/Update 都会剥离），万一用户手工放入，跳过可避免整片噪音行。
func diffTree(oldDir, newDir string) (*UpdatePreview, error) {
	oldFiles, err := listFiles(oldDir)
	if err != nil {
		return nil, err
	}
	newFiles, err := listFiles(newDir)
	if err != nil {
		return nil, err
	}
	// 路径全集排序，保证输出顺序稳定（测试与 UI 展示都依赖）。
	rels := make([]string, 0, len(oldFiles)+len(newFiles))
	for rel := range oldFiles {
		rels = append(rels, rel)
	}
	for rel := range newFiles {
		if _, ok := oldFiles[rel]; !ok {
			rels = append(rels, rel)
		}
	}
	sort.Strings(rels)

	p := &UpdatePreview{Files: []FileDiff{}}
	for _, rel := range rels {
		of, hasOld := oldFiles[rel]
		nf, hasNew := newFiles[rel]
		switch {
		case hasOld && !hasNew:
			// 上游已删除：删除行数记为该文件行数，便于展示规模。
			data, rerr := os.ReadFile(of)
			if rerr != nil {
				return nil, i18n.Wrapf(rerr, "storeErrReadFile", func() string { return locales.StoreErrReadFile(rel) })
			}
			p.Files = append(p.Files, FileDiff{
				Path: rel, Status: DiffRemoved,
				Removed: len(splitLines(data)), OldHash: hashBytes(data),
			})
		case !hasOld && hasNew:
			// 上游新增：新增行数记为该文件行数。
			data, rerr := os.ReadFile(nf)
			if rerr != nil {
				return nil, i18n.Wrapf(rerr, "storeErrReadFile", func() string { return locales.StoreErrReadFile(rel) })
			}
			p.Files = append(p.Files, FileDiff{
				Path: rel, Status: DiffAdded,
				Added: len(splitLines(data)), NewHash: hashBytes(data),
			})
		default:
			od, rerr := os.ReadFile(of)
			if rerr != nil {
				return nil, i18n.Wrapf(rerr, "storeErrReadFile", func() string { return locales.StoreErrReadFile(rel) })
			}
			nd, rerr := os.ReadFile(nf)
			if rerr != nil {
				return nil, i18n.Wrapf(rerr, "storeErrReadFile", func() string { return locales.StoreErrReadFile(rel) })
			}
			if bytes.Equal(od, nd) {
				p.Unchanged++
				continue
			}
			a, r := lineDiffCounts(splitLines(od), splitLines(nd))
			p.Files = append(p.Files, FileDiff{
				Path: rel, Status: DiffChanged,
				Added: a, Removed: r,
				OldHash: hashBytes(od), NewHash: hashBytes(nd),
			})
		}
	}
	return p, nil
}

// listFiles 列出 dir 下全部普通文件：键为 / 分隔的相对路径，值为绝对路径。
// 根级 .kura-meta.json 一律跳过（kura 自管文件，不属于技能内容），
// .git 目录整棵剪枝（两侧一致，理由见 diffTree）。
func listFiles(dir string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if rel == ".git" || strings.HasPrefix(rel, ".git/") {
				return filepath.SkipDir
			}
			return nil
		}
		if rel == MetaFileName {
			return nil
		}
		files[rel] = p
		return nil
	})
	if err != nil {
		return nil, i18n.Wrapf(err, "storeErrWalkDir", func() string { return locales.StoreErrWalkDir(dir) })
	}
	return files, nil
}

// lcsCellLimit 限制 LCS 动态规划的规模（行数乘积）。技能文件通常几十到
// 几百行（远低于阈值）；超过时退化为与顺序无关的多重集差，避免大文件
// 撑爆内存。4e6 格 × 4 字节 ≈ 16MB，是可接受的峰值。
const lcsCellLimit = 4_000_000

// splitLines 把内容按 \n 切成行；末尾单个换行不产生额外的空行，
// 空文件为 0 行（与常规文本编辑器的行数直觉一致）。
func splitLines(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	s := strings.TrimSuffix(string(b), "\n")
	return strings.Split(s, "\n")
}

// lineDiffCounts 计算从旧行集到新行集需要删除/新增的行数。
// 常规规模用 LCS 最小编辑距离：added = len(new)-LCS、removed = len(old)-LCS，
// 与 git diff --numstat 的口径一致；超大文件退化为多重集差（忽略行顺序）。
func lineDiffCounts(oldLines, newLines []string) (added, removed int) {
	if slices.Equal(oldLines, newLines) {
		return 0, 0
	}
	if len(oldLines)*len(newLines) <= lcsCellLimit {
		lcs := lcsLength(oldLines, newLines)
		return len(newLines) - lcs, len(oldLines) - lcs
	}
	return bagDiffCounts(oldLines, newLines)
}

// lcsLength 计算两个行序列的最长公共子序列长度（两行滚动数组 DP）。
func lcsLength(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	prev := make([]int32, len(b)+1)
	cur := make([]int32, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			switch {
			case a[i-1] == b[j-1]:
				cur[j] = prev[j-1] + 1
			case prev[j] >= cur[j-1]:
				cur[j] = prev[j]
			default:
				cur[j] = cur[j-1]
			}
		}
		prev, cur = cur, prev
	}
	return int(prev[len(b)])
}

// bagDiffCounts 按多重集差统计行数：能配对的行互相抵消，新行集剩下的
// 记为新增，旧行集剩下的记为删除。仅作为超大文件的退化路径。
func bagDiffCounts(oldLines, newLines []string) (added, removed int) {
	pool := make(map[string]int, len(oldLines))
	for _, l := range oldLines {
		pool[l]++
	}
	for _, l := range newLines {
		if pool[l] > 0 {
			pool[l]--
		} else {
			added++
		}
	}
	for _, n := range pool {
		removed += n
	}
	return added, removed
}

// hashBytes 计算单个文件内容的 sha256（十六进制，无前缀）。
func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
