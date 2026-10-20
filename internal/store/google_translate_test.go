package store

// 免费服务的测试只使用进程内 RoundTripper，不联网、不使用真实凭据。

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"
)

type googleTestTransport func(*http.Request) (*http.Response, error)

func (f googleTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func withGoogleTransport(t *testing.T, f googleTestTransport) {
	t.Helper()
	old := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: f}
	t.Cleanup(func() { http.DefaultClient = old })
}

func googleTestResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestGoogleDefaultTranslationAndLegacyCredentialIsolation(t *testing.T) {
	for _, s := range []Settings{
		{},
		{Provider: TranslationGoogle, TargetLang: "en-US",
			BaseURL: "https://private.invalid", APIKey: "fake-private-key", Model: "private-model"},
	} {
		t.Run(s.TargetLang, func(t *testing.T) {
			calls := 0
			withGoogleTransport(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet || r.URL.Scheme != "https" ||
					r.URL.Host != "translate.googleapis.com" || r.URL.Path != "/translate_a/single" {
					t.Fatalf("错误的 Google 请求目标: %s %s", r.Method, r.URL.Host)
				}
				q := r.URL.Query()
				wantLang := DefaultTargetLang
				if s.TargetLang == "en-US" {
					wantLang = "en"
				}
				if q.Get("q") != "Hello" || q.Get("tl") != wantLang ||
					q.Get("sl") != "auto" || q.Get("client") != "gtx" || q.Get("dj") != "1" || q.Get("dt") != "t" {
					t.Fatalf("请求参数不正确: %v", q)
				}
				if r.Header.Get("Authorization") != "" || strings.Contains(r.URL.String(), "private") {
					t.Fatal("免费翻译不能携带旧自定义配置")
				}
				return googleTestResponse(200, `{"sentences":[{"trans":"你"},{"trans":"好"}]}`), nil
			})
			got, err := s.Translate(context.Background(), "Hello")
			if err != nil || got != "你好" || calls != 1 {
				t.Fatalf("翻译结果 %q，错误 %v，请求次数 %d", got, err, calls)
			}
		})
	}
}

func TestGoogleChunkingPreservesUnicodeAndBoundaries(t *testing.T) {
	src := strings.Repeat("汉字🙂 alpha ", 550) + strings.Repeat("长", 1200)
	chunks := googleTextChunks(src)
	if strings.Join(chunks, "") != src {
		t.Fatal("分段改变了输入")
	}
	for _, chunk := range chunks {
		if len(chunk) > googleMaxTextBytes || !utf8.ValidString(chunk) {
			t.Fatalf("分段不是有效 UTF-8 或超出限制: %d bytes", len(chunk))
		}
	}
	calls := 0
	withGoogleTransport(t, func(r *http.Request) (*http.Response, error) {
		calls++
		q := r.URL.Query()
		if len(q.Get("q")) > googleMaxTextBytes || len(r.URL.String()) > 5000 {
			t.Fatal("单次 Google 请求过长")
		}
		if q.Get("tl") != "en" {
			t.Fatalf("en-GB 应规范成 en，得到 %q", q.Get("tl"))
		}
		body, _ := json.Marshal(map[string]any{"sentences": []map[string]string{{"trans": q.Get("q")}}})
		return googleTestResponse(200, string(body)), nil
	})
	got, err := googleTranslatePlain(context.Background(), "en-GB", src)
	if err != nil || got != src || calls < 2 {
		t.Fatalf("分段翻译未保留空白/内容: err=%v calls=%d equal=%v", err, calls, got == src)
	}
}

func TestGoogleErrorsDoNotExposeRequestOrResponse(t *testing.T) {
	const privateText = "private-original-text"
	cases := []struct {
		name   string
		status int
		body   string
		err    error
	}{
		{"限流", 429, privateText, nil},
		{"服务错误", 500, privateText, nil},
		{"响应损坏", 200, `{"private":` + privateText, nil},
		{"空译文", 200, `{"sentences":[{"orig":"` + privateText + `"}]}`, nil},
		{"网络错误", 0, "", &url.Error{Op: "Get", URL: "https://example.invalid/?q=" + privateText, Err: errors.New(privateText)}},
		{"响应过大", 200, strings.Repeat("x", googleMaxResponse+1), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			withGoogleTransport(t, func(*http.Request) (*http.Response, error) {
				calls++
				if tc.err != nil {
					return nil, tc.err
				}
				return googleTestResponse(tc.status, tc.body), nil
			})
			got, err := googleTranslatePlain(context.Background(), "zh-CN", privateText)
			if got != "" || err == nil || calls != 1 {
				t.Fatalf("失败应只请求一次并返回空结果: got=%q err=%v calls=%d", got, err, calls)
			}
			if strings.Contains(err.Error(), privateText) || strings.Contains(err.Error(), "q=") {
				t.Fatalf("错误泄漏了请求原文: %v", err)
			}
		})
	}
}

func TestGoogleRefusesRedirects(t *testing.T) {
	calls := 0
	withGoogleTransport(t, func(*http.Request) (*http.Response, error) {
		calls++
		r := googleTestResponse(http.StatusFound, "")
		r.Header.Set("Location", "https://another.invalid/?q=private")
		return r, nil
	})
	_, err := googleTranslatePlain(context.Background(), "zh-CN", "Hello")
	if err == nil || calls != 1 {
		t.Fatalf("重定向不得发送到第二个目标: err=%v calls=%d", err, calls)
	}
}

func TestGoogleCancellationPreservesIdentityWithoutText(t *testing.T) {
	started := make(chan struct{})
	withGoogleTransport(t, func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := googleTranslatePlain(ctx, "zh-CN", "private-original")
		result <- err
	}()
	<-started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "private-original") {
		t.Fatalf("取消必须保留身份且不泄漏原文: %v", err)
	}
}

func TestFreeTranslationUnknownProviderAndBadLanguageNeverRequest(t *testing.T) {
	withGoogleTransport(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("无效设置不应发请求")
		return nil, errors.New("unexpected request")
	})
	for _, s := range []Settings{
		{Provider: "future-provider"},
		{Provider: TranslationOpenAI},
		{APIKey: "fake-incomplete-key"},
		{Provider: TranslationGoogle, TargetLang: "unknown"},
	} {
		if _, err := s.Translate(context.Background(), "Hello"); err == nil {
			t.Fatal("无效设置应报错")
		}
	}
}
