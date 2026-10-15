// 全文页渲染的测试：kura 自己保留的两件事（去掉 YAML frontmatter、超大源码
// 截断）是纯函数，直接断言；渲染本身交给官方 ui/markdown，这里只断言「屏幕上
// 真的出现了应有的内容」（Texts()）、以及换文本后官方视图的状态正确重置。
//
// 全部输入都是内存里的字符串，不读盘、不联网。主题走 kuraScheme，与真实界面
// 同一条路径。
package ui

import (
	"github.com/ArnoChenFx/kura/locales"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ArnoChenFx/kura/internal/store"
	myui "github.com/egoist/mygo/ui"
)

// ftShow 用 Tester 渲染一段源码（走 App.viewMarkdown，与全文页同一条路径），
// 外层套 Scroll，与真实使用一致；走两帧让官方视图稳定。
func ftShow(t *testing.T, a *App, src string) *myui.Tester {
	t.Helper()
	tt := myui.NewTester(func(c *myui.Context) {
		sk := kuraScheme.apply(c, "dark")
		myui.Scroll(c).Fill().Background(sk.pal.canvas).Children(func() {
			myui.Column(c).FillWidth().Padding(c.Theme().Space(5)).Children(func() {
				a.viewMarkdown(c, src)
			})
		})
	}, 720, 900)
	tt.Frame()
	tt.Frame()
	return tt
}

// ftAll 把上一帧的全部文本拼起来，方便子串断言。
func ftAll(tt *myui.Tester) string { return strings.Join(tt.Texts(), "\n") }

// TestStripFrontmatter 覆盖 frontmatter 的识别规则：只有第一行恰好是 --- 才算，
// 且必须有闭合的 ---（或 ...）；没有闭合时按原样返回，绝不吞掉正文。
func TestStripFrontmatter(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"无 frontmatter", "# 标题\n\n正文", "# 标题\n\n正文"},
		{"闭合的 ---", "---\nname: demo\n---\n\n# 标题\n", "\n# 标题\n"},
		{"闭合的 ...", "---\nname: demo\n...\n\n# 标题\n", "\n# 标题\n"},
		{"只有 frontmatter", "---\nname: demo\n---", ""},
		{"闭合行后有空格与 CR", "---\r\nname: demo\r\n--- \r\n正文", "正文"},
		{"未闭合不吞正文", "---\n正文还在\n", "---\n正文还在\n"},
		{"没有换行的一行", "---", "---"},
		{"第一行不是 ---", " # 标题\n---\na: b\n---\n", " # 标题\n---\na: b\n---\n"},
		{"前导空行不算 frontmatter", "\n---\na: b\n---\n正文", "\n---\na: b\n---\n正文"},
		{"BOM 后的 frontmatter", "\uFEFF---\nname: demo\n---\n正文", "正文"},
		{"BOM 且无 frontmatter", "\uFEFF# 标题", "# 标题"},
		{"frontmatter 里的 --- 缩进不算闭合", "---\n  ---\na: b\n---\n正文", "正文"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripFrontmatter(tc.src); got != tc.want {
				t.Errorf("stripFrontmatter(%q) = %q，期望 %q", tc.src, got, tc.want)
			}
		})
	}
}

