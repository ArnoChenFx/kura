package ui

import (
	"fmt"
	"math"
	"testing"
	"time"

	myui "github.com/egoist/mygo/ui"
)

// 显式定位必须优先于尚未结束的滚轮动画，市场搜索重置位置依赖这个约定。
func TestWheelScrollYieldsToListJumpDuringGlide(t *testing.T) {
	for _, delay := range []time.Duration{0, 50 * time.Millisecond} {
		t.Run(delay.String(), func(t *testing.T) { testWheelScrollListJump(t, delay) })
	}
}

func testWheelScrollListJump(t *testing.T, delay time.Duration) {
	var list myui.ListState
	var wheel *wheelScroll
	var reset uint64
	tt := myui.NewTester(func(c *myui.Context) {
		e := smoothScroll(myui.List(c.Key("list"), &list, 1000).Fill().Rows(func(row myui.ListRow) {
			myui.Text(row.Context, fmt.Sprintf("item-%d", row.Index)).Height(40)
		}), reset)
		wheel = myui.Local(e, "kura-wheel-scroll", func() wheelScroll { return wheelScroll{} })
	}, 300, 200)
	tt.Scroll(100, 100, 0, 2000)
	if delay > 0 {
		tt.Advance(delay)
	}
	if !wheel.scroll.Scrolling() {
		t.Fatalf("前置条件：滚轮应仍在动画中，y=%g moving=%v", wheel.scroll.Y, wheel.scroll.Scrolling())
	}
	reset++ // 与市场切榜单、搜索时的显式跳转契约一致。
	list.ScrollTo(0, myui.Start)
	tt.Frame()
	tt.Advance(200 * time.Millisecond)
	if row, ok := tt.Find("item-0"); !ok || math.Abs(float64(row.Y)) > 0.1 {
		t.Fatalf("列表跳到顶部后不能被旧滚轮动画带走：first=%v found=%v y=%g", row, ok, wheel.scroll.Y)
	}
}

// ListState 的行锚点跨页面保留；临时的平滑滚动状态不能把返回位置改回顶部。
func TestWheelScrollListKeepsPositionAfterHidden(t *testing.T) {
	var list myui.ListState
	show := true
	tt := myui.NewTester(func(c *myui.Context) {
		if !show {
			myui.Box(c.Key("other")).Fill()
			return
		}
		smoothScroll(myui.List(c.Key("list"), &list, 1000).Fill().Rows(func(row myui.ListRow) {
			myui.Text(row.Context, fmt.Sprintf("item-%d", row.Index)).Height(40)
		}))
	}, 300, 200)
	list.ScrollTo(500, myui.Start)
	tt.Frame()
	before, ok := tt.Find("item-500")
	if !ok {
		t.Fatal("前置条件：应先定位到第 500 项")
	}
	show = false
	tt.Frame()
	tt.Frame()
	show = true
	tt.Frame()
	tt.Advance(200 * time.Millisecond)
	after, ok := tt.Find("item-500")
	if !ok || math.Abs(float64(after.Y-before.Y)) > 0.1 {
		t.Fatalf("离开再返回应保留原位置：before=%v after=%v found=%v", before, after, ok)
	}
}
