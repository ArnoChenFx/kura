// SKILL.md 的 Markdown 渲染器：把 markdown 源文本渲染成 MyGo 原生元素树。
//
// 只支持 SKILL.md 里常见的子集（frontmatter、标题、段落、列表、围栏代码块、
// 引用、分隔线，以及行内的 code / 粗体 / 斜体 / 删除线 / 链接）。不认识的结构
// 一律退化成普通段落，绝不 panic。
//
// 结构：先把源文本解析成与 UI 无关的块列表（parseMarkdown，纯函数，好测），
// 再由 renderMarkdown 逐块画出来。渲染函数只吃字符串，不做任何 I/O。
//
// 本文件里的「•」「◦」「▪」、" │ " 与裸链接末尾的标点字符集是 **markdown
// 语法**（解析/输出用的符号），不是给用户看的文案，也不该随界面语言变，所以
// 它们不进文案目录；真正的界面文案（截断提示、未显示行数）走 i18n，见下面。
package ui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ArnoChenFx/kura/internal/i18n"
	myui "github.com/egoist/mygo/ui"
)

const (
	// mdMaxSource 是渲染的源码上限（字节）。SKILL.md 正常只有几 KB；
	// 超大文件（误选了二进制、日志）会让解析与布局都拖慢 UI 线程，所以截断。
	mdMaxSource = 512 * 1024
	// mdMaxBlocks 是单次渲染的块数上限（列表项、代码行也各算一块），
	// 防止病态输入（几十万个列表项）生成海量元素。
	mdMaxBlocks = 2000
	// mdMaxInline 是做行内解析的单段文字上限（字节）。行内解析对未闭合的
	// 强调符号可能退化成平方级，超长段落直接按纯文字渲染。
	mdMaxInline = 8000
	// mdMaxDepth 是行内强调嵌套与引用嵌套的深度上限。
	mdMaxDepth = 8
	// mdMaxCodeLines 是单个代码块渲染的行数上限；mdMaxLineRunes 是单行的字符上限。
	mdMaxCodeLines = 400
	mdMaxLineRunes = 2000
	// mdMonoFont 是等宽字体族名；Windows 上 Consolas 一定存在。
	mdMonoFont = "Consolas"
)

// 截断提示是界面文案，渲染时现取（mdTruncatedNotice 是它的中文取值，
// 供 markdown_test.go 引用，定义在 catalog.go）。

// mdKind 是块的种类。
type mdKind int

const (
	mdParagraph mdKind = iota // 段落（含退化的表格行）
	mdHeading                 // 标题，level 1~6
	mdListItem                // 列表项：indent 层级、marker 符号
	mdCode                    // 围栏代码块
	mdQuote                   // 引用，children 是其中的块
	mdRule                    // 分隔线
	mdNotice                  // 截断提示
)

// mdBlock 是解析出的一个块。字段按 kind 取用，其余为零值。
type mdBlock struct {
	kind     mdKind
	level    int       // 标题级别 1~6
	indent   int       // 列表层级，0 起
	marker   string    // 列表符号："•" 或 "3."
	text     string    // 段落/标题/列表项正文（行内 markdown 未解析）
	lang     string    // 代码块语言标记
	lines    []string  // 代码块的各行
	children []mdBlock // 引用里的块
}

// parseMarkdown 把源文本解析成块列表。budget 之外的内容被丢弃并以
// mdNotice 提示。纯函数：不读盘、不依赖 UI。
func parseMarkdown(src string) []mdBlock {
	truncated := false
	if len(src) > mdMaxSource {
		// 在 UTF-8 字符边界上截断，避免切出半个字符。
		cut := mdMaxSource
		for cut > 0 && !utf8.RuneStart(src[cut]) {
			cut--
		}
		src = src[:cut]
		truncated = true
	}
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "\n")
	src = strings.TrimPrefix(src, "\uFEFF") // 去掉 BOM
	lines := strings.Split(src, "\n")
	lines = mdSkipFrontmatter(lines)

	budget := mdMaxBlocks
	blocks := mdParseBlocks(lines, 0, &budget)
	if budget <= 0 {
		truncated = true
	}
	if truncated {
		blocks = append(blocks, mdBlock{kind: mdNotice, text: i18n.T("ui.markdown.truncated")})
	}
	return blocks
}

