// Package selfupdate 把 kura 应用自身的更新接入 MyGo 更新器（里程碑 6）。
//
// 下载/替换/重启全部交给 MyGo（plugins/updater + 原生 UI
// 窗口 plugins/updater/native），kura 不手写任何更新逻辑：
//
//   - Install：在 mygo.App.Run 之前挂载 native.Plugin（原生 UI 的更新窗口，
//     与 kura 其余界面一致，不需要 webview）。自动检查默认每天一次，语言
//     跟随用户系统（Options.Language 留空 = App.Locale），偏好存放在
//     updater.json，由用户在更新窗口里控制。
//   - AppMenu：应用就绪后构建应用菜单：kura 子菜单里有「关于 kura」、
//     「检查更新…」（updater.MenuItem，打开 MyGo 更新窗口）与退出项。
//   - 「自动检查更新」偏好与「发现新版本」的信号：AutomaticChecks /
//     SetAutomaticChecks 转发插件的偏好（存在插件的 updater.json，不是
//     kura 的 config.json），WatchUpdateFound 在插件每次完成检查后自己再
//     查一次、告诉界面要不要点亮左栏的蓝点（见 WatchUpdateFound 的注释）。
//     这些函数都依赖插件已挂载，只由 main.go 注入给界面，内部里不直接调。
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
	"context"
	"sync"
	"time"

	"github.com/ArnoChenFx/kura/locales"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
)

// updateCheckTimeout 是 kura 自己那次查询（checkForUpdate）的总时限：与
// 插件的检查一样，宁可放弃也不能挂着——失败只是不点亮蓝点。
const updateCheckTimeout = 30 * time.Second

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

// AutomaticChecks 报告更新器是否在后台自动检查（保存在插件的
// updater.json 里，默认开）。界面里的「自动检查更新」开关读它，读到的值
// 就是唯一真值源。未挂载更新插件时调用会 panic（与 CheckForUpdates 一样），
// 所以只由 main.go 经界面钩子接线。
func AutomaticChecks() bool {
	return updater.AutomaticChecks()
}

// SetAutomaticChecks 开关后台自动检查（写插件的 updater.json）。关掉后
// 插件自己不再调度检查，kura 不额外做任何事。未挂载更新插件时调用会 panic。
func SetAutomaticChecks(on bool) {
	updater.SetAutomaticChecks(on)
}

// WatchUpdateFound 在插件每次完成一次检查后告诉调用方「这次检查是否发现
// 了新版本」，found 由调用方负责回主线程。返回的函数取消监听。
//
// 为什么需要它：插件自己没有「发现新版本」的回调，只在自己完成一次成功检查
// 时经 OnChange 通知了 LastCheck 变了（失败不通知，因为没有东西可报告）。
// 因此在没有窗口的路径上，kura 只能自己再问一次
// mygo.Updater.Check 才知道到底有没有新版本——这就是「为了点亮一个小蓝点
// 多跑一次请求」的代价，换来的是不必改插件、也不跟插件自己的更新窗口耦合。
//
// 为什么要去重：OnChange 不只在检查成功时触发，用户动「自动检查更新」开关
// （SetAutomaticChecks）、回答插件的更新窗口（勾选自动下载、跳过某版本）也会
// 触发；这些事件里 LastCheck 没变，照样查一次就是白跑请求。所以记下上次
// 用来判断的 LastCheck，时间没变就跳过。
//
// 为什么失败时不动状态：查询失败（断网、GitHub 抖动、清单暂时取不到）不说明
// 「没有新版本」，把蓝点抹掉就是把已经看到的提示收回去。失败时什么都不做，
// 上次的结果保持不变。
//
// 一次只跑一个查询：上一次还没回来时再来一个事件直接跳过，不排一堆并发请求。
// 整个回调在后台 goroutine 里跑，不阻塞 OnChange 所在的主线程。
// 未挂载更新插件时调用会 panic（OnChange 一样）。
func WatchUpdateFound(found func(available bool)) (off func()) {
	return watchUpdateFound(found, updater.OnChange, updater.LastCheck, checkForUpdate)
}

// watchUpdateFound 是 WatchUpdateFound 的实现：插件的那几个函数都做成参数，
// 包内测试可以注入假实现，绝不挂插件、绝不联网。
func watchUpdateFound(found func(available bool), onchange func(func()) func(),
	lastCheck func() time.Time, check func(context.Context) (bool, error)) func() {
	var mu sync.Mutex
	// seen 是已经问出答案的那次检查的 LastCheck（还在路上的那次先记进 asked，
	// 失败后退回 seen，下次还有机会重问）；running 是有查询在路上。
	var seen, asked time.Time
	var running bool
	return onchange(func() {
		last := lastCheck()
		mu.Lock()
		if running || last.Equal(seen) {
			mu.Unlock()
			return
		}
		running, asked = true, last
		mu.Unlock()
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
			defer cancel()
			available, err := check(ctx)
			mu.Lock()
			running = false
			if err != nil {
				// 失败：这次检查没有答案，不记进 seen，下次事件还会再问；
				// 也绝不把蓝点抹掉——断网不说明没有新版本。
				seen = time.Time{}
			} else {
				seen = asked
			}
			mu.Unlock()
			if err != nil {
				return
			}
			found(available)
		}()
	})
}

// checkForUpdate 是 kura 自己的一次查询：有新版本为真，无新版本为假。
// 只是转发 mygo.Updater.Check（插件内部用的就是它）。开发构建
// （未打包、没有签名更新清单）返回 mygo.ErrUpdatesDisabled，此时
// 插件本来就不会后台检查，这个查询也到不了；真到了也算失败，不动蓝点。
func checkForUpdate(ctx context.Context) (bool, error) {
	up, err := mygo.Updater.Check(ctx)
	if err != nil {
		return false, err
	}
	return up != nil, nil
}

// appMenuLabel 是应用菜单的名字；Windows/Linux 的菜单栏第一项也用它。
// 它是产品名，不随界面语言变，所以不进目录。
const appMenuLabel = "kura"

// aboutLabel / quitLabel 是菜单项文字，随界面语言变（见 locales 的
// selfupdateMenuAbout / selfupdateMenuQuit）。带 Role 的退出条目其 Click
// 与行为被保留，只覆盖显示文字（RoleQuit 的默认文字是英文 "Quit"）。

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
			{Label: locales.SelfupdateMenuAbout(), Click: func(*mygo.MenuItem, *mygo.Window) { showAbout() }},
			mygo.Separator(),
			// 「检查更新…」：打开 MyGo 原生更新窗口（检查 → 发布说明 →
			// 下载进度 → 替换 → 重启）。
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

// appVersion 返回应用版本，用于「关于」弹窗。打包应用（mygo dev/build）
// 返回 mygo.json 的 version；普通 go run/go test 时为空串，弹窗里省略。
func appVersion() string {
	return mygo.App.Version()
}

// showAbout 弹出「关于 kura」原生消息框。只在主线程调用（菜单 Click 由
// MyGo 保证在主线程）；Dialog.Message 阻塞到用户关闭为止，菜单回调里
// 阻塞弹窗是官方示例的做法（examples/native/main.go 的 Confirm 同款）。
//
// 正文由两整段拼成（正文 + 更新说明），版本行按需插在中间：每段都是完整
// 句子，在目录里各自成条（见 locales/），不做词序相关的拼接。
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
