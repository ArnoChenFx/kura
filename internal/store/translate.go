package store

// translate.go — 翻译服务派发与 OpenAI 兼容客户端。
//
// 职责边界：仅把文本发给用户选择的翻译服务，返回译文。
// 翻译结果**只返回给调用方（UI）显示**，本文件绝不写技能目录、绝不改
// SKILL.md、绝不碰 .kura-meta.json——「翻译不落盘」是用户拍板的硬规则。
//
// 实现只用标准库 net/http + context，不引第三方依赖。http.Client 用包级
// 默认（零值复用连接池），超时**不由 Client.Timeout 兜底**而是交给调用方的
// ctx：翻译是用户主动触发的动作，UI 会挂自己的超时与取消（文案可长可短，
// 库层面拍死一个超时反而让长文本无法完成）。ctx 超时/取消时请求会被中断，
// 连接由 http 层回收。

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/locales"
)

// errSnippetMax 是非 2xx 时错误信息里携带的响应体最大字节数。截断是为了
// 不把巨大的 HTML 错误页整页塞进错误信息（弹窗显示不了，日志也难看）。
const errSnippetMax = 512

// maxResponseBytes 是单次读取响应体的上限（10MB）。正常译文远小于它；
// 上限只防御异常响应（如网关把错误页渲染成几百 MB）把内存吃爆。
// 超限的响应会被截断，JSON 解析会报错，调用方看到的是可理解的错误。
const maxResponseBytes = 10 << 20

// chatRequest 是 /chat/completions 的请求体：只放需要的字段。temperature
// 固定 0：翻译要的是稳定、可复现的输出，不要创造性。
type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

// chatMessage 是一条对话消息（system 提示或 user 正文）。
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatResponse 是 /chat/completions 响应里我们关心的部分；其余字段
// （usage、finish_reason 等）一律忽略，避免绑一整套大结构体。
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// systemPrompt 是发给模型的系统提示。要求写死：
//   - 只输出译文本体，不要解释、不要前后缀、不要加引号；
//   - 保持 markdown 结构（标题/列表/代码块围栏）不变；
//   - 代码块内容不翻译——技能文档里的命令、配置、代码翻译了会失效。
//
// 这是发给**模型**的指令，不是界面文案：它必须稳定、可测试，不能随界面语言
// 变，所以有意留在代码里而不进 catalog（catalog 只管用户能看到的文字）。
// translate.go 因此被 internal/i18n 的 TestNoHardcodedCJK 列入允许清单。
const systemPrompt = "你是一名技术文档翻译引擎。把用户发来的内容完整翻译成目标语言，" +
	"严格遵守：\n" +
	"1. 只输出译文，不要任何解释、说明或前后缀，也不要给译文加引号。\n" +
	"2. 保持 markdown 结构不变：标题层级、列表、表格、链接、粗体斜体都照原样保留。\n" +
	"3. 围栏代码块（```）的内容保持原样，绝对不要翻译代码、命令、路径、配置与标识符。\n" +
	"4. front matter（--- 包围的部分）保持原样，不要翻译。\n" +
	"5. 术语要自然、符合目标语言的技术写作习惯。"

// Translate 按所选服务把 text 翻译成 s.TargetLang（空串按 DefaultTargetLang）。
// 免费服务先保护 Markdown 与代码；已有自定义接口仍保留原来的模型请求。
//
//   - ctx 用来取消与超时，调用方负责挂超时；本实现不额外设 Client.Timeout。
//   - s.Ready() 为假时**不发请求**，返回 ErrNotConfigured（errors.Is 可判断）。
//   - text 为空串时返回 ("", nil)——空文本没有翻译价值，省一次 API 调用。
//   - 失败返回以「store: 」开头的错误（文字随界面语言变，见 catalog）。
//     错误信息里**绝不包含 API key**（请求头从不打印）。
func (s *Settings) Translate(ctx context.Context, text string) (string, error) {
	if !s.Ready() {
		return "", ErrNotConfigured
	}
	if strings.TrimSpace(text) == "" {
		return "", nil
	}
	lang, err := NormalizeTargetLang(s.TargetLang)
	if err != nil {
		return "", err
	}
	switch s.EffectiveProvider() {
	case TranslationGoogle:
		return translateGoogleMarkdown(ctx, text, func(ctx context.Context, text string) (string, error) {
			return googleTranslatePlain(ctx, lang, text)
		})
	case TranslationMicrosoft:
		return translateGoogleMarkdown(ctx, text, func(ctx context.Context, text string) (string, error) {
			return microsoftTranslatePlain(ctx, lang, text)
		})
	default:
		return s.translateOpenAI(ctx, lang, text)
	}
}