// mdSkipFrontmatter 跳过文件开头的 --- … --- 区块。没有闭合的 --- 时
// 不当作 frontmatter（否则整篇文档都会被吞掉），按普通内容处理。
func mdSkipFrontmatter(lines []string) []string {
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t") != "---" {
		return lines
	}
	for i := 1; i < len(lines); i++ {
		if s := strings.TrimRight(lines[i], " \t"); s == "---" || s == "..." {
			return lines[i+1:]
		}
	}
	return lines
}

// mdLeading 返回 line 开头空白的列宽（制表符按 4 列算）与去掉空白后的剩余部分。
func mdLeading(line string) (int, string) {
	w := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case ' ':
			w++
		case '\t':
			w += 4 - w%4
		default:
			return w, line[i:]
		}
	}
	return w, ""
}

// mdIsRule 判断一行是不是分隔线：三个及以上相同的 - * _，中间可夹空格。
func mdIsRule(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 3 || (s[0] != '-' && s[0] != '*' && s[0] != '_') {
		return false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case s[0]:
			n++
		case ' ', '\t':
		default:
			return false
		}
	}
	return n >= 3
}

// mdFenceOpen 判断 s（已去掉缩进）是不是围栏开头，返回围栏字符、长度与语言标记。
func mdFenceOpen(s string) (ch byte, n int, lang string, ok bool) {
	if len(s) < 3 || (s[0] != '`' && s[0] != '~') {
		return 0, 0, "", false
	}
	ch = s[0]
	for n < len(s) && s[n] == ch {
		n++
	}
	if n < 3 {
		return 0, 0, "", false
	}
	lang = strings.TrimSpace(s[n:])
	// 反引号围栏的信息串里不能再有反引号（否则是行内 code，如 ```x```）。
	if ch == '`' && strings.Contains(lang, "`") {
		return 0, 0, "", false
	}
	// 只取第一个词当语言标记（``` go title="x" 之类的附加串不要）。
	if i := strings.IndexAny(lang, " \t{"); i >= 0 {
		lang = lang[:i]
	}
	return ch, n, lang, true
}

// mdListMarker 判断 s（已去掉缩进）是不是列表项开头，返回符号、正文。
// 无序：- * + 后跟空白；有序：1~9 位数字加 . 或 )，后跟空白。
// 返回的符号（•）是渲染用的 markdown 语法，不是界面文案。
func mdListMarker(s string) (marker, rest string, ok bool) {
	if len(s) >= 2 && (s[0] == '-' || s[0] == '*' || s[0] == '+') && (s[1] == ' ' || s[1] == '\t') {
		return "•", strings.TrimSpace(s[2:]), true
	}
	i := 0
	for i < len(s) && i < 9 && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i > 0 && i+1 < len(s) && (s[i] == '.' || s[i] == ')') && (s[i+1] == ' ' || s[i+1] == '\t') {
		return s[:i] + ".", strings.TrimSpace(s[i+2:]), true
	}
	// 空列表项（"-" 或 "1." 单独成行）：当作带空正文的列表项。
	if s == "-" || s == "*" || s == "+" {
		return "•", "", true
	}
	return "", "", false
}

// mdHeadingLine 判断 s（已去掉缩进）是不是 ATX 标题，返回级别与正文。
func mdHeadingLine(s string) (level int, text string, ok bool) {
	for level < len(s) && s[level] == '#' {
		level++
	}
	if level == 0 || level > 6 {
		return 0, "", false
	}
	rest := s[level:]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return 0, "", false // "#hashtag" 不是标题
	}
	rest = strings.TrimSpace(rest)
	// 去掉可选的收尾 #：「## 标题 ##」。
	if t := strings.TrimRight(rest, "#"); t != rest && (t == "" || t[len(t)-1] == ' ') {
		rest = strings.TrimSpace(t)
	}
	return level, rest, true
}

// mdIsTableLine 判断一行像不像表格行（以 | 开头）。
func mdIsTableLine(s string) bool { return strings.HasPrefix(s, "|") }

// mdTableRow 把表格行退化成一行文字：单元格以「 │ 」分隔；分隔行（|---|---|）
// 返回 ok=false，直接丢弃。
func mdTableRow(s string) (string, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	cells := strings.Split(s, "|")
	sep := true
	for i, c := range cells {
		c = strings.TrimSpace(c)
		cells[i] = c
		if strings.Trim(c, "-: ") != "" {
			sep = false
		}
	}
	if sep {
		return "", false
	}
	return strings.Join(cells, " │ "), true
}

