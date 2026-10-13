package cli

// 命令实现：list/scan、disable/enable、remove、restore、import-meta、
// add、update。每个命令返回退出码，只通过注入的 writer 输出。

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/ArnoChenFx/kura/internal/store"
)

// printError 按模式输出错误：--json 时输出结构化错误，否则输出人类可读文本。
// 返回 ExitError，调用方直接 `return printError(...)`。
func printError(g *globalFlags, stderr io.Writer, format string, a ...any) int {
	msg := fmt.Sprintf(format, a...)
	if g.json {
		_ = writeJSON(stderr, errorJSON{Error: msg})
	} else {
		fmt.Fprintf(stderr, "kura: %s\n", msg)
	}
	return ExitError
}

// ----------------------------- list / scan -----------------------------

// skillJSON 是 list/scan --json 输出里的单个技能条目。
// 字段全部 omitempty：只有实际存在的信息才出现，字段集合按版本演进
// 只增不改义，保证向后兼容。
type skillJSON struct {
	Name        string `json:"name"`
	State       string `json:"state"`
	HasSkillMd  bool   `json:"hasSkillMd"`
	Dir         string `json:"dir"`
	SourceType  string `json:"sourceType,omitempty"`
	SourceURL   string `json:"sourceUrl,omitempty"`
	Ref         string `json:"ref,omitempty"`
	SubPath     string `json:"subPath,omitempty"`
	PluginName  string `json:"pluginName,omitempty"`
	UpdatedAt   string `json:"updatedAt,omitempty"`
	ContentHash string `json:"contentHash,omitempty"`
	MetaError   string `json:"metaError,omitempty"` // 元数据损坏时的原因
}

// listResult 是 list/scan --json 的顶层形状。
type listResult struct {
	Root   string      `json:"root"`
	Skills []skillJSON `json:"skills"`
}

// toSkillJSON 把 store.Skill 折叠成输出条目（元数据缺失/损坏时来源字段为空）。
// trashed 条目的目录结构是 trash/<name>/<stamp>，容器本身没有 SKILL.md
// 与元数据属于正常现象，不作为告警字段输出。
func toSkillJSON(s *store.Skill) skillJSON {
	out := skillJSON{
		Name:       s.Name,
		State:      string(s.State),
		HasSkillMd: s.HasSKILLMD,
		Dir:        s.Dir,
	}
	if s.State != store.StateTrashed {
		out.MetaError = s.MetaErr
		if s.Meta != nil {
			out.SourceType = s.Meta.Source.Type
			out.SourceURL = s.Meta.Source.URL
			out.Ref = s.Meta.Source.Ref
			out.SubPath = s.Meta.Source.SubPath
			out.PluginName = s.Meta.PluginName
			out.UpdatedAt = s.Meta.UpdatedAt
			out.ContentHash = s.Meta.ContentHash
		}
	}
	return out
}

// cmdList 实现 `kura list` 与 `kura scan`（同义命令，scan 强调可用 --root
// 覆盖根目录）。
func cmdList(args []string, stdout, stderr io.Writer, pre *globalFlags) int {
	g := &globalFlags{}
	fs := newFlagSet("list", stderr, g, pre)
	positional, err := parseArgs(fs, args)
	if err != nil {
		return usageError(stderr, err)
	}
	if len(positional) > 0 {
		return printError(g, stderr, "list 不接受位置参数（得到 %q）", strings.Join(positional, " "))
	}
	c, err := resolveConfig(g)
	if err != nil {
		return printError(g, stderr, "%v", err)
	}
	skills, err := c.ScanAll()
	if err != nil {
		return printError(g, stderr, "%v", err)
	}
	if g.json {
		items := make([]skillJSON, 0, len(skills))
		for _, s := range skills {
			items = append(items, toSkillJSON(s))
		}
		return writeJSONOrErr(stdout, listResult{Root: c.Root, Skills: items})
	}
	printSkillsText(stdout, c, skills)
	return ExitOK
}

