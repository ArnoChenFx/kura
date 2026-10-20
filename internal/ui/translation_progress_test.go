package ui

// 全文翻译的停止与续译：所有请求都由假实现完成，目录全在 t.TempDir()。

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/ArnoChenFx/kura/locales"
	myui "github.com/egoist/mygo/ui"
)

func TestDocTranslationFailureResumesOnlyUnfinishedParts(t *testing.T) {
	a, _ := newTestApp(t)
	a.settings = trReadySettings()
	key, src := docTransKey("alpha"), docOf(3)
	calls, fail := map[byte]int{}, true
	a.translate = func(_ context.Context, _ store.Settings, text string) (string, error) {
		calls[text[0]]++
		if text[0] == 'b' && fail {
			return "", errors.New("temporary failure")
		}
		return "translated:" + text, nil
	}
	a.TranslateDocAsync(key, src)
	a.settle()
	e := a.trans.items[key]
	if e.pending || e.err == "" || len(e.parts) != 1 || len(e.chunks) != 3 || e.dst != "" {
		t.Fatalf("失败应保留第一段与进度，得到 %+v", e)
	}
	// 普通重绘不能自动重发，避免失败页面不断请求服务。
	a.TranslateDocAsync(key, src)
	a.settle()
	if calls['b'] != 1 {
		t.Fatalf("重绘不应重试失败段，calls=%v", calls)
	}
	fail = false
	a.ResumeDocTranslation(key, src)
	a.settle()
	e = a.trans.items[key]
	if e.pending || e.err != "" || e.paused || len(e.parts) != 3 || e.dst == "" {
		t.Fatalf("续译应完成整份文档，得到 %+v", e)
	}
	if calls['a'] != 1 || calls['b'] != 2 || calls['c'] != 1 {
		t.Fatalf("已完成的第一段不应再请求，calls=%v", calls)
	}
}

func TestDocTranslationStopPreservesProgressAndCancelsRequest(t *testing.T) {
	a, _ := newTestApp(t)
	a.settings = trReadySettings()
	key, src := docTransKey("alpha"), docOf(3)
	calls, stop := map[byte]int{}, true
	a.translate = func(ctx context.Context, _ store.Settings, text string) (string, error) {
		calls[text[0]]++
		if text[0] == 'b' && stop {
			a.flush() // 第一段已显示在 UI 上，再按停止。
			a.StopDocTranslation(key, src)
			if !errors.Is(ctx.Err(), context.Canceled) {
				t.Errorf("停止必须取消在途请求，ctx.Err=%v", ctx.Err())
			}
			return "", ctx.Err()
		}
		return "translated:" + text, nil
	}
	a.TranslateDocAsync(key, src)
	a.settle()
	e := a.trans.items[key]
	if e.pending || !e.paused || e.err != "" || len(e.parts) != 1 {
		t.Fatalf("停止应保留第一段且不记为失败，得到 %+v", e)
	}
	a.TranslateDocAsync(key, src)
	a.settle()
	if calls['b'] != 1 || calls['c'] != 0 {
		t.Fatalf("停止后重绘不能偷偷继续，calls=%v", calls)
	}
	stop = false
	a.ResumeDocTranslation(key, src)
	a.settle()
	if e = a.trans.items[key]; e.dst == "" || e.pending || e.paused {
		t.Fatalf("继续后应完成，得到 %+v", e)
	}
	if calls['a'] != 1 || calls['b'] != 2 || calls['c'] != 1 {
		t.Fatalf("继续仅请求未完成的段，calls=%v", calls)
	}
}

func TestDocTranslationStoppedCallbacksCannotFinishNewAttempt(t *testing.T) {
	a, _ := newTestApp(t)
	a.settings = trReadySettings()
	key, src := docTransKey("alpha"), "same source"
	var jobs []func()
	a.spawn = func(fn func()) { jobs = append(jobs, fn) }
	output := "old result"
	a.translate = func(context.Context, store.Settings, string) (string, error) { return output, nil }
	a.TranslateDocAsync(key, src)
	jobs[0]() // 旧请求完成，但它的进度/完成回调还没有被 UI 处理。
	a.StopDocTranslation(key, src)
	a.ResumeDocTranslation(key, src)
	a.flush() // 旧回调不能把新的任务写成完成。
	if e := a.trans.items[key]; !e.pending || e.dst != "" || len(e.parts) != 0 {
		t.Fatalf("旧回调覆盖了续译任务：%+v", e)
	}
	output = "new result"
	jobs[1]()
	a.flush()
	if e := a.trans.items[key]; e.pending || e.dst != "new result" {
		t.Fatalf("应只显示新任务结果，得到 %+v", e)
	}
}