// mdJoin 把软换行的两行并成一行：中日韩文字之间不加空格，其余加一个空格。
func mdJoin(prev, next string) string {
	if prev == "" {
		return next
	}
	a, _ := utf8.DecodeLastRuneInString(prev)
	b, _ := utf8.DecodeRuneInString(next)
	if mdIsCJK(a) && mdIsCJK(b) {
		return prev + next
	}
	return prev + " " + next
}

// mdIsCJK 判断 r 是否中日韩文字或全角标点。
func mdIsCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) ||
		(r >= 0x3000 && r <= 0x303F) || (r >= 0xFF00 && r <= 0xFFEF)
}

// mdParseBlocks 解析 lines 为块。budget 是剩余块数，耗尽即停。depth 是
// 引用嵌套深度。
func mdParseBlocks(lines []string, depth int, budget *int) []mdBlock {
	var blocks []mdBlock
	var para string   // 正在累积的段落
	var inPara bool   // 是否有段落在累积
	var stack []int   // 列表缩进栈：下标即层级
	listOpen := false // 上一个块是列表项（允许懒续行）

	add := func(b mdBlock) bool {
		if *budget <= 0 {
			return false
		}
		*budget--
		blocks = append(blocks, b)
		return true
	}
	flush := func() bool {
		if !inPara {
			return true
		}
		inPara = false
		text := para
		para = ""
		return add(mdBlock{kind: mdParagraph, text: text})
	}

	for i := 0; i < len(lines); i++ {
		if *budget <= 0 {
			break
		}
		line := lines[i]
		indent, s := mdLeading(line)

		// 空行：结束段落；列表保持打开（空行后缩进的内容仍属于列表）。
		if s == "" {
			if !flush() {
				break
			}
			// 空行后下一行若不是缩进/列表项，则列表结束。
			if i+1 < len(lines) {
				ni, ns := mdLeading(lines[i+1])
				if _, _, isItem := mdListMarker(ns); !isItem && ni == 0 {
					stack = stack[:0]
					listOpen = false
				}
			}
			continue
		}

		// 围栏代码块。
		if ch, n, lang, ok := mdFenceOpen(s); ok {
			if !flush() {
				break
			}
			var body []string
			skipped := 0
			j := i + 1
			for ; j < len(lines); j++ {
				ci, cs := mdLeading(lines[j])
				if cs != "" && cs[0] == ch {
					k := 0
					for k < len(cs) && cs[k] == ch {
						k++
					}
					if k >= n && strings.TrimSpace(cs[k:]) == "" && ci < indent+4 {
						break // 闭合围栏
					}
				}
				// 单块行数有上限：超出的行只计数不保留，避免海量元素拖慢 UI 线程。
				if len(body) >= mdMaxCodeLines {
					skipped++
					continue
				}
				// 去掉与围栏相同的缩进（最多 indent 列），保留其余空白。
				body = append(body, mdClipLine(mdStripIndent(lines[j], indent)))
			}
			if skipped > 0 {
				body = append(body, i18n.Tf("ui.markdown.moreLines", skipped))
			}
			// 代码块按行数多收一些预算：一行就是一个界面元素。
			*budget -= len(body) / 4
			if !add(mdBlock{kind: mdCode, lang: lang, lines: body}) {
				break
			}
			i = j // 未闭合时 j == len(lines)，循环自然结束
			listOpen = false
			continue
		}

		// 标题。
		if level, text, ok := mdHeadingLine(s); ok && indent < 4 {
			if !flush() {
				break
			}
			if !add(mdBlock{kind: mdHeading, level: level, text: text}) {
				break
			}
			stack = stack[:0]
			listOpen = false
			continue
		}

		// 分隔线（要先于列表判断：「- - -」「* * *」是分隔线）。
		if indent < 4 && mdIsRule(s) {
			if !flush() {
				break
			}
			if !add(mdBlock{kind: mdRule}) {
				break
			}
			stack = stack[:0]
			listOpen = false
			continue
		}

		// 引用：收集连续的以 > 开头的行（含懒续行不处理，保持简单），去掉一层 > 后递归。
		if strings.HasPrefix(s, ">") {
			if !flush() {
				break
			}
			var inner []string
			j := i
			for ; j < len(lines); j++ {
				_, qs := mdLeading(lines[j])
				if !strings.HasPrefix(qs, ">") {
					break
				}
				qs = strings.TrimPrefix(qs, ">")
				qs = strings.TrimPrefix(qs, " ")
				inner = append(inner, qs)
			}
			var kids []mdBlock
			if depth < mdMaxDepth {
				kids = mdParseBlocks(inner, depth+1, budget)
			} else {
				// 嵌套过深：不再递归，剩余内容按一个段落收下。
				kids = []mdBlock{{kind: mdParagraph, text: strings.TrimSpace(strings.Join(inner, " "))}}
			}
			if !add(mdBlock{kind: mdQuote, children: kids}) {
				break
			}
			i = j - 1
			stack = stack[:0]
			listOpen = false
			continue
		}

		// 列表项。
		if marker, rest, ok := mdListMarker(s); ok {
			if !flush() {
				break
			}
			// 按缩进栈决定层级：缩进更深就新开一层，回退就弹栈。
			for len(stack) > 0 && indent < stack[len(stack)-1] {
				stack = stack[:len(stack)-1]
			}
			if len(stack) == 0 || indent > stack[len(stack)-1] {
				stack = append(stack, indent)
			}
			lvl := len(stack) - 1
			if lvl > 6 {
				lvl = 6 // 层级再深也只缩进到 6 档，避免正文被挤出视口
			}
			if !add(mdBlock{kind: mdListItem, indent: lvl, marker: marker, text: rest}) {
				break
			}
			listOpen = true
			continue
		}

		// 表格行（不支持表格，退化成一行一段的文字）。
		if mdIsTableLine(s) {
			if !flush() {
				break
			}
			if row, ok := mdTableRow(s); ok {
				if !add(mdBlock{kind: mdParagraph, text: row}) {
					break
				}
			}
			listOpen = false
			continue
		}

		// 普通文字行：列表项后的续行并入该项；否则并入段落。
		text := strings.TrimSpace(s)
		// 行尾两个空格或反斜杠是硬换行：保留成 \n。
		hard := strings.HasSuffix(line, "  ") || strings.HasSuffix(text, "\\")
		text = strings.TrimSuffix(text, "\\")
		if listOpen && !inPara && len(blocks) > 0 && blocks[len(blocks)-1].kind == mdListItem {
			last := &blocks[len(blocks)-1]
			last.text = mdJoin(last.text, text)
			if hard {
				last.text += "\n"
			}
			continue
		}
		if strings.HasSuffix(para, "\n") {
			para += text
		} else {
			para = mdJoin(para, text)
		}
		if hard {
			para += "\n"
		}
		inPara = true
		stack = stack[:0]
		listOpen = false
	}
	flush()
	return blocks
}

