package store

// catalog_test.go — 文案与错误随语言变化、身份不变的自检。
//
// 文案本身（422 条）住在 locales/（ARB，由 `go tool mygo generate` 校验），
// 这里守的是两条迁移时最容易做砸的性质：
//
//   - 同一条错误的文字随当前语言现取（后台任务里产生的错误，切语言后显示
//     也要是新语言的句子）；
//   - 错误身份只由 key 决定，与语言无关（errors.Is 在英文下照样成立）。
//
// 本包既有的测试比对的是错误信息里的中文文案（如「已存在同名技能」），所以
// TestMain 先把界面语言固定成中文；本文件的测试切到英文验证完会切回来。
// 语言现在是进程级的 locale func（i18n.Set），任何一个测试忘了还原都会污染
// 同包其它测试，因此一律用 setLangForTest（t.Cleanup 还原）。

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/locales"
)

// TestMain 把界面语言固定成中文再跑本包测试：既有断言依赖中文文案，不该随
// 开发机或 CI 的系统 locale 变化。英文取值由本文件的测试逐个覆盖。
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

// TestChineseTextIsVerbatim 钉住几条改造前就有的中文取值：它们是既有测试与
// 用户习惯依赖的文案，改动必须是有意的（改这里等于改用户可见文案）。
func TestChineseTextIsVerbatim(t *testing.T) {
	zh := locales.In(string(i18n.ZH))
	pinned := map[string]string{
		"未找到该技能":     zh.StoreErrUnknownSkill(),
		"根目录已存在同名技能": zh.StoreErrSkillExists("pdf-tools"),
		"只描述默认技能目录":  zh.StoreErrImportDefaultOnly(),
	}
	for want, got := range pinned {
		if !strings.Contains(got, want) {
			t.Errorf("中文取值应含 %q，得到 %q", want, got)
		}
	}
	if got := zh.StoreErrSkillExists("pdf-tools"); got != "store: 根目录已存在同名技能 pdf-tools" {
		t.Errorf("带占位符的中文 = %q", got)
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
	// 带占位符的错误同样跟着语言走。
	withArg := i18n.NewError("storeErrInstallSkill", func() string { return locales.StoreErrInstallSkill("pdf-tools") })
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
	// 「未找到该技能: x（回收站中不存在）」：整句来自 locales，身份仍是 ErrUnknownSkill。
	notInTrash := sentinelErr(ErrUnknownSkill, "storeErrUnknownSkillInTrash",
		func() string { return locales.StoreErrUnknownSkillInTrash("ghost") })
	if !errors.Is(notInTrash, ErrUnknownSkill) {
		t.Errorf("英文下 %q 应能判定为 ErrUnknownSkill", notInTrash)
	}
	if got := notInTrash.Error(); got != "store: skill not found: ghost (not in the trash)" {
		t.Errorf("英文下「回收站中不存在」= %q", got)
	}
	// 同上：非法的技能名 + 具体上下文，身份仍是 ErrInvalidName。
	badStamp := sentinelErr(ErrInvalidName, "storeErrInvalidStampName",
		func() string { return locales.StoreErrInvalidStampName("a/b") })
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
// 语言下都一样（简体中文在英文界面里仍写作「简体中文」）。
func TestTargetLangNamesAreSelfNames(t *testing.T) {
	if zhName, enName := locales.In(string(i18n.ZH)).StoreLangNameZhCN(), locales.In(string(i18n.EN)).StoreLangNameZhCN(); zhName != "简体中文" || enName != "简体中文" {
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
