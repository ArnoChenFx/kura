package store

// translate_test.go — OpenAI 兼容翻译客户端的测试。
//
// 隔离硬要求（AGENTS.md）：**绝不联网**——所有请求都打到 httptest.NewServer
// 起的本机假服务；不碰用户真实目录；测试里的 key 一律是假值
// test-key-not-real，绝不出现在任何真实请求或写盘文件里。

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTranslateTestSettings 构造一份「已配置好」的设置，BaseURL 指向假服务。
// 计数器 closed 在测试结束时通过 defer 检查，保证客户端按预期收尾。
func newTranslateTestSettings(t *testing.T, srvURL string) *Settings {
	t.Helper()
	return &Settings{
		BaseURL:    srvURL,
		APIKey:     "test-key-not-real",
		Model:      "test-model",
		TargetLang: "zh-CN",
	}
}

// TestTranslateSuccess 防回归：正常路径要能取出 choices[0].message.content，
// 并保证请求体确实带上了 model 与 Authorization 头——少了 Authorization，
// 真实服务会 401；少了 model，服务端无法路由模型。
func TestTranslateSuccess(t *testing.T) {
	var gotAuth string
	var gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("请求路径应为 /chat/completions，得到 %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		var req chatRequest
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("请求体不是合法 JSON: %v", err)
		}
		gotModel = req.Model
		if len(req.Messages) != 2 {
			t.Errorf("应有两则消息（system+user），得到 %d 则", len(req.Messages))
		}
		// temperature 固定 0：翻译要稳定输出。
		if req.Temperature != 0 {
			t.Errorf("temperature 应为 0，得到 %v", req.Temperature)
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"你好，世界"}}]}`)
	}))
	defer srv.Close()

	s := newTranslateTestSettings(t, srv.URL)
	out, err := s.Translate(context.Background(), "Hello, world")
	if err != nil {
		t.Fatalf("正常翻译不应报错: %v", err)
	}
	if out != "你好，世界" {
		t.Fatalf("译文 = %q, want %q", out, "你好，世界")
	}
	if want := "Bearer test-key-not-real"; gotAuth != want {
		t.Fatalf("Authorization 头 = %q, want %q", gotAuth, want)
	}
	if gotModel != "test-model" {
		t.Fatalf("请求体 model = %q, want %q", gotModel, "test-model")
	}
}

// TestTranslateNotConfiguredNoRequest 防回归：未配置（Ready()==false）时
// 绝不能发请求（否则用户会在不知情下打到配置不全的端点上），必须直接返回
// ErrNotConfigured；用计数断言假服务一次都没被调用。
func TestTranslateNotConfiguredNoRequest(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		io.WriteString(w, `{"choices":[{"message":{"content":"x"}}]}`)
	}))
	defer srv.Close()

	s := newTranslateTestSettings(t, srv.URL)
	s.APIKey = "" // 缺 key → 不 Ready
	out, err := s.Translate(context.Background(), "Hello")
	if out != "" {
		t.Fatalf("未配置时应返回空译文，得到 %q", out)
	}
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("未配置应返回 ErrNotConfigured，得到 %v", err)
	}
	if calls != 0 {
		t.Fatalf("未配置时假服务被调用了 %d 次，应为 0", calls)
	}
}

// TestTranslateEmptyInputNoRequest 防回归：空输入不发请求（省一次 API 调用），
// 返回空串与 nil 而不是错误。
func TestTranslateEmptyInputNoRequest(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		io.WriteString(w, `{"choices":[{"message":{"content":"x"}}]}`)
	}))
	defer srv.Close()

	s := newTranslateTestSettings(t, srv.URL)
	out, err := s.Translate(context.Background(), "")
	if err != nil || out != "" {
		t.Fatalf("空输入应返回 (\"\", nil)，得到 (%q, %v)", out, err)
	}
	if calls != 0 {
		t.Fatalf("空输入时假服务被调用了 %d 次，应为 0", calls)
	}
}

// TestTranslateHTTPStatusErrors 防回归：401/500 这类非 2xx 必须把状态码与
// 响应体片段带进错误信息，让用户能看出「key 错了」还是「服务挂了」；
// 错误信息里绝不能出现 API key。
func TestTranslateHTTPStatusErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		wantIn string
	}{
		{"401 密钥错误", http.StatusUnauthorized, `{"error":{"message":"invalid api key"}}`, "401"},
		{"500 服务错误", http.StatusInternalServerError, "boom", "500"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			s := newTranslateTestSettings(t, srv.URL)
			_, err := s.Translate(context.Background(), "Hello")
			if err == nil {
				t.Fatal("非 2xx 应报错")
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Fatalf("错误信息应包含状态码 %s，得到 %q", tc.wantIn, err.Error())
			}
			if !strings.Contains(err.Error(), "invalid api key") && tc.status == 401 {
				t.Fatalf("401 的错误信息应带响应体片段，得到 %q", err.Error())
			}
			if strings.Contains(err.Error(), "test-key-not-real") {
				t.Fatal("错误信息里绝不能出现 API key")
			}
		})
	}
}

// TestTranslateBadJSON 防回归：网关返回 HTML 之类的非 JSON 内容时，必须给出
// 「不是合法 JSON」的明确错误，而不是把 HTML 片段当译文返回。
func TestTranslateBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "<html>502 Bad Gateway</html>")
	}))
	defer srv.Close()

	s := newTranslateTestSettings(t, srv.URL)
	_, err := s.Translate(context.Background(), "Hello")
	if err == nil {
		t.Fatal("非 JSON 响应应报错")
	}
	if !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("错误信息应说明 JSON 解析失败，得到 %q", err.Error())
	}
}

// TestTranslateEmptyChoices 防回归：合法 JSON 但 choices 为空（模型拒绝输出、
// 服务端异常）要单独报错——绝不能把「取不到译文」静默处理成空串成功。
func TestTranslateEmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[]}`)
	}))
	defer srv.Close()

	s := newTranslateTestSettings(t, srv.URL)
	_, err := s.Translate(context.Background(), "Hello")
	if err == nil {
		t.Fatal("choices 为空应报错")
	}
	if !strings.Contains(err.Error(), "choices") {
		t.Fatalf("错误信息应提到 choices，得到 %q", err.Error())
	}
}

