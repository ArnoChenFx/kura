package ui

// 技能文本翻译的界面侧：内存缓存 + 进行中/失败标记 + 后台任务。
//
// 边界（硬约束）：
//   - 翻译只用于界面显示。本文件与翻译相关的一切代码都不写技能目录：不改
//     SKILL.md、不写 .kura-meta.json、不碰技能根目录；译文也不落任何缓存
//     文件，只活在进程内存里（translateState.items），退出即丢。
//   - 目标语言是全局一个（设置里选），不是每个技能单独选。
//   - 渲染路径只读缓存；网络请求只经 TranslateAsync 进后台 goroutine。
//
// 翻译策略（取舍）：只翻译「当前选中的技能」（市场详情页里打开的技能同理，见 market.go）——描述在右栏详情与中栏它自己的
// 那张卡片（或列表行）里各出现一次，共用同一次请求；「全文」页在选中后逐段翻译
// 整份 SKILL.md（见 TranslateDocAsync）。其余卡片在译文模式下继续显示原文，不为
// 整页几十张卡片一次性打爆翻译 API；切换选中后新选中的技能才发起翻译，已翻过的
// 命中缓存、不再请求。

import (
	"context"
	"strings"
	"time"

	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/ArnoChenFx/kura/locales"
)

// translateTimeout 是一次翻译请求的总时限（含连接、等待模型与读响应）。
const translateTimeout = 30 * time.Second

// 译文的 key 前缀：同一技能同一字段每次都算出同一个 key，来回切换才能命中
// 缓存。卡片（含列表行）与详情各一个 key，但选中技能的两处共用一次请求。
const (
	translateCardPrefix   = "card:"
	translateDetailPrefix = "detail:"
	translateDescSuffix   = ":description"
)

// docChunkRunes 是全文翻译每段的大致上限（字符数）：切分只在空行处进行，超过
// 上限的单个块整块发送（见 store.SplitForTranslation）。
const docChunkRunes = 1800

// 全文译文的 key：每个技能一份，形如 "doc:<技能名>:full"。
const (
	translateDocPrefix = "doc:"
	translateDocSuffix = ":full"
)

// docTransKey 是技能全文译文的 key。
func docTransKey(name string) string { return translateDocPrefix + name + translateDocSuffix }

// cardDescKey / detailDescKey 是技能描述在卡片与详情里的翻译 key。
func cardDescKey(name string) string   { return translateCardPrefix + name + translateDescSuffix }
func detailDescKey(name string) string { return translateDetailPrefix + name + translateDescSuffix }

// translateEntry 是一个文本的翻译状态：pending / err / dst 三者互斥
// （pending 为真时另两项为空；失败时 err 非空、dst 为空；成功时 dst 非空）。
// src 是发起翻译时的原文：技能的描述后来变了（更新、手改）时，原文对不上，
// 旧译文就不再采用，也不会拿它冒充新原文的译文。
type translateEntry struct {
	pending bool
	err     string
	dst     string
	src     string
	// 全文任务保留已经发布的分段结果；停止或失败后仍可继续，不重复请求成功段。
	doc    bool
	chunks []string
	parts  []string
	paused bool
	retry  bool
	ticket uint64
	cancel context.CancelFunc
}

// translateState 保存每段文本的翻译结果（键：调用方给的稳定 key，如
// "card:<技能名>:description" 或 "detail:<技能名>:description"）以及
// 全局的「显示原文/译文」开关。
//
// 并发：translateState 只在 UI 线程读写，不加锁——begin 在构建/动作里调用，
// finish 经 App.publish 回到 UI 线程调用，lookup / text 在构建时调用。后台
// goroutine 绝不直接碰它（它们只拿到文本与结果，由 publish 的闭包回写）。
type translateState struct {
	on    bool                      // true = 显示译文
	items map[string]translateEntry // 键 → 翻译状态
	// epoch 随 reset 递增：翻译配置（接口、密钥、模型、目标语言）变了，
	// 旧配置下还在路上的请求结果不能写进新缓存（见 App.TranslateAsync）。
	epoch int
	// ctx 属于当前 epoch，reset 时取消并换新：后台任务在 UI 线程发起时拿到它，
	// 每段请求之前检查、并用它派生请求的 context，配置一变，剩下的段不再
	// 发出，在途的请求也被中断（后台 goroutine 不读 epoch 本身）。
	ctx    context.Context
	cancel context.CancelFunc
	// ticket 在同一配置代次内也唯一，避免停止后立刻继续时旧回调覆盖新任务。
	ticket uint64
}

