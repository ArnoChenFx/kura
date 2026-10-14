//go:build !windows

package i18n

import (
	"os"
	"strings"
)

// systemLocale 返回系统 locale（如 "zh-CN"、"en_US.UTF-8"）。拿到空串、
// "C" 或 "POSIX" 时返回空串，调用方按 Default（英文）处理。
//
// macOS 与 Linux 上有窗口时用 mygo.App.Locale()（它读的是系统 API）；
// 这条路径给 CLI 用。
func systemLocale() string {
	for _, env := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := strings.TrimSpace(os.Getenv(env))
		if v != "" && v != "C" && v != "POSIX" {
			return v
		}
	}
	return ""
}