// printSkillsText 输出人类可读的技能表（一行一个技能，制表符分隔）。
func printSkillsText(w io.Writer, c *store.Config, skills []*store.Skill) {
	fmt.Fprintf(w, "root: %s\n", c.Root)
	if len(skills) == 0 {
		fmt.Fprintln(w, "（没有技能）")
		return
	}
	for _, s := range skills {
		fmt.Fprintf(w, "%s\t[%s]", s.Name, s.State)
		if s.State == store.StateTrashed {
			// trash/<name>/<stamp> 容器本身没有 SKILL.md，不告警。
			fmt.Fprintln(w, "\t(回收站条目，内容在时间戳子目录中)")
			continue
		}
		if !s.HasSKILLMD {
			fmt.Fprint(w, "\t⚠ 缺少 SKILL.md")
		}
		switch {
		case s.MetaErr != "":
			fmt.Fprintf(w, "\t⚠ 元数据损坏: %s", s.MetaErr)
		case s.Meta == nil:
			fmt.Fprint(w, "\t(无元数据)")
		default:
			fmt.Fprintf(w, "\t%s", s.Meta.Source.Type)
			if s.Meta.Source.URL != "" {
				fmt.Fprintf(w, " %s", s.Meta.Source.URL)
			}
			if s.Meta.Source.Path != "" {
				fmt.Fprintf(w, " %s", s.Meta.Source.Path)
			}
			if s.Meta.UpdatedAt != "" {
				fmt.Fprintf(w, "\tupdatedAt=%s", s.Meta.UpdatedAt)
			}
		}
		fmt.Fprintln(w)
	}
}

// ------------------------- disable / enable ---------------------------

// cmdToggle 实现 `kura disable NAME` 与 `kura enable NAME`。
func cmdToggle(args []string, stdout, stderr io.Writer, action string, pre *globalFlags) int {
	g := &globalFlags{}
	fs := newFlagSet(action, stderr, g, pre)
	positional, err := parseArgs(fs, args)
	if err != nil {
		return usageError(stderr, err)
	}
	if len(positional) != 1 {
		return printError(g, stderr, "用法: kura %s <NAME>", action)
	}
	name := positional[0]
	c, err := resolveConfig(g)
	if err != nil {
		return printError(g, stderr, "%v", err)
	}
	var res *store.Result
	if action == "disable" {
		res, err = c.Disable(name)
	} else {
		res, err = c.Enable(name)
	}
	if err != nil {
		return printError(g, stderr, "%s 失败: %v", action, err)
	}
	if g.json {
		return writeJSONOrErr(stdout, resultJSON{
			ID: res.ID, State: string(res.State), Dir: res.Dir, SnapshotDir: res.SnapshotDir,
		})
	}
	fmt.Fprintf(stdout, "%s: %s -> %s\n", action, res.ID, res.Dir)
	return ExitOK
}

// ------------------------------ remove --------------------------------

// cmdRemove 实现 `kura remove NAME [--stamp TS]`。--stamp 指定回收站与快照
// 的时间戳目录名；省略时由 store 用当前时间。
func cmdRemove(args []string, stdout, stderr io.Writer, pre *globalFlags) int {
	g := &globalFlags{}
	fs := newFlagSet("remove", stderr, g, pre)
	stampVal := fs.String("stamp", "", "回收站/快照时间戳目录名（默认当前时间）")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return usageError(stderr, err)
	}
	if len(positional) != 1 {
		return printError(g, stderr, "用法: kura remove <NAME> [--stamp TS]")
	}
	name := positional[0]
	c, err := resolveConfig(g)
	if err != nil {
		return printError(g, stderr, "%v", err)
	}
	var res *store.Result
	if *stampVal == "" {
		res, err = c.Remove(name)
	} else {
		res, err = c.RemoveWithStamp(name, *stampVal)
	}
	if err != nil {
		return printError(g, stderr, "remove 失败: %v", err)
	}
	if g.json {
		return writeJSONOrErr(stdout, resultJSON{
			ID: res.ID, State: string(res.State), Dir: res.Dir, SnapshotDir: res.SnapshotDir,
		})
	}
	fmt.Fprintf(stdout, "removed: %s -> %s\n", res.ID, res.Dir)
	if res.SnapshotDir != "" {
		fmt.Fprintf(stdout, "snapshot: %s\n", res.SnapshotDir)
	}
	return ExitOK
}

// ------------------------------ restore -------------------------------