// current 返回当前 epoch 的 context（惰性创建）。只在 UI 线程调用。
func (s *translateState) current() context.Context {
	if s.ctx == nil {
		s.ctx, s.cancel = context.WithCancel(context.Background())
	}
	return s.ctx
}

// newTranslateState 创建空的翻译状态（默认显示原文）。
func newTranslateState() *translateState {
	return &translateState{items: map[string]translateEntry{}}
}

// get 取 key 对应、且原文与 src 一致的条目。
func (s *translateState) get(key, src string) (translateEntry, bool) {
	e, ok := s.items[key]
	if !ok || e.src != src {
		return translateEntry{}, false
	}
	return e, true
}

// lookup 返回 key 的译文；没有、正在翻译或失败时 ok 为 false。
func (s *translateState) lookup(key string) (dst string, ok bool) {
	e, found := s.items[key]
	if !found || e.dst == "" {
		return "", false
	}
	return e.dst, true
}

// text 返回界面应显示的文本：译文可用（且原文没变）时返回译文，否则返回
// 原文 src，保证界面永不留白。是否处于译文模式由调用方判断（见 App.descText）。
func (s *translateState) text(key, src string) string {
	if e, ok := s.get(key, src); ok && e.dst != "" {
		return e.dst
	}
	return src
}

// begin 为 key 占位 pending，返回是否真的开始：该 key 对同一原文已有条目
// （进行中 / 已完成 / 已失败）时返回 false，同一 key 同一原文只发一次，
// 失败也不会在每一帧里自动重试（重试见 clearErrors）。
func (s *translateState) begin(key, src string) bool {
	if _, ok := s.get(key, src); ok {
		return false
	}
	if old := s.items[key]; old.cancel != nil {
		old.cancel()
	}
	s.items[key] = translateEntry{pending: true, src: src}
	return true
}

// finish 记下 key 的翻译结果：err 非空记失败（译文清空），否则记 dst。
// 原文以 begin 时的为准：条目已被 reset 清掉或换成别的原文时不写，
// 避免迟到的结果污染缓存。
func (s *translateState) finish(key, src, dst, err string) {
	e, ok := s.items[key]
	if !ok || e.src != src || !e.pending {
		return
	}
	if err != "" {
		s.items[key] = translateEntry{err: err, src: src}
		return
	}
	s.items[key] = translateEntry{dst: dst, src: src}
}

// clearErrors 丢掉所有失败条目：用户再次切到译文时借此重试失败的文本，
// 不碰成功的译文与进行中的请求。
func (s *translateState) clearErrors() {
	for k, e := range s.items {
		if e.err != "" {
			if e.doc {
				// 切回译文与显式继续走同一条续译路径，不能丢掉已经成功的段落。
				e.err, e.retry = "", true
				s.items[k] = e
			} else {
				delete(s.items, k)
			}
		}
	}
}

// reset 清空缓存并让所有在途结果作废：翻译配置变了，旧译文不再适用。
func (s *translateState) reset() {
	s.items = map[string]translateEntry{}
	s.epoch++
	if s.cancel != nil {
		s.cancel()
	}
	s.ctx, s.cancel = nil, nil
}

// descText 返回技能描述在界面上应显示的文本：译文模式下用缓存的译文（拿
// 不到就退回原文），否则原文。prefix 区分位置（translateCardPrefix /
// translateDetailPrefix）。只读缓存，绝不发请求。
func (a *App) descText(prefix string, s *store.Skill) string {
	if !a.trans.on || strings.TrimSpace(s.Description) == "" {
		return s.Description
	}
	return a.trans.text(prefix+s.Name+translateDescSuffix, s.Description)
}

