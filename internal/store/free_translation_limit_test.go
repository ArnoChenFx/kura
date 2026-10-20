package store

// 限流测试只走假传输或本机 httptest。虚拟时钟覆盖长等待，不能让测试真实等待
// 每个分块的一秒间隔，也不能把某一测试的冷却状态遗留给下一项。

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ArnoChenFx/kura/internal/i18n"
)

type advancingTranslationClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTranslationTestClock() *advancingTranslationClock {
	return &advancingTranslationClock{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
}
func (c *advancingTranslationClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}
func (c *advancingTranslationClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}
func (c *advancingTranslationClock) Wait(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.advance(d)
	return nil
}

func useTranslationTestGates(t *testing.T) *advancingTranslationClock {
	t.Helper()
	oldGoogle, oldMicrosoft := googleTranslationGate, microsoftTranslationGate
	clock := newTranslationTestClock()
	googleTranslationGate, microsoftTranslationGate = newFreeTranslationGate(clock), newFreeTranslationGate(clock)
	t.Cleanup(func() {
		googleTranslationGate, microsoftTranslationGate = oldGoogle, oldMicrosoft
	})
	return clock
}

func TestTranslationRetryAfter(t *testing.T) {
	now := newTranslationTestClock().Now()
	for _, tc := range []struct {
		header string
		delay  time.Duration
	}{
		{"61", 61 * time.Second},
		{"3600", time.Hour},
		{" 120 ", 2 * time.Minute},
		{now.Add(2 * time.Hour).Format(http.TimeFormat), 2 * time.Hour},
		{"", time.Minute},
		{"invalid", time.Minute},
		{"-1", time.Minute},
		{"+1", time.Minute},
		{"0", time.Second},
		{now.Add(-time.Hour).Format(http.TimeFormat), time.Second},
		{"18446744073709551615", time.Duration(1<<63 - 1)},
		{"999999999999999999999999999999", time.Duration(1<<63 - 1)},
	} {
		t.Run(tc.header, func(t *testing.T) {
			if got := translationRetryAfter(now, tc.header); !got.Equal(now.Add(tc.delay)) {
				t.Fatalf("Retry-After=%q 得到 %s，期望延迟 %s", tc.header, got, tc.delay)
			}
		})
	}
	// HTTP 日期直接保存，不能先转 Duration 再加回而截短很远的未来日期。
	farFuture := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := translationRetryAfter(now, farFuture.Format(http.TimeFormat)); !got.Equal(farFuture) {
		t.Fatalf("远未来 HTTP 日期被截短：%s", got)
	}
}

func TestTranslationRateLimitErrorIsLocalizedAndStable(t *testing.T) {
	err := translationRateLimitError(60*time.Second + time.Millisecond)
	if !errors.Is(err, ErrTranslateRateLimited) || !strings.Contains(err.Error(), "61") {
		t.Fatalf("限流身份或向上取整的秒数不正确：%v", err)
	}
	setLangForTest(t, i18n.EN)
	if hasCJK(err.Error()) || !strings.Contains(err.Error(), "61") || !errors.Is(err, ErrTranslateRateLimited) {
		t.Fatalf("切换语言后限流错误或身份不正确：%v", err)
	}
}

func TestFreeTranslationRequestsAreSerialAndSpaced(t *testing.T) {
	clock := newTranslationTestClock()
	gate := newFreeTranslationGate(clock)
	var starts, ends []time.Time
	var active, maxActive int
	client := &http.Client{Transport: googleTestTransport(func(*http.Request) (*http.Response, error) {
		active++
		maxActive = max(maxActive, active)
		starts = append(starts, clock.Now())
		clock.advance(250 * time.Millisecond)
		ends = append(ends, clock.Now())
		active--
		return googleTestResponse(http.StatusOK, "ok"), nil
	})}
	var workers sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		workers.Go(func() {
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.invalid", nil)
			_, err := freeTranslationRequest(gate, client, req, 100)
			errs <- err
		})
	}
	workers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if maxActive != 1 || len(starts) != 8 {
		t.Fatalf("同服务请求未串行：maxActive=%d requests=%d", maxActive, len(starts))
	}
	for i := 1; i < len(starts); i++ {
		if gap := starts[i].Sub(ends[i-1]); gap < freeTranslationInterval {
			t.Fatalf("第 %d 次请求间隔不足：%s", i, gap)
		}
	}
}

type translationTestBody struct {
	read  func([]byte) (int, error)
	close func() error
}

func (b translationTestBody) Read(p []byte) (int, error) { return b.read(p) }
func (b translationTestBody) Close() error               { return b.close() }

func TestFreeTranslationGateWaitsForBodyClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := newFreeTranslationGate(nil)
		closing, releaseClose := make(chan struct{}), make(chan struct{})
		calls := 0
		client := &http.Client{Transport: googleTestTransport(func(*http.Request) (*http.Response, error) {
			calls++
			resp := googleTestResponse(http.StatusOK, "ok")
			resp.Body = translationTestBody{
				read: strings.NewReader("ok").Read,
				close: func() error {
					close(closing)
					<-releaseClose
					return nil
				},
			}
			return resp, nil
		})}
		firstDone := make(chan error, 1)
		go func() {
			req, _ := http.NewRequest(http.MethodGet, "https://example.invalid", nil)
			_, err := freeTranslationRequest(gate, client, req, 100)
			firstDone <- err
		}()
		<-closing
		ctx, cancel := context.WithCancel(context.Background())
		secondDone := make(chan error, 1)
		go func() {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.invalid", nil)
			_, err := freeTranslationRequest(gate, client, req, 100)
			secondDone <- err
		}()
		synctest.Wait()
		if calls != 1 {
			t.Fatal("响应尚未关闭就发出了第二个请求")
		}
		cancel()
		if err := <-secondDone; !errors.Is(err, context.Canceled) {
			t.Fatalf("排队任务应能立即取消：%v", err)
		}
		close(releaseClose)
		if err := <-firstDone; err != nil {
			t.Fatal(err)
		}
		if len(gate.token) != 0 {
			t.Fatal("读取结束后没有释放 gate")
		}
	})
}

func TestFreeTranslationSpacingWaitCanBeCancelled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := newFreeTranslationGate(nil)
		start := time.Now()
		gate.next = start.Add(time.Second)
		calls := 0
		client := &http.Client{Transport: googleTestTransport(func(*http.Request) (*http.Response, error) {
			calls++
			return googleTestResponse(http.StatusOK, "ok"), nil
		})}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.invalid", nil)
			_, err := freeTranslationRequest(gate, client, req, 100)
			done <- err
		}()
		synctest.Wait()
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) || calls != 0 {
			t.Fatalf("间隔等待取消后仍发请求：calls=%d err=%v", calls, err)
		}
		req, _ := http.NewRequest(http.MethodGet, "https://example.invalid", nil)
		if _, err := freeTranslationRequest(gate, client, req, 100); err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(start); elapsed != time.Second || calls != 1 {
			t.Fatalf("取消任务不应额外占用一个请求间隔：elapsed=%s calls=%d", elapsed, calls)
		}
	})
}

func TestFreeTranslationNetworkTimeoutStartsAfterWaitAndReleasesGate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := newFreeTranslationGate(nil)
		start := time.Now()
		gate.next = start.Add(2 * freeTranslationTimeout)
		closed := false
		calls := 0
		client := &http.Client{Transport: googleTestTransport(func(req *http.Request) (*http.Response, error) {
			calls++
			deadline, ok := req.Context().Deadline()
			if !ok || deadline.Sub(time.Now()) != freeTranslationTimeout {
				t.Fatal("排队时间消耗了实际 HTTP 的网络预算")
			}
			if calls > 1 {
				return googleTestResponse(http.StatusOK, "ok"), nil
			}
			resp := googleTestResponse(http.StatusOK, "")
			resp.Body = translationTestBody{
				read: func([]byte) (int, error) {
					<-req.Context().Done()
					return 0, req.Context().Err()
				},
				close: func() error { closed = true; return nil },
			}
			return resp, nil
		})}
		req, _ := http.NewRequest(http.MethodGet, "https://example.invalid", nil)
		_, err := freeTranslationRequest(gate, client, req, 100)
		if !errors.Is(err, context.DeadlineExceeded) || !closed || time.Since(start) != 3*freeTranslationTimeout {
			t.Fatalf("读响应超时应关闭连接并释放 gate：err=%v closed=%v elapsed=%s", err, closed, time.Since(start))
		}
		if _, err := freeTranslationRequest(gate, client, req, 100); err != nil || calls != 2 {
			t.Fatalf("超时后其他任务不能永久阻塞：calls=%d err=%v", calls, err)
		}
	})
}

func TestFreeTranslationRespectsEarlierCallerDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := newFreeTranslationGate(nil)
		start := time.Now()
		client := &http.Client{Transport: googleTestTransport(func(req *http.Request) (*http.Response, error) {
			deadline, ok := req.Context().Deadline()
			if !ok || deadline.Sub(start) != 2*time.Second {
				t.Fatal("免费服务不能延长调用方已有时限")
			}
			<-req.Context().Done()
			return nil, req.Context().Err()
		})}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.invalid", nil)
		_, err := freeTranslationRequest(gate, client, req, 100)
		if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) != 2*time.Second {
			t.Fatalf("调用方时限未生效：%v", err)
		}
	})
}

func TestFreeTranslationLongBlockKeepsPerRequestBudget(t *testing.T) {
	for _, provider := range []string{TranslationGoogle, TranslationMicrosoft} {
		t.Run(provider, func(t *testing.T) {
			calls := 0
			withGoogleTransport(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if deadline, ok := r.Context().Deadline(); !ok || time.Until(deadline) > freeTranslationTimeout {
					t.Error("每个实际 HTTP 都必须有独立的时限")
				}
				return successfulFreeTranslationResponse(r), nil
			})
			clock := useTranslationTestGates(t)
			start := clock.Now()
			src := strings.Repeat("normal prose ", 5500)
			out, err := (&Settings{Provider: provider}).Translate(context.Background(), src)
			if err != nil || out != src || calls < 32 || clock.Now().Sub(start) <= freeTranslationTimeout {
				t.Fatalf("无空行长段不能仅因限速等待超时：err=%v calls=%d elapsed=%s equal=%v",
					err, calls, clock.Now().Sub(start), out == src)
			}
		})
	}
}

