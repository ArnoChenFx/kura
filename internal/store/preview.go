package store

// PreviewUpdate（里程碑 4）：在真正执行 Update 之前回答「更新会改什么」。
//
// 设计要点：
//   - 只读操作：除临时目录外绝不修改磁盘上的任何内容（不写快照、不动技能
//     目录、不改元数据）。github 来源复用 Update 的 refetchGitHub，把上游
//     clone 进 os.MkdirTemp 并在返回前清理；local 来源直接只读原路径。
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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
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
	// "sha256:hex"），分别对应已安装内容与更新后的内容。
	OldHash string
	NewHash string
	// Files 只包含有差异的文件（added/removed/changed），按路径排序；
	// 未变化的文件数放在 Unchanged。这样评审面板天然「每行一个变更文件」，
	// 无差异技能的 Files 为空数组。
	Files []FileDiff
	// Unchanged 是两侧内容完全一致的文件数。
	Unchanged int
}

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

// PreviewUpdate 对比已安装技能与「按元数据重新拉取的上游内容」，返回
// 文件级差异摘要。它不修改任何磁盘内容（github 来源的 clone 发生在
// 临时目录中并自动清理），也不创建快照——那是 Update 的职责。
//
// 错误语义与 Update 对齐：技能不存在 → ErrUnknownSkill；元数据缺失或
// 损坏 → 明确报错；clone 失败/来源路径消失 → 原样上抛，调用方展示即可。
func (c *Config) PreviewUpdate(name string) (*UpdatePreview, error) {
	src := c.SkillPath(name)
	if !fileExists(src) {
		return nil, fmt.Errorf("store: %w: %s", ErrUnknownSkill, name)
	}
	// 读取元数据；缺失或损坏时无法知道来源，直接报错（与 Update 一致）。
	res := readMeta(src)
	if res.Err != nil {
		return nil, fmt.Errorf("store: %s 的元数据不可读: %w", name, res.Err)
	}
	if !res.Found {
		return nil, fmt.Errorf("store: %s 缺少 .kura-meta.json，无法检查更新", name)
	}
	meta := res.Meta

	var newDir, commit string
	var cleanup func()
	switch meta.Source.Type {
	case SourceLocal:
		// 本地来源：Update 是从原路径复制，预览直接只读该路径即可。
		if meta.Source.Path == "" {
			return nil, errors.New("store: 元数据缺少 local 来源路径")
		}
		newDir = meta.Source.Path
		if !fileExists(newDir) {
			return nil, fmt.Errorf("store: 本地来源不存在: %s", meta.Source.Path)
		}
	case SourceGitHub:
		// 复用 Update 的拉取逻辑：浅 clone 到临时目录（自动清理）。
		if meta.Source.URL == "" {
			return nil, errors.New("store: 元数据缺少 github 来源 URL")
		}
		var err error
		newDir, _, cleanup, err = c.refetchGitHub(name, meta)
		if err != nil {
			return nil, err
		}
		defer cleanup()
		// 在临时目录还活着时读取上游 commit（rev-parse 对子目录同样有效）。
		commit = gitHeadStaging(newDir)
	default:
		return nil, fmt.Errorf("store: 未知来源类型 %q", meta.Source.Type)
	}

	p, err := diffTree(src, newDir)
	if err != nil {
		return nil, err
	}
	p.ID = name
	p.SourceType = meta.Source.Type
	p.Commit = commit
	// 旧哈希与 Meta.ContentHash/doctor 同口径（HashDir），便于展示漂移。
	p.OldHash, err = HashDir(src)
	if err != nil {
		return nil, err
	}
	// 新哈希排除 .git 与元数据文件，与「Update 完成后 HashDir 的结果」一致。
	p.NewHash, err = hashTreeExcluding(newDir, skipUpstream)
	if err != nil {
		return nil, err
	}
	return p, nil
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
		return "", fmt.Errorf("store: 遍历 %s 失败: %w", dir, err)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, rel := range paths {
		_, _ = h.Write([]byte(rel))
		h.Write([]byte{0})
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return "", fmt.Errorf("store: 读取 %s 失败: %w", rel, err)
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
				return nil, fmt.Errorf("store: 读取 %s 失败: %w", rel, rerr)
			}
			p.Files = append(p.Files, FileDiff{
				Path: rel, Status: DiffRemoved,
				Removed: len(splitLines(data)), OldHash: hashBytes(data),
			})
		case !hasOld && hasNew:
			// 上游新增：新增行数记为该文件行数。
			data, rerr := os.ReadFile(nf)
			if rerr != nil {
				return nil, fmt.Errorf("store: 读取 %s 失败: %w", rel, rerr)
			}
			p.Files = append(p.Files, FileDiff{
				Path: rel, Status: DiffAdded,
				Added: len(splitLines(data)), NewHash: hashBytes(data),
			})
		default:
			od, rerr := os.ReadFile(of)
			if rerr != nil {
				return nil, fmt.Errorf("store: 读取 %s 失败: %w", rel, rerr)
			}
			nd, rerr := os.ReadFile(nf)
			if rerr != nil {
				return nil, fmt.Errorf("store: 读取 %s 失败: %w", rel, rerr)
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
		return nil, fmt.Errorf("store: 遍历 %s 失败: %w", dir, err)
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
