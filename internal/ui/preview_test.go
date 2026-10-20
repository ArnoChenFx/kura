package ui

// 检查更新与评审面板的 Tester 测试（里程碑 4）。技能 root 全部在 t.TempDir()
// 下，绝不触碰真实的 ~/.agents/skills。预览来自 local 来源（PreviewUpdate
// 对 local 直接只读来源目录，无网络、无 git 依赖）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/ArnoChenFx/kura/locales"
)

// seedWithLocalSource 建一个已安装技能（github 元数据仅用于展示字段）+
// 一个内容不同的本地来源目录，返回来源目录路径。
func seedWithLocalSource(t *testing.T, cfg *store.Config, name, installed, upstream string) string {
	t.Helper()
	// 本地来源目录（上游内容）。
	src := filepath.Join(t.TempDir(), "src-"+name)
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte(upstream), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "upstream-only.md"), []byte("new file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 已安装内容（与上游不同 → 预览必有变化）。
	seedSkill(t, cfg, name, &store.Meta{
		ID:          name,
		DisplayName: name,
		Source:      store.Source{Type: store.SourceLocal, Path: src},
		ContentHash: "sha256:placeholder",
	})
	if err := os.WriteFile(filepath.Join(cfg.SkillPath(name), "SKILL.md"), []byte(installed), 0o644); err != nil {
		t.Fatal(err)
	}
	return src
}

