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

func TestRenderDesignShots(t *testing.T) {
	out := os.Getenv("KURA_UI_SHOTS")
	if out == "" {
		t.Skip("未设置 KURA_UI_SHOTS，跳过设计稿截图")
	}
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
	// 第二个目录。
	other := filepath.Join(filepath.Dir(cfg.Root), "work", ".claude", "skills")
	if err := os.MkdirAll(filepath.Join(other, "review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := a.lib.AddRoot(other); err != nil {
		t.Fatal(err)
	}

	render := func(name string, w, h int, prep func(tt *myui.Tester)) {
		tt := newTester(a, w, h)
		a.settle()
		tt.Frame()
		a.settle()
		tt.Frame()
		if prep != nil {
			prep(tt)
			a.settle()
			tt.Frame()
			tt.Frame()
		}
		saveShot(t, out, name, tt)
	}
	a.updates["tdd"] = true
	render("design-1-grid", 1280, 800, nil)
	render("design-2-selected", 1280, 800, func(tt *myui.Tester) { a.selName = "yixin-api" })
	render("design-3-narrow", 960, 640, func(tt *myui.Tester) { a.selName = "tdd" })
	render("design-5-install-github", 1280, 800, func(tt *myui.Tester) {
		a.selName, a.filter = "", filterAll
		a.install = installState{open: true, repo: "https://github.com/mattpocock/skills/tree/main/skills/grill-me"}
	})
	render("design-6-install-local", 1280, 800, func(tt *myui.Tester) {
		a.install = installState{open: true, source: installLocal, local: `D:\skills\pdf-tools`,
			err: "store: 已存在同名技能: pdf-tools"}
	})
	a.install = installState{}
	render("design-4-trash", 1280, 800, func(tt *myui.Tester) { a.selName = "legacy-lint"; a.filter = filterTrashed })
}
