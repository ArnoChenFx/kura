# MyGo 0.6.3 升级评估

本次从 0.6.2 升到 0.6.3，随 kura 0.1.20 发布；应用与独立 CLI 的版本号同步。依据：

- [0.6.3 发布说明](https://github.com/egoist/mygo/releases/tag/v0.6.3)
- [0.6.2 → 0.6.3 改动](https://github.com/egoist/mygo/compare/v0.6.2...v0.6.3)

## 变更内容

与本项目相关的几条：

- Windows：隐藏标题栏并启用窗口材质时，最大化窗口不再露出系统的标题栏按钮（kura 正是 `TitleBarHidden`，这条直接受益）。
- `ui`：隐藏的滚动条轨道也放行点击，不再吃掉指针事件。
- `ui`：表格最后一列与剩余空间共享时不再有可拖拽的边。
- Windows：文本框独占按键，编辑菜单里的 Ctrl+V 只粘贴一次。
- 文本层：大段文字的内联变换每段只查找一次，并把大段落缓存在活动布局旁边，滚动更省。
- Windows：Direct3D 在画面没有变化时按显示器刷新率节流；终端清屏时同步清 ConPTY 控制台。
- 其余为 WebKit 内容规则（`mygo.CompileContentRules`）、Metal 字形图集采样、任务对话框栈等 web/mac 侧改动。

## 对 kura 的影响

kura 用到的六个包（`mygo`、`ui`、`ui/markdown`、`plugins/loading`、`plugins/updater`、`plugins/updater/native`）用 `go doc -all` 逐包比对 `comm -13` / `comm -23`：**只有 `mygo` 包新增了 `ContentRules` 类型与 `CompileContentRules` 函数**（WebKit 内容拦截规则，原生 UI 用不到），其余五个包无增无删。因此没有业务代码改动，只改了 `go.mod` / `go.sum`，并因新依赖多了一条 `golang.org/x/net v0.59.0 // indirect`。

## 验证证据

以下命令在 `E:\Project\kura`（main，Go 1.27.3，Windows amd64）上执行，全部退出码 0。

```
$ go get github.com/egoist/mygo@v0.6.3
go: downloading github.com/egoist/mygo v0.6.3
go: downloading golang.org/x/net v0.59.0
go: upgraded github.com/egoist/mygo v0.6.2 => v0.6.3

$ go vet ./...
（无输出）

$ go tool mygo vet .
（无输出）

$ go tool mygo i18n check
  • 506 messages in en, zh-Hans

$ go test ./...
ok      github.com/ArnoChenFx/kura                        7.616s
ok      github.com/ArnoChenFx/kura/internal/cli          17.637s
ok      github.com/ArnoChenFx/kura/internal/i18n          0.060s
ok      github.com/ArnoChenFx/kura/internal/market        6.014s
ok      github.com/ArnoChenFx/kura/internal/selfupdate    6.731s
ok      github.com/ArnoChenFx/kura/internal/store        89.772s
ok      github.com/ArnoChenFx/kura/internal/ui           47.851s

$ CGO_ENABLED=0 go build ./...
（无输出）

$ go tool mygo build -platform windows/amd64
    build/windows-amd64/
      kura.exe                       17.3 MB
      kura Setup 0.1.19 amd64.exe     6.6 MB
```

gofmt 逐文件去 CR 后 `bad=0`（`core.autocrlf` 会让未改动文件也出现在 `gofmt -l .` 里，本机已知的假阳性）。

## 真实窗口

用 0.6.3 打出来的 Windows 产物（`go version -m` 显示 `mygo v0.6.3`）、隔离 home（`USERPROFILE` / `APPDATA` / `LOCALAPPDATA` 指向临时目录）启动：标题栏、三栏布局、左栏目录与底部按钮、空状态文案渲染正常，没有布局或绘制回归。

## 未验证

- macOS / Linux 二进制只由 CI 构建，没有在对应系统上启动过；本机只能验 Windows。
- 0.6.3 那几条 Windows 修复（最大化窗口的 DWM 标题按钮、隐藏滚动条轨道的点击放行、文本框 Ctrl+V 只粘一次）没有逐条构造场景复现，只确认界面整体正常。
- 捏合手势、Wayland 激活令牌、Metal 图集采样等平台特性本机无法验证。
