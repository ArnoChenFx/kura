# MyGo 0.8.3 升级评估

本次从 0.6.3 升到 **0.8.3**（跨 0.6.4、0.7.0～0.7.4、0.8.0～0.8.3），随 kura 0.1.22 发布。依据：[0.8.3 发布说明](https://github.com/egoist/mygo/releases/tag/v0.8.3) 与 `go doc -all` 逐包对比。

## 导出 API 变化

对 kura 用到的六个包（`mygo`、`ui`、`ui/markdown`、`plugins/loading`、`plugins/updater`、`plugins/updater/native`）在 0.6.3 与 0.8.3 之间逐条比对 `go doc -all` 的导出声明，kura 一个都没有用到下面这些被删/改的地方：

- `mygo`：删掉 `Cookies`/`CookiesModule`（页面 cookie 改成 `Page` 上的方法）与 `Tray.OnClick`（换成 `OnLeftClick`/`OnRightClick`）。kura 没有托盘，也没有读 cookie。
- `ui`：`Element.ScrollIntoView()` 改成 `ScrollIntoView(options ...ScrollIntoViewOptions)`（可选参数，旧调用照样编译）；`TextStyle` 的若干字段改名（`Lang`、`Kerning` 等）。kura 两者都没用。
- `plugins/loading`、`plugins/updater`、`plugins/updater/native`、`ui/markdown`：导出符号无增无删。

业务代码因此**一行没改**，只有两处测试断言随行为变化调整（见下）。

## 行为变化里与 kura 有关的三处

- **滚轮反向立刻起作用**（0.7.0 起）：一格反向滚轮不再被「还在路上的行程」吸收，残留行程就地取消、从当前位置往回走。`internal/ui/scroll_test.go` 原来断言「反向正好回到 200」，现在断言「反向立刻往回滚」——上游的 `TestWheelReverseAtEnd` 就是这条语义。kura 自己没改滚动逻辑，用户看到的是滚到底再往回拨时立刻响应。
- **`Tester.Click` 校验命中目标**（0.8.0 起）：`tt.Click("字段标题")` 不再接受「文字中心落在另一个控件上」的点击。`TestSettingsModalSavesFromInputs` 改成点标题靠左 40 DIP 的位置（真人点标题左侧的空白也会聚焦输入框），断言不变。
- **`mygo build` 的产物命名**（0.7.0 起）：Windows 安装器变成 `kura-setup-<version>-<arch>.exe`，macOS 变成 `kura-<version>-<arch>.dmg`（原来是 `kura Setup <version> amd64.exe`、`kura <version> <arch>.dmg`）；还多出 `install.ps1`、`SHA256SUMS`。已安装的应用照旧自更新。发布工作流的 `-upload` 按 `-setup-` 标记挑安装器，不需要改；补传裸 `kura.exe` 的那一步也不受影响。README 与 `docs/note/ci.md` 里的资产名已同步。

其余变化（WebView、终端、编辑器、`plugins/automation`、音频等）kura 都用不到：kura 是原生 `ui` 应用，没有 webview，也不挂终端/编辑器/自动化插件。

## 验证（本机 Windows，go 1.27.3）

```
$ go get github.com/egoist/mygo@v0.8.3
go: upgraded go 1.27.2 => 1.27.3
go: upgraded github.com/egoist/mygo v0.6.3 => v0.8.3
```

`go.mod` 只有两行变化（go 指令与 require），`go.sum` 多两条。

```
$ gofmt -l .   # 逐文件去 CR 后判断
bad=0
$ go vet ./...
（空）
$ go tool mygo vet .
（空）
$ go tool mygo i18n check
  • 506 messages in en, zh-Hans
$ go test ./...
ok   github.com/ArnoChenFx/kura            0.1s
ok   github.com/ArnoChenFx/kura/internal/cli      11.8s
ok   github.com/ArnoChenFx/kura/internal/i18n      0.1s
ok   github.com/ArnoChenFx/kura/internal/market    0.1s
ok   github.com/ArnoChenFx/kura/internal/selfupdate 0.4s
ok   github.com/ArnoChenFx/kura/internal/store    87.0s
ok   github.com/ArnoChenFx/kura/internal/ui       48.3s
$ CGO_ENABLED=0 go test ./...
（同上全绿，ui 63.7s、store 109.5s）
$ go test -race ./internal/selfupdate/ ./internal/store/
ok   internal/selfupdate 4.7s
ok   internal/store      126.0s
$ CGO_ENABLED=0 go build ./...
（成功）
$ go tool mygo build -platform windows/amd64
  ✓ Built kura 0.1.21 in 22.0s
    build/windows-amd64/
      kura.exe                       18.9 MB
      kura-setup-0.1.21-amd64.exe     7.0 MB
```

## 真机窗口

隔离 home（`USERPROFILE`/`HOME`/`APPDATA`/`LOCALAPPDATA`/`TEMP` 全部指向临时目录，`config.json` 里只放临时技能目录，`autoCheckUpdates: false`），用 0.8.3 打出来的 exe 启动后 `tools/capture-window.ps1` 截图：

- 深色 + 卡片网格：标题栏图标、三栏布局、卡片、开关、状态行都正常。
- 浅色 + 卡片网格：皮肤、开关、边框正常。
- 深色 + 列表视图 + 英文界面：列表行、筛选计数、「No metadata」等文案正常。

真实 `~/.kura/config.json` 在这次验证里没有被写入（验证前后都核对过：roots 仍是 16 条、`theme` 仍是 `dark`）。

## 未验证

- macOS / Linux 的窗口：本机只有 Windows，跨平台产物要等 CI 构建。
- 触控板捏合（0.7.2 起页面捏合默认关闭，kura 用的是原生 `ui`，不受影响，但没有硬件可验）。
- 自更新链路要在真正发版后才会走到，这次只验到「打包成功、清单与签名由 CI 出具」。
