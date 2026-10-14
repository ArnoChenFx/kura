package store

// 仓库内技能发现：输入一个已经 clone 好的本地仓库目录，找出里面所有
// 「技能目录」（含 SKILL.md 的目录），输出相对仓库根的 slash 路径。
// 发现结果供安装入口在「用户只给了 owner/repo、没给子目录」时决定装什么：
//   - 恰好一个 → 直接装它；
//   - 多个     → 返回 CandidatesError，把选择权交还给调用方（CLI 列表 / UI 选择）；
//   - 零个     → 维持旧报错「来源目录缺少 SKILL.md」。
//
// 纯磁盘只读，不发网络请求、不写任何文件；clone 由调用方（add.go 的
// resolveGitHub）负责，本文件不复述克隆逻辑。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ArnoChenFx/kura/internal/i18n"
)

// 发现的边界值。巨型仓库（如把整个 monorepo 克隆下来）递归扫描可能失控，
// 用两层保险：目录深度不超过 maxDiscoverDepth（1 = 仓库根的直接子目录）；
// 候选数超过 maxDiscoverSkills 时截断并返回 ErrTooManySkills（宁可让用户
// 显式指定 --sub-path，也不要输出一份几百行的清单淹没调用方）。
const (
	maxDiscoverDepth  = 6
	maxDiscoverSkills = 64
)

// ErrTooManySkills 表示仓库内的技能候选超过上限（见 maxDiscoverSkills）。
// 用 errors.Is 判断。
var ErrTooManySkills = i18n.NewError("store.err.tooManySkills")

// DiscoverResult 是一次发现的结果：Paths 按字典序排序，元素是相对仓库根
// 的 slash 路径（形如 "skills/engineering/tdd"），可直接当作 SubPath 使用。
type DiscoverResult struct {
	// Paths 是全部技能目录（相对仓库根的 slash 路径，已排序）。
	Paths []string
	// Manifest 为 true 表示路径来自 .claude-plugin/plugin.json 的 skills
	// 清单（作者显式声明，比扫描更权威）；false 表示来自目录扫描。
	Manifest bool
}

// CandidatesError 表示仓库里有多个技能、需要用户挑选。Paths 已排序；
// Error() 给出一段可读的中文清单（列出全部路径——上限 64 条，全列出
// 也不会把输出撑爆，且调用方不用再二次格式化）。
type CandidatesError struct {
	// Repo 是来源仓库（owner/repo），用于提示语。
	Repo string
	// Paths 是全部候选（相对仓库根的 slash 路径，已排序）。
	Paths []string
}

// Error 实现 error 接口。
func (e *CandidatesError) Error() string {
	var b strings.Builder
	b.WriteString(i18n.Tf("store.err.tooManyCandidates", e.Repo, len(e.Paths)))
	for i, p := range e.Paths {
		b.WriteString("\n  ")
		b.WriteString(p)
		if i >= 63 { // 防御：Paths 正常不会超过 maxDiscoverSkills，这里兜底截断
			b.WriteString("\n  ")
			b.WriteString(i18n.T("store.term.truncated"))
			break
		}
	}
	return b.String()
}

// pluginJSON 是 .claude-plugin/plugin.json 里我们关心的字段（其余忽略）。
type pluginJSON struct {
	// Skills 是技能目录列表，形如 "./skills/engineering/tdd"。
	// 指向单个文件（如 .../SKILL.md）或为空串的条目按无效处理。
	Skills []string `json:"skills"`
}

// DiscoverSkills 在已 clone 的本地仓库 dir 里发现技能目录。
// 返回 DiscoverResult 与 error；dir 必须存在（调用方刚 clone 完，不存在
// 属于编程错误路径，返回错误即可，不 panic）。
//
// 发现规则（优先级从高到低）：
//  1. 仓库根有 SKILL.md → 仓库根本身就是一个技能（返回 {"."}）。
//  2. .claude-plugin/plugin.json 带有效 skills 数组 → 用清单（作者显式
//     声明要发布的技能，能排除 in-progress 等草稿目录；与扫描冲突时
//     清单优先，因为「作者想让你装什么」比「目录里恰好有什么」更权威）。
//  3. 否则递归扫描：每个含 SKILL.md 的目录都是一个候选。
func DiscoverSkills(dir string) (*DiscoverResult, error) {
	if !fileExists(dir) {
		return nil, i18n.Errorf("store.err.repoDirMissing", dir)
	}
	// 1) 仓库根本身是技能：最简单也最常见的单技能仓库布局。
	if fileExists(filepath.Join(dir, "SKILL.md")) {
		return &DiscoverResult{Paths: []string{"."}}, nil
	}
	// 2) 插件清单：作者声明过技能列表，优先于盲扫。
	paths, ok, err := discoverFromManifest(dir)
	if err != nil {
		return nil, err
	}
	if ok {
		return &DiscoverResult{Paths: paths, Manifest: true}, nil
	}
	// 3) 递归扫描。
	return discoverByScan(dir)
}

// normalizeSubPath 把清单里的条目规范化为相对仓库根的 slash 路径：
// 去 ./ 前缀、把 \ 与 / 统一成 /、去首尾斜杠。返回空串表示条目无效。
func normalizeSubPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "./")
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.Trim(p, "/")
	if p == "" || strings.Contains(p, "..") {
		// 空条目或越界（..）都视为无效：清单是作者写的，写错了不能变成
		// 一次目录穿越。调用方按「无效条目」忽略。
		return ""
	}
	return p
}

