package ui

import (
	"bytes"
	"image"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ArnoChenFx/kura/internal/store"
	"github.com/ArnoChenFx/kura/locales"
	myui "github.com/egoist/mygo/ui"
)

// motionCard 打开真实 App 中的一张卡片，等初始内容进入结束后返回没有文字
// 覆盖的卡片内采样点。只推进 Tester 的虚拟时间，不等待真实时钟。
func motionCard(t *testing.T, reduced bool) (*App, *myui.Tester, image.Point) {
	t.Helper()
	a, cfg := newTestApp(t)
	seedDescribed(t, cfg, "motion-card", "Short description", nil)
	tt := showApp(a, 1280, 800)
	tt.SetDark(true)
	tt.SetPreferences(myui.Preferences{ReduceMotion: reduced})
	refresh(a, tt)
	tt.Advance(contentMotion.Duration + interactionMotion.Duration)
	name, nameOK := tt.Find("motion-card")
	toggle, toggleOK := tt.Find(locales.UiCardSwitchLabel("motion-card"))
	if !nameOK || !toggleOK {
		t.Fatal("找不到动效卡片")
	}
	return a, tt, image.Pt(int(toggle.X+toggle.W/2), int(name.Y+80))
}

// 悬停真实卡片应经过可见的中间颜色，到达目标颜色后不再持续请求动画帧。
func TestSurfaceMotionCardHoverInterpolatesAndSettles(t *testing.T) {
	a, tt, point := motionCard(t, false)
	idle := tt.Image().RGBAAt(point.X, point.Y)
	if !near(idle, a.pal.card, 1) {
		t.Fatalf("初始采样点应在卡片内：%v，想要 %v", idle, a.pal.card)
	}
	tt.Move(float32(point.X), float32(point.Y))
	start := tt.Image().RGBAAt(point.X, point.Y)
	if start != idle {
		t.Fatalf("悬停初帧不应跳到最终颜色：idle=%v start=%v", idle, start)
	}
	tt.Advance(interactionMotion.Duration / 3)
	middle := tt.Image().RGBAAt(point.X, point.Y)
	tt.Advance(interactionMotion.Duration)
	end := tt.Image().RGBAAt(point.X, point.Y)
	if middle == idle || middle == end {
		t.Fatalf("应存在不同于起止状态的中间帧：idle=%v middle=%v end=%v", idle, middle, end)
	}
	if !near(end, a.pal.cardHover, 1) {
		t.Fatalf("悬停结束应到达皮肤目标色：%v，想要 %v", end, a.pal.cardHover)
	}
	final := append([]byte(nil), tt.Image().Pix...)
	tt.Advance(2 * time.Second)
	if tt.FramePending() {
		t.Fatal("悬停动效结束后仍有未完成帧")
	}
	if !bytes.Equal(final, tt.Image().Pix) {
		t.Fatal("指针静止且动效结束后画面仍在变化")
	}
}

// 系统要求减少动效时，卡片保持原位置，悬停颜色直接到位。
func TestSurfaceMotionReducedMotionKeepsCardStill(t *testing.T) {
	a, tt, point := motionCard(t, true)
	before, _ := tt.Find("motion-card")
	tt.Move(float32(point.X), float32(point.Y))
	after, _ := tt.Find("motion-card")
	if before != after {
		t.Fatalf("减少动效时卡片不应抬起：before=%v after=%v", before, after)
	}
	if got := tt.Image().RGBAAt(point.X, point.Y); !near(got, a.pal.cardHover, 1) {
		t.Fatalf("减少动效时悬停样式应立即到位：%v，想要 %v", got, a.pal.cardHover)
	}
	tt.Advance(time.Second)
	end, _ := tt.Find("motion-card")
	if end != before || tt.FramePending() {
		t.Fatalf("减少动效时不应有迟到的位移或循环帧：before=%v end=%v pending=%v", before, end, tt.FramePending())
	}
}

// 长描述的两行文字必须给底部状态标签留出空间，不因 Grow 与卡片固定高度
// 的组合侵入标签；直接渲染生产 viewCard 并检查实际布局边界。
func TestCardLongDescriptionDoesNotOverlapStatus(t *testing.T) {
	for _, width := range []int{248, 320} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			a, _ := newTestApp(t)
			desc := strings.Repeat("A long description with enough words to wrap across several lines. ", 8)
			skill := &store.Skill{
				Name: "layout-skill", State: store.StateDisabled, Description: desc,
				HasSKILLMD: false,
			}
			a.updates[skill.Name] = true
			tt := myui.NewTester(func(c *myui.Context) {
				a.skin = kuraScheme.apply(c, store.ThemeLight)
				a.pal = a.skin.pal
				myui.Box(c).Size(float32(width+40), cardHeight+40).Padding(20).Children(func() {
					a.viewCard(c, skill, false)
				})
			}, width+40, cardHeight+40)
			tt.SetPreferences(myui.Preferences{ReduceMotion: true})
			tt.Frame()
			description, ok := tt.Find(desc)
			if !ok || description.H <= 0 {
				t.Fatalf("长描述应占有可见空间，得到 %v，found=%v", description, ok)
			}
			for _, label := range []string{locales.UiStateDisabled(), locales.UiPillHasUpdate(), locales.UiPillNoSkillMD()} {
				tag, ok := tt.Find(label)
				if !ok {
					t.Fatalf("缺少状态标签 %q", label)
				}
				if description.Y+description.H > tag.Y {
					t.Errorf("描述与 %q 重叠：description=%v tag=%v", label, description, tag)
				}
				if tag.Y+tag.H > 20+cardHeight {
					t.Errorf("标签 %q 超出卡片底部：%v", label, tag)
				}
			}
		})
	}
}

