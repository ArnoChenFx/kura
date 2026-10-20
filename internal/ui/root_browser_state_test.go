package ui

import (
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
	myui "github.com/egoist/mygo/ui"
)

func TestRootBrowserRestoresSearchFilterAndExactSelection(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "same", nil)
	if _, err := cfg.Disable("same"); err != nil {
		t.Fatal(err)
	}
	seedSkill(t, cfg, "same", nil)
	other := makeRoot(t, "other", "beta")
	if err := a.lib.AddRoot(other); err != nil {
		t.Fatal(err)
	}
	a.ReloadAsync()
	a.flush()
	a.query, a.filter = " SAME ", filterDisabled
	a.setSelection("same", store.StateDisabled)
	a.syncSelection(a.visible())

	a.SelectRoot(other)
	if a.query != "" || a.filter != filterAll || a.selName != "" {
		t.Fatalf("首次进入目录不能继承旧条件：query=%q filter=%d selection=%q", a.query, a.filter, a.selName)
	}
	a.flush()
	a.query, a.filter = "beta", filterEnabled
	a.setSelection("beta", store.StateEnabled)
	a.syncSelection(a.visible())

	a.SelectRoot(cfg.Root)
	// 后台扫描尚未发布的空白帧，不能清掉恢复的技能身份或套用旧点击下标。
	a.syncSelection(a.visible())
	if a.query != " SAME " || a.filter != filterDisabled || a.selName != "same" || a.selState != store.StateDisabled {
		t.Fatalf("扫描前丢失目录上下文：query=%q filter=%d selection=%q/%s", a.query, a.filter, a.selName, a.selState)
	}
	a.flush()
	a.syncSelection(a.visible())
	if s := a.selected(); s == nil || s.State != store.StateDisabled || a.selIdx < 0 {
		t.Fatalf("返回后没有恢复准确副本：selected=%+v index=%d", s, a.selIdx)
	}

	a.SelectRoot(other)
	a.flush()
	if a.query != "beta" || a.filter != filterEnabled || a.selName != "beta" {
		t.Fatalf("第二个目录的上下文被覆盖：query=%q filter=%d selection=%q", a.query, a.filter, a.selName)
	}
}

func TestRootBrowserClearsRemovedCopyAfterSuccessfulScan(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "same", nil)
	if _, err := cfg.Disable("same"); err != nil {
		t.Fatal(err)
	}
	seedSkill(t, cfg, "same", nil)
	other := makeRoot(t, "other", "beta")
	if err := a.lib.AddRoot(other); err != nil {
		t.Fatal(err)
	}
	a.ReloadAsync()
	a.flush()
	a.setSelection("same", store.StateDisabled)
	a.SelectRoot(other)
	a.flush()
	if err := os.RemoveAll(cfg.DisabledPath("same")); err != nil {
		t.Fatal(err)
	}
	a.SelectRoot(cfg.Root)
	if a.selName != "same" {
		t.Fatal("扫描完成前应保留待验证选择")
	}
	a.flush()
	if a.selName != "" || a.selected() != nil {
		t.Fatalf("停用副本已不存在，不应改选同名启用副本：%q/%s", a.selName, a.selState)
	}
}

func TestRootBrowserLateScanCannotClearRestoredSelection(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "alpha", nil)
	other := makeRoot(t, "other", "beta")
	if err := a.lib.AddRoot(other); err != nil {
		t.Fatal(err)
	}
	a.ReloadAsync()
	a.flush()
	a.setSelection("alpha", store.StateEnabled)
	q := queueReloads(a)
	a.SelectRoot(other)
	a.SelectRoot(cfg.Root)
	a.syncSelection(nil)
	q.run(t, 1)
	q.take(t)()
	if a.selName != "alpha" || a.selected() == nil {
		t.Fatalf("目标目录扫描应验证并保留原选择：%q", a.selName)
	}
	q.run(t, 0)
	q.take(t)()
	if a.cfg.Root != cfg.Root || a.selName != "alpha" || a.selected() == nil {
		t.Fatalf("旧扫描不能清掉恢复的选择：root=%q selection=%q", a.cfg.Root, a.selName)
	}
}

