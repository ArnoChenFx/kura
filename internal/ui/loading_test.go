package ui

import (
	"bytes"
	"testing"
	"time"

	"github.com/ArnoChenFx/kura/locales"
	myui "github.com/egoist/mygo/ui"
)

// 任务指示器保留本地化名称和主题尺寸；减少动效或消失后不继续请求动画帧。
func TestLoadingIndicatorMotionAndVisibility(t *testing.T) {
	show := true
	var size float32
	label := locales.UiTranslateInProgress()
	tt := myui.NewTester(func(c *myui.Context) {
		myui.Box(c).Fill().Center().Background(c.Theme().Surface).Children(func() {
			if show {
				size = c.Theme().FontSize * 1.25
				loadingIndicator(c, label)
			}
		})
	}, 80, 80)
	r, ok := tt.Find(label)
	if !ok || r.W != size || r.H != size {
		t.Fatalf("任务名称或主题尺寸丢失：bounds=%v found=%v size=%g", r, ok, size)
	}
	start := append([]byte(nil), tt.Image().Pix...)
	if !tt.FramePending() {
		t.Fatal("加载中应请求动画帧")
	}
	tt.Advance(100 * time.Millisecond)
	if bytes.Equal(start, tt.Image().Pix) {
		t.Fatal("加载动画没有变化")
	}
	tt.SetPreferences(myui.Preferences{ReduceMotion: true})
	tt.Frame()
	still := append([]byte(nil), tt.Image().Pix...)
	if tt.FramePending() {
		t.Fatal("减少动效时不应持续请求动画帧")
	}
	tt.Advance(time.Second)
	if !bytes.Equal(still, tt.Image().Pix) {
		t.Fatal("减少动效时指示器仍在运动")
	}
	show = false
	tt.SetPreferences(myui.Preferences{ReduceMotion: false})
	tt.Frame()
	if _, ok := tt.Find(label); ok || tt.FramePending() {
		t.Fatal("任务结束后指示器或动画帧仍存在")
	}
	// 再次出现时应重新绘制，不能持有上次构建的元素。
	tt.Frame()
	show = true
	tt.Frame()
	if _, ok := tt.Find(label); !ok || !tt.FramePending() {
		t.Fatal("新任务的加载指示器未恢复")
	}
}
