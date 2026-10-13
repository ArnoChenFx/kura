//go:build windows

package store

import (
	"context"
	"os/exec"
	"syscall"
)

// gitCommandContext 构造一条用于 GUI 进程的 git 命令（Windows 实现）。这是
// store 里启动 git 的唯一入口（经 gitOutput / runGit 调用），不要在别处直接
// exec.Command("git", …)，否则会把命令行黑窗重新带回来。
//
// kura 的窗口版是 GUI 子系统程序（PE Subsystem = 2），而 git 是控制台程序：
// 从 GUI 进程直接启动它会闪出/弹出一个黑色控制台窗口。CREATE_NO_WINDOW
// （0x08000000）让子进程不附着任何控制台，黑窗就不再出现；HideWindow 是
// 同一问题的另一道开关，两个一起设。
//
// 注意 CREATE_NO_WINDOW 与 CREATE_DETACHED 不同：前者子进程照常继承 stdin/
// stdout/stderr 管道，Output 仍能拿到输出，只是没有控制台。
func gitCommandContext(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", args...)
	// 默认不读 stdin：git 在某些场景（凭证提示、分页器）会等待输入，
	// GUI 进程里没有控制台可等，nil 让它直接读到 EOF。
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	return cmd
}
