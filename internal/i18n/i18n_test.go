// i18n 自己的测试：语言解析（这是「第一次打开跟随系统」的判定核心）、
// 目录完整性，以及「非测试代码里不留硬编码中文」的守卫。

package i18n

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFromLocale 验证系统 locale 到语言的映射：中文（含各种写法）是中文，
// 其它一律英文——这是「系统不是中文就用英文」的要求。
func TestFromLocale(t *testing.T) {
	zh := []string{"zh", "zh-CN", "zh_CN", "zh-Hans", "zh-Hant", "zh-TW", "zh_CN.UTF-8", "ZH-cn", " zh-Hans-CN "}
	en := []string{"", "C", "POSIX", "en", "en-US", "en_GB.UTF-8", "ja-JP", "de", "fr-FR", "ko", "ru-RU", "zhx"}
	for _, loc := range zh {
		if got := FromLocale(loc); got != ZH {
			t.Errorf("FromLocale(%q) = %q，想要 zh", loc, got)
		}
	}
	for _, loc := range en {
		if got := FromLocale(loc); got != EN {
			t.Errorf("FromLocale(%q) = %q，想要 en", loc, got)
		}
	}
}

// TestResolve 验证偏好与系统的合成：固定语言优先；跟随系统时才看 locale。
func TestResolve(t *testing.T) {
	cases := []struct {
		pref   Lang
		locale string
		want   Lang
	}{
		{LangSystem, "zh-CN", ZH},
		{LangSystem, "en-US", EN},
		{LangSystem, "", EN},
		{LangSystem, "ja-JP", EN},
		{ZH, "en-US", ZH},
		{EN, "zh-CN", EN},
	}
	for _, c := range cases {
		if got := Resolve(c.pref, c.locale); got != c.want {
			t.Errorf("Resolve(%q, %q) = %q，想要 %q", c.pref, c.locale, got, c.want)
		}
	}
}

// TestNormalize 验证配置里的 lang 值：空/"system" 是跟随系统，zh/en 及其
// 标签是固定语言，别的值不认识。
func TestNormalize(t *testing.T) {
	ok := map[string]Lang{
		"":       LangSystem,
		"system": LangSystem,
		"auto":   LangSystem,
		"zh":     ZH,
		"zh-CN":  ZH,
		"ZH":     ZH,
		"en":     EN,
		"en-US":  EN,
		" En ":   EN,
		"en_GB":  EN,
	}
	for in, want := range ok {
		got, fine := Normalize(in)
		if !fine || got != want {
			t.Errorf("Normalize(%q) = (%q, %v)，想要 (%q, true)", in, got, fine, want)
		}
	}
	for _, bad := range []string{"jp", "chinese", "english", "zhx"} {
		if _, fine := Normalize(bad); fine {
			t.Errorf("Normalize(%q) 应不认识", bad)
		}
	}
}

// TestSetAndCurrent 验证 Set 只接受 zh/en，其它值落到默认（英文）。
func TestSetAndCurrent(t *testing.T) {
	defer Set(Default)
	if got := Set(ZH); got != ZH || Current() != ZH {
		t.Fatalf("Set(zh) 后 Current() = %q", Current())
	}
	if got := Set(EN); got != EN || Current() != EN {
		t.Fatalf("Set(en) 后 Current() = %q", Current())
	}
	if got := Set("de"); got != Default || Current() != Default {
		t.Fatalf("Set(de) 应落到默认 %q，得到 %q", Default, got)
	}
}

// TestTAndTf 验证取文案与格式化，以及缺 key 时返回 key 本身。
func TestTAndTf(t *testing.T) {
	RegisterZH(map[string]string{"test.hello": "你好，%s"})
	RegisterEN(map[string]string{"test.hello": "Hello, %s"})
	defer Set(Default)

	Set(ZH)
	if got := Tf("test.hello", "世界"); got != "你好，世界" {
		t.Errorf("Tf = %q", got)
	}
	Set(EN)
	if got := Tf("test.hello", "world"); got != "Hello, world" {
		t.Errorf("Tf = %q", got)
	}
	if got := T("test.missing"); got != "test.missing" {
		t.Errorf("缺 key 应返回 key 本身，得到 %q", got)
	}
	if !Has("test.hello") {
		t.Error("Has(test.hello) 应为 true")
	}
}

// TestRegisterDuplicatePanics 验证同一个 key 登记两次会被拦住（复制粘贴
// 造成的静默覆盖比 panic 更难查）。
func TestRegisterDuplicatePanics(t *testing.T) {
	RegisterEN(map[string]string{"test.dup": "one"})
	RegisterZH(map[string]string{"test.dup": "一"})
	defer func() {
		if recover() == nil {
			t.Error("重复登记应 panic")
		}
	}()
	RegisterZH(map[string]string{"test.dup": "二"})
}

// TestCatalogParity 验证两套目录的 key 完全一致（缺失的那个方向在界面上会
// 显示成 key 本身）。
func TestCatalogParity(t *testing.T) {
	onlyZH, onlyEN := LangList()
	if len(onlyZH) > 0 {
		t.Errorf("只有中文的 key：%v", onlyZH)
	}
	if len(onlyEN) > 0 {
		t.Errorf("只有英文的 key：%v", onlyEN)
	}
}

