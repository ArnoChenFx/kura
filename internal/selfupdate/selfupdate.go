// Package selfupdate 把 kura 应用自身的更新接入 MyGo 更新器：
//
//   - main.go 在 App.Run 之前调用 Install，把 native.Plugin（原生 UI 的
//     更新窗口，无 webview）挂进应用；
//   - 应用就绪后（App.WhenReady）构建应用菜单：kura 子菜单里有「关于 kura」、
//     「检查更新…」（来自 updater.MenuItem，触发 MyGo 更新窗口）、分隔线与
//     退出项，另挂编辑/窗口菜单保住复制粘贴等标准快捷键；
//   - 「关于 kura」弹出原生消息框，显示 mygo.json 里的版本号。
//
// 更新的签名公钥来自 mygo.json 的 updates.publicKey（keygen 生成的
// mygo-update.pub 内容），应用编译进 updates 清单的校验由 MyGo 在
// `mygo build` 时完成：私钥（MYGO_UPDATER_PRIVATE_KEY）只在发布时用，
// 绝不能进仓库。
//
// 线程约定：Install 必须在主线程 App.Run 之前调用；AppMenu 必须在应用
// 就绪后（App.WhenReady 的回调，主线程）调用——updater.MenuItem() 内部
// 读取 App.Locale()，会等待主线程事件循环，应用未运行时会永久阻塞，
// 因此绝不能在 App.Run 之前或测试进程里调用（测试请用 MenuTemplate）。
package selfupdate

import (
	"os"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
)

// PublicKey 是 updates 签名密钥对的公钥（keygen 产出的 mygo-update.pub
// 内容）。MyGo 的 `mygo build` 会把 mygo.json updates.publicKey 对应的
// 私钥签名编进发布产物；已安装的应用只接受用它签出的更新。
// 私钥本身绝不进仓库：见 docs/note/updates.md 的维护者发布流程。
//
// 占位值 "PLACEHOLDER" 表示密钥尚未生成；keygen 生成后替换为真实公钥。
const PublicKey = "bfsNsslVHmWkHqKFOactV75/JN1mpP6Osq5QYmL34b8="

// HasRealKey 报告 PublicKey 是否已是真实公钥（非维护者手工占位）。
// 发布构建前的 CI 可以用它拒绝带占位公钥的打包。
func HasRealKey() bool {
	return PublicKey != "" && !strings.HasPrefix(PublicKey, "PLACEHOLDER")
}

// ReadKey 读 updates 签名密钥文件（keygen 产出的 mygo-update.key 或
// mygo-update.pub）：去掉首尾空白（含末尾换行）后返回内容。keygen 写出
// 的是单行 base64，整段 TrimSpace 即可。空文件返回空串与 nil。
// 供发布脚本或未来的偏好界面读取；仓库里永远只有 .pub。
func ReadKey(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// Install 挂载原生 UI 更新插件。必须在主线程 mygo.App.Run() 之前调用；
// 插件的自动检查（每天一次，行为由用户的 updater.json 偏好控制）在应用
// 就绪后由插件自己调度，kura 不再重复实现。
//
// 窗口是 native UI（package native），和 kura 其余界面一致，不需要
// webview。开发构建（未打包、无签名更新清单）时 MyGo 自己禁用检查并在
// 窗口里说明原因，kura 不做额外判断。
func Install() {
	mygo.Use(native.Plugin)
}

// AppMenu 构建 kura 的应用菜单并交给应用：kura 子菜单（关于、检查更新、
// 退出）+ 编辑/窗口菜单（macOS 标准快捷键依赖它们）。
//
// 必须在应用就绪后调用——正确位置是 mygo.App.WhenReady 的回调（主线程）；
// 在 App.Run 之前或测试进程里调用会因 App.Locale 等待主线程而永久阻塞。
func AppMenu() {
	mygo.App.SetMenu(AppMenuTemplate())
}

// appMenuLabel 是应用菜单的名字；Windows/Linux 的菜单栏第一项也用它。
const appMenuLabel = "kura"

// aboutLabel 是「关于」菜单项的文字。macOS 的 RoleAbout 自带系统样式，
// 但它在非 macOS 上是隐藏条目，所以这里用普通菜单项 + 原生消息框，
// 三端行为一致。
const aboutLabel = "关于 kura"

// quitLabel 是退出菜单项的文字。RoleQuit 的 Click 会被保留，因此这里
// 的自定义文字（而不是默认的英文 "Quit"）在应用退出时同样生效。
const quitLabel = "退出 kura"

// AppMenuTemplate 返回应用菜单模板（测试可检查的结构；不触达主线程）。
// 更新项用 updater.MenuItem()，因此同样只能在应用就绪后的主线程调用。
func AppMenuTemplate() *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: appMenuLabel, Submenu: []*mygo.MenuItem{
			// macOS 上显示系统关于面板；非 macOS 上隐藏（mac-only role）。
			{Role: mygo.RoleAbout},
			// 三端一致的「关于」：原生消息框显示名字与版本。
			{Label: aboutLabel, Click: func(*mygo.MenuItem, *mygo.Window) { showAbout() }},
			mygo.Separator(),
			// 「检查更新…」：打开 MyGo 原生更新窗口（检查/发布说明/下载
			// 进度/替换重启）。文字由插件按用户语言本地化，zh-CN 为
			// 「检查更新…」。与技能窗口里 per-skill 的「检查更新」（检查
			// 技能上游）语义不同：这一项更新 kura 应用本身。
			updater.MenuItem(),
			mygo.Separator(),
			// 退出：保留 RoleQuit 的默认行为，只换文字。
			{Role: mygo.RoleQuit, Label: quitLabel},
		}},
		// 编辑/窗口菜单保住 Cmd/Ctrl+C、V 等标准快捷键与窗口管理。
		{Role: mygo.RoleEditMenu},
		{Role: mygo.RoleWindowMenu},
	})
}

// appVersion 返回应用版本，用于「关于」弹窗。打包应用（mygo dev/build）
// 读 mygo.json 的 version；普通 go run/go test 时为空串，弹窗里省略。
func appVersion() string {
	return mygo.App.Version()
}

// showAbout 弹出「关于 kura」原生消息框。只在主线程调用（菜单 Click 由
// MyGo 保证在主线程）；Dialog.Message 阻塞到用户关闭为止，对菜单项这是
// 官方示例的做法（examples/native/main.go 的 Confirm 同款）。
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
