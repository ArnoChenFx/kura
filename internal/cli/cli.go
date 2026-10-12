// Package cli 实现 kura 的无界面命令行界面（headless CLI，里程碑 3）。
//
// 职责边界：
//   - 只做参数解析、调用 internal/store、格式化输出（人类可读 / JSON）；
//   - 绝不调用 os.Exit：Run 返回退出码，由 cmd/kura/main.go 统一 os.Exit；
//   - 绝不 import package ui，保持纯文本、可脱离窗口环境测试。
//
// 全局约定：
//   - 所有子命令都支持 --json（机器可读输出）、--root/--home（目录覆盖）；
//     环境变量 KURA_ROOT/KURA_HOME 等价于对应 flag，命令行参数优先级更高；
//   - 退出码：0 成功；1 操作失败（技能不存在、同名冲突、IO 错误等）；
//     2 用法错误（未知命令、flag 解析失败、参数个数不对）；3 doctor 发现问题。
//
// JSON 输出契约（稳定，供未来 UI 消费，详见各类型注释与 docs/note/cli-json-shape.md）：
//
//	list/scan  : {"root": string, "skills": [...]}
//	变更命令   : {"id", "state", "dir", "snapshotDir"?}
//	doctor     : {"root": string, "problems": [{"kind","skill","detail"}]}
//	import-meta: {"root", "lockPath", "dryRun", "wouldImport"|"imported"}
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ArnoChenFx/kura/internal/store"
)

// Version 是 CLI 打印的版本号，与 mygo.json 保持一致。
const Version = "0.1.0"

// 退出码约定（见包注释与 --help）。
const (
	ExitOK            = 0 // 成功
	ExitError         = 1 // 操作失败（技能不存在、同名冲突、IO 错误等）
	ExitUsage         = 2 // 用法错误（未知命令、flag 解析失败、参数个数不对）
	ExitDoctorProblem = 3 // doctor 发现问题
)

// Run 解析 args（不含程序名）并执行对应子命令，返回进程退出码。
// stdout/stderr 由调用方注入，便于测试捕获；本函数绝不调用 os.Exit。
//
// 全局 flag（--json/--root/--home）允许出现在子命令之前或之后：
// 先用一个前置 FlagSet 把子命令前的全局 flag 剥掉，剩下的第一个 token
// 就是子命令；子命令内部的 FlagSet 再以这些值为默认值解析它自己的参数，
// 因此 `kura --json list` 与 `kura list --json` 等价。
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return ExitUsage
	}
	// 前置 flag 解析：只认 --json/--root/--home，遇到子命令即停。
	// flag 包停在第一个非 flag token 上并把它留在 NArg 里，正合需要。
	pre := &globalFlags{}
	preFS := flag.NewFlagSet("kura", flag.ContinueOnError)
	preFS.SetOutput(io.Discard) // 错误由子命令层重新解析时给出完整提示
	preFS.BoolVar(&pre.json, "json", false, "")
	preFS.StringVar(&pre.root, "root", "", "")
	preFS.StringVar(&pre.home, "home", "", "")
	if err := preFS.Parse(args); err != nil {
		// flag 包对 -h/--help 有内建处理：未定义该 flag 时返回 ErrHelp。
		if errors.Is(err, flag.ErrHelp) {
			printUsage(stdout)
			return ExitOK
		}
		fmt.Fprintf(stderr, "kura: 参数错误: %v（用 kura --help 查看用法）\n", err)
		_ = writeJSON(stderr, errorJSON{Error: "参数错误: " + err.Error()})
		return ExitUsage
	}
	rest := preFS.Args()
	if len(rest) == 0 {
		// 只有全局 flag 没有子命令：视为缺子命令。
		printUsage(stderr)
		return ExitUsage
	}
	switch rest[0] {
	case "help", "--help", "-h":
		printUsage(stdout)
		return ExitOK
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "kura %s\n", Version)
		return ExitOK
	}
	cmd, sub := rest[0], rest[1:]
	switch cmd {
	case "list":
		return cmdList(sub, stdout, stderr, pre)
	case "scan":
		// scan 与 list 同义；历史上它强调"可用 --root 覆盖根目录"。
		return cmdList(sub, stdout, stderr, pre)
	case "disable":
		return cmdToggle(sub, stdout, stderr, "disable", pre)
	case "enable":
		return cmdToggle(sub, stdout, stderr, "enable", pre)
	case "remove":
		return cmdRemove(sub, stdout, stderr, pre)
	case "restore":
		return cmdRestore(sub, stdout, stderr, pre)
	case "import-meta":
		return cmdImportMeta(sub, stdout, stderr, pre)
	case "add":
		return cmdAdd(sub, stdout, stderr, pre)
	case "update":
		return cmdUpdate(sub, stdout, stderr, pre)
	case "doctor":
		return cmdDoctor(sub, stdout, stderr, pre)
	default:
		fmt.Fprintf(stderr, "kura: 未知命令 %q\n\n", cmd)
		printUsage(stderr)
		return ExitUsage
	}
}

// globalFlags 是所有子命令共享的 flag 集合。
// 前置解析（子命令之前）得到 pre 值；子命令自己的 FlagSet 以它为默认值，
// 于是子命令之后的同名字 flag 可以覆盖前置值。
type globalFlags struct {
	json bool
	root string
	home string
}

