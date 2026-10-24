package selfupdate

// watch.go — kura 自己的后台更新检查：查到新版本只亮左栏「检查更新」按钮上的小蓝点，
// 从不打开窗口，也不抢焦点。
//
// 为什么由 kura 排定检查，而不用插件的调度：插件查到新版本后会弹出它自己的更新窗口
// （非用户发起的检查也会显示，抢焦点、闪任务栏），或者（勾了「以后自动下载并安装更新」）
// 静默安装。它没有「只提示」这一档，而它的调度开关和「自动检查更新」是同一个偏好。所以
// 偏好搬进 kura 的 config.json，检查由 kura 排定，只调 mygo.Updater.Check 问一次。
//
// 三种模式，由「自动检查更新」与插件的「以后自动下载并安装更新」共同决定：
//
//   - 关：偏好关了。kura 不查，插件也不调度。
//   - kura 检查（默认）：偏好开、没勾自动下载。每次启动约 10 秒后查一次（只有上次成功
//     检查距今不足 10 分钟才跳过，免得连续重启、崩溃重启反复请求更新源），同一次运行里
//     之后每 24 小时一次。查到且没被跳过的新版本亮蓝点；查不到则熄灭；失败不动蓝点，
//     一小时后重试。插件的调度开关关着，它永远不会自己弹窗。
//   - 插件静默安装：偏好开、勾了自动下载。插件自己查、静默下载，下次启动生效；kura 不再
//     查、不亮蓝点（用户已经选了静默安装，蓝点只会是噪音）。
//
// 上次检查算不算数的判据是「应用版本有没有变」。config.json 里的 updateCheckedAt 带上了
// 那次检查时运行的版本（updateCheckedVersion），插件的 lastCheck 也一并只在这个前提下
// 采信：
//
//   - 记录里的版本 == 当前运行版本（b.version，来自 mygo.App.Version）：用这条时间（以及
//     插件的 lastCheck 中较新的一个）排期，也就是原来的行为——用户在更新窗口里手动检查
//     成功，仍会推迟下一次后台检查。
//   - 版本不同，或记录里的版本未知（旧版本写的 config.json 只有时间）：完全不采信，也不采信
//     插件的 lastCheck，启动约 10 秒后就查一次。装完新版本第一次启动先查一遍就是靠这一条。
//   - 当前运行版本为空（开发构建、go test 取不到版本）：退回按时间排期的老行为，且下一次
//     检查最多被推到「一天后」。否则每次检查写下的记录都对不上版本，会退化成每 10 秒查一次。
//
// 开发构建与包管理器安装（mygo.Updater.Enabled 为假）：什么也不做，插件也不动。
//
// 「稍后提醒我」之后蓝点不会马上回来；下一次后台检查仍发现同一个版本时才亮，与插件的
// 行为一致。「跳过这个版本」之后，同一个版本不再亮蓝点，更新的版本才会。

import (
	"context"
	"sync"
	"time"

	"github.com/ArnoChenFx/kura/internal/store"
)

const (
	// firstCheckWait 是启动之后到第一次检查的等待，与插件一致：先让窗口出来，不和启动抢资源。
	firstCheckWait = 10 * time.Second
	// checkInterval 是两次成功检查之间的间隔，与插件的默认值一致。
	checkInterval = 24 * time.Hour
	// launchInterval 是「刚查过就不必再查」的窗口，只对本次启动的第一次检查有效：上次
	// 成功检查距今不足它就跳过这次启动的检查（连续重启、崩溃重启不反复请求更新源），否则
	// 启动后查一次。它的用意是「每次启动都看一眼有没有新版本」，而不是按上次检查的时刻
	// 排期——用户隔几小时重启一次，也要在启动时知道有没有新版本。
	launchInterval = 10 * time.Minute
	// retryInterval 是检查失败之后最晚再试的间隔，与插件一致。
	retryInterval = time.Hour
	// checkTimeout 是一次检查的总时限：宁可放弃也不挂着，失败只是不动蓝点。
	checkTimeout = 30 * time.Second
)

// stopper 是 time.Timer 的 Stop，测试里换成假定时器。
type stopper interface{ Stop() bool }

