package cli

// `kura doctor`：对整个根目录做只读体检，报告问题但不修改任何文件。
// 退出码：0 干净；3 发现问题。诊断过程绝不 panic——任何单点失败
//（损坏的 JSON、非目录的"技能文件夹"、不可读的文件）都转化为一条问题记录。

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/ArnoChenFx/kura/internal/store"
)

// doctor 问题类别（JSON 输出中 problem.kind 的全部取值，稳定契约）：
//
//	not-a-dir   : 根目录/disabled/trash 下存在与技能同名的普通文件，
//	             或根目录本身不可读
//	no-meta     : 技能文件夹缺少 .kura-meta.json（或元数据缺 contentHash）
//	meta-parse  : .kura-meta.json 存在但无法解析（JSON 损坏）
//	hash-drift  : 元数据 contentHash 与当前内容不符（内容已漂移）
//	no-skillmd  : 技能文件夹缺少 SKILL.md
//	leftover    : disabled/trash 中的残留条目（提醒，可用 enable/restore 处理）
const (
	KindNotADir   = "not-a-dir"
	KindNoMeta    = "no-meta"
	KindMetaParse = "meta-parse"
	KindHashDrift = "hash-drift"
	KindNoSkillMD = "no-skillmd"
	KindLeftover  = "leftover"
)

// Problem 是 doctor 的一条问题记录。
type Problem struct {
	// Kind 是问题类别，取值见上方常量（稳定契约，UI 依赖它做展示分类）。
	Kind string `json:"kind"`
	// Skill 是问题所属的技能名；与具体技能无关的问题为空串。
	Skill string `json:"skill"`
	// Detail 是人类可读的详细说明。
	Detail string `json:"detail"`
}

// doctorResult 是 doctor --json 的顶层形状（稳定契约，见 docs/note）。
type doctorResult struct {
	// Root 是被体检的技能根目录（绝对路径）。
	Root string `json:"root"`
	// Problems 是全部问题记录；干净时为空数组（非 null）。
	Problems []Problem `json:"problems"`
}

// cmdDoctor 实现 `kura doctor`：只读体检根目录 + disabled + trash。
func cmdDoctor(args []string, stdout, stderr io.Writer, pre *globalFlags) int {
	g := &globalFlags{}
	fs := newFlagSet("doctor", stderr, g, pre)
	positional, err := parseArgs(fs, args)
	if err != nil {
		return usageError(stderr, err)
	}
	if len(positional) > 0 {
		return printError(g, stderr, "doctor 不接受位置参数")
	}
	c, err := resolveConfig(g)
	if err != nil {
		return printError(g, stderr, "%v", err)
	}
	result := runDoctor(c)
	hasProblems := len(result.Problems) > 0
	if g.json {
		// 保证 problems 恒为数组，消费方无需判 null。
		if result.Problems == nil {
			result.Problems = []Problem{}
		}
		if code := writeJSONOrErr(stdout, result); code != ExitOK {
			return code
		}
	} else {
		printDoctorText(stdout, c, result)
	}
	// 退出码与输出模式无关：有问题就是 3，干净就是 0。
	if hasProblems {
		return ExitDoctorProblem
	}
	return ExitOK
}

// runDoctor 执行体检并收集全部问题。它只读取文件系统，绝不写入。
func runDoctor(c *store.Config) doctorResult {
	result := doctorResult{Root: c.Root}
	result.Problems = append(result.Problems, checkRootEntries(c)...)
	enabled, scanErr := c.Scan()
	if scanErr != nil {
		result.Problems = append(result.Problems, Problem{
			Kind: KindNotADir, Detail: fmt.Sprintf("扫描根目录失败: %v", scanErr),
		})
	} else {
		result.Problems = append(result.Problems, checkSkills(enabled, "根目录")...)
	}
	result.Problems = append(result.Problems, checkDisabled(c)...)
	result.Problems = append(result.Problems, checkTrash(c)...)
	sortProblems(result.Problems)
	return result
}