// translateOpenAI 保留已有 /chat/completions 行为，免费服务不会走这里，
// 所以保存过的自定义 API 密钥不会被带到 Google 或微软。
func (s *Settings) translateOpenAI(ctx context.Context, lang, text string) (string, error) {
	// 端点：BaseURL 去掉尾部斜杠后拼 /chat/completions。
	endpoint := strings.TrimRight(s.BaseURL, "/") + "/chat/completions"

	body, err := json.Marshal(chatRequest{
		Model: s.Model,
		Messages: []chatMessage{
			// 目标语言在系统提示里点名，避免和正文混在一起被当成待译内容。
			// 「目标语言：」是系统提示的一部分（发给模型的指令），同样留在代码里。
			{Role: "system", Content: systemPrompt + "目标语言：" + lang + "。"},
			{Role: "user", Content: text},
		},
		Temperature: 0,
	})
	if err != nil {
		// 结构体是静态的，正常不会走到这里；兜住并给出目录里的错误。
		return "", i18n.Wrapf(err, "storeErrTranslateRequest", func() string { return locales.StoreErrTranslateRequest() })
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		// URL 非法等构造期错误；带上端点便于用户发现 BaseURL 填错。
		return "", i18n.Wrapf(err, "storeErrTranslateEndpoint", func() string { return locales.StoreErrTranslateEndpoint(endpoint) })
	}
	req.Header.Set("Content-Type", "application/json")
	// Authorization 只进请求头；本函数的任何错误路径都不会打印 req。
	req.Header.Set("Authorization", "Bearer "+s.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// 网络错误 / ctx 超时：原样包装，带上原因（*url.Error 已含取消语义）。
		// 不打印 req（里面有 Authorization 头）。
		return "", i18n.Wrapf(err, "storeErrTranslateDo", func() string { return locales.StoreErrTranslateDo() })
	}
	defer resp.Body.Close() //nolint:errcheck // 只读响应体，关闭失败无可恢复

	// 一口气读完响应体（限长 maxResponseBytes 防御异常大响应）。
	// 注意不能先读 512 字节判状态码再读剩余：Reader 已被消费，剩余部分
	// 会是空，短响应体会被截成空串导致 JSON 解析失败（这里曾有此 bug）。
	full, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", i18n.Wrapf(err, "storeErrTranslateRead", func() string { return locales.StoreErrTranslateRead() })
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// 错误信息只带响应体前 errSnippetMax 字节：够看清 401/429 的 JSON
		// 错误说明，又不会把整页 HTML 错误页塞进弹窗。
		return "", i18n.NewError("storeErrTranslateStatus", func() string { return locales.StoreErrTranslateStatus(resp.StatusCode, snippetOf(full)) })
	}

	var cr chatResponse
	if err := json.Unmarshal(full, &cr); err != nil {
		// 响应不是合法 JSON（常见于网关返回 HTML 或被代理劫持的页面）。
		return "", i18n.Wrapf(err, "storeErrTranslateParse", func() string { return locales.StoreErrTranslateParse() })
	}
	if len(cr.Choices) == 0 {
		// 合法 JSON 但没有 choices：模型拒绝输出、或服务端行为异常。
		return "", i18n.NewError("storeErrTranslateNoChoices", func() string { return locales.StoreErrTranslateNoChoices() })
	}
	content := cr.Choices[0].Message.Content
	if strings.TrimSpace(content) == "" {
		// 有 choices 但内容为空：不把空串当成功返回，UI 会当成翻译消失。
		return "", i18n.NewError("storeErrTranslateEmpty", func() string { return locales.StoreErrTranslateEmpty() })
	}
	return content, nil
}

// snippetOf 把原始响应体整理成适合放进错误信息的一行文本：截断到
// errSnippetMax 字节、丢掉控制字符，防止错误信息里出现换行嵌套。
func snippetOf(data []byte) string {
	// data 在调用处已经 LimitReader 到 errSnippetMax，这里再做一次字节级
	// 截断保护（防将来调用方改动后忘了上游限长）。
	if len(data) > errSnippetMax {
		data = data[:errSnippetMax]
	}
	var b strings.Builder
	for _, r := range string(data) {
		if r == '\n' || r == '\r' || r == '\t' {
			b.WriteByte(' ')
			continue
		}
		if r < 0x20 {
			continue
		}
		b.WriteRune(r)
	}
	out := b.String()
	out = strings.TrimSpace(out)
	if len(data) >= errSnippetMax && out != "" {
		out += locales.StoreTermTruncated()
	}
	return out
}

