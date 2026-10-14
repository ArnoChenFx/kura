package cli

// catalog_test.go — cli 目录（中文/英文两套）的自检、界面语言解析（--lang /
// KURA_LANG / config.json / 系统 locale）的表驱动测试，以及「输出随语言变、
// 退出码不随语言变」的端到端验证。
//
// 语言是进程级的：Run 每次都会按优先级重新解析并 i18n.Set。既有测试比对的是
// 中文输出（「用法」「3 个技能」「已是最新（上游 commit 未变）」等），所以
// TestMain 给整个测试进程设 KURA_LANG=zh——它优先于 config.json 与系统
// locale，因此不随开发机或 CI 的 locale 变化；英文路径由本文件的测试用
// --lang en 覆盖（flag 优先于环境变量）。改用例时不要忘了这一点。

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/internal/store"
)

// TestMain 固定中文（见文件头）；同时把 i18n 的当前语言也设成中文，供直接
// 调 i18n.T 的断言使用。
func TestMain(m *testing.M) {
	os.Setenv(langEnvVar, string(i18n.ZH))
	i18n.Set(i18n.ZH)
	os.Exit(m.Run())
}

// TestCatalogPair 验证两套目录：key 集合一致、取值非空、登记有效、占位符
// 顺序相同（helper 与 internal/store/catalog_test.go 的同名实现一致；
// 两个包各自的测试互不可见，所以各留一份）。
func TestCatalogPair(t *testing.T) {
	checkCatalogPair(t, "cli", catalogZH, catalogEN)
}

// TestCatalogChineseTextIsVerbatim 钉住几条改造前就有的中文文案：--help 的
// 抬头、用法错误与最新提示都是用户直接看到的文字，改动必须是有意的。
func TestCatalogChineseTextIsVerbatim(t *testing.T) {
	pinned := map[string]string{
		"cli.err.unknownCommand": "kura: 未知命令 %q\n\n",
		"cli.err.badFlag":        "kura: 参数错误: %v（用 kura --help 查看用法）\n",
		"cli.check.upToDate":     "%s: 已是最新%s\n",
		"cli.doctor.allGood":     "一切正常（没有发现问题）",
		"cli.status.added":       "新增",
	}
	for key, want := range pinned {
		if got := i18n.Lookup(i18n.ZH, key); got != want {
			t.Errorf("%s 的中文取值被改过：\n现在 %q\n原有 %q", key, got, want)
		}
	}
	if text := i18n.Lookup(i18n.ZH, "cli.usage.text"); !strings.Contains(text, "用法:") {
		t.Errorf("--help 全文里应有「用法:」")
	}
}