// TestReviewPanelShowsFilesAndApplies 验证：检查更新 → 评审面板逐行显示
// 变更文件 → 应用更新 → 内容替换、列表刷新、面板关闭。
func TestReviewPanelShowsFilesAndApplies(t *testing.T) {
	a, cfg := newTestApp(t)
	seedWithLocalSource(t, cfg, "rev",
		"---\nname: rev\n---\n\ninstalled\n", // 已安装
		"---\nname: rev\n---\n\nupstream\n")  // 上游

	tt := newTester(a, 980, 640)
	a.settle()
	tt.Frame()
	a.settle()
	tt.Frame()

	// 选中技能，点「检查更新」。
	if err := tt.Click("rev"); err != nil {
		t.Fatal(err)
	}
	a.settle()
	tt.Frame()
	if err := tt.Click("详情:检查更新"); err != nil {
		t.Fatalf("详情面板应有检查更新按钮: %v", err)
	}
	a.waitBusy()
	tt.Frame()

	// 评审面板打开：显示标题、每个变更文件一行、来源与 ±行数。
	if !a.review.open {
		t.Fatal("检查后评审面板应打开")
	}
	if !tt.HasText("更新 rev") {
		t.Errorf("面板应显示标题，文本 %v", tt.Texts())
	}
	if !tt.HasText("SKILL.md") || !tt.HasText("upstream-only.md") {
		t.Errorf("面板应列出变更文件，文本 %v", tt.Texts())
	}
	if !tt.HasText("修改") || !tt.HasText("新增") {
		t.Errorf("面板应显示差异状态标签，文本 %v", tt.Texts())
	}
	if !tt.HasText("来源: local") {
		t.Errorf("面板应显示来源，文本 %v", tt.Texts())
	}
	// +1/-1 行数标签（SKILL.md）。
	if !tt.HasText("+1/-1") {
		t.Errorf("面板应显示行数统计，文本 %v", tt.Texts())
	}

	// 应用更新：后台 Update 完成后内容替换、面板关闭。
	if err := tt.Click("应用更新"); err != nil {
		t.Fatal(err)
	}
	a.waitBusy()
	tt.Frame()
	if a.review.open {
		t.Error("应用后评审面板应关闭")
	}
	got, err := os.ReadFile(filepath.Join(cfg.SkillPath("rev"), "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "---\nname: rev\n---\n\nupstream\n" {
		t.Errorf("应用后 SKILL.md 应为上游内容, got %q", got)
	}
	if _, err := os.Stat(filepath.Join(cfg.SkillPath("rev"), "upstream-only.md")); err != nil {
		t.Error("应用后应新增 upstream-only.md")
	}
	// 状态行报告成功；快照已写入。
	if !tt.HasText("已更新 1 个技能") {
		t.Errorf("状态行应显示更新成功，文本 %v", tt.Texts())
	}
}

// TestReviewPanelNoChanges 验证：上游一致时面板显示「无需更新」且不能应用。
func TestReviewPanelNoChanges(t *testing.T) {
	a, cfg := newTestApp(t)
	src := filepath.Join(t.TempDir(), "src-same")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("---\nname: same\n---\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedSkill(t, cfg, "same", &store.Meta{
		ID:          "same",
		DisplayName: "same",
		Source:      store.Source{Type: store.SourceLocal, Path: src},
	})
	// 已安装内容与来源完全一致 → 预览应报告无变化。
	if err := os.WriteFile(filepath.Join(cfg.SkillPath("same"), "SKILL.md"), []byte("---\nname: same\n---\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tt := newTester(a, 980, 640)
	a.settle()
	tt.Frame()
	a.settle()
	tt.Frame()

	if err := tt.Click("same"); err != nil {
		t.Fatal(err)
	}
	a.settle()
	tt.Frame()
	if err := tt.Click("详情:检查更新"); err != nil {
		t.Fatal(err)
	}
	a.waitBusy()
	tt.Frame()

	if !a.review.open || a.review.preview == nil {
		t.Fatal("评审面板应打开且带预览")
	}
	if a.review.preview.HasChanges() {
		t.Fatalf("不应有变化: %+v", a.review.preview.Files)
	}
	if !tt.HasText("与上游一致") {
		t.Errorf("面板应显示无变化提示，文本 %v", tt.Texts())
	}
	// 取消关闭面板，内容不动。
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.review.open {
		t.Error("取消后面板应关闭")
	}
	if got, _ := os.ReadFile(filepath.Join(cfg.SkillPath("same"), "SKILL.md")); string(got) != "---\nname: same\n---\n\nbody\n" {
		t.Errorf("取消后内容不应变化, got %q", got)
	}
}

// TestReviewPanelShowsError 验证：预览失败时错误进状态行，不打开面板。
func TestReviewPanelShowsError(t *testing.T) {
	a, cfg := newTestApp(t)
	// 指向不存在来源的 local 元数据 → PreviewUpdate 报错。
	seedSkill(t, cfg, "broken", &store.Meta{
		ID:          "broken",
		DisplayName: "broken",
		Source:      store.Source{Type: store.SourceLocal, Path: filepath.Join(t.TempDir(), "no-such-src")},
	})

	tt := newTester(a, 980, 640)
	a.settle()
	tt.Frame()
	a.settle()
	tt.Frame()

	if err := tt.Click("broken"); err != nil {
		t.Fatal(err)
	}
	a.settle()
	tt.Frame()
	if err := tt.Click("详情:检查更新"); err != nil {
		t.Fatal(err)
	}
	a.waitBusy()
	tt.Frame()

	if a.review.open {
		t.Error("失败时评审面板不应打开")
	}
	if !tt.HasText("检查 broken 失败") {
		t.Errorf("状态行应显示失败，文本 %v", tt.Texts())
	}
}

// TestCheckAllToolbarAction 验证：工具栏「全部检查」汇总面板：有变化的技能
// 列文件、无变化的标已是最新、失败标错误；应用后全部更新。
func TestCheckAllToolbarAction(t *testing.T) {
	a, cfg := newTestApp(t)
	// fresh：有更新。
	seedWithLocalSource(t, cfg, "fresh",
		"---\nname: fresh\n---\n\nold\n",
		"---\nname: fresh\n---\n\nnew\n")
	// uptodate：与来源一致。
	src2 := filepath.Join(t.TempDir(), "src-uptodate")
	if err := os.MkdirAll(src2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src2, "SKILL.md"), []byte("---\nname: uptodate\n---\n\nsame\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedSkill(t, cfg, "uptodate", &store.Meta{
		ID: "uptodate", DisplayName: "uptodate",
		Source: store.Source{Type: store.SourceLocal, Path: src2},
	})
	// 已安装内容与来源一致 → 汇总里应标「已是最新」。
	if err := os.WriteFile(filepath.Join(cfg.SkillPath("uptodate"), "SKILL.md"), []byte("---\nname: uptodate\n---\n\nsame\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// failing：来源目录消失 → 检查失败。
	seedSkill(t, cfg, "failing", &store.Meta{
		ID: "failing", DisplayName: "failing",
		Source: store.Source{Type: store.SourceLocal, Path: filepath.Join(t.TempDir(), "vanished")},
	})

	tt := newTester(a, 980, 640)
	a.settle()
	tt.Frame()
	a.settle()
	tt.Frame()

	if err := tt.Click("全部检查"); err != nil {
		t.Fatal(err)
	}
	a.waitBusy()
	tt.Frame()

	if !a.review.open || !a.review.all {
		t.Fatal("全部检查后汇总面板应打开")
	}
	if !tt.HasText("全部检查") {
		t.Errorf("面板应显示标题，文本 %v", tt.Texts())
	}
	// fresh 有变化（SKILL.md + upstream-only.md），uptodate 已是最新，failing 失败。
	if !tt.HasText("failing") || !tt.HasText("检查失败") {
		t.Errorf("汇总应标记失败技能，文本 %v", tt.Texts())
	}
	if !tt.HasText("已是最新") {
		t.Errorf("汇总应标记无变化技能，文本 %v", tt.Texts())
	}
	if !tt.HasText("upstream-only.md") {
		t.Errorf("汇总应列出变更文件，文本 %v", tt.Texts())
	}

	// 夹具的占位哈希被识别为本地修改，需要显式勾选后才更新。
	if err := tt.Click(locales.UiReviewSelectSkill("fresh")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click(locales.UiReviewApplySelected(1)); err != nil {
		t.Fatal(err)
	}
	a.waitBusy()
	tt.Frame()
	if !a.review.open {
		t.Error("应用后汇总面板应保留逐项结果")
	}
	got, err := os.ReadFile(filepath.Join(cfg.SkillPath("fresh"), "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "---\nname: fresh\n---\n\nnew\n" {
		t.Errorf("fresh 应已更新, got %q", got)
	}
}

// TestCheckAllDisabledButtonWhileChecking 验证：检查进行中工具栏按钮禁用，
// 防止并发检查互相覆盖。
func TestCheckAllDisabledButtonWhileChecking(t *testing.T) {
	a, cfg := newTestApp(t)
	seedWithLocalSource(t, cfg, "one",
		"---\nname: one\n---\n\nold\n",
		"---\nname: one\n---\n\nnew\n")

	tt := newTester(a, 980, 640)
	a.settle()
	tt.Frame()
	a.settle()
	tt.Frame()

	// 模拟检查进行中：checkingAll 置位时再次点击应被拒绝。
	a.checkingAll = true
	before := len(a.pending)
	a.CheckAllAsync()
	a.CheckAsync("one")
	if len(a.pending) != before {
		t.Error("检查进行中的重复发起应被忽略")
	}
	a.checkingAll = false

	// 正常路径：点工具栏按钮，面板打开。
	if err := tt.Click("全部检查"); err != nil {
		t.Fatal(err)
	}
	a.waitBusy()
	tt.Frame()
	if !a.review.open || !a.review.all {
		t.Fatal("全部检查后汇总面板应打开")
	}
}

// TestApplyAllPartialFailure 验证：批量应用时个别技能失败不影响其他技能，
// 状态行报告「几个成功、几个失败」，失败的技能仍标「有更新」。
func TestApplyAllPartialFailure(t *testing.T) {
	a, cfg := newTestApp(t)
	seedWithLocalSource(t, cfg, "good",
		"---\nname: good\n---\n\nold\n", "---\nname: good\n---\n\nnew\n")
	badSrc := seedWithLocalSource(t, cfg, "bad",
		"---\nname: bad\n---\n\nold\n", "---\nname: bad\n---\n\nnew\n")

	tt := newTester(a, 980, 640)
	a.settle()
	tt.Frame()
	a.settle()
	tt.Frame()

	if err := tt.Click("全部检查"); err != nil {
		t.Fatal(err)
	}
	a.waitBusy()
	tt.Frame()
	if !a.updates["good"] || !a.updates["bad"] {
		t.Fatalf("检查后两个技能都应标有更新: %v", a.updates)
	}
	// 检查之后、应用之前，bad 的来源消失。
	if err := os.RemoveAll(badSrc); err != nil {
		t.Fatal(err)
	}
	// 夹具用占位哈希，两个条目都需要显式勾选后才可更新。
	for i := range a.review.entries {
		a.review.entries[i].selected = true
	}
	tt.Frame()
	if err := tt.Click(locales.UiReviewApplySelected(2)); err != nil {
		t.Fatal(err)
	}
	a.waitBusy()
	tt.Frame()

	if got, _ := os.ReadFile(filepath.Join(cfg.SkillPath("good"), "SKILL.md")); string(got) != "---\nname: good\n---\n\nnew\n" {
		t.Errorf("good 应已更新, got %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(cfg.SkillPath("bad"), "SKILL.md")); string(got) != "---\nname: bad\n---\n\nold\n" {
		t.Errorf("bad 应保持原样, got %q", got)
	}
	if a.updates["good"] || !a.updates["bad"] {
		t.Errorf("只应清掉成功的标记: %v", a.updates)
	}
	if !a.statusErr || !strings.Contains(a.status, "已更新 1 个技能，1 个失败") {
		t.Errorf("状态行应报告部分失败, got %q (err=%v)", a.status, a.statusErr)
	}
	if !a.review.open || !batchReviewEntry(t, a, "good").applied || batchReviewEntry(t, a, "bad").applyErr == "" {
		t.Error("汇总面板应保留逐项成功、失败结果")
	}
}

// TestReviewPanelLocalModifiedAndBasis 验证：上游没变但本地被改过时，面板写明
// 判定依据并提醒本地改动；全部检查汇总里也标出「本地有改动」。
func TestReviewPanelLocalModifiedAndBasis(t *testing.T) {
	a, cfg := newTestApp(t)
	p := &store.UpdatePreview{
		ID: "mod", SourceType: store.SourceGitHub, Commit: "0123456789abcdef",
		Basis: store.BasisCommit, LocalModified: true,
		OldHash: "sha256:aaaaaaaaaaaaaaaa", NewHash: "sha256:bbbbbbbbbbbbbbbb",
		Files: []store.FileDiff{},
	}
	a.review = reviewState{open: true, cfg: cfg, skill: "mod", preview: p}
	tt := newTester(a, 980, 640)
	a.settle()
	tt.Frame()
	tt.Frame()
	if !tt.HasText("与上游一致，无需更新") {
		t.Errorf("应显示与上游一致，文本 %v", tt.Texts())
	}
	if !tt.HasText("已安装的内容与安装时不一致（本地改过）") {
		t.Errorf("应提醒本地改过，文本 %v", tt.Texts())
	}
	meta := previewMetaText(p)
	if !strings.Contains(meta, "上游 commit 未变") || !strings.Contains(meta, "旧 ") {
		t.Errorf("元信息应写明依据并在本地改过时显示新旧哈希: %q", meta)
	}
	// 本地没改过：不显示哈希。
	p.LocalModified = false
	if m := previewMetaText(p); strings.Contains(m, "旧 ") {
		t.Errorf("上游没变且本地未改时不应显示哈希: %q", m)
	}

	a.review = reviewState{open: true, cfg: cfg, all: true,
		entries: []skillReviewEntry{{skill: "mod", preview: &store.UpdatePreview{Basis: store.BasisTree, LocalModified: true, Files: []store.FileDiff{}}}}}
	tt.Frame()
	tt.Frame()
	if !tt.HasText("已是最新") || !tt.HasText("本地有改动") {
		t.Errorf("汇总应标已是最新与本地有改动，文本 %v", tt.Texts())
	}
}
