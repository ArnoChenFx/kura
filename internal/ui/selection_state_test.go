package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/ArnoChenFx/kura/locales"
)

// 同名新技能已经装回根目录时，回收站里的旧副本仍能被独立选中；切换筛选
// 不抢走详情，恢复冲突也不能变成对新技能的停用或移除操作。
func TestSameNameTrashSelectionKeepsItsDetails(t *testing.T) {
	for _, mode := range []string{store.ViewGrid, store.ViewList} {
		t.Run(mode, func(t *testing.T) {
			a, cfg := newTestApp(t)
			a.viewMode = mode
			seedSkill(t, cfg, "duplicate", nil)
			if _, err := cfg.RemoveWithStamp("duplicate", "older"); err != nil {
				t.Fatal(err)
			}
			seedSkill(t, cfg, "duplicate", nil)
			activePath := filepath.Join(cfg.SkillPath("duplicate"), "SKILL.md")
			activeBefore, err := os.ReadFile(activePath)
			if err != nil {
				t.Fatal(err)
			}

			tt := showApp(a, 1280, 800)
			click(t, a, tt, "duplicate")
			if got := a.selected(); got == nil || got.State != store.StateEnabled {
				t.Fatalf("首次点击应选中启用副本，得到 %#v", got)
			}
			detClickTab(t, a, tt, locales.UiDetailTabFull())
			a.filter = filterTrashed
			tt.Frame()
			if got := a.selected(); got == nil || got.State != store.StateEnabled || a.selIdx != -1 {
				t.Fatalf("筛掉启用副本后详情应保留它：selected=%#v index=%d", got, a.selIdx)
			}

			click(t, a, tt, "duplicate")
			wantDir := filepath.Join(cfg.TrashRoot(), "duplicate")
			if got := a.selected(); got == nil || got.State != store.StateTrashed || got.Dir != wantDir {
				t.Fatalf("回收站行应选中旧副本，得到 %#v，想要 %s", got, wantDir)
			}
			if a.detailTab != 0 {
				t.Fatal("切到同名旧副本时应重置为概览 tab")
			}
			if !tt.HasText(wantDir) || !tt.HasText(locales.UiActionPurge()) {
				t.Fatalf("详情应显示回收站路径与永久删除入口，文本 %v", tt.Texts())
			}
			if _, ok := tt.Find(locales.UiDetailCheckLabel()); ok {
				t.Fatal("回收站详情不应显示启用技能的检查更新操作")
			}
			detClickTab(t, a, tt, locales.UiDetailTabFull())
			if !tt.HasText(locales.UiDetailNoSkillMD()) {
				t.Fatalf("回收站全文不能复用启用副本的正文，文本 %v", tt.Texts())
			}

			// 隐藏回收站卡片后从仍保留的详情操作，可明确验证点击的是详情按钮。
			a.filter = filterEnabled
			tt.Frame()
			if got := a.selected(); got == nil || got.State != store.StateTrashed || a.selIdx != -1 {
				t.Fatalf("筛掉回收站副本后详情应保留它：selected=%#v index=%d", got, a.selIdx)
			}
			click(t, a, tt, locales.UiMenuRestore())
			if !a.statusErr {
				t.Fatal("启用目录有同名技能时恢复应报告冲突")
			}
			activeAfter, err := os.ReadFile(activePath)
			if err != nil || string(activeAfter) != string(activeBefore) {
				t.Fatalf("恢复冲突不应移动或修改启用副本：%v", err)
			}
			if got := a.selected(); got == nil || got.State != store.StateTrashed {
				t.Fatalf("恢复失败后详情应仍指向回收站副本，得到 %#v", got)
			}
		})
	}
}

// 停用后重新安装同名技能也是合法状态；全部视图中的两行必须有独立控件
// 身份，筛选后点击停用副本不能显示启用副本的路径或同时高亮两个副本。
func TestSameNameEnabledAndDisabledHaveIndependentSelection(t *testing.T) {
	for _, mode := range []string{store.ViewGrid, store.ViewList} {
		t.Run(mode, func(t *testing.T) {
			a, cfg := newTestApp(t)
			a.viewMode = mode
			seedSkill(t, cfg, "duplicate", nil)
			if _, err := cfg.Disable("duplicate"); err != nil {
				t.Fatal(err)
			}
			seedSkill(t, cfg, "duplicate", nil)
			tt := showApp(a, 1280, 800)
			a.filter = filterDisabled
			tt.Frame()
			click(t, a, tt, "duplicate")
			if got := a.selected(); got == nil || got.State != store.StateDisabled || got.Dir != cfg.DisabledPath("duplicate") {
				t.Fatalf("应选中停用副本，得到 %#v", got)
			}
			a.filter = filterAll
			tt.Frame()
			selected := 0
			for _, s := range a.visible() {
				if a.isSelected(s) {
					selected++
				}
			}
			if selected != 1 || a.selIdx != 1 {
				t.Fatalf("全部视图应只高亮停用副本：selected=%d index=%d", selected, a.selIdx)
			}
		})
	}
}