// mdClipLine 把过长的代码行截到 mdMaxLineRunes 个字符并加省略号。
func mdClipLine(s string) string {
	if len(s) <= mdMaxLineRunes {
		return s
	}
	r := []rune(s)
	if len(r) <= mdMaxLineRunes {
		return s
	}
	return string(r[:mdMaxLineRunes]) + "…"
}

// mdStripIndent 去掉 line 开头最多 n 列空白，不动其余内容。
func mdStripIndent(line string, n int) string {
	w := 0
	for i := 0; i < len(line); i++ {
		if w >= n {
			return line[i:]
		}
		switch line[i] {
		case ' ':
			w++
		case '\t':
			w += 4 - w%4
		default:
			return line[i:]
		}
	}
	return ""
}

// mdSpan 是行内解析出的一段带样式文字。
type mdSpan struct {
	text   string
	bold   bool
	italic bool
	code   bool
	strike bool
	link   string // 非空：http(s) 链接目标
}

// mdInline 解析行内 markdown。不认识或没闭合的符号按字面输出。
func mdInline(s string) []mdSpan {
	if len(s) > mdMaxInline {
		return []mdSpan{{text: s}}
	}
	return mdMerge(mdInlineRec(s, mdSpan{}, 0))
}

// mdInlineRec 是行内解析的递归主体：base 是当前累积的样式。
func mdInlineRec(s string, base mdSpan, depth int) []mdSpan {
	var out []mdSpan
	var buf strings.Builder
	emit := func(sp mdSpan, text string) {
		if text == "" {
			return
		}
		sp.text = text
		out = append(out, sp)
	}
	flush := func() {
		emit(base, buf.String())
		buf.Reset()
	}
	// 嵌套过深就不再解析强调，按字面保留。
	canNest := depth < mdMaxDepth

	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s) && mdIsPunct(s[i+1]):
			buf.WriteByte(s[i+1])
			i += 2

		case c == '`':
			// 行内 code：找同样长度的反引号收尾，内容原样不再解析。
			n := 0
			for i+n < len(s) && s[i+n] == '`' {
				n++
			}
			end := mdFindRun(s, i+n, '`', n)
			if end < 0 {
				buf.WriteString(s[i : i+n])
				i += n
				break
			}
			flush()
			code := s[i+n : end]
			if len(code) > 2 && code[0] == ' ' && code[len(code)-1] == ' ' {
				code = code[1 : len(code)-1]
			}
			sp := base
			sp.code = true
			emit(sp, code)
			i = end + n

		case c == '!' && i+1 < len(s) && s[i+1] == '[':
			// 图片：不支持，退化成它的替代文字。
			if label, _, next, ok := mdLinkAt(s, i+1); ok {
				buf.WriteString(label)
				i = next
				break
			}
			buf.WriteByte(c)
			i++

		case c == '[':
			label, url, next, ok := mdLinkAt(s, i)
			if !ok {
				buf.WriteByte(c)
				i++
				break
			}
			flush()
			sp := base
			if mdIsHTTP(url) {
				sp.link = url
			}
			// 标签内可以再有强调；非 http 链接只保留文字。
			if canNest {
				out = append(out, mdInlineRec(label, sp, depth+1)...)
			} else {
				emit(sp, label)
			}
			i = next

		case c == '<' && (strings.HasPrefix(s[i:], "<http://") || strings.HasPrefix(s[i:], "<https://")):
			// 自动链接 <https://…>。
			if end := strings.IndexByte(s[i:], '>'); end > 0 && !strings.ContainsAny(s[i+1:i+end], " \t") {
				flush()
				sp := base
				sp.link = s[i+1 : i+end]
				emit(sp, s[i+1:i+end])
				i += end + 1
				break
			}
			buf.WriteByte(c)
			i++

		case (c == 'h') && mdIsHTTP(s[i:]) && (i == 0 || !mdIsWord(s[i-1])):
			// 裸链接 https://…：到空白为止，去掉末尾的标点。
			end := i
			for end < len(s) && s[end] != ' ' && s[end] != '\t' && s[end] != '\n' && s[end] != '<' && s[end] != '>' {
				end++
			}
			// 字符集是各类句末标点（含全角），用来剥掉裸链接末尾点错的标点：
			// 是 markdown 语法，不是界面文案。
			for end > i && strings.IndexByte(".,;:!?)]}\"'，。；：！？）", s[end-1]) >= 0 {
				end--
			}
			if end-i <= len("https://") {
				buf.WriteByte(c)
				i++
				break
			}
			flush()
			sp := base
			sp.link = s[i:end]
			emit(sp, s[i:end])
			i = end

		case canNest && (strings.HasPrefix(s[i:], "**") || strings.HasPrefix(s[i:], "__")):
			d := s[i : i+2]
			if c == '_' && i > 0 && mdIsWord(s[i-1]) {
				buf.WriteString(d) // snake__case 里的 __ 不是强调
				i += 2
				break
			}
			if j := mdFindClose(s, i+2, d); j > 0 {
				flush()
				sp := base
				sp.bold = true
				out = append(out, mdInlineRec(s[i+2:j], sp, depth+1)...)
				i = j + 2
				break
			}
			buf.WriteString(d)
			i += 2

		case canNest && strings.HasPrefix(s[i:], "~~"):
			if j := mdFindClose(s, i+2, "~~"); j > 0 {
				flush()
				sp := base
				sp.strike = true
				out = append(out, mdInlineRec(s[i+2:j], sp, depth+1)...)
				i = j + 2
				break
			}
			buf.WriteString("~~")
			i += 2

		case canNest && (c == '*' || c == '_'):
			// 单个 * / _：斜体。_ 要求在词边界上，避免 snake_case 被吃掉。
			if c == '_' && ((i > 0 && mdIsWord(s[i-1])) || i+1 >= len(s)) {
				buf.WriteByte(c)
				i++
				break
			}
			if i+1 >= len(s) || s[i+1] == ' ' || s[i+1] == '\t' || s[i+1] == c {
				buf.WriteByte(c)
				i++
				break
			}
			if j := mdFindItalicClose(s, i+1, c); j > 0 {
				flush()
				sp := base
				sp.italic = true
				out = append(out, mdInlineRec(s[i+1:j], sp, depth+1)...)
				i = j + 1
				break
			}
			buf.WriteByte(c)
			i++

		default:
			buf.WriteByte(c)
			i++
		}
	}
	flush()
	return out
}

