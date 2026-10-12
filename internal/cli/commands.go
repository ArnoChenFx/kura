package cli

// 命令实现：list/scan、disable/enable、remove、restore、import-meta、
// add、update。每个命令返回退出码，只通过注入的 writer 输出。

import (
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

// cmdAdd 实现 `kura add --local PATH [--name N]` 与
// `kura add --github owner/repo [--sub-path P] [--ref R] [--name N]`。
// --name 直接传给 store.AddOptions.Name（安装时命名，元数据如实记录
// 真实来源，与技能名无关）。
func cmdAdd(args []string, stdout, stderr io.Writer, pre *globalFlags) int {
	g := &globalFlags{}
	fs := newFlagSet("add", stderr, g, pre)
	af := &addFlags{}
	fs.StringVar(&af.local, "local", "", "本地技能目录")
	fs.StringVar(&af.github, "github", "", "GitHub 仓库，格式 owner/repo")
	fs.StringVar(&af.subPath, "sub-path", "", "仓库内技能子目录（github 来源可选）")
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

// cmdUpdate 实现 `kura update NAME`：按元数据记录的来源重新拉取并替换。
func cmdUpdate(args []string, stdout, stderr io.Writer, pre *globalFlags) int {
	g := &globalFlags{}
	fs := newFlagSet("update", stderr, g, pre)
	positional, err := parseArgs(fs, args)
	if err != nil {
		return usageError(stderr, err)
	}
	if len(positional) != 1 {
		return printError(g, stderr, "用法: kura update <NAME>")
	}
	name := positional[0]
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