func TestDocTranslationChangedSourceCancelsOldTask(t *testing.T) {
	a, _ := newTestApp(t)
	a.settings = trReadySettings()
	key := docTransKey("alpha")
	var jobs []func()
	a.spawn = func(fn func()) { jobs = append(jobs, fn) }
	var texts []string
	a.translate = func(_ context.Context, _ store.Settings, text string) (string, error) {
		texts = append(texts, text)
		return "translated:" + text, nil
	}
	a.TranslateDocAsync(key, "first")
	a.TranslateDocAsync(key, "second")
	a.TranslateDocAsync(key, "first") // 返回同一原文，也不能让第一次的旧任务重新有效。
	for _, job := range jobs {
		job()
	}
	a.flush()
	if len(texts) != 1 || texts[0] != "first" {
		t.Fatalf("原文切换后旧任务应取消，仅最新任务发请求，texts=%v", texts)
	}
	if e := a.trans.items[key]; e.pending || e.dst != "translated:first" {
		t.Fatalf("最终结果不符：%+v", e)
	}
}

func TestDocTranslationCacheAvoidsBackgroundWork(t *testing.T) {
	a, _ := newTestApp(t)
	a.settings = trReadySettings()
	key, src := docTransKey("alpha"), docOf(3)
	jobs, calls := 0, 0
	a.spawn = func(fn func()) { jobs++; fn() }
	a.translate = func(_ context.Context, _ store.Settings, text string) (string, error) {
		calls++
		return "translated:" + text, nil
	}
	a.TranslateDocAsync(key, src)
	a.flush()
	for range 20 {
		a.TranslateDocAsync(key, src)
	}
	if jobs != 1 || calls != 3 {
		t.Fatalf("命中缓存后不应再次分段或发请求，jobs=%d calls=%d", jobs, calls)
	}
}

func TestDocTranslationToggleRetriesWithoutLosingCompletedParts(t *testing.T) {
	a, _ := newTestApp(t)
	a.settings = trReadySettings()
	key, src := docTransKey("alpha"), docOf(2)
	calls, fail := 0, true
	a.translate = func(_ context.Context, _ store.Settings, text string) (string, error) {
		calls++
		if text[0] == 'b' && fail {
			return "", errors.New("temporary failure")
		}
		return "translated:" + text, nil
	}
	a.TranslateDocAsync(key, src)
	a.flush()
	a.trans.clearErrors()
	fail = false
	a.TranslateDocAsync(key, src)
	a.flush()
	if calls != 3 || a.trans.items[key].dst == "" {
		t.Fatalf("切回译文重试应复用第一段，calls=%d entry=%+v", calls, a.trans.items[key])
	}
}