// TestLangPref 表驱动验证语言解析的优先级：
// --lang flag > KURA_LANG 环境变量 > config.json 的 lang > 跟随系统。
// 写坏的 flag/环境变量值作废并继续往下一级找，同时把原值报给调用方。
func TestLangPref(t *testing.T) {
	env := func(kv map[string]string) func(string) string {
		return func(k string) string { return kv[k] }
	}
	cases := []struct {
		name       string
		args       []string
		env        map[string]string
		configLang string
		wantPref   i18n.Lang
		wantSrc    langSource
		wantBad    string
	}{
		{
			name: "都没有：跟随系统", args: []string{"list"},
			wantPref: i18n.LangSystem, wantSrc: langFromSystem,
		},
		{
			name: "flag zh", args: []string{"--lang", "zh", "list"},
			wantPref: i18n.ZH, wantSrc: langFromFlag,
		},
		{
			name: "flag en（= 形式）", args: []string{"--lang=en"},
			wantPref: i18n.EN, wantSrc: langFromFlag,
		},
		{
			name: "flag 写在子命令之后", args: []string{"list", "--lang", "en"},
			wantPref: i18n.EN, wantSrc: langFromFlag,
		},
		{
			name: "flag 覆盖环境变量与配置", args: []string{"--lang", "en"},
			env: map[string]string{langEnvVar: "zh"}, configLang: "zh",
			wantPref: i18n.EN, wantSrc: langFromFlag,
		},
		{
			name: "flag system 就是跟随系统（不再看环境变量与配置）",
			args: []string{"--lang", "system"}, env: map[string]string{langEnvVar: "zh"}, configLang: "zh",
			wantPref: i18n.LangSystem, wantSrc: langFromFlag,
		},
		{
			name: "环境变量 zh",
			args: []string{"list"}, env: map[string]string{langEnvVar: "zh"},
			wantPref: i18n.ZH, wantSrc: langFromEnv,
		},
		{
			name: "环境变量覆盖配置",
			args: []string{"list"}, env: map[string]string{langEnvVar: "en"}, configLang: "zh",
			wantPref: i18n.EN, wantSrc: langFromEnv,
		},
		{
			name: "配置 zh",
			args: []string{"list"}, configLang: "zh",
			wantPref: i18n.ZH, wantSrc: langFromConfig,
		},
		{
			name: "配置 system 等于没配", args: []string{"list"}, configLang: "system",
			wantPref: i18n.LangSystem, wantSrc: langFromSystem,
		},
		{
			name: "环境变量为空串视为没给", args: []string{"list"},
			env: map[string]string{langEnvVar: "  "}, configLang: "zh",
			wantPref: i18n.ZH, wantSrc: langFromConfig,
		},
		{
			name: "flag 值为空视为没给", args: []string{"--lang="},
			env:      map[string]string{langEnvVar: "en"},
			wantPref: i18n.EN, wantSrc: langFromEnv,
		},
		{
			name: "写坏的 flag 作废，落到环境变量",
			args: []string{"--lang", "jp"}, env: map[string]string{langEnvVar: "zh"},
			wantPref: i18n.ZH, wantSrc: langFromEnv, wantBad: "jp",
		},
		{
			name: "写坏的环境变量作废，落到配置",
			args: []string{"list"}, env: map[string]string{langEnvVar: "chinese"}, configLang: "en",
			wantPref: i18n.EN, wantSrc: langFromConfig, wantBad: "chinese",
		},
		{
			name: "写坏的 flag 与环境变量都作废：跟随系统",
			args: []string{"--lang", "jp"}, env: map[string]string{langEnvVar: "jp"},
			wantPref: i18n.LangSystem, wantSrc: langFromSystem, wantBad: "jp",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pref, src, bad := langPref(c.args, env(c.env), c.configLang)
			if pref != c.wantPref || src != c.wantSrc || bad != c.wantBad {
				t.Errorf("langPref(%v, %v, %q) = (%q, %q, %q)，想要 (%q, %q, %q)",
					c.args, c.env, c.configLang, pref, src, bad, c.wantPref, c.wantSrc, c.wantBad)
			}
		})
	}
}

// TestLangPrefReadsConfigFile 验证 config.json 的 lang 真被读进来（--home 与
// KURA_HOME 决定路径），以及损坏的配置文件不影响语言解析。
func TestLangPrefReadsConfigFile(t *testing.T) {
	home := t.TempDir()
	writeConfig(t, home, `{"lang":"en"}`)
	// 这一条专测「配置文件」这一级，所以把环境变量清掉（空串视为没给）。
	t.Setenv(langEnvVar, "")

	// 命令行给了 --home：走它，不看 KURA_HOME。
	t.Setenv("KURA_HOME", filepath.Join(home, "elsewhere"))
	args := []string{"list", "--home", home}
	pref, src, bad := langPref(args, os.Getenv, configLang(langConfigHome(args, os.Getenv)))
	if pref != i18n.EN || src != langFromConfig || bad != "" {
		t.Errorf("--home 指定的 config.json 应给出 en/config，得到 (%q, %q, %q)", pref, src, bad)
	}

	// 没有 --home 时走 KURA_HOME。
	t.Setenv("KURA_HOME", home)
	pref, src, _ = langPref([]string{"list"}, os.Getenv, configLang(langConfigHome([]string{"list"}, os.Getenv)))
	if pref != i18n.EN || src != langFromConfig {
		t.Errorf("KURA_HOME 指定的 config.json 应给出 en/config，得到 (%q, %q)", pref, src)
	}

	// config.json 损坏：不算错误，按跟随系统处理（与 resolveConfig 的容错一致）。
	writeConfig(t, home, "{ 不是 JSON")
	if got := configLang(home); got != "" {
		t.Errorf("损坏的 config.json 应返回空语言偏好，得到 %q", got)
	}
}

