package ui

// catalog 的测试与新语言行为：目录完整性、切到英文后界面真的变英文、
// 设置弹窗里的界面语言下拉，以及中英两张主界面截图（仅本地迭代用）。
//
// TestMain 把本包的测试统一放在中文下：既有测试断言的是中文界面文案
// （例如 detailTabFull），切换语言的测试自己临时切到英文再切回来。生产的
// 默认语言仍是英文（i18n.Default），由 main.go / CLI 在启动时解析。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/internal/store"
	myui "github.com/egoist/mygo/ui"
)

func TestMain(m *testing.M) {
	i18n.Set(i18n.ZH)
	os.Exit(m.Run())
}

// formatVerbs 返回 s 里的 fmt 动词序列（%s、%d、%v…；%% 不算），用来验证
// 两种语言的占位符数量与顺序一致（顺序不同会把参数填错位）。
func formatVerbs(s string) []string {
	var out []string
	for i := 0; i+1 < len(s); i++ {
		if s[i] != '%' {
			continue
		}
		if s[i+1] == '%' {
			i++
			continue
		}
		out = append(out, string(s[i+1]))
		i++
	}
	return out
}

// TestCatalogParity 验证两套目录的 key 完全一致、都登记进了 i18n、取值就是
// 目录里的那一条，且占位符一致。
func TestCatalogParity(t *testing.T) {
	if len(catalogZH) == 0 || len(catalogEN) == 0 {
		t.Fatal("目录不该是空的")
	}
	for k := range catalogZH {
		if _, ok := catalogEN[k]; !ok {
			t.Errorf("key %q 只有中文", k)
		}
	}
	for k := range catalogEN {
		if _, ok := catalogZH[k]; !ok {
			t.Errorf("key %q 只有英文", k)
		}
	}
	for k, zh := range catalogZH {
		en, ok := catalogEN[k]
		if !ok {
			continue
		}
		if !i18n.Has(k) {
			t.Errorf("key %q 没有登记进 i18n", k)
			continue
		}
		if got := i18n.Lookup(i18n.ZH, k); got != zh {
			t.Errorf("%q 的中文取值 = %q，目录里是 %q", k, got, zh)
		}
		if got := i18n.Lookup(i18n.EN, k); got != en {
			t.Errorf("%q 的英文取值 = %q，目录里是 %q", k, got, en)
		}
		if a, b := formatVerbs(zh), formatVerbs(en); len(a) != len(b) {
			t.Errorf("%q 的占位符数量不同：中文 %v，英文 %v", k, a, b)
		} else {
			for i := range a {
				if a[i] != b[i] {
					t.Errorf("%q 的占位符不同：中文 %v，英文 %v", k, a, b)
					break
				}
			}
		}
		if zh == en {
			t.Errorf("%q 的两种语言取值一样（%q），多半是忘了翻译", k, zh)
		}
	}
	t.Logf("目录里共 %d 条文案", len(catalogZH))
}

// TestDetailTabConstantsMatchCatalog 验证既有测试引用的两个中文常量就是
// 目录里的中文取值（它们不能各写各的）。
func TestDetailTabConstantsMatchCatalog(t *testing.T) {
	for _, c := range []struct{ got, key string }{
		{detailTabOverview, "ui.detail.tabOverview"},
		{detailTabFull, "ui.detail.tabFull"},
		{mdTruncatedNotice, "ui.markdown.truncated"},
	} {
		if want := i18n.Lookup(i18n.ZH, c.key); c.got != want {
			t.Errorf("%s 的中文常量 = %q，目录里是 %q", c.key, c.got, want)
		}
	}
}

// TestUISwitchesToEnglish 验证切到英文后界面文字真的变英文（筛选栏、状态条），
// 并且不再出现对应中文；切回中文恢复。文案每帧现取，所以只改 i18n 再渲染即可。
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

	click(t, a, tt, i18n.T("ui.settings.title"))
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
	label := i18n.T("ui.settings.interfaceLang")
	for i := 0; i < 80 && !tt.Focused(label); i++ {
		tt.Key(0, myui.KeyTab)
	}
	if !tt.Focused(label) {
		t.Fatalf("Tab 走不到界面语言下拉（%q），文本 %v", label, tt.Texts())
	}
}

// TestUISourceKeysAreRegistered 扫描本包非测试源码里的 i18n.T / i18n.Tf /
// i18n.NewError / i18n.Errorf / i18n.Wrapf 调用，断言每个 key 都在目录里。
// 漏登记的 key 不会编译报错，只会在界面上显示成 key 本身（i18n.T 的约定），
// 所以这里用源码扫描把它拦住。
func TestUISourceKeysAreRegistered(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	used := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range i18nKeyRe.FindAllStringSubmatch(string(src), -1) {
			key := m[1]
			if key == "" {
				key = m[2] // Wrapf 的 key 在第二个参数
			}
			used++
			if !i18n.Has(key) {
				t.Errorf("%s 用了未登记的 key %q", name, key)
			}
		}
	}
	if used == 0 {
		t.Error("一个 key 都没扫到，正则多半过期了")
	}
}

// i18nKeyRe 匹配取文案的调用：i18n.T/Tf/NewError/Errorf 的第一个参数，或
// i18n.Wrapf 的第二个参数（第一个是被包住的错误）。
var i18nKeyRe = regexp.MustCompile(`i18n\.(?:T|Tf|NewError|Errorf)\(\s*"([^"]+)"|i18n\.Wrapf\([^,]*,\s*"([^"]+)"`)

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