// cmdRestore 实现 `kura restore NAME [--stamp TS]`（省略 --stamp 取最新）。
func cmdRestore(args []string, stdout, stderr io.Writer, pre *globalFlags) int {
	g := &globalFlags{}
	fs := newFlagSet("restore", stderr, g, pre)
	stampVal := fs.String("stamp", "", "恢复回收站中指定时间戳的版本（默认最新一次）")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return usageError(stderr, err)
	}
	if len(positional) != 1 {
		return printError(g, stderr, "用法: kura restore <NAME> [--stamp TS]")
	}
	name := positional[0]
	c, err := resolveConfig(g)
	if err != nil {
		return printError(g, stderr, "%v", err)
	}
	res, err := c.Restore(name, *stampVal)
	if err != nil {
		return printError(g, stderr, "restore 失败: %v", err)
	}
	if g.json {
		return writeJSONOrErr(stdout, resultJSON{
			ID: res.ID, State: string(res.State), Dir: res.Dir, SnapshotDir: res.SnapshotDir,
		})
	}
	fmt.Fprintf(stdout, "restored: %s -> %s\n", res.ID, res.Dir)
	return ExitOK
}

// ---------------------------- import-meta -----------------------------

// importMetaResult 是 import-meta --json 的顶层形状。
type importMetaResult struct {
	Root        string   `json:"root"`
	LockPath    string   `json:"lockPath"`
	LockFound   bool     `json:"lockFound"`
	DryRun      bool     `json:"dryRun"`
	WouldImport []string `json:"wouldImport,omitempty"` // --dry-run 时的候选列表
	Imported    []string `json:"imported,omitempty"`    // 实际写入的列表
}

// cmdImportMeta 实现 `kura import-meta [--dry-run] [--lock PATH]`。
// lock 默认取根目录同级的 .skill-lock.json（REQUIREMENTS.md 约定），
// --lock 可显式覆盖。真实的用户 ~/.agents 上的导入必须加 --dry-run。
// .skill-lock.json 只描述默认根目录：--root 指向界面里追加的目录时与界面
// 一样拒绝（含 --dry-run 与 --lock），退出码 1。
func cmdImportMeta(args []string, stdout, stderr io.Writer, pre *globalFlags) int {
	g := &globalFlags{}
	fs := newFlagSet("import-meta", stderr, g, pre)
	dryRun := fs.Bool("dry-run", false, "只分析将要导入哪些技能，不写文件")
	lockPath := fs.String("lock", "", "lock 文件路径（默认 <root>/../.skill-lock.json）")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return usageError(stderr, err)
	}
	if len(positional) > 0 {
		return printError(g, stderr, "import-meta 不接受位置参数")
	}
	c, err := resolveConfig(g)
	if err != nil {
		return printError(g, stderr, "%v", err)
	}
	if !c.CanImportMeta() {
		return printError(g, stderr, "import-meta 失败: %v: %s", store.ErrImportDefaultOnly, c.Root)
	}
	lp := *lockPath
	if lp == "" {
		lp = filepath.Join(filepath.Dir(c.Root), ".skill-lock.json")
	}
	if *dryRun {
		names, lerr := dryRunImport(c, lp)
		if lerr != nil {
			return printError(g, stderr, "%v", lerr)
		}
		_, found, _ := store.ReadLockFile(lp)
		return emitImportMeta(stdout, g, importMetaResult{
			Root: c.Root, LockPath: lp, LockFound: found, DryRun: true, WouldImport: names,
		})
		// 注意：dryRunImport 返回 (nil, nil) 表示 lock 文件不存在，此时
		// WouldImport 为空列表而非 null（见 emitImportMeta 的 JSON 处理）。
	}
	imported, err := c.ImportMeta(lp)
	if err != nil {
		return printError(g, stderr, "import-meta 失败: %v", err)
	}
	_, lockFound, _ := store.ReadLockFile(lp)
	if imported == nil {
		imported = []string{} // JSON 里输出 [] 而不是 null
	}
	return emitImportMeta(stdout, g, importMetaResult{
		Root: c.Root, LockPath: lp, LockFound: lockFound, Imported: imported,
	})
}

