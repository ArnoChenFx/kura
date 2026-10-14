package ui

// 翻译配置变更后的后台任务（PR #4 评审缺陷 5）：旧任务不再发后面的段，并且
// 一个任务里的所有请求用同一套配置。翻译函数一律是注入的假实现，不联网；
// 假密钥写 fakeKey。

import (
	"strings"
	"testing"
)

// docOf 造一份会被切成 n 段的全文：每段约 docChunkRunes*0.6 个字符，两段放
// 不进同一块。
func docOf(n int) string {
	var paras []string
	for i := 0; i < n; i++ {
		paras = append(paras, strings.Repeat(string(rune('a'+i)), docChunkRunes*6/10))
	}
	return strings.Join(paras, "\n\n")
}

// TestTranslateDocStopsRequestingAfterReset 验证：全文翻译进行到一半配置变了
// （reset），后面的段不再请求；请求次数等于已发出的段数，不是总段数。
func TestTranslateDocStopsRequestingAfterReset(t *testing.T) {
	a, _, f, _ := trApp(t, true)
	src := docOf(3)
	f.onCall = func(n int) {
		if n == 1 {
			a.trans.reset() // 第 1 段在路上时，用户保存了新的翻译设置
		}
	}
	a.TranslateDocAsync(docTransKey("alpha"), src)
	a.settle()

	if f.calls != 1 {
		t.Errorf("配置变更后不应再请求后面的段：请求了 %d 次，想要 1（已发出的段数）", f.calls)
	}
	if _, ok := a.trans.lookup(docTransKey("alpha")); ok {
		t.Error("旧配置下的结果不应写进新缓存")
	}
	if len(a.trans.items) != 0 {
		t.Errorf("缓存应为空，得到 %v", a.trans.items)
	}
}

// TestTranslateDocNotSentWhenResetBeforeStart 验证：任务发起后、后台 goroutine
// 开始跑之前配置就变了，一段请求都不发。
func TestTranslateDocNotSentWhenResetBeforeStart(t *testing.T) {
	a, _, f, _ := trApp(t, true)
	var queued []func()
	a.spawn = func(fn func()) { queued = append(queued, fn) }

	a.TranslateDocAsync(docTransKey("alpha"), docOf(3))
	a.trans.reset()
	for _, fn := range queued {
		fn()
	}
	a.settle()
	if f.calls != 0 {
		t.Errorf("配置已变更，旧任务不应发出任何请求，得到 %d 次", f.calls)
	}
}

// TestTranslateDocPinsSettingsForWholeTask 验证：同一个全文任务里所有段用任务
// 开始时的那套配置，即使中途 a.settings 被改了（修复前后几段会读到新模型）。
func TestTranslateDocPinsSettingsForWholeTask(t *testing.T) {
	a, _, f, _ := trApp(t, true)
	f.onCall = func(n int) {
		if n == 1 {
			a.settings.Model = "model-changed-midway" // 只改内存设置，不 reset
		}
	}
	a.TranslateDocAsync(docTransKey("alpha"), docOf(3))
	a.settle()

	if f.calls != 3 {
		t.Fatalf("应请求 3 段，得到 %d 次", f.calls)
	}
	for i, m := range f.models {
		if m != "fake-model" {
			t.Errorf("第 %d 段用了模型 %q，同一任务内应全部是任务开始时的 fake-model", i+1, m)
		}
	}
}

// TestTranslateAsyncNotSentWhenResetBeforeStart 验证单段翻译：发起后、请求前
// 配置变了就不发请求；请求中途变了则结果不写缓存。
func TestTranslateAsyncNotSentWhenResetBeforeStart(t *testing.T) {
	a, _, f, _ := trApp(t, true)
	var queued []func()
	a.spawn = func(fn func()) { queued = append(queued, fn) }

	a.TranslateAsync("detail:alpha:description", "Reads PDF files.")
	a.trans.reset()
	for _, fn := range queued {
		fn()
	}
	a.settle()
	if f.calls != 0 {
		t.Errorf("配置已变更，不应再发起请求，得到 %d 次", f.calls)
	}
	if len(a.trans.items) != 0 {
		t.Errorf("缓存应为空，得到 %v", a.trans.items)
	}
}

// TestTranslateAsyncMidFlightResetDropsResultAndPinsSettings 验证：请求在途时
// 配置变了，结果不写缓存；请求用的是发起时固定的设置。
func TestTranslateAsyncMidFlightResetDropsResultAndPinsSettings(t *testing.T) {
	a, _, f, _ := trApp(t, true)
	f.onCall = func(int) {
		a.settings.Model = "model-changed-midway"
		a.trans.reset()
	}
	a.TranslateAsync("detail:alpha:description", "Reads PDF files.")
	a.settle()

	if f.calls != 1 || f.models[0] != "fake-model" {
		t.Errorf("请求应只发一次且用发起时的模型，calls=%d models=%v", f.calls, f.models)
	}
	if len(a.trans.items) != 0 {
		t.Errorf("配置变更后的旧结果不应写进缓存，得到 %v", a.trans.items)
	}
}

// TestTranslateErrorStillRedactsKey 验证固定设置之后，错误文本里的 API key
// 仍然被抹掉。
func TestTranslateErrorStillRedactsKey(t *testing.T) {
	a, _, f, _ := trApp(t, true)
	f.err = errString("upstream rejected key " + fakeKey)
	a.TranslateDocAsync(docTransKey("alpha"), docOf(2))
	a.settle()
	e := a.trans.items[docTransKey("alpha")]
	if e.err == "" || strings.Contains(e.err, fakeKey) {
		t.Errorf("错误应记下且不含密钥，得到 %q", e.err)
	}
}

// errString 是只带文本的 error。
type errString string

func (e errString) Error() string { return string(e) }
