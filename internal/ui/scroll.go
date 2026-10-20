package ui

import (
	"time"

	myui "github.com/egoist/mygo/ui"
)

const wheelScale = float32(0.45)

var wheelAnimation = myui.Animation{Duration: 140 * time.Millisecond, Ease: myui.EaseOut}

type wheelScroll struct {
	scroll myui.ScrollState
	target float32
	delta  float32
	reset  uint64
}

// smoothScroll 只细化离散滚轮输入；触控板已有连续位移和系统惯性，继续交给
// 原生滚动。状态跟随容器身份，主题重建不会重置位置，虚拟列表仍管理行锚点。
func smoothScroll(e myui.Element, reset ...uint64) myui.Element {
	s := myui.Local(e, "kura-wheel-scroll", func() wheelScroll { return wheelScroll{} })
	// 显式行跳转与动画各有自己的状态；通过递增序号让程序跳转优先，
	// 包括动画刚开始、当前位置恰好仍在顶部的情况。
	if len(reset) > 0 && reset[0] != s.reset {
		s.stop()
		s.reset = reset[0]
	}
	return e.TrackScroll(&s.scroll).HandleInput(s.input)
}

func (s *wheelScroll) stop() {
	// 清掉动画，保留框架已经写回的位置与容器身份。
	s.scroll = myui.ScrollState{
		Handle: s.scroll.Handle, X: s.scroll.X, Y: s.scroll.Y,
		MaxX: s.scroll.MaxX, MaxY: s.scroll.MaxY,
	}
	s.delta = 0
}

func (s *wheelScroll) input(ev myui.InputEvent) bool {
	if ev.Kind == myui.InputKeyDown || ev.Kind == myui.InputPointerDown {
		s.stop()
	}
	if ev.Kind != myui.InputScroll {
		return false
	}
	if ev.Precise || ev.Mods&myui.Shift != 0 || ev.DY == 0 {
		s.stop()
		return false
	}
	// 连续同向滚轮累积目标，反向则立即从当前可见位置转向，避免继续追赶
	// 尚未完成的旧目标。滚动条拖动、触控板或列表跳转后也重新从当前位置开始。
	if !s.scroll.Scrolling() || s.delta*ev.DY <= 0 {
		s.target = s.scroll.Y
	}
	s.target = max(0, min(s.target+ev.DY*wheelScale, s.scroll.MaxY))
	if s.target == s.scroll.Y {
		return false
	}
	s.delta = ev.DY
	s.scroll.ScrollTo(s.scroll.X, s.target, wheelAnimation)
	return true
}