// newFlagSet 构造带全局 flag 的 FlagSet（解析错误输出到 stderr）。
// 每个子命令各建一个 FlagSet，因此全局 flag 可以写在子命令之后，
// 例如 `kura list --json`、`kura scan --root PATH`。
// pre 非 nil 时其值作为默认值传入（见 Run 的前置解析）。
func newFlagSet(name string, stderr io.Writer, g *globalFlags, pre *globalFlags) *flag.FlagSet {
	fs := flag.NewFlagSet("kura "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	defJSON, defRoot, defHome := false, "", ""
	if pre != nil {
		defJSON, defRoot, defHome = pre.json, pre.root, pre.home
	}
	fs.BoolVar(&g.json, "json", defJSON, "以 JSON 输出（错误也以 JSON 输出到 stderr）")
	fs.StringVar(&g.root, "root", defRoot, "技能根目录（默认 $KURA_ROOT 或 ~/.agents/skills）")
	fs.StringVar(&g.home, "home", defHome, "kura 数据目录（默认 $KURA_HOME 或 ~/.kura）")
	return fs
}

// resolveConfig 把 flag 与环境变量合成为 store.Config。
// 优先级：flag > 环境变量 > 默认值；root/home 都转为绝对路径，
// 保证 JSON 输出中的路径稳定且与工作目录无关。
func resolveConfig(g *globalFlags) (*store.Config, error) {
	def, err := store.DefaultConfig()
	if err != nil {
		return nil, err
	}
	root := firstNonEmpty(g.root, os.Getenv("KURA_ROOT"), def.Root)
	home := firstNonEmpty(g.home, os.Getenv("KURA_HOME"), def.Home)
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	if abs, err := filepath.Abs(home); err == nil {
		home = abs
	}
	return store.NewConfig(root, home)
}

// firstNonEmpty 返回第一个非空字符串（全空返回空串）。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// parseArgs 解析 flag 与位置参数，允许二者任意交错。
//
// Go 标准库 flag 在遇到第一个位置参数时就停止解析，导致
// `kura remove NAME --stamp TS` 里的 --stamp 落进位置参数。这里通过
// 多轮 Parse 解决：每轮把停止处的一个位置参数收走，继续解析剩余 token，
// 直到没有任何位置参数为止。返回收齐的位置参数列表。
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		rest = fs.Args()[1:]
	}
}

// printUsage 打印帮助文本（命令列表、全局 flag、退出码与 JSON 契约摘要）。
func printUsage(w io.Writer) {
	fmt.Fprint(w, usageText)
}

const usageText = `kura —— agent skills 管理器（无界面命令行）

用法:
  kura <命令> [参数] [flags]

命令:
  list                 列出全部技能（enabled + disabled + trashed）
  scan                 同 list（别名，习惯上配合 --root 覆盖根目录）
  disable <NAME>       禁用技能（移动到 <home>/disabled/，纯移动）
  enable <NAME>        启用技能（从 disabled/ 移回根目录）
  remove <NAME> [--stamp TS]
                       移入回收站（先写快照）；--stamp 指定回收站时间戳目录名
  restore <NAME> [--stamp TS]
                       从回收站恢复；--stamp 省略时取最新一次
  import-meta [--dry-run] [--lock PATH]
                       依据根目录同级的 .skill-lock.json 补写缺失的
                       .kura-meta.json；--dry-run 只分析不写文件
  add --local PATH [--name N]
                       从本地目录安装技能
  add --github owner/repo [--sub-path P] [--ref R] [--name N]
                       从 GitHub 仓库安装技能
  update <NAME>        按元数据记录的来源重新拉取并替换内容（先快照）
  update --check <NAME>
                       预览 NAME 的更新会改什么（不修改任何文件）；
                       有待更新退出码 3，无变化退出码 0
  update --check --all
                       预览根目录全部技能的更新（每个技能一段结果）
  doctor               只读体检：缺元数据 / 元数据损坏 / 内容漂移 /
                       缺 SKILL.md / disabled 与 trash 中的残留条目
  version              打印版本

全局 flags（可写在任何子命令之后）:
  --json        输出 JSON；出错时错误信息以 {"error": "..."} 输出到 stderr
  --root PATH   技能根目录（默认 $KURA_ROOT 或 ~/.agents/skills）
  --home PATH   kura 数据目录（默认 $KURA_HOME 或 ~/.kura）

退出码:
  0  成功
  1  操作失败（技能不存在、同名冲突、IO 错误等）
  2  用法错误（未知命令、flag 解析失败、参数个数不对）
  3  doctor 发现问题（明细见其输出）

JSON 输出形状（--json 时）:
  list/scan   {"root": string, "skills": [{name, state, hasSkillMd, dir,
              sourceType?, sourceURL?, ref?, subPath?, pluginName?,
              updatedAt?, metaError?}]}
  变更命令     {"id": string, "state": string, "dir": string,
              "snapshotDir"?: string}
  doctor      {"root": string, "problems": [{kind, skill, detail}]}
              kind ∈ not-a-dir | no-meta | meta-parse | hash-drift |
                      no-skillmd | leftover
  import-meta {"root": string, "lockPath": string, "dryRun": bool,
              "wouldImport"?: [string], "imported"?: [string]}
  update --check NAME
              {"skill": string, "sourceType": string, "commit"?: string,
              "hasChanges": bool, "oldHash": string, "newHash": string,
              "files": [{path, status, added, removed, oldHash?, newHash?}],
              "unchanged": int}
              status ∈ added | changed | removed；files 只含差异文件，
              无变化时为 []。退出码：有待更新 3，无变化 0。
  update --check --all
              {"root": string,
              "results": [{skill, error?, check?}]}
              check 的形状同上；任一技能有待更新退出码 3。
`
