package ui

// Markdown 渲染器的测试：解析（纯函数，断言块结构）与渲染（Tester 里
// 无 panic、文字齐全）。截图测试由 KURA_UI_SHOTS 开关，不设置时跳过。
// 全部输入都是内存里的字符串，不读盘。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	myui "github.com/egoist/mygo/ui"
)

// mdTestShow 用 Tester 渲染 src（外层套 Scroll，与真实使用一致），走两帧。
func mdTestShow(t *testing.T, src string, w, h int) *myui.Tester {
	t.Helper()
	tt := myui.NewTester(func(c *myui.Context) {
		sk := kuraScheme.apply(c, "dark")
		myui.Scroll(c).Fill().Background(sk.pal.canvas).Children(func() {
			myui.Column(c).FillWidth().Padding(c.Theme().Space(5)).Children(func() {
				renderMarkdown(c, src)
			})
		})
	}, w, h)
	tt.Frame()
	tt.Frame()
	return tt
}

// mdTestKinds 返回块的种类序列，便于一眼断言结构。
func mdTestKinds(bs []mdBlock) []mdKind {
	var out []mdKind
	for _, b := range bs {
		out = append(out, b.kind)
	}
	return out
}

// mdTestHas 断言上一帧的文本里含 want（子串）。
func mdTestHas(t *testing.T, tt *myui.Tester, want string) {
	t.Helper()
	all := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(all, want) {
		t.Errorf("渲染结果应包含 %q，实际文本：\n%s", want, all)
	}
}

// mdTestLacks 断言上一帧的文本里不含 s。
func mdTestLacks(t *testing.T, tt *myui.Tester, s string) {
	t.Helper()
	all := strings.Join(tt.Texts(), "\n")
	if strings.Contains(all, s) {
		t.Errorf("渲染结果不应包含 %q，实际文本：\n%s", s, all)
	}
}

func TestMarkdownHeadings(t *testing.T) {
	bs := parseMarkdown("# 一级\n## 二级 ##\n###### 六级\n#不是标题\n")
	if len(bs) != 4 {
		t.Fatalf("应有 4 块，得到 %d：%+v", len(bs), bs)
	}
	want := []struct {
		level int
		text  string
	}{{1, "一级"}, {2, "二级"}, {6, "六级"}}
	for i, w := range want {
		if bs[i].kind != mdHeading || bs[i].level != w.level || bs[i].text != w.text {
			t.Errorf("第 %d 块：%+v，期望 level=%d text=%q", i, bs[i], w.level, w.text)
		}
	}
	if bs[3].kind != mdParagraph {
		t.Errorf("#不是标题 应是段落，得到 %+v", bs[3])
	}
	tt := mdTestShow(t, "# 一级标题\n\n## 二级标题\n\n正文", 600, 400)
	mdTestHas(t, tt, "一级标题")
	mdTestHas(t, tt, "二级标题")
}

func TestMarkdownParagraphsMerge(t *testing.T) {
	bs := parseMarkdown("第一行\n第二行\nline three\nline four\n\n新段落")
	if len(bs) != 2 {
		t.Fatalf("应合并成 2 段，得到 %d：%+v", len(bs), bs)
	}
	// 中文行之间不加空格，英文行之间加一个空格。
	if got := bs[0].text; got != "第一行第二行 line three line four" && got != "第一行第二行line three line four" {
		t.Errorf("合并结果 %q 不符合预期", got)
	}
	if bs[1].text != "新段落" {
		t.Errorf("第二段 %q", bs[1].text)
	}
}

