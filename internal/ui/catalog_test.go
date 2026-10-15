package ui

// 文案与新语言行为：切到英文后界面真的变英文、设置弹窗里的界面语言下拉，
// 以及中英两张主界面截图（仅本地迭代用）。
//
// TestMain 把本包的测试统一放在中文下：既有测试断言的是中文界面文案
// （例如「全文」），切换语言的测试自己临时切到英文再切回来。生产的默认语言
// 由 MyGo 解析（跟随系统/用户语言，缺省英文）。
//
// 本包的视图每帧调 locales.Xxx()，取值口径是 mygo 官方的 locale func：生产
// 环境里 MyGo 把它接到 App.Locale；测试里 mygo 的事件循环没跑，TestMain 与
// newAppFor 用 internal/i18n 的 Set 换上固定的语言（见 app_test.go）。

import (
	"os"
	"testing"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/ArnoChenFx/kura/locales"
	myi18n "github.com/egoist/mygo/i18n"
	myui "github.com/egoist/mygo/ui"
)

func TestMain(m *testing.M) {
	i18n.Set(i18n.ZH)
	os.Exit(m.Run())
}

// TestUISwitchesToEnglish 验证切到英文后界面文字真的变英文（筛选栏、状态条），
// 并且不再出现对应中文；切回中文恢复。文案每帧现取，所以只改语言再渲染即可。
func TestUISwitchesToEnglish(t *testing.T) {
	defer i18n.Set(i18n.ZH)
	i18n.Set(i18n.ZH)

	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "alpha", nil)
	tt := showApp(a, 1280, 800)

	if !detHasText(tt, "全部 1") || !detHasText(tt, "共 1 个技能") {
		t.Fatalf("中文下应有中文的筛选栏与状态条，文本 %v", tt.Texts())
	}

	i18n.Set(i18n.EN)
	tt.Frame()
	if !detHasText(tt, "All 1") || !detHasText(tt, "Skills: 1") {
		t.Fatalf("英文下应有英文的筛选栏与状态条，文本 %v", tt.Texts())
	}
	for _, gone := range []string{"全部 1", "共 1 个技能", "启用 1", "设置"} {
		if detHasText(tt, gone) {
			t.Errorf("英文界面不该再出现中文 %q，文本 %v", gone, tt.Texts())
		}
	}

	i18n.Set(i18n.ZH)
	tt.Frame()
	if !detHasText(tt, "全部 1") || detHasText(tt, "All 1") {
		t.Fatalf("切回中文后应恢复中文文案，文本 %v", tt.Texts())
	}
}