// emitImportMeta 按模式输出 import-meta 结果并返回退出码。
func emitImportMeta(stdout io.Writer, g *globalFlags, r importMetaResult) int {
	if g.json {
		if r.WouldImport == nil {
			r.WouldImport = []string{}
		}
		if r.Imported == nil {
			r.Imported = []string{}
		}
		return writeJSONOrErr(stdout, r)
	}
	label := "已导入"
	list := r.Imported
	if r.DryRun {
		label = "将要导入"
		list = r.WouldImport
	}
	fmt.Fprintf(stdout, "lock: %s", r.LockPath)
	if !r.LockFound {
		fmt.Fprint(stdout, "（不存在）")
	}
	fmt.Fprintln(stdout)
	if len(list) == 0 {
		fmt.Fprintln(stdout, "没有需要导入的技能")
		return ExitOK
	}
	fmt.Fprintf(stdout, "%s %d 个技能:\n", label, len(list))
	for _, n := range list {
		fmt.Fprintf(stdout, "  %s\n", n)
	}
	return ExitOK
}

// dryRunImport 计算 --dry-run 的候选列表：根目录下无元数据、且在 lock 文件
// 中列出的技能名（与 store.ImportMeta 的判定完全一致，只是不写文件）。
// lock 文件不存在返回 (nil, nil)。
func dryRunImport(c *store.Config, lockPath string) ([]string, error) {
	lf, found, err := store.ReadLockFile(lockPath)
	if err != nil || !found {
		return nil, err
	}
	skills, err := c.Scan()
	if err != nil {
		return nil, err
	}
	var names []string
	for _, s := range skills {
		if s.Meta != nil {
			continue // 已有元数据，不覆盖。
		}
		if _, ok := lf.Skills[s.Name]; ok {
			names = append(names, s.Name)
		}
	}
	return names, nil
}

// -------------------------------- add ---------------------------------

// addFlags 承载 `kura add` 的来源参数。
type addFlags struct {
	local   string
	github  string
	subPath string
	ref     string
	name    string
}

// candidatesJSON 是 `add --json` 遇到多候选时的顶层形状：candidates 是
// 相对仓库根的 slash 路径数组（已排序），可直接拿去当 --sub-path 用。
type candidatesJSON struct {
	Error      string   `json:"error"`
	Repo       string   `json:"repo"`
	Candidates []string `json:"candidates"`
}

// cmdAdd 实现 `kura add --local PATH [--name N]` 与
// `kura add --github owner/repo [--sub-path P] [--ref R] [--name N]`。
// --name 直接传给 store.AddOptions.Name（安装时命名，元数据如实记录
// 真实来源，与技能名无关）。
// github 来源不给 --sub-path 时由 store 自动发现仓库内的技能：唯一候选
// 直接安装；多个候选列出清单并以退出码 1 失败（沿用「操作失败」语义），
// 用户用 --sub-path 指定其中一个后再装。
func cmdAdd(args []string, stdout, stderr io.Writer, pre *globalFlags) int {
	g := &globalFlags{}
	fs := newFlagSet("add", stderr, g, pre)
	af := &addFlags{}
	fs.StringVar(&af.local, "local", "", "本地技能目录")
	fs.StringVar(&af.github, "github", "", "GitHub 仓库，格式 owner/repo")
	fs.StringVar(&af.subPath, "sub-path", "", "仓库内技能子目录（github 来源可选；省略时自动发现仓库内的技能，多个候选时必填其一）")
	fs.StringVar(&af.ref, "ref", "", "分支/标签/commit（github 来源可选）")
	fs.StringVar(&af.name, "name", "", "安装后的技能名（默认取来源目录名）")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return usageError(stderr, err)
	}
	if len(positional) > 0 {
		return printError(g, stderr, "add 不接受位置参数")
	}
	if (af.local == "") == (af.github == "") {
		return printError(g, stderr, "用法: kura add --local PATH 或 kura add --github owner/repo（必须且只能提供一个来源）")
	}
	c, err := resolveConfig(g)
	if err != nil {
		return printError(g, stderr, "%v", err)
	}

	var res *store.Result
	if af.local != "" {
		res, err = c.Add(&store.AddOptions{LocalPath: af.local, Name: af.name})
	} else {
		res, err = c.Add(&store.AddOptions{Repo: af.github, SubPath: af.subPath, Ref: af.ref, Name: af.name})
	}
	if err != nil {
		// 多候选是「需要用户选择」而非程序故障：文本模式列出清单，
		// JSON 模式给出结构化 candidates 数组；退出码沿用操作失败（1）。
		var ce *store.CandidatesError
		if errors.As(err, &ce) {
			if g.json {
				_ = writeJSON(stderr, candidatesJSON{
					Error: ce.Error(), Repo: ce.Repo, Candidates: ce.Paths,
				})
				return ExitError
			}
			fmt.Fprintf(stderr, "kura: %s\n", ce.Error())
			return ExitError
		}
		return printError(g, stderr, "add 失败: %v", err)
	}
	if g.json {
		return writeJSONOrErr(stdout, resultJSON{
			ID: res.ID, State: string(res.State), Dir: res.Dir, SnapshotDir: res.SnapshotDir,
		})
	}
	fmt.Fprintf(stdout, "added: %s -> %s\n", res.ID, res.Dir)
	return ExitOK
}

