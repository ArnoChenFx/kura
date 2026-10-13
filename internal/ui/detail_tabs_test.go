// 详情栏「概览/全文」tab 与来源链接的界面测试。全部数据造在 t.TempDir()
// 里（见 newTestApp），绝不触碰真实的 ~/.agents/skills。detTest 前缀避免
// 与既有测试重名。
package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
	myui "github.com/egoist/mygo/ui"
)

// detSeedWithMD 造一个带指定 SKILL.md 正文的技能（无元数据）。
func detSeedWithMD(t *testing.T, cfg *store.Config, name, body string) {
	t.Helper()
	dir := cfg.SkillPath(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// detClickTab 点击详情栏的 tab 标签并让界面跟上：完整周期处理点击、
// 回放后台读盘结果，再渲染一帧。
func detClickTab(t *testing.T, a *App, tt *myui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatalf("点不到 tab %q: %v；文本 %v", label, err, tt.Texts())
	}
	a.settle()
	tt.Frame()
	a.settle()
	tt.Frame()
}

// detHasText 报告上一帧里是否出现包含 sub 的文本。
func detHasText(tt *myui.Tester, sub string) bool {
	for _, s := range tt.Texts() {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// TestDetailTabResetsOnSkillSwitch 验证：切到「全文」后换选另一个技能，
// tab 应回到「概览」，不残留上一个技能的全文。
func TestDetailTabResetsOnSkillSwitch(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "alpha", nil)
	seedSkill(t, cfg, "beta", nil)
	tt := showApp(a, 1280, 800)

	click(t, a, tt, "alpha")
	detClickTab(t, a, tt, detailTabFull)
	if a.detailTab != 1 {
		t.Fatalf("点「全文」后 detailTab = %d，想要 1", a.detailTab)
	}
	// 标题正文被渲染成单独的文本元素（frontmatter 已剥掉）。
	if !detHasText(tt, "alpha") {
		t.Fatalf("全文页应渲染出 alpha 的 SKILL.md，文本 %v", tt.Texts())
	}

	// 换选 beta：tab 应回概览，元数据组（beta 的）应回来，且不残留
	// alpha 的全文正文（概览分支不会渲染 renderMarkdown 的内容）。
	click(t, a, tt, "beta")
	if a.detailTab != 0 {
		t.Errorf("换技能后 detailTab = %d，想要 0（概览）", a.detailTab)
	}
	if !detHasText(tt, "没有 .kura-meta.json") {
		t.Errorf("概览页应有 beta 的元数据组，文本 %v", tt.Texts())
	}
	for _, s := range tt.Texts() {
		if strings.Contains(s, "第一段") {
			t.Errorf("概览页不应残留全文正文，文本 %v", tt.Texts())
		}
	}
}

// TestDetailFullTextMissing 验证：没有 SKILL.md 的技能（含回收站里的）
// 切到全文页显示明确原因，不空白、不 panic。
func TestDetailFullTextMissing(t *testing.T) {
	a, cfg := newTestApp(t)
	hollow := cfg.SkillPath("hollow")
	if err := os.MkdirAll(hollow, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hollow, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	tt := showApp(a, 1280, 800)
	click(t, a, tt, "hollow")
	detClickTab(t, a, tt, detailTabFull)
	a.settle()
	refresh(a, tt)
	if !detHasText(tt, "没有 SKILL.md 可显示") {
		t.Errorf("缺 SKILL.md 时应说明原因，文本 %v", tt.Texts())
	}

	// 回收站里的技能同样给出提示：回收站卡片只在「回收站」筛选下出现，
	// Remove 后重扫才能刷出卡片与筛选计数。
	seedSkill(t, cfg, "binned", nil)
	if _, err := cfg.Remove("binned"); err != nil {
		t.Fatal(err)
	}
	a.ReloadAsync()
	a.settle()
	refresh(a, tt)
	click(t, a, tt, "回收站 1")
	click(t, a, tt, "binned")
	detClickTab(t, a, tt, detailTabFull)
	a.settle()
	refresh(a, tt)
	if !detHasText(tt, "没有 SKILL.md 可显示") {
		t.Errorf("回收站技能的全文页应给出提示，文本 %v", tt.Texts())
	}
}

// TestDetailFullTextRendersMarkdown 验证：全文页后台读出 SKILL.md 并用
// renderMarkdown 渲染（标题、列表、代码块、正文都在帧里），tab 来回切
// 走缓存不再重复读盘。
func TestDetailFullTextRendersMarkdown(t *testing.T) {
	a, cfg := newTestApp(t)
	body := "---\nname: docned\n---\n\n# 全文标题\n\n第一段正文。\n\n- 第一项\n- 第二项\n\n```bash\nkura list\n```\n"
	detSeedWithMD(t, cfg, "docned", body)
	tt := showApp(a, 1280, 800)

	click(t, a, tt, "docned")
	detClickTab(t, a, tt, detailTabFull)
	a.settle()
	refresh(a, tt)
	for _, want := range []string{"全文标题", "第一段正文。", "第一项", "kura list"} {
		if !detHasText(tt, want) {
			t.Errorf("全文页应渲染出 %q，文本 %v", want, tt.Texts())
		}
	}
	// frontmatter 不当正文渲染。
	if detHasText(tt, "name: docned") {
		t.Errorf("frontmatter 不应出现在全文里，文本 %v", tt.Texts())
	}

	// 回到概览再进全文：命中缓存，内容仍在（同步 spawn 下第二次读盘
	// 不会发生，缓存由 a.skillDocs 证明）。
	if len(a.skillDocs) != 1 {
		t.Fatalf("全文应有 1 份缓存，得到 %v", a.skillDocs)
	}
	detClickTab(t, a, tt, detailTabOverview)
	if a.detailTab != 0 || !detHasText(tt, "位置") {
		t.Errorf("应切回概览页，文本 %v", tt.Texts())
	}
	detClickTab(t, a, tt, detailTabFull)
	for _, want := range []string{"全文标题", "kura list"} {
		if !detHasText(tt, want) {
			t.Errorf("再次进全文页应命中缓存渲染出 %q，文本 %v", want, tt.Texts())
		}
	}
}

// TestDetailSourceLinkForGitHub 验证：GitHub 来源的「来源」行渲染出
// URL 文本（Link 元素携带它）；本地来源保持普通文本。headless 下无法
// 验证点击真的打开浏览器，只验证链接文本存在（见报告）。
func TestDetailSourceLinkForGitHub(t *testing.T) {
	a, cfg := newTestApp(t)
	const ghURL = "https://github.com/me/linked.git"
	seedSkill(t, cfg, "gh", &store.Meta{
		ID: "gh", DisplayName: "gh",
		Source:      store.Source{Type: store.SourceGitHub, URL: ghURL},
		ContentHash: "sha256:deadbeef",
	})
	tt := showApp(a, 1280, 800)

	click(t, a, tt, "gh")
	if !tt.HasText(ghURL) {
		t.Errorf("GitHub 来源应渲染出 URL 文本 %q，文本 %v", ghURL, tt.Texts())
	}
	// 链接 URL 只出现一次：本测试里卡片底行显示 owner/repo（me/linked），
	// 不会重复完整 URL，多出来说明渲染重复。
	if got := countText(tt, ghURL); got != 1 {
		t.Errorf("完整 URL 应只出现 1 次（链接），得到 %d", got)
	}

	// 本地来源：URL 是路径，保持普通文本，同样能显示出来。
	a2, cfg2 := newTestApp(t)
	src := filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	seedSkill(t, cfg2, "loc", &store.Meta{
		ID: "loc", DisplayName: "loc",
		Source:      store.Source{Type: store.SourceLocal, Path: src},
		ContentHash: "sha256:beefdead",
	})
	tt2 := showApp(a2, 1280, 800)
	click(t, a2, tt2, "loc")
	if !tt2.HasText(src) {
		t.Errorf("本地来源应显示路径 %q，文本 %v", src, tt2.Texts())
	}
}
