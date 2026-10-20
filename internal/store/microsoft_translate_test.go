package store

// 微软网页接口测试只走内存里的 HTTP transport；匿名会话参数均为自造值。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

const microsoftTestPage = `<html><script>var _G={IG:"ABC123"}; var params_AbusePreventionHelper = [1700000000000,"fake-anonymous-session",3600000];</script><div data-iid="unrelated.1"></div><div id="rich_tta" data-iid="translator.5023"></div></html>`

func microsoftTestTranslated(text, lang string) *http.Response {
	body, _ := json.Marshal([]map[string]any{{"translations": []map[string]string{{"text": text, "to": lang}}}})
	return googleTestResponse(http.StatusOK, string(body))
}

func TestMicrosoftTranslationSessionAndCredentialIsolation(t *testing.T) {
	calls := 0
	withGoogleTransport(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Scheme != "https" || r.URL.Host != "www.bing.com" || r.Header.Get("Authorization") != "" {
			t.Fatal("微软翻译必须使用固定 HTTPS 主机且不发送自定义密钥")
		}
		switch calls {
		case 1:
			if r.Method != http.MethodGet || r.URL.Path != "/translator" || r.URL.Query().Get("to") != "zh-Hans" {
				t.Fatalf("匿名网页请求错误: %s %s", r.Method, r.URL.Path)
			}
			if r.Header.Get("Cookie") != "" {
				t.Fatal("匿名会话不能继承默认客户端的 cookies")
			}
			response := googleTestResponse(200, microsoftTestPage)
			response.Header.Add("Set-Cookie", "test_session=fake-cookie; Path=/; Secure")
			return response, nil
		case 2:
			if r.Method != http.MethodPost || r.URL.Path != "/ttranslatev3" {
				t.Fatalf("翻译请求错误: %s %s", r.Method, r.URL.Path)
			}
			q := r.URL.Query()
			if q.Get("IG") != "ABC123" || q.Get("IID") != "translator.5023" || q.Get("SFX") != "1" {
				t.Fatalf("必须使用翻译控件的 IID 与独立 SFX，得到 %v", q)
			}
			if r.Header.Get("Origin") != microsoftTranslateOrigin || !strings.HasPrefix(r.Referer(), microsoftTranslateOrigin+"/translator?") {
				t.Fatal("缺少匿名页面的 Origin/Referer")
			}
			cookie, err := r.Cookie("test_session")
			if err != nil || cookie.Value != "fake-cookie" {
				t.Fatal("页面和翻译请求应共用同一匿名 cookiejar")
			}
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.PostForm.Get("text") != "Hello" || r.PostForm.Get("to") != "zh-Hans" ||
				r.PostForm.Get("fromLang") != "auto-detect" || r.PostForm.Get("token") != "fake-anonymous-session" ||
				r.PostForm.Get("key") != "1700000000000" || r.PostForm.Get("isFirstTurn") != "true" {
				t.Fatalf("微软翻译表单与页面契约不符: %v", r.PostForm)
			}
			if strings.Contains(r.URL.String(), "private") || strings.Contains(r.PostForm.Encode(), "private") {
				t.Fatal("免费翻译不能转发旧自定义服务的配置")
			}
			return microsoftTestTranslated("你好", "zh-Hans"), nil
		default:
			t.Fatal("匿名会话与翻译之外不应有额外请求")
			return nil, errors.New("unexpected request")
		}
	})
	s := &Settings{
		Provider: TranslationMicrosoft, TargetLang: "zh-CN",
		BaseURL: "https://private.invalid", APIKey: "fake-private-key", Model: "private-model",
	}
	got, err := s.Translate(context.Background(), "Hello")
	if err != nil || got != "你好" || calls != 2 {
		t.Fatalf("微软翻译未成功: got=%q err=%v calls=%d", got, err, calls)
	}
}

func TestMicrosoftLongTextUsesOneSessionAndBoundedChunks(t *testing.T) {
	src := "  " + strings.Repeat("Read 示例🙂 ", 400) + strings.Repeat("长", 1000) + "\n"
	pages, translations := 0, 0
	withGoogleTransport(t, func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			pages++
			if r.URL.Query().Get("to") != "en" {
				t.Fatal("英语地区代码应映射到微软 en")
			}
			return googleTestResponse(200, microsoftTestPage), nil
		}
		translations++
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		part := r.PostForm.Get("text")
		if len(part) > microsoftMaxTextBytes || !utf8.ValidString(part) || part == "" {
			t.Fatalf("请求片段不符合长度与 UTF-8 要求: %d bytes", len(part))
		}
		if r.URL.Query().Get("SFX") != strconv.Itoa(translations) ||
			(r.PostForm.Get("isFirstTurn") == "true") != (translations == 1) {
			t.Fatal("长文应复用会话并递增请求序号")
		}
		return microsoftTestTranslated(part, "en"), nil
	})
	got, err := microsoftTranslatePlain(context.Background(), "en-GB", src)
	if err != nil || got != src || pages != 1 || translations < 2 {
		t.Fatalf("分段未保留原始边界: err=%v equal=%v pages=%d translations=%d", err, got == src, pages, translations)
	}
}

