package main

import (
	"log"

	"github.com/ArnoChenFx/kura/internal/store"
	kuraui "github.com/ArnoChenFx/kura/internal/ui"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	// 默认配置：根目录 ~/.agents/skills，kura 数据目录 ~/.kura。
	// 目录不存在不致命：界面会显示错误并保持可用。
	cfg, err := store.DefaultConfig()
	if err != nil {
		log.Fatal(err)
	}
	app := kuraui.NewApp(cfg)

	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:     "kura",
			Width:     980,
			Height:    640,
			MinWidth:  720,
			MinHeight: 460,
			// 记住上次窗口位置与大小。
			StateKey: "main",
			// The window shows the interface MyGo draws, not a web page.
			Content: ui.View(app.View),
		})
		// 后台任务的磁盘 I/O 结果经 Window.Update 发布回主线程。
		app.SetWindow(win)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
