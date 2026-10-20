package store

// Google 网页翻译只用于用户主动切到译文后的请求。服务地址固定，不读取旧
// 自定义接口的地址、密钥或模型。网页接口可能限流，失败保留原文并由用户重试。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/locales"
)

const (
	googleTranslateEndpoint = "https://translate.googleapis.com/translate_a/single"
	// 最坏情况下 URL 编码将每个字节扩成三个字符；每段 1500 字节让完整
	// URL 仍远小于常见代理的 8 KB 上限。切分不截断 UTF-8 或 Markdown 标记。
	googleMaxTextBytes = 1500
	googleMaxResponse  = 1 << 20
)

// googleTranslatePlain 接收已保护 Markdown 的文本，分段请求并保留段间空白。
// 失败时不返回部分译文，防止上层把缺少占位符的片段当成完整文档。
func googleTranslatePlain(ctx context.Context, lang, text string) (string, error) {
	if lang == "en-US" || lang == "en-GB" {
		lang = "en"
	}
	var out strings.Builder
	for _, part := range googleTextChunks(text) {
		if err := ctx.Err(); err != nil {
			return "", googlePrivateError(err, "storeErrTranslateDo", locales.StoreErrTranslateDo)
		}
		core := strings.TrimSpace(part)
		if core == "" {
			out.WriteString(part)
			continue
		}
		start := strings.Index(part, core)
		translated, err := googleTranslateChunk(ctx, lang, core)
		if err != nil {
			return "", err
		}
		out.WriteString(part[:start])
		out.WriteString(translated)
		out.WriteString(part[start+len(core):])
	}
	return out.String(), nil
}

// googleTextChunks 优先在空白处分段；只有超长连续词才按字符边界切开。
// Markdown 保护标记两侧始终有空格，因此不会从标记内部切开。
func googleTextChunks(text string) []string {
	return freeTranslationChunks(text, googleMaxTextBytes)
}

// freeTranslationChunks 供两个免费服务按各自长度限制拆分文本。
func freeTranslationChunks(text string, maxBytes int) []string {
	maxBytes = max(maxBytes, utf8.UTFMax)
	var parts []string
	for len(text) > maxBytes {
		cut := maxBytes
		for !utf8.RuneStart(text[cut]) {
			cut--
		}
		space := 0
		for i, r := range text[:cut] {
			if unicode.IsSpace(r) {
				space = i + utf8.RuneLen(r)
			}
		}
		if space > 0 {
			cut = space
		}
		parts = append(parts, text[:cut])
		text = text[cut:]
	}
	if text != "" {
		parts = append(parts, text)
	}
	return parts
}

func googleTranslateChunk(ctx context.Context, lang, text string) (string, error) {
	query := url.Values{
		"client": {"gtx"},
		"sl":     {"auto"},
		"tl":     {lang},
		"dt":     {"t"},
		"dj":     {"1"},
		"q":      {text},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, googleTranslateEndpoint+"?"+query.Encode(), nil)
	if err != nil {
		return "", googlePrivateError(err, "storeErrTranslateRequest", locales.StoreErrTranslateRequest)
	}
	// 拷贝默认客户端以复用代理、证书与连接池；不改全局配置。拒绝重定向，
	// 避免文本由返回的 Location 带到另一个主机，也不携带自定义 API 密钥。
	client := *http.DefaultClient
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	data, err := freeTranslationRequest(googleTranslationGate, &client, req, googleMaxResponse)
	if err != nil {
		return "", err
	}
	var body struct {
		Sentences []struct {
			Translation string `json:"trans"`
		} `json:"sentences"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return "", i18n.NewError("storeErrTranslateParse", locales.StoreErrTranslateParse)
	}
	var out strings.Builder
	for _, sentence := range body.Sentences {
		out.WriteString(sentence.Translation)
	}
	if strings.TrimSpace(out.String()) == "" {
		return "", i18n.NewError("storeErrTranslateEmpty", locales.StoreErrTranslateEmpty)
	}
	return out.String(), nil
}

// googlePrivateError 不公开 URL、请求原文或响应正文。取消和超时仍可由
// errors.Is 判断，其余网络错误只返回稳定的本地化说明。
func googlePrivateError(err error, key string, render func() string) error {
	switch {
	case errors.Is(err, context.Canceled):
		return i18n.WithSentinel(context.Canceled, key, render)
	case errors.Is(err, context.DeadlineExceeded):
		return i18n.WithSentinel(context.DeadlineExceeded, key, render)
	default:
		return i18n.NewError(key, render)
	}
}