// Background 是 kura 的后台更新检查。Prepare 在 mygo.App.Run 之前调用，Start 在窗口
// 就绪后调用，界面切换偏好时调用 SetAutomaticChecks。
type Background struct {
	cfg     *store.Config
	host    host
	enabled bool   // 应用能否自我更新，Prepare 时定下，之后只读
	version string // 当前运行的应用版本，Prepare 时定下，之后只读；空串表示版本未知

	now   func() time.Time
	after func(d time.Duration, fn func()) stopper
	// spawn 在后台运行 fn；生产里是 go fn()，测试里换成同步执行，结果才确定。
	spawn func(fn func())

	// syncMu 串行化「算出插件开关应有的值 → 写进插件」：在它里面才读偏好，
	// 所以最后写进去的总是最新的值。
	syncMu   sync.Mutex
	pluginOn bool // 插件调度开关的当前值，由 syncMu 保护；只在要变时才写，免得每次都改插件文件

	// mu 保护以下字段。found 在持有 mu 时调用，保证亮灭的先后与状态变化一致。
	mu      sync.Mutex
	found   func(lit bool)
	started bool      // Start 之后才排定检查
	pref    bool      // 「自动检查更新」的当前值
	checked time.Time // kura 最近一次成功的检查
	// checkedVersion 是 checked 那次检查时运行的版本；与 version 不一致（或为空）时
	// 这条时间不再推迟检查。
	checkedVersion string
	failed         time.Time // 最近一次失败的检查，零值为没有
	running        bool      // 有检查在路上，一次只跑一个
	// launchCheck 报告本次启动的第一次检查还没有发生：这一轮排期用 launchInterval 的短
	// 窗口，首检一发生（见 fire）或判定不必查（见 waitLocked）就清零，之后一律按
	// nextCheck 的节奏。
	launchCheck bool
	gen         int // 每次重新排定都递增：过期的定时器回调直接作废
	timer       stopper
}

// Prepare 在 mygo.App.Run 之前调用（插件已经挂载，窗口还没有）：定下「自动检查更新」的
// 偏好，把旧版本留在插件 updater.json 里的值迁移进 config.json（见
// store.ResolveAutoCheck），并把插件的调度开关对齐到它应有的值。必须赶在插件启动之前，
// 插件在应用就绪时读开关排定自己的检查，读到旧值就会照旧弹窗。config.json 损坏时偏好按
// 缺省（开）处理并返回错误，应用照常运行。
func Prepare(cfg *store.Config) (*Background, error) {
	return prepare(cfg, productionHost{})
}

func prepare(cfg *store.Config, h host) (*Background, error) {
	b := newBackground(cfg, h)
	legacy := h.legacyAutomaticChecks()
	pref, err := store.ResolveAutoCheck(cfg, legacy)
	b.pref = pref
	b.pluginOn = legacy != nil && *legacy
	b.enabled = h.enabled()
	b.version = h.appVersion()
	if last, readErr := store.LastUpdateCheck(cfg); readErr == nil {
		b.checked = last.At
		b.checkedVersion = last.Version
	}
	b.apply()
	return b, err
}

func newBackground(cfg *store.Config, h host) *Background {
	return &Background{
		cfg:   cfg,
		host:  h,
		now:   time.Now,
		after: func(d time.Duration, fn func()) stopper { return time.AfterFunc(d, fn) },
		spawn: func(fn func()) { go fn() },
		// 每个 Background 就是一次启动，第一次检查的短窗口从它开始算。
		launchCheck: true,
	}
}

// Start 在窗口就绪后调用（主线程）。found 收到蓝点的亮灭，可能在任意 goroutine 里调用，
// 里面不要再调用 Background 的方法。不能自我更新的构建什么也不做。
func (b *Background) Start(found func(lit bool)) {
	b.mu.Lock()
	b.found = found
	b.started = b.enabled
	b.mu.Unlock()
	if !b.enabled {
		return
	}
	b.host.onChange(func() { b.spawn(b.apply) })
	b.host.onResume(func() { b.spawn(b.reschedule) })
	b.spawn(b.apply)
}

// SetAutomaticChecks 应用界面切换后的偏好，立刻据此调整插件的调度开关与 kura 的检查。
// 写 config.json 是界面的事（见 ui.SetAutoCheckAsync），这里只管运行时的状态。
func (b *Background) SetAutomaticChecks(on bool) {
	b.mu.Lock()
	b.pref = on
	b.mu.Unlock()
	b.apply()
}

// apply 把插件的调度开关（只在静默安装那一档开着）与 kura 自己的定时器、蓝点对齐到
// 当前的偏好与「自动下载」上。
func (b *Background) apply() {
	if b.enabled {
		b.syncMu.Lock()
		if want := b.wantPlugin(); want != b.pluginOn {
			b.host.setAutomaticChecks(want)
			b.pluginOn = want
		}
		b.syncMu.Unlock()
	}
	b.reschedule()
}

// wantPlugin 报告插件的调度开关应当开着：偏好开，且用户勾了「以后自动下载并安装更新」。
func (b *Background) wantPlugin() bool {
	b.mu.Lock()
	pref := b.pref
	b.mu.Unlock()
	return pref && b.host.automaticDownloads()
}

// reschedule 按当前模式重新排定（或取消）kura 的下一次检查。不是 kura 检查的模式里熄灭蓝点。
func (b *Background) reschedule() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.gen++
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	if !b.started {
		return
	}
	if !b.ownLocked() {
		b.report(false)
		return
	}
	gen := b.gen
	b.timer = b.after(b.waitLocked(b.lastCheckLocked(), b.now()), func() { b.fire(gen) })
}

