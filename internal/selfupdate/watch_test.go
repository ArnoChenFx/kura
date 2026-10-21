// selfupdate 包的后台检查测试（watch.go）。
//
// 边界（硬约束）：这里绝不挂更新插件、绝不联网、绝不真的等待。更新源、插件的偏好与
// 状态都由 fakeHost 提供并记录；时间由 fakeClock 推进，到期的定时器同步触发，所以
// 每个断言都是确定的。与生产代码只差一处：host 与时钟换成了假的，检查的编排逻辑
// （Prepare / Start / SetAutomaticChecks / fire / finish）是同一份。
package selfupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ArnoChenFx/kura/internal/store"
)

const day = 24 * time.Hour

// fakeClock 是假时钟：时间只在 advance 时前进，到期的假定时器按时间顺序同步触发。
type fakeClock struct {
	mu     sync.Mutex
	at     time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	c      *fakeClock
	due    time.Time
	fn     func()
	active bool
}

func newFakeClock() *fakeClock {
	return &fakeClock{at: time.Date(2026, time.October, 18, 9, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *fakeClock) after(d time.Duration, fn func()) stopper {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{c: c, due: c.at.Add(d), fn: fn, active: true}
	c.timers = append(c.timers, t)
	return t
}

func (t *fakeTimer) Stop() bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	was := t.active
	t.active = false
	return was
}

// advance 把时间推进 d，按到期时间顺序同步触发定时器。回调里新排的定时器只要在
// 这段时间里到期，也会在这次 advance 里触发。
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	end := c.at.Add(d)
	c.mu.Unlock()
	for {
		c.mu.Lock()
		var next *fakeTimer
		for _, t := range c.timers {
			if t.active && !t.due.After(end) && (next == nil || t.due.Before(next.due)) {
				next = t
			}
		}
		if next == nil {
			c.at = end
			c.mu.Unlock()
			return
		}
		next.active = false
		c.at = next.due
		c.mu.Unlock()
		next.fn()
	}
}

// armed 返回还在等待触发的定时器个数。
func (c *fakeClock) armed() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, t := range c.timers {
		if t.active {
			n++
		}
	}
	return n
}

// fakeHost 模拟 MyGo 更新器与它的插件：更新源返回什么、插件记着什么偏好、写过什么，
// 都由测试设置或记录。
type fakeHost struct {
	mu sync.Mutex

	canUpdate     bool
	version       string // 当前运行的应用版本，空串即版本未知（开发构建）
	latest        string // 更新源的最新版本，空串即已是最新
	checkErr      error
	beforeCheck   func() // 非 nil 时每次检查都在它返回之后才有结果
	checks        int
	autoDownloads bool
	skipped       string
	pluginLast    time.Time
	legacy        *bool
	pluginOn      bool
	pluginWrites  []bool
	onChanges     []func()
	onResumes     []func()
}

func (h *fakeHost) enabled() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.canUpdate
}

func (h *fakeHost) check(context.Context) (string, bool, error) {
	h.mu.Lock()
	h.checks++
	before := h.beforeCheck
	h.mu.Unlock()
	if before != nil {
		before()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.checkErr != nil {
		return "", false, h.checkErr
	}
	if h.latest == "" {
		return "", false, nil
	}
	return h.latest, true, nil
}

func (h *fakeHost) automaticDownloads() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.autoDownloads
}

func (h *fakeHost) lastCheck() time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.pluginLast
}

func (h *fakeHost) appVersion() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.version
}

func (h *fakeHost) skippedVersion() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.skipped
}

func (h *fakeHost) legacyAutomaticChecks() *bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.legacy
}

func (h *fakeHost) setAutomaticChecks(on bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pluginOn = on
	h.pluginWrites = append(h.pluginWrites, on)
}

func (h *fakeHost) onChange(fn func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onChanges = append(h.onChanges, fn)
}

func (h *fakeHost) onResume(fn func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onResumes = append(h.onResumes, fn)
}

