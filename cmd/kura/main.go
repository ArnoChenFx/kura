// cmd/kura 是 kura 的无界面命令行入口（里程碑 3）。
//
// 这个文件只做一件事：把 os.Args 接到 internal/cli.Run，并把返回的退出码
// 变成进程退出码。所有逻辑都在 internal/cli（可测试），本文件保持零逻辑。
// 注意：与根目录 main.go（MyGo 窗口版）互不影响，二者各自构建独立二进制。
package main

import (
	"os"

	"github.com/ArnoChenFx/kura/internal/cli"
)

func main() {
	// os.Args[0] 是程序名，cli.Run 只关心子命令及之后的参数。
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
