# MyGo 0.6.2 升级评估

本次从 0.6.0 升到 0.6.2，随 kura 0.1.19 发布；应用与独立 CLI 的版本号同步。依据：

- [0.6.2 发布说明](https://github.com/egoist/mygo/releases/tag/v0.6.2)
- [0.6.0 → 0.6.2 改动](https://github.com/egoist/mygo/compare/v0.6.0...v0.6.2)

## 变更内容

- Linux：拉丁词之间的中文标点改用该语言的字体绘制，避免落到拉丁字体上显得错位。
- 触控板双指捏合作为 `InputZoom` 上报给原生 UI，滚动容器可以把缩放与滚动交给同一套手势；`ui` 包的 `OnInput` 文档同步更新。
- Wayland：第二个实例启动时把激活令牌转给已在运行的第一个实例，窗口能正常获得焦点。
- 内部改动集中在平台层（`internal/darwin`、`internal/linux`、`internal/windows`、`internal/web`）与 `internal/text`，kura 不直接依赖这些包。

## 对 kura 的影响

kura 用到的六个包（`mygo`、`ui`、`ui/markdown`、`plugins/loading`、`plugins/updater`、`plugins/updater/native`）导出符号**无增无删**：用 `go doc -all` 逐包比对 `comm -13` / `comm -23`，两边都为空，唯一的差异是 `mygo.Version` 常量从 `0.6.0` 变成 `0.6.2`。因此没有业务代码改动，只改了 `go.mod` / `go.sum`。

捏合手势与中文标点的渲染改善都不需要 kura 主动接线：kura 的滚动区交给 MyGo 原生滚动容器，字体由 MyGo 按语言解析。

## 验证证据

以下命令在 `E:\Project\kura`（main，Go 1.27.2，Windows amd64）上执行，全部退出码 0。

```
$ go get github.com/egoist/mygo@v0.6.2
go: downloading github.com/egoist/mygo v0.6.2
go: upgraded github.com/egoist/mygo v0.6.0 => v0.6.2

$ go vet ./...
（无输出）

$ go tool mygo vet .
（无输出）

$ go tool mygo i18n check
  • 506 messages in en, zh-Hans

$ go test ./...
ok      github.com/ArnoChenFx/kura                        7.426s
ok      github.com/ArnoChenFx/kura/internal/cli          18.069s
ok      github.com/ArnoChenFx/kura/internal/i18n          0.069s
ok      github.com/ArnoChenFx/kura/internal/market        6.022s
ok      github.com/ArnoChenFx/kura/internal/selfupdate    6.613s
ok      github.com/ArnoChenFx/kura/internal/store        93.393s
ok      github.com/ArnoChenFx/kura/internal/ui           49.123s

$ CGO_ENABLED=0 go build ./...
（无输出）

$ go tool mygo build -platform windows/amd64
    build/windows-amd64/
      kura.exe                       17.3 MB
      kura Setup 0.1.18 amd64.exe     6.6 MB
```

gofmt 逐文件去 CR 后 `bad=0`（`core.autocrlf` 会让未改动文件也出现在 `gofmt -l .` 里，这是本机已知的假阳性）。

## 真实窗口

用 `mygo build` 出来的 Windows 产物、隔离 home（`USERPROFILE` / `APPDATA` / `LOCALAPPDATA` 指向临时目录）启动：窗口正常渲染标题栏、三栏与左栏底部的按钮，没有布局或绘制回归。截图是 `tools/` 下的临时产物，不入库。

## 未验证

- macOS / Linux 二进制只由 CI 构建，没有在对应系统上启动过；本机只能验 Windows。
- 捏合手势与 Wayland 激活令牌需要对应硬件与桌面环境，本机无法验证。
