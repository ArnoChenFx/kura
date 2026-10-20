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
//   - 界面语言由 --lang（或 KURA_LANG）切换：flag > 环境变量 > config.json 的
//     lang > 跟随系统 locale（见 lang.go）；文案一律经 locales 取（见 locales/），
//     中文与英文两套；
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
	"github.com/ArnoChenFx/kura/locales"
)

// Version 是 CLI 打印的版本号，与 mygo.json 保持一致。
const Version = "0.1.14"

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
// 全局 flag（--json/--root/--home/--lang）允许出现在子命令之前或之后：
// 先用一个前置 FlagSet 把子命令前的全局 flag 剥掉，剩下的第一个 token
// 就是子命令；子命令内部的 FlagSet 再以这些值为默认值解析它自己的参数，
// 因此 `kura --json list` 与 `kura list --json` 等价。
//
// --lang 比较特殊：它的值由 applyLanguage 在最前面从完整参数里预扫并应用
// （见 lang.go），这里注册只是为了让 flag 包认识它、写在哪儿都不报错。
func Run(args []string, stdout, stderr io.Writer) int {
	// 界面语言必须在任何输出之前定下来（--lang > KURA_LANG > config.json >
	// 跟随系统）；写坏的语言值在这里就报用法错误。
	if code := applyLanguage(args, os.Getenv, stderr); code != ExitOK {
		return code
	}
	if len(args) == 0 {
		printUsage(stderr)
		return ExitUsage
	}
	// 前置 flag 解析：只认 --json/--root/--home/--lang，遇到子命令即停。
	// flag 包停在第一个非 flag token 上并把它留在 NArg 里，正合需要。
	pre := &globalFlags{}
	preFS := flag.NewFlagSet("kura", flag.ContinueOnError)
	preFS.SetOutput(io.Discard) // 错误由子命令层重新解析时给出完整提示
	preFS.BoolVar(&pre.json, "json", false, "")
	preFS.StringVar(&pre.root, "root", "", "")
	preFS.StringVar(&pre.home, "home", "", "")
	preFS.StringVar(&pre.lang, "lang", "", "")
	if err := preFS.Parse(args); err != nil {
		// flag 包对 -h/--help 有内建处理：未定义该 flag 时返回 ErrHelp。
		if errors.Is(err, flag.ErrHelp) {
			printUsage(stdout)
			return ExitOK
		}
		return badFlags(stderr, err)
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
		fmt.Fprint(stderr, locales.CliErrUnknownCommand(cmd))
		printUsage(stderr)
		return ExitUsage
	}
}

// badFlags 报「全局 flag 解析失败」：人类可读的一行加（--json 时）stderr 上的
// 结构化错误。文本与结构化错误都从目录取，两种语言下都完整成句。
func badFlags(stderr io.Writer, err error) int {
	fmt.Fprint(stderr, locales.CliErrBadFlag(err))
	_ = writeJSON(stderr, errorJSON{Error: locales.CliErrBadFlagJSON(err)})
	return ExitUsage
}

// globalFlags 是所有子命令共享的 flag 集合。
// 前置解析（子命令之前）得到 pre 值；子命令自己的 FlagSet 以它为默认值，
// 于是子命令之后的同名字 flag 可以覆盖前置值。
// lang 只需能解析（写在哪一处都行）：它的值已由 applyLanguage 预扫并应用，
// 这里注册只是为了让 flag 包认识这个 flag。
type globalFlags struct {
	json bool
	root string
	home string
	lang string
}

// newFlagSet 构造带全局 flag 的 FlagSet（解析错误输出到 stderr）。
// 每个子命令各建一个 FlagSet，因此全局 flag 可以写在子命令之后，
// 例如 `kura list --json`、`kura scan --root PATH`。
// pre 非 nil 时其值作为默认值传入（见 Run 的前置解析）。
func newFlagSet(name string, stderr io.Writer, g *globalFlags, pre *globalFlags) *flag.FlagSet {
	fs := flag.NewFlagSet("kura "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	defJSON, defRoot, defHome := false, "", ""
	defLang := ""
	if pre != nil {
		defJSON, defRoot, defHome, defLang = pre.json, pre.root, pre.home, pre.lang
	}
	fs.BoolVar(&g.json, "json", defJSON, locales.CliFlagJson())
	fs.StringVar(&g.root, "root", defRoot, locales.CliFlagRoot())
	fs.StringVar(&g.home, "home", defHome, locales.CliFlagHome())
	fs.StringVar(&g.lang, "lang", defLang, locales.CliFlagLang())
	return fs
}

// resolveConfig 把 flag 与环境变量合成为 store.Config。
// 优先级：flag > 环境变量 > 默认值；root/home 都转为绝对路径，
// 保证 JSON 输出中的路径稳定且与工作目录无关。root 是界面里添加过的
// 目录时，使用该目录在 Library 里的私有状态位置。
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
	base, err := store.NewConfig(def.Root, home)
	if err != nil {
		return nil, err
	}
	// 界面里追加的根目录有各自的 disabled/trash/snapshots（Library 决定），
	// 命令行用 --root 指向它时必须落到同一处。config.json 损坏不影响命令行：
	// LoadLibrary 仍返回只含默认根目录的列表，此时沿用原有布局。
	lib, _ := store.LoadLibrary(base)
	return lib.Config(root), nil
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
// 全文按当前界面语言取（见 locales 的 cliUsageText）。
func printUsage(w io.Writer) {
	fmt.Fprint(w, locales.CliUsageText())
}