// TestSettingsLanguageDropdown 验证设置弹窗里的「界面语言」下拉：存在、能
// 用键盘展开并选中英语、立刻换文案、偏好写进 config.json；再切回中文。
func TestSettingsLanguageDropdown(t *testing.T) {
	defer i18n.Set(i18n.ZH)
	a, cfg := newTestApp(t)
	// 目标语言先落到日本語：它默认的「简体中文」与界面语言下拉的一个选项同名，
	// 按文字点击会点到目标语言那个下拉（Find 只认第一个）。
	a.settings = store.Settings{TargetLang: "ja"}
	tt := showApp(a, 1280, 800)

	click(t, a, tt, locales.UiSettingsTitle())
	if !detHasText(tt, "界面语言") {
		t.Fatalf("设置弹窗应有界面语言一项，文本 %v", tt.Texts())
	}
	if !detHasText(tt, "跟随系统") {
		t.Fatalf("界面语言下拉应显示当前偏好，文本 %v", tt.Texts())
	}

	// 焦点经 Tab 走到下拉上（它的无障碍名是「界面语言」），再按 Down 展开：
	// 「跟随系统」在同一个窗口里也是外观开关的分段名，按文字点会点到它。
	focusLangSelect(t, tt)
	tt.Key(0, myui.KeyDown)
	click(t, a, tt, "English")
	if a.langPref != store.LangEN || i18n.Current() != i18n.EN {
		t.Fatalf("选 English 后偏好 = %q，当前语言 = %q", a.langPref, i18n.Current())
	}
	if !detHasText(tt, "Language") {
		t.Errorf("弹窗应立刻变英文（Language），文本 %v", tt.Texts())
	}
	a.settle()
	lib, err := store.LoadLibrary(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if lib.Lang != store.LangEN {
		t.Errorf("config.json 里的界面语言 = %q，想要 en", lib.Lang)
	}

	// 再切回简体中文：下拉的选项是同名语言的自称，两种界面语言下都认得。
	focusLangSelect(t, tt)
	tt.Key(0, myui.KeyDown)
	click(t, a, tt, "简体中文")
	if a.langPref != store.LangZH || i18n.Current() != i18n.ZH {
		t.Fatalf("选简体中文后偏好 = %q，当前语言 = %q", a.langPref, i18n.Current())
	}
	a.settle()
	if lib, err := store.LoadLibrary(cfg); err != nil || lib.Lang != store.LangZH {
		t.Errorf("config.json 里的界面语言 = %q err=%v，想要 zh", lib.Lang, err)
	}
}

// focusLangSelect 用 Tab 把键盘焦点移到界面语言下拉的触发器上；走不到就
// 让测试失败（下拉不在焦点顺序里，用户也就没法用键盘选它）。
func focusLangSelect(t *testing.T, tt *myui.Tester) {
	t.Helper()
	label := locales.UiSettingsInterfaceLang()
	for i := 0; i < 80 && !tt.Focused(label); i++ {
		tt.Key(0, myui.KeyTab)
	}
	if !tt.Focused(label) {
		t.Fatalf("Tab 走不到界面语言下拉（%q），文本 %v", label, tt.Texts())
	}
}

// TestUISourceHasNoHardcodedCJK 是本包替代旧「key 已登记」守卫的检查：视图
// 源码里不该出现中文字面量，文案一律经 locales.Xxx() 取。key 的完整性与
// 占位符由 `go tool mygo i18n check` 保证（CI 与生成时都跑）。
func TestUISourceHasNoHardcodedCJK(t *testing.T) {
	found, err := i18n.CJKLiterals(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range found {
		t.Errorf("%s 有硬编码中文界面文案；请移进 locales 并用 locales.Xxx() 取用", f)
	}
}

// TestMyGoControlsFollowTheWindowLocale 是本次迁移的收益证据：MyGo 自己的
// 控件文案（这里用查找栏的「无匹配项」「完成」）也跟着窗口语言变。kura 的
// locales 包在 init 里链接了 MyGo 的 zh-Hans 翻译（见 locales/catalog_gen.go
// 的导入），所以窗口语言是中文时 MyGo 的控件说中文；迁移前的自写 i18n
// 做不到这一点。
func TestMyGoControlsFollowTheWindowLocale(t *testing.T) {
	query, open, cur := "", true, 0
	tt := myui.NewTester(func(c *myui.Context) {
		myui.FindBar(c, &open, &query, 0, &cur)
	}, 600, 200)
	if !tt.HasText("Done") {
		t.Fatalf("英文下查找栏应说 Done，文本 %v", tt.Texts())
	}
	tt.SetLocale(string(i18n.ZH))
	if !tt.HasText("完成") || tt.HasText("Done") {
		t.Errorf("中文下查找栏应说「完成」，文本 %v", tt.Texts())
	}
}

// TestRenderI18nShots 渲染主界面的中文与英文两张截图（仅本地迭代用）：
// KURA_UI_SHOTS=<绝对路径> 时输出 <out>/main-zh.png 与 <out>/main-en.png，
// 供人工对比两套文案在同一布局下的观感。数据全在 t.TempDir() 里。
func TestRenderI18nShots(t *testing.T) {
	out := os.Getenv("KURA_UI_SHOTS")
	if out == "" {
		t.Skip("未设置 KURA_UI_SHOTS，跳过双语截图")
	}
	defer i18n.Set(i18n.ZH)
	// 同一份数据、同一个选中，只换语言：两张图除文案外逐像素可比（t.TempDir
	// 每调一次编号就变，重建应用会让路径也不一样）。
	a := seedDesignApp(t)
	a.selName = "notes-api"
	for _, c := range []struct {
		lang i18n.Lang
		name string
	}{{i18n.ZH, "main-zh"}, {i18n.EN, "main-en"}} {
		i18n.Set(c.lang)
		tt := newTester(a, 1280, 800)
		tt.SetPreferences(myui.Preferences{ReduceMotion: true})
		a.settle()
		tt.Frame()
		a.settle()
		tt.Frame()
		// 状态行存的是动作发生时渲染好的那一句（见 app.go 的 status 字段），
		// 不会因为切语言而变；两张图都在最后一帧前清掉它，对比才是纯粹的
		// 「同一界面两种文案」。
		a.status, a.statusErr = "", false
		tt.Frame()
		saveShot(t, out, c.name, tt)
	}
}

// TestLocaleForPref 钉住「偏好 + 系统 locale → 交给 MyGo 的语言标签」这条
// 映射：固定语言直接就是标签；system 由 kura 自己按系统 locale 解（zh* 算
// 中文，其余算英文），与 CLI 的 --lang 判据一致。
//
// 这条测试是为了防一个真回退：如果把 system 直接交给 MyGo（SetLocale("")），
// 繁体中文系统（zh-TW / zh-Hant / zh-HK）匹配不上唯一的 zh-Hans 候选，会落
// 到 catalogs 的 source 语言——窗口显示英文而 CLI 显示中文。
func TestLocaleForPref(t *testing.T) {
	cases := []struct {
		pref      string
		sysLocale string
		want      string
	}{
		// 固定语言：与系统 locale 无关。
		{"zh", "en-US", "zh-Hans"},
		{"en", "zh-CN", "en"},
		{"zh", "zh-TW", "zh-Hans"},
		// 跟随系统：简繁都是中文，其余英文。
		{store.LangSystem, "zh-CN", "zh-Hans"},
		{store.LangSystem, "zh-TW", "zh-Hans"},
		{store.LangSystem, "zh-HK", "zh-Hans"},
		{store.LangSystem, "zh-Hant-TW", "zh-Hans"},
		{store.LangSystem, "en-US", "en"},
		{store.LangSystem, "de-DE", "en"},
		{store.LangSystem, "ja-JP", "en"},
		// 认不出来的系统 locale 与空偏好：英文（与旧实现一致）。
		{store.LangSystem, "C", "en"},
		{store.LangSystem, "POSIX", "en"},
		{store.LangSystem, "", "en"},
		{"", "zh-CN", "zh-Hans"},
		{"", "en-US", "en"},
		// 配置里写了不认识的取值：按「跟随系统」处理（store.LangPref 的口径）。
		{"klingon", "zh-CN", "zh-Hans"},
	}
	for _, c := range cases {
		if got := localeFor(c.pref, c.sysLocale); got != c.want {
			t.Errorf("localeFor(%q, %q) = %q，想要 %q", c.pref, c.sysLocale, got, c.want)
		}
	}
}

// TestLocaleForPrefFallsBackToATranslatedLanguage 是上面那条的守门人：解出来
// 的标签必须是 catalogs 真的有的语言（否则 MyGo 会落到 source 语言，界面又
// 变回英文，而且没有任何报错）。
func TestLocaleForPrefFallsBackToATranslatedLanguage(t *testing.T) {
	langs := locales.Catalog.Languages()
	has := func(tag string) bool {
		for _, l := range langs {
			if l == tag {
				return true
			}
		}
		return false
	}
	for _, sysLocale := range []string{"zh-CN", "zh-TW", "zh-Hant-TW", "en-US", "de-DE", ""} {
		tag := localeFor(store.LangSystem, sysLocale)
		if !has(tag) {
			t.Errorf("localeFor(system, %q) = %q，不在 catalogs 的语言 %v 里",
				sysLocale, tag, langs)
		}
	}
	// 接线自己也要钉：生产路径走的是 localeForPref（带真实系统 locale），不是 localeFor。它必须给出一个具体语言标签，不能是空串
	// （空串等于把「跟随系统」交给 MyGo，台繁系统上就是回退）。
	tag := localeForPref(store.LangSystem)
	if tag == "" {
		t.Errorf("localeForPref(system) 为空，等于把「跟随系统」交给 MyGo")
	} else if !has(tag) {
		t.Errorf("localeForPref(system) = %q，不在 catalogs 的语言 %v 里", tag, langs)
	}

	// 这条是回退的判据：MyGo 的匹配对 zh-TW 找不到 zh-Hans。
	if m := myi18n.Match([]string{"zh-TW"}, langs); m != "" {
		t.Logf("MyGo 现在能把 zh-TW 匹配到 %q 了（若如此，本测试的前提值得重看）", m)
	}
}
