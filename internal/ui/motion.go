package ui

import (
	"time"

	myui "github.com/egoist/mygo/ui"
)

// 动效统一用短缓出曲线，避免滚动、搜索或异步操作后出现长时间的追赶。
// 位移动画交给绘制层，不为每一帧启动后台任务或重建应用数据。
var (
	interactionMotion = myui.Animation{Duration: 150 * time.Millisecond, Ease: myui.EaseOut}
	contentMotion     = myui.Animation{Duration: 200 * time.Millisecond, Ease: myui.EaseOut}
)

// surfaceMotion 只负责外观，不读取 Clicked/Pressed，网格单元格仍拥有点击。
// 标量动画在减少动效模式下直接到位；颜色和边框始终只有同一层。
func surfaceMotion(c *myui.Context, e myui.Element, pal palette, selected, lift bool) {
	hover := e.AnimateTo("surface-hover", boolF(e.Hovered() && !selected), interactionMotion)
	chosen := e.AnimateTo("surface-selected", boolF(selected), interactionMotion)
	emphasis := hover
	if !c.Theme().Dark {
		// 纸白卡片以投影表达选中，深色保留原有的底色与悬停投影。
		emphasis = max(hover, chosen)
	}
	e.Background(pal.card.Mix(pal.cardHover, hover).Mix(pal.cardSel, chosen)).
		Border(1, pal.line.Mix(pal.lineHover, hover).Mix(c.Theme().Accent, chosen)).
		Shadow(0, 2+3*emphasis, 6+6*emphasis, 0, pal.shadow.Mix(pal.shadowHover, emphasis))
	pose := myui.Pose{}
	if lift && e.Hovered() && !c.Preferences().ReduceMotion {
		pose.Y = -2
	}
	e.AnimatePose(pose, interactionMotion)
}

// buttonMotion 保留原生按钮的焦点、键盘与无障碍语义，只增加轻微的按压反馈。
func buttonMotion(c *myui.Context, e myui.Element, enabled bool) {
	pose := myui.Pose{}
	if enabled && !c.Preferences().ReduceMotion && e.Pressed() {
		pose.Scale = 0.97
	}
	e.AnimatePose(pose, interactionMotion)
}

// enterMotion 仅在内容进入已存在的父容器时淡入；没有循环、延迟或退出残影，
// 因而不会让搜索后的旧结果继续留在屏幕上，也不会拖延焦点和点击。
func enterMotion(c *myui.Context, e myui.Element) {
	if c.Preferences().ReduceMotion {
		return
	}
	e.Transition(myui.ElementTransition{
		Animation: &contentMotion,
		Colors:    true,
		Enter:     &myui.Motion{Y: 8},
	})
}

// countMotion 只让发生变化的数字过渡；系统要求减少动效时直接显示最终内容。
func countMotion(c *myui.Context, e myui.Element) {
	if !c.Preferences().ReduceMotion {
		e.ContentTransition(myui.ContentTransition{Numeric: true, Animation: &contentMotion})
	}
}

// detailMotion 从右侧短距离进入，与详情栏的空间位置一致；不改变三栏宽度，
// 拖动分割线仍然跟手，关闭详情也不保留会遮挡内容的退出残影。
func detailMotion(c *myui.Context, panel myui.Element) {
	if !c.Preferences().ReduceMotion {
		panel.Transition(myui.ElementTransition{
			Animation: &contentMotion,
			Colors:    true,
			Enter:     &myui.Motion{X: 16},
		})
	}
}