func TestRootBrowserKeepsSelectionWhenScanFailsThenRetries(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "alpha", nil)
	other := makeRoot(t, "other", "beta")
	if err := a.lib.AddRoot(other); err != nil {
		t.Fatal(err)
	}
	a.ReloadAsync()
	a.flush()
	a.setSelection("alpha", store.StateEnabled)
	a.SelectRoot(other)
	a.flush()
	parked := cfg.Root + "-parked"
	if err := os.Rename(cfg.Root, parked); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.Root, []byte("暂时不可用的目录"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.SelectRoot(cfg.Root)
	a.flush()
	if !a.statusErr || a.selName != "alpha" {
		t.Fatalf("扫描错误不能当作技能已删除：error=%v selection=%q", a.statusErr, a.selName)
	}
	if err := os.Remove(cfg.Root); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(parked, cfg.Root); err != nil {
		t.Fatal(err)
	}
	a.ReloadAsync()
	a.flush()
	if a.statusErr || a.selected() == nil || a.selName != "alpha" {
		t.Fatalf("目录恢复后应验证并恢复原选择：error=%v selection=%q", a.statusErr, a.selName)
	}
}

func TestRootBrowserKeepsIndependentGridAndListScroll(t *testing.T) {
	for _, mode := range []string{store.ViewGrid, store.ViewList} {
		t.Run(mode, func(t *testing.T) {
			a, cfg := newTestApp(t)
			other := makeRoot(t, "other")
			if err := a.lib.AddRoot(other); err != nil {
				t.Fatal(err)
			}
			otherCfg := a.lib.Config(other)
			for i := 0; i < 60; i++ {
				seedDescribed(t, cfg, fmt.Sprintf("a-%03d", i), "First library", nil)
				seedDescribed(t, otherCfg, fmt.Sprintf("b-%03d", i), "Second library", nil)
			}
			a.viewMode = mode
			tt := showApp(a, 1360, 800)
			tt.SetPreferences(myui.Preferences{ReduceMotion: true})
			refresh(a, tt)
			jump := func(index int) {
				if mode == store.ViewList {
					a.list.ScrollTo(index, myui.Start)
				} else {
					a.grid.ScrollTo(index, myui.Start)
				}
				tt.Frame()
			}
			jump(24)
			before, ok := tt.Find("a-024")
			if !ok || before.H <= 0 {
				t.Fatal("前置条件：第一目录应滚动到第 24 项")
			}
			firstGrid, firstList := a.grid, a.list
			a.SelectRoot(other)
			refresh(a, tt)
			if a.grid == firstGrid || a.list == firstList {
				t.Fatal("不同目录不可复用同一份原生列表状态")
			}
			if first, ok := tt.Find("b-000"); !ok || first.H <= 0 {
				t.Fatalf("新目录应从顶部开始：%v found=%v", first, ok)
			}
			jump(42)
			otherBefore, ok := tt.Find("b-042")
			if !ok || otherBefore.H <= 0 {
				t.Fatal("前置条件：第二目录应滚动到第 42 项")
			}
			for range 2 {
				a.SelectRoot(cfg.Root)
				refresh(a, tt)
				after, ok := tt.Find("a-024")
				if a.grid != firstGrid || a.list != firstList || !ok || math.Abs(float64(after.Y-before.Y)) > 0.5 {
					t.Fatalf("第一目录没有恢复滚动位置：before=%v after=%v found=%v", before, after, ok)
				}
				a.SelectRoot(other)
				refresh(a, tt)
				otherAfter, ok := tt.Find("b-042")
				if !ok || math.Abs(float64(otherAfter.Y-otherBefore.Y)) > 0.5 {
					t.Fatalf("第二目录没有保留独立位置：before=%v after=%v found=%v", otherBefore, otherAfter, ok)
				}
			}
		})
	}
}

func TestRootBrowserForgetsRemovedRoot(t *testing.T) {
	a, _ := newTestApp(t)
	other := makeRoot(t, "other", "beta")
	if err := a.lib.AddRoot(other); err != nil {
		t.Fatal(err)
	}
	a.SelectRoot(other)
	a.flush()
	a.query = "beta"
	a.setSelection("beta", store.StateEnabled)
	a.RemoveRootAsync(other)
	a.flush()
	if _, ok := a.rootBrowsers[other]; ok {
		t.Fatal("移除目录后不应继续保留其浏览状态")
	}
	if err := a.lib.AddRoot(other); err != nil {
		t.Fatal(err)
	}
	a.SelectRoot(other)
	a.flush()
	if a.query != "" || a.selName != "" {
		t.Fatalf("重新添加目录应使用新的会话状态：query=%q selection=%q", a.query, a.selName)
	}
}
