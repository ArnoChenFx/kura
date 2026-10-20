package ui

// 批量更新使用临时目录内的本地来源；通过真实文件变化验证勾选、部分失败和重试。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
)

func batchReviewEntry(t *testing.T, a *App, name string) *skillReviewEntry {
	t.Helper()
	for i := range a.review.entries {
		if a.review.entries[i].skill == name {
			return &a.review.entries[i]
		}
	}
	t.Fatalf("找不到评审条目 %q", name)
	return nil
}

func assertBatchSkillContent(t *testing.T, cfg *store.Config, name, want string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(cfg.SkillPath(name), "SKILL.md"))
	if err != nil || string(got) != want {
		t.Fatalf("%s 内容 = %q，错误 %v；预期 %q", name, got, err, want)
	}
}

func TestBatchReviewRetainsResultsAndRetriesOnlyFailures(t *testing.T) {
	a, cfg := newTestApp(t)
	okSource := seedWithLocalSource(t, cfg, "ok", "old ok\n", "new ok\n")
	failSource := seedWithLocalSource(t, cfg, "failed", "old failed\n", "new failed\n")
	seedWithLocalSource(t, cfg, "waiting", "old waiting\n", "new waiting\n")
	a.CheckAllAsync()
	a.settle()
	batchReviewEntry(t, a, "ok").selected = true
	batchReviewEntry(t, a, "failed").selected = true
	// 检查成功以后来源消失，应用时才会失败。
	parked := failSource + "-parked"
	if err := os.Rename(failSource, parked); err != nil {
		t.Fatal(err)
	}
	a.ApplyAsync()
	a.settle()
	if !a.review.open || a.review.applyBusy {
		t.Fatal("批量结果应留在面板中，且释放忙碌状态")
	}
	ok := batchReviewEntry(t, a, "ok")
	failed := batchReviewEntry(t, a, "failed")
	if !ok.applied || ok.selected || ok.applyErr != "" || failed.applied || failed.applyErr == "" || !failed.selected {
		t.Fatalf("逐项结果未正确保留: ok=%+v failed=%+v", ok, failed)
	}
	if a.review.failedUpdateCount() != 1 || a.updates["ok"] || !a.updates["failed"] {
		t.Fatal("失败计数或更新标记不正确")
	}
	assertBatchSkillContent(t, cfg, "ok", "new ok\n")
	assertBatchSkillContent(t, cfg, "failed", "old failed\n")
	assertBatchSkillContent(t, cfg, "waiting", "old waiting\n")

	if err := os.Rename(parked, failSource); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(okSource, "SKILL.md"), []byte("must not apply twice\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 即使用户另外勾选了一个未执行条目，“重试失败”也只处理失败项。
	batchReviewEntry(t, a, "waiting").selected = true
	ok.selected = true // 成功状态本身也必须阻止再次执行旧预览。
	a.RetryFailedUpdates()
	a.settle()
	if a.review.failedUpdateCount() != 0 || !failed.applied || failed.applyErr != "" || !a.review.open {
		t.Fatalf("重试后的结果不正确: %+v", failed)
	}
	assertBatchSkillContent(t, cfg, "ok", "new ok\n")
	assertBatchSkillContent(t, cfg, "failed", "new failed\n")
	assertBatchSkillContent(t, cfg, "waiting", "old waiting\n")
	if a.review.selectedUpdateCount() != 1 {
		t.Fatal("只应剩下尚未执行的勾选条目")
	}
	a.ApplyAsync()
	a.settle()
	assertBatchSkillContent(t, cfg, "waiting", "new waiting\n")
	assertBatchSkillContent(t, cfg, "ok", "new ok\n")
}

func TestBatchReviewDefaultSelectionProtectsLocalChanges(t *testing.T) {
	a, cfg := newTestApp(t)
	seedWithLocalSource(t, cfg, "clean", "installed\n", "upstream\n")
	seedWithLocalSource(t, cfg, "modified", "hand edited\n", "upstream\n")
	// 记录 clean 的真实安装哈希；modified 保留夹具占位哈希，代表本地改动。
	hash, err := store.HashDir(cfg.SkillPath("clean"))
	if err != nil {
		t.Fatal(err)
	}
	metaPath := filepath.Join(cfg.SkillPath("clean"), store.MetaFileName)
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	var meta store.Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatal(err)
	}
	meta.ContentHash = hash
	data, err = json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metaPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	a.CheckAllAsync()
	a.settle()
	clean := batchReviewEntry(t, a, "clean")
	modified := batchReviewEntry(t, a, "modified")
	if !clean.selected || clean.preview.LocalModified || modified.selected || !modified.preview.LocalModified {
		t.Fatalf("默认勾选应跳过本地修改: clean=%+v modified=%+v", clean, modified)
	}
	a.ApplyAsync()
	a.settle()
	assertBatchSkillContent(t, cfg, "clean", "upstream\n")
	assertBatchSkillContent(t, cfg, "modified", "hand edited\n")
}

