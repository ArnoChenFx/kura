package selfupdate

// catalog.go — selfupdate 包的用户可见文案（中文 / 英文两套）。
//
// 规则见 internal/i18n 的包注释：中文取值与改造前的界面文字**逐字一致**，
// 英文取值自然、简短。这里只有应用菜单与「关于 kura」弹窗的文案。
//
// 注意：菜单在 AppMenu() 里构建，取值时必须已经 i18n.Set（main.go 在 App.Run
// 之前定语言），所以这些文案用 i18n.T 现取，不做包级常量——常量会在 init 阶段
// 就把语言写死。

import (
	"github.com/ArnoChenFx/kura/internal/i18n"
)

// 中文文案：与改造前的界面文字逐字一致。
var catalogZH = map[string]string{
	"selfupdate.menu.about": "关于 kura",
	"selfupdate.menu.quit":  "退出 kura",

	"selfupdate.about.title": "关于 kura",
	"selfupdate.about.body":  "kura — agent 技能管理器（github.com/ArnoChenFx/kura）",
	// 版本行与更新说明各自是一整段，拼在 body 后面（见 showAbout）。
	"selfupdate.about.version": "\n版本 %s",
	"selfupdate.about.updater": "\n\n应用更新由 MyGo 更新器提供：菜单里的「检查更新…」检查新版本。",
	"selfupdate.about.ok":      "好",
}

// 英文文案：key 与中文一一对应（含同样的占位符）。
var catalogEN = map[string]string{
	"selfupdate.menu.about": "About kura",
	"selfupdate.menu.quit":  "Quit kura",

	"selfupdate.about.title":   "About kura",
	"selfupdate.about.body":    "kura — an agent skill manager (github.com/ArnoChenFx/kura)",
	"selfupdate.about.version": "\nversion %s",
	"selfupdate.about.updater": "\n\nApp updates come from the MyGo updater: the \"Check for Updates...\" menu item looks for new versions.",
	"selfupdate.about.ok":      "OK",
}

func init() { i18n.RegisterZH(catalogZH); i18n.RegisterEN(catalogEN) }
