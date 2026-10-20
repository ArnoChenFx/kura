package ui

import (
	"os"
	"slices"
	"testing"
	"time"

	"github.com/ArnoChenFx/kura/internal/store"
)

// reloadQueue 明确控制后台任务与 UI 回调的顺序；任务确实在另一 goroutine
// 执行，App 字段仍只由测试主线程读写，因而也能用 race 检查发布边界。
type reloadQueue struct {
	work    []func()
	updates chan func()
}

func queueReloads(a *App) *reloadQueue {
	q := &reloadQueue{updates: make(chan func(), 16)}
	a.spawn = func(fn func()) { q.work = append(q.work, fn) }
	a.update = func(fn func()) { q.updates <- fn }
	return q
}

func (q *reloadQueue) run(t *testing.T, index int) {
	t.Helper()
	if index >= len(q.work) {
		t.Fatalf("没有第 %d 个后台任务（总数 %d）", index, len(q.work))
	}
	fn := q.work[index]
	q.work = slices.Delete(q.work, index, index+1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("后台任务未返回")
	}
}

func (q *reloadQueue) take(t *testing.T) func() {
	t.Helper()
	select {
	case fn := <-q.updates:
		return fn
	case <-time.After(5 * time.Second):
		t.Fatal("未收到 UI 发布")
		return nil
	}
}

func TestReloadPublishesCurrentRootBeforeCounts(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "current", nil)
	other := makeRoot(t, "other", "one", "two")
	if err := a.lib.AddRoot(other); err != nil {
		t.Fatal(err)
	}
	q := queueReloads(a)
	a.ReloadAsync()
	q.run(t, 0)
	q.take(t)()
	if !slices.Equal(skillNames(a.skills), []string{"current"}) || a.scanning {
		t.Fatalf("当前列表应先显示且结束刷新忙碌：skills=%v scanning=%v", skillNames(a.skills), a.scanning)
	}
	if a.rootCounts[cfg.Root] != 1 {
		t.Fatalf("当前目录计数未发布：%v", a.rootCounts)
	}
	if _, exists := a.rootCounts[other]; exists {
		t.Fatal("尚未执行第二阶段，不应已读取其他目录")
	}
	if len(q.work) != 1 {
		t.Fatalf("应留下一个独立计数任务，得到 %d", len(q.work))
	}
	q.run(t, 0)
	q.take(t)()
	if a.rootCounts[other] != 2 {
		t.Fatalf("第二阶段应更新其他目录计数：%v", a.rootCounts)
	}
}

func TestReloadSwitchRootStartsImmediately(t *testing.T) {
	for _, roundtrip := range []bool{false, true} {
		name := "third-root"
		if roundtrip {
			name = "return-to-first"
		}
		t.Run(name, func(t *testing.T) {
			a, cfg := newTestApp(t)
			seedSkill(t, cfg, "first", nil)
			second := makeRoot(t, "second", "second")
			third := makeRoot(t, "third", "third")
			for _, root := range []string{second, third} {
				if err := a.lib.AddRoot(root); err != nil {
					t.Fatal(err)
				}
			}
			target, want := third, "third"
			if roundtrip {
				target, want = cfg.Root, "first"
			}
			q := queueReloads(a)
			a.ReloadAsync()
			a.SelectRoot(second)
			a.SelectRoot(target)
			a.ReloadAsync()
			if len(q.work) != 3 {
				t.Fatalf("每个新目录立即启动一次扫描；同目录重复刷新应合并，任务数=%d", len(q.work))
			}
			// 第一次扫描先完成，也不能结束最终目录的忙碌或复活旧列表。
			q.run(t, 0)
			q.take(t)()
			if !a.scanning || len(a.skills) != 0 {
				t.Fatalf("旧扫描不能改当前忙碌/列表：scanning=%v skills=%v", a.scanning, skillNames(a.skills))
			}
			// 最终目录越过第二次扫描完成。
			q.run(t, 1)
			q.take(t)()
			if a.scanning || !slices.Equal(skillNames(a.skills), []string{want}) {
				t.Fatalf("最终目录未就绪：scanning=%v skills=%v", a.scanning, skillNames(a.skills))
			}
			status := a.status
			q.run(t, 0)
			q.take(t)()
			if a.scanning || a.status != status || !slices.Equal(skillNames(a.skills), []string{want}) {
				t.Fatalf("迟到扫描覆盖了最终目录：status=%q skills=%v", a.status, skillNames(a.skills))
			}
			// 只有最终目录能启动计数阶段。
			if len(q.work) != 1 {
				t.Fatalf("旧扫描不应再启动计数，任务数=%d", len(q.work))
			}
			q.run(t, 0)
			q.take(t)()
		})
	}
}

func TestReloadCannotOverwriteCompletedMove(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "moving", nil)
	q := queueReloads(a)
	a.ReloadAsync()
	q.run(t, 0)
	oldScan := q.take(t)
	// 扫描结果已固定为 enabled，但移动任务先把更新后的 disabled 列表发布。
	a.ToggleAsync("moving", false)
	q.run(t, 0)
	q.take(t)()
	status := a.status
	oldScan()
	if a.scanning || a.isBusy("moving") {
		t.Fatalf("已完成任务不应继续忙碌：scanning=%v busy=%v", a.scanning, a.busy)
	}
	if len(a.skills) != 1 || a.skills[0].State != store.StateDisabled || a.status != status {
		t.Fatalf("旧扫描覆盖移动结果：skills=%v status=%q", a.skills, a.status)
	}
}

func TestReloadCountsCannotOverwriteNewRoot(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "first", nil)
	other := makeRoot(t, "other", "one", "two")
	if err := a.lib.AddRoot(other); err != nil {
		t.Fatal(err)
	}
	q := queueReloads(a)
	a.ReloadAsync()
	q.run(t, 0)
	q.take(t)()
	q.run(t, 0)
	oldCounts := q.take(t)
	// 旧计数是 2，切换后扫描应显示刚停用一份后的 1。
	if _, err := a.lib.Config(other).Disable("two"); err != nil {
		t.Fatal(err)
	}
	a.SelectRoot(other)
	q.run(t, 0)
	q.take(t)()
	oldCounts()
	if a.rootCounts[other] != 1 {
		t.Fatalf("旧计数覆盖了新目录扫描：%v", a.rootCounts)
	}
	q.run(t, 0)
	q.take(t)()
}

func TestReloadCountsDoNotRestoreRemovedRoot(t *testing.T) {
	a, _ := newTestApp(t)
	other := makeRoot(t, "other", "one")
	if err := a.lib.AddRoot(other); err != nil {
		t.Fatal(err)
	}
	q := queueReloads(a)
	a.ReloadAsync()
	q.run(t, 0)
	q.take(t)()
	q.run(t, 0)
	oldCounts := q.take(t)
	a.RemoveRootAsync(other)
	q.run(t, 0)
	q.take(t)()
	oldCounts()
	if _, exists := a.rootCounts[other]; exists {
		t.Fatalf("迟到计数恢复了已移除目录：%v", a.rootCounts)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("从列表移除不应动目录：%v", err)
	}
}
