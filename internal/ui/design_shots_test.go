package ui

// 设计稿截图（仅本地迭代用）：设置 KURA_UI_SHOTS=<输出目录> 后渲染三栏
// 布局的几种典型状态。全部数据造在 t.TempDir() 里。

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
	myui "github.com/egoist/mygo/ui"
)

// seedDescribed 写一个带 description 的技能。
func seedDescribed(t *testing.T, cfg *store.Config, name, desc string, meta *store.Meta) {
	t.Helper()
	seedSkill(t, cfg, name, meta)
	body := "---\nname: " + name + "\ndescription: " + desc + "\n---\n\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(cfg.SkillPath(name), "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// seedDesignApp 造一个有各种状态技能的应用：启用、本地来源、无元数据、
// 停用、回收站，外加第二个技能目录。
func seedDesignApp(t *testing.T) *App {
	t.Helper()
	gh := func(name string) *store.Meta {
		return &store.Meta{ID: name, DisplayName: name,
			Source:      store.Source{Type: store.SourceGitHub, URL: "https://github.com/mattpocock/skills.git", Ref: "main", SubPath: "skills/" + name, Commit: "3f9c2a1b7d"},
			PluginName:  "mattpocock-skills",
			InstalledAt: "2026-06-21T04:42:05Z", UpdatedAt: "2026-09-06T13:34:08Z",
			ContentHash: "sha256:9b1f0c8e2d4a6b7c8d9e0f1a2b3c"}
	}
	local := &store.Meta{ID: "pdf-tools", Source: store.Source{Type: store.SourceLocal, Path: `D:\skills\pdf-tools`}}

	a, cfg := newTestApp(t)
	seedDescribed(t, cfg, "yixin-api", "调用易信开放平台 API：发消息、查群成员、管理机器人。需要 YIXIN_TOKEN 环境变量。", gh("yixin-api"))
	seedDescribed(t, cfg, "tdd", "以测试驱动的方式实现功能：先写失败的测试，再写最少的代码让它通过，最后重构。", gh("tdd"))
	seedDescribed(t, cfg, "pdf-tools", "读取、合并、拆分 PDF，提取表格与图片。", local)
	seedDescribed(t, cfg, "git-guardrails", "在执行 git push --force、reset --hard 等危险命令前拦截并确认。", gh("git-guardrails"))
	seedDescribed(t, cfg, "write-a-prd", "和用户一起把模糊的想法整理成产品需求文档。", gh("write-a-prd"))
	seedSkill(t, cfg, "scratch-notes", nil)
	seedDescribed(t, cfg, "old-deploy", "旧版部署脚本，已被 deploy-v2 取代。", nil)
	if _, err := cfg.Disable("old-deploy"); err != nil {
		t.Fatal(err)
	}
	seedDescribed(t, cfg, "legacy-lint", "旧的 lint 规则集合。", nil)
	if _, err := cfg.Remove("legacy-lint"); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(filepath.Dir(cfg.Root), "work", ".claude", "skills")
	if err := os.MkdirAll(filepath.Join(other, "review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := a.lib.AddRoot(other); err != nil {
		t.Fatal(err)
	}
	a.updates["tdd"] = true
	return a
}

// TestRenderDesignShots 渲染三栏布局的典型状态，每个状态深色、浅色各一张。
func TestRenderDesignShots(t *testing.T) {
	out := os.Getenv("KURA_UI_SHOTS")
	if out == "" {
		t.Skip("未设置 KURA_UI_SHOTS，跳过设计稿截图")
	}
	a := seedDesignApp(t)
	// render 渲染一个状态，深色、浅色各一张。post 在两帧稳定后对 Tester 做
	// 指针/键盘操作（悬停、聚焦），再补一帧才截图。
	render := func(name string, w, h int, prep func(), post ...func(tt *myui.Tester)) {
		if prep != nil {
			prep()
		}
		for _, dark := range []bool{true, false} {
			tt := newTester(a, w, h)
			tt.SetPreferences(myui.Preferences{ReduceMotion: true})
			tt.SetDark(dark)
			a.settle()
			tt.Frame()
			a.settle()
			tt.Frame()
			for _, f := range post {
				f(tt)
			}
			tt.Frame()
			mode := "light"
			if dark {
				mode = "dark"
			}
			saveShot(t, out, name+"-"+mode, tt)
		}
	}
	render("1-grid", 1280, 800, nil)
	render("2-selected", 1280, 800, func() { a.selName = "yixin-api" })
	render("3-narrow", 960, 640, func() { a.selName = "tdd" })
	render("4-trash", 1280, 800, func() { a.selName = "legacy-lint"; a.filter = filterTrashed })
	render("5-install-github", 1280, 800, func() {
		a.selName, a.filter = "", filterAll
		a.install = installState{open: true, repo: "https://github.com/mattpocock/skills/tree/main/skills/grill-me"}
	})
	render("6-install-local", 1280, 800, func() {
		a.install = installState{open: true, source: installLocal, local: `D:\skills\pdf-tools`,
			err: "store: 已存在同名技能: pdf-tools"}
	})
	// 悬停：指针停在 tdd 的卡片上（未选中的卡片亮一档），以及停在 pdf-tools 的开关上。
	render("7-hover-card", 1280, 800, func() { a.selName, a.filter, a.install = "yixin-api", filterAll, installState{} }, func(tt *myui.Tester) {
		r, _ := tt.Find("tdd")
		tt.Move(r.X+40, r.Y+60)
	})
	render("8-hover-switch", 1280, 800, func() { a.selName, a.filter, a.install = "", filterAll, installState{} }, func(tt *myui.Tester) {
		r, _ := tt.Find("pdf-tools 开关")
		tt.Move(r.X+r.W/2, r.Y+r.H/2)
	})
	// 键盘聚焦：Tab 到第一个开关上，出现聚焦环。
	render("9-focus-switch", 1280, 800, func() { a.selName, a.filter, a.install = "", filterAll, installState{} }, func(tt *myui.Tester) {
		for i := 0; i < 40 && !(tt.Focused("git-guardrails 开关") && !tt.Focused("pdf-tools 开关")); i++ {
			tt.Key(0, myui.KeyTab)
		}
	})
	// 译文 + 全文：译文模式下打开全文页。译文来自假翻译（前缀「[译]」），只为看版式。
	render("10-fulltext-translated", 1280, 800, func() {
		f := &fakeTranslator{}
		a.translate = f.translate
		a.settings = trReadySettings()
		a.selName, a.filter, a.install = "yixin-api", filterAll, installState{}
	}, func(tt *myui.Tester) {
		// detClickTab 完整跑两轮（全文先读盘、再翻译），截图才是译文而不是「翻译全文中…」。
		detClickTab(t, a, tt, "译文")
		detClickTab(t, a, tt, detailTabFull)
	})
}