// renameInstalled 已废弃删除：--name 现在由 store.Add 在安装时直接命名
//（见 AddOptions.Name），不再需要安装后改名。

// ------------------------------- update -------------------------------

// cmdUpdate 实现 `kura update NAME` 与 `kura update --check`（里程碑 4）。
//
//   - `kura update NAME`            按元数据记录的来源重新拉取并替换（先快照）；
//   - `kura update --check NAME`    预览更新会改什么，不落盘；有待更新退出 3，无则 0；
//   - `kura update --check --all`   对根目录全部 enabled 技能逐个预览。
func cmdUpdate(args []string, stdout, stderr io.Writer, pre *globalFlags) int {
	g := &globalFlags{}
	fs := newFlagSet("update", stderr, g, pre)
	check := fs.Bool("check", false, "预览更新会改什么（不修改任何文件）；有待更新时退出码 3")
	all := fs.Bool("all", false, "配合 --check：检查根目录全部技能")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return usageError(stderr, err)
	}
	if !*check {
		// 原有路径：直接更新。
		if len(positional) != 1 || *all {
			return printError(g, stderr, "用法: kura update <NAME>")
		}
		return runUpdateApply(g, positional[0], stdout, stderr)
	}
	// --check 路径：NAME 与 --all 二选一。
	if *all {
		if len(positional) != 0 {
			// 用法错误：--all 不接受 NAME。
			fmt.Fprintf(stderr, "kura: 用法: kura update --check --all（不能同时给 NAME）\n")
			_ = writeJSON(stderr, errorJSON{Error: "用法: kura update --check --all（不能同时给 NAME）"})
			return ExitUsage
		}
		return runUpdateCheckAll(g, stdout, stderr)
	}
	if len(positional) != 1 {
		// 用法错误（缺 NAME）：按约定退出 2，并把用法写进 stderr。
		fmt.Fprintf(stderr, "kura: 用法: kura update --check <NAME> | kura update --check --all\n")
		_ = writeJSON(stderr, errorJSON{Error: "用法: kura update --check <NAME> | kura update --check --all"})
		return ExitUsage
	}
	return runUpdateCheck(g, positional[0], stdout, stderr)
}

// runUpdateApply 执行真正的更新（原有行为，见 store.Update）。
func runUpdateApply(g *globalFlags, name string, stdout, stderr io.Writer) int {
	c, err := resolveConfig(g)
	if err != nil {
		return printError(g, stderr, "%v", err)
	}
	res, err := c.Update(name)
	if err != nil {
		return printError(g, stderr, "update 失败: %v", err)
	}
	if g.json {
		return writeJSONOrErr(stdout, resultJSON{
			ID: res.ID, State: string(res.State), Dir: res.Dir, SnapshotDir: res.SnapshotDir,
		})
	}
	fmt.Fprintf(stdout, "updated: %s\n", res.ID)
	if res.SnapshotDir != "" {
		fmt.Fprintf(stdout, "snapshot: %s\n", res.SnapshotDir)
	}
	return ExitOK
}