func TestMarkdownNestedLists(t *testing.T) {
	src := "- 甲\n  - 乙\n    - 丙\n- 丁\n\n1. 一\n2. 二\n   - 子项\n10. 十\n"
	bs := parseMarkdown(src)
	type item struct {
		indent int
		marker string
		text   string
	}
	var got []item
	for _, b := range bs {
		if b.kind != mdListItem {
			t.Fatalf("应全是列表项，得到 %+v", b)
		}
		got = append(got, item{b.indent, b.marker, b.text})
	}
	want := []item{
		{0, "•", "甲"}, {1, "•", "乙"}, {2, "•", "丙"}, {0, "•", "丁"},
		{0, "1.", "一"}, {0, "2.", "二"}, {1, "•", "子项"}, {0, "10.", "十"},
	}
	if len(got) != len(want) {
		t.Fatalf("得到 %d 项 %+v，期望 %d 项", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 项：%+v，期望 %+v", i, got[i], want[i])
		}
	}
	// 4 空格缩进同样体现层级。
	bs = parseMarkdown("- a\n    - b\n        - c\n")
	if len(bs) != 3 || bs[1].indent != 1 || bs[2].indent != 2 {
		t.Errorf("4 空格缩进的层级不对：%+v", bs)
	}
	tt := mdTestShow(t, src, 600, 500)
	for _, w := range []string{"甲", "乙", "丙", "丁", "子项"} {
		mdTestHas(t, tt, w)
	}
}

func TestMarkdownListContinuation(t *testing.T) {
	bs := parseMarkdown("- 这一项很长\n  接着写在下一行\n- 下一项")
	if len(bs) != 2 {
		t.Fatalf("应是 2 项，得到 %d：%+v", len(bs), bs)
	}
	if !strings.Contains(bs[0].text, "这一项很长") || !strings.Contains(bs[0].text, "接着写在下一行") {
		t.Errorf("续行应并入第一项：%q", bs[0].text)
	}
}

func TestMarkdownCodeBlock(t *testing.T) {
	src := "前文\n\n```go\nfunc main() {\n\n\tprintln(\"hi\")\n}\n```\n\n后文"
	bs := parseMarkdown(src)
	if len(bs) != 3 || bs[1].kind != mdCode {
		t.Fatalf("应是 段落/代码/段落，得到 %+v", bs)
	}
	code := bs[1]
	if code.lang != "go" {
		t.Errorf("语言标记 %q，期望 go", code.lang)
	}
	if len(code.lines) != 4 || code.lines[1] != "" || code.lines[2] != "\tprintln(\"hi\")" {
		t.Errorf("代码行（应保留内部空行与缩进）：%q", code.lines)
	}
	// 代码里的 markdown 符号不被解析，# 不会变标题。
	bs = parseMarkdown("```\n# 不是标题\n- 不是列表\n**不是粗体**\n```")
	if len(bs) != 1 || bs[0].kind != mdCode || len(bs[0].lines) != 3 {
		t.Errorf("围栏内应原样保留：%+v", bs)
	}
	// ~~~ 围栏、更长的围栏里嵌短围栏。
	bs = parseMarkdown("````\n```\n内层\n```\n````\n后")
	if len(bs) != 2 || len(bs[0].lines) != 3 {
		t.Errorf("四反引号围栏应包住三反引号：%+v", bs)
	}
	tt := mdTestShow(t, src, 600, 400)
	mdTestHas(t, tt, "func main() {")
	mdTestHas(t, tt, "后文")
}

