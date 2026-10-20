package store

// 免费文本翻译接口不理解 Markdown。这里先隐藏代码、链接目标和结构，
// 只把可读正文交给接口；占位符必须原样、有序返回，否则整段回退原文。
// 这是保守的保护层，不重写 Markdown 解析器：不确定的代码/HTML 行保留原样。

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/locales"
)

type translationRange struct{ start, end int }

var (
	translationPrefix = regexp.MustCompile(`^(?: {0,3}>[ \t]?)*(?: {0,3}(?:#{1,6}|[-+*]|[0-9]+[.)])[ \t]+)?`)
	translationRef    = regexp.MustCompile(`^ {0,3}\[[^]]+\]:`)
	translationList   = regexp.MustCompile(`^(?:[-+*]|[0-9]+[.)])[ \t]+`)
)

// translateGoogleMarkdown 的 translatePlain 只接收隐藏了受保护片段的文本。
// 整段一次翻译，避免逐词请求；HTTP 层仍可按长度拆请求，但不能拆开占位符。
func translateGoogleMarkdown(ctx context.Context, text string, translatePlain func(context.Context, string) (string, error)) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	ranges := protectedTranslationRanges(text)
	prefix := "ZXQKURA"
	for collision := 0; strings.Contains(text, prefix); collision++ {
		// 不按源文本长度一直追加 X，避免极端输入把标记撑到请求分块上限。
		prefix = "ZXQKURAX" + strconv.Itoa(collision) + "P"
	}
	var encoded strings.Builder
	tokens := make([]string, len(ranges))
	pos, hasProse := 0, false
	for i, r := range ranges {
		prose := text[pos:r.start]
		hasProse = hasProse || strings.ContainsFunc(prose, unicode.IsLetter)
		encoded.WriteString(prose)
		tokens[i] = prefix + strconv.Itoa(i) + "QXZ"
		encoded.WriteByte(' ')
		encoded.WriteString(tokens[i])
		encoded.WriteByte(' ')
		pos = r.end
	}
	tail := text[pos:]
	hasProse = hasProse || strings.ContainsFunc(tail, unicode.IsLetter)
	encoded.WriteString(tail)
	if !hasProse {
		return text, nil
	}
	out, err := translatePlain(ctx, encoded.String())
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(ranges) == 0 {
		return out, nil
	}
	// 多出来、被吞掉、被改名或重排的标记一律拒绝。错误不带原文或译文。
	if strings.Count(out, prefix) != len(tokens) {
		return "", translationMarkupError()
	}
	var restored strings.Builder
	pos = 0
	for i, token := range tokens {
		if strings.Count(out, token) != 1 {
			return "", translationMarkupError()
		}
		at := strings.Index(out[pos:], token)
		if at < 0 {
			return "", translationMarkupError()
		}
		part := out[pos : pos+at]
		// 标记旁的原始空白已在保护区内；去掉服务为标记补的分隔空格。
		if i > 0 {
			part = strings.TrimLeftFunc(part, unicode.IsSpace)
		}
		part = strings.TrimRightFunc(part, unicode.IsSpace)
		restored.WriteString(part)
		restored.WriteString(text[ranges[i].start:ranges[i].end])
		pos += at + len(token)
	}
	restored.WriteString(strings.TrimLeftFunc(out[pos:], unicode.IsSpace))
	return restored.String(), nil
}

func translationMarkupError() error {
	return i18n.NewError("storeErrTranslateMarkup", func() string { return locales.StoreErrTranslateMarkup() })
}

