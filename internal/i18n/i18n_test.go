// i18n 自己的测试：语言解析（这是「第一次打开跟随系统」的判定核心）、
// 进程语言口径，以及「非测试代码里不留硬编码中文」的守卫。
//
// 文案本身不在这里测：它们住在 locales/（ARB），由 `go tool mygo generate`
// 校验（key、占位符、复数形式），取值由 locales 包自己的测试覆盖。

package i18n

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArnoChenFx/kura/locales"
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

// TestSetAndCurrent 验证 Set 只接受 zh/en，其它值落到默认（英文），并且
// locales 取值真的跟着进程语言变。
func TestSetAndCurrent(t *testing.T) {
	defer Set(Default)
	if got := Set(ZH); got != ZH || Current() != ZH {
		t.Fatalf("Set(zh) 后 Current() = %q", Current())
	}
	if got := locales.StoreErrUnknownSkill(); !strings.Contains(got, "未找到该技能") {
		t.Errorf("Set(zh) 后文案应为中文，得到 %q", got)
	}
	if got := Set(EN); got != EN || Current() != EN {
		t.Fatalf("Set(en) 后 Current() = %q", Current())
	}
	if got := locales.StoreErrUnknownSkill(); strings.Contains(got, "未找到该技能") {
		t.Errorf("Set(en) 后文案应为英文，得到 %q", got)
	}
	if got := Set("de"); got != Default || Current() != Default {
		t.Fatalf("Set(de) 应落到默认 %q，得到 %q", Default, got)
	}
}

// TestErrorIs 验证目录错误的身份：同一个 key 的错误彼此相等，errors.Is 可用，
// 与语言无关。
func TestErrorIs(t *testing.T) {
	defer Set(Default)
	sentinel := NewError("storeErrUnknownSkill", func() string { return locales.StoreErrUnknownSkill() })
	same := NewError("storeErrUnknownSkill", func() string { return locales.StoreErrUnknownSkill() })
	if !errors.Is(sentinel, same) {
		t.Error("同一个 key 的错误应彼此相等")
	}
	if errors.Is(sentinel, NewError("storeErrInvalidName", func() string { return locales.StoreErrInvalidName() })) {
		t.Error("不同 key 的错误不应相等")
	}
	// 用 sentinel 造的错误：文字来自目录、身份是 base。
	wrapped := WithSentinel(sentinel, "storeErrUnknownSkillInTrash",
		func() string { return locales.StoreErrUnknownSkillInTrash("ghost") })
	if !errors.Is(wrapped, sentinel) {
		t.Error("WithSentinel 应保留哨兵身份")
	}
	if got := wrapped.Error(); !strings.Contains(got, "ghost") {
		t.Errorf("WithSentinel 的文字应含参数，得到 %q", got)
	}
	// Wrapf 包住的底层错误仍可判定。
	base := NewError("storeErrUnknownSkill", func() string { return locales.StoreErrUnknownSkill() })
	outer := Wrapf(base, "storeErrInstallSkill", func() string { return locales.StoreErrInstallSkill("pdf-tools") })
	if !errors.Is(outer, base) {
		t.Error("Wrapf 应保留底层错误的身份")
	}
	if got, want := outer.Error(), locales.StoreErrInstallSkill("pdf-tools")+": "+locales.StoreErrUnknownSkill(); got != want {
		t.Errorf("Wrapf 文案 = %q，想要 %q", got, want)
	}
}

// TestErrorTextFollowsLanguage 验证同一条错误的文字随语言变化，而身份不变。
func TestErrorTextFollowsLanguage(t *testing.T) {
	defer Set(Default)
	e := NewError("storeErrInstallSkill", func() string { return locales.StoreErrInstallSkill("pdf-tools") })
	Set(ZH)
	zh := e.Error()
	if !strings.Contains(zh, "安装 pdf-tools 失败") {
		t.Fatalf("中文 = %q", zh)
	}
	Set(EN)
	en := e.Error()
	if strings.Contains(en, "安装") || en == zh {
		t.Errorf("英文 = %q（应与中文不同且不含中文）", en)
	}
	// 身份与语言无关。
	if !errors.Is(e, NewError("storeErrInstallSkill", func() string { return locales.StoreErrInstallSkill("x") })) {
		t.Error("换语言后同一 key 的错误身份应不变")
	}
}