// discoverFromManifest 读 .claude-plugin/plugin.json 的 skills 数组。
// 第二个返回值 false 表示「没有可用清单」（文件不存在 / JSON 解析失败 /
// 过滤后一条不剩），调用方继续走扫描。清单里指向不存在目录或文件的条目
// 直接忽略而不是报错：仓库作者可能先声明后删除，或声明的是文件路径；
// 忽略能让有效条目照常工作，而报错会让整个安装卡死在一条陈旧声明上。
func discoverFromManifest(dir string) ([]string, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if err != nil {
		return nil, false, nil // 没有清单：正常路径
	}
	var pj pluginJSON
	if err := json.Unmarshal(data, &pj); err != nil {
		// 清单损坏：忽略并回退扫描。清单是「锦上添花」的加速与筛选，
		// 它坏了不应该让一个本来可扫描的仓库装不上。
		return nil, false, nil
	}
	var paths []string
	seen := map[string]bool{}
	for _, raw := range pj.Skills {
		p := normalizeSubPath(raw)
		if p == "" || seen[p] {
			continue
		}
		// 条目可能指向目录（./skills/engineering/tdd）或文件（.../SKILL.md）。
		// 指向文件时取其所在目录；两者都必须真实存在，否则忽略该条目。
		full := filepath.Join(dir, filepath.FromSlash(p))
		if fileExists(filepath.Join(full, "SKILL.md")) {
			// 目录，本身是技能。
		} else if strings.HasSuffix(p, "/SKILL.md") && fileExists(full) {
			// 文件，取所在目录。
			p = strings.TrimSuffix(p, "/SKILL.md")
		} else {
			continue
		}
		seen[p] = true
		paths = append(paths, p)
	}
	if len(paths) == 0 {
		return nil, false, nil
	}
	sort.Strings(paths)
	return paths, true, nil
}

// discoverByScan 递归扫描目录树，收集含 SKILL.md 的目录。
// 跳过 .git、node_modules 与常见构建产物目录；深度超过 maxDiscoverDepth
// 不再下钻。候选数达到 maxDiscoverSkills 时返回 ErrTooManySkills（附上
// 已收集的部分）——截断报错比静默丢候选安全：用户会看到「请指定子路径」，
// 而不会误以为列出的就是全部。
func discoverByScan(dir string) (*DiscoverResult, error) {
	var paths []string
	var walk func(rel string, depth int) error
	walk = func(rel string, depth int) error {
		if depth > maxDiscoverDepth {
			return nil // 不再下钻；深埋的目录大概率不是技能
		}
		entries, err := os.ReadDir(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		for _, e := range entries {
			name := e.Name()
			if !e.IsDir() || skipDiscoverDir(name) {
				continue
			}
			sub := name
			if rel != "" {
				sub = rel + "/" + name
			}
			if fileExists(filepath.Join(dir, filepath.FromSlash(sub), "SKILL.md")) {
				if len(paths) >= maxDiscoverSkills {
					return sentinelErr(ErrTooManySkills, "store.err.tooManySkillsLimit", maxDiscoverSkills)
				}
				paths = append(paths, sub)
				// 命中即停：技能目录不该再往下钻出「技能里的技能」，
				// SKILL.md 的 assets/ 之类误报只会污染候选列表。
				continue
			}
			if err := walk(sub, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk("", 1); err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return &DiscoverResult{Paths: []string{}}, nil
	}
	sort.Strings(paths)
	return &DiscoverResult{Paths: paths}, nil
}

// skipDiscoverDir 报告目录名是否该跳过：版本库内部、包管理器、构建产物、
// 缓存。这些目录既不该有技能，也大得离谱，钻进去只会浪费时间。
func skipDiscoverDir(name string) bool {
	switch name {
	case ".git", ".github", ".svn", ".hg",
		"node_modules", "vendor", "bower_components",
		"dist", "build", "out", "target", ".next", ".nuxt",
		"__pycache__", ".venv", "venv",
		".gradle", ".terraform",
		"disabled", "trash", "snapshots", // kura 自己的状态目录（用户可能把根目录当仓库装回来）
		".kura":
		return true
	}
	return false
}

// DiscoverRepoSkills 供 UI 使用：clone owner/repo（可选 ref）到临时目录并
// 发现里面的技能，返回相对仓库根的 slash 路径。复用 resolveGitHub 的
// clone 与清理方式，不在网络边界上另起一套逻辑。仓库里没有任何技能时
// 返回空切片（不报错：调用方自行决定怎么展示）。
func (c *Config) DiscoverRepoSkills(repo, ref string) ([]string, error) {
	opts := &AddOptions{Repo: repo, Ref: ref}
	if err := opts.validate(); err != nil {
		return nil, err
	}
	// resolveGitHub 在 SubPath 为空时只检出发现技能要用的文件（各级 SKILL.md
	// 与插件清单），clone 根本身正好是发现入口需要的「已 clone 好的仓库目录」。
	u, _, err := c.resolveGitHub(opts)
	if err != nil {
		return nil, err
	}
	defer u.Close()
	dr, err := DiscoverSkills(u.dir)
	if err != nil {
		return nil, err
	}
	return dr.Paths, nil
}
