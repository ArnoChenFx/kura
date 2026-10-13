//go:build windows

package store

import (
	"errors"
	"syscall"
)

// 跨设备判定（Windows 实现）。
//
// Windows 上 os.Rename 跨卷失败返回 WinError 17 ERROR_NOT_SAME_DEVICE，
// 以 syscall.Errno 形式包裹在 *LinkError 里。标准库 syscall 没有导出这个
// 常量（golang.org/x/sys/windows 里有，但为此引依赖不值），这里按 Win32
// 错误码表自定义。
//
// 特别注意：syscall.EXDEV 在 Windows 不是这个含义（它属于一组为 POSIX 兼容
// 「发明」的值），本机实测 errors.Is(err, syscall.EXDEV) 对真实的跨卷错误
// 返回 false，绝不能用 EXDEV 判断。
const errNotSameDevice syscall.Errno = 17 // ERROR_NOT_SAME_DEVICE

// isCrossDevice 判断 err 是否为「源与目标不在同一卷」的移动失败。
func isCrossDevice(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == errNotSameDevice
	}
	return false
}
