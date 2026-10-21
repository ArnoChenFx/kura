package ui

import (
	"fmt"
	"math"
	"testing"
	"time"

	myui "github.com/egoist/mygo/ui"
)

// 原生滚轮保留中间帧并累积完整步长；反向输入从累积目标扣除，停止后不漂移。
func TestWheelScrollSmoothAccumulationAndReverse(t *testing.T) {
	var scroll myui.ScrollState
	tt := myui.NewTester(func(c *myui.Context) {
		myui.Scroll(c).Fill().TrackScroll(&scroll).Children(func() {
			myui.Box(c).Height(2000).Shrink(0)
		})
	}, 300, 200)
	tt.Wheel(100, 100, 0, 100)
	if scroll.Y >= 100 || !tt.FramePending() {
		t.Fatalf("滚轮应平滑过渡，首帧位置=%g", scroll.Y)
	}
	tt.Advance(50 * time.Millisecond)
	if scroll.Y <= 0 || scroll.Y >= 100 {
		t.Fatalf("缺少滚动中间帧：%g", scroll.Y)
	}
	tt.Wheel(100, 100, 0, 100)
	tt.Advance(200 * time.Millisecond)
	if math.Abs(float64(scroll.Y-200)) > 0.1 {
		t.Fatalf("连续两步应累积到200：%g", scroll.Y)
	}
	tt.Wheel(100, 100, 0, 100)
	tt.Advance(40 * time.Millisecond)
	before := scroll.Y
	tt.Wheel(100, 100, 0, -100)
	tt.Advance(200 * time.Millisecond)
	if before <= 200 || math.Abs(float64(scroll.Y-200)) > 0.1 {
		t.Fatalf("反向应抵消尚未完成的同等步长：before=%g, after=%g", before, scroll.Y)
	}
	stop := scroll.Y
	tt.Advance(time.Second)
	if scroll.Y != stop {
		t.Fatalf("停止滚动后发生漂移：%g → %g", stop, scroll.Y)
	}
}

func TestWheelScrollReducedMotionAndVirtualList(t *testing.T) {
	var scroll myui.ScrollState
	var list myui.ListState
	built := 0
	tt := myui.NewTester(func(c *myui.Context) {
		built = 0
		myui.List(c, &list, 1000).Fill().TrackScroll(&scroll).Rows(func(row myui.ListRow) {
			built++
			myui.Text(row.Context, fmt.Sprintf("item-%d", row.Index)).Height(40)
		})
	}, 300, 200)
	tt.SetPreferences(myui.Preferences{ReduceMotion: true})
	tt.Wheel(100, 100, 0, 1000)
	if math.Abs(float64(scroll.Y-1000)) > 0.1 {
		t.Fatalf("减少动效时应直接滚动完整步长：%g", scroll.Y)
	}
	if built >= 30 {
		t.Fatalf("滚动不应破坏虚拟化：构建%d行", built)
	}
	if _, ok := tt.Find("item-25"); !ok {
		t.Fatal("滚动后未构建新的可见行")
	}
	list.ScrollTo(500, myui.Start)
	tt.Frame()
	if _, ok := tt.Find("item-500"); !ok {
		t.Fatal("原生滚动覆盖了列表的行跳转")
	}
}

// 触控板立即接管滚轮动画，横向滚轮仍可滚动。
func TestWheelScrollLeavesPreciseAndHorizontalInput(t *testing.T) {
	var scroll myui.ScrollState
	tt := myui.NewTester(func(c *myui.Context) {
		myui.ScrollBoth(c).Fill().TrackScroll(&scroll).Children(func() {
			myui.Box(c).Width(2000).Height(2000).Shrink(0)
		})
	}, 300, 200)
	tt.Wheel(100, 100, 0, 100)
	tt.Advance(40 * time.Millisecond)
	before := scroll.Y
	tt.Scroll(100, 100, 0, 3)
	if math.Abs(float64(scroll.Y-before-3)) > 0.1 {
		t.Fatalf("触控板应立即移动3：before=%g after=%g", before, scroll.Y)
	}
	stop := scroll.Y
	tt.Advance(time.Second)
	if scroll.Y != stop {
		t.Fatal("触控板接管后旧动画仍在继续")
	}
	tt.Wheel(100, 100, 100, 0)
	tt.Advance(200 * time.Millisecond)
	if math.Abs(float64(scroll.X-100)) > 0.1 || scroll.Y != stop {
		t.Fatalf("横向滚轮应只横向移动：x=%g y=%g", scroll.X, scroll.Y)
	}
}

// 内层到达底部后，后续滚轮输入必须交给外层滚动区。
func TestWheelScrollAtBoundaryReachesOuterContainer(t *testing.T) {
	var inner, outer myui.ScrollState
	tt := myui.NewTester(func(c *myui.Context) {
		myui.Scroll(c).Fill().TrackScroll(&outer).Children(func() {
			myui.Scroll(c).Height(150).FillWidth().Shrink(0).TrackScroll(&inner).Children(func() {
				myui.Box(c).Height(500).Shrink(0)
			})
			myui.Box(c).Height(1000).Shrink(0)
		})
	}, 300, 200)
	inner.Y = inner.MaxY
	tt.Frame()
	tt.Wheel(100, 75, 0, 100)
	tt.Advance(200 * time.Millisecond)
	if inner.Y != inner.MaxY || math.Abs(float64(outer.Y-100)) > 0.1 {
		t.Fatalf("边界滚轮未交给外层：inner=%g/%g outer=%g", inner.Y, inner.MaxY, outer.Y)
	}
}