// emitChange 模拟插件的 OnChange：偏好或检查时间变了，回调在主线程上被调用。
func (h *fakeHost) emitChange() {
	h.mu.Lock()
	fns := slices.Clone(h.onChanges)
	h.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

func (h *fakeHost) setLatest(v string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.latest = v
}

func (h *fakeHost) setCheckErr(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checkErr = err
}

func (h *fakeHost) setAutoDownloads(on bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.autoDownloads = on
}

func (h *fakeHost) checkCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.checks
}

func (h *fakeHost) hookCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.onChanges) + len(h.onResumes)
}

// plugin 返回插件调度开关的当前值与写过的全部值。
func (h *fakeHost) plugin() (on bool, writes []bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.pluginOn, slices.Clone(h.pluginWrites)
}

// rig 把 Background 接到假时钟、假 host 与一个临时的 config.json 上。
type rig struct {
	clock *fakeClock
	host  *fakeHost
	b     *Background

	mu      sync.Mutex
	reports []bool
}

// newRig 用 prepare（生产的 Prepare 去掉定时器与线程之后的同一条路径）建立 Background，
// 并把时间与定时器换成假的。它不调用 Start：需要先检查 Prepare 结果的测试自己调用 start。
func newRig(t *testing.T, h *fakeHost, cfg *store.Config) *rig {
	t.Helper()
	b, err := prepare(cfg, h)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	r := &rig{clock: newFakeClock(), host: h, b: b}
	b.now = r.clock.now
	b.after = r.clock.after
	b.spawn = func(fn func()) { fn() }
	return r
}

func (r *rig) start() {
	r.b.Start(func(lit bool) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.reports = append(r.reports, lit)
	})
}

// reported 返回蓝点收到过的全部亮灭，按时间先后。
func (r *rig) reported() []bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.reports)
}

