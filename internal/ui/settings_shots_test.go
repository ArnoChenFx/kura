// 设置弹窗与译文模式的截图（仅本地迭代用）：设置 KURA_UI_SHOTS=<输出目录>
// 后渲染几张 PNG，供人工检查布局；不设置时直接跳过，CI 不受影响。
// 翻译函数是假实现，不联网；数据全在 t.TempDir() 里。
package ui

import (
	"os"
	"testing"
)

// TestRenderSettingsTranslateShots 渲染：设置弹窗（填了值）、译文模式
// 未配置时的引导、译文模式下的详情与卡片。
func TestRenderSettingsTranslateShots(t *testing.T) {
	out := os.Getenv("KURA_UI_SHOTS")
	if out == "" {
		t.Skip("未设置 KURA_UI_SHOTS，跳过截图")
	}

	t.Run("settings-modal", func(t *testing.T) {
		a, _, _, tt := trApp(t, false)
		click(t, a, tt, "设置")
		a.settingsUI.baseURL = "https://api.openai.com/v1"
		a.settingsUI.apiKey = fakeKey
		a.settingsUI.lang = "日本語"
		tt.Frame()
		saveShot(t, out, "settings-modal", tt)
	})

	t.Run("translate-not-configured", func(t *testing.T) {
		a, _, _, tt := trApp(t, false)
		click(t, a, tt, "alpha")
		click(t, a, tt, "译文")
		saveShot(t, out, "translate-not-configured", tt)
	})

	t.Run("translate-on", func(t *testing.T) {
		a, _, _, tt := trApp(t, true)
		click(t, a, tt, "alpha")
		click(t, a, tt, "译文")
		a.settle()
		tt.Frame()
		saveShot(t, out, "translate-on", tt)
	})
}