func TestBatchReviewNoSelectionAndRepeatedClickDoNotStartWork(t *testing.T) {
	a, cfg := newTestApp(t)
	seedWithLocalSource(t, cfg, "one", "old\n", "new\n")
	a.CheckAllAsync()
	a.settle()
	var jobs []func()
	a.spawn = func(fn func()) { jobs = append(jobs, fn) }
	a.ApplyAsync()
	a.RetryFailedUpdates()
	if len(jobs) != 0 || a.review.applyBusy {
		t.Fatal("无勾选或无失败条目时不应开始任务")
	}
	batchReviewEntry(t, a, "one").selected = true
	a.ApplyAsync()
	a.ApplyAsync()
	a.RetryFailedUpdates()
	if len(jobs) != 1 || !a.review.applyBusy {
		t.Fatal("应用期间重复操作不应开启另一任务")
	}
	jobs[0]()
	a.settle()
	if a.review.applyBusy || !batchReviewEntry(t, a, "one").applied {
		t.Fatal("应用未完成")
	}
}

func TestBatchReviewCompletionDoesNotReplaceAnotherRootReview(t *testing.T) {
	a, cfg := newTestApp(t)
	seedWithLocalSource(t, cfg, "one", "old\n", "new\n")
	a.CheckAllAsync()
	a.settle()
	batchReviewEntry(t, a, "one").selected = true
	other := makeRoot(t, "other", "two")
	if err := a.lib.AddRoot(other); err != nil {
		t.Fatal(err)
	}
	var jobs []func()
	a.spawn = func(fn func()) { jobs = append(jobs, fn) }
	a.ApplyAsync()
	a.SelectRoot(other)
	a.review = reviewState{open: true, all: true, cfg: a.cfg,
		entries: []skillReviewEntry{{skill: "two", applyErr: "preserved"}}}
	a.status = "new root status"
	jobs[0]()
	a.settle()
	if !a.review.open || len(a.review.entries) != 1 || a.review.entries[0].skill != "two" ||
		a.review.entries[0].applyErr != "preserved" || a.status != "new root status" {
		t.Fatalf("旧任务覆盖了新目录评审: %+v", a.review)
	}
	if a.busy[cfg.Root+"\x00one"] {
		t.Fatal("切换目录后也应释放原目录的写入标记")
	}
	assertBatchSkillContent(t, cfg, "one", "new\n")
}

func TestBatchReviewBusyTargetPreventsEntireBatchAndReservesWrites(t *testing.T) {
	a, cfg := newTestApp(t)
	seedWithLocalSource(t, cfg, "one", "old one\n", "new one\n")
	seedWithLocalSource(t, cfg, "two", "old two\n", "new two\n")
	a.CheckAllAsync()
	a.settle()
	for i := range a.review.entries {
		a.review.entries[i].selected = true
	}
	var jobs []func()
	a.spawn = func(fn func()) { jobs = append(jobs, fn) }
	a.busy[a.busyKey("two")] = true
	a.ApplyAsync()
	if len(jobs) != 0 || a.review.applyBusy || a.isBusy("one") || a.status == "" {
		t.Fatal("任一条目忙碌时应整批不启动，不留下部分登记")
	}
	delete(a.busy, a.busyKey("two"))
	a.ApplyAsync()
	if len(jobs) != 1 || !a.isBusy("one") || !a.isBusy("two") {
		t.Fatal("应用开始必须登记每个目标的写入")
	}
	a.ToggleAsync("one", false)
	a.RemoveAsync("two")
	a.CheckAsync("one")
	a.CheckAllAsync()
	if len(jobs) != 1 || a.checkingAll || a.checking != "" {
		t.Fatal("应用期间不能再移动同名技能或发起当前目录检查")
	}
	jobs[0]()
	a.settle()
	if a.isBusy("one") || a.isBusy("two") {
		t.Fatal("应用完成后应释放目标写入标记")
	}
	assertBatchSkillContent(t, cfg, "one", "new one\n")
	assertBatchSkillContent(t, cfg, "two", "new two\n")
}

func TestBatchReviewSnapshotRestoreBlocksChecksAndOldReviewApply(t *testing.T) {
	a, cfg := newTestApp(t)
	seedWithLocalSource(t, cfg, "one", "installed\n", "upstream\n")
	seedHistory(t, cfg, "one", "20260101-120000", "historical\n")
	a.CheckAllAsync()
	a.settle()
	batchReviewEntry(t, a, "one").selected = true
	skills, err := cfg.ScanAll()
	if err != nil {
		t.Fatal(err)
	}
	a.OpenSnapshots(skills[0])
	a.settle()
	if a.history == nil || a.history.preview == nil {
		t.Fatal("恢复预览未准备好")
	}
	var jobs []func()
	a.spawn = func(fn func()) { jobs = append(jobs, fn) }
	a.RestoreSnapshotAsync()
	if len(jobs) != 1 || !a.isBusy("one") {
		t.Fatal("未启动历史恢复")
	}
	// 关闭面板不终止后台恢复，动作级守卫仍必须生效。
	a.history.open = false
	a.CheckAsync("one")
	a.CheckAllAsync()
	a.ApplyAsync()
	if len(jobs) != 1 || a.checkingAll || a.checking != "" || a.review.applyBusy {
		t.Fatal("历史恢复期间检查/应用绕过了写入互斥")
	}
	jobs[0]()
	a.settle()
	if a.isBusy("one") {
		t.Fatal("历史恢复后未释放写入标记")
	}
	assertBatchSkillContent(t, cfg, "one", "historical\n")
}
