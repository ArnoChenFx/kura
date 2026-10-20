package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/ArnoChenFx/kura/locales"
	myui "github.com/egoist/mygo/ui"
)

func seedHistory(t *testing.T, cfg *store.Config, name, stamp, body string) {
	t.Helper()
	dir := cfg.SnapshotPath(name, stamp)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotHistoryShowsDiffAndConfirmsRestore(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "alpha", nil)
	seedHistory(t, cfg, "alpha", "20260101-120000", "# historic\n\nold instructions\n")
	tt := showApp(a, 1280, 850)
	click(t, a, tt, "alpha")
	click(t, a, tt, locales.UiSnapshotHistory())
	if a.history == nil || a.history.preview == nil || a.history.diff == nil {
		t.Fatal("历史面板应加载预览和逐行差异")
	}
	if !tt.HasText("+ # historic") {
		t.Errorf("缺少实际内容差异: %v", tt.Texts())
	}
	click(t, a, tt, locales.UiSnapshotRestore())
	if !a.history.confirm {
		t.Fatal("恢复前应确认")
	}
	before, err := os.ReadFile(filepath.Join(cfg.SkillPath("alpha"), "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) == "# historic\n\nold instructions\n" {
		t.Fatal("确认之前不得恢复")
	}
	// 确认弹窗默认按钮是恢复，用 Enter 走真实确认框处理路径。
	tt.Key(0, myui.KeyEnter)
	refresh(a, tt)
	got, err := os.ReadFile(filepath.Join(cfg.SkillPath("alpha"), "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "# historic\n\nold instructions\n" {
		t.Fatal("历史内容没有恢复")
	}
	if a.history.open || a.isBusy("alpha") {
		t.Fatal("完成后面板与busy应清理")
	}
	items, err := cfg.ListSnapshots("alpha")
	if err != nil || len(items) != 2 {
		t.Fatalf("恢复前必须生成新快照: %v %v", items, err)
	}
}

func TestSnapshotHistoryFitsMinimumWindow(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "alpha", nil)
	seedHistory(t, cfg, "alpha", "20260101-120000", "# historical\n")
	tt := showApp(a, 880, 520)
	a.OpenSnapshots(a.skills[0])
	a.settle()
	tt.Frame()
	for _, label := range []string{locales.UiCommonClose(), locales.UiSnapshotRestore()} {
		r, ok := tt.Find(label)
		// Modal 的外部留白和内部 padding 各 20，按钮需留在内容底边 480 内。
		if !ok || r.Y < 40 || r.Y+r.H > 480 || r.X < 0 || r.X+r.W > 880 {
			t.Errorf("最小窗口操作不可见 %q: %+v", label, r)
		}
	}
}

func TestSnapshotHistoryLateRestoreKeepsNewRootPanel(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "alpha", nil)
	seedHistory(t, cfg, "alpha", "20260101-120000", "# old root history\n")
	other := makeRoot(t, "other", "alpha")
	if err := a.lib.AddRoot(other); err != nil {
		t.Fatal(err)
	}
	otherCfg := a.lib.Config(other)
	seedHistory(t, otherCfg, "alpha", "20260201-120000", "# new root history\n")
	tt := showApp(a, 1280, 850)
	a.OpenSnapshots(a.skills[0])
	a.settle()
	tt.Frame()
	var held func()
	a.spawn = func(fn func()) { held = fn }
	a.RestoreSnapshotAsync()
	if held == nil {
		t.Fatal("未发起恢复")
	}
	a.spawn = func(fn func()) { fn() }
	a.SelectRoot(other)
	a.settle()
	a.OpenSnapshots(a.skills[0])
	a.settle()
	tt.Frame()
	newHistory := a.history
	newStatus := a.status
	held()
	a.settle()
	tt.Frame()
	if a.history != newHistory || !newHistory.open || a.status != newStatus {
		t.Fatal("旧恢复覆盖了新根目录面板或状态")
	}
	got, err := os.ReadFile(filepath.Join(cfg.SkillPath("alpha"), "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "# old root history\n" {
		t.Fatal("旧目录写操作应完成")
	}
	newContent, err := os.ReadFile(filepath.Join(otherCfg.SkillPath("alpha"), "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(newContent) == "# old root history\n" {
		t.Fatal("误写新目录")
	}
}

func TestSnapshotHistoryStalePreviewKeepsPanelAndCurrent(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "alpha", nil)
	seedHistory(t, cfg, "alpha", "20260101-120000", "# history\n")
	tt := showApp(a, 1280, 850)
	a.OpenSnapshots(a.skills[0])
	a.settle()
	tt.Frame()
	path := filepath.Join(cfg.SkillPath("alpha"), "SKILL.md")
	if err := os.WriteFile(path, []byte("# edited after preview\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.RestoreSnapshotAsync()
	a.settle()
	tt.Frame()
	if !a.history.open || a.history.err == "" || a.history.busy {
		t.Fatal("失败应保留面板并显示错误")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "# edited after preview\n" {
		t.Fatal("过期预览覆盖了当前编辑")
	}
}

func TestSnapshotInvalidSelectionCancelsLoadingState(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "alpha", nil)
	seedHistory(t, cfg, "alpha", "20260101-120000", "# history\n")
	tt := showApp(a, 1280, 850)
	a.OpenSnapshots(a.skills[0])
	a.settle()
	tt.Frame()
	var held func()
	a.spawn = func(fn func()) { held = fn }
	a.selectSnapshot(0)
	a.history.items = append(a.history.items, store.Snapshot{Stamp: "broken", Err: store.ErrSnapshotInvalid})
	a.selectSnapshot(1)
	if a.history.loading || a.history.err == "" {
		t.Fatal("损坏快照应立即报错而不是保留旧加载状态")
	}
	held()
	a.settle()
	if a.history.preview != nil || a.history.loading {
		t.Fatal("旧预览不应覆盖损坏快照的选择")
	}
}
