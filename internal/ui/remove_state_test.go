package ui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/ArnoChenFx/kura/locales"
)

// 详情和右键菜单都必须把发起移除的状态带到确认框；同名启用副本在确认前后
// 保持不变。右键不改变原选择，确认后也不能把原先的启用详情抢到回收站。
func TestRemoveDisabledCopyThroughEveryEntry(t *testing.T) {
	for _, mode := range []string{store.ViewGrid, store.ViewList} {
		for _, entry := range []string{"detail", "menu"} {
			t.Run(mode+"/"+entry, func(t *testing.T) {
				a, cfg := newTestApp(t)
				a.viewMode = mode
				detSeedWithMD(t, cfg, "duplicate", "# Disabled copy\n")
				if _, err := cfg.Disable("duplicate"); err != nil {
					t.Fatal(err)
				}
				detSeedWithMD(t, cfg, "duplicate", "# Enabled copy\n")
				enabledPath := filepath.Join(cfg.SkillPath("duplicate"), "SKILL.md")
				enabledBefore, err := os.ReadFile(enabledPath)
				if err != nil {
					t.Fatal(err)
				}
				disabledPath := cfg.DisabledPath("duplicate")
				disabledBefore, err := os.ReadFile(filepath.Join(disabledPath, "SKILL.md"))
				if err != nil {
					t.Fatal(err)
				}
				a.filter = filterEnabled
				tt := showApp(a, 1280, 800)
				click(t, a, tt, "duplicate")
				a.filter = filterDisabled
				tt.Frame()
				wantSelection := store.StateEnabled
				if entry == "detail" {
					click(t, a, tt, "duplicate")
					// 卡片隐藏后只从仍指向停用副本的详情操作。
					a.filter = filterEnabled
					tt.Frame()
					click(t, a, tt, locales.UiDetailTrashLabel())
					wantSelection = store.StateTrashed
				} else {
					openMenu(t, a, tt, "duplicate")
					choose(t, a, tt, locales.UiMenuMoveTrash())
				}
				if !a.confirm.open || a.confirm.name != "duplicate" || a.confirm.state != store.StateDisabled {
					t.Fatalf("确认框必须指向停用副本：%+v", a.confirm)
				}
				if _, err := os.Stat(disabledPath); err != nil {
					t.Fatalf("确认前不能移动停用副本：%v", err)
				}
				click(t, a, tt, locales.UiConfirmTrashOK())
				if a.statusErr {
					t.Fatalf("移除停用副本应成功：%s", a.status)
				}
				if got, err := os.ReadFile(enabledPath); err != nil || string(got) != string(enabledBefore) {
					t.Fatalf("不能移动或修改同名启用副本：%q err=%v", got, err)
				}
				if _, err := os.Stat(disabledPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("停用副本应已移走：%v", err)
				}
				entries, err := os.ReadDir(cfg.TrashPath("duplicate"))
				if err != nil || len(entries) != 1 {
					t.Fatalf("应存在一份回收站副本：%v err=%v", entries, err)
				}
				trashedPath := filepath.Join(cfg.TrashPath("duplicate"), entries[0].Name(), "SKILL.md")
				if got, err := os.ReadFile(trashedPath); err != nil || string(got) != string(disabledBefore) {
					t.Fatalf("回收站应保存停用副本的内容：%q err=%v", got, err)
				}
				if got := a.selected(); got == nil || got.State != wantSelection {
					t.Fatalf("移除后选择错误：%+v，想要 %s", got, wantSelection)
				}
			})
		}
	}
}
