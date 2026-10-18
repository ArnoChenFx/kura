package main

import (
	"log"
	"os"

	"github.com/ArnoChenFx/kura/internal/selfupdate"
	"github.com/ArnoChenFx/kura/internal/store"
	kuraui "github.com/ArnoChenFx/kura/internal/ui"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	// 更新插件必须在 App.Run 之前挂载：原生 UI 更新窗口（无 webview），语言跟随系统。
	// 插件自己的后台调度已关掉：后台检查由 selfupdate.Prepare / Start 负责，查到新版本
	// 只亮左栏的蓝点，从不弹窗。下载/签名校验/替换/重启全部由 MyGo 更新器完成，公钥与
	// 更新源在 mygo.json 的 updates 节。
	selfupdate.Install()

	// 默认配置：根目录 ~/.agents/skills，kura 数据目录 ~/.kura。
	// 目录不存在不致命：界面会显示错误并保持可用。
	cfg, err := store.DefaultConfig()
	if err != nil {
		log.Fatal(err)
	}
	// 「自动检查更新」偏好必须在 NewApp（读 config.json）与 App.Run（插件启动）之前定下：
	// 旧版本留在插件 updater.json 里的值在这里迁移进 config.json，并把插件的调度开关对齐。
	// config.json 损坏不致命，偏好按缺省（开）处理。
	background, err := selfupdate.Prepare(cfg)
	if err != nil {
		log.Println(err)
	}
	app := kuraui.NewApp(cfg)
	// 界面语言必须在 App.Run 之前定下来，否则第一帧就已经是错的语言。语言
	// 交给 MyGo：固定语言是 BCP 47 标签（zh-Hans / en），system 是 ""（跟随
	// 用户语言，系统语言变化时 MyGo 会自己重解析）。这也让菜单、文件对话框
	// 等 MyGo 自己的文字与 kura 的文案用同一种语言。
	app.SyncLang()
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
		// 自动发现本机已有的 agent 框架技能目录（Claude Code、Codex 等）并加入
		// 列表；每个路径只考虑一次，用户移除后不会再加回来。后台执行，不阻塞窗口。
		if home, err := os.UserHomeDir(); err == nil {
			app.DiscoverAgentRootsAsync(home)
		}
		// 接线「检查更新」按钮（左栏左下角）：只在这里注入生产实现——
		// 它依赖已挂载的更新插件（上面 selfupdate.Install()），测试里的
		// App 保持 nil，绝不触发真实更新检查。
		app.SetCheckUpdates(selfupdate.CheckForUpdates)
		// 「自动检查更新」切换时通知后台；偏好本身的读写在 config.json，由界面经 store 完成。
		app.SetAutoChecks(background.SetAutomaticChecks)
		// 后台检查：启动约 10 秒后查一次，之后每天一次。查到新版本只亮左栏的蓝点
		// （SetUpdateFound 能从后台 goroutine 调用），从不打开窗口。
		background.Start(app.SetUpdateFound)
		// 应用菜单：关于 kura / 检查更新…（MyGo 更新窗口）/ 退出。
		// 必须在应用就绪后调用（updater.MenuItem 要读 App.Locale）。
		// 从菜单打开更新窗口也熄灭蓝点，与点击按钮的行为一致。
		selfupdate.AppMenu(app.ClearUpdateFound)
		// kura 自己的菜单项文字（「关于 kura」「退出 kura」）不是 role 标签，
		// MyGo 不会重命名它们；语言一变就重建菜单（固定语言与跟随系统都走这里）。
		mygo.App.OnLocaleChanged(func(string) { selfupdate.AppMenu(app.ClearUpdateFound) })
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