// waitLocked 返回排下一次检查要等多久。
//
// 本次启动的第一次检查走短窗口 launchInterval：上次成功检查距今不足它就不查（连续重启、
// 崩溃后重启不反复请求更新源），否则 firstCheckWait 之后就查——这样隔几个小时重启一次也
// 能在启动时看到有没有新版本，不必等到「上次成功检查满 24 小时」。首检之后一律按
// nextCheck：距上次成功检查满一个间隔（24 小时），失败过则一小时后再试。
//
// 只在真的排定首检时才清零 launchCheck：重新排定（系统唤醒、插件状态变化）可能发生在新定时器
// 到点之前，那时候还得继续按首检排，不能把这次检查推到一天后。持有 mu 时调用。
func (b *Background) waitLocked(last, now time.Time) time.Duration {
	// nextCheck 自带 firstCheckWait 的下限：没有可采信的时间（版本变了、旧记录没有版本）时
	// 零值 last 让它立刻到期，于是启动约 10 秒后就查。
	normal := max(nextCheck(last, b.failed, now).Sub(now), firstCheckWait)
	if !b.launchCheck {
		return normal
	}
	if now.Sub(last) < launchInterval {
		// 刚查过：这次启动不必再查，按正常节奏排；首检就此用掉，不再参与之后的排期。
		// 零值的 last 在这里不会满足条件（那是「一直没有可采信的时间」），所以它会去查。
		b.launchCheck = false
		return normal
	}
	return firstCheckWait
}

// lastCheckLocked 返回下一次检查应当据以起算的「上一次成功检查」时刻；零值表示没有可
// 采信的时间，按 firstCheckWait 尽快查一次。持有 mu 时调用。
//
// 判据是版本：插件 updater.json 里的 lastCheck 只记「什么时候查过」，不知道那是哪个版本
// 的检查，而用户手动检查并装上新版之后它就刚被刷新（重启也不会查——这是升级后一直等不到
// 蓝点的成因）。config.json 里的 updateCheckedAt 现在带上了当时的版本，所以：
//
//   - 当前版本为空（开发构建、go test）：退回按时间排期的老行为，且最多推迟一个间隔
//     （nextCheck 本身就不会把检查推到 now+checkInterval 之后），不引入每 10 秒一查的抖动。
//   - 记录里的版本与当前版本相同：用这条时间，并继续采信更新的插件 lastCheck（手动检查成功
//     推迟下一次后台检查，是原有且有意为之的行为）。
//   - 版本不同或未知：两条时间都不采信，尽快查一次。
func (b *Background) lastCheckLocked() time.Time {
	if b.version == "" || b.checkedVersion == b.version {
		last := b.checked
		if t := b.host.lastCheck(); t.After(last) {
			last = t
		}
		return last
	}
	return time.Time{}
}

// ownLocked 报告后台检查由 kura 负责：偏好开，且没有勾插件的静默安装。持有 mu 时调用。
func (b *Background) ownLocked() bool {
	return b.started && b.pref && !b.host.automaticDownloads()
}

// fire 是定时器到点之后的一次检查，在定时器自己的 goroutine 里跑。gen 不是当前排定的那一次、
// 模式已经变了、或上一次还没回来，都直接返回；上一次回来时会自己重新排定。
func (b *Background) fire(gen int) {
	b.mu.Lock()
	if gen != b.gen || b.running || !b.ownLocked() {
		b.mu.Unlock()
		return
	}
	b.running = true
	// 本次启动的第一次检查已经发生：之后的排期回到 nextCheck（24 小时 / 失败一小时重试）。
	b.launchCheck = false
	b.timer = nil
	b.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	version, available, err := b.host.check(ctx)
	cancel()
	lit := available && err == nil && version != b.host.skippedVersion()
	b.finish(lit, err)
}

// finish 记下一次检查的结果，然后排定下一次。成功：记下时间与当时的应用版本（写进
// config.json）并亮灭蓝点；失败：只记下失败的时刻，蓝点保持原样。
func (b *Background) finish(lit bool, err error) {
	now := b.now()
	b.mu.Lock()
	b.running = false
	if err != nil {
		b.failed = now
	} else {
		b.failed = time.Time{}
		b.checked = now
		b.checkedVersion = b.version
		if b.ownLocked() {
			b.report(lit)
		}
	}
	b.mu.Unlock()
	if err == nil {
		// 写不下来只影响下次启动之后何时再查，不值得打扰用户。
		_ = store.SetLastUpdateCheck(b.cfg, store.UpdateCheck{At: now, Version: b.version})
	}
	b.reschedule()
}

// report 把蓝点的状态交给界面。持有 mu 时调用；界面的发布只是投递到主线程，不会阻塞。
func (b *Background) report(lit bool) {
	if b.found != nil {
		b.found(lit)
	}
}

// nextCheck 返回下一次检查的时刻：距上次成功检查满一个间隔；失败过则不早于失败后一小时。
// 与插件的 nextCheck 一致。
func nextCheck(last, failed, now time.Time) time.Time {
	if last.After(now) { // 时钟被调回过去
		last = now
	}
	due := last.Add(checkInterval)
	if retry := failed.Add(retryInterval); !failed.IsZero() && retry.After(due) {
		due = retry
	}
	return due
}