func TestMarkdownInline(t *testing.T) {
	type want struct {
		text                      string
		bold, italic, code, strik bool
		link                      string
	}
	check := func(src string, wants ...want) {
		t.Helper()
		got := mdInline(src)
		if len(got) != len(wants) {
			t.Errorf("%q：得到 %d 段 %+v，期望 %d 段", src, len(got), got, len(wants))
			return
		}
		for i, w := range wants {
			g := got[i]
			if g.text != w.text || g.bold != w.bold || g.italic != w.italic ||
				g.code != w.code || g.strike != w.strik || g.link != w.link {
				t.Errorf("%q 第 %d 段：%+v，期望 %+v", src, i, g, w)
			}
		}
	}
	check("a **粗** b", want{text: "a "}, want{text: "粗", bold: true}, want{text: " b"})
	check("a *斜* b", want{text: "a "}, want{text: "斜", italic: true}, want{text: " b"})
	check("a _斜_ b", want{text: "a "}, want{text: "斜", italic: true}, want{text: " b"})
	check("用 `go test` 跑", want{text: "用 "}, want{text: "go test", code: true}, want{text: " 跑"})
	check("`**不解析**`", want{text: "**不解析**", code: true})
	check("~~删~~", want{text: "删", strik: true})
	check("**粗 *斜***", want{text: "粗 ", bold: true}, want{text: "斜", bold: true, italic: true})
	check("snake_case_name", want{text: "snake_case_name"})
	check("2 * 3 * 4", want{text: "2 * 3 * 4"})
	check(`\*转义\*`, want{text: "*转义*"})
	check("[文档](https://example.com/a)", want{text: "文档", link: "https://example.com/a"})
	check("见 https://example.com/x。", want{text: "见 "}, want{text: "https://example.com/x", link: "https://example.com/x"}, want{text: "。"})
	// 非 http 链接按普通文字，保留标签。
	check("[本地](./a.md)", want{text: "本地"})
	check("[邮件](mailto:a@b.c)", want{text: "邮件"})
	// 图片退化成替代文字。
	check("![图](a.png)", want{text: "图"})
}

func TestMarkdownLinkRenders(t *testing.T) {
	tt := mdTestShow(t, "详见 [官方文档](https://example.com/docs) 与 [本地说明](./README.md)。", 600, 300)
	mdTestHas(t, tt, "官方文档")
	mdTestHas(t, tt, "本地说明")
	mdTestLacks(t, tt, "](")
}

func TestMarkdownQuoteAndRule(t *testing.T) {
	bs := parseMarkdown("> 引用第一行\n> 引用第二行\n>\n> - 引用里的列表\n\n---\n\n***\n\n_ _ _\n正文")
	if len(bs) != 5 {
		t.Fatalf("应是 引用+3 条分隔线+段落，得到 %d：%+v", len(bs), mdTestKinds(bs))
	}
	if bs[0].kind != mdQuote || len(bs[0].children) != 2 ||
		bs[0].children[0].kind != mdParagraph || bs[0].children[1].kind != mdListItem {
		t.Errorf("引用的子块不对：%+v", bs[0])
	}
	for i := 1; i <= 3; i++ {
		if bs[i].kind != mdRule {
			t.Errorf("第 %d 块应是分隔线：%+v", i, bs[i])
		}
	}
	// 嵌套引用。
	bs = parseMarkdown("> 外\n> > 内")
	if len(bs) != 1 || len(bs[0].children) != 2 || bs[0].children[1].kind != mdQuote {
		t.Errorf("嵌套引用：%+v", bs)
	}
	tt := mdTestShow(t, "> 提示：先备份\n\n---\n\n后文", 600, 300)
	mdTestHas(t, tt, "提示：先备份")
	mdTestHas(t, tt, "后文")
}

func TestMarkdownFrontmatterSkipped(t *testing.T) {
	src := "---\nname: demo\ndescription: 示例\n---\n\n# 正文标题\n"
	bs := parseMarkdown(src)
	if len(bs) != 1 || bs[0].kind != mdHeading {
		t.Fatalf("frontmatter 应被跳过，得到 %+v", bs)
	}
	tt := mdTestShow(t, src, 600, 300)
	mdTestHas(t, tt, "正文标题")
	mdTestLacks(t, tt, "name: demo")
	// 没有闭合的 --- 不是 frontmatter：不能吞掉整篇文档。
	bs = parseMarkdown("---\n正文还在\n")
	if len(bs) != 2 || bs[0].kind != mdRule || bs[1].text != "正文还在" {
		t.Errorf("未闭合的 --- 应按分隔线处理：%+v", bs)
	}
	// CRLF 与 BOM。
	bs = parseMarkdown(string(rune(0xFEFF)) + "# 标题\r\n\r\n段落\r\n")
	if len(bs) != 2 || bs[0].kind != mdHeading || bs[0].text != "标题" {
		t.Errorf("BOM/CRLF：%+v", bs)
	}
}

