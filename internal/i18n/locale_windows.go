//go:build windows

package i18n

import (
	"syscall"
	"unsafe"
)

// procGetUserDefaultLocaleName 是 Windows 的
// GetUserDefaultLocaleName（kernel32）：返回当前用户的界面语言，形如
// "zh-CN"、"en-US"。数值取自 Windows SDK 的 LOCALE_NAME_MAX_LENGTH。
var procGetUserDefaultLocaleName = syscall.NewLazyDLL("kernel32.dll").
	NewProc("GetUserDefaultLocaleName")

// systemLocale 返回系统界面语言（如 "zh-CN"）。拿不到时返回空串，调用方
// 会按 DefaultLang 处理（英文）。
//
// 这是 CLI（无 MyGo 应用）用的探测；有窗口时优先用 mygo.App.Locale()，
// 它与这里同源但由 MyGo 处理各平台差异。
func systemLocale() string {
	buf := make([]uint16, 85)
	n, _, _ := procGetUserDefaultLocaleName.Call(
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}