// TestNoHardcodedCJK 是这个仓库的守卫：非测试的 Go 文件里不许有中文界面
// 字面量，文案一律走 locales/。漏掉的 key 会在界面上显示成 key 本身，
// 这层防线是为了让「忘了登记」在 CI 里就失败。
func TestNoHardcodedCJK(t *testing.T) {
	root := repoRoot(t)
	// 迁移范围：应用自身的包（internal）。tools/ 是开发者脚本，不是用户
	// 界面，留着中文没问题；locales/ 是文案本身，不在扫描范围。
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
		t.Errorf("%s 有硬编码中文界面文案；请移进 locales 并用 locales.Xxx() 取用",
			strings.TrimPrefix(filepath.ToSlash(f), filepath.ToSlash(root)+"/"))
	}
}

// allowedFiles 是允许出现中文的文件名（i18n.CJKLiterals 按文件名匹配）。
// 迁移后没有任何界面包需要它：文案全在 locales/，各包的 catalog.go 已删除。
// 留这个函数是为了让守卫的调用形状固定，将来真要放行某个文件时只在这里加。
func allowedFiles() []string { return nil }

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

// TestCJKLiteralsFindsFilesInTheGivenDir 是守卫自身的自测：CJKLiterals 必须
// 真的扫到 dir 里的文件，尤其调用方传 "."（相对路径）时。
//
// 这条测试是因为踩过坑：WalkDir 的第一个条目就是根目录自己，旧实现看到名字以
// "." 开头就 SkipDir，于是 CJKLiterals(".") 一个文件都不扫、静默返回空——
// internal/ui 的守卫测试因此一直是空转（把中文探针塞进 view.go 也照样 PASS）。
func TestCJKLiteralsFindsFilesInTheGivenDir(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(filepath.Join(sub, ".hidden"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		filepath.Join(dir, "probe.go"):           "package probe\n\nvar s = \"硬编码中文\"\n",
		filepath.Join(sub, "nested.go"):          "package probe\n\nvar s = \"嵌套中文\"\n",
		filepath.Join(dir, "probe_test.go"):      "package probe\n\nvar s = \"测试中文\"\n",
		filepath.Join(dir, "comment.go"):         "package probe\n\n// 只有中文注释\nvar s = \"plain\"\n",
		filepath.Join(sub, ".hidden", "skip.go"): "package probe\n\nvar s = \"点目录中文\"\n",
	}
	for p, body := range files {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// 绝对路径：probe.go 与 sub/nested.go 各一条，测试文件/注释/点目录都不算。
	got, err := CJKLiterals(dir)
	if err != nil {
		t.Fatalf("CJKLiterals(%q): %v", dir, err)
	}
	checkCJKFindings(t, dir, got)

	// 相对路径 "."：这条正是 internal/ui 守卫的用法（旧实现在这里返回空）。
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("恢复工作目录: %v", err)
		}
	}()
	got, err = CJKLiterals(".")
	if err != nil {
		t.Fatalf(`CJKLiterals("."): %v`, err)
	}
	if len(got) == 0 {
		t.Fatal(`CJKLiterals(".") 一条都没找到：调用方传 "." 时扫描退化成了空转`)
	}
	for _, f := range got {
		if !strings.HasPrefix(f, "probe.go:") && !strings.HasPrefix(f, "sub/") &&
			!strings.HasPrefix(f, "sub\\") {
			t.Errorf(`CJKLiterals(".") 的结果路径不以 "." 为基点：%s`, f)
		}
	}
	if len(got) != 2 {
		t.Errorf(`CJKLiterals(".") 找到 %d 条，想要 2 条：%v`, len(got), got)
	}
}

// checkCJKFindings 断言扫描结果正好是期望的那两条（probe.go 与 sub/nested.go）。
func checkCJKFindings(t *testing.T, dir string, got []string) {
	t.Helper()
	if len(got) != 2 {
		t.Fatalf("CJKLiterals(%q) 找到 %d 条，想要 2 条：%v", dir, len(got), got)
	}
	for _, f := range got {
		switch {
		case strings.Contains(f, "_test.go"):
			t.Errorf("测试文件不该被扫：%s", f)
		case strings.Contains(f, ".hidden"):
			t.Errorf("点开头目录不该被扫：%s", f)
		case strings.Contains(f, "comment.go"):
			t.Errorf("只有注释的文件不该报：%s", f)
		}
	}
}