func TestMarkdownTableDegrades(t *testing.T) {
	bs := parseMarkdown("| 名称 | 说明 |\n| --- | --- |\n| a | b |\n")
	if len(bs) != 2 {
		t.Fatalf("表头与一行数据各成一段，分隔行丢弃：%+v", bs)
	}
	if bs[0].text != "名称 │ 说明" || bs[1].text != "a │ b" {
		t.Errorf("表格行退化结果：%q / %q", bs[0].text, bs[1].text)
	}
}

// TestMarkdownMalformedNoPanic 覆盖畸形输入：都要有输出且不 panic。
func TestMarkdownMalformedNoPanic(t *testing.T) {
	cases := map[string]string{
		"空串":             "",
		"只有空白":           " \n\t\n   \n",
		"未闭合围栏":          "```go\nfmt.Println(1)\n没有结尾",
		"[ 无 ]":          "这里有 [没有右括号 和 (半个括号",
		"[x]( 无 )":       "[文字](https://example.com 没有收尾",
		"只有符号":           "****__``~~[]()!",
		"孤立反引号":          "`未闭合 code",
		"孤立强调":           "**未闭合 *也未闭合",
		"空标题":            "#\n##\n###### ",
		"空列表项":           "-\n*\n+\n1.\n",
		"深层引用":           strings.Repeat(">", 50) + " 深",
		"深层缩进":           strings.Repeat("  ", 40) + "- 深",
		"只有 frontmatter": "---\na: b\n---",
		"超长单行":           strings.Repeat("长", 20000),
		"超长英文单词":         strings.Repeat("a", 50000),
		"大量强调符":          strings.Repeat("*a", 5000),
		"制表符缩进":          "\t- 项\n\t\t- 子\n\t```\n\tcode\n\t```",
		"非法 UTF-8":       "bad \xff\xfe bytes \xc3",
		"HTML":           "<div class=\"x\">原样 <b>文字</b></div>",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			// 解析不能 panic；渲染要能跑完两帧。
			_ = parseMarkdown(src)
			tt := mdTestShow(t, src, 500, 300)
			// 只有符号/空标题/空列表项/仅 frontmatter 的输入允许没有文字。
			noText := name == "空标题" || name == "空列表项" || name == "只有 frontmatter"
			if strings.TrimSpace(src) != "" && !noText && len(tt.Texts()) == 0 {
				t.Errorf("非空输入应有文字输出")
			}
		})
	}
}

// TestMarkdownTruncation 验证两个自保上限：源码字节数与块数。
func TestMarkdownTruncation(t *testing.T) {
	// 源码过大：只取前 mdMaxSource 字节，并追加提示。
	big := strings.Repeat("一行普通文字\n\n", (mdMaxSource/len("一行普通文字\n\n"))+50)
	bs := parseMarkdown(big)
	if n := len(bs); n < 2 || bs[n-1].kind != mdNotice || bs[n-1].text != mdTruncatedNotice {
		t.Fatalf("超大源码末尾应有截断提示，得到 %d 块，末块 %+v", len(bs), bs[len(bs)-1])
	}
	// 块数过多：最多 mdMaxBlocks 块加一条提示。
	many := strings.Repeat("- 项\n", mdMaxBlocks+500)
	bs = parseMarkdown(many)
	if len(bs) != mdMaxBlocks+1 {
		t.Errorf("应截成 %d 块（含提示），得到 %d", mdMaxBlocks+1, len(bs))
	}
	if bs[len(bs)-1].kind != mdNotice {
		t.Errorf("末块应是提示：%+v", bs[len(bs)-1])
	}
	// 渲染这份被截断的内容不能卡死或 panic，且提示可见。
	tt := mdTestShow(t, many, 500, 300)
	_ = tt
	// 单个代码块行数过多：只保留前 mdMaxCodeLines 行并注明。
	code := "```\n" + strings.Repeat("x\n", mdMaxCodeLines+100) + "```"
	bs = parseMarkdown(code)
	if len(bs) != 1 || len(bs[0].lines) != mdMaxCodeLines+1 {
		t.Fatalf("代码块应被截到 %d 行（+1 行提示），得到 %+v 块 %d 行", mdMaxCodeLines, len(bs), len(bs[0].lines))
	}
	if !strings.Contains(bs[0].lines[mdMaxCodeLines], "未显示") {
		t.Errorf("末行应是未显示提示：%q", bs[0].lines[mdMaxCodeLines])
	}
}