// 增加状态身份后，普通启用/停用/移除/恢复仍应让详情跟随被移动的技能，
// 不能因原状态消失而丢失选择。
func TestSelectionFollowsSkillAcrossStateMoves(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "moving", nil)
	tt := showApp(a, 1280, 800)
	click(t, a, tt, "moving")
	for _, step := range []struct {
		state store.State
		move  func()
	}{
		{store.StateDisabled, func() { a.ToggleAsync("moving", false) }},
		{store.StateEnabled, func() { a.ToggleAsync("moving", true) }},
		{store.StateTrashed, func() { a.RemoveAsync("moving") }},
		{store.StateEnabled, func() { a.RestoreAsync("moving") }},
	} {
		step.move()
		refresh(a, tt)
		if got := a.selected(); got == nil || got.Name != "moving" || got.State != step.state {
			t.Fatalf("移动后详情应跟随到 %s，得到 %#v", step.state, got)
		}
	}
}

// 移除启用副本后，即使扫描先列出同名停用副本，详情也应跟随移入回收站的
// 那一份，不能由扫描顺序决定选中哪份同名技能。
func TestRemoveSelectionFollowsEnabledCopyWhileDisabledRemains(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "duplicate", nil)
	if _, err := cfg.Disable("duplicate"); err != nil {
		t.Fatal(err)
	}
	seedSkill(t, cfg, "duplicate", nil)
	a.filter = filterEnabled
	tt := showApp(a, 1280, 800)
	click(t, a, tt, "duplicate")
	a.RemoveAsync("duplicate")
	refresh(a, tt)
	if a.statusErr {
		t.Fatalf("移除启用副本应成功：%s", a.status)
	}
	if got := a.selected(); got == nil || got.State != store.StateTrashed {
		t.Fatalf("详情应跟随启用副本进入回收站，得到 %#v", got)
	}
	if _, err := os.Stat(cfg.DisabledPath("duplicate")); err != nil {
		t.Fatalf("同名停用副本应保留：%v", err)
	}
}

// 并发重扫可能先发布已经移动后的列表，临时让详情按同名回退。它不是用户
// 改选；真正的移动完成回调随后到达时，仍应跟随被移动副本的目标状态。
func TestMoveSelectionFollowsAfterReloadPublishesFirst(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "duplicate", nil)
	if _, err := cfg.Disable("duplicate"); err != nil {
		t.Fatal(err)
	}
	seedSkill(t, cfg, "duplicate", nil)
	a.filter = filterDisabled
	tt := showApp(a, 1280, 800)
	click(t, a, tt, "duplicate")

	a.RemoveSkillAsync("duplicate", store.StateDisabled)
	completion := a.pending
	a.pending = nil
	a.ReloadAsync()
	refresh(a, tt)
	if got := a.selected(); got == nil || got.State != store.StateEnabled {
		t.Fatalf("重扫先发布时应已经看见移除后的列表，得到 %#v", got)
	}
	a.pending = append(a.pending, completion...)
	refresh(a, tt)
	if a.statusErr {
		t.Fatalf("移除停用副本应成功：%s", a.status)
	}
	if got := a.selected(); got == nil || got.State != store.StateTrashed {
		t.Fatalf("完成回调应把详情跟随到被移动的副本，得到 %#v", got)
	}
}

