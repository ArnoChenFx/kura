//go:build !windows

package store

import (
	"errors"
	"syscall"
)

// 跨设备判定（非 Windows 实现）：POSIX 上 os.Rename 跨文件系统失败返回
// EXDEV（"invalid cross-device link"），它作为可比较的错误值导出。
func isCrossDevice(err error) bool {
	return errors.Is(err, syscall.EXDEV)
}
