package main

import (
	"testing"

	"github.com/ArnoChenFx/kura/internal/ui"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
)

// 探针：分步记录，定位卡点。
func TestProbeMenuAfterUse(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Logf("panic: %v", r)
		}
	}()
	t.Log("step1: Use")
	mygo.Use(native.Plugin)
	t.Log("step2: MenuItem")
	item := updater.MenuItem()
	t.Logf("step3: label=%q", item.Label)
	m := mygo.NewMenu([]*mygo.MenuItem{
		{Label: "kura", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			updater.MenuItem(),
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
	})
	t.Logf("step4: items=%d", len(m.Items()))
	_ = ui.NewApp(nil)
}