// protectedTranslationRanges 生成有序、互不重叠的保护区。相邻空白一起保留，
// 因此还原时可以安全移除翻译接口在占位符两边添加的空格。
func protectedTranslationRanges(src string) []translationRange {
	var ranges []translationRange
	add := func(start, end int) {
		for start > 0 && translationSpace(src[start-1]) {
			start--
		}
		for end < len(src) && translationSpace(src[end]) {
			end++
		}
		if n := len(ranges); n > 0 && start <= ranges[n-1].end {
			if end > ranges[n-1].end {
				ranges[n-1].end = end
			}
			return
		}
		ranges = append(ranges, translationRange{start, end})
	}
	offset := 0
	if end := translationFrontmatterEnd(src); end > 0 {
		add(0, end)
		offset = end
	}
	fence, htmlTag, htmlComment := "", "", false
	for offset < len(src) {
		end := strings.IndexByte(src[offset:], '\n')
		if end < 0 {
			end = len(src)
		} else {
			end += offset + 1
		}
		line := strings.TrimRight(src[offset:end], "\r\n")
		trimmed := strings.TrimSpace(line)
		body := strings.TrimLeft(line, " ")
		// 引用内的围栏同样视为代码；多层引用的 “>” 不发送给翻译服务。
		quotedCode := false
		for strings.HasPrefix(body, ">") {
			body = strings.TrimPrefix(body[1:], " ")
			quotedCode = quotedCode || strings.HasPrefix(body, "    ") || strings.HasPrefix(body, "\t")
			next := strings.TrimLeft(body, " ")
			if !strings.HasPrefix(next, ">") {
				break
			}
			body = next
		}
		if prefix := translationList.FindString(body); prefix != "" {
			body = body[len(prefix):]
		}
		switch {
		case fence != "":
			add(offset, end)
			if isClosingFence(body, fence) {
				fence = ""
			}
		case htmlComment:
			add(offset, end)
			htmlComment = !strings.Contains(line, "-->")
		case htmlTag != "":
			add(offset, end)
			if strings.Contains(strings.ToLower(line), "</"+htmlTag+">") {
				htmlTag = ""
			}
		case translationRawHTMLTag(trimmed) != "":
			htmlTag = translationRawHTMLTag(trimmed)
			add(offset, end)
			if strings.Contains(strings.ToLower(line), "</"+htmlTag+">") {
				htmlTag = ""
			}
		case strings.Contains(trimmed, "<!--"):
			add(offset, end)
			htmlComment = !strings.Contains(trimmed, "-->")
		case fenceMarker(body) != "":
			fence = fenceMarker(body)
			add(offset, end)
		case strings.HasPrefix(line, "    "), strings.HasPrefix(line, "\t"), quotedCode,
			translationRef.MatchString(line), translationRuleLine(trimmed),
			strings.HasPrefix(trimmed, "<"):
			add(offset, end)
		default:
			if prefix := translationPrefix.FindString(line); prefix != "" {
				add(offset, offset+len(prefix))
			}
			for i := 0; i < len(line); {
				// 上一行开始的行内代码可能跨行；闭合反引号不能再当作开头。
				if n := len(ranges); n > 0 && offset+i < ranges[n-1].end {
					i = ranges[n-1].end - offset
					continue
				}
				n := protectedInlineLength(line[i:])
				if line[i] == '`' {
					if span := protectedCodeSpanLength(src[offset+i:]); span > 0 {
						n = span
					}
				}
				if n > 0 {
					add(offset+i, offset+i+n)
					i += n
				} else {
					_, n = utf8.DecodeRuneInString(line[i:])
					i += n
				}
			}
			if eol := offset + len(line); eol < end {
				add(eol, end)
			}
		}
		offset = end
	}
	return ranges
}

func translationSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }

func translationRawHTMLTag(line string) string {
	line = strings.ToLower(line)
	for _, tag := range []string{"pre", "script", "style", "textarea"} {
		prefix := "<" + tag
		if strings.HasPrefix(line, prefix) && len(line) > len(prefix) {
			next := line[len(prefix)]
			if next == '>' || next == ' ' || next == '\t' {
				return tag
			}
		}
	}
	return ""
}

func translationFrontmatterEnd(src string) int {
	// frontmatter 只在文档开头认；没有闭合标记则保守保留全部。
	first, rest, ok := strings.Cut(src, "\n")
	if !ok || strings.TrimSpace(strings.TrimPrefix(first, "\uFEFF")) != "---" {
		return 0
	}
	offset := len(first) + 1
	for rest != "" {
		line, next, more := strings.Cut(rest, "\n")
		offset += len(line)
		if more {
			offset++
		}
		if t := strings.TrimSpace(line); t == "---" || t == "..." {
			return offset
		}
		rest = next
	}
	return len(src)
}

func translationRuleLine(line string) bool {
	if line == "" {
		return true
	}
	for _, r := range line {
		if !strings.ContainsRune("-=*_~|: \t", r) {
			return false
		}
	}
	return true
}

// protectedInlineLength 按字节返回当前起点的受保护片段长度。遇到未闭合的
// 行内代码/链接目标时保留行尾，宁可少翻一句，也不改坏可执行内容。
func protectedInlineLength(s string) int {
	switch s[0] {
	case '\\':
		if len(s) > 1 {
			_, n := utf8.DecodeRuneInString(s[1:])
			return 1 + n
		}
		return 1
	case '`':
		if n := protectedCodeSpanLength(s); n > 0 {
			return n
		}
		return len(s)
	case '[':
		// 无目标的方括号可能是引用式链接/脚注；保守保留引用名。
		if end := strings.IndexByte(s, ']'); end >= 0 {
			if end+1 == len(s) || s[end+1] != '(' && s[end+1] != '[' {
				return end + 1
			}
		}
		return 1
	case ']':
		if len(s) > 1 && s[1] == '(' {
			depth := 1
			for i := 2; i < len(s); i++ {
				switch s[i] {
				case '\\':
					i++
				case '(':
					depth++
				case ')':
					depth--
					if depth == 0 {
						return i + 1
					}
				}
			}
			return len(s)
		}
		if strings.HasPrefix(s, "][") {
			if end := strings.IndexByte(s[2:], ']'); end >= 0 {
				return end + 3
			}
		}
		return 1
	case '<':
		if end := strings.IndexByte(s, '>'); end >= 0 {
			return end + 1
		}
	case '*', '_', '~', '|', '!', '#', '(', ')', '>':
		return 1
	}
	if strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://") ||
		strings.HasPrefix(s, "mailto:") || strings.HasPrefix(s, "www.") {
		if end := strings.IndexAny(s, " \t\r\n<>\""); end >= 0 {
			return end
		}
		return len(s)
	}
	return 0
}

func protectedCodeSpanLength(s string) int {
	n := len(s) - len(strings.TrimLeft(s, "`"))
	for pos := n; pos < len(s); {
		next := strings.IndexByte(s[pos:], '`')
		if next < 0 {
			break
		}
		pos += next
		run := len(s[pos:]) - len(strings.TrimLeft(s[pos:], "`"))
		if run == n {
			return pos + n
		}
		pos += run
	}
	return 0
}
