package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/ArnoChenFx/kura/locales"
	myui "github.com/egoist/mygo/ui"
)

// 常用宽度下工具区只占紧凑的标题/操作与搜索/筛选两行；高度 640 时，
// 展开详情也应完整展示前两排卡片。窄窗口可以换行，但每个操作必须可达。
func TestCompactHeaderKeepsSpaceForSkillsAndActionsVisible(t *testing.T) {
	for _, width := range []int{900, 1360} {
		for _, mode := range []string{store.ThemeLight, store.ThemeDark} {
			t.Run(fmt.Sprintf("%d-%s", width, mode), func(t *testing.T) {
				a, cfg := newTestApp(t)
				for i := range 12 {
					seedDescribed(t, cfg, fmt.Sprintf("compact-skill-%02d", i), "A short description", nil)
				}
				a.themeMode = mode
				tt := showApp(a, width, 640)
				tt.SetPreferences(myui.Preferences{ReduceMotion: true})
				a.selectSkill(a.skills[0])
				refresh(a, tt)
				left, right := a.rootsEff+gripHit, float32(width)-a.detailEff-gripHit
				var actions []myui.Rect
				for _, label := range []string{
					locales.UiActionRefresh(), locales.UiActionCheckAll(), locales.UiActionImportMeta(),
					locales.UiActionMarket(), locales.UiActionInstall(), locales.UiSearchPlaceholder(),
					locales.UiFilterAll(12), locales.UiFilterEnabled(12),
					locales.UiFilterDisabled(0), locales.UiFilterTrashed(0), locales.UiViewGrid(), locales.UiViewList(),
				} {
					r, ok := tt.Find(label)
					if !ok || r.W <= 0 || r.H <= 0 || r.X < left || r.X+r.W > right+0.5 {
						t.Fatalf("工具 %q 不完整或超出中栏 [%g,%g]：%v found=%v", label, left, right, r, ok)
					}
					actions = append(actions, r)
				}
				for i, r := range actions {
					for _, prior := range actions[:i] {
						if r.X < prior.X+prior.W && prior.X < r.X+r.W &&
							r.Y < prior.Y+prior.H && prior.Y < r.Y+r.H {
							t.Fatalf("工具区操作互相覆盖：%v 与 %v", r, prior)
						}
					}
				}
				search, _ := tt.Find(locales.UiSearchPlaceholder())
				layout, _ := tt.Find(locales.UiViewList())
				if layout.Y+layout.H < search.Y || search.Y+search.H < layout.Y {
					t.Fatalf("排列开关应与搜索框保持同一行：search=%v layout=%v", search, layout)
				}
				if countText(tt, locales.UiLibraryTitle()) != 0 || countText(tt, cfg.Root) != 0 {
					t.Fatal("紧凑工具区不应再单独显示眉题或完整路径行")
				}
				if width == 1360 {
					first, ok := tt.Find("compact-skill-00")
					if !ok || first.Y > 190 {
						t.Fatalf("常用宽度下首排卡片开始太低：%v found=%v", first, ok)
					}
					// 两列布局的第二排开关代表卡片顶部，卡片固定高度需完整落在状态条前。
					second, ok := tt.Find(locales.UiCardSwitchLabel("compact-skill-02"))
					if !ok || second.Y+cardHeight-16 > 600 {
						t.Fatalf("640 高窗口应完整容纳两排卡片：%v found=%v", second, ok)
					}
				}
			})
		}
	}
}

// 完整路径仍能从紧凑标题的悬停提示读取，不需重新占用一整行。
func TestCompactHeaderKeepsFullPathTooltip(t *testing.T) {
	a, cfg := newTestApp(t)
	tt := myui.NewTester(func(c *myui.Context) {
		a.skin = kuraScheme.apply(c, store.ThemeLight)
		a.pal = a.skin.pal
		a.viewMainHeader(c)
	}, 800, 200)
	tt.SetPreferences(myui.Preferences{ReduceMotion: true})
	tt.Frame()
	title, ok := tt.Find(rootLabel(cfg.Root))
	if !ok || title.W <= 0 {
		t.Fatal("目录标题不可见")
	}
	tt.Move(title.X+title.W/2, title.Y+title.H/2)
	tt.Advance(time.Second)
	if path, ok := tt.Find(cfg.Root); !ok || path.H <= 0 {
		t.Fatalf("完整路径应显示在标题提示中：%v found=%v", path, ok)
	}
}

// 大目录的四位数计数不会把最窄中栏的筛选项挤出窗口。
func TestCompactFiltersKeepLargeCountsVisible(t *testing.T) {
	a, _ := newTestApp(t)
	for _, state := range []store.State{store.StateEnabled, store.StateDisabled, store.StateTrashed} {
		for i := range 1000 {
			a.skills = append(a.skills, &store.Skill{Name: fmt.Sprintf("%s-%04d", state, i), State: state})
		}
	}
	tt := myui.NewTester(func(c *myui.Context) {
		a.skin = kuraScheme.apply(c, store.ThemeLight)
		a.pal = a.skin.pal
		a.viewFilterBar(c)
	}, 300, 220)
	tt.SetPreferences(myui.Preferences{ReduceMotion: true})
	tt.Frame()
	for _, label := range []string{
		locales.UiFilterAll(2000), locales.UiFilterEnabled(1000),
		locales.UiFilterDisabled(1000), locales.UiFilterTrashed(1000),
	} {
		r, ok := tt.Find(label)
		if !ok || r.W <= 0 || r.H <= 0 || r.X < 20 || r.X+r.W > 280.5 {
			t.Fatalf("大计数筛选 %q 不完整或超出中栏：%v found=%v", label, r, ok)
		}
	}
	if err := tt.Click(locales.UiFilterTrashed(1000)); err != nil || a.filter != filterTrashed {
		t.Fatalf("换行后的回收站筛选不可点击：filter=%d err=%v", a.filter, err)
	}
}