// TestCapSource 覆盖源码上限：不超限原样返回；超限在 UTF-8 字符边界上截断，
// 绝不切出半个字符。limit 是参数，小数值就能覆盖全部分支。
func TestCapSource(t *testing.T) {
	if got, cut := capSource("短文本", 64); got != "短文本" || cut {
		t.Errorf("未超限时不应截断：got=%q cut=%v", got, cut)
	}
	// 恰好到上限：不截断。
	exact := strings.Repeat("a", 64)
	if got, cut := capSource(exact, 64); cut || len(got) != 64 {
		t.Errorf("恰好到上限不应截断：len=%d cut=%v", len(got), cut)
	}
	// 超限（纯 ASCII）：截到上限。
	got, cut := capSource(exact+"more", 64)
	if !cut || len(got) != 64 {
		t.Errorf("超限应截到 64：len=%d cut=%v", len(got), cut)
	}
	// 上限处正好落在一个三字节汉字中间：往前收刀，结果必须是合法 UTF-8。
	multi := strings.Repeat("a", 63) + strings.Repeat("中", 4)
	got, cut = capSource(multi, 64)
	if !cut {
		t.Fatal("应报告截断")
	}
	if len(got) != 63 {
		t.Errorf("应在字符边界收刀到 63，得到 %d", len(got))
	}
	if !strings.HasSuffix(got, "a") || !utf8.ValidString(got) {
		t.Errorf("截断结果不是完整的 UTF-8：尾部 %q", got[len(got)-8:])
	}
	// 非法 limit：不截断（不 panic）。
	if got, cut := capSource("abcd", 0); got != "abcd" || cut {
		t.Errorf("limit<=0 时应原样返回：got=%q cut=%v", got, cut)
	}
}

// TestFullTextSource 覆盖渲染前的源码处理：frontmatter 剥掉；按真实的
// fullTextMaxSource 截断（用纯函数覆盖，不把 512 KB 真的渲染一遍）。
func TestFullTextSource(t *testing.T) {
	src := "---\nname: demo\n---\n\n# 标题\n"
	if got, cut := fullTextSource(src); got != "\n# 标题\n" || cut {
		t.Errorf("应剥掉 frontmatter 且不截断：got=%q cut=%v", got, cut)
	}
	big := strings.Repeat("a", fullTextMaxSource+10)
	got, cut := fullTextSource(big)
	if !cut || len(got) != fullTextMaxSource {
		t.Errorf("超过 %d 字节应截断到上限：len=%d cut=%v", fullTextMaxSource, len(got), cut)
	}
}

// TestFullTextRendersOfficialMarkdown 验证官方视图渲染出的内容真的在帧里：
// 标题、列表、代码块、GFM 表格与任务列表、链接文字都在，frontmatter 不在。
func TestFullTextRendersOfficialMarkdown(t *testing.T) {
	a, _ := newTestApp(t)
	src := "---\n" +
		"name: pdf-toolkit\n" +
		"description: 处理 PDF 文件\n" +
		"---\n\n" +
		"# 全文标题\n\n" +
		"正文第一段，含 **粗体** 与 `pdftotext`，详见 [官方文档](https://example.com/docs)。\n\n" +
		"- 第一项\n" +
		"- 第二项\n" +
		"  - 子项\n\n" +
		"1. 编号一\n" +
		"2. 编号二\n\n" +
		"- [x] 已完成\n" +
		"- [ ] 未完成\n\n" +
		"| 工具 | 用途 |\n| --- | --- |\n| pdftotext | 提取文字 |\n\n" +
		"```bash\nkura list --json\n```\n"
	tt := ftShow(t, a, src)
	all := ftAll(tt)
	for _, want := range []string{"全文标题", "正文第一段", "官方文档", "第一项", "子项", "编号一", "已完成", "未完成", "pdftotext", "提取文字", "kura list --json"} {
		if !strings.Contains(all, want) {
			t.Errorf("全文页应渲染出 %q，实际文本：\n%s", want, all)
		}
	}
	// frontmatter 是元数据，不当正文渲染。
	if strings.Contains(all, "name: pdf-toolkit") || strings.Contains(all, "description: 处理 PDF 文件") {
		t.Errorf("frontmatter 不应出现在全文里：\n%s", all)
	}
	// 行内 markdown 符号不该原样露出（粗体/代码/链接都被解析掉了）。
	if strings.Contains(all, "**粗体**") || strings.Contains(all, "`pdftotext`") ||
		strings.Contains(all, "](https://example.com/docs)") {
		t.Errorf("行内 markdown 语法未被解析：\n%s", all)
	}
}

