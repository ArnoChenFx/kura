package ui

// 市场搜索仅处理假榜单；通过真实输入与点击验证过滤后的技能身份和翻页行为。

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/ArnoChenFx/kura/internal/market"
	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/ArnoChenFx/kura/locales"
	myui "github.com/egoist/mygo/ui"
)

func typeMarketSearch(t *testing.T, a *App, tt *myui.Tester, query string) {
	t.Helper()
	if err := tt.Click(locales.UiSearchPlaceholder()); err != nil {
		t.Fatal(err)
	}
	tt.Key(myui.Ctrl, myui.KeyA)
	tt.Key(0, myui.KeyBackspace)
	tt.Type(query)
	refresh(a, tt)
}

func TestMarketSearchNameSourceAndClear(t *testing.T) {
	for _, mode := range []string{store.ViewGrid, store.ViewList} {
		t.Run(mode, func(t *testing.T) {
			a, f, _ := installMarketApp(t)
			a.viewMode = mode
			seedSkill(t, a.cfg, "local-selection", nil)
			first := mkSkill("unrelated", 2)
			target := market.Skill{Source: "Other/Toolkit", SkillID: "target-id", Name: "target-dock", Installs: 1}
			f.set(market.ViewAllTime, 0, okPage(false, first, target))
			tt := showApp(a, 980, 640)
			click(t, a, tt, "local-selection")
			click(t, a, tt, locales.UiActionMarket())

			for _, query := range []string{"TARGET", "  other/toolkit  "} {
				typeMarketSearch(t, a, tt, query)
				if tt.HasText(first.Name) || !tt.HasText(target.Name) {
					t.Fatalf("搜索 %q 应只显示目标技能：%v", query, tt.Texts())
				}
				if a.selName != "local-selection" || a.selState != store.StateEnabled {
					t.Fatal("市场搜索不应改变本地技能选择")
				}
			}
			if !tt.HasText(locales.UiMarketSearchScope(2)) {
				t.Fatalf("搜索范围应明确是已加载的两个技能：%v", tt.Texts())
			}

			typeMarketSearch(t, a, tt, "no-such-skill")
			if !tt.HasText(locales.UiMarketSearchEmpty()) || tt.HasText(target.Name) {
				t.Fatalf("无匹配时应显示搜索空态：%v", tt.Texts())
			}
			click(t, a, tt, locales.UiSearchClear())
			for range 3 {
				tt.Frame()
			}
			if a.market.query != "" || !tt.HasText(first.Name) || !tt.HasText(target.Name) {
				t.Fatalf("清空后应恢复原榜单：query=%q texts=%v", a.market.query, tt.Texts())
			}
			if strings.Join(f.calls, " ") != "all-time/0" {
				t.Fatalf("搜索与清空不应触发额外请求：%v", f.calls)
			}
		})
	}
}

func TestMarketSearchLoadMoreAndSwitchBoard(t *testing.T) {
	a, f, _ := installMarketApp(t)
	f.set(market.ViewAllTime, 0, okPage(true, mkSkill("unrelated", 3)))
	f.set(market.ViewAllTime, 1, okPage(false, mkSkill("needle-page-two", 2)))
	f.set(market.ViewHot, 0, okPage(false, mkSkill("needle-hot", 9), mkSkill("hot-other", 8)))
	tt := showApp(a, 980, 640)
	click(t, a, tt, locales.UiActionMarket())
	typeMarketSearch(t, a, tt, "needle")

	if !tt.HasText(locales.UiMarketSearchEmpty()) || !tt.HasText(locales.UiMarketLoadMore()) {
		t.Fatalf("已加载内容无匹配时仍应允许加载下一页：%v", tt.Texts())
	}
	more, ok := tt.Find(locales.UiMarketLoadMore())
	if !ok || more.Y+more.H > 640 {
		t.Fatalf("搜索空态不能把加载更多挤到窗口外：%+v", more)
	}
	click(t, a, tt, locales.UiMarketLoadMore())
	if !tt.HasText("needle-page-two") || tt.HasText("unrelated") || !tt.HasText(locales.UiMarketSearchScope(2)) {
		t.Fatalf("新加载的匹配项应立即出现在结果中：%v", tt.Texts())
	}
	click(t, a, tt, locales.UiMarketViewHot())
	if a.market.query != "needle" || !tt.HasText("needle-hot") || tt.HasText("hot-other") {
		t.Fatalf("切榜单应保留搜索词并过滤新榜单：query=%q texts=%v", a.market.query, tt.Texts())
	}
	click(t, a, tt, locales.UiMarketViewAllTime())
	if !tt.HasText("needle-page-two") || tt.HasText("needle-hot") {
		t.Fatalf("切回后应显示原榜单的匹配项：%v", tt.Texts())
	}
	if strings.Join(f.calls, " ") != "all-time/0 all-time/1 hot/0" {
		t.Fatalf("仅显式翻页与首次切榜单需要请求：%v", f.calls)
	}
}

