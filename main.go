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
	// 外观偏好（~/.kura/config.json 的 theme）在 App.Run 之前同步给 MyGo
	// （MyGo 允许这样用已存的偏好），原生标题栏与系统菜单从第一帧起就是
	// 所选的外观。
	app.SyncTheme()

	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:     "kura",
			Width:     1200,
			Height:    760,
			MinWidth:  880,
			MinHeight: 520,
			// 记住上次窗口位置与大小。
			StateKey: "main",
			// 隐藏系统标题栏，但保留右上角的最小化/最大化/关闭三个窗口按钮；
			// 视图在 ui.View 里自绘一条标题栏（见 viewTitlebar），经 DragWindow
			// 拖动窗口、双击最大化/还原。
			TitleBarStyle: mygo.TitleBarHidden,
			// 自绘标题栏的高度（DIP）：40 比默认的 32（Windows 11 自己标题栏的
			// 高度）高一档，放得下 kura 的标识与名字，也给窗口按钮留出呼吸感；
			// 视图里用 max(c.TitleBar().Height, 40) 兜底，两边保持一致。
			TitleBarHeight: 40,
			// The window shows the interface MyGo draws, not a web page.
			Content: ui.View(app.View),
		})
		// 后台任务的磁盘 I/O 结果经 Window.Update 发布回主线程。
		app.SetWindow(win)
		// 接线「检查更新」按钮（左栏左下角）：只在这里注入生产实现——
		// 它依赖已挂载的更新插件（上面 selfupdate.Install()），测试里的
		// App 保持 nil，绝不触发真实更新检查。
		app.SetCheckUpdates(selfupdate.CheckForUpdates)
		// 应用菜单：关于 kura / 检查更新…（MyGo 更新窗口）/ 退出。
		// 必须在应用就绪后调用（updater.MenuItem 要读 App.Locale）。
		selfupdate.AppMenu()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