// checkRootEntries 检查根目录本身：不可读报告问题；一级条目里与技能
// 同级的普通文件单独报告（store.Scan 只看目录，会静默跳过它们）。
func checkRootEntries(c *store.Config) []Problem {
	entries, err := os.ReadDir(c.Root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 根目录不存在：空安装，不是问题。
		}
		return []Problem{{
			Kind:   KindNotADir,
			Detail: fmt.Sprintf("根目录不可读: %v", err),
		}}
	}
	var problems []Problem
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue // 隐藏条目（如 .skill-lock.json 的兄弟目录）不是技能。
		}
		if !e.IsDir() {
			problems = append(problems, Problem{
				Kind:   KindNotADir,
				Skill:  e.Name(),
				Detail: "根目录下存在同名普通文件，技能必须是目录",
			})
		}
	}
	return problems
}

// checkSkills 对一组 store.Skill 做完整检查：缺元数据 / 元数据损坏 /
// 缺 SKILL.md / 内容哈希漂移。任何单点失败都转化为问题记录，绝不 panic。
// where 仅用于人类可读文案（"根目录"/"disabled"）。
func checkSkills(skills []*store.Skill, where string) []Problem {
	var problems []Problem
	for _, s := range skills {
		// 元数据损坏（文件存在但解析失败）：无法核对哈希，跳过漂移检查。
		if s.MetaErr != "" {
			problems = append(problems, Problem{
				Kind:   KindMetaParse,
				Skill:  s.Name,
				Detail: s.MetaErr,
			})
		}
		// 缺少元数据文件。
		if s.MetaErr == "" && s.Meta == nil {
			problems = append(problems, Problem{
				Kind:   KindNoMeta,
				Skill:  s.Name,
				Detail: fmt.Sprintf("%s 的技能缺少 .kura-meta.json（可用 kura import-meta 从 lock 文件补齐）", where),
			})
		}
		// 缺少 SKILL.md。
		if !s.HasSKILLMD {
			problems = append(problems, Problem{
				Kind:   KindNoSkillMD,
				Skill:  s.Name,
				Detail: fmt.Sprintf("%s 的技能缺少 SKILL.md", where),
			})
		}
		// 内容漂移：仅在元数据可读且有记录哈希时核对。
		if s.MetaErr == "" && s.Meta != nil {
			switch expected := s.Meta.ContentHash; {
			case expected == "":
				problems = append(problems, Problem{
					Kind:   KindNoMeta,
					Skill:  s.Name,
					Detail: "元数据缺少 contentHash，无法检测内容漂移",
				})
			case s.ContentHash != expected:
				problems = append(problems, Problem{
					Kind:  KindHashDrift,
					Skill: s.Name,
					Detail: fmt.Sprintf("内容与元数据不符: 当前 %s，记录 %s（可用 kura update 刷新）",
						shortHash(s.ContentHash), shortHash(expected)),
				})
			}
		}
	}
	return problems
}

// checkDisabled 检查 disabled/：结构问题 + 残留提醒。禁用的技能不参与
// 更新，漂移核对没有参照意义，故只报结构问题（损坏/缺文件）与 leftover。
func checkDisabled(c *store.Config) []Problem {
	var problems []Problem
	// 普通文件混入 disabled/（store.ScanDisabled 会跳过非目录条目）。
	problems = append(problems, checkSideEntries(c.DisabledRoot(), "disabled")...)
	skills, err := c.ScanDisabled()
	if err != nil {
		problems = append(problems, Problem{
			Kind: KindNotADir, Detail: fmt.Sprintf("扫描 disabled 目录失败: %v", err),
		})
		return problems
	}
	for _, s := range skills {
		// 结构检查复用 checkSkills（损坏/缺失/SKILL.md），但不做哈希核对：
		// 手工把 s.Meta.ContentHash 清空再交给 checkSkills 即可绕过漂移。
		clone := *s
		if clone.Meta != nil {
			m := *clone.Meta
			m.ContentHash = ""
			clone.Meta = &m
		}
		problems = append(problems, checkSkills([]*store.Skill{&clone}, "disabled")...)
		// 残留提醒。
		problems = append(problems, Problem{
			Kind:   KindLeftover,
			Skill:  s.Name,
			Detail: fmt.Sprintf("disabled 中有已禁用的技能（可用 kura enable %s 恢复）", s.Name),
		})
	}
	return problems
}