// SplitForTranslation 把长文本（技能的 SKILL.md 全文）切成若干段，每段尽量不超过
// maxRunes 个字符，供调用方逐段调用 Translate：一份文档往往远超一次请求能处理
// 的长度。切分只发生在空行处，且不切开代码、frontmatter 等跨行保护区，
// 所以每段仍保留保护所需的上下文，译文按空行拼回去结构不变。
// 单个块本身超过上限时整块保留，不硬切。空白文本返回 nil。
func SplitForTranslation(text string, maxRunes int) []string {
	if maxRunes <= 0 {
		maxRunes = 1
	}
	var (
		out     []string
		cur     []string
		curRune int
	)
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.Join(cur, "\n\n"))
			cur, curRune = nil, 0
		}
	}
	for _, block := range markdownBlocks(text) {
		n := utf8.RuneCountInString(block)
		if curRune > 0 && curRune+n > maxRunes {
			flush()
		}
		cur = append(cur, block)
		curRune += n
	}
	flush()
	return out
}

// markdownBlocks 按空行把 text 切成块，完整文档保护区内的空行不算分界。
// 保护必须先于分段，否则列表围栏、HTML 或元数据的后半段会被误认成正文。
// 每块去掉首尾的空行；全是空白的块不返回。
func markdownBlocks(text string) []string {
	// 保护区包含相邻空白；去掉这些边缘空白后再检查分界，避免普通段落的
	// 换行标记把全篇错误地合成一块。区间内部的空行仍然不可切开。
	ranges := protectedTranslationRanges(text)
	kept := ranges[:0]
	for _, r := range ranges {
		part := text[r.start:r.end]
		r.start += len(part) - len(strings.TrimLeft(part, " \t\r\n"))
		r.end -= len(part) - len(strings.TrimRight(part, " \t\r\n"))
		if r.start < r.end {
			kept = append(kept, r)
		}
	}
	ranges = kept
	var (
		blocks []string
		cur    []string
		offset int
		active int
	)
	emit := func() {
		// 去掉首尾空行后整块保留（缩进与代码内容都原样）。
		start, end := 0, len(cur)
		for start < end && strings.TrimSpace(cur[start]) == "" {
			start++
		}
		for end > start && strings.TrimSpace(cur[end-1]) == "" {
			end--
		}
		if start < end {
			blocks = append(blocks, strings.Join(cur[start:end], "\n"))
		}
		cur = cur[:0]
	}
	for _, line := range strings.Split(text, "\n") {
		start := offset
		offset += len(line) + 1
		for active < len(ranges) && ranges[active].end <= start {
			active++
		}
		protected := active < len(ranges) && ranges[active].start < start &&
			ranges[active].end > start+len(line)
		if strings.TrimSpace(line) == "" && !protected {
			emit()
			continue
		}
		cur = append(cur, line)
	}
	emit()
	return blocks
}

// fenceMarker 按 CommonMark 判断 line 是不是开围栏，是则返回围栏标记（至少
// 三个 ` 或 ~），不是返回空串。围栏行可以有 0–3 个前导空格；4 个及以上空格
// （或制表符）是缩进代码块，不算围栏。反引号围栏的 info 串里不能再有反引号
// （否则那是行内代码）。
func fenceMarker(line string) string {
	rest, ok := trimFenceIndent(line)
	if !ok {
		return ""
	}
	ch := rest[:1]
	if ch != "`" && ch != "~" {
		return ""
	}
	n := len(rest) - len(strings.TrimLeft(rest, ch))
	if n < 3 {
		return ""
	}
	if ch == "`" && strings.Contains(rest[n:], "`") {
		return ""
	}
	return strings.Repeat(ch, n)
}

// isClosingFence 报告 line 是否闭合标记为 fence 的围栏：缩进不超过 3 个空格，
// 同种标记字符、长度不短于开围栏，其后只能是空白。
func isClosingFence(line, fence string) bool {
	rest, ok := trimFenceIndent(line)
	if !ok {
		return false
	}
	run := strings.TrimLeft(rest, fence[:1])
	n := len(rest) - len(run)
	return n >= len(fence) && strings.TrimSpace(run) == ""
}

// trimFenceIndent 去掉行首至多 3 个空格；缩进更深（4 个及以上空格）、以制表符
// 缩进或整行为空时 ok 为假。
func trimFenceIndent(line string) (rest string, ok bool) {
	rest = strings.TrimLeft(line, " ")
	if len(line)-len(rest) > 3 || rest == "" || rest[0] == '\t' {
		return "", false
	}
	return rest, true
}