// mdFindRun 在 s[from:] 里找恰好 n 个连续 ch 组成的一段，返回起点，找不到返回 -1。
func mdFindRun(s string, from int, ch byte, n int) int {
	for i := from; i < len(s); {
		if s[i] != ch {
			i++
			continue
		}
		k := 0
		for i+k < len(s) && s[i+k] == ch {
			k++
		}
		if k == n {
			return i
		}
		i += k
	}
	return -1
}

// mdFindClose 找 d（** / __ / ~~）的收尾位置：内容非空，且收尾符号前不是空白。
func mdFindClose(s string, from int, d string) int {
	for from < len(s) {
		j := strings.Index(s[from:], d)
		if j < 0 {
			return -1
		}
		j += from
		// 三个相连的 *（「**粗 *斜***」）：前一个属于内层斜体，取后两个收尾。
		if d == "**" && j+2 < len(s) && s[j+2] == '*' && (j+3 >= len(s) || s[j+3] != '*') {
			j++
		}
		if j > from && s[j-1] != ' ' && s[j-1] != '\t' {
			if d == "__" && j+2 < len(s) && mdIsWord(s[j+2]) {
				from = j + 2 // __ 后面紧跟字母：词中间，不是收尾
				continue
			}
			return j
		}
		from = j + 1
	}
	return -1
}

