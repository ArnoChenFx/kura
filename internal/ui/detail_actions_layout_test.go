package ui

import (
	"fmt"
	"math"
	"testing"

	"github.com/ArnoChenFx/kura/internal/i18n"
	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/ArnoChenFx/kura/locales"
	myui "github.com/egoist/mygo/ui"
)

// 直接渲染实际详情面板，覆盖侧栏拖拽允许的宽度以及最小窗口高度。
// 守住完整文字、固定底部与有间距的动作分组，而不是只确认按钮存在。
func TestDetailActionsStayAlignedWithinPanel(t *testing.T) {
	defer i18n.Set(i18n.ZH)
	for _, lang := range []i18n.Lang{i18n.ZH, i18n.EN} {
		i18n.Set(lang)
		for _, mode := range []string{store.ThemeLight, store.ThemeDark} {
			for _, width := range []int{260, 320, 560} {
				for _, state := range []store.State{store.StateEnabled, store.StateDisabled, store.StateTrashed} {
					t.Run(fmt.Sprintf("%s/%s/%d/%s", lang, mode, width, state), func(t *testing.T) {
						a, _ := newTestApp(t)
						a.themeMode, a.detailEff = mode, float32(width)
						skill := store.Skill{Name: "layout-skill", State: state}
						naturalWidths := map[string]float32{}
						tt := myui.NewTester(func(c *myui.Context) {
							a.skin = kuraScheme.apply(c, a.themeMode)
							a.pal = a.skin.pal
							for _, label := range []string{
								locales.UiActionCheckUpdate(), locales.UiMenuDisable(), locales.UiSnapshotHistory(),
								locales.UiMenuEnable(), locales.UiMenuRestore(), locales.UiActionPurge(),
							} {
								naturalWidths[label], _ = c.MeasureText(0, myui.Span{Text: label})
							}
							a.viewDetail(c, &skill)
						}, width, 480)
						tt.SetPreferences(myui.Preferences{ReduceMotion: true})
						tt.Frame()

						primary, secondary := locales.UiDetailCheckLabel(), locales.UiSnapshotHistory()
						switch state {
						case store.StateDisabled:
							primary = locales.UiMenuEnable()
						case store.StateTrashed:
							primary, secondary = locales.UiMenuRestore(), locales.UiDetailPurgeLabel()
						}
						top := detailActionRect(t, tt, primary, width)
						bottom := detailActionRect(t, tt, secondary, width)
						if bottom.Y-(top.Y+top.H) < 7.5 {
							t.Errorf("两组动作之间应有明确间距：primary=%v secondary=%v", top, bottom)
						}
						if bottom.Y+bottom.H < 440 {
							t.Errorf("动作区应固定在详情底部：%v", bottom)
						}
						if state == store.StateEnabled {
							disable := detailActionRect(t, tt, locales.UiMenuDisable(), width)
							if math.Abs(float64(top.Y+top.H/2-disable.Y-disable.H/2)) > 0.5 {
								if disable.Y-(top.Y+top.H) < 7.5 || bottom.Y-(disable.Y+disable.H) < 7.5 {
									t.Errorf("最窄布局可以三行，但每组都要有明确间距：%v %v %v", top, disable, bottom)
								}
							} else if top.X+top.W > disable.X {
								t.Errorf("同一行的检查更新与停用不能重叠：%v %v", top, disable)
							}
							text := detailActionRect(t, tt, locales.UiActionCheckUpdate(), width)
							if text.X < top.X || text.X+text.W > top.X+top.W {
								t.Errorf("检查更新的完整文字应在按钮内：button=%v text=%v", top, text)
							}
							if text.W+0.5 < naturalWidths[locales.UiActionCheckUpdate()] {
								t.Errorf("检查更新不能显示省略号：naturalWidth=%g text=%v", naturalWidths[locales.UiActionCheckUpdate()], text)
							}
							// 检查开始后取消按钮占用同一行，不把下方历史与删除挤走。
							a.checking = skill.Name
							tt.Frame()
							cancel := detailActionRect(t, tt, locales.UiActionCancelCheck(), width)
							assertDetailSameRow(t, cancel, top)
							afterDisable := detailActionRect(t, tt, locales.UiMenuDisable(), width)
							assertDetailSameRow(t, afterDisable, disable)
							after := detailActionRect(t, tt, secondary, width)
							if after.Y != bottom.Y {
								t.Errorf("检查中不应改变辅助动作位置：before=%v after=%v", bottom, after)
							}
						}
						if state != store.StateTrashed {
							trash := detailActionRect(t, tt, locales.UiDetailTrashLabel(), width)
							assertDetailSameRow(t, bottom, trash)
							if bottom.X+bottom.W > trash.X {
								t.Errorf("历史与删除不能重叠：%v %v", bottom, trash)
							}
						}
						for label, wantWidth := range naturalWidths {
							if r, ok := tt.Find(label); ok && r.W+0.5 < wantWidth {
								t.Errorf("动作文字不能省略：%q naturalWidth=%g rendered=%v", label, wantWidth, r)
							}
						}
					})
				}
			}
		}
	}
}

func detailActionRect(t *testing.T, tt *myui.Tester, label string, width int) myui.Rect {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok || r.W <= 0 || r.H <= 0 {
		t.Fatalf("找不到可见动作 %q：%v found=%v", label, r, ok)
	}
	if r.X < 19.5 || r.X+r.W > float32(width)-19.5 || r.Y < 0 || r.Y+r.H > 468.5 {
		t.Errorf("动作 %q 超出详情操作区：%v panelWidth=%d", label, r, width)
	}
	return r
}

func assertDetailSameRow(t *testing.T, left, right myui.Rect) {
	t.Helper()
	if math.Abs(float64(left.Y+left.H/2-right.Y-right.H/2)) > 0.5 {
		t.Errorf("同组动作应居中对齐：%v %v", left, right)
	}
}

// 新布局的底部按钮仍对应当前副本，危险动作点击只打开确认，不提前移除。
func TestDetailActionsKeepStatefulTargets(t *testing.T) {
	a, cfg := newTestApp(t)
	seedSkill(t, cfg, "footer-skill", nil)
	a.detailW = detailMinW
	tt := showApp(a, 880, 520)
	click(t, a, tt, "footer-skill")
	click(t, a, tt, locales.UiSnapshotHistory())
	if a.history == nil || !a.history.open {
		t.Fatal("历史快照按钮应打开当前技能的快照面板")
	}
	a.history.open = false
	tt.Frame()
	click(t, a, tt, locales.UiMenuDisable())
	if s := a.selected(); s == nil || s.State != store.StateDisabled {
		t.Fatalf("停用后应跟随当前技能：%+v", s)
	}
	click(t, a, tt, locales.UiDetailTrashLabel())
	if !a.confirm.open || a.confirm.name != "footer-skill" || a.confirm.state != store.StateDisabled {
		t.Fatalf("删除按钮应确认停用副本：%+v", a.confirm)
	}
	if s := a.selected(); s == nil || s.State != store.StateDisabled {
		t.Fatal("确认前不能移动技能")
	}
	a.confirm.open = false
	tt.Frame()
	click(t, a, tt, locales.UiMenuEnable())
	if s := a.selected(); s == nil || s.State != store.StateEnabled {
		t.Fatalf("启用按钮应恢复当前技能：%+v", s)
	}
}
