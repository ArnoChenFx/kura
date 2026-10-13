// 设置弹窗与设置读写的界面测试。全部数据造在 t.TempDir() 里（见 newTestApp），
// 绝不触碰真实的 ~/.kura 与 ~/.agents；假密钥一律写 test-key-not-real；
// 不发任何网络请求（翻译函数只在 translate_ui_test.go 里被注入假实现）。
package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
)

// fakeKey 是测试里用的假密钥，绝不是真实凭据。
const fakeKey = "test-key-not-real"

// TestSaveSettingsRoundTrip 验证：SaveSettingsAsync 写盘后，store.LoadSettings
// 能读回填的值；目标语言被规范化；再次保存不改动首建时间。
func TestSaveSettingsRoundTrip(t *testing.T) {
	a, cfg := newTestApp(t)
	a.SaveSettingsAsync(store.Settings{
		BaseURL: " https://example.invalid/v1 ", APIKey: fakeKey, Model: "fake-model", TargetLang: "ja",
	})
	// 内存立刻生效（后台写盘与状态行经 update 回放）。
	if a.settings.BaseURL != "https://example.invalid/v1" || a.settings.TargetLang != "ja" {
		t.Fatalf("内存里的设置没立刻更新：%+v", a.settings)
	}
	a.settle()

	got, exists, err := store.LoadSettings(cfg)
	if err != nil || !exists {
		t.Fatalf("读回设置失败 exists=%v err=%v", exists, err)
	}
	if got.BaseURL != "https://example.invalid/v1" || got.APIKey != fakeKey || got.Model != "fake-model" || got.TargetLang != "ja" {
		t.Errorf("读回的设置与填写的不一致：%+v", got)
	}
	if got.CreatedAt == "" {
		t.Error("首次保存应补上 createdAt")
	}
	if a.statusErr || a.status != "设置已保存" {
		t.Errorf("状态行应报告已保存，得到 %q (err=%v)", a.status, a.statusErr)
	}

	// 再存一次（只改模型）：首建时间不漂移。
	first := got.CreatedAt
	a.SaveSettingsAsync(store.Settings{BaseURL: got.BaseURL, APIKey: fakeKey, Model: "other-model", TargetLang: "en"})
	a.settle()
	again, _, err := store.LoadSettings(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if again.Model != "other-model" || again.TargetLang != "en" || again.CreatedAt != first {
		t.Errorf("第二次保存后应只改模型与语言、保留 createdAt=%q：%+v", first, again)
	}
}

// TestSaveSettingsRejectsUnknownLang 验证：目标语言不被支持时不写盘、不改内存，
// 错误显示在弹窗里（经「确认保存」按钮走完整的界面路径）。
func TestSaveSettingsRejectsUnknownLang(t *testing.T) {
	a, cfg := newTestApp(t)
	a.SaveSettingsAsync(store.Settings{BaseURL: "https://x", APIKey: fakeKey, Model: "m", TargetLang: "klingon"})
	a.settle()
	if _, exists, err := store.LoadSettings(cfg); err != nil || exists {
		t.Fatalf("非法语言不应写盘：exists=%v err=%v", exists, err)
	}
	if a.settings.Ready() || a.settingsUI.err == "" {
		t.Errorf("非法语言应拒绝并给出弹窗错误：settings=%+v err=%q", a.settings, a.settingsUI.err)
	}

	// 界面路径：下拉里选了一个不认识的名字（手工构造），点「确认保存」。
	tt := showApp(a, 1280, 800)
	click(t, a, tt, "设置")
	a.settingsUI.lang = "火星文"
	a.settingsUI.baseURL, a.settingsUI.apiKey, a.settingsUI.model = "https://x", fakeKey, "m"
	tt.Frame() // 先重排一帧：「没填全」提醒消失会让按钮上移，点击要用新位置
	click(t, a, tt, "确认保存")
	if !a.settingsUI.open {
		t.Error("保存被拒绝后弹窗应保持打开")
	}
	if !detHasText(tt, "不支持的翻译目标语言") {
		t.Errorf("弹窗里应显示语言错误，文本 %v", tt.Texts())
	}
	if _, exists, _ := store.LoadSettings(cfg); exists {
		t.Error("界面路径下非法语言也不应写盘")
	}
}

// TestSettingsModalOpenAndCancel 验证：左栏「设置」打开弹窗，帧里有各字段
// 标签；取消后关闭，且不写盘。
func TestSettingsModalOpenAndCancel(t *testing.T) {
	a, cfg := newTestApp(t)
	tt := showApp(a, 1280, 800)

	if a.settingsUI.open {
		t.Fatal("初始不应打开设置弹窗")
	}
	click(t, a, tt, "设置")
	if !a.settingsUI.open {
		t.Fatalf("点「设置」后弹窗应打开，文本 %v", tt.Texts())
	}
	for _, want := range []string{"翻译 API（OpenAI 兼容）", "API 地址", "密钥", "模型", "目标语言"} {
		if !detHasText(tt, want) {
			t.Errorf("设置弹窗缺少 %q，文本 %v", want, tt.Texts())
		}
	}
	// 没填全时给出提醒，但不阻止保存。
	if !detHasText(tt, "才能翻译技能描述") {
		t.Errorf("未填全时应提醒翻译暂不可用，文本 %v", tt.Texts())
	}

	click(t, a, tt, "取消")
	if a.settingsUI.open {
		t.Error("点「取消」后弹窗应关闭")
	}
	if _, exists, _ := store.LoadSettings(cfg); exists {
		t.Error("取消不应写盘")
	}
}

// TestSettingsModalSavesFromInputs 验证：在弹窗输入框里真的输入文字（走键盘
// 路径），点「确认保存」后写盘、弹窗关闭、状态行报告。
func TestSettingsModalSavesFromInputs(t *testing.T) {
	a, cfg := newTestApp(t)
	tt := showApp(a, 1280, 800)
	click(t, a, tt, "设置")

	for _, in := range []struct{ label, text string }{
		{"API 地址", "https://example.invalid/v1"},
		{"密钥", fakeKey},
		{"模型", "fake-model"},
	} {
		if err := tt.Click(in.label); err != nil {
			t.Fatalf("点不到输入框 %q: %v；文本 %v", in.label, err, tt.Texts())
		}
		tt.Type(in.text)
		refresh(a, tt)
	}
	if a.settingsUI.baseURL != "https://example.invalid/v1" || a.settingsUI.apiKey != fakeKey || a.settingsUI.model != "fake-model" {
		t.Fatalf("输入没有进到弹窗状态：%+v", a.settingsUI)
	}

	click(t, a, tt, "确认保存")
	if a.settingsUI.open {
		t.Errorf("保存成功后弹窗应关闭，文本 %v", tt.Texts())
	}
	a.settle()
	got, exists, err := store.LoadSettings(cfg)
	if err != nil || !exists {
		t.Fatalf("应写出 settings.json：exists=%v err=%v", exists, err)
	}
	if got.BaseURL != "https://example.invalid/v1" || got.APIKey != fakeKey || got.Model != "fake-model" || got.TargetLang != "zh-CN" {
		t.Errorf("写盘内容不对：%+v", got)
	}
	if !a.settings.Ready() {
		t.Error("保存后内存里的设置应已就绪")
	}
	// 弹窗里的输入副本不该留着密钥。
	if a.settingsUI.apiKey != "" {
		t.Error("保存后弹窗状态不应残留密钥")
	}
}

// TestSettingsModalKeepsEditsAcrossBuilds 验证：用户在弹窗里改过的值不会在
// 后续构建里被 a.settings 覆盖；重新打开才会重新灌入。
func TestSettingsModalKeepsEditsAcrossBuilds(t *testing.T) {
	a, _ := newTestApp(t)
	a.settings = store.Settings{BaseURL: "https://saved", APIKey: fakeKey, Model: "saved-model", TargetLang: "fr"}
	tt := showApp(a, 1280, 800)

	a.OpenSettings()
	if a.settingsUI.baseURL != "https://saved" || a.settingsUI.model != "saved-model" || a.settingsUI.lang != "Français" {
		t.Fatalf("打开时应灌入已存设置：%+v", a.settingsUI)
	}
	a.settingsUI.baseURL = "https://edited"
	tt.Frame()
	tt.Frame()
	if a.settingsUI.baseURL != "https://edited" {
		t.Errorf("构建不应覆盖用户的编辑，得到 %q", a.settingsUI.baseURL)
	}
}

// TestOpenSettingsUnknownSavedLang 验证：settings.json 里手改出的未知语言
// 不会让弹窗崩溃——落到默认语言并提示。
func TestOpenSettingsUnknownSavedLang(t *testing.T) {
	a, _ := newTestApp(t)
	a.settings = store.Settings{TargetLang: "klingon"}
	a.OpenSettings()
	if a.settingsUI.lang != "简体中文" || a.settingsUI.note == "" {
		t.Errorf("未知语言应落到默认并提示：%+v", a.settingsUI)
	}
}

// TestTargetLangsAreNormalized 验证：UI 侧列出的每个语言码都被 store 认得，
// 且规范化后不变；显示名互不重复。否则保存会被拒绝。
func TestTargetLangsAreNormalized(t *testing.T) {
	seen := map[string]bool{}
	for _, l := range targetLangs {
		got, err := store.NormalizeTargetLang(l.code)
		if err != nil || got != l.code {
			t.Errorf("语言 %q 规范化得到 %q, err=%v", l.code, got, err)
		}
		if seen[l.label] {
			t.Errorf("显示名 %q 重复", l.label)
		}
		seen[l.label] = true
		if langCodeFor(l.label) != l.code || langLabelFor(l.code) != l.label {
			t.Errorf("语言 %q 的码↔名映射不闭合", l.code)
		}
	}
	if langLabelFor(store.DefaultTargetLang) == "" {
		t.Error("默认语言必须在选项里")
	}
}

// TestCorruptSettingsFileDoesNotBreakApp 验证：settings.json 损坏时应用照常
// 构造、可渲染，错误显示在左栏与设置弹窗里，保存会覆盖它。
func TestCorruptSettingsFileDoesNotBreakApp(t *testing.T) {
	root := t.TempDir()
	cfg, err := store.NewConfig(filepath.Join(root, "skills"), filepath.Join(root, ".kura"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.Home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.Home, store.SettingsFileName), []byte("{bad json"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newAppFor(cfg)
	if a.settingsErr == "" || a.settings.Ready() {
		t.Fatalf("损坏的设置应记录错误并按零值处理：err=%q settings=%+v", a.settingsErr, a.settings)
	}
	tt := showApp(a, 1280, 800)
	if !detHasText(tt, "设置读取失败") {
		t.Errorf("左栏应报告设置读取失败，文本 %v", tt.Texts())
	}
	click(t, a, tt, "设置")
	if !detHasText(tt, "保存会覆盖它") {
		t.Errorf("弹窗应说明保存会覆盖损坏的文件，文本 %v", tt.Texts())
	}

	// 保存后文件恢复可读，错误清掉。
	a.SaveSettingsAsync(store.Settings{BaseURL: "https://x", APIKey: fakeKey, Model: "m"})
	a.settle()
	if _, exists, err := store.LoadSettings(cfg); err != nil || !exists {
		t.Errorf("保存应修复损坏的文件：exists=%v err=%v", exists, err)
	}
	if a.settingsErr != "" {
		t.Errorf("保存成功后应清掉读取错误，得到 %q", a.settingsErr)
	}
}