func TestMarketSearchFilteredActionsKeepSkillIdentity(t *testing.T) {
	for _, mode := range []string{store.ViewGrid, store.ViewList} {
		t.Run(mode, func(t *testing.T) {
			a, f, _ := installMarketApp(t)
			a.viewMode = mode
			seedMarketRepo(t, "owner/right-repo", "skills/chosen-skill", "skills/other")
			target := market.Skill{Source: "owner/right-repo", SkillID: "chosen-skill", Name: "chosen-skill"}
			f.set(market.ViewAllTime, 0, okPage(false, mkSkill("wrong-first", 4), target))
			f.setDoc(target.Source, target.Name, docResp{doc: market.Document{SkillMD: "# correct-target"}})
			tt := showApp(a, 980, 640)
			click(t, a, tt, locales.UiActionMarket())
			typeMarketSearch(t, a, tt, "right-repo")

			click(t, a, tt, target.Name)
			if a.market.detail != target || !tt.HasText("correct-target") {
				t.Fatalf("过滤后的第一项应仍打开原目标：detail=%+v texts=%v", a.market.detail, tt.Texts())
			}
			click(t, a, tt, locales.UiMarketBackToBoard())
			if a.market.query != "right-repo" {
				t.Fatal("详情返回应保留搜索")
			}
			click(t, a, tt, locales.UiMarketInstall())
			if !a.installedNames()[target.Name] || a.installedNames()["wrong-first"] || a.installedNames()["other"] {
				t.Fatalf("安装必须指向过滤后的原技能：%v err=%q status=%q texts=%v",
					a.installedNames(), a.market.err, a.status, tt.Texts())
			}
			if a.market.screen != marketList || !tt.HasText(locales.UiMarketInstalled()) {
				t.Fatalf("安装操作应留在榜单并更新目标状态：%v", tt.Texts())
			}
		})
	}
}

func TestMarketSearchResetsVirtualScroll(t *testing.T) {
	for _, mode := range []string{store.ViewGrid, store.ViewList} {
		t.Run(mode, func(t *testing.T) {
			a, f, _ := installMarketApp(t)
			a.viewMode = mode
			skills := make([]market.Skill, 300)
			for i := range skills {
				skills[i] = mkSkill(fmt.Sprintf("skill-%03d", i), int64(300-i))
			}
			f.set(market.ViewAllTime, 0, okPage(false, skills...))
			tt := showApp(a, 980, 640)
			click(t, a, tt, locales.UiActionMarket())
			if tt.HasText("skill-299") {
				t.Fatal("大量榜单仍应只构建可见项")
			}
			if mode == store.ViewGrid {
				a.market.grid.ScrollToEnd()
			} else {
				a.market.list.ScrollToEnd()
			}
			tt.Frame()
			if !tt.HasText("skill-299") {
				t.Fatal("测试应先滚动到榜单底部")
			}
			typeMarketSearch(t, a, tt, "skill-000")
			if !tt.HasText("skill-000") || tt.HasText("skill-299") {
				t.Fatalf("改搜索后应回顶部显示匹配项：%v", tt.Texts())
			}
			typeMarketSearch(t, a, tt, "")
			for range 3 {
				tt.Frame()
			}
			if !tt.HasText("skill-000") || tt.HasText("skill-299") {
				t.Fatalf("清空后应从榜单顶部继续浏览：%v", tt.Texts())
			}
		})
	}
}