// TestFullTextSwitchesText 验证同一个 Options、同一个视图位置换一份文本时，
// 官方视图自行重置：第二份内容出现，第一份的独有内容消失（不混排）。
func TestFullTextSwitchesText(t *testing.T) {
	a, _ := newTestApp(t)
	src := "---\nname: one\n---\n\n# 第一份标题\n\n第一份独有段落。\n"
	tt := myui.NewTester(func(c *myui.Context) {
		sk := kuraScheme.apply(c, "dark")
		myui.Scroll(c).Fill().Background(sk.pal.canvas).Children(func() {
			myui.Column(c).FillWidth().Padding(c.Theme().Space(5)).Children(func() {
				a.viewMarkdown(c, src)
			})
		})
	}, 720, 600)
	tt.Frame()
	tt.Frame()
	if !strings.Contains(ftAll(tt), "第一份独有段落") {
		t.Fatalf("第一份文本应已渲染：\n%s", ftAll(tt))
	}

	src = "---\nname: two\n---\n\n# 第二份标题\n\n第二份独有段落。\n"
	tt.Frame()
	tt.Frame()
	all := ftAll(tt)
	if !strings.Contains(all, "第二份独有段落") || !strings.Contains(all, "第二份标题") {
		t.Errorf("换文本后应渲染第二份：\n%s", all)
	}
	if strings.Contains(all, "第一份") {
		t.Errorf("换文本后不应残留第一份内容：\n%s", all)
	}
}

