// 状态截图工具（仅本地迭代用）：设置 KURA_UI_SHOTS=<输出目录> 后，
// go test ./internal/ui/ -run TestRenderStateShots 会把各界面状态渲染成
// PNG，供人工检查布局。不设置时测试直接跳过，CI 不受影响。
// 绝不触碰真实的 ~/.agents/skills：全部状态都构造在 t.TempDir() 里。
package ui

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
	myui "github.com/egoist/mygo/ui"
)

// TestRenderStateShots 渲染：空态、未选中详情、停用/回收站/损坏行、
// 搜索无结果、评审面板。每个状态一张 PNG。
func TestRenderStateShots(t *testing.T) {
	out := os.Getenv("KURA_UI_SHOTS")
	if out == "" {
		t.Skip("未设置 KURA_UI_SHOTS，跳过状态截图")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}

	// save 把最后一帧写成 PNG。
	save := func(t *testing.T, tt *myui.Tester, name string) {
		t.Helper()
		f, err := os.Create(filepath.Join(out, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
		t.Logf("已保存 %s", filepath.Join(out, name+".png"))
	}

	// 状态一：完全空的根目录。
	t.Run("empty-root", func(t *testing.T) {
		a, _ := newTestApp(t)
		tt := newTester(a, 1140, 760)
		a.settle()
		tt.Frame()
		a.settle()
		tt.Frame()
		save(t, tt, "empty-root")
	})

	// 状态二：根目录里有技能但未选中任何一项（详情空态）。
	t.Run("no-selection", func(t *testing.T) {
		a, cfg := newTestApp(t)
		seedSkill(t, cfg, "alpha", nil)
		seedSkill(t, cfg, "beta", nil)
		tt := newTester(a, 1140, 760)
		a.settle()
		tt.Frame()
		a.settle()
		tt.Frame()
		save(t, tt, "no-selection")
	})

	// 状态三：停用、回收站、元数据损坏的行外观 + 选中带完整元数据的技能。
	t.Run("states", func(t *testing.T) {
		a, cfg := newTestApp(t)
		seedSkill(t, cfg, "enabled-one", &store.Meta{
			ID: "enabled-one", DisplayName: "enabled-one",
			Source:      store.Source{Type: store.SourceGitHub, URL: "https://github.com/me/en.git", Ref: "main", Commit: "abc1234abcd"},
			PluginName:  "some-plugin",
			InstalledAt: "2026-01-01T00:00:00Z",
			UpdatedAt:   "2026-02-02T00:00:00Z",
			ContentHash: "sha256:deadbeefdeadbeef",
		})
		seedSkill(t, cfg, "disabled-one", nil)
		if _, err := cfg.Disable("disabled-one"); err != nil {
			t.Fatal(err)
		}
		seedSkill(t, cfg, "trashed-one", nil)
		if _, err := cfg.Remove("trashed-one"); err != nil {
			t.Fatal(err)
		}
		// 元数据损坏的技能。
		bad := cfg.SkillPath("corrupt-one")
		if err := os.MkdirAll(bad, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bad, store.MetaFileName), []byte("{bad"), 0o644); err != nil {
			t.Fatal(err)
		}
		tt := newTester(a, 1140, 760)
		a.settle()
		tt.Frame()
		a.settle()
		tt.Frame()
		// 选中 enabled-one 看元数据字段。
		if err := tt.Click("enabled-one"); err != nil {
			t.Fatal(err)
		}
		a.settle()
		tt.Frame()
		save(t, tt, "mixed-states-selected")
	})

	// 状态四：搜索无结果。
	t.Run("search-empty", func(t *testing.T) {
		a, cfg := newTestApp(t)
		seedSkill(t, cfg, "alpha", nil)
		tt := newTester(a, 1140, 760)
		a.settle()
		tt.Frame()
		a.settle()
		tt.Frame()
		a.query = "不存在的词"
		tt.Frame()
		save(t, tt, "search-empty")
	})

	// 状态五：评审面板（单技能，有变化）。
	t.Run("review-modal", func(t *testing.T) {
		a, cfg := newTestApp(t)
		seedWithLocalSource(t, cfg, "rev",
			"---\nname: rev\n---\n\ninstalled\n",
			"---\nname: rev\n---\n\nupstream\n")
		tt := newTester(a, 1140, 760)
		a.settle()
		tt.Frame()
		a.settle()
		tt.Frame()
		a.review = reviewState{open: true, skill: "rev", preview: mustPreview(t, cfg, "rev")}
		tt.Frame()
		save(t, tt, "review-modal")
	})
}

// mustPreview 在测试里同步取一次更新预览。
func mustPreview(t *testing.T, cfg *store.Config, name string) *store.UpdatePreview {
	t.Helper()
	p, err := cfg.PreviewUpdate(name)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