// 滚轮尚未滑到目标时，搜索和切榜单都必须立即回到顶部，后续动画帧也不能拉走新结果。
func TestMarketSearchAndBoardSwitchInterruptWheelGlide(t *testing.T) {
	for _, mode := range []string{store.ViewGrid, store.ViewList} {
		for _, action := range []string{"search", "switch-board"} {
			for _, delay := range []time.Duration{0, 50 * time.Millisecond} {
				t.Run(mode+"/"+action+"/"+delay.String(), func(t *testing.T) {
					a, f, _ := installMarketApp(t)
					a.viewMode = mode
					skills, hot := make([]market.Skill, 300), make([]market.Skill, 300)
					for i := range skills {
						skills[i] = mkSkill(fmt.Sprintf("skill-%03d", i), int64(300-i))
						hot[i] = mkSkill(fmt.Sprintf("hot-skill-%03d", i), int64(300-i))
					}
					f.set(market.ViewAllTime, 0, okPage(false, skills...))
					f.set(market.ViewHot, 0, okPage(false, hot...))
					var wheel *wheelScroll
					tt := myui.NewTester(func(c *myui.Context) {
						a.View(c)
						if a.market.screen != marketList {
							return
						}
						handle := a.market.list.Handle
						if mode == store.ViewGrid {
							handle = a.market.grid.Handle
						}
						if e := c.Resolve(handle); e.Valid() {
							wheel = myui.Local(e, "kura-wheel-scroll", func() wheelScroll { return wheelScroll{} })
						}
					}, 980, 640)
					tt.SetPreferences(myui.Preferences{ReduceMotion: false})
					refresh(a, tt)
					refresh(a, tt)
					click(t, a, tt, locales.UiActionMarket())
					// 先缓存热度榜，避免切换时卸载空列表碰巧清掉动画，掩盖跳转优先级错误。
					click(t, a, tt, locales.UiMarketViewHot())
					tt.Advance(250 * time.Millisecond)
					hotFirst, ok := tt.Find("hot-skill-000")
					if !ok {
						t.Fatal("前置条件：热度榜应已加载")
					}
					click(t, a, tt, locales.UiMarketViewAllTime())
					tt.Advance(250 * time.Millisecond)
					first, ok := tt.Find("skill-000")
					if !ok {
						t.Fatal("前置条件：总榜应从首项开始")
					}
					tt.Scroll(first.X+first.W/2, first.Y+first.H/2, 0, 2000)
					if delay > 0 {
						tt.Advance(delay)
					}
					if wheel == nil || !wheel.scroll.Scrolling() {
						t.Fatal("前置条件：必须在真实滚轮动画未完成时操作")
					}
					if delay > 0 && (wheel.scroll.Y <= 0 || wheel.scroll.Y >= wheel.target) {
						t.Fatalf("前置条件：应处于滚动中间帧：y=%g target=%g", wheel.scroll.Y, wheel.target)
					}

					wantName, wantY := "skill-000", first.Y
					if action == "search" {
						// 留下 100 条匹配结果，仍可滚动；不能靠内容高度缩短阻止旧目标漂移。
						typeMarketSearch(t, a, tt, "skill-0")
					} else {
						wantName, wantY = "hot-skill-000", hotFirst.Y
						click(t, a, tt, locales.UiMarketViewHot())
					}
					checkTop := func(stage string) {
						t.Helper()
						got, found := tt.Find(wantName)
						if !found || math.Abs(float64(got.Y-wantY)) > 0.5 {
							t.Fatalf("%s：新结果应停在顶部，first=%q wantY=%g got=%+v found=%v",
								stage, wantName, wantY, got, found)
						}
					}
					checkTop("操作后立即")
					tt.Advance(200 * time.Millisecond)
					checkTop("旧动画原定结束后")
					if wheel.scroll.Scrolling() {
						t.Fatal("重置位置后不应留下旧滚轮动画")
					}
				})
			}
		}
	}
}