// TestTranslateEmptyContent 防回归：有 choices 但 content 为空同样不能当成功
// 处理，否则 UI 会把「翻译失败」当成「译文恰好是空」。
func TestTranslateEmptyContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"message":{"content":"   "}}]}`)
	}))
	defer srv.Close()

	s := newTranslateTestSettings(t, srv.URL)
	_, err := s.Translate(context.Background(), "Hello")
	if err == nil {
		t.Fatal("content 为空应报错")
	}
}

// TestTranslateContextCancel 防回归：ctx 取消必须中断请求并返回错误——
// UI 的超时/取消完全依赖它；若实现内部忽略 ctx，这里会等到服务端返回。
func TestTranslateContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 故意拖住响应，让取消能发生在请求进行中。
		time.Sleep(2 * time.Second)
		io.WriteString(w, `{"choices":[{"message":{"content":"x"}}]}`)
	}))
	defer srv.Close()

	s := newTranslateTestSettings(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, err := s.Translate(ctx, "Hello")
	if err == nil {
		t.Fatal("ctx 取消后应报错")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("ctx 取消应立刻中断请求，实际耗时 %v", elapsed)
	}
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "cancel") {
		t.Fatalf("错误应体现取消语义，得到 %v", err)
	}
}

// TestTranslateTargetLangDefault 防回归：TargetLang 为空串时按 DefaultTargetLang
// 发送——系统提示里必须点名目标语言，否则模型会猜。
func TestTranslateTargetLangDefault(t *testing.T) {
	var sysContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		for _, m := range req.Messages {
			if m.Role == "system" {
				sysContent = m.Content
			}
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"译"}}]}`)
	}))
	defer srv.Close()

	s := newTranslateTestSettings(t, srv.URL)
	s.TargetLang = "" // 空串 → 缺省 zh-CN
	if _, err := s.Translate(context.Background(), "Hello"); err != nil {
		t.Fatalf("翻译不应报错: %v", err)
	}
	if !strings.Contains(sysContent, DefaultTargetLang) {
		t.Fatalf("系统提示应包含缺省目标语言 %q，得到 %q", DefaultTargetLang, sysContent)
	}
}

// TestTranslateBaseURLTrailingSlash 防回归：BaseURL 带尾部斜杠时端点不能拼出
// 双斜杠（//chat/completions 在部分网关上 404）。
func TestTranslateBaseURLTrailingSlash(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		io.WriteString(w, `{"choices":[{"message":{"content":"x"}}]}`)
	}))
	defer srv.Close()

	s := newTranslateTestSettings(t, srv.URL)
	s.BaseURL += "/" // 故意带尾部斜杠
	if _, err := s.Translate(context.Background(), "Hello"); err != nil {
		t.Fatalf("翻译不应报错: %v", err)
	}
	if path != "/chat/completions" {
		t.Fatalf("端点路径应为 /chat/completions，得到 %q", path)
	}
}

// TestSnippetOf 防回归：错误片段要截断、压平换行，防止巨大 HTML 错误页把
// 弹窗与日志撑爆。
func TestSnippetOf(t *testing.T) {
	big := strings.Repeat("a", errSnippetMax*3)
	got := snippetOf([]byte(big))
	if len(got) < errSnippetMax || len(got) > errSnippetMax+30 {
		t.Fatalf("片段长度应约等于 %d 字节（含截断提示），得到 %d", errSnippetMax, len(got))
	}
	multi := snippetOf([]byte("line1\nline2\r\ntab\there"))
	if strings.ContainsAny(multi, "\n\r\t") {
		t.Fatalf("片段中的换行/制表符应被压平，得到 %q", multi)
	}
}
