package ui

// 翻译只为当前页面工作；取消排队和正在请求的旧任务，返回后复用成功结果。

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ArnoChenFx/kura/internal/market"
	"github.com/ArnoChenFx/kura/internal/store"
)

func TestTranslationsSelectionCancelsQueuedRequests(t *testing.T) {
	a, cfg := newTestApp(t)
	a.settings, a.trans.on = trReadySettings(), true
	first := &store.Skill{Name: "alpha", State: store.StateEnabled, Dir: cfg.SkillPath("alpha"), Description: "first description"}
	next := &store.Skill{Name: "beta", State: store.StateEnabled, Dir: cfg.SkillPath("beta"), Description: "next description"}
	a.skills = []*store.Skill{first, next}
	a.skillDocs = map[string]fullTextDoc{
		skillKey(first): {src: "first document"},
		skillKey(next):  {src: "next document"},
	}
	var jobs []func()
	a.spawn = func(fn func()) { jobs = append(jobs, fn) }
	var requested []string
	a.translate = func(_ context.Context, _ store.Settings, src string) (string, error) {
		requested = append(requested, src)
		return "translated:" + src, nil
	}
	selectFullText := func(s *store.Skill) {
		a.selectSkill(s)
		a.detailSel, a.detailTab = skillKey(s), 1
		a.ensureCurrentTranslations()
		a.TranslateDocAsync(docTransKey(s.Dir), a.skillDocs[skillKey(s)].src)
	}
	selectFullText(first)
	selectFullText(next)
	for range 10 {
		a.ensureCurrentTranslations()
		a.TranslateDocAsync(docTransKey(next.Dir), "next document")
	}
	if len(jobs) != 4 {
		t.Fatalf("重绘应复用当前描述和全文任务，jobs=%d", len(jobs))
	}
	for _, job := range jobs {
		job()
	}
	a.flush()
	if want := []string{"next description", "next document"}; !reflect.DeepEqual(requested, want) {
		t.Fatalf("离开的页面不应再发请求，got=%v want=%v", requested, want)
	}
	if e := a.trans.items[docTransKey(first.Dir)]; !e.paused || e.pending {
		t.Fatalf("离开的全文应暂停：%+v", e)
	}
	if got := a.trans.text(cardDescKey(next.Name), next.Description); got != "translated:next description" {
		t.Fatalf("旧任务取消不能影响当前描述：%q", got)
	}
	if got := a.trans.items[docTransKey(next.Dir)].dst; got != "translated:next document" {
		t.Fatalf("旧任务取消不能影响当前全文：%q", got)
	}
}

func TestDocTranslationLeavingPageCancelsRemainingPartsAndResumes(t *testing.T) {
	for _, action := range []string{"skill", "original", "close-detail", "market", "overview"} {
		t.Run(action, func(t *testing.T) {
			a, cfg := newTestApp(t)
			a.settings, a.trans.on = trReadySettings(), true
			first := &store.Skill{Name: "alpha", State: store.StateEnabled, Dir: cfg.SkillPath("alpha")}
			next := &store.Skill{Name: "beta", State: store.StateEnabled, Dir: cfg.SkillPath("beta")}
			a.skills = []*store.Skill{first, next}
			a.selectSkill(first)
			a.detailSel, a.detailTab = skillKey(first), 1
			key, src := docTransKey(first.Dir), docOf(3)
			a.skillDocs = map[string]fullTextDoc{skillKey(first): {src: src}}
			a.market.board(a.market.current()).loaded = true

			updates := make(chan func(), 32)
			started, cancelled := make(chan struct{}), make(chan struct{})
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
			var callsMu sync.Mutex
			calls := map[byte]int{}
			a.translate = func(ctx context.Context, _ store.Settings, text string) (string, error) {
				callsMu.Lock()
				calls[text[0]]++
				n := calls[text[0]]
				callsMu.Unlock()
				if text[0] == 'b' && n == 1 {
					close(started)
					<-ctx.Done()
					close(cancelled)
					return "", ctx.Err()
				}
				return "translated:" + text, nil
			}
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
			a.TranslateDocAsync(key, src)
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("第二段请求未开始")
			}
			drain()
			switch action {
			case "skill":
				a.selectSkill(next)
			case "original":
				a.SetTranslateOn(false)
			case "close-detail":
				a.clearSelection()
			case "market":
				a.OpenMarket()
			case "overview":
				a.detailTab = 0
				a.ensureCurrentTranslations()
			}
			select {
			case <-cancelled:
			case <-time.After(2 * time.Second):
				t.Fatal("离开页面没有取消正在请求的第二段")
			}
			workers.Wait()
			drain()
			if e := a.trans.items[key]; e.pending || !e.paused || e.err != "" || len(e.parts) != 1 {
				t.Fatalf("暂停应保留成功的第一段：%+v", e)
			}
			callsMu.Lock()
			before := calls['c']
			callsMu.Unlock()
			if before != 0 {
				t.Fatal("页面离开后仍然发送了第三段")
			}

			a.CloseMarket()
			a.selectSkill(first)
			a.detailSel, a.detailTab = skillKey(first), 1
			a.SetTranslateOn(true)
			a.ensureCurrentTranslations()
			a.TranslateDocAsync(key, src)
			if e := a.trans.items[key]; !e.paused || e.pending {
				t.Fatalf("返回页面应等待明确的继续操作：%+v", e)
			}
			a.ResumeDocTranslation(key, src)
			workers.Wait()
			drain()
			if e := a.trans.items[key]; e.pending || e.paused || len(e.parts) != 3 || e.dst == "" {
				t.Fatalf("继续后应完成全部段落：%+v", e)
			}
			callsMu.Lock()
			defer callsMu.Unlock()
			if calls['a'] != 1 || calls['b'] != 2 || calls['c'] != 1 {
				t.Fatalf("继续只能请求未完成的段落，calls=%v", calls)
			}
		})
	}
}