// TestFullTextShowsTranslatedSource 验证译文模式下全文页交给官方视图的是译文：
// 假翻译把整段变成「[译]」+原文，所以译文就绪后帧里应出现只可能来自译文的
// 内容；切回原文后它消失（来回切不串）。
//
// 注意断言里的 `[译]---`：假翻译在前加标记，首行就不再是 `---`，frontmatter
// 也就不再被识别（`stripFrontmatter` 只认首行恰好是 `---`）——这与旧的自写
// 渲染器行为一致，不是本次改动引入的。
func TestFullTextShowsTranslatedSource(t *testing.T) {
	a, cfg, f, tt := trApp(t, true) // 已配置的假翻译 + 中文界面
	body := "---\nname: alpha\ndescription: Reads PDF files.\n---\n\n# alpha\n\n独特的原文句子。\n"
	if err := os.WriteFile(filepath.Join(cfg.SkillPath("alpha"), "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	a.ReloadAsync()
	a.settle()
	tt.Frame()

	click(t, a, tt, "alpha")
	click(t, a, tt, "译文")
	a.settle()
	tt.Frame()
	detClickTab(t, a, tt, locales.UiDetailTabFull())
	a.settle()
	refresh(a, tt)
	if f.calls == 0 {
		t.Fatal("测试没有真的触发翻译，断言无意义")
	}
	all := ftAll(tt)
	if !strings.Contains(all, "[译]---") {
		t.Errorf("译文模式下全文页应渲染译文：\n%s", all)
	}

	// 切回原文：译文内容消失，原文回来（原文的 frontmatter 照常剥掉）。
	click(t, a, tt, "原文")
	a.settle()
	refresh(a, tt)
	all = ftAll(tt)
	if strings.Contains(all, "[译]---") {
		t.Errorf("原文模式不应再显示译文：\n%s", all)
	}
	if !strings.Contains(all, "独特的原文句子。") {
		t.Errorf("原文模式应显示原文：\n%s", all)
	}
	if strings.Contains(all, "name: alpha") {
		t.Errorf("原文模式的 frontmatter 应被剥掉：\n%s", all)
	}
}

// TestViewMarkdownShowsTruncatedNotice 验证「报告截断时 viewMarkdown 会追加提示」
// 这一段接线：直接调 viewMarkdownBody（viewMarkdown 里就是 fullTextSource 的结果
// 交给它），不再靠调小一个可变上限来触发。截断的判定本身由 TestCapSource /
// TestFullTextSource 覆盖，两段拼起来就是完整路径。
func TestViewMarkdownShowsTruncatedNotice(t *testing.T) {
	a, _ := newTestApp(t)
	show := func(truncated bool) string {
		tt := myui.NewTester(func(c *myui.Context) {
			kuraScheme.apply(c, "dark")
			myui.Scroll(c).Fill().Children(func() {
				myui.Column(c).FillWidth().Children(func() {
					a.viewMarkdownBody(c, "# 标题\n\n正文。", truncated)
				})
			})
		}, 600, 400)
		tt.Frame()
		tt.Frame()
		return ftAll(tt)
	}
	if got := show(false); strings.Contains(got, locales.UiMarkdownTruncated()) {
		t.Errorf("未截断时不应有提示：\n%s", got)
	}
	got := show(true)
	if !strings.Contains(got, "标题") || !strings.Contains(got, "正文。") {
		t.Errorf("截断提示不应取代正文：\n%s", got)
	}
	if !strings.Contains(got, locales.UiMarkdownTruncated()) {
		t.Errorf("报截断时应有提示 %q：\n%s", locales.UiMarkdownTruncated(), got)
	}
}

// TestFullTextThemeFollowsAppearance 验证主题跟外观走、但不每帧重建：同一外观
// 连续两帧用的是同一个 *Theme；换浅色/换回深色时 Theme 重建，正文色、代码块
// 底色、表格线都取自对应皮肤。
func TestFullTextThemeFollowsAppearance(t *testing.T) {
	a, _ := newTestApp(t)
	mode := store.ThemeDark
	tt := myui.NewTester(func(c *myui.Context) {
		kuraScheme.apply(c, mode)
		myui.Scroll(c).Fill().Children(func() {
			myui.Column(c).FillWidth().Children(func() {
				a.viewMarkdown(c, "正文 **粗体** 与 `code`\n\n```go\nfunc main() {}\n```\n")
			})
		})
	}, 600, 400)

	tt.Frame()
	first := a.md.Theme
	if first == nil {
		t.Fatal("渲染一帧后应有主题")
	}
	tt.Frame()
	if a.md.Theme != first {
		t.Errorf("同一外观连续两帧不应重建 Theme：%p → %p", first, a.md.Theme)
	}
	dark := *first
	if dark.Text != kuraScheme.dark.theme.Text {
		t.Errorf("深色下正文色 %v，想要深色皮肤的 %v", dark.Text, kuraScheme.dark.theme.Text)
	}

	mode = store.ThemeLight
	tt.Frame()
	lit := *a.md.Theme
	if a.md.Theme == first {
		t.Error("切浅色后 Theme 应重建，不能沿用深色那个")
	}
	if lit.Text != kuraScheme.light.theme.Text {
		t.Errorf("浅色下正文色 %v，想要浅色皮肤的 %v", lit.Text, kuraScheme.light.theme.Text)
	}
	if lit.TableBorder != kuraScheme.light.theme.Border {
		t.Errorf("浅色下表格线 %v，想要浅色皮肤的边框色 %v", lit.TableBorder, kuraScheme.light.theme.Border)
	}
	if lit.Text == dark.Text || lit.TableBorder == dark.TableBorder {
		t.Error("正文色与表格线都应随皮肤变")
	}
	if lit.CodeBlockBackground == dark.CodeBlockBackground {
		t.Error("浅色与深色的代码块底色不应相同")
	}

	mode = store.ThemeDark
	tt.Frame()
	back := a.md.Theme
	if back == nil || back.Text != dark.Text || back.TableBorder != dark.TableBorder {
		t.Errorf("切回深色后主题应回到深色：%+v", back)
	}
	first = back
	tt.Frame()
	if a.md.Theme != first {
		t.Errorf("仍是深色时不应重建 Theme：%p → %p", first, a.md.Theme)
	}
}