func newTestConfig(t *testing.T) *store.Config {
	t.Helper()
	root := t.TempDir()
	cfg, err := store.NewConfig(filepath.Join(root, "skills"), filepath.Join(root, ".kura"))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// setPref 把 config.json 里的「自动检查更新」偏好写成 on（走 store 的正式写入口）。
func setPref(t *testing.T, cfg *store.Config, on bool) {
	t.Helper()
	lib, err := store.LoadLibrary(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.SetAutoCheck(on); err != nil {
		t.Fatal(err)
	}
}

func boolPtr(v bool) *bool { return &v }

// TestBackgroundFoundLightsDotWithoutWindow 验证：后台查到新版本只亮蓝点。首检在启动约
// 10 秒后；检查模式下插件的调度开关始终关着（它一开就会自己弹窗）。
func TestBackgroundFoundLightsDotWithoutWindow(t *testing.T) {
	h := &fakeHost{canUpdate: true, latest: "0.2.0"}
	r := newRig(t, h, newTestConfig(t))
	r.start()
	if n := r.clock.armed(); n != 1 {
		t.Fatalf("启动后应只排定一次首检，得到 %d 个定时器", n)
	}
	if h.checkCount() != 0 {
		t.Fatal("启动那一刻不应立刻检查")
	}
	r.clock.advance(firstCheckWait)
	if got := r.reported(); !slices.Equal(got, []bool{true}) {
		t.Fatalf("查到新版本应亮一次蓝点，得到 %v", got)
	}
	if h.checkCount() != 1 {
		t.Fatalf("首检应只查一次，得到 %d", h.checkCount())
	}
	if on, writes := h.plugin(); on || len(writes) != 0 {
		t.Errorf("检查模式下插件的调度开关必须关着（否则它会自己弹窗）：on=%v writes=%v", on, writes)
	}
}

// TestBackgroundNoUpdateClearsDot 验证：之后的检查若没有新版本，蓝点熄灭。
func TestBackgroundNoUpdateClearsDot(t *testing.T) {
	h := &fakeHost{canUpdate: true, latest: "0.2.0"}
	r := newRig(t, h, newTestConfig(t))
	r.start()
	r.clock.advance(firstCheckWait)
	h.setLatest("")
	r.clock.advance(day)
	if got := r.reported(); !slices.Equal(got, []bool{true, false}) {
		t.Errorf("蓝点应先亮后灭，得到 %v", got)
	}
}

// TestBackgroundSkippedVersionDoesNotLightDot 验证：用户在更新窗口里跳过的版本不亮蓝点，
// 比它更新的版本仍然会亮。
func TestBackgroundSkippedVersionDoesNotLightDot(t *testing.T) {
	h := &fakeHost{canUpdate: true, latest: "0.2.0", skipped: "0.2.0"}
	r := newRig(t, h, newTestConfig(t))
	r.start()
	r.clock.advance(firstCheckWait)
	if got := r.reported(); !slices.Equal(got, []bool{false}) {
		t.Fatalf("跳过的版本不应亮蓝点，得到 %v", got)
	}
	h.setLatest("0.3.0")
	r.clock.advance(day)
	if got := r.reported(); !slices.Equal(got, []bool{false, true}) {
		t.Errorf("比跳过的版本更新的版本应亮蓝点，得到 %v", got)
	}
}

// TestBackgroundCadenceIsADayAfterTheLastSuccess 验证节奏：首检之后每 24 小时一次，
// 间隔从上一次成功检查算起。
func TestBackgroundCadenceIsADayAfterTheLastSuccess(t *testing.T) {
	h := &fakeHost{canUpdate: true}
	r := newRig(t, h, newTestConfig(t))
	r.start()
	r.clock.advance(firstCheckWait)
	r.clock.advance(day - time.Second)
	if h.checkCount() != 1 {
		t.Fatalf("距上次检查不满一天不应再查，查询次数 %d", h.checkCount())
	}
	r.clock.advance(time.Second)
	if h.checkCount() != 2 {
		t.Errorf("满一天应再查一次，查询次数 %d", h.checkCount())
	}
}

// TestBackgroundFailureKeepsDotAndRetriesInAnHour 验证：检查失败不动蓝点，一小时后再试。
func TestBackgroundFailureKeepsDotAndRetriesInAnHour(t *testing.T) {
	h := &fakeHost{canUpdate: true, latest: "0.2.0"}
	r := newRig(t, h, newTestConfig(t))
	r.start()
	r.clock.advance(firstCheckWait)
	h.setCheckErr(errors.New("network is down"))
	r.clock.advance(day)
	if got := r.reported(); !slices.Equal(got, []bool{true}) {
		t.Fatalf("检查失败不应改动蓝点，得到 %v", got)
	}
	n := h.checkCount()
	r.clock.advance(time.Hour - time.Second)
	if h.checkCount() != n {
		t.Fatal("失败之后不应在一小时之内再查")
	}
	r.clock.advance(time.Second)
	if h.checkCount() != n+1 {
		t.Errorf("失败之后一小时应再查，查询次数 %d", h.checkCount())
	}
}

// TestBackgroundOffMeansNoChecks 验证：偏好关着时 kura 不查，插件也不调度，蓝点是熄灭的。
func TestBackgroundOffMeansNoChecks(t *testing.T) {
	cfg := newTestConfig(t)
	setPref(t, cfg, false)
	h := &fakeHost{canUpdate: true, latest: "0.2.0"}
	r := newRig(t, h, cfg)
	r.start()
	if r.clock.armed() != 0 {
		t.Errorf("偏好关着时不应排定任何检查，定时器 %d 个", r.clock.armed())
	}
	r.clock.advance(3 * day)
	if h.checkCount() != 0 {
		t.Errorf("偏好关着时不应检查，查询次数 %d", h.checkCount())
	}
	if on, writes := h.plugin(); on || len(writes) != 0 {
		t.Errorf("偏好关着时插件的调度开关应保持关：on=%v writes=%v", on, writes)
	}
	if got := r.reported(); !slices.Equal(got, []bool{false}) {
		t.Errorf("没有检查时蓝点应是熄灭的，得到 %v", got)
	}
}

// TestBackgroundSwitchingOffStopsAndClearsDot 验证：亮着蓝点时关掉「自动检查更新」，
// 蓝点熄灭，之后不再查。
func TestBackgroundSwitchingOffStopsAndClearsDot(t *testing.T) {
	h := &fakeHost{canUpdate: true, latest: "0.2.0"}
	r := newRig(t, h, newTestConfig(t))
	r.start()
	r.clock.advance(firstCheckWait)
	r.b.SetAutomaticChecks(false)
	r.clock.advance(3 * day)
	if got := r.reported(); !slices.Equal(got, []bool{true, false}) {
		t.Errorf("关掉之后蓝点应熄灭，得到 %v", got)
	}
	if h.checkCount() != 1 {
		t.Errorf("关掉之后不应再查，查询次数 %d", h.checkCount())
	}
}

// TestBackgroundSwitchingOnChecksAgain 验证：从关打开之后，启动后很快会查一次。
func TestBackgroundSwitchingOnChecksAgain(t *testing.T) {
	cfg := newTestConfig(t)
	setPref(t, cfg, false)
	h := &fakeHost{canUpdate: true, latest: "0.2.0"}
	r := newRig(t, h, cfg)
	r.start()
	r.b.SetAutomaticChecks(true)
	r.clock.advance(firstCheckWait)
	if got := r.reported(); !slices.Equal(got, []bool{false, true}) {
		t.Errorf("打开之后应查到并亮蓝点，得到 %v", got)
	}
}

// TestBackgroundAutoDownloadHandsSchedulingToThePlugin 验证静默安装那一档：勾了自动下载，
// 插件的调度开关打开（由它静默下载安装），kura 自己不查、不亮蓝点；取消勾选后检查交还
// 给 kura。
func TestBackgroundAutoDownloadHandsSchedulingToThePlugin(t *testing.T) {
	h := &fakeHost{canUpdate: true, latest: "0.2.0", autoDownloads: true}
	r := newRig(t, h, newTestConfig(t))
	r.start()
	if on, writes := h.plugin(); !on || !slices.Equal(writes, []bool{true}) {
		t.Fatalf("勾了自动下载时插件的调度开关应打开：on=%v writes=%v", on, writes)
	}
	r.clock.advance(3 * day)
	if h.checkCount() != 0 {
		t.Errorf("静默安装模式下 kura 不应自己检查，查询次数 %d", h.checkCount())
	}
	if got := r.reported(); slices.Contains(got, true) {
		t.Errorf("静默安装模式下不应亮蓝点，得到 %v", got)
	}

	h.setAutoDownloads(false)
	h.emitChange()
	if on, _ := h.plugin(); on {
		t.Error("取消自动下载后插件的调度开关应关上")
	}
	r.clock.advance(firstCheckWait)
	if h.checkCount() != 1 {
		t.Errorf("取消自动下载后检查交还给 kura，查询次数 %d", h.checkCount())
	}
	if got := r.reported(); !slices.Equal(got, []bool{false, true}) {
		t.Errorf("交还之后查到新版本应亮蓝点，得到 %v", got)
	}
}

// TestBackgroundDevBuildDoesNothing 验证：不能自我更新的构建（开发构建、包管理器安装）
// 什么也不做：不注册插件回调、不排定检查、不写插件的文件、不亮蓝点。
func TestBackgroundDevBuildDoesNothing(t *testing.T) {
	h := &fakeHost{canUpdate: false, latest: "0.2.0", legacy: boolPtr(true)}
	r := newRig(t, h, newTestConfig(t))
	r.start()
	r.clock.advance(3 * day)
	if h.checkCount() != 0 || r.clock.armed() != 0 {
		t.Errorf("不能更新的构建不应检查：查询 %d 次，定时器 %d 个", h.checkCount(), r.clock.armed())
	}
	if _, writes := h.plugin(); len(writes) != 0 {
		t.Errorf("不能更新的构建不应写插件的调度开关，写入 %v", writes)
	}
	if h.hookCount() != 0 {
		t.Errorf("不能更新的构建不应注册插件回调，注册了 %d 个", h.hookCount())
	}
	if got := r.reported(); len(got) != 0 {
		t.Errorf("不能更新的构建不应报告蓝点，得到 %v", got)
	}
}

// TestPrepareMigratesThePluginsPreference 验证旧版本的偏好迁移：config.json 还没有这一项时，
// 用插件 updater.json 里留下的值定下偏好，并把插件的调度开关对齐（只在它要变时才写）。
func TestPrepareMigratesThePluginsPreference(t *testing.T) {
	cases := []struct {
		name       string
		legacy     *bool
		autoDown   bool
		wantPref   bool
		wantWrites []bool
	}{
		{"从未设过：默认开，插件保持关", nil, false, true, nil},
		{"从未设过但勾了自动下载：插件打开调度", nil, true, true, []bool{true}},
		{"旧版本关过：迁移成关", boolPtr(false), false, false, nil},
		{"旧版本开着：插件的自动调度要关掉，由 kura 检查", boolPtr(true), false, true, []bool{false}},
		{"旧版本开着且勾了自动下载：插件继续负责静默安装", boolPtr(true), true, true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newTestConfig(t)
			h := &fakeHost{canUpdate: true, legacy: tc.legacy, autoDownloads: tc.autoDown}
			b, err := prepare(cfg, h)
			if err != nil {
				t.Fatal(err)
			}
			if b.pref != tc.wantPref {
				t.Errorf("偏好 = %v，想要 %v", b.pref, tc.wantPref)
			}
			if _, writes := h.plugin(); !slices.Equal(writes, tc.wantWrites) {
				t.Errorf("插件调度开关的写入 = %v，想要 %v", writes, tc.wantWrites)
			}
			lib, err := store.LoadLibrary(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if lib.AutoCheck != tc.wantPref {
				t.Errorf("config.json 应记下迁移后的偏好 %v，得到 %v", tc.wantPref, lib.AutoCheck)
			}
		})
	}
}

// TestPrepareConfigWinsOverTheLegacyValue 验证：config.json 已经有偏好时以它为准，插件里的
// 旧值不再参与；插件的调度开关仍然被对齐到 kura 说了算的那一个值。
func TestPrepareConfigWinsOverTheLegacyValue(t *testing.T) {
	t.Run("config 开、插件关", func(t *testing.T) {
		cfg := newTestConfig(t)
		setPref(t, cfg, true)
		h := &fakeHost{canUpdate: true, legacy: boolPtr(false)}
		b, err := prepare(cfg, h)
		if err != nil {
			t.Fatal(err)
		}
		if !b.pref {
			t.Error("config.json 说开，就应当是开")
		}
		if _, writes := h.plugin(); len(writes) != 0 {
			t.Errorf("插件已经是关，不应再写，写入 %v", writes)
		}
	})
	t.Run("config 关、插件开", func(t *testing.T) {
		cfg := newTestConfig(t)
		setPref(t, cfg, false)
		h := &fakeHost{canUpdate: true, legacy: boolPtr(true)}
		b, err := prepare(cfg, h)
		if err != nil {
			t.Fatal(err)
		}
		if b.pref {
			t.Error("config.json 说关，就应当是关")
		}
		if on, writes := h.plugin(); on || !slices.Equal(writes, []bool{false}) {
			t.Errorf("插件的旧开关应被关掉：on=%v writes=%v", on, writes)
		}
	})
}

// TestPrepareDamagedConfigDefaultsOnAndKeepsTheFile 验证 config.json 损坏时：偏好按缺省（开），
// 返回错误，文件原样保留，不覆盖用户手改的内容。
func TestPrepareDamagedConfigDefaultsOnAndKeepsTheFile(t *testing.T) {
	cfg := newTestConfig(t)
	if err := os.MkdirAll(cfg.Home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.ConfigPath(), []byte("{bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := prepare(cfg, &fakeHost{canUpdate: true})
	if err == nil {
		t.Fatal("损坏的 config.json 应返回错误")
	}
	if !b.pref {
		t.Error("损坏时偏好应按缺省（开）")
	}
	data, readErr := os.ReadFile(cfg.ConfigPath())
	if readErr != nil || string(data) != "{bad" {
		t.Errorf("损坏的文件不应被覆盖，得到 %q (err=%v)", data, readErr)
	}
}

// TestBackgroundOneCheckAtATime 验证一次只跑一个检查：上一次还在路上时，再来的触发直接跳过，
// 不排一堆并发请求。
func TestBackgroundOneCheckAtATime(t *testing.T) {
	h := &fakeHost{canUpdate: true, latest: "0.2.0"}
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	h.beforeCheck = func() {
		entered <- struct{}{}
		<-release
	}
	r := newRig(t, h, newTestConfig(t))
	r.start()

	done := make(chan struct{})
	go func() {
		r.clock.advance(firstCheckWait)
		close(done)
	}()
	<-entered

	r.b.mu.Lock()
	gen := r.b.gen
	r.b.mu.Unlock()
	r.b.fire(gen)
	if h.checkCount() != 1 {
		t.Fatalf("上一次还在路上时不应再起一个检查，查询次数 %d", h.checkCount())
	}

	close(release)
	<-done
	if got := r.reported(); !slices.Equal(got, []bool{true}) {
		t.Errorf("在路上的那次检查完成后应亮蓝点，得到 %v", got)
	}
}

// setLastCheck 把 config.json 里的「上一次成功检查」写成 at / version（走 store 的正式写入口）。
// version 为空串就是旧版本留下且版本未知的那种记录（写出来的文件里没有版本字段）。
func setLastCheck(t *testing.T, cfg *store.Config, at time.Time, version string) {
	t.Helper()
	if err := store.SetLastUpdateCheck(cfg, store.UpdateCheck{At: at, Version: version}); err != nil {
		t.Fatal(err)
	}
}

// lastCheckRecord 读回 config.json 里的「上一次成功检查」记录。
func lastCheckRecord(t *testing.T, cfg *store.Config) store.UpdateCheck {
	t.Helper()
	rec, err := store.LastUpdateCheck(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

// TestBackgroundChecksSoonAfterTheAppVersionChanges 是「装完新版后二十四小时内不再检查」的
// 回归测试：上一次成功检查发生在升级之前的版本上（二十分钟前），用户又在这时从更新窗口里
// 手动检查并装上了新版（插件 lastCheck 刚被刷新、比那条记录还新）。重启后这两条时间都不能
// 把检查推到二十四小时之后：启动约 10 秒后就要查一次。
func TestBackgroundChecksSoonAfterTheAppVersionChanges(t *testing.T) {
	cfg := newTestConfig(t)
	recent := newFakeClock().now().Add(-20 * time.Minute)
	setLastCheck(t, cfg, recent, "0.1.17")

	// 插件的手动检查比 kura 那条记录还新（5 分钟前），旧行为会拿它把检查推到 24 小时后。
	h := &fakeHost{canUpdate: true, version: "0.1.18", latest: "0.2.0", pluginLast: recent.Add(15 * time.Minute)}
	r := newRig(t, h, cfg)
	r.start()
	r.clock.advance(firstCheckWait)
	if h.checkCount() != 1 {
		t.Fatalf("装完新版本后第一次启动应重新查一次，查询次数 %d", h.checkCount())
	}
	if got := r.reported(); !slices.Equal(got, []bool{true}) {
		t.Errorf("查到新版本应亮蓝点，得到 %v", got)
	}
	// 检查成功后记录里的版本换成当前版本，之后恢复成一天一次，不会反复重查。
	if rec := lastCheckRecord(t, cfg); rec.Version != "0.1.18" {
		t.Errorf("检查成功后应记下当前版本 %q，得到 %q", "0.1.18", rec.Version)
	}
	r.clock.advance(day - time.Second)
	if h.checkCount() != 1 {
		t.Errorf("版本记录对齐之后不该立刻重查，查询次数 %d", h.checkCount())
	}
	r.clock.advance(2 * time.Second)
	if h.checkCount() != 2 {
		t.Errorf("版本记录对齐之后应回到一天一次，查询次数 %d", h.checkCount())
	}
}

// TestBackgroundIgnoresACheckRecordWithoutAVersion 验证旧格式的兼容：旧版本写的 config.json
// 只有 updateCheckedAt、没有版本。版本未知时这条时间不采信（也不采信插件的 lastCheck），
// 宁可多查一次。
func TestBackgroundIgnoresACheckRecordWithoutAVersion(t *testing.T) {
	cfg := newTestConfig(t)
	recent := newFakeClock().now().Add(-20 * time.Minute)
	// 直接写文件，模拟旧版本留下的 config.json（新版本的写入口总是会写版本）。
	if err := os.MkdirAll(cfg.Home, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"roots":[],"autoCheckUpdates":true,"updateCheckedAt":"` + recent.UTC().Format(time.RFC3339) + `"}`
	if err := os.WriteFile(cfg.ConfigPath(), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	h := &fakeHost{canUpdate: true, version: "0.1.18", pluginLast: recent}
	r := newRig(t, h, cfg)
	r.start()
	r.clock.advance(firstCheckWait)
	if h.checkCount() != 1 {
		t.Fatalf("记录里的版本未知时应尽快查一次，查询次数 %d", h.checkCount())
	}
}

// TestBackgroundKeepsTheManualCheckDelayOnTheSameVersion 验证版本没变时行为不变：插件里手动
// 检查的时间仍然推迟下一次后台检查（不重复查更新源）。
func TestBackgroundKeepsTheManualCheckDelayOnTheSameVersion(t *testing.T) {
	cfg := newTestConfig(t)
	recent := newFakeClock().now().Add(-20 * time.Minute)
	setLastCheck(t, cfg, recent, "0.1.17")

	h := &fakeHost{canUpdate: true, version: "0.1.17"}
	r := newRig(t, h, cfg)
	r.start()
	r.clock.advance(firstCheckWait)
	if h.checkCount() != 0 {
		t.Fatalf("同一个版本上刚查过不到一天不应再查，查询次数 %d", h.checkCount())
	}
	r.clock.advance(day)
	if h.checkCount() != 1 {
		t.Errorf("距上次成功满一天应再查一次，查询次数 %d", h.checkCount())
	}
}

// TestBackgroundUnknownAppVersionFallsBackToTheClock 验证当前版本取不到（开发构建、go test 里
// mygo.App.Version() 为空串）时不因此每次都查：按时间排期，刚查过就等一天，不会退化成
// 每 10 秒查一次更新源。不管记录里写的是没有版本、还是别的版本号（比如上一次是打包构建跑的）。
func TestBackgroundUnknownAppVersionFallsBackToTheClock(t *testing.T) {
	for _, version := range []string{"", "0.1.17"} {
		t.Run("记录里的版本="+strconv.Quote(version), func(t *testing.T) {
			cfg := newTestConfig(t)
			recent := newFakeClock().now().Add(-20 * time.Minute)
			setLastCheck(t, cfg, recent, version)

			h := &fakeHost{canUpdate: true, pluginLast: recent} // 当前运行版本也是未知
			r := newRig(t, h, cfg)
			r.start()
			r.clock.advance(firstCheckWait)
			if h.checkCount() != 0 {
				t.Fatalf("版本未知时不应把每次启动都当成「版本变了」立刻查，查询次数 %d", h.checkCount())
			}
			r.clock.advance(day)
			if n := h.checkCount(); n != 1 {
				t.Errorf("版本未知时应回到一天一次，查询次数 %d", n)
			}
		})
	}
}

// TestNextCheck 验证下一次检查的时刻：距上次成功满一个间隔；失败过则不早于失败后一小时；
// 时钟被调回过去时按现在起算。
func TestNextCheck(t *testing.T) {
	now := time.Date(2026, time.October, 18, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name         string
		last, failed time.Time
		want         time.Time
	}{
		{"从未检查：立刻到期", time.Time{}, time.Time{}, time.Time{}.Add(checkInterval)},
		{"一小时前检查过：二十三小时后", now.Add(-time.Hour), time.Time{}, now.Add(23 * time.Hour)},
		{"刚失败：不早于一小时后，但不早于正常的到期", now.Add(-time.Hour), now.Add(-10 * time.Minute), now.Add(23 * time.Hour)},
		{"很久没查且刚失败：失败后一小时", now.Add(-30 * day), now.Add(-10 * time.Minute), now.Add(50 * time.Minute)},
		{"时钟被调回过去：从现在起算一天", now.Add(time.Hour), time.Time{}, now.Add(day)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nextCheck(tc.last, tc.failed, now); !got.Equal(tc.want) {
				t.Errorf("nextCheck = %v，想要 %v", got, tc.want)
			}
		})
	}
}