func TestMicrosoftRejectsInvalidPagesWithoutSendingText(t *testing.T) {
	for _, page := range []string{
		`<html>Sign in or complete verification</html>`,
		strings.Replace(microsoftTestPage, `id="rich_tta"`, `id="another"`, 1),
		strings.Replace(microsoftTestPage, `1700000000000`, `"not-a-number"`, 1),
		strings.Replace(microsoftTestPage, `"fake-anonymous-session"`, `""`, 1),
		strings.Repeat("x", microsoftMaxPageBytes+1),
	} {
		t.Run(strconv.Itoa(len(page)), func(t *testing.T) {
			calls := 0
			withGoogleTransport(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet {
					t.Fatal("没有有效匿名会话时不能发送用户原文")
				}
				return googleTestResponse(200, page), nil
			})
			got, err := microsoftTranslatePlain(context.Background(), "zh-CN", "private-original")
			if got != "" || err == nil || calls != 1 {
				t.Fatalf("无效页面应明确失败且不再请求: got=%q err=%v calls=%d", got, err, calls)
			}
		})
	}
}

func TestMicrosoftFailurePrivacyAndNoRetry(t *testing.T) {
	const privateText = "private-original"
	for _, tc := range []struct {
		name   string
		status int
		body   string
		err    error
	}{
		{"会话被拒绝", 401, `{"ShowCaptcha":false,"echo":"` + privateText + `"}`, nil},
		{"限流", 429, privateText, nil},
		{"服务错误", 503, privateText, nil},
		{"要求验证", 200, `{"ShowCaptcha":true,"echo":"` + privateText + `"}`, nil},
		{"错误语言", 200, `[{"translations":[{"text":"wrong-language","to":"ja"}]}]`, nil},
		{"空译文", 200, `[{"translations":[{"text":"","to":"zh-Hans"}]}]`, nil},
		{"过大响应", 200, strings.Repeat("x", microsoftMaxResponse+1), nil},
		{"网络错误", 0, "", &url.Error{Op: "Post", URL: "https://example.invalid/?text=" + privateText, Err: errors.New("fake-anonymous-session")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			withGoogleTransport(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return googleTestResponse(200, microsoftTestPage), nil
				}
				if tc.err != nil {
					return nil, tc.err
				}
				return googleTestResponse(tc.status, tc.body), nil
			})
			got, err := microsoftTranslatePlain(context.Background(), "zh-CN", privateText)
			if got != "" || err == nil || calls != 2 {
				t.Fatalf("失败不得自动重试或返回部分译文: got=%q err=%v calls=%d", got, err, calls)
			}
			if strings.Contains(err.Error(), privateText) || strings.Contains(err.Error(), "fake-anonymous-session") {
				t.Fatalf("错误不能暴露正文或会话值: %v", err)
			}
		})
	}
}

func TestMicrosoftRefusesRedirectsAtEitherStage(t *testing.T) {
	for _, stage := range []int{1, 2} {
		t.Run(strconv.Itoa(stage), func(t *testing.T) {
			calls := 0
			withGoogleTransport(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if calls < stage {
					return googleTestResponse(200, microsoftTestPage), nil
				}
				resp := googleTestResponse(307, "")
				resp.Header.Set("Location", "https://another.invalid")
				return resp, nil
			})
			_, err := microsoftTranslatePlain(context.Background(), "zh-CN", "private-original")
			if err == nil || calls != stage {
				t.Fatalf("重定向不得跟随: err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestMicrosoftCancellationDuringPageOrTranslation(t *testing.T) {
	for _, stage := range []int{1, 2} {
		t.Run(strconv.Itoa(stage), func(t *testing.T) {
			started := make(chan struct{})
			calls := 0
			withGoogleTransport(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if calls < stage {
					return googleTestResponse(200, microsoftTestPage), nil
				}
				close(started)
				<-r.Context().Done()
				return nil, r.Context().Err()
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := microsoftTranslatePlain(ctx, "zh-CN", "private-original")
				done <- err
			}()
			<-started
			cancel()
			if err := <-done; !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "private-original") {
				t.Fatalf("取消应保留身份且不能泄漏原文: %v", err)
			}
		})
	}
}
