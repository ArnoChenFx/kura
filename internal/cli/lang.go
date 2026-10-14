package cli

// lang.go — CLI 的界面语言解析与设置。
//
// 命令行没有窗口，语言按下面的优先级定一次，之后所有文案（包括 --help 全文、
// 用法错误、JSON 里的 error 字符串）都按它取：
//
//	--lang flag > KURA_LANG 环境变量 > config.json 的 lang > 跟随系统 locale
//
// 这里的解析必须在**任何输出之前**发生（见 applyLanguage），否则第一条错误
// 消息就会用错语言：子命令自己的 FlagSet 要等解析完才拿到 --lang 的值，那时
// flag 包的报错已经写进 stderr 了，所以 --lang/--home 的值直接从完整参数里
// 预扫（scanFlagValue），不依赖 flag 包。

import (
	"io"
	"strings"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/internal/store"
)

// langEnvVar 是与 --lang 等价的环境变量名。
const langEnvVar = "KURA_LANG"

// langSource 说明语言偏好来自哪一级（错误信息与测试断言用）。
type langSource string

const (
	// langFromFlag 来自 --lang。
	langFromFlag langSource = "flag"
	// langFromEnv 来自 KURA_LANG。
	langFromEnv langSource = "env"
	// langFromConfig 来自 config.json 的 lang。
	langFromConfig langSource = "config"
	// langFromSystem 谁都没给：跟随系统 locale。
	langFromSystem langSource = "system"
)

// langPref 按优先级解析语言偏好（见文件头）。args 是完整命令行参数，getenv
// 取环境变量（注入以便测试），configLang 是 config.json 里的 lang 字段。
//
// 返回值：pref 是偏好（可能是 i18n.LangSystem，留给调用方用系统 locale 解成
// 具体语言）；src 是它的来源；invalid 是**写坏了的那一级**的原始值（没有时
// 为空串）。
//
// 写坏的值**不猜**：--lang 或 KURA_LANG 里的值不在 system/zh/en 之列时，那一级
// 直接作废并继续往下一级找，同时把原值交给调用方报用法错误。config.json 里的
// 未知值不算写坏（与窗口侧一致，按跟随系统处理，见 store.LangPref）。
func langPref(args []string, getenv func(string) string, configLang string) (pref i18n.Lang, src langSource, invalid string) {
	levels := []struct {
		raw string
		src langSource
	}{
		{strings.TrimSpace(scanFlagValue(args, "lang")), langFromFlag},
		{strings.TrimSpace(getenv(langEnvVar)), langFromEnv},
	}
	for _, lv := range levels {
		if lv.raw == "" {
			continue // 这一级没给，往下找。
		}
		if l, ok := i18n.Normalize(lv.raw); ok {
			return l, lv.src, invalid
		}
		if invalid == "" {
			invalid = lv.raw
		}
	}
	if l := store.LangPref(configLang); l != i18n.LangSystem {
		return l, langFromConfig, invalid
	}
	return i18n.LangSystem, langFromSystem, invalid
}

// applyLanguage 在任何输出之前把界面语言定下来，并在 --lang/KURA_LANG 的值
// 写坏时按用法错误处理（返回 ExitUsage）。报错本身用「跳过写坏的那一级」之后
// 解析出的语言，保证用户看得懂。
func applyLanguage(args []string, getenv func(string) string, stderr io.Writer) int {
	pref, _, invalid := langPref(args, getenv, configLang(langConfigHome(args, getenv)))
	// 先把语言定下来：即使有写坏的一级，下面的报错也要用下一级的语言。
	i18n.Set(i18n.Resolve(pref, i18n.SystemLocale()))
	if invalid != "" {
		_, _ = io.WriteString(stderr, i18n.Tf("cli.err.badLang", invalid))
		return ExitUsage
	}
	return ExitOK
}

// langConfigHome 返回 config.json 所在的数据目录，口径与 resolveConfig 一致：
// --home flag > KURA_HOME > store.DefaultConfig()（后者取不到时返回空串，
// 调用方会跳过配置文件这一级）。
func langConfigHome(args []string, getenv func(string) string) string {
	if v := strings.TrimSpace(scanFlagValue(args, "home")); v != "" {
		return v
	}
	if v := strings.TrimSpace(getenv("KURA_HOME")); v != "" {
		return v
	}
	if def, err := store.DefaultConfig(); err == nil {
		return def.Home
	}
	return ""
}

// configLang 读取数据目录下 config.json 的 lang 字段。home 为空、文件不存在
// 或 JSON 损坏都返回空串（命令行不因为配置文件损坏而失败，与 resolveConfig
// 的容错口径一致）。LoadHomeConfig 只用 Config 的 Home 字段。
func configLang(home string) string {
	if home == "" {
		return ""
	}
	hc, _, err := store.LoadHomeConfig(&store.Config{Home: home})
	if err != nil {
		return ""
	}
	return hc.Lang
}

// scanFlagValue 从完整参数里预扫名为 name 的 flag 的值，接受
// `--name v`、`--name=v`、`-name v`、`-name=v` 四种写法，只取第一个出现处。
// 没给（或 flag 后面没有值）时返回空串。
func scanFlagValue(args []string, name string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		for _, dash := range []string{"--", "-"} {
			p := dash + name
			if a == p {
				if i+1 < len(args) {
					return args[i+1]
				}
				return ""
			}
			if v, ok := strings.CutPrefix(a, p+"="); ok {
				return v
			}
		}
	}
	return ""
}
