package store

import (
	"os"
	"testing"
	"time"
)

func boolPtr(v bool) *bool { return &v }

// writeRawConfig 直接写 config.json（不经任何 store 函数），用来搭出「旧版本留下的」
// 或「手改过的」配置。
func writeRawConfig(t *testing.T, c *Config, body string) {
	t.Helper()
	if err := os.MkdirAll(c.Home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.ConfigPath(), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readRawConfig(t *testing.T, c *Config) string {
	t.Helper()
	data, err := os.ReadFile(c.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestResolveAutoCheckDefaultsToOnAndWritesIt(t *testing.T) {
	base, _ := newLibraryFixture(t)
	on, err := ResolveAutoCheck(base, nil)
	if err != nil || !on {
		t.Fatalf("没有旧值时应缺省为开，得到 on=%v err=%v", on, err)
	}
	hc, ok, err := LoadHomeConfig(base)
	if err != nil || !ok || hc.AutoCheck == nil || !*hc.AutoCheck {
		t.Fatalf("缺省值应写进 config.json（之后不再看插件的旧值），得到 %+v", hc)
	}
}

func TestResolveAutoCheckMigratesLegacyValueOnce(t *testing.T) {
	base, _ := newLibraryFixture(t)
	on, err := ResolveAutoCheck(base, boolPtr(false))
	if err != nil || on {
		t.Fatalf("旧版本关过，迁移后应为关，得到 on=%v err=%v", on, err)
	}
	// 第二次启动：插件文件里的派生值又变成了别的（这里假装是开），也必须以
	// config.json 为准，不能再被插件的值覆盖。
	again, err := ResolveAutoCheck(base, boolPtr(true))
	if err != nil || again {
		t.Fatalf("迁移只发生一次：config.json 已有值时应忽略旧值，得到 on=%v err=%v", again, err)
	}
}

func TestResolveAutoCheckKeepsConfigValueOverLegacy(t *testing.T) {
	base, _ := newLibraryFixture(t)
	writeRawConfig(t, base, `{"roots":[],"autoCheckUpdates":false}`)
	on, err := ResolveAutoCheck(base, boolPtr(true))
	if err != nil || on {
		t.Fatalf("config.json 已有 false 时应忽略插件的 true，得到 on=%v err=%v", on, err)
	}
}

func TestResolveAutoCheckLeavesCorruptConfigAlone(t *testing.T) {
	base, _ := newLibraryFixture(t)
	const broken = "{bad"
	writeRawConfig(t, base, broken)
	on, err := ResolveAutoCheck(base, boolPtr(false))
	if err == nil {
		t.Fatal("config.json 损坏时应返回错误")
	}
	if !on {
		t.Error("损坏时应回到缺省（开）")
	}
	if got := readRawConfig(t, base); got != broken {
		t.Errorf("损坏的 config.json 不应被覆盖，得到 %q", got)
	}
}

func TestSetAutoCheckPersistsAndKeepsOtherSettings(t *testing.T) {
	base, extra := newLibraryFixture(t)
	lib, err := LoadLibrary(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.SetTheme(ThemeDark); err != nil {
		t.Fatal(err)
	}
	if err := lib.AddRoot(extra); err != nil {
		t.Fatal(err)
	}
	if err := lib.SetAutoCheck(false); err != nil {
		t.Fatal(err)
	}
	if lib.AutoCheck {
		t.Error("SetAutoCheck 后内存里的值应为关")
	}
	again, err := LoadLibrary(base)
	if err != nil {
		t.Fatal(err)
	}
	if again.AutoCheck {
		t.Error("重新读取后应为关")
	}
	if again.Theme != ThemeDark || len(again.Roots) != 2 {
		t.Errorf("写自动检查偏好不应丢掉其他设置：theme=%q roots=%v", again.Theme, again.Roots)
	}
}

func TestSetAutoCheckOverwritesCorruptConfig(t *testing.T) {
	base, _ := newLibraryFixture(t)
	writeRawConfig(t, base, "{bad")
	lib, _ := LoadLibrary(base)
	if err := lib.SetAutoCheck(false); err != nil {
		t.Fatalf("损坏后仍应能保存偏好: %v", err)
	}
	again, err := LoadLibrary(base)
	if err != nil {
		t.Fatalf("保存后 config.json 应可读: %v", err)
	}
	if again.AutoCheck {
		t.Error("保存后应为关")
	}
}

// TestCorruptConfigFallbacksCarryAutoCheck 验证 config.json 损坏时，任何一次
// 写回（改外观、改语言、增删目录）都会带上这次运行里的自动检查偏好。缺了它，
// 写回的新文件会把偏好当成缺省的开，而用户明明关过。
func TestCorruptConfigFallbacksCarryAutoCheck(t *testing.T) {
	writes := map[string]func(lib *Library, extra string) error{
		"SetTheme": func(lib *Library, _ string) error { return lib.SetTheme(ThemeLight) },
		"SetView":  func(lib *Library, _ string) error { return lib.SetView(ViewList) },
		"SetLang":  func(lib *Library, _ string) error { return lib.SetLang(LangEN) },
		"AddRoot":  func(lib *Library, extra string) error { return lib.AddRoot(extra) },
	}
	for name, write := range writes {
		t.Run(name, func(t *testing.T) {
			base, extra := newLibraryFixture(t)
			writeRawConfig(t, base, "{bad")
			lib, _ := LoadLibrary(base)
			lib.AutoCheck = false
			if err := write(lib, extra); err != nil {
				t.Fatal(err)
			}
			again, err := LoadLibrary(base)
			if err != nil {
				t.Fatal(err)
			}
			if again.AutoCheck {
				t.Error("config.json 损坏后的写回丢了「自动检查更新」的偏好")
			}
		})
	}
}

// TestSetLangFallbackKeepsThemeLoadedThisRun 验证：启动后 config.json 才被
// 弄坏，改语言时走的回退写入会带上启动时读到的外观（内存里的值），不会丢。
func TestSetLangFallbackKeepsThemeLoadedThisRun(t *testing.T) {
	base, _ := newLibraryFixture(t)
	writeRawConfig(t, base, `{"theme":"dark"}`)
	lib, _ := LoadLibrary(base)
	writeRawConfig(t, base, "{bad")
	if err := lib.SetLang(LangZH); err != nil {
		t.Fatal(err)
	}
	again, err := LoadLibrary(base)
	if err != nil {
		t.Fatal(err)
	}
	if again.Theme != ThemeDark || again.Lang != LangZH {
		t.Errorf("回退写入应带上内存里的外观，得到 theme=%q lang=%q", again.Theme, again.Lang)
	}
}

func TestLoadLibraryAutoCheckDefaultsOn(t *testing.T) {
	base, _ := newLibraryFixture(t)
	lib, err := LoadLibrary(base)
	if err != nil {
		t.Fatal(err)
	}
	if !lib.AutoCheck {
		t.Error("没有配置时「自动检查更新」应缺省为开")
	}
	writeRawConfig(t, base, `{"roots":[],"autoCheckUpdates":false}`)
	lib, _ = LoadLibrary(base)
	if lib.AutoCheck {
		t.Error("config.json 里是 false 时应读成关")
	}
}

func TestLastUpdateCheckRoundTrip(t *testing.T) {
	base, _ := newLibraryFixture(t)
	got, err := LastUpdateCheck(base)
	if err != nil || !got.At.IsZero() || got.Version != "" {
		t.Fatalf("从未检查过应返回零值，得到 %v err=%v", got, err)
	}
	at := time.Date(2026, 10, 18, 9, 30, 15, 0, time.FixedZone("CST", 8*3600))
	if err := SetLastUpdateCheck(base, UpdateCheck{At: at, Version: "0.1.18"}); err != nil {
		t.Fatal(err)
	}
	got, err = LastUpdateCheck(base)
	if err != nil {
		t.Fatal(err)
	}
	if !got.At.Equal(at.Truncate(time.Second)) {
		t.Errorf("读回的时间 %v 与写入的 %v 不一致", got.At, at)
	}
	if got.Version != "0.1.18" {
		t.Errorf("读回的版本 = %q，想要 %q", got.Version, "0.1.18")
	}
	// 再写一条版本未知的记录：时间要换掉，版本也必须被清掉，不能留着上一条。
	later := at.Add(time.Hour)
	if err := SetLastUpdateCheck(base, UpdateCheck{At: later}); err != nil {
		t.Fatal(err)
	}
	got, err = LastUpdateCheck(base)
	if err != nil {
		t.Fatal(err)
	}
	if !got.At.Equal(later.Truncate(time.Second)) || got.Version != "" {
		t.Errorf("清空版本的一记录应真的清掉版本，得到 %v / %q", got.At, got.Version)
	}
}

// TestLastUpdateCheckReadsTheOldFormat 验证兼容旧文件：旧版本的 config.json 只有
// updateCheckedAt（RFC3339 时间串），没有版本。读出来时间照旧，版本是空串，也就是
// 「版本未知」；读取方据此不采信它，宁可多查一次。
func TestLastUpdateCheckReadsTheOldFormat(t *testing.T) {
	base, _ := newLibraryFixture(t)
	writeRawConfig(t, base, `{"roots":[],"updateCheckedAt":"2026-10-18T09:30:15Z"}`)
	got, err := LastUpdateCheck(base)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 10, 18, 9, 30, 15, 0, time.UTC); !got.At.Equal(want) {
		t.Errorf("旧格式的时间 = %v，想要 %v", got.At, want)
	}
	if got.Version != "" {
		t.Errorf("旧格式没有版本，应读成空串（版本未知），得到 %q", got.Version)
	}
}

func TestSetLastUpdateCheckRefusesCorruptConfig(t *testing.T) {
	base, _ := newLibraryFixture(t)
	const broken = "{bad"
	writeRawConfig(t, base, broken)
	if err := SetLastUpdateCheck(base, UpdateCheck{At: time.Now(), Version: "0.1.18"}); err == nil {
		t.Fatal("config.json 损坏时后台写入应返回错误")
	}
	if got := readRawConfig(t, base); got != broken {
		t.Errorf("后台写入不应覆盖损坏的 config.json，得到 %q", got)
	}
	if _, err := LastUpdateCheck(base); err == nil {
		t.Error("损坏时读取检查时间应返回错误")
	}
}

func TestSetLastUpdateCheckKeepsOtherSettings(t *testing.T) {
	base, _ := newLibraryFixture(t)
	lib, _ := LoadLibrary(base)
	if err := lib.SetView(ViewList); err != nil {
		t.Fatal(err)
	}
	if err := SetLastUpdateCheck(base, UpdateCheck{At: time.Date(2026, 10, 18, 1, 0, 0, 0, time.UTC), Version: "0.1.18"}); err != nil {
		t.Fatal(err)
	}
	again, err := LoadLibrary(base)
	if err != nil {
		t.Fatal(err)
	}
	if again.View != ViewList {
		t.Errorf("记下检查时间不应丢掉排列模式，得到 %q", again.View)
	}
	if got, err := LastUpdateCheck(base); err != nil || got.Version != "0.1.18" {
		t.Errorf("记下检查时间应保留版本，得到 %q err=%v", got.Version, err)
	}
}