// 同名回收站可以保留多次移除记录；恢复最新记录后，详情必须跟随到恢复的
// 启用副本，不能因为旧记录仍然存在而停留在回收站。
func TestRestoreSelectionFollowsLatestWhileOlderTrashRemains(t *testing.T) {
	for _, mode := range []string{store.ViewGrid, store.ViewList} {
		t.Run(mode, func(t *testing.T) {
			a, cfg := newTestApp(t)
			seedSelectionTrashHistory(t, cfg)
			a.viewMode, a.filter = mode, filterTrashed
			tt := showApp(a, 1280, 800)
			click(t, a, tt, "duplicate")

			a.RestoreAsync("duplicate")
			refresh(a, tt)
			if a.statusErr {
				t.Fatalf("恢复应成功：%s", a.status)
			}
			if got := a.selected(); got == nil || got.State != store.StateEnabled || got.Dir != cfg.SkillPath("duplicate") {
				t.Fatalf("恢复后详情应跟随启用副本，得到 %#v", got)
			}
			if a.selIdx != -1 || !tt.HasText(cfg.SkillPath("duplicate")) {
				t.Fatalf("回收站筛选应隐藏恢复副本但保留详情：index=%d 文本 %v", a.selIdx, tt.Texts())
			}
			body, err := os.ReadFile(filepath.Join(cfg.SkillPath("duplicate"), "SKILL.md"))
			if err != nil || !strings.Contains(string(body), "# Latest document") {
				t.Fatalf("应恢复最新正文：body=%q err=%v", body, err)
			}
			if _, err := os.Stat(filepath.Join(cfg.TrashRoot(), "duplicate", "20260101-000000", "SKILL.md")); err != nil {
				t.Fatalf("恢复后旧回收站记录应保留：%v", err)
			}
			// 新详情出现后的额外帧不能被仍存在的同名回收站行抢回。
			tt.Frame()
			if got := a.selected(); got == nil || got.State != store.StateEnabled {
				t.Fatalf("额外帧应保留恢复后的详情，得到 %#v", got)
			}
		})
	}
}

// 延迟真正的后台工作，让用户先切换或关闭详情。即使后来又选回同一个
// 回收站条目，旧操作也不能越过这次用户选择，抢去恢复后的启用副本。
func TestRestoreKeepsSelectionChangedDuringWork(t *testing.T) {
	for _, mode := range []string{store.ViewGrid, store.ViewList} {
		for _, action := range []string{"other-skill", "same-name-copy", "close", "select-back", "close-and-select-back", "other-root", "other-root-and-back"} {
			t.Run(mode+"/"+action, func(t *testing.T) {
				a, cfg := newTestApp(t)
				seedSelectionTrashHistory(t, cfg)
				seedSkill(t, cfg, "duplicate", nil)
				if _, err := cfg.Disable("duplicate"); err != nil {
					t.Fatal(err)
				}
				seedSkill(t, cfg, "other", nil)
				otherRoot := makeRoot(t, "extra", "duplicate")
				if err := a.lib.AddRoot(otherRoot); err != nil {
					t.Fatal(err)
				}
				otherCfg := a.lib.Config(otherRoot)
				if _, err := otherCfg.Disable("duplicate"); err != nil {
					t.Fatal(err)
				}
				a.viewMode, a.filter = mode, filterTrashed
				tt := showApp(a, 1280, 800)
				click(t, a, tt, "duplicate")

				var work func()
				spawn := a.spawn
				a.spawn = func(fn func()) { work = fn }
				a.RestoreAsync("duplicate")
				a.spawn = spawn
				if work == nil {
					t.Fatal("恢复应排入后台工作")
				}
				selectFrom := func(filter int, name string) {
					a.filter = filter
					tt.Frame()
					click(t, a, tt, name)
				}
				wantName, wantState, wantRoot := "duplicate", store.StateTrashed, cfg.Root
				switch action {
				case "other-skill":
					selectFrom(filterEnabled, "other")
					wantName, wantState = "other", store.StateEnabled
				case "same-name-copy":
					selectFrom(filterDisabled, "duplicate")
					wantState = store.StateDisabled
				case "close":
					click(t, a, tt, locales.UiActionCloseDetail())
					wantName, wantState = "", ""
				case "select-back":
					selectFrom(filterEnabled, "other")
					selectFrom(filterTrashed, "duplicate")
				case "close-and-select-back":
					click(t, a, tt, locales.UiActionCloseDetail())
					click(t, a, tt, "duplicate")
				case "other-root":
					click(t, a, tt, rootLabel(otherRoot))
					selectFrom(filterDisabled, "duplicate")
					wantState, wantRoot = store.StateDisabled, otherRoot
				case "other-root-and-back":
					click(t, a, tt, rootLabel(otherRoot))
					click(t, a, tt, rootLabel(cfg.Root))
					selectFrom(filterTrashed, "duplicate")
				}

				work()
				refresh(a, tt)
				tt.Frame()
				if a.statusErr {
					t.Fatalf("原目录的恢复应成功：%s", a.status)
				}
				if a.cfg.Root != wantRoot || a.selName != wantName || a.selState != wantState {
					t.Fatalf("恢复不应覆盖期间选择：root=%q name=%q state=%s，想要 %q / %q / %s", a.cfg.Root, a.selName, a.selState, wantRoot, wantName, wantState)
				}
				if got := a.selected(); wantName == "" && got != nil || wantName != "" && (got == nil || got.Name != wantName || got.State != wantState) {
					t.Fatalf("详情应保留用户的新选择，得到 %#v", got)
				}
				if _, err := os.Stat(cfg.SkillPath("duplicate")); err != nil {
					t.Fatalf("原目录里的最新记录仍应完成恢复：%v", err)
				}
			})
		}
	}
}

