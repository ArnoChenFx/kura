// Package i18n 是 kura 的界面语言判定：把「语言偏好 + 系统 locale」解成
// 一个 BCP 47 标签，交给 mygo 官方 catalogs（locales/）取文案。
//
// 迁移说明（里程碑 i18n-official）：文案本身已经全部搬进 locales/en.json 与
// locales/zh-Hans.json（ARB + ICU，由 `go tool mygo generate` 生成类型化函数
// locales.Xxx()）。本包不再保存任何文案，只剩三件事：
//
//   - 语言解析：Normalize / FromLocale / Resolve / System，以及 config.json
//     的 lang 取值（system/zh/en）与 BCP 47（zh-Hans/en）的映射；
//   - 进程语言口径：窗口应用由 MyGo 的 mygo.App.Locale 决定（官方
//     package i18n 的 locale func 已由 MyGo 接好，locales.Xxx() 每帧现取）；
//     CLI 没有 MyGo 应用，用 Set 换掉 locale func，测试同理；
//   - 错误层：见 error.go（身份稳定、文字随语言现取）。
//
// 规则（仍是全仓库约定）：非测试 Go 代码里不写用户可见的文案，界面字符串
// 一律进 locales/；注释用中文（见 AGENTS.md）。
package i18n

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	myi18n "github.com/egoist/mygo/i18n"
)

// Lang 是语言的取值口径：BCP 47 标签。它与 config.json 里 lang 字段的
// 存储值（system/zh/en）不同，映射见 store.NormalizeLang / store.LangPref。
type Lang string

const (
	// ZH 是简体中文，对应 locales/zh-Hans.json。
	ZH Lang = "zh-Hans"
	// EN 是英文，也是 catalogs 的 source 语言（locales/en.json）。
	EN Lang = "en"
	// LangSystem 是「跟随系统」的标记：它不是一种语言，Resolve 会把它解成
	// 具体的一种。
	LangSystem Lang = "system"
)

// Default 是没有其它信息时的语言。系统 locale 认不出来（空、C、POSIX）时
// 也用它：非中文系统一律英文。
const Default = EN

// Set 把进程的语言取值口径设成 l（CLI 与测试用；窗口应用不要调它）。
//
// 实现是替换 mygo 官方 package i18n 的 locale func：之后所有 locales.Xxx()
// 与错误文案都按 l 取。窗口应用不需要它——MyGo 已经把自己的 App.Locale 接在
// 那里，用户切语言走 mygo.App.SetLocale。
//
// 未知语言按 Default 处理，与旧实现一致。
func Set(l Lang) Lang {
	if l != ZH {
		l = Default
	}
	myi18n.SetLocaleFunc(func() string { return string(l) })
	return l
}

// Current 返回当前语言（一定是 ZH 或 EN，不会是 LangSystem）。
//
// 取值来自 mygo 官方的 i18n.Locale()：窗口应用里它是 MyGo 解析出的 App
// 语言，CLI 与测试里是 Set 换上的那个。认不出来时返回 Default（英文）。
func Current() Lang {
	if l, ok := Normalize(myi18n.Locale()); ok && l != LangSystem {
		return l
	}
	return Default
}

// Normalize 把配置里的语言值规范成 LangSystem、ZH 或 EN 之一：
// 空串与 "system" 是跟随系统；"zh"、"en"（大小写不敏感、允许 zh-CN 这类
// 标签）是固定语言。其它值返回 false。
func Normalize(v string) (Lang, bool) {
	s := strings.ToLower(strings.TrimSpace(v))
	switch {
	case s == "", s == "system", s == "auto":
		return LangSystem, true
	case s == "zh", strings.HasPrefix(s, "zh-"), strings.HasPrefix(s, "zh_"):
		return ZH, true
	case s == "en", strings.HasPrefix(s, "en-"), strings.HasPrefix(s, "en_"):
		return EN, true
	}
	return "", false
}

// FromLocale 返回 locale 对应的语言：主语言是 zh 的（zh、zh-CN、zh_TW、
// zh-Hant-TW…）算中文，其余（含空串、"C"、"POSIX"）算英文。
func FromLocale(locale string) Lang {
	s := strings.ToLower(strings.TrimSpace(locale))
	if i := strings.IndexAny(s, ".@"); i >= 0 {
		s = s[:i]
	}
	s = strings.ReplaceAll(s, "_", "-")
	if s == "zh" || strings.HasPrefix(s, "zh-") {
		return ZH
	}
	return EN
}

// Resolve 把「用户偏好」解成具体语言：偏好是 system 时用 locale（系统
// locale，如 "zh-CN"、"en-US"）判断——中文（含 zh_CN、zh-Hant）用中文，
// 其余一律英文。
func Resolve(pref Lang, locale string) Lang {
	if pref == ZH || pref == EN {
		return pref
	}
	if FromLocale(locale) == ZH {
		return ZH
	}
	return EN
}

// System 返回操作系统的界面语言（见 systemLocale）。
func System() Lang { return FromLocale(systemLocale()) }

// SystemLocale 返回系统 locale 原文（如 "zh-CN"；认不出时空串）。
func SystemLocale() string { return systemLocale() }

// Ptr 返回值的指针，便于在结构体里表达「没设置」。
func Ptr(l Lang) *Lang { return &l }

// CJKLiterals 扫描 dir 下的 Go 文件，返回含中日韩文字的用户可见字符串
// 字面量（文件:行: 文本）。注释不算（中文注释是仓库约定）。
//
// 给守卫测试用：除 locales/ 与少数有意留在代码里的文件外，用户可见的包
// 不该有硬编码中文（见 internal/i18n/i18n_test.go 的 TestNoHardcodedCJK）。
// want 里给出允许的文件名（不含路径）。
func CJKLiterals(dir string, allow ...string) ([]string, error) {
	ok := map[string]bool{}
	for _, a := range allow {
		ok[a] = true
	}
	var out []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// testdata 与点开头的目录（.git、.worktrees…）整棵跳过。
			//
			// path != dir 这个条件是必需的：WalkDir 的第一个条目就是根目录
			// 自己，调用方传 "." 时它的 Name() 正是 "."，无脑跳过会让整次
			// 扫描一个文件都不看、静默通过——调用方给的目录一定是要扫的。
			if name := d.Name(); path != dir && (name == "testdata" || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || ok[name] {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return nil // 解析不了的文件（不该发生）跳过，不打断检查
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, isLit := n.(*ast.BasicLit)
			if !isLit || lit.Kind != token.STRING {
				return true
			}
			if hasCJK(lit.Value) {
				out = append(out, fmt.Sprintf("%s:%d: %s",
					filepath.ToSlash(path), fset.Position(lit.Pos()).Line, lit.Value))
			}
			return true
		})
		return nil
	})
	sort.Strings(out)
	return out, err
}

func hasCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) {
			return true
		}
	}
	return false
}