// mdFindItalicClose 找单个 * / _ 的收尾：内容非空，前面不是空白，且不是 ** 的一半。
func mdFindItalicClose(s string, from int, c byte) int {
	for i := from; i < len(s); i++ {
		if s[i] == '`' {
			// 跳过行内 code：code 里的 * 不算收尾。
			if end := strings.IndexByte(s[i+1:], '`'); end >= 0 {
				i += end + 1
			}
			continue
		}
		if s[i] != c {
			continue
		}
		if i+1 < len(s) && s[i+1] == c {
			i++ // ** 整体跳过
			continue
		}
		if i > from && s[i-1] != ' ' && s[i-1] != '\t' {
			if c == '_' && i+1 < len(s) && mdIsWord(s[i+1]) {
				continue
			}
			return i
		}
	}
	return -1
}

// mdLinkAt 解析 s[i:] 处的 [文字](url)，返回文字、url 与下一个位置。
func mdLinkAt(s string, i int) (label, url string, next int, ok bool) {
	if i >= len(s) || s[i] != '[' {
		return "", "", 0, false
	}
	// 找匹配的 ]（允许一层嵌套的 [] ）。
	depth := 0
	j := i
	for ; j < len(s); j++ {
		if s[j] == '\\' {
			j++
			continue
		}
		if s[j] == '[' {
			depth++
		} else if s[j] == ']' {
			depth--
			if depth == 0 {
				break
			}
		}
	}
	if j >= len(s) || j+1 >= len(s) || s[j+1] != '(' {
		return "", "", 0, false
	}
	// 找匹配的 )（url 里允许成对的括号）。
	p := 0
	k := j + 1
	for ; k < len(s); k++ {
		if s[k] == '(' {
			p++
		} else if s[k] == ')' {
			p--
			if p == 0 {
				break
			}
		}
	}
	if k >= len(s) {
		return "", "", 0, false
	}
	url = strings.TrimSpace(s[j+2 : k])
	// 去掉可选的标题："url \"title\""。
	if sp := strings.IndexAny(url, " \t"); sp >= 0 {
		url = url[:sp]
	}
	url = strings.Trim(url, "<>")
	return s[i+1 : j], url, k + 1, true
}