// seedSelectionTrashHistory 准备两份正文不同的回收站记录，名字相同而时间可排序。
func seedSelectionTrashHistory(t *testing.T, cfg *store.Config) {
	t.Helper()
	for _, version := range []struct{ stamp, body string }{
		{"20260101-000000", "# Older document\n"},
		{"20260102-000000", "# Latest document\n"},
	} {
		detSeedWithMD(t, cfg, "duplicate", version.body)
		if _, err := cfg.RemoveWithStamp("duplicate", version.stamp); err != nil {
			t.Fatal(err)
		}
	}
}

// 安装完成后应选中新副本，即使安装前详情正停在同名的停用/回收站副本。
func TestInstallSelectsNewCopyWhileOldCopyRemains(t *testing.T) {
	for _, oldState := range []store.State{store.StateDisabled, store.StateTrashed} {
		t.Run(string(oldState), func(t *testing.T) {
			a, cfg := newTestApp(t)
			seedSkill(t, cfg, "duplicate", nil)
			var err error
			if oldState == store.StateDisabled {
				_, err = cfg.Disable("duplicate")
				a.filter = filterDisabled
			} else {
				_, err = cfg.RemoveWithStamp("duplicate", "older")
				a.filter = filterTrashed
			}
			if err != nil {
				t.Fatal(err)
			}
			tt := showApp(a, 1280, 800)
			click(t, a, tt, "duplicate")
			if got := a.selected(); got == nil || got.State != oldState {
				t.Fatalf("安装前应选中旧副本，得到 %#v", got)
			}
			source := filepath.Join(makeRoot(t, "source", "duplicate"), "duplicate")
			startInstall(a, &store.AddOptions{LocalPath: source})
			refresh(a, tt)
			if a.statusErr {
				t.Fatalf("重新安装应成功：%s", a.status)
			}
			if got := a.selected(); got == nil || got.State != store.StateEnabled || got.Dir != cfg.SkillPath("duplicate") {
				t.Fatalf("安装后应选中新副本，得到 %#v", got)
			}
			foundOld := false
			for _, s := range a.skills {
				foundOld = foundOld || s.Name == "duplicate" && s.State == oldState
			}
			if !foundOld {
				t.Fatal("重新安装不能移除旧副本")
			}
		})
	}
}

// 同名的启用/停用副本可以有不同正文；切换详情不能复用另一副本的缓存。
func TestSameNameDetailsKeepSeparateFullText(t *testing.T) {
	a, cfg := newTestApp(t)
	detSeedWithMD(t, cfg, "duplicate", "# Disabled document\n")
	if _, err := cfg.Disable("duplicate"); err != nil {
		t.Fatal(err)
	}
	detSeedWithMD(t, cfg, "duplicate", "# Enabled document\n")
	a.filter = filterEnabled
	tt := showApp(a, 1280, 800)
	for _, step := range []struct {
		filter int
		text   string
		absent string
	}{
		{filterEnabled, "Enabled document", "Disabled document"},
		{filterDisabled, "Disabled document", "Enabled document"},
		{filterEnabled, "Enabled document", "Disabled document"},
	} {
		a.filter = step.filter
		tt.Frame()
		click(t, a, tt, "duplicate")
		detClickTab(t, a, tt, locales.UiDetailTabFull())
		if !tt.HasText(step.text) || tt.HasText(step.absent) {
			t.Fatalf("全文应显示 %q 而非 %q，文本 %v", step.text, step.absent, tt.Texts())
		}
	}
}
