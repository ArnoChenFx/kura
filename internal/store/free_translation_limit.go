package store

// 免费网页服务按提供方共享请求节奏。描述、全文和匿名会话不能各自连续请求；
// 收到限流后，排队任务立即返回冷却提示，由用户稍后重试，不自动重发。

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/locales"
)

const (
	freeTranslationInterval = time.Second
	freeTranslationCooldown = time.Minute
	freeTranslationTimeout  = 30 * time.Second
)

// ErrTranslateRateLimited 表示提供方正在冷却，可用 errors.Is 判断。
// 实际错误携带当前剩余秒数，身份不随等待时间或界面语言变化。
var ErrTranslateRateLimited = translationRateLimitError(freeTranslationCooldown)

var (
	googleTranslationGate    = newFreeTranslationGate(nil)
	microsoftTranslationGate = newFreeTranslationGate(nil)
)

// 时钟由构造时注入；测试推进虚拟时间，不为每个分块真实等待一秒。
type translationClock interface {
	Now() time.Time
	Wait(context.Context, time.Duration) error
}

type realTranslationClock struct{}

func (realTranslationClock) Now() time.Time { return time.Now() }
func (realTranslationClock) Wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type freeTranslationGate struct {
	token    chan struct{}
	clock    translationClock
	next     time.Time
	cooldown time.Time
}

func newFreeTranslationGate(clock translationClock) *freeTranslationGate {
	if clock == nil {
		clock = realTranslationClock{}
	}
	return &freeTranslationGate{token: make(chan struct{}, 1), clock: clock}
}

// acquire 的返回函数在读完并关闭响应后调用。时间状态仅由持有 token 的任务读写，
// 因而 429 冷却一定先于下一项任务可见；排队和间隔等待都能立即取消。
func (g *freeTranslationGate) acquire(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case g.token <- struct{}{}:
	}
	release := func() { <-g.token }
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	now := g.clock.Now()
	if g.cooldown.After(now) {
		err := translationRateLimitError(g.cooldown.Sub(now))
		release()
		return nil, err
	}
	if delay := g.next.Sub(now); delay > 0 {
		if err := g.clock.Wait(ctx, delay); err != nil {
			release()
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	return func() {
		g.next = g.clock.Now().Add(freeTranslationInterval)
		release()
	}, nil
}

// freeTranslationRequest 是两个免费服务唯一的 HTTP 执行入口。
// 不回显 URL、正文、会话或服务端响应；重定向策略仍由各自的匿名客户端设置。
func freeTranslationRequest(gate *freeTranslationGate, client *http.Client, req *http.Request, limit int64) ([]byte, error) {
	finish, err := gate.acquire(req.Context())
	if err != nil {
		if errors.Is(err, ErrTranslateRateLimited) {
			return nil, err
		}
		return nil, googlePrivateError(err, "storeErrTranslateDo", locales.StoreErrTranslateDo)
	}
	defer finish()
	// 等待限速不占用网络预算；超时覆盖连接、响应头和正文读取。
	// 调用方若已有更早的 deadline，WithTimeout 仍会尊重它。
	ctx, cancel := context.WithTimeout(req.Context(), freeTranslationTimeout)
	defer cancel()
	req = req.Clone(ctx)
	resp, err := client.Do(req)
	if err != nil {
		return nil, googlePrivateError(err, "storeErrTranslateDo", locales.StoreErrTranslateDo)
	}
	defer resp.Body.Close() //nolint:errcheck // 关闭发生在释放 gate 前，不能并发发送下一项
	if resp.StatusCode == http.StatusTooManyRequests {
		now := gate.clock.Now()
		if until := translationRetryAfter(now, resp.Header.Get("Retry-After")); until.After(gate.cooldown) {
			gate.cooldown = until
		}
		return nil, translationRateLimitError(gate.cooldown.Sub(now))
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, i18n.NewError("storeErrTranslateStatus", func() string {
			return locales.StoreErrTranslateStatus(resp.StatusCode, http.StatusText(resp.StatusCode))
		})
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, googlePrivateError(err, "storeErrTranslateRead", locales.StoreErrTranslateRead)
	}
	if int64(len(data)) > limit {
		return nil, i18n.NewError("storeErrTranslateRead", locales.StoreErrTranslateRead)
	}
	return data, nil
}

// Retry-After 支持秒数和 HTTP 日期。长于默认一分钟的服务端要求完整保留；
// 无效值采用一分钟，已过期或零秒仍保留正常的一秒间隔，绝不立即反复请求。
func translationRetryAfter(now time.Time, header string) time.Time {
	header = strings.TrimSpace(header)
	digits := header != ""
	for _, r := range header {
		digits = digits && r >= '0' && r <= '9'
	}
	if digits {
		seconds, err := strconv.ParseUint(header, 10, 64)
		const maxDelay = time.Duration(1<<63 - 1)
		if errors.Is(err, strconv.ErrRange) || seconds > uint64(maxDelay/time.Second) {
			// 防止恶意或损坏的巨大秒数溢出成负数而跳过冷却。
			return now.Add(maxDelay)
		}
		if err == nil {
			return now.Add(max(time.Duration(seconds)*time.Second, freeTranslationInterval))
		}
	}
	if until, err := http.ParseTime(header); err == nil {
		if until.After(now) {
			return until
		}
		return now.Add(freeTranslationInterval)
	}
	return now.Add(freeTranslationCooldown)
}

func translationRateLimitError(delay time.Duration) error {
	seconds := int(delay / time.Second)
	if delay%time.Second != 0 {
		seconds++
	}
	seconds = max(seconds, 1)
	return i18n.NewError("storeErrTranslateRateLimited", func() string {
		return locales.StoreErrTranslateRateLimited(seconds)
	})
}
