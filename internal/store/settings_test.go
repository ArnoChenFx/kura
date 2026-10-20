package store

// settings_test.go — 设置读写的测试。
//
// 隔离硬要求（AGENTS.md）：全部用 t.TempDir() 当 Home，绝不碰用户真实的
// ~/.kura、~/.agents/skills。本文件不发任何网络请求。

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// newSettingsTestConfig 造一个隔离的 Config：root/home 都在 t.TempDir() 下，
// 复用 store_test.go 的 newTestConfig，保证与既有测试同一种隔离方式。
func newSettingsTestConfig(t *testing.T) *Config {
	t.Helper()
	return newTestConfig(t)
}

// TestLoadSettingsMissing 防回归：文件不存在时必须返回零值 + false 且不报错
// ——这是「首次使用」的正常路径，报错会让 UI 一启动就弹错误。
func TestLoadSettingsMissing(t *testing.T) {
	c := newSettingsTestConfig(t)
	s, ok, err := LoadSettings(c)
	if err != nil {
		t.Fatalf("文件不存在时不应报错: %v", err)
	}
	if ok {
		t.Fatal("文件不存在时 ok 应为 false")
	}
	if s == nil || s.BaseURL != "" || s.APIKey != "" || s.Model != "" || s.TargetLang != "" {
		t.Fatalf("文件不存在时应返回零值设置，得到 %+v", s)
	}
	if !s.Ready() || s.EffectiveProvider() != TranslationGoogle {
		t.Fatal("新用户应默认使用免密钥 Google 翻译")
	}
}

// TestSaveThenLoadRoundTrip 防回归：保存后再读必须逐字段一致，否则用户在
// 设置弹窗里填的内容会在重启后丢失。
func TestSaveThenLoadRoundTrip(t *testing.T) {
	c := newSettingsTestConfig(t)
	in := &Settings{
		Provider:   TranslationOpenAI,
		BaseURL:    "https://api.example.com/v1",
		APIKey:     "test-key-not-real", // 测试里只用明显的假 key
		Model:      "gpt-4o-mini",
		TargetLang: "zh-CN",
	}
	if err := SaveSettings(c, in); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}
	out, ok, err := LoadSettings(c)
	if err != nil || !ok {
		t.Fatalf("读回设置失败: ok=%v err=%v", ok, err)
	}
	if out.Provider != in.Provider || out.BaseURL != in.BaseURL || out.APIKey != in.APIKey ||
		out.Model != in.Model || out.TargetLang != in.TargetLang {
		t.Fatalf("读回的设置与写入不一致:\n got %+v\nwant %+v", out, in)
	}
	if out.CreatedAt == "" {
		t.Fatal("首次保存应自动补 CreatedAt")
	}
	// CreatedAt 一旦有了就不再漂移（首建时间语义）。
	if err := SaveSettings(c, out); err != nil {
		t.Fatalf("二次保存失败: %v", err)
	}
	again, _, err := LoadSettings(c)
	if err != nil {
		t.Fatalf("二次读回失败: %v", err)
	}
	if again.CreatedAt != out.CreatedAt {
		t.Fatalf("二次保存不应改动 CreatedAt: %q -> %q", out.CreatedAt, again.CreatedAt)
	}
}

