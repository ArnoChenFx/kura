package market

// 榜单客户端的测试：全部用 httptest 本地服务器，不联网。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeSkills 起一个只回放固定响应的服务器，并记录每次请求的路径与头。
type fakeSkills struct {
	mu     sync.Mutex
	status int
	body   string
	paths  []string
	heads  []http.Header
	srv    *httptest.Server
}

func newFakeSkills(t *testing.T, status int, body string) *fakeSkills {
	t.Helper()
	f := &fakeSkills{status: status, body: body}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.Path)
		f.heads = append(f.heads, r.Header.Clone())
		f.mu.Unlock()
		w.WriteHeader(f.status)
		_, _ = w.Write([]byte(f.body))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSkills) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.paths...)
}

// TestListRequestsRouteForEachView 验证三个榜单各走自己的路径、页码放在路径末尾，
// 并且不带任何密钥（匿名读取）。
func TestListRequestsRouteForEachView(t *testing.T) {
	f := newFakeSkills(t, http.StatusOK, `{"skills":[],"total":0,"hasMore":false,"page":2}`)
	c := New(f.srv.URL)
	for _, v := range []View{ViewAllTime, ViewHot, ViewTrending} {
		if _, err := c.List(context.Background(), v, 2); err != nil {
			t.Fatalf("List(%s): %v", v, err)
		}
	}
	want := []string{"/api/skills/all-time/2", "/api/skills/hot/2", "/api/skills/trending/2"}
	got := f.requests()
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("请求路径 = %v，想要 %v", got, want)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, h := range f.heads {
		if h.Get("Authorization") != "" {
			t.Errorf("不应带认证头，得到 %q", h.Get("Authorization"))
		}
	}
}

// TestListParsesSkillsAndSkipsBadRecords 验证字段解析，以及不合法的单条记录被跳过、
// 不拖累整页：来源不是 owner/repo、没有名字的记录都不可安装。
func TestListParsesSkillsAndSkipsBadRecords(t *testing.T) {
	body := `{"skills":[
		{"source":"vercel-labs/agent-skills","skillId":"react-best-practices","name":"react-best-practices","installs":69954},
		{"source":"noslash","skillId":"x","name":"x","installs":5},
		{"source":"owner/repo/extra","skillId":"y","name":"y","installs":5},
		{"source":"owner/repo","skillId":"z","name":"   ","installs":5},
		{"source":"me/tools","name":"  fmt-go  ","installs":-3},
		{"source":"a b/c","name":"spaces","installs":1}
	],"total":6,"hasMore":true,"page":0}`
	f := newFakeSkills(t, http.StatusOK, body)
	p, err := New(f.srv.URL).List(context.Background(), ViewAllTime, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !p.HasMore || p.Total != 6 {
		t.Errorf("分页信息 = hasMore %v total %d", p.HasMore, p.Total)
	}
	if len(p.Skills) != 2 {
		t.Fatalf("应只保留 2 条可安装的记录，得到 %d 条: %+v", len(p.Skills), p.Skills)
	}
	first := p.Skills[0]
	if first.Source != "vercel-labs/agent-skills" || first.SkillID != "react-best-practices" ||
		first.Name != "react-best-practices" || first.Installs != 69954 {
		t.Errorf("第一条解析不对：%+v", first)
	}
	// skillId 缺失时用名字；名字两端的空白去掉；负数的安装量按 0 算。
	second := p.Skills[1]
	if second.SkillID != "fmt-go" || second.Name != "fmt-go" || second.Installs != 0 {
		t.Errorf("第二条解析不对：%+v", second)
	}
}

// TestListEmptyPageAndMissingSkillsArray 验证空页是合法的，而缺少 skills 数组是结构错误。
func TestListEmptyPageAndMissingSkillsArray(t *testing.T) {
	f := newFakeSkills(t, http.StatusOK, `{"skills":[],"total":0,"hasMore":false}`)
	p, err := New(f.srv.URL).List(context.Background(), ViewHot, 0)
	if err != nil || len(p.Skills) != 0 || p.HasMore {
		t.Fatalf("空页应正常返回：%+v, %v", p, err)
	}

	for _, bad := range []string{`{"total":3,"hasMore":false}`, `{"skills":"nope"}`, `<html>blocked</html>`} {
		g := newFakeSkills(t, http.StatusOK, bad)
		if _, err := New(g.srv.URL).List(context.Background(), ViewHot, 0); err == nil {
			t.Errorf("响应 %q 应报错", bad)
		}
	}
}

// TestListReportsHTTPStatus 验证非 2xx 报错并带上状态码。
func TestListReportsHTTPStatus(t *testing.T) {
	f := newFakeSkills(t, http.StatusServiceUnavailable, "maintenance")
	_, err := New(f.srv.URL).List(context.Background(), ViewTrending, 1)
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("应报告 503，得到 %v", err)
	}
}

// TestListRejectsOversizedBody 验证超过大小上限直接报错，不静默截断。
func TestListRejectsOversizedBody(t *testing.T) {
	f := newFakeSkills(t, http.StatusOK, strings.Repeat("x", maxPageBytes+1))
	_, err := New(f.srv.URL).List(context.Background(), ViewAllTime, 0)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("应为 ErrTooLarge，得到 %v", err)
	}
}

// TestListValidatesArgumentsBeforeRequest 验证不认识的榜单与负页码在发请求之前就被拒绝。
func TestListValidatesArgumentsBeforeRequest(t *testing.T) {
	f := newFakeSkills(t, http.StatusOK, `{"skills":[]}`)
	c := New(f.srv.URL)
	if _, err := c.List(context.Background(), View("stars"), 0); !errors.Is(err, ErrUnknownView) {
		t.Errorf("未知榜单应为 ErrUnknownView，得到 %v", err)
	}
	if _, err := c.List(context.Background(), ViewHot, -1); !errors.Is(err, ErrBadPage) {
		t.Errorf("负页码应为 ErrBadPage，得到 %v", err)
	}
	if n := len(f.requests()); n != 0 {
		t.Errorf("非法参数不应发请求，实际 %d 次", n)
	}
}

// TestListHonorsCancelledContext 验证调用方取消后立即返回错误。
func TestListHonorsCancelledContext(t *testing.T) {
	f := newFakeSkills(t, http.StatusOK, `{"skills":[]}`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(f.srv.URL).List(ctx, ViewAllTime, 0); err == nil {
		t.Fatal("已取消的 ctx 应报错")
	}
}

// TestNewDefaultsAndTrimsBase 验证默认地址与尾部斜杠的规范化。
func TestNewDefaultsAndTrimsBase(t *testing.T) {
	if got := New("").base; got != DefaultBaseURL {
		t.Errorf("默认地址 = %q", got)
	}
	if got := New("http://127.0.0.1:1/").base; got != "http://127.0.0.1:1" {
		t.Errorf("尾部斜杠没去掉：%q", got)
	}
}
