package ui

// 详情页「全文」tab 的渲染：交给 mygo 官方的 ui/markdown 视图
// （github.com/egoist/mygo/ui/markdown），kura 自己只保留两件官方不做的事：
//
//   - 跳过 SKILL.md 开头的 YAML frontmatter（那是元数据，不该当正文渲染）；
//   - 给超大源码一个上限（fullTextMaxSource），病态文件不把界面拖垮。
//
// 主题只在外观（窗口主题）变化时重建，不每帧新建（原因与实测数据见
// syncMarkdownTheme）：外观开关（浅色/深色/跟随系统）一换，正文颜色、代码块
// 底色、表格线立刻跟上。Options 本身必须跨帧稳定：官方视图把解析状态放在
// ui.Local 里，但 Options 是应用传给它的指针，每帧在栈上造一个新 Options 再
// 取地址会让状态失配，所以它存在 App.md 上。
//
// 渲染路径不读盘、不发翻译请求；译文只是另一份源文本，由 viewFullTextBody
// 决定把哪一份交给这里（见 view.go、translate.go）。

import (
	"strings"
	"unicode/utf8"

	"github.com/ArnoChenFx/kura/internal/i18n"
	myui "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// fullTextMaxSource 是全文渲染的源码上限（字节）。SKILL.md 正常只有几 KB；
// 误选的超大文件（二进制、日志）会让解析与布局都变慢，所以截断并提示。
const fullTextMaxSource = 512 * 1024

// stripFrontmatter 去掉正文开头的 YAML frontmatter 区块。
//
// 规则（与旧的自写渲染器一致）：只有第一行恰好是 --- 才算 frontmatter，并且
// 必须能找到闭合的 ---（或 ...）；没有闭合时按原样返回，绝不吞掉整篇正文。
// 开头若有 BOM 先去 BOM。返回的文本其余部分原样保留（含 CRLF），交给官方解析器。
func stripFrontmatter(src string) string {
	s := strings.TrimPrefix(src, "\uFEFF")
	line, rest, ok := strings.Cut(s, "\n")
	if !ok || strings.TrimRight(line, " \t\r") != "---" {
		return s
	}
	for {
		l, r, more := strings.Cut(rest, "\n")
		switch strings.TrimRight(l, " \t\r") {
		case "---", "...":
			if !more {
				return ""
			}
			return r
		}
		if !more {
			return s
		}
		rest = r
	}
}

// capSource 把源码截到 limit 字节以内，截断时在 UTF-8 字符边界上收刀
// （不切出半个字符），并报告是否截断。
//
// limit 是参数而不是直接读常量：单测用很小的 limit 就能覆盖「超限即截断」
// 「上限正好落在多字节字符中间」这些情况，不必真的构造 512 KB 文本。
// 生产代码只经 fullTextSource 调用，传 fullTextMaxSource。
func capSource(src string, limit int) (string, bool) {
	if limit <= 0 || len(src) <= limit {
		return src, false
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(src[cut]) {
		cut--
	}
	return src[:cut], true
}

// fullTextSource 是全文渲染前的源码处理（纯函数）：剥掉 frontmatter，再按
// fullTextMaxSource 截断，报告是否截断。
func fullTextSource(src string) (string, bool) {
	return capSource(stripFrontmatter(src), fullTextMaxSource)
}

// syncMarkdownTheme 让 a.md.Theme 跟上窗口主题：只在窗口主题（c.Theme()）
// 与上一次不同时重建。
//
// 为什么不能每帧重建：官方视图拿我们给的 *Theme 与它上一帧用的主题比
// （view.go 的 `th != st.th`）来判断「主题变了」，变了就清空段落缓存、重新
// 解析与测量 span。markdown.DefaultTheme 每次都返回新的 *editor.Theme，每帧
// 新建会让这个判断每帧为真。实测（40 KB、1200 段的文档，720×900 的 Tester，
// 60 帧）：每帧重建 352.7 ms（≈5.9 ms/帧），只在窗口主题变化时重建
// 205.5 ms（≈3.4 ms/帧），差的就是每帧 clear(paras)。官方视图自己也会按窗口
// 主题缓存它的默认主题（st.def），所以这里只跟窗口主题的值比就够了。
func (a *App) syncMarkdownTheme(c *myui.Context) {
	t := c.Theme()
	if a.md.Theme != nil && a.mdTheme == *t {
		return
	}
	a.mdTheme = *t
	a.md.Theme = markdown.DefaultTheme(t)
}

// viewMarkdown 把一段 Markdown 源文本渲染进当前视图，是全文页唯一的渲染入口。
func (a *App) viewMarkdown(c *myui.Context, src string) {
	src, truncated := fullTextSource(src)
	a.viewMarkdownBody(c, src, truncated)
}

// viewMarkdownBody 渲染处理好的源码；truncated 为真时在正文后追加截断提示
// （文案走目录，见 catalog.go 的 ui.markdown.truncated）。
func (a *App) viewMarkdownBody(c *myui.Context, src string, truncated bool) {
	a.syncMarkdownTheme(c)
	markdown.View(c, src, &a.md)
	if truncated {
		t := c.Theme()
		myui.Text(c, i18n.T("ui.markdown.truncated")).FontSize(t.FontSize-1).
			TextColor(t.TextMuted).Margin(t.Space(2), 0, 0, 0)
	}
}
