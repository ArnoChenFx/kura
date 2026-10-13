package main

import (
	"log"

	"github.com/ArnoChenFx/kura/internal/selfupdate"
	"github.com/ArnoChenFx/kura/internal/store"
	kuraui "github.com/ArnoChenFx/kura/internal/ui"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	// 更新插件必须在 App.Run 之前挂载：原生 UI 更新窗口（无 webview），
	// 每天自动检查一次，语言跟随系统。下载/签名校验/替换/重启全部由
	// MyGo 更新器完成，公钥与更新源在 mygo.json 的 updates 节。
	selfupdate.Install()

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
			Width:     1200,
			Height:    760,
			MinWidth:  880,
			MinHeight: 520,
			// 记住上次窗口位置与大小。
			StateKey: "main",
			// The window shows the interface MyGo draws, not a web page.
			Content: ui.View(app.View),
		})
		// 后台任务的磁盘 I/O 结果经 Window.Update 发布回主线程。
		app.SetWindow(win)
		// 应用菜单：关于 kura / 检查更新…（MyGo 更新窗口）/ 退出。
		// 必须在应用就绪后调用（updater.MenuItem 要读 App.Locale）。
		selfupdate.AppMenu()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