func TestDescriptionCancelledCallbackCannotFinishNewSameSourceTask(t *testing.T) {
	a, cfg := newTestApp(t)
	a.settings, a.trans.on = trReadySettings(), true
	s := &store.Skill{Name: "alpha", State: store.StateEnabled, Dir: cfg.SkillPath("alpha"), Description: "same source"}
	a.skills = []*store.Skill{s}
	a.selectSkill(s)
	var jobs []func()
	a.spawn = func(fn func()) { jobs = append(jobs, fn) }
	output := "old result"
	a.translate = func(context.Context, store.Settings, string) (string, error) { return output, nil }
	a.ensureCurrentTranslations()
	jobs[0]() // 旧请求已完成，但回调还在 UI 队列里。
	a.SetTranslateOn(false)
	a.SetTranslateOn(true)
	a.flush()
	for _, key := range []string{detailDescKey(s.Name), cardDescKey(s.Name)} {
		if e := a.trans.items[key]; !e.pending || e.dst != "" {
			t.Fatalf("取消的旧回调不能完成新的同源任务：key=%s entry=%+v", key, e)
		}
	}
	output = "new result"
	jobs[1]()
	a.flush()
	for _, key := range []string{detailDescKey(s.Name), cardDescKey(s.Name)} {
		if e := a.trans.items[key]; e.pending || e.dst != output {
			t.Fatalf("新任务结果应写入两个位置：key=%s entry=%+v", key, e)
		}
	}
	a.SetTranslateOn(false)
	a.SetTranslateOn(true)
	if len(jobs) != 2 {
		t.Fatalf("成功缓存不得因页面切换重新请求，jobs=%d", len(jobs))
	}
}

func TestDescriptionAliasesSharePendingAndCompletedRequest(t *testing.T) {
	a, _ := newTestApp(t)
	a.settings = trReadySettings()
	var jobs []func()
	a.spawn = func(fn func()) { jobs = append(jobs, fn) }
	a.translate = func(context.Context, store.Settings, string) (string, error) { return "translated", nil }
	a.TranslateAsync("detail", "source")
	a.TranslateAsync("detail", "source", "card")
	if len(jobs) != 1 {
		t.Fatal("增加同源显示位置不应再发一次请求")
	}
	jobs[0]()
	a.flush()
	a.TranslateAsync("card", "source", "another")
	if len(jobs) != 1 {
		t.Fatal("已完成的同源结果应复用")
	}
	for _, key := range []string{"detail", "card", "another"} {
		if e := a.trans.items[key]; e.pending || e.dst != "translated" {
			t.Fatalf("显示位置未共享译文：key=%s entry=%+v", key, e)
		}
	}
}

func TestDescriptionChangedSourceCancelsQueuedRequests(t *testing.T) {
	a, _ := newTestApp(t)
	a.settings = trReadySettings()
	var jobs []func()
	a.spawn = func(fn func()) { jobs = append(jobs, fn) }
	var requested []string
	a.translate = func(_ context.Context, _ store.Settings, src string) (string, error) {
		requested = append(requested, src)
		return "translated:" + src, nil
	}
	a.TranslateAsync("detail", "first", "card")
	a.TranslateAsync("detail", "second", "card")
	a.TranslateAsync("detail", "first", "card")
	for _, job := range jobs {
		job()
	}
	a.flush()
	if !reflect.DeepEqual(requested, []string{"first"}) {
		t.Fatalf("原文来回变化后只应发送最新请求：%v", requested)
	}
	if e := a.trans.items["card"]; e.pending || e.dst != "translated:first" {
		t.Fatalf("最终原文的译文不正确：%+v", e)
	}
}