// TestErrorIs 验证目录错误的身份：同一个 key 的错误彼此相等，errors.Is 可用。
func TestErrorIs(t *testing.T) {
	defer Set(Default)
	RegisterZH(map[string]string{
		"test.err.outer":        "外层 %s",
		"test.err.inner":        "内层出错",
		"test.err.other":        "另一个",
		"test.err.see-sentinel": "哨兵",
	})
	RegisterEN(map[string]string{
		"test.err.outer": "outer %s",
	})

	sentinel := NewError("test.err.sentinel")
	var target error = NewError("test.err.sentinel", "别的参数")
	if !errors.Is(sentinel, target) {
		t.Error("同一个 key 的错误应彼此相等")
	}
	if errors.Is(sentinel, NewError("test.err.other")) {
		t.Error("不同 key 的错误不应相等")
	}
	// 包住的底层错误仍可判定
	inner := NewError("test.err.inner")
	outer := Wrapf(inner, "test.err.outer", "x")
	if !errors.Is(outer, inner) {
		t.Error("Wrapf 应保留底层错误的身份")
	}
	Set(ZH)
	if got := outer.Error(); got != "外层 x: 内层出错" {
		t.Errorf("Wrapf 文案 = %q", got)
	}
	Set(EN)
	if got := outer.Error(); got != "outer x: test.err.inner" {
		t.Errorf("Wrapf 英文文案 = %q（内层没登记英文时显示 key，这是设计）", got)
	}
}

// TestErrorTextFollowsLanguage 验证同一条错误的文字随语言变化，而身份不变。
func TestErrorTextFollowsLanguage(t *testing.T) {
	RegisterZH(map[string]string{"test.err.text": "中文 %d"})
	RegisterEN(map[string]string{"test.err.text": "English %d"})
	e := NewError("test.err.text", 7)
	defer Set(Default)
	Set(ZH)
	if got := e.Error(); got != "中文 7" {
		t.Errorf("中文 = %q", got)
	}
	Set(EN)
	if got := e.Error(); got != "English 7" {
		t.Errorf("英文 = %q", got)
	}
}

// TestNoHardcodedCJK 是这个仓库的守卫：非测试的 Go 文件里不许有中文界面
// 字面量，文案一律走目录（catalog*.go 自己除外）。漏掉的 key 会在界面上
// 显示成 key 本身，这层防线是为了让「忘了登记」在 CI 里就失败。
func TestNoHardcodedCJK(t *testing.T) {
	root := repoRoot(t)
	// 迁移范围：应用自身的包（internal）与 main.go。tools/ 是开发者脚本，
	// 不是用户界面，留着中文没问题。
	found, err := CJKLiterals(filepath.Join(root, "internal"), allowedFiles()...)
	if err != nil {
		t.Fatal(err)
	}
	allowed := allowedPaths(root)
	for _, f := range found {
		// CJKLiterals 的每条结果是「绝对路径:行号: 字面量」（路径里可能有
		// 盘符的 ':'），所以只能按前缀判允许，不能按 ':' 切路径。
		if hasAllowedPrefix(f, allowed) {
			continue
		}
		// 报错时把绝对路径缩成仓库相对路径，消息好读。
		t.Errorf("%s 有硬编码中文界面文案；请移进 catalog 并用 i18n.T 取用",
			strings.TrimPrefix(filepath.ToSlash(f), filepath.ToSlash(root)+"/"))
	}
}

// allowedFiles 是允许出现中文的文件名（i18n.CJKLiterals 按文件名匹配，
// 所以这里只能给文件名）：各包的目录文件、i18n 包自己的实现与测试
// （「文本不存在」这类错误是对程序员的，不是界面文案）。
//
// 加进这个清单的每个名字都要想清楚：它会放行**所有包**里的同名文件。
// 要精确到某一个文件就用 allowedPaths。不要为了让它过而放宽别的判断
// （例如整目录排除）。
func allowedFiles() []string {
	return []string{
		"catalog.go", "catalog_zh.go", "catalog_en.go",
		"i18n.go", "i18n_test.go", "locale_windows.go", "locale_other.go",
	}
}

// allowedPaths 是按完整路径（仓库相对、/ 分隔）允许含中文的文件，返回带
// 结尾 ':' 的前缀（与 CJKLiterals 结果里的「路径:行号」对齐），给「文件名
// 不够精确」的场合用：
//
//   - internal/store/translate.go：systemPrompt（以及拼在它后面的「目标
//     语言：」）是发给**模型**的指令，不是界面文案：它必须稳定、可测试，
//     不能随界面语言变，所以有意留在代码里（见该文件的注释）。
//     这里写路径而不是把 "translate.go" 加进 allowedFiles：internal/ui/
//     translate.go 是普通界面代码，那里的中文仍要被守卫拦下。
func allowedPaths(root string) []string {
	rel := []string{"internal/store/translate.go"}
	out := make([]string, 0, len(rel))
	for _, p := range rel {
		out = append(out, filepath.ToSlash(filepath.Join(root, filepath.FromSlash(p)))+":")
	}
	return out
}

// hasAllowedPrefix 报告一条 CJKLiterals 结果是否落在允许列表里的某个文件。
func hasAllowedPrefix(finding string, allowed []string) bool {
	for _, p := range allowed {
		if strings.HasPrefix(finding, p) {
			return true
		}
	}
	return false
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("找不到仓库根（go.mod）")
		}
		dir = parent
	}
}
