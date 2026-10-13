package ui

// 设计稿截图（仅本地迭代用）：设置 KURA_UI_SHOTS=<输出目录> 后渲染三栏
// 布局的几种典型状态。全部数据造在 t.TempDir() 里。

import (
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
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

// TestRenderDesignShots 渲染三栏布局的典型状态（默认配色方案）。
func TestRenderDesignShots(t *testing.T) {
	out := os.Getenv("KURA_UI_SHOTS")
	if out == "" {
		t.Skip("未设置 KURA_UI_SHOTS，跳过设计稿截图")
	}
	a := seedDesignApp(t)
	render := func(name string, w, h int, dark bool, prep func()) {
		tt := newTester(a, w, h)
		tt.SetDark(dark)
		a.settle()
		tt.Frame()
		a.settle()
		tt.Frame()
		if prep != nil {
			prep()
			a.settle()
			tt.Frame()
			tt.Frame()
		}
		saveShot(t, out, name, tt)
	}
	render("1-grid", 1280, 800, false, nil)
	render("2-selected", 1280, 800, false, func() { a.selName = "yixin-api" })
	render("3-narrow", 960, 640, false, func() { a.selName = "tdd" })
	render("4-trash", 1280, 800, false, func() { a.selName = "legacy-lint"; a.filter = filterTrashed })
	render("5-install-github", 1280, 800, false, func() {
		a.selName, a.filter = "", filterAll
		a.install = installState{open: true, repo: "https://github.com/mattpocock/skills/tree/main/skills/grill-me"}
	})
	render("6-install-local", 1280, 800, false, func() {
		a.install = installState{open: true, source: installLocal, local: `D:\skills\pdf-tools`,
			err: "store: 已存在同名技能: pdf-tools"}
	})
	a.install = installState{}
	a.filter, a.selName = filterAll, "write-a-prd"
	render("7-dark", 1280, 800, true, nil)
}

// TestRenderSchemeShots 把每套候选配色方案在浅色与深色下各渲染一张
// （选中一张卡片的状态，强调色出现得最多），并拼成一张对比图
// compare.png：每列一套方案（森林绿、陶土橙、石墨），上行浅色、下行深色。
func TestRenderSchemeShots(t *testing.T) {
	out := os.Getenv("KURA_UI_SHOTS")
	if out == "" {
		t.Skip("未设置 KURA_UI_SHOTS，跳过配色截图")
	}
	const gap = 16
	schemes := []*scheme{schemeForest, schemeClay, schemeGraphite}
	var halves [2][]*image.RGBA
	for _, s := range schemes {
		a := seedDesignApp(t)
		a.scheme = s
		a.selName = "yixin-api"
		for row, dark := range []bool{false, true} {
			tt := newTester(a, 1280, 800)
			tt.SetDark(dark)
			a.settle()
			tt.Frame()
			a.settle()
			tt.Frame()
			mode := "light"
			if dark {
				mode = "dark"
			}
			saveShot(t, out, s.name+"-"+mode, tt)
			halves[row] = append(halves[row], halve(tt.Image()))
		}
	}
	w, h := halves[0][0].Bounds().Dx(), halves[0][0].Bounds().Dy()
	sheet := image.NewRGBA(image.Rect(0, 0, len(schemes)*(w+gap)+gap, 2*(h+gap)+gap))
	draw.Draw(sheet, sheet.Bounds(), image.NewUniform(color.RGBA{0xc8, 0xc8, 0xc8, 0xff}), image.Point{}, draw.Src)
	for row := range halves {
		for col, img := range halves[row] {
			at := image.Pt(gap+col*(w+gap), gap+row*(h+gap))
			draw.Draw(sheet, image.Rectangle{Min: at, Max: at.Add(img.Bounds().Size())}, img, img.Bounds().Min, draw.Src)
		}
	}
	saveImage(t, out, "compare", sheet)
}

// halve 把图片按 2×2 平均缩小一半（盒式滤波），用来拼对比图。
func halve(src *image.RGBA) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx()/2, b.Dy()/2
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var r, g, bl, a int
			for dy := 0; dy < 2; dy++ {
				for dx := 0; dx < 2; dx++ {
					c := src.RGBAAt(b.Min.X+2*x+dx, b.Min.Y+2*y+dy)
					r, g, bl, a = r+int(c.R), g+int(c.G), bl+int(c.B), a+int(c.A)
				}
			}
			dst.SetRGBA(x, y, color.RGBA{uint8(r / 4), uint8(g / 4), uint8(bl / 4), uint8(a / 4)})
		}
	}
	return dst
}