// 分段控件保留原生点击与方向键选择；两组选择互相独立，动效结束后停止重绘。
func TestFilterTabsKeepSelectionIndependent(t *testing.T) {
	for _, reduced := range []bool{false, true} {
		t.Run(strconv.FormatBool(reduced), func(t *testing.T) {
			first, second := 0, 0
			tt := myui.NewTester(func(c *myui.Context) {
				kuraScheme.apply(c, store.ThemeDark)
				myui.Column(c).Padding(20).Gap(20).Children(func() {
					filterTabs(c, &first, "All", "Enabled", "Disabled")
					filterTabs(c, &second, "Grid", "List")
				})
			}, 400, 160)
			tt.SetPreferences(myui.Preferences{ReduceMotion: reduced})
			tt.Frame()
			target, ok := tt.Find("Disabled")
			if !ok {
				t.Fatal("找不到 Disabled 分段")
			}
			tt.ClickAt(target.X+target.W/2, target.Y+target.H-1)
			if first != 2 || second != 0 {
				t.Fatalf("点击分段底边未选中目标：first=%d second=%d", first, second)
			}
			tt.Key(0, myui.KeyLeft)
			if first != 1 || second != 0 {
				t.Fatalf("分段键盘选择错误：first=%d second=%d", first, second)
			}
			tt.Click("List")
			if first != 1 || second != 1 {
				t.Fatalf("两组分段选择互相影响：first=%d second=%d", first, second)
			}
			tt.Advance(time.Second)
			if tt.FramePending() {
				t.Fatal("分段动效结束后仍有未完成帧")
			}
		})
	}
}

// 长名称、长来源和多个标签共存时，开关仍可见；说明、来源与标签不重叠，
// 标签换行后仍在列表行内。覆盖最窄中栏的实际行宽。
func TestListRowNarrowKeepsActionsAndMetadataVisible(t *testing.T) {
	for _, width := range []int{260, 320, 640} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			a, _ := newTestApp(t)
			s := &store.Skill{
				Name: "a-very-long-skill-name-that-needs-truncation", State: store.StateDisabled,
				Description: "A long description that should truncate before reaching the toggle.",
				Meta: &store.Meta{Source: store.Source{Type: store.SourceGitHub,
					URL: "https://github.com/long-organization-name/a-very-long-repository.git"}},
			}
			a.updates[s.Name] = true
			tt := myui.NewTester(func(c *myui.Context) {
				a.skin = kuraScheme.apply(c, store.ThemeLight)
				a.pal = a.skin.pal
				myui.Column(c).Width(float32(width)).Children(func() { a.viewRow(c, s, false) })
			}, width, 240)
			tt.SetPreferences(myui.Preferences{ReduceMotion: true})
			tt.Frame()
			name, _ := tt.Find(s.Name)
			desc, _ := tt.Find(s.Description)
			sw, ok := tt.Find(locales.UiCardSwitchLabel(s.Name))
			if !ok || sw.W < 30 || sw.X+sw.W > float32(width)-10 || name.X+name.W > sw.X || desc.X+desc.W > sw.X {
				t.Fatalf("名称或描述挤占了开关：name=%v desc=%v switch=%v", name, desc, sw)
			}
			labels := []string{cardSource(s), locales.UiPillHasUpdate(), locales.UiStateDisabled(), locales.UiPillNoSkillMD()}
			var boxes []myui.Rect
			for _, label := range labels {
				box, ok := tt.Find(label)
				if !ok || box.W <= 0 || box.H <= 0 || box.X < 0 || box.X+box.W > float32(width) || box.Y < desc.Y+desc.H {
					t.Fatalf("信息 %q 越界或覆盖说明：box=%v desc=%v", label, box, desc)
				}
				for _, prior := range boxes {
					if box.X < prior.X+prior.W && prior.X < box.X+box.W && box.Y < prior.Y+prior.H && prior.Y < box.Y+box.H {
						t.Fatalf("信息区发生重叠：%v 与 %v", box, prior)
					}
				}
				boxes = append(boxes, box)
			}
		})
	}
}
