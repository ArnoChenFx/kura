// Package i18n 是 kura 的界面文案：中文与英文两套，按当前语言取用。
//
// 规则（也是全仓库的约定）：
//   - 非测试 Go 代码里**不写**用户可见的中文字面量：文案都放在各包的
//     catalog.go（map[key]string）里，用 i18n.T / i18n.Tf 取。
//   - key 形如 `<领域>.<分类>.<名字>`，全小写、点分隔、只含 ASCII，例如
//     ui.status.installFailed、store.err.skillExists、cli.usage.title。
//   - 中文取值与改造前的界面文字**逐字一致**（既有测试与用户习惯都不变）；
//     英文取值为自然、简短的英语。两种语言的 %s/%d 占位符顺序相同。
//   - 整句进目录，不做拼接：语序在中英文之间不同，拼接会译歪。
//   - 注释仍然用中文（见 AGENTS.md），不受此限。
//
// 当前语言是进程级的（Set），由 main.go / CLI 在启动时按「用户偏好 → 系统
// locale，中文用中文、其余用英文」解析后设置一次；用户在设置里切换时再设。
// 界面与后台任务可能并发读取，所以内部用读写锁保护。
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
	"sync"
	"unicode"
)

// Lang 是一种语言。取值与 config.json 里的 lang 字段一致。
type Lang string

const (
	// ZH 是中文（简体）。
	ZH Lang = "zh"
	// EN 是英文。
	EN Lang = "en"
)

// Default 是没有其它信息时的语言。系统 locale 认不出来（空、C、POSIX）时
// 也用它：非中文系统一律英文。
const Default = EN

var (
	mu  sync.RWMutex
	cur = Default
	zh  = map[string]string{}
	en  = map[string]string{}
)

// RegisterZH 登记一批中文文案。各包在自己的 init 里调用；同一个 key 登记
// 两次是编程错误（多半是复制粘贴），直接 panic，免得悄悄用错的那条。
func RegisterZH(m map[string]string) { register(zh, m, "zh") }

// RegisterEN 登记一批英文文案，语义同 RegisterZH。
func RegisterEN(m map[string]string) { register(en, m, "en") }

func register(dst, src map[string]string, lang string) {
	mu.Lock()
	defer mu.Unlock()
	for k, v := range src {
		if k == "" {
			panic("i18n: 登记了空 key（语言 " + lang + "）")
		}
		if v == "" {
			panic("i18n: key " + k + " 的 " + lang + " 文案是空的")
		}
		if _, dup := dst[k]; dup {
			panic("i18n: key " + k + " 的 " + lang + " 文案重复登记")
		}
		dst[k] = v
	}
}

// Set 设置当前语言，并返回设置后的值。未知语言按 Default 处理。
func Set(l Lang) Lang {
	if l != ZH {
		l = Default
	}
	mu.Lock()
	cur = l
	mu.Unlock()
	return l
}

// Current 返回当前语言。
func Current() Lang {
	mu.RLock()
	defer mu.RUnlock()
	return cur
}

// T 返回 key 在当前语言下的文案。key 不存在时返回 key 本身：界面上立刻能
// 看出漏了哪条，测试里也有断言可以拦（见 TestCatalogsMatch）。
func T(key string) string {
	mu.RLock()
	defer mu.RUnlock()
	return lookup(key, cur)
}

// Tf 是 T 加 fmt.Sprintf。
func Tf(key string, args ...any) string {
	return fmt.Sprintf(T(key), args...)
}

// Has 报告 key 是否已登记（两种语言都有才算）。
func Has(key string) bool {
	mu.RLock()
	defer mu.RUnlock()
	_, a := zh[key]
	_, b := en[key]
	return a && b
}

// Keys 返回全部 key（已排序），供测试与工具使用。
func Keys() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(zh))
	for k := range zh {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Lookup 按语言取值，主要给测试用（Current 会变，测试需要直接比对）。
func Lookup(l Lang, key string) string {
	mu.RLock()
	defer mu.RUnlock()
	return lookup(key, l)
}

func lookup(key string, l Lang) string {
	if l == ZH {
		if v, ok := zh[key]; ok {
			return v
		}
		return key
	}
	if v, ok := en[key]; ok {
		return v
	}
	return key
}

// LangList 返回两套 catalog 的 key 差异：只在中文里有、只在英文里有。
// 空、空表示两套一致。测试用。
func LangList() (onlyZH, onlyEN []string) {
	mu.RLock()
	defer mu.RUnlock()
	for k := range zh {
		if _, ok := en[k]; !ok {
			onlyZH = append(onlyZH, k)
		}
	}
	for k := range en {
		if _, ok := zh[k]; !ok {
			onlyEN = append(onlyEN, k)
		}
	}
	sort.Strings(onlyZH)
	sort.Strings(onlyEN)
	return onlyZH, onlyEN
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

// LangSystem 是「跟随系统」的标记：config.json 里 lang 为空或 "system" 时
// 用它。它不是一种语言，Resolve 会把它解成具体的一种。
const LangSystem Lang = "system"

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

// System 返回操作系统的界面语言（见 systemLocale）。
func System() Lang { return FromLocale(systemLocale()) }

// SystemLocale 返回系统 locale 原文（如 "zh-CN"；认不出时空串）。
func SystemLocale() string { return systemLocale() }

// Ptr 返回值的指针，便于在结构体里表达「没设置」。
func Ptr(l Lang) *Lang { return &l }

// Error 是一个文案来自目录的错误：身份稳定（同一个 key 返回的指针相等，
// errors.Is 可用），文字随当前语言变化。
type Error struct {
	key  string
	args []any
}

// NewError 造一个由 key 决定文案的错误。
func NewError(key string, args ...any) *Error { return &Error{key: key, args: args} }

// Error 实现 error。
func (e *Error) Error() string { return Tf(e.key, e.args...) }

// Key 返回这条错误对应的目录 key。
func (e *Error) Key() string { return e.key }

// Is 让同一个 key 的错误彼此相等（args 不同也算同一类错误）。
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.key == e.key
}

// Errorf 与 fmt.Errorf 同形，但格式串来自目录。返回的类型带 Is，可作
// 哨兵错误的替身（配合 errors.Is 使用）。
//
// 需要包住底层错误时用 Wrapf。
func Errorf(key string, args ...any) error { return NewError(key, args...) }

// Wrapf 在格式串末尾接上 ": " 包住 err：文案形如 "store: 安装 x 失败: <err>"。
//
// 文字到调用 Error() 时才生成（
// 错误可能在后台任务里产生、在切换语言之后才显示）。
func Wrapf(err error, key string, args ...any) error {
	return &wrapped{err: err, key: key, args: args}
}

// wrapped 是 Wrapf 的结果：身份来自被包住的错误，文字每次渲染时现取。
type wrapped struct {
	err  error
	key  string
	args []any
}

// Error 实现 error：每次调用都按当前语言取文案。
func (w *wrapped) Error() string { return Tf(w.key, w.args...) + ": " + w.err.Error() }

// Unwrap 让 errors.Is / errors.As 能穿到底层错误。
func (w *wrapped) Unwrap() error { return w.err }

// CJKLiterals 扫描 dir 下的 Go 文件，返回含中日韩文字的用户可见字符串
// 字面量（文件:行: 文本）。注释不算（中文注释是仓库约定）。
//
// 给各包的测试用：除 catalog*.go 外，用户可见的包不该有硬编码中文
// （见 TestNoHardcodedCJK）。want 里给出允许的文件名（不含路径）。
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
			if name := d.Name(); name == "testdata" || strings.HasPrefix(name, ".") {
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
