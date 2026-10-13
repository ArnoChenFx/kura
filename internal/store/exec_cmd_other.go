//go:build !windows

package store

import "os/exec"

// gitCommand 构造一条 git 命令（非 Windows 实现）：POSIX 上没有控制台窗口
// 问题，不加任何特殊属性，保持与直接 exec.Command 相同的行为。
func gitCommand(args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	// 与 Windows 实现保持一致：不读 stdin，避免 git 等待输入。
	cmd.Stdin = nil
	return cmd
}