// mdIsHTTP 判断 s 是否以 http:// 或 https:// 开头。
func mdIsHTTP(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// mdIsPunct 判断 b 是否可被反斜杠转义的 ASCII 标点。
func mdIsPunct(b byte) bool {
	return strings.IndexByte("\\`*_{}[]()#+-.!<>|~\"'", b) >= 0
}

// mdIsWord 判断 b 是否字母数字（含非 ASCII 字节，视为词内字符）。
func mdIsWord(b byte) bool {
	return b >= 0x80 || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// mdMerge 合并样式相同的相邻片段，减少元素数量。
func mdMerge(in []mdSpan) []mdSpan {
	var out []mdSpan
	for _, sp := range in {
		if n := len(out); n > 0 {
			p := &out[n-1]
			if p.bold == sp.bold && p.italic == sp.italic && p.code == sp.code &&
				p.strike == sp.strike && p.link == sp.link {
				p.text += sp.text
				continue
			}
		}
		out = append(out, sp)
	}
	return out
}

// ---------------------------------------------------------------- 渲染

// mdStyle 是渲染一段文字时的基础样式（标题、引用会改它）。
type mdStyle struct {
	size   float32
	weight int
	color  myui.Color
}

// renderMarkdown 把 markdown 源文本渲染成 MyGo 原生 UI 元素树。
// 只支持 SKILL.md 里常见的子集，不认识的结构退化成普通段落，绝不 panic。
// 不自带滚动：外层已有 Scroll，这里只返回铺满宽度的内容列。
func renderMarkdown(c *myui.Context, src string) myui.Element {
	t := c.Theme()
	blocks := parseMarkdown(src)
	base := mdStyle{size: t.FontSize, weight: 400, color: t.Text}
	return myui.Column(c).FillWidth().MinWidth(0).Gap(t.Space(3)).Children(func() {
		mdRenderBlocks(c, blocks, base, true)
	})
}

// mdRenderBlocks 逐块渲染。连续的列表项收进一个紧凑的列（项间距比块间距小）。
func mdRenderBlocks(c *myui.Context, blocks []mdBlock, st mdStyle, top bool) {
	t := c.Theme()
	for i := 0; i < len(blocks); i++ {
		b := blocks[i]
		switch b.kind {
		case mdHeading:
			mdRenderHeading(c, b, st, top && i == 0)
		case mdParagraph:
			mdRenderInline(c, b.text, st)
		case mdListItem:
			j := i
			for j < len(blocks) && blocks[j].kind == mdListItem {
				j++
			}
			items := blocks[i:j]
			myui.Column(c).FillWidth().MinWidth(0).Gap(t.Space(1.5)).Children(func() {
				for _, it := range items {
					mdRenderItem(c, it, st)
				}
			})
			i = j - 1
		case mdCode:
			mdRenderCode(c, b)
		case mdQuote:
			mdRenderQuote(c, b, st)
		case mdRule:
			myui.Divider(c)
		case mdNotice:
			myui.Text(c, b.text).FontSize(t.FontSize - 1).TextColor(t.TextMuted)
		}
	}
}

// mdRenderHeading 画标题：级别越高字号越小，Bold；h1/h2 与上文多留一点距离。
func mdRenderHeading(c *myui.Context, b mdBlock, st mdStyle, first bool) {
	t := c.Theme()
	var delta float32
	switch b.level {
	case 1:
		delta = 6
	case 2:
		delta = 4
	case 3:
		delta = 2
	default:
		delta = 1
	}
	hs := mdStyle{size: t.FontSize + delta, weight: 700, color: st.color}
	if b.level >= 5 {
		hs.color = t.TextMuted // h5/h6 弱化，和 h4 区分
	}
	col := myui.Column(c).FillWidth().MinWidth(0)
	if !first {
		// 标题与上文留出距离，级别越高越大：段落块间距之外再补一点。
		pad := t.Space(0.5)
		switch {
		case b.level <= 2:
			pad = t.Space(2)
		case b.level == 3:
			pad = t.Space(1.5)
		}
		col.Padding(pad, 0, 0, 0)
	}
	col.Children(func() {
		mdRenderInline(c, b.text, hs)
	})
}

// mdRenderItem 画一个列表项：左侧固定宽度的符号，右侧正文占满余下宽度。
// 层级用左内边距体现。
func mdRenderItem(c *myui.Context, it mdBlock, st mdStyle) {
	t := c.Theme()
	marker := it.marker
	if marker == "•" {
		switch it.indent % 3 {
		case 1:
			marker = "◦"
		case 2:
			marker = "▪"
		}
	}
	// 符号列宽：至少 5 档间距，数字多时按字数加宽。
	w := t.Space(5)
	if n := utf8.RuneCountInString(marker); n > 2 {
		w += float32(n-2) * st.size * 0.6
	}
	left := float32(it.indent) * t.Space(5)
	myui.Row(c).FillWidth().MinWidth(0).Gap(t.Space(1)).Padding(0, 0, 0, left).Children(func() {
		myui.Box(c).Width(w).Shrink(0).Children(func() {
			myui.Text(c, marker).SingleLine().FontSize(st.size).TextColor(t.TextMuted)
		})
		myui.Column(c).Grow(1).MinWidth(0).Children(func() {
			mdRenderInline(c, it.text, st)
		})
	})
}

// mdRenderCode 画围栏代码块：t.Surface 底、描边、圆角、内边距，每行一个元素以
// 保留换行与前导空格（空行用一个空格占位，保证行高）。
func mdRenderCode(c *myui.Context, b mdBlock) {
	t := c.Theme()
	size := t.FontSize - 1
	myui.Column(c).FillWidth().MinWidth(0).Gap(0).
		Padding(t.Space(3), t.Space(4)).
		Background(t.Surface).Radius(t.Radius).Border(1, t.Border).Children(func() {
		if b.lang != "" {
			myui.Text(c, b.lang).SingleLine().FontSize(t.FontSize-3).TextColor(t.TextMuted).
				Padding(0, 0, t.Space(1.5), 0)
		}
		for _, ln := range b.lines {
			if ln == "" {
				ln = " "
			}
			// 制表符展开成 4 个空格：字体里 \t 的宽度不可预期。
			ln = strings.ReplaceAll(ln, "\t", "    ")
			myui.RichText(c, myui.Span{Text: ln, Font: mdMonoFont, Size: size, Color: t.Text})
		}
	})
}

// mdRenderQuote 画引用：左侧一条窄竖线，内容弱化色。
func mdRenderQuote(c *myui.Context, b mdBlock, st mdStyle) {
	t := c.Theme()
	qs := st
	qs.color = t.TextMuted
	myui.Row(c).FillWidth().MinWidth(0).AlignItems(myui.Stretch).Gap(t.Space(3)).Children(func() {
		myui.Box(c).Width(t.Space(0.75)).Shrink(0).Background(t.Border).Radius(t.Space(0.5))
		myui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(2)).Children(func() {
			mdRenderBlocks(c, b.children, qs, false)
		})
	})
}

// mdRenderInline 把一段含行内 markdown 的文字画出来。
// 没有 http 链接：一个 RichText 搞定，自动折行。
// 有链接：链接要用现成的 Link 元素（可点击），它不能嵌进 RichText，
// 所以按链接把段落切成「文字 / 链接 / 文字」，放进可换行的 Row。
func mdRenderInline(c *myui.Context, text string, st mdStyle) {
	spans := mdInline(text)
	if len(spans) == 0 {
		return
	}
	hasLink := false
	for _, sp := range spans {
		if sp.link != "" {
			hasLink = true
			break
		}
	}
	if !hasLink {
		myui.RichText(c, mdSpans(c, spans, st)...).FillWidth()
		return
	}
	myui.Row(c).FillWidth().MinWidth(0).Wrap().Children(func() {
		var run []mdSpan
		flush := func() {
			if len(run) > 0 {
				myui.RichText(c, mdSpans(c, run, st)...)
				run = nil
			}
		}
		for _, sp := range spans {
			if sp.link == "" {
				run = append(run, sp)
				continue
			}
			flush()
			myui.Link(c, sp.text, sp.link).FontSize(st.size)
		}
		flush()
	})
}

// mdSpans 把解析出的片段转成 myui.Span。
func mdSpans(c *myui.Context, in []mdSpan, st mdStyle) []myui.Span {
	t := c.Theme()
	out := make([]myui.Span, 0, len(in))
	for _, sp := range in {
		s := myui.Span{Text: sp.text, Size: st.size, Weight: st.weight, Color: st.color}
		if sp.bold && s.Weight < 700 {
			s.Weight = 700
		}
		if sp.italic {
			s.Italic = true
		}
		if sp.strike {
			s.Strikethrough = true
		}
		if sp.code {
			s.Font = mdMonoFont
			s.Background = t.Surface
			s.Size = st.size - 1
		}
		out = append(out, s)
	}
	return out
}