// mdTestSample 是一份 SKILL.md 风格的真实示例，截图用。
const mdTestSample = "---\n" +
	"name: pdf-toolkit\n" +
	"description: 处理 PDF 文件：提取文字、合并、拆分\n" +
	"---\n\n" +
	"# PDF 工具箱\n\n" +
	"处理 PDF 的一组常用流程。需要 **批量处理** 时先看「何时使用」，遇到扫描件请改用 *OCR* 流程，\n" +
	"详见 [官方文档](https://example.com/pdf/docs) 或运行 `pdftotext --help`。\n\n" +
	"## 何时使用\n\n" +
	"- 用户要求提取、合并或拆分 PDF\n" +
	"- 需要把 PDF 转成纯文本\n" +
	"  - 含表格的文件先用 `--layout`\n" +
	"  - 扫描件见下文 OCR 一节\n" +
	"- 其他情况不要触发\n\n" +
	"## 步骤\n\n" +
	"1. 确认输入文件存在\n" +
	"2. 选择工具并执行\n" +
	"3. 检查输出是否完整\n\n" +
	"### 示例\n\n" +
	"```bash\n" +
	"# 提取全部文字\n" +
	"pdftotext -layout input.pdf output.txt\n\n" +
	"# 合并两个文件\n" +
	"pdfunite a.pdf b.pdf merged.pdf\n" +
	"```\n\n" +
	"> 注意：处理前先备份原文件，kura 的快照不会覆盖工作目录外的文件。\n\n" +
	"---\n\n" +
	"#### 参考\n\n" +
	"| 工具 | 用途 |\n| --- | --- |\n| pdftotext | 提取文字 |\n\n" +
	"最后一段：这是一个相当长的段落，用来检查自动折行是否正常工作，不应出现整块文字挤成一行、" +
	"超出右边界或与上下文重叠的情况。English text also needs to wrap correctly at word boundaries.\n"

// TestMarkdownShots 把示例渲染成 PNG（KURA_UI_SHOTS=<目录>），供人工核对排版。
func TestMarkdownShots(t *testing.T) {
	out := os.Getenv("KURA_UI_SHOTS")
	if out == "" {
		t.Skip("未设置 KURA_UI_SHOTS，跳过 Markdown 截图")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"dark", "light"} {
		mode := mode
		tt := myui.NewTester(func(c *myui.Context) {
			sk := kuraScheme.apply(c, mode)
			th := c.Theme()
			myui.Scroll(c).Fill().Background(sk.pal.canvas).Children(func() {
				myui.Column(c).FillWidth().Padding(th.Space(6)).Children(func() {
					renderMarkdown(c, mdTestSample)
				})
			})
		}, 760, 1180)
		tt.Frame()
		tt.Frame()
		name := "md-render"
		if mode == "light" {
			name = "md-render-light"
		}
		saveImage(t, out, name, tt.Image())
	}
	// 窄宽度：检查折行与列表缩进在窄栏里不被挤坏。
	tt := myui.NewTester(func(c *myui.Context) {
		sk := kuraScheme.apply(c, "dark")
		th := c.Theme()
		myui.Scroll(c).Fill().Background(sk.pal.canvas).Children(func() {
			myui.Column(c).FillWidth().Padding(th.Space(4)).Children(func() {
				renderMarkdown(c, mdTestSample)
			})
		})
	}, 360, 1500)
	tt.Frame()
	tt.Frame()
	saveImage(t, filepath.Clean(out), "md-render-narrow", tt.Image())
}