// SetTranslateOn 切换「原文 / 译文」。切到译文时重试之前失败的文本，并立刻
// 为选中的技能发起翻译（设置未就绪时不发，界面给引导）。只改内存状态与
// 发起后台任务，不写磁盘。
func (a *App) SetTranslateOn(on bool) {
	if a.trans.on == on {
		return
	}
	a.trans.on = on
	if on {
		a.trans.clearErrors()
		a.ensureCurrentTranslations()
	}
}

// ensureTranslations 为选中的技能发起描述翻译（若需要）：译文模式开着、
// 翻译 API 已就绪、描述非空、且缓存里还没有它。每次构建都可以调用——
// begin 保证同一原文只发一次，所以它不是「渲染路径上的副作用」，而是
// 受缓存占位保护的任务（与 startFullTextLoad 同一做法）。
func (a *App) ensureTranslations(s *store.Skill) {
	if s == nil || !a.trans.on || !a.settings.Ready() || strings.TrimSpace(s.Description) == "" {
		return
	}
	a.TranslateAsync(detailDescKey(s.Name), s.Description, cardDescKey(s.Name))
}

// TranslateAsync 在后台翻译 text，把结果写进 key（以及 alias 里的各个 key，
// 它们共用这一次请求）。
//
//   - 发起前先在 UI 线程 begin 占位 pending：同一 key 同一原文只发一次。
//   - 请求带 translateTimeout 的超时，走注入的 a.translate（测试换成假实现，
//     绝不联网）；goroutine 里不碰任何 UI 状态。发起时固定设置副本，请求
//     用的就是这一份；请求的 context 派生自当前 epoch，配置变了即被取消。
//   - 结果经 a.publish 回到 UI 线程写缓存，下一帧读到；配置变了
//     （epoch 不同）的旧结果直接丢弃。
//   - 失败只把错误文本记进条目供界面显示；错误里若出现 API key，先抹掉
//     （store 层本就不会带上它，这里再兜一层）。
func (a *App) TranslateAsync(key, text string, alias ...string) {
	var keys []string
	for _, k := range append([]string{key}, alias...) {
		if a.trans.begin(k, text) {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return
	}
	fn, epoch, cfg, live := a.translate, a.trans.epoch, a.settings, a.trans.current()
	secret := cfg.APIKey
	a.spawn(func() {
		ctx, cancel := context.WithTimeout(live, translateTimeout)
		defer cancel()
		var (
			dst    string
			errMsg string
		)
		if fn == nil {
			errMsg = store.ErrNotConfigured.Error()
		} else if live.Err() != nil {
			errMsg = locales.UiTranslateConfigChanged() // 发起后配置就变了：不再发请求，结果也会被丢弃
		} else if out, err := fn(ctx, cfg, text); err != nil {
			errMsg = redactSecret(err.Error(), secret)
		} else if strings.TrimSpace(out) == "" {
			errMsg = locales.UiTranslateEmptyResult()
		} else {
			dst = out
		}
		a.publish(func() {
			if epoch != a.trans.epoch {
				return // 翻译配置已变，这份旧结果不要
			}
			for _, k := range keys {
				a.trans.finish(k, text, dst, errMsg)
			}
		})
	})
}

// redactSecret 把 msg 里出现的 secret 换成 ***。secret 为空时原样返回。
func redactSecret(msg, secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return msg
	}
	return strings.ReplaceAll(msg, secret, "***")
}

// ensureDocTranslation 为技能 name 的全文发起翻译（若需要）：译文模式开着、翻译
// API 已就绪、全文非空，且缓存里还没有它。begin 保证同一原文只发一次，与
// ensureTranslations 同一做法：它是受缓存占位保护的任务，不是渲染路径上的副作用。
func (a *App) ensureDocTranslation(name, src string) {
	if !a.trans.on || !a.settings.Ready() || strings.TrimSpace(src) == "" {
		return
	}
	a.TranslateDocAsync(docTransKey(name), src)
}

// TranslateDocAsync 先查缓存再启动后台分段；已完成、失败或停止的任务不因重绘重发。
// 失败任务可以由用户切回译文或点击继续来恢复，已经成功的段落只保留在内存里。
func (a *App) TranslateDocAsync(key, src string) {
	if src == "" {
		return
	}
	if e, ok := a.trans.get(key, src); ok && !e.retry {
		return
	}
	a.startDocTranslation(key, src)
}