// checkJSON 是 `kura update --check <NAME> --json` 的顶层形状（稳定契约）。
// files 只含 added/changed/removed 的文件，unchanged 是两侧一致的文件数；
// 数组字段永远输出 []（不输出 null）。
type checkJSON struct {
	// Skill 是技能名。
	Skill string `json:"skill"`
	// SourceType 是元数据记录的来源类型（local/github）。
	SourceType string `json:"sourceType"`
	// Commit 是上游最新 commit（local 来源无此字段）。
	Commit string `json:"commit,omitempty"`
	// HasChanges 是是否存在任何文件差异。
	HasChanges bool `json:"hasChanges"`
	// Basis 说明凭什么判定：commit（上游 commit 与安装时相同）、tree（上游技能
	// 目录的树哈希相同）、content（逐文件比较了内容；local 来源恒为此值）。
	// commit/tree 时没有下载任何文件，files 必为空。
	Basis string `json:"basis"`
	// LocalModified 表示已安装内容与安装/更新时记录的哈希不一致（被手改过）。
	LocalModified bool `json:"localModified"`
	// OldHash/NewHash 是目录级内容哈希（与 .kura-meta.json 的 contentHash 同格式）。
	OldHash string `json:"oldHash"`
	NewHash string `json:"newHash"`
	// Files 是全部差异文件（按路径排序）。
	Files []fileDiffJSON `json:"files"`
	// Unchanged 是两侧内容一致的文件数。
	Unchanged int `json:"unchanged"`
}

// fileDiffJSON 是 --check 输出里的单个文件差异条目。
type fileDiffJSON struct {
	Path    string `json:"path"`
	Status  string `json:"status"` // added | changed | removed
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	OldHash string `json:"oldHash,omitempty"`
	NewHash string `json:"newHash,omitempty"`
}

// toCheckJSON 把 store.UpdatePreview 转成 CLI 的输出形状。
func toCheckJSON(p *store.UpdatePreview) checkJSON {
	files := make([]fileDiffJSON, 0, len(p.Files))
	for _, f := range p.Files {
		files = append(files, fileDiffJSON{
			Path: f.Path, Status: f.Status,
			Added: f.Added, Removed: f.Removed,
			OldHash: f.OldHash, NewHash: f.NewHash,
		})
	}
	return checkJSON{
		Skill: p.ID, SourceType: p.SourceType, Commit: p.Commit,
		HasChanges: p.HasChanges(), Basis: p.Basis, LocalModified: p.LocalModified,
		OldHash: p.OldHash, NewHash: p.NewHash,
		Files: files, Unchanged: p.Unchanged,
	}
}

// runUpdateCheck 预览单个技能的更新。退出码：3 有待更新，0 无变化；
// 预览失败按操作失败处理（退出码 1）。绝不修改磁盘。
func runUpdateCheck(g *globalFlags, name string, stdout, stderr io.Writer) int {
	c, err := resolveConfig(g)
	if err != nil {
		return printError(g, stderr, "%v", err)
	}
	p, err := c.PreviewUpdate(name)
	if err != nil {
		return printError(g, stderr, "check 失败: %v", err)
	}
	cj := toCheckJSON(p)
	if g.json {
		if code := writeJSONOrErr(stdout, cj); code != ExitOK {
			return code
		}
	} else {
		printCheckText(stdout, &cj)
	}
	// 退出码与输出模式无关：有待更新 3，无变化 0。
	if cj.HasChanges {
		return ExitDoctorProblem
	}
	return ExitOK
}

// checkAllJSON 是 `kura update --check --all --json` 的顶层形状。
// results 按 skill 名排序（store.Scan 保证）；数组恒为 []（不输出 null）。
type checkAllJSON struct {
	// Root 是被检查的技能根目录。
	Root string `json:"root"`
	// Results 是每个技能的预览结果（含失败条目）。
	Results []checkEntryJSON `json:"results"`
}

// checkEntryJSON 是 --all 汇总里的单技能条目：成功时带 check 载荷，
// 失败时只有 skill + error。
type checkEntryJSON struct {
	Skill string `json:"skill"`
	// Error 是预览失败原因；成功时省略。
	Error string `json:"error,omitempty"`
	// Check 是预览成功时的完整结果；失败时省略。
	Check *checkJSON `json:"check,omitempty"`
}

