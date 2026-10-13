// Package selfupdate 把 kura 应用自身的更新接入 MyGo 更新器（里程碑 6）。
//
// 职责只有两件事，下载/替换/重启全部交给 MyGo（plugins/updater + 原生 UI
// 窗口 plugins/updater/native），kura 不手写任何更新逻辑：
//
//   - Install：在 mygo.App.Run 之前挂载 native.Plugin（原生 UI 的更新窗口，
//     与 kura 其余界面一致，不需要 webview）。自动检查默认每天一次，语言
//     跟随用户系统（Options.Language 留空 = App.Locale），偏好存放在
//     updater.json，由用户在更新窗口里控制。
//   - AppMenu：应用就绪后构建应用菜单：kura 子菜单里有「关于 kura」、
//     「检查更新…」（updater.MenuItem，打开 MyGo 更新窗口）与退出项。
//
// 更新签名的公钥在 mygo.json 的 updates.publicKey（keygen 生成的
// mygo-update.pub 内容）；`mygo build` 会把它与更新源一起编译进应用，
// 已安装的应用只接受对应私钥签出的更新。私钥绝不进仓库：见
// docs/note/updates.md。
//
// 线程约定：Install 必须在主线程 App.Run 之前调用；AppMenu 必须在应用
// 就绪后（App.WhenReady 的回调，主线程）调用——updater.MenuItem() 内部
// 读取 App.Locale()，经 onMainValue 等待主线程事件循环，应用未运行时
// （如 go test）会永久阻塞，因此测试用替身条目构建模板（appMenu），
// 绝不直接调 updater.MenuItem。
package selfupdate

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
)

// Install 挂载原生 UI 更新插件。必须在主线程 mygo.App.Run() 之前调用，
// 且整个进程只调一次（mygo.Use 对同名插件重复挂载会 panic）。
//
// 更新窗口是 native UI（package native），与 kura 其余界面一致，不需要
// webview。插件用默认选项：每天自动检查一次；语言跟随用户系统；
// DisableAutomaticChecks 不设（用户可在更新窗口里关闭自动检查）。
// 开发构建（未打包、无签名更新清单）时 MyGo 自己禁用检查并在窗口里
// 说明原因，kura 不做额外判断。
func Install() {
	mygo.Use(native.Plugin)
}

// CheckForUpdates 打开 MyGo 的原生更新窗口、检查 kura 应用自身的更新，
// 供界面与菜单之外的新入口（如左下角的「检查更新」按钮）复用：UI 包只
// 依赖这一个函数，不直接 import updater 插件。只是转发：立即返回，检查
// 在后台进行，窗口由插件打开；必须已挂载更新插件（Install，main.go 里
// App.Run 之前），未挂载时调用会 panic，所以测试里不调它。
func CheckForUpdates() {
	updater.CheckForUpdates()
}

// appMenuLabel 是应用菜单的名字；Windows/Linux 的菜单栏第一项也用它。
const appMenuLabel = "kura"

// aboutLabel 是三端一致的「关于」菜单项文字。
const aboutLabel = "关于 kura"

// quitLabel 是退出菜单项的文字。带 Role 的条目其 Click 与行为被保留，
// 只覆盖显示文字（RoleQuit 的默认文字是英文 "Quit"）。
const quitLabel = "退出 kura"

// AppMenu 构建 kura 的应用菜单并交给应用。必须在应用就绪后调用——正确
// 位置是 mygo.App.WhenReady 的回调（主线程）：updater.MenuItem() 要读
// App.Locale，事件循环未运行时会永久阻塞。
func AppMenu() {
	mygo.App.SetMenu(appMenu(updater.MenuItem()))
}

// appMenu 返回应用菜单模板。updateItem 是「检查更新」条目：生产环境
// AppMenu 传入 updater.MenuItem()（文字按用户语言本地化，zh-CN 为
// 「检查更新…」，触发 MyGo 更新窗口）；测试注入替身。
//
// 与技能窗口里技能级「检查更新/全部检查」（预览技能上游差异）语义不同：
// 这一项更新 kura 应用本身，见 docs/note/updates.md 的界面文案一节。
func appMenu(updateItem *mygo.MenuItem) *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: appMenuLabel, Submenu: []*mygo.MenuItem{
			// macOS：系统标准关于面板（mac-only role，Windows/Linux 自动隐藏）。
			{Role: mygo.RoleAbout},
			// 三端一致的「关于」：原生消息框显示名字与版本。
			{Label: aboutLabel, Click: func(*mygo.MenuItem, *mygo.Window) { showAbout() }},
			mygo.Separator(),
			// 「检查更新…」：打开 MyGo 原生更新窗口（检查 → 发布说明 →
			// 下载进度 → 替换 → 重启）。
			updateItem,
			mygo.Separator(),
			// 退出：保留 RoleQuit 的默认行为，只换文字。
			{Role: mygo.RoleQuit, Label: quitLabel},
		}},
		// 编辑/窗口菜单保住 Ctrl/Cmd+C、V 等标准快捷键与窗口管理。
		{Role: mygo.RoleEditMenu},
		{Role: mygo.RoleWindowMenu},
	})
}

// appVersion 返回应用版本，用于「关于」弹窗。打包应用（mygo dev/build）
// 返回 mygo.json 的 version；普通 go run/go test 时为空串，弹窗里省略。
func appVersion() string {
	return mygo.App.Version()
}

// showAbout 弹出「关于 kura」原生消息框。只在主线程调用（菜单 Click 由
// MyGo 保证在主线程）；Dialog.Message 阻塞到用户关闭为止，菜单回调里
// 阻塞弹窗是官方示例的做法（examples/native/main.go 的 Confirm 同款）。
func showAbout() {
	msg := "kura — agent 技能管理器（github.com/ArnoChenFx/kura）"
	if v := appVersion(); v != "" {
		msg += "\n版本 " + v
	}
	msg += "\n\n应用更新由 MyGo 更新器提供：菜单里的「检查更新…」检查新版本。"
	_, _ = mygo.Dialog.Message(mygo.MessageOptions{
		Title:   "关于 kura",
		Message: msg,
		Buttons: []string{"好"},
	})
}