// TestCLIOutputFollowsLanguage 端到端：同一条命令在中文与英文下输出各自的
// 文案，英文输出里没有中文字符；--help 全文也跟着切。
func TestCLIOutputFollowsLanguage(t *testing.T) {
	e := newEnv(t)

	// 中文：既有文案逐字不变。
	if code := e.run("disable", "ghost"); code != ExitError {
		t.Fatalf("disable ghost 应退出 1，得到 %d，stderr=%q", code, e.stderr.String())
	}
	zhErr := e.stderr.String()
	if zhErr != "kura: disable 失败: store: 未找到该技能: ghost\n" {
		t.Errorf("中文错误输出 = %q", zhErr)
	}

	// 英文：同一命令换成英文文案（--lang 优先于 TestMain 设的 KURA_LANG=zh）。
	e.run("--lang", "en", "disable", "ghost")
	enErr := e.stderr.String()
	if hasCJKText(enErr) {
		t.Errorf("英文错误输出里不该有中文：%q", enErr)
	}
	if enErr != "kura: disable failed: store: skill not found: ghost\n" {
		t.Errorf("英文错误输出 = %q", enErr)
	}

	// --help：中文含「用法:」，英文含 "Usage:" 且没有中文。
	e.run("--help")
	if got := e.stdout.String(); !strings.Contains(got, "用法:") {
		t.Errorf("中文 --help 应含「用法:」，得到 %q", got)
	}
	e.run("--lang", "en", "--help")
	enHelp := e.stdout.String()
	if !strings.Contains(enHelp, "Usage:") || hasCJKText(enHelp) {
		t.Errorf("英文 --help 应含 \"Usage:\" 且不含中文，得到 %q", enHelp)
	}

	// list：中文（没有技能）与英文 (no skills)。
	e.run("list")
	if got := e.stdout.String(); !strings.Contains(got, "（没有技能）") {
		t.Errorf("中文 list 应含「（没有技能）」，得到 %q", got)
	}
	e.run("list", "--lang", "en") // flag 写在子命令之后也生效
	if got := e.stdout.String(); !strings.Contains(got, "(no skills)") || hasCJKText(got) {
		t.Errorf("英文 list 应含 \"(no skills)\" 且不含中文，得到 %q", got)
	}
}

// TestCLIJSONErrorFollowsLanguage 验证 --json 模式下错误载荷也随语言变
// （形状不变：{"error": "..."}），退出码不变。
func TestCLIJSONErrorFollowsLanguage(t *testing.T) {
	e := newEnv(t)
	if code := e.run("--json", "disable", "ghost"); code != ExitError {
		t.Fatalf("--json 下 disable ghost 应退出 1，得到 %d", code)
	}
	var zh struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(e.stderr.Bytes(), &zh); err != nil {
		t.Fatalf("stderr 不是合法 JSON：%v（%q）", err, e.stderr.String())
	}
	if zh.Error != "disable 失败: store: 未找到该技能: ghost" {
		t.Errorf("中文 JSON 错误 = %q", zh.Error)
	}

	e.run("--json", "--lang=en", "disable", "ghost")
	var en struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(e.stderr.Bytes(), &en); err != nil {
		t.Fatalf("stderr 不是合法 JSON：%v（%q）", err, e.stderr.String())
	}
	if en.Error != "disable failed: store: skill not found: ghost" || hasCJKText(en.Error) {
		t.Errorf("英文 JSON 错误 = %q", en.Error)
	}
}

// TestCLILanguageErrors 验证 --lang / KURA_LANG 写坏时报用法错误（退出码 2）：
// 报错本身用「跳过写坏的那一级」解析出的语言，并列出可选值。
func TestCLILanguageErrors(t *testing.T) {
	e := newEnv(t)
	if code := e.run("--lang", "jp", "list"); code != ExitUsage {
		t.Fatalf("--lang jp 应退出 2，得到 %d，stderr=%q", code, e.stderr.String())
	}
	if got := e.stderr.String(); !strings.Contains(got, `"jp"`) || !strings.Contains(got, "system") {
		t.Errorf("错误信息应回显写坏的值与可选值，得到 %q", got)
	}
	// 报错语言来自下一级（TestMain 设的 KURA_LANG=zh）。
	if got := e.stderr.String(); !strings.Contains(got, "无法识别的界面语言") {
		t.Errorf("中文环境下的报错应是中文，得到 %q", got)
	}

	// 报错语言来自下一级：KURA_LANG=en 时，写坏的 --lang 用英文报错。
	t.Setenv(langEnvVar, "en")
	if code := e.run("--lang", "jp", "list"); code != ExitUsage {
		t.Fatalf("--lang jp 应退出 2（环境为 en 时用英文报错），得到 %d", code)
	}
	if got := e.stderr.String(); !strings.Contains(got, "unknown interface language") {
		t.Errorf("英文环境下的报错应是英文，得到 %q", got)
	}
}