func TestDocTranslationControlsStopAndResume(t *testing.T) {
	a, _ := newTestApp(t)
	a.settings, a.trans.on = trReadySettings(), true
	key, src := docTransKey("alpha"), "remaining text"
	var jobs []func()
	a.spawn = func(fn func()) { jobs = append(jobs, fn) }
	a.translate = func(_ context.Context, _ store.Settings, text string) (string, error) {
		return "translated:" + text, nil
	}
	tt := myui.NewTester(func(c *myui.Context) {
		myui.Column(c).Fill().Children(func() { a.viewDocTranslation(c, key, src) })
	}, 720, 480)
	tt.Frame()
	if !tt.HasText(locales.UiTranslateStop()) || len(jobs) != 1 {
		t.Fatalf("应显示停止按钮且只启动一次，jobs=%d texts=%v", len(jobs), tt.Texts())
	}
	if err := tt.Click(locales.UiTranslateStop()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText(locales.UiTranslateResume()) || !a.trans.items[key].paused {
		t.Fatalf("停止后应出现继续按钮，texts=%v", tt.Texts())
	}
	if err := tt.Click(locales.UiTranslateResume()); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(jobs) != 2 {
		t.Fatalf("继续应启动新的尝试，jobs=%d", len(jobs))
	}
	for _, job := range jobs {
		job()
	}
	a.flush()
	tt.Frame()
	if tt.HasText(locales.UiTranslateStop()) || tt.HasText(locales.UiTranslateResume()) ||
		!strings.Contains(strings.Join(tt.Texts(), "\n"), "translated:remaining text") {
		t.Fatalf("完成后应显示译文并移除任务按钮，texts=%v", tt.Texts())
	}
}

func TestDocTranslationAsyncCancelAndResume(t *testing.T) {
	a, _ := newTestApp(t)
	a.settings = trReadySettings()
	key, src := docTransKey("alpha"), docOf(2)
	updates := make(chan func(), 16)
	started := make(chan struct{})
	cancelled := make(chan struct{})
	var workers sync.WaitGroup
	a.update = func(fn func()) { updates <- fn }
	a.spawn = func(fn func()) {
		workers.Add(1)
		go func() { defer workers.Done(); fn() }()
	}
	t.Cleanup(func() {
		a.trans.reset()
		workers.Wait()
	})
	calls := 0
	var callsMu sync.Mutex
	a.translate = func(ctx context.Context, _ store.Settings, text string) (string, error) {
		callsMu.Lock()
		calls++
		n := calls
		callsMu.Unlock()
		if n == 2 {
			close(started)
			<-ctx.Done()
			close(cancelled)
			return "", ctx.Err()
		}
		return "translated:" + text, nil
	}
	a.TranslateDocAsync(key, src)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("第二段请求未开始")
	}
	// 第二段发出前已经排队第一段进度；在模拟 UI 线程处理后执行停止。
	drain := func() {
		for {
			select {
			case fn := <-updates:
				fn()
			default:
				return
			}
		}
	}
	drain()
	a.StopDocTranslation(key, src)
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("停止没有取消后台请求")
	}
	a.ResumeDocTranslation(key, src)
	workers.Wait()
	drain()
	if e := a.trans.items[key]; e.pending || e.err != "" || len(e.parts) != 2 || e.dst == "" {
		t.Fatalf("真正的后台请求取消后应可继续，entry=%+v", e)
	}
	callsMu.Lock()
	defer callsMu.Unlock()
	if calls != 3 {
		t.Fatalf("应该只有首段、失败第二段与重试第二段，calls=%d", calls)
	}
}

// 全文任务按实际副本目录隔离：其他根目录或停用区的同名技能不能挤掉已完成段落。
func TestLocalDocTranslationResumeSurvivesSameNameCopies(t *testing.T) {
	for _, sameRoot := range []bool{false, true} {
		label := "other-root"
		if sameRoot {
			label = "disabled-copy"
		}
		t.Run(label, func(t *testing.T) {
			a, cfg := newTestApp(t)
			a.settings, a.trans.on = trReadySettings(), true
			first := &store.Skill{Name: "alpha", State: store.StateEnabled, Dir: cfg.SkillPath("alpha")}
			other := &store.Skill{Name: "alpha", State: store.StateEnabled, Dir: filepath.Join(t.TempDir(), "alpha")}
			if sameRoot {
				other.State, other.Dir = store.StateDisabled, cfg.DisabledPath("alpha")
			}
			src, otherSrc := docOf(2), "Other copy has separate text."
			key := docTransKey(first.Dir)
			calls, stop := map[byte]int{}, true
			a.translate = func(ctx context.Context, _ store.Settings, text string) (string, error) {
				calls[text[0]]++
				if text[0] == 'b' && stop {
					a.flush()
					a.StopDocTranslation(key, src)
					return "", ctx.Err()
				}
				return "translated:" + text, nil
			}
			a.TranslateDocAsync(key, src)
			a.flush()
			if e := a.trans.items[key]; !e.paused || len(e.parts) != 1 {
				t.Fatalf("第一份文档应停在已完成一段：%+v", e)
			}
			stop = false
			current, text := other, otherSrc
			tt := myui.NewTester(func(c *myui.Context) {
				myui.Column(c).Fill().Children(func() { a.viewFullTextBody(c, current, text) })
			}, 720, 480)
			a.flush()
			tt.Frame()
			if e := a.trans.items[key]; !e.paused || len(e.parts) != 1 || e.src != src {
				t.Fatal("同名的另一份文档覆盖了第一份的续译状态")
			}
			current, text = first, src
			tt.Frame()
			if !tt.HasText(locales.UiTranslateResume()) || calls['a'] != 1 {
				t.Fatal("回到原副本应保留暂停状态，不自动重译")
			}
			if err := tt.Click(locales.UiTranslateResume()); err != nil {
				t.Fatal(err)
			}
			a.flush()
			tt.Frame()
			if calls['a'] != 1 || calls['b'] != 2 || calls['O'] != 1 {
				t.Fatalf("续译只应请求余下的段落，calls=%v", calls)
			}
			if e := a.trans.items[key]; e.pending || e.paused || e.dst == "" {
				t.Fatalf("原副本续译未完成：%+v", e)
			}
		})
	}
}
