// selfupdate 包第二批测试：转发函数在插件未挂载时的行为，以及 WatchUpdateFound
// 的探测逻辑（去重、失败不动状态、一次只跑一个查询）。
//
// 边界（硬约束）：测试里绝不挂更新插件、绝不联网。updater 包的任何函数在插件
// 未挂载时都会 panic（used()），这正是「internal/ui 里绝不直接 import 插件、
// 一律经 main.go 注入钩子」这条约束的理由，值得把它固定成测试而不是只写在注释里。
//
// 为什么「未挂载」要另起一个进程：本包的 TestInstallRegistersPlugin 会真的挂一次
// 插件（同一个测试二进制里只能挂一次），此后同进程里 updater.* 就不再 panic 了。
// 所以用 Go 的标准做法——把测试二进制自己再跑一遍（见 TestForwardsPanic...）：
// 子进程只跑这一个测试、不挂插件，父进程断言它「四个转发函数都 panic 了」。
package selfupdate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"
)

// panicChildEnv 置上时，本测试作为子进程运行：逐个调用转发函数，确认它们都
// panic；有任何一个没 panic 就以非 0 退出。
const panicChildEnv = "KURA_SELFUPDATE_PANIC_CHILD"

// forwards 是这几个薄转发的调用方式（与生产调用点一一对应）。
func forwards() map[string]func() {
	return map[string]func(){
		"CheckForUpdates":    CheckForUpdates,
		"AutomaticChecks":    func() { _ = AutomaticChecks() },
		"SetAutomaticChecks": func() { SetAutomaticChecks(false) },
		"WatchUpdateFound":   func() { _ = WatchUpdateFound(func(bool) {}) },
	}
}

// panicked 报告 fn 是否 panic（恢复后不放出去）。
func panicked(fn func()) (yes bool) {
	defer func() { yes = recover() != nil }()
	fn()
	return false
}

// TestForwardsPanicWithoutPlugin 验证四个转发函数在插件未挂载时都 panic：
// 它们是「依赖插件已挂载」的薄层，界面绝不直接调，只由 main.go 在 Install
// 之后接线。父进程跑子进程（见文件头注释）。
func TestForwardsPanicWithoutPlugin(t *testing.T) {
	if os.Getenv(panicChildEnv) == "1" {
		for name, fn := range forwards() {
			if !panicked(fn) {
				fmt.Fprintf(os.Stderr, "%s 在插件未挂载时没有 panic\n", name)
				os.Exit(1)
			}
		}
		os.Exit(0)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestForwardsPanicWithoutPlugin$", "-test.v")
	cmd.Env = append(os.Environ(), panicChildEnv+"=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Errorf("子进程里四个转发函数应全部 panic（它自己会用非 0 退出报告没 panic 的那个）：%v\n%s",
			err, stderr.String())
	}
}

// TestCheckForUpdateNeedsARealBuild 验证 kura 自己那次查询在开发构建里是失败：
// mygo.Updater.Check 返回 mygo.ErrUpdatesDisabled（没有打包、没有签名更新清单），
// 于是「有没有新版本」这件事问不出来——WatchUpdateFound 据此不动蓝点（见
// watchUpdateFound 的错误分支），不会因为开发构建把用户看到的提示抹掉。
func TestCheckForUpdateNeedsARealBuild(t *testing.T) {
	available, err := checkForUpdate(context.Background())
	if err == nil {
		t.Fatalf("开发构建（go test）里查询应失败，得到 available=%v err=nil", available)
	}
	if available {
		t.Error("查询失败时不应报告有新版本")
	}
}

// fakeChange 模拟插件的 OnChange：watch 调用它注册回调，emit 触发一次。
type fakeChange struct {
	mu   sync.Mutex
	call func()
}

func (f *fakeChange) onchange(fn func()) func() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.call = fn
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.call = nil
	}
}