// TestExitCodesAndIdentityIgnoreLanguage 验证切语言不改行为：退出码、错误
// 身份（errors.Is）与 doctor 的 kind 都不随语言变。
func TestExitCodesAndIdentityIgnoreLanguage(t *testing.T) {
	e := newEnv(t)
	e.writeSkill("broken-meta")
	if err := os.WriteFile(filepath.Join(e.root, "broken-meta", store.MetaFileName), []byte("\x00\xff 不是 JSON"), 0o644); err != nil {
		t.Fatal(err)
	}
	// doctor 有问题：两种语言下都是退出码 3，kind 也是同一套稳定取值。
	for _, args := range [][]string{{"doctor"}, {"--lang", "en", "doctor"}} {
		if code := e.run(args...); code != ExitDoctorProblem {
			t.Fatalf("%v 应退出 3，得到 %d，stderr=%q", args, code, e.stderr.String())
		}
	}
	codeZH, outZH := e.runJSON(t, "doctor")
	codeEN, outEN := e.runJSON(t, "--lang=en", "doctor")
	if codeZH != codeEN {
		t.Errorf("doctor 退出码不该随语言变：%d vs %d", codeZH, codeEN)
	}
	if !strings.Contains(fmt.Sprint(outZH["problems"]), "meta-parse") ||
		!strings.Contains(fmt.Sprint(outEN["problems"]), "meta-parse") {
		t.Errorf("problem.kind 是稳定契约，不该随语言变：%v / %v", outZH["problems"], outEN["problems"])
	}

	// 错误身份与语言无关：英文下哨兵与包住的错误照样能判定。
	i18n.Set(i18n.EN)
	defer i18n.Set(i18n.ZH)
	if !errors.Is(fmt.Errorf("%w: %s", store.ErrUnknownSkill, "ghost"), store.ErrUnknownSkill) {
		t.Error("英文下 errors.Is 仍应成立（身份不受语言影响）")
	}
	notInTrash := store.ErrUnknownSkill
	if hasCJKText(notInTrash.Error()) {
		t.Errorf("英文下 store 哨兵的文案不该含中文：%q", notInTrash.Error())
	}
}

// writeConfig 写一份 config.json（用于语言偏好的读取测试）。
func writeConfig(t *testing.T, home, content string) {
	t.Helper()
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, store.ConfigFileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// hasCJKText 报告字符串里是否有中日韩文字（口径同 internal/store 的测试）。
func hasCJKText(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) ||
			(r >= 0xff01 && r <= 0xffe6) {
			return true
		}
	}
	return false
}

// checkCatalogPair 是两套目录的自检（与 internal/store/catalog_test.go 的同名
// 实现一致）：key 集合一致、取值非空、登记有效、占位符顺序相同。
func checkCatalogPair(t *testing.T, pkg string, zh, en map[string]string) {
	t.Helper()
	for k, zv := range zh {
		ev, ok := en[k]
		if !ok {
			t.Errorf("%s: %s 只有中文", pkg, k)
			continue
		}
		if zv == "" || ev == "" {
			t.Errorf("%s: %s 有空取值（zh=%q en=%q）", pkg, k, zv, ev)
		}
		if got := i18n.Lookup(i18n.ZH, k); got != zv {
			t.Errorf("%s: %s 的中文取值没登记：Lookup = %q", pkg, k, got)
		}
		if got := i18n.Lookup(i18n.EN, k); got != ev {
			t.Errorf("%s: %s 的英文取值没登记：Lookup = %q", pkg, k, got)
		}
		if a, b := verbsOf(zv), verbsOf(ev); a != b {
			t.Errorf("%s: %s 的占位符顺序不一致：中文 %q，英文 %q", pkg, k, a, b)
		}
	}
	for k := range en {
		if _, ok := zh[k]; !ok {
			t.Errorf("%s: %s 只有英文", pkg, k)
		}
	}
}

// verbsOf 按出现顺序取出格式串里的动词（%s/%d/%q/%v…；%% 不算）。
func verbsOf(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			continue
		}
		j := i + 1
		for j < len(s) && strings.ContainsRune("-+ #0.123456789", rune(s[j])) {
			j++
		}
		if j >= len(s) {
			break
		}
		if s[j] == '%' {
			i = j
			continue
		}
		out.WriteByte(s[j])
		i = j
	}
	return out.String()
}