// StopDocTranslation 立即停止该份全文的请求并保留已经发布的成功段。
// 只作用于匹配原文的任务，旧页面上的动作不能中止后来打开的同名文档。
func (a *App) StopDocTranslation(key, src string) {
	e, ok := a.trans.get(key, src)
	if !ok || !e.doc || !e.pending {
		return
	}
	if e.cancel != nil {
		e.cancel()
	}
	a.trans.ticket++
	e.ticket, e.cancel = a.trans.ticket, nil
	e.pending, e.paused, e.retry, e.err = false, true, false, ""
	a.trans.items[key] = e
}

// ResumeDocTranslation 由明确的继续动作调用；续译失败段及其后内容，不重发成功段。
func (a *App) ResumeDocTranslation(key, src string) {
	e, ok := a.trans.get(key, src)
	if !ok || !e.doc || e.pending || e.dst != "" || !a.settings.Ready() {
		return
	}
	a.startDocTranslation(key, src)
}

// startDocTranslation 在 UI 线程固定配置、任务票号和已完成结果，后台只操作副本。
func (a *App) startDocTranslation(key, src string) {
	e, same := a.trans.get(key, src)
	if !same || !e.doc {
		if old := a.trans.items[key]; old.cancel != nil {
			old.cancel()
		}
		e = translateEntry{src: src, doc: true}
	}
	a.trans.ticket++
	live, cancel := context.WithCancel(a.trans.current())
	e.pending, e.paused, e.retry, e.err = true, false, false, ""
	e.ticket, e.cancel = a.trans.ticket, cancel
	a.trans.items[key] = e
	// chunks 在首次分段后不可变；parts 的后台副本与 UI 进度不共享可写底层数组。
	chunks, parts := e.chunks, append([]string(nil), e.parts...)
	fn, epoch, cfg, ticket := a.translate, a.trans.epoch, a.settings, e.ticket
	secret := cfg.APIKey
	a.spawn(func() {
		defer cancel()
		if live.Err() != nil {
			return
		}
		if chunks == nil {
			chunks = store.SplitForTranslation(src, docChunkRunes)
			a.publish(func() {
				if current, ok := a.docRequest(key, src, epoch, ticket); ok {
					current.chunks = chunks
					a.trans.items[key] = current
				}
			})
		}
		var errMsg string
		if fn == nil {
			errMsg = store.ErrNotConfigured.Error()
		}
		for i := len(parts); errMsg == "" && i < len(chunks); i++ {
			if live.Err() != nil {
				return
			}
			ctx, cancelPart := context.WithTimeout(live, translateTimeout)
			out, err := fn(ctx, cfg, chunks[i])
			cancelPart()
			if live.Err() != nil {
				return
			}
			switch {
			case err != nil:
				errMsg = locales.UiTranslateChunkFailed(i+1, len(chunks), redactSecret(err.Error(), secret))
			case strings.TrimSpace(out) == "":
				errMsg = locales.UiTranslateChunkEmpty(i+1, len(chunks))
			default:
				parts = append(parts, out)
				// 每段都发布进度；单独传索引和译文，避免后台与 UI 共同追加一个切片。
				index, translated := i, out
				a.publish(func() {
					if current, ok := a.docRequest(key, src, epoch, ticket); ok && len(current.parts) == index {
						current.parts = append(current.parts, translated)
						a.trans.items[key] = current
					}
				})
			}
		}
		dst := strings.Join(parts, "\n\n")
		a.publish(func() {
			current, ok := a.docRequest(key, src, epoch, ticket)
			if !ok {
				return
			}
			current.pending, current.cancel, current.err = false, nil, errMsg
			if errMsg == "" {
				current.dst = dst
			}
			a.trans.items[key] = current
		})
	})
}

// docRequest 只在 UI 线程检查回调的身份，屏蔽停止、续译、切配置和换原文后的旧结果。
func (a *App) docRequest(key, src string, epoch int, ticket uint64) (translateEntry, bool) {
	if epoch != a.trans.epoch {
		return translateEntry{}, false
	}
	e, ok := a.trans.get(key, src)
	return e, ok && e.doc && e.pending && e.ticket == ticket
}