// 复用真实派发的请求格式，返回原文，测试不访问任何翻译服务。
func successfulFreeTranslationResponse(req *http.Request) *http.Response {
	if strings.Contains(req.URL.Path, "/translator") {
		return googleTestResponse(http.StatusOK, microsoftTestPage)
	}
	text, lang := req.URL.Query().Get("q"), req.URL.Query().Get("tl")
	var body any
	if req.Method == http.MethodPost {
		_ = req.ParseForm()
		text, lang = req.Form.Get("text"), req.Form.Get("to")
		body = []map[string]any{{"translations": []map[string]string{{"text": text, "to": lang}}}}
	} else {
		body = map[string]any{"sentences": []map[string]string{{"trans": text}}}
	}
	data, _ := json.Marshal(body)
	return googleTestResponse(http.StatusOK, string(data))
}

// 真实本地 HTTP 验证 429 对已排队任务立即生效。提供方的 URL 只在测试传输层
// 改写成本机地址；产品代码仍只请求固定的 Google/Bing 目标。
func TestFreeTranslationHTTP429StopsQueuedTasksAndIsolatesProviders(t *testing.T) {
	for _, scenario := range []struct {
		name, provider string
		microsoftPost  bool
	}{
		{"google", TranslationGoogle, false},
		{"microsoft-session", TranslationMicrosoft, false},
		{"microsoft-translation", TranslationMicrosoft, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			first429, release429 := make(chan struct{}), make(chan struct{})
			var once sync.Once
			var googleCalls, microsoftCalls atomic.Int32
			var limiting atomic.Bool
			limiting.Store(true)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				provider := TranslationGoogle
				if !strings.Contains(r.URL.Path, "/translate_a/") {
					provider = TranslationMicrosoft
					microsoftCalls.Add(1)
				} else {
					googleCalls.Add(1)
				}
				shouldLimit := provider == scenario.provider && (!scenario.microsoftPost || r.Method == http.MethodPost)
				if limiting.Load() && shouldLimit {
					once.Do(func() { close(first429) })
					select {
					case <-release429:
					case <-r.Context().Done():
						return
					}
					w.Header().Set("Retry-After", "120")
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = io.WriteString(w, "private-original fake-anonymous-session")
					return
				}
				resp := successfulFreeTranslationResponse(r)
				defer resp.Body.Close()
				_, _ = io.Copy(w, resp.Body)
			}))
			t.Cleanup(server.Close)
			endpoint, _ := url.Parse(server.URL)
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.Proxy = nil
			t.Cleanup(transport.CloseIdleConnections)
			withGoogleTransport(t, func(r *http.Request) (*http.Response, error) {
				copy := r.Clone(r.Context())
				copy.URL.Scheme, copy.URL.Host = endpoint.Scheme, endpoint.Host
				return transport.RoundTrip(copy)
			})
			clock := useTranslationTestGates(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			results := make(chan error, 5)
			call := func() {
				_, err := (&Settings{Provider: scenario.provider}).Translate(ctx, "private-original")
				results <- err
			}
			go call()
			select {
			case <-first429:
			case <-ctx.Done():
				t.Fatal("本机 HTTP 没有收到第一项请求")
			}
			for range 4 {
				go call()
			}
			close(release429)
			for range 5 {
				if err := <-results; !errors.Is(err, ErrTranslateRateLimited) ||
					strings.Contains(err.Error(), "private-original") || strings.Contains(err.Error(), "fake-anonymous-session") {
					t.Fatalf("排队任务应明确限流且不泄漏原文：%v", err)
				}
			}
			expected := int32(1)
			calls := &googleCalls
			other := TranslationMicrosoft
			if scenario.provider == TranslationMicrosoft {
				calls, other = &microsoftCalls, TranslationGoogle
				if scenario.microsoftPost {
					expected = 2
				}
			}
			if calls.Load() != expected {
				t.Fatalf("收到429后排队任务仍继续发送：calls=%d want=%d", calls.Load(), expected)
			}
			if _, err := (&Settings{Provider: other}).Translate(ctx, "Hello"); err != nil {
				t.Fatalf("另一个服务不能被连带冷却：%v", err)
			}
			if _, err := (&Settings{Provider: scenario.provider}).Translate(ctx, "private-original"); !errors.Is(err, ErrTranslateRateLimited) || calls.Load() != expected {
				t.Fatalf("冷却期间手动重试仍发出了 HTTP：calls=%d err=%v", calls.Load(), err)
			}
			clock.advance(2 * time.Minute)
			limiting.Store(false)
			if out, err := (&Settings{Provider: scenario.provider}).Translate(ctx, "Hello"); err != nil || out != "Hello" {
				t.Fatalf("冷却结束后显式重试应能成功：out=%q err=%v", out, err)
			}
		})
	}
}