func TestMarketTranslationScopeCancelsClosedOrReplacedDetail(t *testing.T) {
	for _, action := range []string{"close-detail", "close-market", "switch-source"} {
		t.Run(action, func(t *testing.T) {
			a, _ := newTestApp(t)
			a.settings, a.trans.on = trReadySettings(), true
			first := market.Skill{Name: "same", Source: "owner/first"}
			next := market.Skill{Name: "same", Source: "owner/next"}
			for _, sk := range []market.Skill{first, next} {
				d := a.market.doc(marketKey(sk))
				d.loaded, d.doc.Description, d.doc.SkillMD = true, sk.Source+" description", sk.Source+" document"
			}
			var jobs []func()
			a.spawn = func(fn func()) { jobs = append(jobs, fn) }
			var requested []string
			a.translate = func(_ context.Context, _ store.Settings, src string) (string, error) {
				requested = append(requested, src)
				return "translated:" + src, nil
			}
			a.OpenMarketDetail(first)
			a.ensureCurrentTranslations()
			switch action {
			case "close-detail":
				a.CloseMarketDetail()
			case "close-market":
				a.CloseMarket()
			case "switch-source":
				a.OpenMarketDetail(next)
				a.ensureCurrentTranslations()
			}
			for _, job := range jobs {
				job()
			}
			a.flush()
			want := []string(nil)
			if action == "switch-source" {
				want = []string{"owner/next description", "owner/next document"}
			}
			if !reflect.DeepEqual(requested, want) {
				t.Fatalf("旧市场详情不应继续请求：got=%v want=%v", requested, want)
			}
			if e := a.trans.items[marketDocKey(first)]; e.pending || !e.paused {
				t.Fatalf("离开的市场全文应暂停：%+v", e)
			}
		})
	}
}

func TestRootSwitchCancelsQueuedTranslations(t *testing.T) {
	a, cfg := newTestApp(t)
	a.settings, a.trans.on = trReadySettings(), true
	s := &store.Skill{Name: "alpha", State: store.StateEnabled, Dir: cfg.SkillPath("alpha"), Description: "description"}
	a.skills = []*store.Skill{s}
	a.selectSkill(s)
	a.detailSel, a.detailTab = skillKey(s), 1
	a.skillDocs = map[string]fullTextDoc{skillKey(s): {src: "document"}}
	var jobs []func()
	a.spawn = func(fn func()) { jobs = append(jobs, fn) }
	a.translate = func(context.Context, store.Settings, string) (string, error) {
		t.Error("切换根目录后旧任务不应发送")
		return "", nil
	}
	a.ensureCurrentTranslations()
	a.TranslateDocAsync(docTransKey(s.Dir), "document")
	a.SelectRoot(filepath.Join(t.TempDir(), "other-skills"))
	for _, job := range jobs[:2] {
		job()
	}
	a.flush()
	if e := a.trans.items[docTransKey(s.Dir)]; e.pending || !e.paused {
		t.Fatalf("切换根目录应暂停旧全文：%+v", e)
	}
}

func TestTranslationProviderDeadlineKeepsCancellation(t *testing.T) {
	for _, provider := range []string{"", store.TranslationGoogle, store.TranslationMicrosoft, store.TranslationOpenAI} {
		for _, doc := range []bool{false, true} {
			label := provider + "/description"
			if doc {
				label = provider + "/document"
			}
			t.Run(label, func(t *testing.T) {
				a, _ := newTestApp(t)
				a.settings = store.Settings{Provider: provider}
				if provider == store.TranslationOpenAI {
					a.settings = trReadySettings()
				}
				called := false
				a.translate = func(ctx context.Context, _ store.Settings, _ string) (string, error) {
					called = true
					_, deadline := ctx.Deadline()
					if want := provider == store.TranslationOpenAI; deadline != want {
						t.Errorf("整次请求的 deadline 不符：provider=%q got=%v want=%v", provider, deadline, want)
					}
					if ctx.Done() == nil {
						t.Fatal("免费服务也必须保留可取消的 context")
					}
					a.trans.reset()
					if !errors.Is(ctx.Err(), context.Canceled) {
						t.Errorf("配置变化必须立即取消请求：%v", ctx.Err())
					}
					return "", ctx.Err()
				}
				if doc {
					a.TranslateDocAsync("doc", "source")
				} else {
					a.TranslateAsync("desc", "source")
				}
				a.flush()
				if !called {
					t.Fatal("假翻译没有被调用")
				}
			})
		}
	}
}
