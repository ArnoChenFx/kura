package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/ArnoChenFx/kura/locales"
	myui "github.com/egoist/mygo/ui"
)

// 走真实下拉交互，检查控件消失/重现后仍保留自定义字段且保存稳定服务标识。
func TestTranslationProviderSelectionPreservesCustomSettings(t *testing.T) {
	a, cfg := newTestApp(t)
	a.settings = trReadySettings()
	tt := showApp(a, 1280, 800)
	a.OpenSettings()
	tt.Frame()
	choose := func(label string) {
		t.Helper()
		field := locales.UiSettingsProvider()
		for i := 0; i < 80 && !tt.Focused(field); i++ {
			tt.Key(0, myui.KeyTab)
		}
		if !tt.Focused(field) {
			t.Fatal("翻译服务下拉未获得焦点")
		}
		tt.Key(0, myui.KeyDown)
		click(t, a, tt, label)
		tt.Frame()
	}
	choose(locales.UiSettingsProviderGoogle())
	if a.settingsUI.provider != store.TranslationGoogle || detHasText(tt, locales.UiSettingsBaseURL()) {
		t.Fatal("免费服务不应显示密钥输入")
	}
	choose(locales.UiSettingsProviderOpenAI())
	if a.settingsUI.apiKey != fakeKey || a.settingsUI.model != "fake-model" || !detHasText(tt, locales.UiSettingsBaseURL()) {
		t.Fatal("切回自定义服务应恢复原字段")
	}
	choose(locales.UiSettingsProviderMicrosoft())
	click(t, a, tt, locales.UiSettingsSaveA11y())
	a.settle()
	s, exists, err := store.LoadSettings(cfg)
	if err != nil || !exists || s.Provider != store.TranslationMicrosoft || s.APIKey != fakeKey {
		t.Fatalf("服务选择或隐藏字段未保存：%v", err)
	}
}

func TestDefaultTranslationRequiresExplicitToggle(t *testing.T) {
	a, cfg := newTestApp(t)
	f := &fakeTranslator{}
	a.translate = f.translate
	seedDescribed(t, cfg, "free", "Free translation example.", nil)
	tt := showApp(a, 1280, 800)
	click(t, a, tt, "free")
	if f.calls != 0 || !a.settings.Ready() || a.settings.EffectiveProvider() != store.TranslationGoogle {
		t.Fatal("默认服务应就绪，但未开启译文时不应发送请求")
	}
	click(t, a, tt, locales.UiTranslateTranslated())
	if f.calls != 1 {
		t.Fatalf("开启译文应恰好翻译一次描述，得到 %d", f.calls)
	}
}

func TestChangingProviderCancelsOldTranslation(t *testing.T) {
	a, _ := newTestApp(t)
	live := a.trans.current()
	a.trans.items["old"] = translateEntry{src: "text", dst: "old translation"}
	a.SaveSettingsAsync(store.Settings{Provider: store.TranslationMicrosoft})
	if live.Err() != context.Canceled || len(a.trans.items) != 0 {
		t.Fatal("切换服务必须取消旧请求并清理旧译文")
	}
	a.settle()
}

func TestSettingsActionsRemainVisibleInMinimumWindow(t *testing.T) {
	for _, provider := range []string{store.TranslationGoogle, store.TranslationMicrosoft, store.TranslationOpenAI} {
		t.Run(provider, func(t *testing.T) {
			a, _ := newTestApp(t)
			a.settings.Provider = provider
			a.settingsErr = strings.Repeat("broken configuration ", 15)
			tt := showApp(a, 880, 520)
			a.OpenSettings()
			tt.Frame()
			for _, label := range []string{locales.UiSettingsSaveA11y(), locales.UiCommonCancel()} {
				r, ok := tt.Find(label)
				if !ok || r.Y < 20 || r.Y+r.H > 500 {
					t.Fatalf("最小窗口中操作按钮被裁切：%s %+v", label, r)
				}
			}
			click(t, a, tt, locales.UiCommonCancel())
			if a.settingsUI.open {
				t.Fatal("可见的取消按钮必须可操作")
			}
		})
	}
}

func TestDescriptionTranslationRetryKeepsOriginalOnFailure(t *testing.T) {
	a, _, f, tt := trApp(t, true)
	f.err = errors.New("temporary failure")
	click(t, a, tt, "alpha")
	click(t, a, tt, locales.UiTranslateTranslated())
	if f.calls != 1 || countText(tt, "Reads PDF files.") != 2 {
		t.Fatal("失败时应保留原文且不自动重试")
	}
	f.err = nil
	click(t, a, tt, locales.UiTranslateRetry())
	if f.calls != 2 || !detHasText(tt, "[译]Reads PDF files.") {
		t.Fatal("重试应重新请求并显示译文")
	}
}