func (f *fakeChange) emit() {
	f.mu.Lock()
	fn := f.call
	f.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// TestWatchUpdateFoundDeduplicates 验证去重：LastCheck 没变的事件（用户动开关、
// 回答插件的更新窗口）不触发查询；LastCheck 变了才查一次，并把结果交给 found。
func TestWatchUpdateFoundDeduplicates(t *testing.T) {
	ch := &fakeChange{}
	var mu sync.Mutex
	last := time.Unix(1000, 0)
	var checks int
	available := true
	got := make(chan bool, 8)
	watchUpdateFound(func(a bool) { got <- a },
		ch.onchange,
		func() time.Time { mu.Lock(); defer mu.Unlock(); return last },
		func(context.Context) (bool, error) {
			mu.Lock()
			defer mu.Unlock()
			checks++
			return available, nil
		})

	// 第一次事件：LastCheck 是零值 → 查一次。
	ch.emit()
	if a := waitFound(t, got); !a {
		t.Error("查到新版本应报 true")
	}
	// 同一个 LastCheck 再来两次（相当于用户动开关）：不再查。
	ch.emit()
	ch.emit()
	mu.Lock()
	if checks != 1 {
		t.Errorf("LastCheck 没变不应重复查询，查询次数 = %d", checks)
	}
	mu.Unlock()

	// LastCheck 前进（一次成功的检查）：再查一次，这次没有新版本。
	mu.Lock()
	last = last.Add(time.Hour)
	available = false
	mu.Unlock()
	ch.emit()
	if a := waitFound(t, got); a {
		t.Error("已是最新应报 false")
	}
	mu.Lock()
	if checks != 2 {
		t.Errorf("LastCheck 变了应再查一次，查询次数 = %d", checks)
	}
	mu.Unlock()
}

// TestWatchUpdateFoundKeepsStateOnError 验证查询失败时既不报告、也不把
// 上一次的结果抹掉（不动状态），并且失败不挡住下一次查询。
func TestWatchUpdateFoundKeepsStateOnError(t *testing.T) {
	ch := &fakeChange{}
	var mu sync.Mutex
	last := time.Unix(2000, 0)
	fail := false
	reports := make(chan bool, 4)
	watchUpdateFound(func(a bool) { reports <- a },
		ch.onchange,
		func() time.Time { mu.Lock(); defer mu.Unlock(); return last },
		func(context.Context) (bool, error) {
			mu.Lock()
			defer mu.Unlock()
			if fail {
				return false, errors.New("network is down")
			}
			return true, nil
		})

	ch.emit()
	if a := waitFound(t, reports); !a {
		t.Fatal("第一次查询成功应报有新版本")
	}

	// 下一轮查询失败：什么都不报（状态保持不变）。
	mu.Lock()
	last = last.Add(time.Hour)
	fail = true
	mu.Unlock()
	ch.emit()
	select {
	case a := <-reports:
		t.Errorf("查询失败时不应报告，却收到 %v", a)
	case <-time.After(200 * time.Millisecond):
	}

	// 失败后再来一次事件（LastCheck 仍相同）：因为上次没问出答案，应该重问。
	mu.Lock()
	fail = false
	mu.Unlock()
	ch.emit()
	if a := waitFound(t, reports); !a {
		t.Error("上一次失败后应能重问并报告结果")
	}
}

// TestWatchUpdateFoundOneQueryAtATime 验证一次只跑一个查询：上一次还在路上时
// 再来事件直接跳过，不排一堆并发请求。
func TestWatchUpdateFoundOneQueryAtATime(t *testing.T) {
	ch := &fakeChange{}
	var mu sync.Mutex
	last := time.Unix(3000, 0)
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	var checks int
	reports := make(chan bool, 4)
	watchUpdateFound(func(a bool) { reports <- a },
		ch.onchange,
		func() time.Time { mu.Lock(); defer mu.Unlock(); return last },
		func(context.Context) (bool, error) {
			mu.Lock()
			checks++
			mu.Unlock()
			started <- struct{}{}
			<-release
			return true, nil
		})

	ch.emit() // 第一个查询开始，卡在 release 上
	<-started
	// 路上再加两个事件：直接跳过。
	mu.Lock()
	last = last.Add(time.Hour)
	mu.Unlock()
	ch.emit()
	ch.emit()
	mu.Lock()
	if checks != 1 {
		t.Errorf("查询在路上时不应再起一个，查询次数 = %d", checks)
	}
	mu.Unlock()
	close(release)
	if a := waitFound(t, reports); !a {
		t.Error("第一个查询完成后应报告结果")
	}
}

// waitFound 等一次 found 回调，超时即失败。
func waitFound(t *testing.T, ch <-chan bool) bool {
	t.Helper()
	select {
	case a := <-ch:
		return a
	case <-time.After(2 * time.Second):
		t.Fatal("等待 found 回调超时")
		return false
	}
}
