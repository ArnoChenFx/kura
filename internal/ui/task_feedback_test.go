package ui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/ArnoChenFx/kura/locales"
)

// 已排队但尚未发布的旧结果不能清掉重新发起的检查，也不能打开旧评审。
func TestCancelCheckRejectsQueuedResult(t *testing.T) {
	a, cfg := newTestApp(t)
	seedWithLocalSource(t, cfg, "one", "old\n", "new\n")
	a.CheckAsync("one")
	a.CancelCheck()
	cancelled := a.status
	if a.checking != "" || a.checkingAll || a.checkCancel != nil || a.statusErr {
		t.Fatalf("cancel did not release check state: %q %v", a.checking, a.checkingAll)
	}
	a.settle()
	if a.review.open || a.status != cancelled {
		t.Fatalf("cancelled result overwrote UI: review=%v status=%q", a.review.open, a.status)
	}

	a.CheckAsync("one")
	a.CancelCheck()
	var jobs []func()
	a.spawn = func(fn func()) { jobs = append(jobs, fn) }
	a.CheckAllAsync()
	a.settle()
	if !a.checkingAll || a.review.open || a.checkCancel == nil {
		t.Fatal("old single-check result cleared the newer batch check")
	}
	jobs[0]()
	a.settle()
	if a.checkingAll || !a.review.open || !a.review.all || a.checkCancel != nil {
		t.Fatal("new batch check did not finish normally")
	}
}

// 切根目录取消旧 context，立即允许新检查；其后的旧回调保持静默。
func TestSelectRootCancelsCheck(t *testing.T) {
	a, cfg := newTestApp(t)
	seedWithLocalSource(t, cfg, "one", "old\n", "new\n")
	other := makeRoot(t, "extra", "two")
	if err := a.lib.AddRoot(other); err != nil {
		t.Fatal(err)
	}
	a.CheckAllAsync()
	cancelled := false
	cancel := a.checkCancel
	a.checkCancel = func() { cancelled = true; cancel() }
	var jobs []func()
	a.spawn = func(fn func()) { jobs = append(jobs, fn) }
	a.SelectRoot(other)
	if !cancelled || a.hasCheckTarget() || a.checkCancel != nil {
		t.Fatal("root switch did not cancel and release old check")
	}
	a.settle()
	if a.review.open || a.status != "" {
		t.Fatalf("old result affected new root: review=%v status=%q", a.review.open, a.status)
	}
}

// beginCheck 的 context 真正被取消，后续任务使用独立的新 context。
func TestCancelCheckCancelsContext(t *testing.T) {
	a, _ := newTestApp(t)
	ctx, _ := a.beginCheck()
	a.checking = "one"
	a.CancelCheck()
	if ctx.Err() != context.Canceled {
		t.Fatalf("context error = %v", ctx.Err())
	}
	next, _ := a.beginCheck()
	defer a.cancelCheck()
	if next.Err() != nil {
		t.Fatalf("new check inherited cancellation: %v", next.Err())
	}
}

func TestCheckAllReportsScanFailure(t *testing.T) {
	a, cfg := newTestApp(t)
	if err := os.WriteFile(cfg.Root, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.CheckAllAsync()
	a.settle()
	if !a.statusErr || a.review.open || a.checkingAll || a.checkCancel != nil {
		t.Fatalf("scan failure looked like a completed check: status=%q review=%v", a.status, a.review.open)
	}
}

// 阶段回调只发布到主线程；完成或切目录后迟到的阶段不复活忙碌状态。
func TestInstallProgressRejectsStaleUpdates(t *testing.T) {
	a, _ := newTestApp(t)
	first, progress := a.beginInstallTask(store.InstallDownload)
	progress(store.InstallInspect)
	if a.install.phase != store.InstallDownload {
		t.Fatal("worker progress changed UI before publish")
	}
	a.settle()
	if a.install.phase != store.InstallInspect || a.status != locales.UiInstallPhaseInspect() {
		t.Fatal("published phase was not shown")
	}
	progress(store.InstallWrite)
	if !a.finishInstallTask(first) {
		t.Fatal("current installation did not finish")
	}
	second, nextProgress := a.beginInstallTask(store.InstallDownload)
	a.settle()
	if a.install.task != second || a.install.phase != store.InstallDownload {
		t.Fatal("old phase replaced the new installation")
	}
	a.gen++
	a.status = "new root"
	nextProgress(store.InstallWrite)
	a.settle()
	if a.status != "new root" {
		t.Fatal("old root progress overwrote current status")
	}
	if a.finishInstallTask(second) || a.install.busy || a.install.task != nil {
		t.Fatal("old root completion must clean busy without applying its result")
	}
}

// 根目录改变不能取消已经开始的写入，完成后只释放旧安装的 busy。
func TestInstallCompletesInOriginalRootAfterSwitch(t *testing.T) {
	a, cfg := newTestApp(t)
	src := makeRoot(t, "source", "installed")
	other := makeRoot(t, "extra", "existing")
	if err := a.lib.AddRoot(other); err != nil {
		t.Fatal(err)
	}
	var jobs []func()
	a.spawn = func(fn func()) { jobs = append(jobs, fn) }
	startInstall(a, &store.AddOptions{LocalPath: filepath.Join(src, "installed")})
	a.SelectRoot(other)
	a.status = "new root"
	jobs[0]()
	a.settle()
	if _, err := os.Stat(filepath.Join(cfg.SkillPath("installed"), "SKILL.md")); err != nil {
		t.Fatalf("installation did not finish in original root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(other, "installed")); !os.IsNotExist(err) {
		t.Fatalf("installation affected new root: %v", err)
	}
	if a.install.busy || a.status != "new root" {
		t.Fatalf("old completion affected new root: busy=%v status=%q", a.install.busy, a.status)
	}
}

// 更新写入结束后，旧根目录的回调不能关闭用户在新根目录打开的评审。
func TestApplyCompletionDoesNotCloseNewRootReview(t *testing.T) {
	a, cfg := newTestApp(t)
	seedWithLocalSource(t, cfg, "one", "old\n", "new\n")
	a.CheckAsync("one")
	a.settle()
	other := makeRoot(t, "extra", "two")
	if err := a.lib.AddRoot(other); err != nil {
		t.Fatal(err)
	}
	var jobs []func()
	a.spawn = func(fn func()) { jobs = append(jobs, fn) }
	a.ApplyAsync()
	a.SelectRoot(other)
	a.review = reviewState{open: true, skill: "two", cfg: a.cfg}
	a.status = "new root"
	jobs[0]()
	a.settle()
	if !a.review.open || a.review.skill != "two" || a.status != "new root" {
		t.Fatalf("old apply completion replaced new review: %+v", a.review)
	}
	content, err := os.ReadFile(filepath.Join(cfg.SkillPath("one"), "SKILL.md"))
	if err != nil || string(content) != "new\n" {
		t.Fatalf("original update did not finish: %q (%v)", content, err)
	}
}
