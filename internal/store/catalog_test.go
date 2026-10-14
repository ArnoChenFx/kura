package store

// catalog_test.go — store 目录（中文/英文两套）的自检，以及「文案随语言变、
// 错误身份不变」的验证。
//
// 本包既有的测试比对的是错误信息里的中文文案（如「已存在同名技能」），所以
// TestMain 先把界面语言固定成中文；本文件的测试切到英文验证完会切回来。
// 语言是进程级的（i18n.Set），任何一个测试忘了还原都会污染同包其它测试，
// 因此一律用 setLangForTest（t.Cleanup 还原）。

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode"

	"github.com/ArnoChenFx/kura/internal/i18n"
)

// TestMain 把界面语言固定成中文再跑本包测试：既有断言依赖中文文案，不该随
// 开发机或 CI 的系统 locale 变化（i18n 的默认值是英文）。英文取值由
// catalog_test.go 逐个覆盖。
func TestMain(m *testing.M) {
	i18n.Set(i18n.ZH)
	os.Exit(m.Run())
}

// setLangForTest 切换界面语言，并在测试结束时还原成切换前的值。
func setLangForTest(t *testing.T, l i18n.Lang) {
	t.Helper()
	old := i18n.Current()
	i18n.Set(l)
	t.Cleanup(func() { i18n.Set(old) })
}

// TestCatalogPair 验证两套目录：key 集合完全一致、取值非空、都真的登记进了
// i18n（取到的是取值而不是回落到 key 本身），且占位符顺序相同。
func TestCatalogPair(t *testing.T) {
	checkCatalogPair(t, "store", catalogZH, catalogEN)
}

// TestCatalogChineseTextIsVerbatim 钉住几条改造前就有的中文取值：它们是既有
// 测试与用户习惯依赖的文案，改动必须是有意的（改这里等于改用户可见文案）。
func TestCatalogChineseTextIsVerbatim(t *testing.T) {
	pinned := map[string]string{
		"store.err.unknownSkill":      "store: 未找到该技能",
		"store.err.skillExists":       "store: 根目录已存在同名技能 %s",
		"store.err.importDefaultOnly": "store: .skill-lock.json 只描述默认技能目录，导入元数据只适用于默认目录",
	}
	for key, want := range pinned {
		if got := i18n.Lookup(i18n.ZH, key); got != want {
			t.Errorf("%s 的中文取值被改过：\n现在 %q\n原有 %q", key, got, want)
		}
	}
}

// TestErrorTextFollowsLanguage 验证同一条错误的文字随语言变：英文下不含任何
// 中文字符，切回中文恢复原文（中文取值与改造前逐字一致）。
func TestErrorTextFollowsLanguage(t *testing.T) {
	zhText := ErrUnknownSkill.Error()
	if !strings.Contains(zhText, "未找到该技能") {
		t.Fatalf("中文下 ErrUnknownSkill = %q", zhText)
	}
	setLangForTest(t, i18n.EN)
	enText := ErrUnknownSkill.Error()
	if hasCJK(enText) {
		t.Errorf("英文下不该出现中文：%q", enText)
	}
	if enText == zhText {
		t.Errorf("英文下文案没变：%q", enText)
	}
	// 带占位符的错误同样跟着语言走（i18n.Tf 在渲染时取当前语言）。
	withArg := i18n.Errorf("store.err.installSkill", "pdf-tools")
	if got := withArg.Error(); got != "store: failed to install pdf-tools" {
		t.Errorf("英文下 installSkill = %q", got)
	}
	setLangForTest(t, i18n.ZH)
	if got := withArg.Error(); got != "store: 安装 pdf-tools 失败" {
		t.Errorf("切回中文后 installSkill = %q", got)
	}
}

