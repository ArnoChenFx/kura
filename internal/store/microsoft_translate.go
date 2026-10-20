package store

// 微软免费翻译使用 Bing 翻译网页的匿名会话，不使用 Azure 密钥，也不读取
// 自定义翻译服务的配置。会话与 cookies 只保存在当前翻译调用内，不写磁盘。
// 页面协议改变、拒绝访问或要求验证时立即报错，由用户重试或切换服务。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/locales"
)

const (
	microsoftTranslateOrigin = "https://www.bing.com"
	microsoftMaxPageBytes    = 2 << 20
	microsoftMaxResponse     = 1 << 20
	// 网页目前按 1000 字符限制输入；以字节限制进一步避免多字节字符超限。
	microsoftMaxTextBytes = 900
)

var (
	microsoftPageIG    = regexp.MustCompile(`\bIG:"([A-Za-z0-9]+)"`)
	microsoftPagePanel = regexp.MustCompile(`<[^>]+\bid=["']rich_tta["'][^>]*>`)
	microsoftPageIID   = regexp.MustCompile(`\bdata-iid=["']([A-Za-z0-9_.-]+)["']`)
	microsoftPageAuth  = regexp.MustCompile(`\bparams_AbusePreventionHelper\s*=\s*(\[[^\]]+\])`)
)

type microsoftSession struct {
	client   *http.Client
	pageURL  string
	ig       string
	iid      string
	key      string
	token    string
	sequence int
}

// microsoftTranslatePlain 接收已保护格式的文本。单次调用只读取一次匿名网页，
// 长文本分段复用该会话；失败不返回部分译文，不自动重试 401、429 或验证码。
func microsoftTranslatePlain(ctx context.Context, lang, text string) (string, error) {
	switch lang {
	case "zh-CN":
		lang = "zh-Hans"
	case "en-US", "en-GB":
		lang = "en"
	}
	if strings.TrimSpace(text) == "" {
		return text, nil
	}
	if err := ctx.Err(); err != nil {
		return "", googlePrivateError(err, "storeErrTranslateDo", locales.StoreErrTranslateDo)
	}
	session, err := newMicrosoftSession(ctx, lang)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for _, part := range freeTranslationChunks(text, microsoftMaxTextBytes) {
		core := strings.TrimSpace(part)
		if core == "" {
			out.WriteString(part)
			continue
		}
		start := strings.Index(part, core)
		translated, err := session.translate(ctx, lang, core)
		if err != nil {
			return "", err
		}
		out.WriteString(part[:start])
		out.WriteString(translated)
		out.WriteString(part[start+len(core):])
	}
	return out.String(), nil
}

func newMicrosoftSession(ctx context.Context, lang string) (*microsoftSession, error) {
	// 复用环境的代理、证书和连接池，同时隔离匿名 cookies。重定向不跟随，
	// 避免页面/服务器把原文或会话令牌带到未经选择的服务。
	client := *http.DefaultClient
	client.Jar, _ = cookiejar.New(nil)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	s := &microsoftSession{
		client: &client,
		pageURL: microsoftTranslateOrigin + "/translator?" + url.Values{
			"ref": {"TThis"}, "from": {"auto-detect"}, "to": {lang},
		}.Encode(),
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.pageURL, nil)
	if err != nil {
		return nil, googlePrivateError(err, "storeErrTranslateRequest", locales.StoreErrTranslateRequest)
	}
	data, err := microsoftRequest(s.client, req, microsoftMaxPageBytes)
	if err != nil {
		return nil, err
	}
	page := string(data)
	ig := microsoftPageIG.FindStringSubmatch(page)
	panel := microsoftPagePanel.FindString(page)
	iid := microsoftPageIID.FindStringSubmatch(panel)
	auth := microsoftPageAuth.FindStringSubmatch(page)
	if len(ig) != 2 || len(iid) != 2 || len(auth) != 2 {
		return nil, microsoftSessionError()
	}
	var params []json.RawMessage
	if json.Unmarshal([]byte(auth[1]), &params) != nil || len(params) != 3 {
		return nil, microsoftSessionError()
	}
	var key int64
	if json.Unmarshal(params[0], &key) != nil || key <= 0 ||
		json.Unmarshal(params[1], &s.token) != nil || s.token == "" || len(s.token) > 4096 {
		return nil, microsoftSessionError()
	}
	s.ig, s.iid, s.key = ig[1], iid[1], strconv.FormatInt(key, 10)
	return s, nil
}

func (s *microsoftSession) translate(ctx context.Context, lang, text string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", googlePrivateError(err, "storeErrTranslateDo", locales.StoreErrTranslateDo)
	}
	s.sequence++
	endpoint := microsoftTranslateOrigin + "/ttranslatev3?" + url.Values{
		"isVertical": {"1"}, "IG": {s.ig}, "IID": {s.iid}, "SFX": {strconv.Itoa(s.sequence)},
	}.Encode()
	form := url.Values{
		"fromLang": {"auto-detect"}, "to": {lang}, "text": {text}, "key": {s.key}, "token": {s.token},
	}
	if s.sequence == 1 {
		form.Set("isFirstTurn", "true")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", googlePrivateError(err, "storeErrTranslateRequest", locales.StoreErrTranslateRequest)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", microsoftTranslateOrigin)
	req.Header.Set("Referer", s.pageURL)
	data, err := microsoftRequest(s.client, req, microsoftMaxResponse)
	if err != nil {
		return "", err
	}
	var response []struct {
		Translations []struct {
			Text string `json:"text"`
			To   string `json:"to"`
		} `json:"translations"`
	}
	if json.Unmarshal(data, &response) != nil {
		// 匿名会话不可用时有时返回验证码/拒绝对象，不去解析或解决挑战。
		return "", microsoftSessionError()
	}
	if len(response) != 1 {
		return "", microsoftSessionError()
	}
	for _, translation := range response[0].Translations {
		if translation.To == lang && strings.TrimSpace(translation.Text) != "" {
			return translation.Text, nil
		}
	}
	return "", i18n.NewError("storeErrTranslateEmpty", locales.StoreErrTranslateEmpty)
}

func microsoftRequest(client *http.Client, req *http.Request, limit int64) ([]byte, error) {
	return freeTranslationRequest(microsoftTranslationGate, client, req, limit)
}

func microsoftSessionError() error {
	return i18n.NewError("storeErrTranslateMicrosoftSession", locales.StoreErrTranslateMicrosoftSession)
}
