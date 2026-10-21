# MyGo 0.6.0 升级评估

本次从 0.5.5 升到 0.6.0，随 kura 0.1.17 发布；应用与独立 CLI 的版本号同步。依据：

- [0.6.0 发布说明](https://github.com/egoist/mygo/releases/tag/v0.6.0)
- [0.5.5 → 0.6.0 改动](https://github.com/egoist/mygo/compare/v0.5.5...v0.6.0)
- 固定版本的 `ui/doc.go`、`docs/ui/migration.md`、`ui/headless.go`、`ui/input.go`、`cmd/mygo/deb.go` 与 `docs/plugins/loading.md`。

## 已采用

| 更新 | kura 中的落点 |
| --- | --- |
| 局部重绘、增量布局、文本测量缓存、不可见窗口暂停绘制 | 升级后由框架自动提供；卡片网格、市场和全文页直接受益。未测量具体性能增幅。 |
| Linux 原生应用免除 WebKitGTK | `mygo.json` 设置 `linux.webview: false`；已检查生成的 `.deb` 依赖和 `install.sh`。GTK 仍然需要。 |
| 原生滚轮平滑滑动 | 删除 kura 自定义 `smoothScroll` / `wheelScroll`，所有滚动区、列表和网格采用 MyGo 默认步长和动画。搜索/切榜单继续通过 `ListState.ScrollTo` 回到顶部，由框架取消旧动画。 |
| loading 插件 | 扫描、技能操作、市场加载、安装与翻译的旧 Spinner 全部替换为官方 `loading.Ring`，共用主题尺寸/颜色及本地化任务名称，减少动效与动画帧调度由插件处理。 |
| 新测试输入区分触控板和滚轮 | `Tester.Scroll` 是即时的精确输入，`Tester.Wheel` 触发滚轮动画；验证完整步长、中间帧、反向累积、减少动效及搜索/切榜单打断动画。 |
| 控件 key 迁移 | 官方 `migrate-ui` 将安装候选区的 key 提前到 `Scroll(c.Key(...))`，让状态初始化前就确定身份。再次预览已无待迁移文件。 |
| 维护约定同步 | 更新 `.agents/skills/mygo-maintenance/SKILL.md` 和 `AGENTS.md` 中过时的输入写回时机；绑定值在构造函数返回前可读，回调在配置后、重建前执行。 |
| Windows 11 菜单样式、原生窗口菜单快捷键、更新窗口跟随语言 | 随依赖升级获得；保留现有菜单角色和 kura 自己的后台更新调度。Windows 原生交互尚未在本环境验证。 |

## 后续可选项

- loading 插件的其他动画样式：目前统一选用 Ring，其他 28 种可按产品需要再评估。
- `ListState` 的单元格/拖动多选：适合将来批量管理，但当前选择模型以单个技能副本为准，需要独立产品设计。
- `HideWindowButtons` / `MaximizeButton`：能自绘按钮并支持 Windows 11 贴靠菜单；当前系统按钮满足需要，接入前须验证 Windows 真机。
- 终端搜索、图片和镜像：kura 当前没有终端功能，不引入。
- WebView 快捷键、文档编辑及托盘功能：当前工作流未用到。`Shell.TrashItem` 的新语义也不替代 kura 自己的可恢复回收站。

## 验证证据

以下是第一阶段依赖升级在 Linux/amd64、Go 1.27.2、MyGo 0.6.0 下的原始输出。命令均以退出码 0 完成；无输出的检查保留为空。改用原生滚动和 loading 后的复验见本节末尾。

```text
$ gofmt -l .
$ go vet ./...
$ go tool mygo vet .
$ go tool mygo i18n check
  • 506 messages in en, zh-Hans
$ go test ./...
ok  	github.com/ArnoChenFx/kura	0.127s
?   	github.com/ArnoChenFx/kura/cmd/kura	[no test files]
ok  	github.com/ArnoChenFx/kura/internal/cli	0.354s
ok  	github.com/ArnoChenFx/kura/internal/i18n	0.025s
ok  	github.com/ArnoChenFx/kura/internal/market	(cached)
ok  	github.com/ArnoChenFx/kura/internal/selfupdate	0.219s
ok  	github.com/ArnoChenFx/kura/internal/store	6.588s
ok  	github.com/ArnoChenFx/kura/internal/ui	12.711s
?   	github.com/ArnoChenFx/kura/locales	[no test files]
?   	github.com/ArnoChenFx/kura/resources	[no test files]
$ CGO_ENABLED=0 go test ./...
ok  	github.com/ArnoChenFx/kura	0.177s
?   	github.com/ArnoChenFx/kura/cmd/kura	[no test files]
ok  	github.com/ArnoChenFx/kura/internal/cli	0.525s
ok  	github.com/ArnoChenFx/kura/internal/i18n	0.038s
ok  	github.com/ArnoChenFx/kura/internal/market	0.039s
ok  	github.com/ArnoChenFx/kura/internal/selfupdate	0.404s
ok  	github.com/ArnoChenFx/kura/internal/store	7.263s
ok  	github.com/ArnoChenFx/kura/internal/ui	12.918s
?   	github.com/ArnoChenFx/kura/locales	[no test files]
?   	github.com/ArnoChenFx/kura/resources	[no test files]
$ CGO_ENABLED=0 go build ./...
$ go tool mygo build -platform windows/amd64,linux/amd64
  ✓ Compiled windows/amd64  17.8s
  ✓ Created kura Setup 0.1.16 amd64.exe  9.0s
  ▲ Not signing an update: set MYGO_UPDATER_PRIVATE_KEY or updates.privateKey
  ✓ Compiled linux/amd64  16.6s
  ▲ Not signing an update: set MYGO_UPDATER_PRIVATE_KEY or updates.privateKey
  ✓ Built kura 0.1.16 in 47.6s
$ dpkg-deb -f build/linux-amd64/kura_0.1.16_amd64.deb Depends
libgtk-3-0 | libgtk-3-0t64
```

额外执行的现有渲染测试：

```text
$ KURA_UI_SHOTS=<隔离输出目录> go test ./internal/ui -run 'TestRenderDesignShots|TestRenderStateShots' -count=1
ok  	github.com/ArnoChenFx/kura/internal/ui	1.312s
```

检查了深浅色截图，以及 Xvfb 中启动的真实 GTK 原生窗口、技能选中和全文 tab 的 Markdown 内容，随后通过窗口关闭按钮退出；重启后也完成了交互。原生窗口用 Go build overlay 将 `main.go` 的默认数据路径及自动发现路径重定向到隔离目录，其他装配与视图保持原样；没有改写仓库入口或触碰真实技能目录。此检查不等同于 Windows PowerShell 真机截图，也未验证 GPU 性能。

CLI 使用临时目录实际完成 `add --local` → `list --json` → `disable` → `enable` → `remove` → `restore` → `doctor --json`；退出码全部为 0，最终 `problems` 为空。

首轮失败原因已定位：旧滚轮测试调用 `Tester.Scroll`，新版把它当作触控板输入，因此不会进入 kura 的滚轮动画。改用 `Tester.Wheel` 后原断言及全套测试通过，没有跳过或削弱断言。未设置输出目录的其他可选截图测试仍按仓库原设计跳过。

未验证：Windows/macOS 原生运行、Windows 11 菜单/贴靠、签名更新及发布链路。本地打包未提供更新私钥，只生成未签名的安装/便携产物，没有上传或发布。


### 原生滚动和 loading 接入后的复验

删除所有自定义滚轮拦截、0.45 步长缩放、140ms 动画和额外 reset 计数器；现在使用框架完整步长及默认动画节奏。测试保留虚拟列表、每个根目录的独立位置、主题切换、搜索/切榜单立即回顶及后续帧不漂移。新增指示器检查覆盖本地化名称、主题尺寸、动画变化、减少动效、消失和再次出现。

在 Xvfb 中用 101 个本地技能实际滚动网格，查看中间帧及结束帧，并检查浅色和深色主题下的 Ring。通过两帧指示器区域的像素差确认动画在变化，完成后不再显示。为了稳定观察短任务，视察用 Go overlay 除隔离数据路径外，还仅在视察副本中给后台任务增加 2 秒等待；仓库中的生产调度未修改。窗口随后通过关闭按钮退出。Windows/macOS 真机交互仍未验证。

原始检查输出：

```text
$ gofmt -l .
[exit 0]
$ go vet ./...
[exit 0]
$ go tool mygo vet .
[exit 0]
$ go tool mygo i18n check
  • 506 messages in en, zh-Hans
[exit 0]
$ go test ./...
ok  	github.com/ArnoChenFx/kura	(cached)
?   	github.com/ArnoChenFx/kura/build/linux-amd64	[no test files]
?   	github.com/ArnoChenFx/kura/build/windows-amd64	[no test files]
?   	github.com/ArnoChenFx/kura/cmd/kura	[no test files]
ok  	github.com/ArnoChenFx/kura/internal/cli	(cached)
ok  	github.com/ArnoChenFx/kura/internal/i18n	0.039s
ok  	github.com/ArnoChenFx/kura/internal/market	(cached)
ok  	github.com/ArnoChenFx/kura/internal/selfupdate	(cached)
ok  	github.com/ArnoChenFx/kura/internal/store	(cached)
ok  	github.com/ArnoChenFx/kura/internal/ui	12.643s
?   	github.com/ArnoChenFx/kura/locales	[no test files]
?   	github.com/ArnoChenFx/kura/resources	[no test files]
[exit 0]
$ CGO_ENABLED=0 go test ./...
ok  	github.com/ArnoChenFx/kura	0.191s
?   	github.com/ArnoChenFx/kura/build/linux-amd64	[no test files]
?   	github.com/ArnoChenFx/kura/build/windows-amd64	[no test files]
?   	github.com/ArnoChenFx/kura/cmd/kura	[no test files]
ok  	github.com/ArnoChenFx/kura/internal/cli	(cached)
ok  	github.com/ArnoChenFx/kura/internal/i18n	0.047s
ok  	github.com/ArnoChenFx/kura/internal/market	(cached)
ok  	github.com/ArnoChenFx/kura/internal/selfupdate	0.360s
ok  	github.com/ArnoChenFx/kura/internal/store	(cached)
ok  	github.com/ArnoChenFx/kura/internal/ui	13.084s
?   	github.com/ArnoChenFx/kura/locales	[no test files]
?   	github.com/ArnoChenFx/kura/resources	[no test files]
[exit 0]
$ CGO_ENABLED=0 go build ./...
[exit 0]
$ go tool mygo build -platform windows/amd64,linux/amd64

  MyGo!!!!! v0.6.0  build

  ○ Generating the accessors of the catalogs
  ✓ The accessors of locales are up to date  28ms
  ○ Compiling windows/amd64
  ✓ Compiled windows/amd64  1.7s
  ○ Creating kura Setup 0.1.16 amd64.exe
  ✓ Created kura Setup 0.1.16 amd64.exe  9.7s
  ▲ Not signing an update: set MYGO_UPDATER_PRIVATE_KEY or updates.privateKey
  • Copied 2 resources next to the executable
  ○ Compiling linux/amd64
  ✓ Compiled linux/amd64  1.7s
  ▲ Not signing an update: set MYGO_UPDATER_PRIVATE_KEY or updates.privateKey
  • Copied 2 resources next to the executable

  ✓ Built kura 0.1.16 in 17.7s

    build/windows-amd64/
      kura.exe                          17.3 MB
      kura Setup 0.1.16 amd64.exe        6.6 MB

    build/linux-amd64/
      kura                              16.6 MB
      kura.png                           975 KB
      kura.desktop                         1 KB
      kura_0.1.16_amd64.deb              8.2 MB
      kura-0.1.16-linux-amd64.tar.gz     8.8 MB
      install.sh                           9 KB

[exit 0]
```

```text
$ KURA_UI_SHOTS=<隔离输出目录> go test ./internal/ui -run 'TestRenderDesignShots|TestRenderStateShots' -count=1
ok  	github.com/ArnoChenFx/kura/internal/ui	1.489s
Native loading animation: PASS
```