// runUpdateCheckAll 预览根目录全部 enabled 技能。退出码：任何一个技能
// 有待更新即 3；全部最新（或没有技能）为 0。预览失败记入对应条目，
// 不中断整体（与 doctor 的容错口径一致）。
func runUpdateCheckAll(g *globalFlags, stdout, stderr io.Writer) int {
	c, err := resolveConfig(g)
	if err != nil {
		return printError(g, stderr, "%v", err)
	}
	skills, err := c.Scan()
	if err != nil {
		return printError(g, stderr, "扫描根目录失败: %v", err)
	}
	names := make([]string, len(skills))
	for i, s := range skills {
		names[i] = s.Name
	}
	out := checkAllJSON{Root: c.Root, Results: []checkEntryJSON{}}
	pending := false
	// 批量检查：同一仓库的技能共用一次 ls-remote / clone。
	for _, r := range c.PreviewUpdates(names) {
		if r.Err != nil {
			out.Results = append(out.Results, checkEntryJSON{Skill: r.Name, Error: r.Err.Error()})
			continue
		}
		cj := toCheckJSON(r.Preview)
		out.Results = append(out.Results, checkEntryJSON{Skill: r.Name, Check: &cj})
		if cj.HasChanges {
			pending = true
		}
	}
	if g.json {
		if code := writeJSONOrErr(stdout, out); code != ExitOK {
			return code
		}
	} else {
		for _, r := range out.Results {
			if r.Error != "" {
				fmt.Fprintf(stdout, "[%s] 检查失败: %s\n", r.Skill, r.Error)
				continue
			}
			printCheckText(stdout, r.Check)
		}
		fmt.Fprintf(stdout, "共 %d 个技能，%d 个有待更新\n", len(out.Results), countPending(out.Results))
	}
	if pending {
		return ExitDoctorProblem
	}
	return ExitOK
}

// countPending 统计汇总里有待更新的技能数。
func countPending(rs []checkEntryJSON) int {
	n := 0
	for _, r := range rs {
		if r.Check != nil && r.Check.HasChanges {
			n++
		}
	}
	return n
}

// printCheckText 输出人类可读的 --check 结果。
func printCheckText(w io.Writer, cj *checkJSON) {
	if !cj.HasChanges {
		note := ""
		switch cj.Basis {
		case store.BasisCommit:
			note = "（上游 commit 未变）"
		case store.BasisTree:
			note = "（上游该目录未变）"
		}
		fmt.Fprintf(w, "%s: 已是最新%s\n", cj.Skill, note)
		if cj.LocalModified {
			fmt.Fprintf(w, "  注意: 本地内容与安装时不一致（被改过）\n")
		}
		return
	}
	fmt.Fprintf(w, "%s: %d 个文件有变化（新增 %d · 修改 %d · 删除 %d）\n",
		cj.Skill, len(cj.Files),
		statusCount(cj, store.DiffAdded), statusCount(cj, store.DiffChanged), statusCount(cj, store.DiffRemoved))
	for _, f := range cj.Files {
		fmt.Fprintf(w, "  %-8s +%-4d -%-4d %s\n", statusLabel(f.Status), f.Added, f.Removed, f.Path)
	}
	fmt.Fprintf(w, "  哈希: %s -> %s\n", shortHashCLI(cj.OldHash), shortHashCLI(cj.NewHash))
	if cj.Commit != "" {
		fmt.Fprintf(w, "  上游 commit: %s\n", cj.Commit)
	}
	if cj.LocalModified {
		fmt.Fprintf(w, "  注意: 本地内容已被改过，应用更新会覆盖这些改动（旧内容在快照中）\n")
	}
}

// statusLabel 把差异状态翻译成短标签（对齐 --check 的文本输出）。
func statusLabel(status string) string {
	switch status {
	case store.DiffAdded:
		return "新增"
	case store.DiffRemoved:
		return "删除"
	default:
		return "修改"
	}
}

// statusCount 统计某状态的文件数。
func statusCount(cj *checkJSON, status string) int {
	n := 0
	for _, f := range cj.Files {
		if f.Status == status {
			n++
		}
	}
	return n
}

// shortHashCLI 缩短 "sha256:..." 哈希用于文本展示（复用 doctor 的口径）。
func shortHashCLI(h string) string { return shortHash(h) }