// TestErrorIdentityIsLanguageIndependent 验证语言不影响错误身份：英文下
// errors.Is 照样成立，包括 fmt.Errorf 包住的哨兵与 sentinelErr 造的整句错误。
func TestErrorIdentityIsLanguageIndependent(t *testing.T) {
	setLangForTest(t, i18n.EN)

	// fmt.Errorf("%w: %s", …) 保留哨兵身份（library.go / cli_support.go 的写法）。
	wrapped := fmt.Errorf("%w: %s", ErrUnknownSkill, "ghost")
	if !errors.Is(wrapped, ErrUnknownSkill) {
		t.Errorf("英文下 %q 应能判定为 ErrUnknownSkill", wrapped)
	}
	// 「未找到该技能: x（回收站中不存在）」：整句来自目录，身份仍是 ErrUnknownSkill。
	notInTrash := sentinelErr(ErrUnknownSkill, "store.err.unknownSkillInTrash", "ghost")
	if !errors.Is(notInTrash, ErrUnknownSkill) {
		t.Errorf("英文下 %q 应能判定为 ErrUnknownSkill", notInTrash)
	}
	if got := notInTrash.Error(); got != "store: skill not found: ghost (not in the trash)" {
		t.Errorf("英文下「回收站中不存在」= %q", got)
	}
	// 同上：非法的技能名 + 具体上下文，身份仍是 ErrInvalidName。
	badStamp := sentinelErr(ErrInvalidName, "store.err.invalidStampName", "a/b")
	if !errors.Is(badStamp, ErrInvalidName) {
		t.Errorf("英文下 %q 应能判定为 ErrInvalidName", badStamp)
	}
	// 哨兵之间不会互相混淆。
	if errors.Is(badStamp, ErrUnknownSkill) {
		t.Errorf("%q 不应判定为 ErrUnknownSkill", badStamp)
	}

	// 切回中文：同上两条的文字恢复成改造前的原文。
	setLangForTest(t, i18n.ZH)
	if got := notInTrash.Error(); got != "store: 未找到该技能: ghost（回收站中不存在）" {
		t.Errorf("中文下「回收站中不存在」= %q", got)
	}
	if got := badStamp.Error(); got != `store: 非法的技能名: 时间戳目录名 "a/b"` {
		t.Errorf("中文下「时间戳目录名」= %q", got)
	}
}

// TestTargetLangNamesAreSelfNames 验证目标语言的显示名是语言的自称：两种界面
// 语言下都一样（简体中文在英文界面里仍写作「简体中文」）。它们进目录只是
// 为了让「界面文案不硬编码」的守卫只有一个标准（见 settings.go 的 langLabel）。
func TestTargetLangNamesAreSelfNames(t *testing.T) {
	zhName := i18n.Lookup(i18n.ZH, "store.langName.zhCN")
	enName := i18n.Lookup(i18n.EN, "store.langName.zhCN")
	if zhName != "简体中文" || enName != "简体中文" {
		t.Errorf("简体中文的自称两种语言下都应是「简体中文」，得到 %q / %q", zhName, enName)
	}
	s := &Settings{TargetLang: "ja"}
	setLangForTest(t, i18n.EN)
	if got := s.LangLabel(); got != "日本語" {
		t.Errorf("英文界面下 ja 的显示名 = %q，想要「日本語」", got)
	}
	if got := (&Settings{TargetLang: "unknown"}).LangLabel(); got != "unknown" {
		t.Errorf("未登记的语言码应原样返回，得到 %q", got)
	}
}

// hasCJK 报告字符串里是否有中日韩文字（英文文案的自检用）：判定口径与
// internal/i18n 的守卫一致（汉字、平假名、片假名、谚文），另加全角/半角
// 形式区（，。：「」等）。
func hasCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) ||
			(r >= 0xff01 && r <= 0xffe6) { // 全角 ASCII、半角片假名、全角符号
			return true
		}
	}
	return false
}

// checkCatalogPair 是两套目录的自检（store 与 cli 的 catalog_test.go 各用
// 一次）：key 集合一致、取值非空、登记有效、占位符顺序相同。
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

// verbsOf 按出现顺序取出格式串里的动词（%s/%d/%q/%v…；%% 是字面百分号，
// 不算），用来检查两种语言的占位符顺序一致。
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
