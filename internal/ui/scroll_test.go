package ui

import (
	"fmt"
	"math"
	"testing"
	"time"

	myui "github.com/egoist/mygo/ui"
)

// 验证真实滚轮事件经过容器的处理器，能经过中间帧抵达更小的目标，快速连滚
// 不丢步，反向滚动立即转向，且停止后不再漂移。
func TestWheelScrollSmoothAccumulationAndReverse(t *testing.T) {
	var s *wheelScroll
	tt := myui.NewTester(func(c *myui.Context) {
		e := smoothScroll(myui.Scroll(c).Fill())
		s = myui.Local(e, "kura-wheel-scroll", func() wheelScroll { return wheelScroll{} })
		e.Children(func() { myui.Box(c).Height(2000).Shrink(0) })
	}, 300, 200)
	tt.Scroll(100, 100, 0, 100)
	if s.scroll.Y >= 45 {
		t.Fatalf("滚轮应平滑过渡，首帧位置=%g", s.scroll.Y)
	}
	tt.Advance(50 * time.Millisecond)
	if s.scroll.Y <= 0 || s.scroll.Y >= 45 {
		t.Fatalf("缺少滚动中间帧：%g", s.scroll.Y)
	}
	tt.Scroll(100, 100, 0, 100)
	tt.Advance(200 * time.Millisecond)
	if math.Abs(float64(s.scroll.Y-90)) > 0.1 || s.scroll.Scrolling() {
		t.Fatalf("连续两步应累积并停在90：位置=%g, moving=%v", s.scroll.Y, s.scroll.Scrolling())
	}
	tt.Scroll(100, 100, 0, 100)
	tt.Advance(40 * time.Millisecond)
	before := s.scroll.Y
	tt.Scroll(100, 100, 0, -100)
	tt.Advance(200 * time.Millisecond)
	if math.Abs(float64(s.scroll.Y-(before-45))) > 0.1 {
		t.Fatalf("反向应从当前可见位置向上45：before=%g, after=%g", before, s.scroll.Y)
	}
	stop := s.scroll.Y
	tt.Advance(time.Second)
	if s.scroll.Y != stop {
		t.Fatalf("停止滚动后发生漂移：%g → %g", stop, s.scroll.Y)
	}
}

func TestWheelScrollReducedMotionAndVirtualList(t *testing.T) {
	var s *wheelScroll
	var list myui.ListState
	built := 0
	tt := myui.NewTester(func(c *myui.Context) {
		built = 0
		e := myui.List(c, &list, 1000).Fill().Rows(func(row myui.ListRow) {
			built++
			myui.Text(row.Context, fmt.Sprintf("item-%d", row.Index)).Height(40)
		})
		smoothScroll(e)
		s = myui.Local(e, "kura-wheel-scroll", func() wheelScroll { return wheelScroll{} })
	}, 300, 200)
	tt.SetPreferences(myui.Preferences{ReduceMotion: true})
	tt.Scroll(100, 100, 0, 1000)
	if math.Abs(float64(s.scroll.Y-450)) > 0.1 || s.scroll.Scrolling() {
		t.Fatalf("减少动效时应直接滚动较小步长：%g, moving=%v", s.scroll.Y, s.scroll.Scrolling())
	}
	if built >= 30 {
		t.Fatalf("平滑滚动不应破坏虚拟化：构建%d行", built)
	}
	if _, ok := tt.Find("item-12"); !ok {
		t.Fatal("滚动后未构建新的可见行")
	}
	// 列表自身跳转仍然生效，不被 TrackScroll 的旧位置覆盖。
	list.ScrollTo(500, myui.Start)
	tt.Frame()
	if _, ok := tt.Find("item-500"); !ok {
		t.Fatal("TrackScroll 覆盖了列表的行跳转")
	}
}

func TestWheelScrollLeavesPreciseAndBoundaryInput(t *testing.T) {
	s := wheelScroll{scroll: myui.ScrollState{Y: 100, MaxY: 500}}
	for _, ev := range []myui.InputEvent{
		{Kind: myui.InputScroll, DY: 3, Precise: true},
		{Kind: myui.InputScroll, DY: 50, Mods: myui.Shift},
		{Kind: myui.InputScroll, DX: 50},
		{Kind: myui.InputPointerDown},
	} {
		if s.input(ev) || s.scroll.Scrolling() {
			t.Fatalf("不应拦截原生输入：%+v", ev)
		}
	}
	s.scroll.Y = 500
	if s.input(myui.InputEvent{Kind: myui.InputScroll, DY: 50}) {
		t.Fatal("到达底部应允许外层滚动容器处理事件")
	}
	s.scroll.Y = 0
	if s.input(myui.InputEvent{Kind: myui.InputScroll, DY: -50}) {
		t.Fatal("到达顶部应允许外层滚动容器处理事件")
	}
}
