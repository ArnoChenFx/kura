//go:build !windows

package store

import (
	"context"
	"os/exec"
)

// gitCommandContext 构造一条 git 命令（非 Windows 实现）：POSIX 上没有控制台
// 窗口问题，不加任何特殊属性。这是 store 里启动 git 的唯一入口（经
// gitOutput / runGit 调用），不要在别处直接 exec.Command("git", …)。
func gitCommandContext(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", args...)
	// 与 Windows 实现保持一致：默认不读 stdin，避免 git 等待输入。
	cmd.Stdin = nil
	return cmd
}