// TestLoadSettingsCorruptJSON 防回归：JSON 损坏必须报错而不是静默按零值处理
// ——settings.json 里有 API key，静默丢掉等于让用户重填密钥。
func TestLoadSettingsCorruptJSON(t *testing.T) {
	c := newSettingsTestConfig(t)
	if err := os.MkdirAll(c.Home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Home, "settings.json"), []byte("{bad json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := LoadSettings(c)
	if err == nil {
		t.Fatal("损坏的 JSON 应报错")
	}
}

// TestSaveSettingsFilePerm 防回归：settings.json 含 API key，权限必须是 0600。
// Windows 的 NTFS 不完全遵循 POSIX 权限位（os.WriteFile 的 perm 被忽略，
// Stat().Mode().Perm() 通常返回 0666），所以在这类平台上**跳过**断言，
// 不写一个注定会挂的检查。
func TestSaveSettingsFilePerm(t *testing.T) {
	c := newSettingsTestConfig(t)
	if err := SaveSettings(c, &Settings{BaseURL: "https://x", APIKey: "test-key-not-real", Model: "m"}); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	info, err := os.Stat(filepath.Join(c.Home, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !posixPermsReliable() {
		t.Skipf("%s 的文件权限位不遵循 POSIX 语义（观察值 perm=%o），跳过权限断言",
			runtime.GOOS, info.Mode().Perm())
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("settings.json 权限应为 0600，得到 %o", got)
	}
}

// posixPermsReliable 报告当前平台的 Stat().Mode().Perm() 是否遵循 POSIX 语义。
// Windows 的 NTFS 不遵循（os.WriteFile 的 perm 参数被忽略），在这类平台上
// 权限断言只会给出假失败，必须跳过而不是写一个注定挂的检查。
func posixPermsReliable() bool {
	switch runtime.GOOS {
	case "windows", "plan9", "js":
		return false
	default:
		return true
	}
}

// TestNormalizeTargetLang 防回归：语言代码的归并与拒绝规则。大小写、
// 分隔符造成的「同一种语言」必须落到同一个规范码，否则 UI 下拉框会出现
// 重复项；不认识的代码必须报错，防止悄悄翻译成错误语言。
func TestNormalizeTargetLang(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", DefaultTargetLang},    // 空串 → 默认
		{"   ", DefaultTargetLang}, // 纯空白 → 默认
		{"zh-cn", "zh-CN"},         // 小写连字符
		{"zh_CN", "zh-CN"},         // 下划线
		{"ZH-CN", "zh-CN"},         // 大写
		{"zh", "zh-CN"},            // 纯语言码归并到 zh-CN
		{"zh-Hans", "zh-CN"},       // 文字脚本别名
		{"en", "en"},               // 纯语言码保持
		{"en-us", "en-US"},         // 区域码大写规范
		{"EN_GB", "en-GB"},         // 下划线 + 大写
		{"ja", "ja"},               // 其余语言原样规范
		{" ko ", "ko"},             // 首尾空白
	}
	for _, tc := range cases {
		got, err := NormalizeTargetLang(tc.in)
		if err != nil {
			t.Fatalf("NormalizeTargetLang(%q) 不应报错: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("NormalizeTargetLang(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// 不认识的输入必须以 ErrUnknownLang 报错（errors.Is 可判断）。
	for _, bad := range []string{"xx", "zz-ZZ", "中文", "zh-Hant"} {
		_, err := NormalizeTargetLang(bad)
		if !errors.Is(err, ErrUnknownLang) {
			t.Fatalf("NormalizeTargetLang(%q) 应返回 ErrUnknownLang，得到 %v", bad, err)
		}
	}
}

// TestSettingsReady 防回归：新用户默认免密钥；旧自定义配置仍要求三项齐全。
// 显式免费服务不受保留的旧字段影响，未知服务不得自动回退。
func TestSettingsReady(t *testing.T) {
	cases := []struct {
		name string
		s    *Settings
		want bool
	}{
		{"空指针", nil, false},
		{"零值", &Settings{}, true},
		{"Google", &Settings{Provider: TranslationGoogle}, true},
		{"Microsoft", &Settings{Provider: TranslationMicrosoft}, true},
		{"Google 保留旧字段", &Settings{Provider: TranslationGoogle, BaseURL: "https://custom.invalid"}, true},
		{"显式自定义未配置", &Settings{Provider: TranslationOpenAI}, false},
		{"未知服务", &Settings{Provider: "unknown"}, false},
		{"只填地址", &Settings{BaseURL: "https://api.example.com/v1"}, false},
		{"缺模型", &Settings{BaseURL: "https://api.example.com/v1", APIKey: "test-key-not-real"}, false},
		{"全填", &Settings{BaseURL: "https://api.example.com/v1", APIKey: "test-key-not-real", Model: "gpt-4o-mini"}, true},
		{"空格算没填", &Settings{BaseURL: " https://api.example.com/v1 ", APIKey: "  ", Model: "m"}, false},
	}
	for _, tc := range cases {
		if got := tc.s.Ready(); got != tc.want {
			t.Fatalf("%s: Ready() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestSettingsLegacyProvider 保留旧配置发送目标，包括尚未填齐的配置；
// 不能因为缺一项就把私有接口用户的文本改发到免费服务。
func TestSettingsLegacyProvider(t *testing.T) {
	cases := []struct {
		s    Settings
		want string
	}{
		{Settings{}, TranslationGoogle},
		{Settings{TargetLang: "ja"}, TranslationGoogle},
		{Settings{BaseURL: "https://custom.invalid"}, TranslationOpenAI},
		{Settings{APIKey: "fake-key"}, TranslationOpenAI},
		{Settings{Model: "local-model"}, TranslationOpenAI},
		{Settings{BaseURL: " ", APIKey: "\t", Model: "\n"}, TranslationGoogle},
		{Settings{Provider: TranslationMicrosoft, BaseURL: "https://custom.invalid"}, TranslationMicrosoft},
		{Settings{Provider: "unknown", APIKey: "fake-key"}, "unknown"},
	}
	for _, tc := range cases {
		if got := tc.s.EffectiveProvider(); got != tc.want {
			t.Fatalf("Provider %q 的实际服务为 %q，预期 %q", tc.s.Provider, got, tc.want)
		}
	}
}

// TestLangLabel 防回归：目标语言的可读名。未知代码原样返回而不是报错——
// LangLabel 只做显示，错误拦截是 NormalizeTargetLang 的职责。
func TestLangLabel(t *testing.T) {
	cases := []struct {
		lang string
		want string
	}{
		{"zh-CN", "简体中文"},
		{"en-US", "English (US)"},
		{"ja", "日本語"},
		{"", "简体中文"},                 // 空串按默认语言显示
		{"xx-Unknown", "xx-Unknown"}, // 未登记原样返回
	}
	for _, tc := range cases {
		s := &Settings{TargetLang: tc.lang}
		if got := s.LangLabel(); got != tc.want {
			t.Fatalf("LangLabel(%q) = %q, want %q", tc.lang, got, tc.want)
		}
	}
}

// TestErrNoSettingsSentinel 防回归：ErrNoSettings 是给调用方区分「从未保存过」
// 的哨兵，必须保持可 errors.Is 判断且文案带 store: 前缀。
func TestErrNoSettingsSentinel(t *testing.T) {
	if !errors.Is(ErrNoSettings, ErrNoSettings) {
		t.Fatal("哨兵错误应可与自身匹配")
	}
	if want := "store: "; len(ErrNoSettings.Error()) < len(want) || ErrNoSettings.Error()[:len(want)] != want {
		t.Fatalf("错误文案应以 %q 开头，得到 %q", want, ErrNoSettings.Error())
	}
}