// checkTrash 检查 trash/：结构是 trash/<name>/<stamp>，每个时间戳副本都是
// 一条 leftover 提醒（可恢复）；非目录条目与不可读条目按结构问题报告。
func checkTrash(c *store.Config) []Problem {
	var problems []Problem
	problems = append(problems, checkSideEntries(c.TrashRoot(), "trash")...)
	names, err := c.ScanTrash()
	if err != nil {
		problems = append(problems, Problem{
			Kind: KindNotADir, Detail: fmt.Sprintf("扫描 trash 目录失败: %v", err),
		})
		return problems
	}
	for _, n := range names {
		stamps, derr := os.ReadDir(n.Dir)
		if derr != nil {
			problems = append(problems, Problem{
				Kind:   KindNotADir,
				Skill:  n.Name,
				Detail: fmt.Sprintf("回收站条目不可读: %v", derr),
			})
			continue
		}
		for _, st := range stamps {
			if !st.IsDir() {
				continue // 时间戳层的零散文件不影响 restore，忽略。
			}
			problems = append(problems, Problem{
				Kind:  KindLeftover,
				Skill: n.Name,
				Detail: fmt.Sprintf("回收站中保留着 %s/%s（可用 kura restore %s --stamp %s 恢复）",
					n.Name, st.Name(), n.Name, st.Name()),
			})
		}
	}
	return problems
}

// checkSideEntries 检查 disabled/trash 一级条目里的普通文件
// （store 的扫描函数只返回目录，非目录条目需要 doctor 自己发现）。
func checkSideEntries(dir, label string) []Problem {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 目录不存在：从未禁用/删除过，正常。
		}
		return []Problem{{Kind: KindNotADir, Detail: fmt.Sprintf("读取 %s 失败: %v", dir, err)}}
	}
	var problems []Problem
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if !e.IsDir() {
			problems = append(problems, Problem{
				Kind:   KindNotADir,
				Skill:  e.Name(),
				Detail: fmt.Sprintf("%s 下存在同名普通文件，技能必须是目录", label),
			})
		}
	}
	return problems
}

// shortHash 把 "sha256:64hex" 缩短为 "sha256:8hex…"，便于人类阅读。
func shortHash(h string) string {
	const prefix = "sha256:"
	if len(h) > len(prefix)+8 {
		return h[:len(prefix)+8] + "…"
	}
	return h
}

// sortProblems 按 (Kind, Skill) 排序，保证输出顺序稳定（测试与 diff 友好）。
func sortProblems(ps []Problem) {
	sort.Slice(ps, func(i, j int) bool {
		if ps[i].Kind != ps[j].Kind {
			return ps[i].Kind < ps[j].Kind
		}
		return ps[i].Skill < ps[j].Skill
	})
}

// printDoctorText 输出人类可读的体检报告。
func printDoctorText(w io.Writer, c *store.Config, result doctorResult) {
	fmt.Fprintf(w, "root: %s\n", c.Root)
	if len(result.Problems) == 0 {
		fmt.Fprintln(w, "一切正常（没有发现问题）")
		return
	}
	fmt.Fprintf(w, "发现 %d 个问题:\n", len(result.Problems))
	for _, p := range result.Problems {
		fmt.Fprintf(w, "  [%s] %s: %s\n", p.Kind, skillOrDash(p.Skill), p.Detail)
	}
}

// skillOrDash 空技能名显示为 "-"。
func skillOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
