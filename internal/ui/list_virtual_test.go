package ui

import (
	"fmt"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/ArnoChenFx/kura/locales"
	myui "github.com/egoist/mygo/ui"
)

// 长列表只构建视口附近的项。Texts 包含已构建但被裁切的文字，所以此断言
// 也能识别把所有行塞进 Scroll 的回归，而不只验证末行被视口裁切。
func TestLocalListBuildsOnlyVisibleRowsAndReachesEnd(t *testing.T) {
	a, cfg := newTestApp(t)
	const total = 200
	names := make(map[string]bool, total)
	for i := range total {
		name := fmt.Sprintf("virtual-skill-%03d", i)
		names[name] = true
		seedDescribed(t, cfg, name, "A local skill in a long library", nil)
	}
	a.viewMode = store.ViewList
	tt := showApp(a, 1360, 640)
	tt.SetPreferences(myui.Preferences{ReduceMotion: true})
	refresh(a, tt)
	built := 0
	for _, text := range tt.Texts() {
		if names[text] {
			built++
		}
	}
	if built == 0 || built >= total/4 {
		t.Fatalf("首帧应只构建少量可见行，构建了 %d/%d 行", built, total)
	}
	last := fmt.Sprintf("virtual-skill-%03d", total-1)
	if _, ok := tt.Find(last); ok {
		t.Fatal("远端行已提前构建")
	}

	a.list.ScrollToEnd()
	tt.Frame()
	first, end := a.list.Visible()
	box, ok := tt.Find(last)
	if !ok || box.H <= 0 || end != total-1 || !a.list.AtEnd() {
		t.Fatalf("原生列表应能到达最后一行：box=%v found=%v visible=%d..%d atEnd=%v", box, ok, first, end, a.list.AtEnd())
	}
	// 末行仍由行按钮处理选择；展开详情后保持所选项及其开关可操作。
	click(t, a, tt, last)
	if a.selName != last || a.selState != store.StateEnabled {
		t.Fatalf("虚拟末行的选择丢失：%q/%s", a.selName, a.selState)
	}
	a.list.ScrollToEnd()
	tt.Frame()
	sw, ok := tt.Find(locales.UiCardSwitchLabel(last))
	if !ok || sw.W < 30 || sw.H <= 0 {
		t.Fatalf("展开详情后末行开关不可见：%v found=%v", sw, ok)
	}
}

// 同名副本共享名称，却必须拥有不同的虚拟行身份。筛选和插入新项时，
// 控件身份跟随技能状态，不能退回列表下标或只用名字。
func TestLocalListKeysDistinguishSameNameStates(t *testing.T) {
	a, cfg := newTestApp(t)
	seedDescribed(t, cfg, "same-name", "Disabled copy", nil)
	if _, err := cfg.Disable("same-name"); err != nil {
		t.Fatal(err)
	}
	seedDescribed(t, cfg, "same-name", "Enabled copy", nil)
	a.viewMode = store.ViewList
	tt := showApp(a, 1280, 800)
	tt.SetPreferences(myui.Preferences{ReduceMotion: true})
	refresh(a, tt)
	rows := a.visible()
	if len(rows) != 2 || a.list.Key == nil {
		t.Fatalf("同名两份技能应进入虚拟列表：rows=%d", len(rows))
	}
	keys := map[store.State]any{}
	for i, s := range rows {
		keys[s.State] = a.list.Key(i)
	}
	if keys[store.StateEnabled] == keys[store.StateDisabled] {
		t.Fatal("同名启用和停用副本共用了行身份")
	}
	a.filter = filterDisabled
	tt.Frame()
	if key := a.list.Key(0); key != keys[store.StateDisabled] {
		t.Fatalf("筛选后停用副本身份改变：before=%v after=%v", keys[store.StateDisabled], key)
	}
}
