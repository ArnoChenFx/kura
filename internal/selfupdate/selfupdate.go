// Package selfupdate 把 kura 应用自身的更新接入 MyGo 更新器（里程碑 6）。
//
// 下载、验签、替换、重启全部交给 MyGo（plugins/updater 与原生 UI 窗口
// plugins/updater/native），kura 不手写任何更新逻辑。kura 做的是三件事的装配，
// 以及一件它必须自己做的事——后台检查的节奏：
//
//   - Install：在 mygo.App.Run 之前挂载 native 更新插件，并关掉插件自己的后台调度
//     （DisableAutomaticChecks）。更新窗口只由用户主动打开：左栏「检查更新」按钮、
//     应用菜单「检查更新…」，都经 CheckForUpdates / AppMenu。
//   - Prepare / Background（watch.go）：kura 自己的后台检查。查到新版本只亮左栏的
//     小蓝点，从不弹窗；「自动检查更新」偏好存在 kura 的 config.json，插件的调度
//     开关由它派生，只在用户勾了「以后自动下载并安装更新」时才打开。
//   - AppMenu：应用就绪后构建应用菜单（关于、检查更新…、退出）。
//
// 更新签名的公钥在 mygo.json 的 updates.publicKey（keygen 生成的 mygo-update.pub
// 内容）；`mygo build` 会把它与更新源一起编译进应用，已安装的应用只接受对应私钥签出
// 的更新。私钥绝不进仓库：见 docs/note/updates.md。
//
// 线程约定：Install 与 Prepare 必须在主线程 App.Run 之前调用；Background.Start 与
// AppMenu 必须在应用就绪后（App.WhenReady 的回调，主线程）调用——updater.MenuItem()
// 内部读取 App.Locale，经 onMainValue 等待主线程事件循环，应用未运行时（如 go test）
// 会永久阻塞，因此测试用替身条目构建菜单模板（appMenu），绝不直接调 updater.MenuItem。
package selfupdate

import (
	"github.com/ArnoChenFx/kura/locales"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
)

// Install 挂载原生 UI 更新插件，且关掉它自己的后台调度。必须在主线程 mygo.App.Run()
// 之前调用，整个进程只调一次（mygo.Use 对同名插件重复挂载会 panic）。
//
// 后台检查由 Background 决定何时进行、是否亮蓝点；插件只在用户勾了「以后自动下载并
// 安装更新」时才由 Background 打开它的调度（那时它静默安装，不弹窗）。
func Install() {
	mygo.Use(native.New(updater.Options{DisableAutomaticChecks: true}))
}

// CheckForUpdates 打开 MyGo 的原生更新窗口，并检查 kura 应用自身的更新。界面只依赖
// 这一个函数，不直接 import updater 插件。只是转发：立即返回，检查在后台进行，窗口
// 由插件打开。必须已挂载更新插件（Install），未挂载时调用会 panic，所以测试里不调它。
func CheckForUpdates() {
	updater.CheckForUpdates()
}

// appMenuLabel 是应用菜单的名字；Windows/Linux 的菜单栏第一项也用它。
// 它是产品名，不随界面语言变，所以不进目录。
const appMenuLabel = "kura"

// AppMenu 构建 kura 的应用菜单并交给应用。必须在应用就绪后调用——正确位置是
// mygo.App.WhenReady 的回调（主线程）：updater.MenuItem() 要读 App.Locale，事件循环
// 未运行时会永久阻塞。onOpen 在用户从菜单打开更新窗口之前调用，在主线程运行，用来熄灭
// 左栏的蓝点，与点击按钮的行为一致。
func AppMenu(onOpen func()) {
	mygo.App.SetMenu(appMenu(openingWith(updater.MenuItem(), onOpen)))
}

// openingWith 让菜单项在打开更新窗口之前先调用 onOpen，它原有的 Click 照常执行。
func openingWith(item *mygo.MenuItem, onOpen func()) *mygo.MenuItem {
	check := item.Click
	item.Click = func(mi *mygo.MenuItem, w *mygo.Window) {
		if onOpen != nil {
			onOpen()
		}
		if check != nil {
			check(mi, w)
		}
	}
	return item
}

// appMenu 返回应用菜单模板。updateItem 是「检查更新」条目：生产环境 AppMenu 传入
// updater.MenuItem()（文字按用户语言本地化，中文为「检查更新…」，触发 MyGo 更新窗口）；
// 测试注入替身。
//
// 与技能窗口里技能级的「检查更新/全部检查」（预览技能上游差异）语义不同：这一项更新
// kura 应用本身，见 docs/note/updates.md 的界面文案一节。
func appMenu(updateItem *mygo.MenuItem) *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: appMenuLabel, Submenu: []*mygo.MenuItem{
			// macOS：系统标准关于面板（mac-only role，Windows/Linux 自动隐藏）。
			{Role: mygo.RoleAbout},
			// 三端一致的「关于」：原生消息框显示名字与版本。
			{Label: locales.SelfupdateMenuAbout(), Click: func(*mygo.MenuItem, *mygo.Window) { showAbout() }},
			mygo.Separator(),
			// 「检查更新…」：打开 MyGo 原生更新窗口（检查 → 发布说明 → 下载进度 → 替换 → 重启）。
			updateItem,
			mygo.Separator(),
			// 退出：保留 RoleQuit 的默认行为，只换文字。
			{Role: mygo.RoleQuit, Label: locales.SelfupdateMenuQuit()},
		}},
		// 编辑/窗口菜单保住 Ctrl/Cmd+C、V 等标准快捷键与窗口管理。
		{Role: mygo.RoleEditMenu},
		{Role: mygo.RoleWindowMenu},
	})
}

// appVersion 返回应用版本，用于「关于」弹窗。打包应用（mygo dev/build）返回
// mygo.json 的 version；普通 go run/go test 时为空串，弹窗里省略。
func appVersion() string {
	return mygo.App.Version()
}

// showAbout 弹出「关于 kura」原生消息框。只在主线程调用（菜单 Click 由 MyGo 保证在
// 主线程）；Dialog.Message 阻塞到用户关闭为止，菜单回调里阻塞弹窗是官方示例的做法
// （examples/native/main.go 的 Confirm 同款）。
//
// 正文由两整段拼成（正文 + 更新说明），版本行按需插在中间：每段都是完整句子，在目录里
// 各自成条（见 locales/），不做词序相关的拼接。
func showAbout() {
	msg := locales.SelfupdateAboutBody()
	if v := appVersion(); v != "" {
		msg += locales.SelfupdateAboutVersion(v)
	}
	msg += locales.SelfupdateAboutUpdater()
	_, _ = mygo.Dialog.Message(mygo.MessageOptions{
		Title:   locales.SelfupdateAboutTitle(),
		Message: msg,
		Buttons: []string{locales.SelfupdateAboutOk()},
	})
}
