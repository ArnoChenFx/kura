# kura 自更新(里程碑 6)

kura 的应用自更新完全交给 MyGo 更新器(`plugins/updater` + `native.Plugin`), kura 自己不写任何下载、校验、替换代码。本文说明各部分如何配合、用户看到什么、维护者怎么发一个带自更新的版本,以及哪些部分**尚未验证**。

机制参考:`go list -m -f '{{.Dir}}' github.com/egoist/mygo` 下的 `docs/updates.md`、`docs/plugins/updater.md` 与 `cmd/mygo/updates.go`(v0.4.8)。

## 各部分如何配合

```
mygo keygen                 生成 Ed25519 密钥对
  ├─ mygo-update.pub(公钥)  → 内容抄进 mygo.json 的 updates.publicKey(可提交)
  └─ mygo-update.key(私钥)  → 绝不进仓库:安全保存原件 + CI secret
                              MYGO_UPDATER_PRIVATE_KEY(存私钥内容本身)

mygo.json                   updates.publicKey + updates.github = "ArnoChenFx/kura"
mygo build                  把公钥与更新源清单地址用 ldflags 编进应用
  (MYGO_UPDATER_PRIVATE_KEY 时) 同时对每平台更新包签名,生成
                            kura-<版本>-<目标>.tar.gz、delta 与 update-<目标>.json 清单
git tag vX.Y.Z              发布标签,前缀固定 "v"(updates.tagPrefix 默认 v)
GitHub Release              清单从 .../releases/latest/download/update-<目标>.json 提供
已安装的应用                 用内置公钥验签 → 下载 → 替换 → 重启
```

kura 这边的三个接点(都在本里程碑落盘):

- `internal/selfupdate.Install()`——`main.go` 在 `mygo.App.Run()` **之前**调用, 挂载 `native.Plugin`(原生 UI 的更新窗口,kura 全程无 webview)。整个进程只能调一次。
- `internal/selfupdate.AppMenu()`——`mygo.App.WhenReady` 回调里(主线程)调用, 构建应用菜单:`kura` 子菜单 = 关于面板(role)+「关于 kura」(原生消息框, 显示 `mygo.json` 的版本号)+ 分隔线 +「检查更新…」(`updater.MenuItem()`)+ 分隔线 +「退出 kura」,另挂编辑/窗口菜单保住标准快捷键。
- `mygo.json` 的 `updates` 节与 `version` 字段——版本号的唯一来源是 `mygo.json` 的 `version`:`mygo build` 把它注入 `App.Version()`,更新器拿它和清单版本比较;「关于 kura」弹窗显示的也是它。注意 `internal/cli` 里另有一个 `Version` 常量(CLI 是独立二进制,不参与自更新),发版时需人工与 `mygo.json` 保持一致。

密钥现状:密钥对用 `go tool mygo keygen` 生成,keygen 的默认输出目录是 `%APPDATA%\mygo\update-keys\`(Windows;macOS/Linux 为 `~/Library/Application Support/mygo/update-keys` 与 `~/.config/mygo/update-keys`), 即 `mygo-update.key`(私钥)与 `mygo-update.pub`(公钥)。该目录在仓库之外, 不受 `.gitignore` 管辖;**密钥文件也一样不能复制进仓库**。公钥内容已抄进 `mygo.json` 的 `updates.publicKey`。仓库与全部 git 历史中没有任何私钥文件。 CI 侧应把**私钥内容**存为仓库 secret `MYGO_UPDATER_PRIVATE_KEY`(名字与 mygo 读取的环境变量一致),详见下文「CI 现状」。

> 版本记录:2026-10-13 用 `go tool mygo keygen` 重新生成过一次密钥对——此前 > `mygo.json` 里的公钥已找不到对应的私钥文件（keygen 的 `-o` 指向了临时目录， > 随目录删除而丢失），而当时尚未发布任何 Release、也没有已安装的应用持有旧公钥， > 换钥零代价。此后请固定使用 keygen 默认目录并做好备份。

## 用户看到什么

- **应用菜单「检查更新…」**:点开立即检查,窗口当场给出结果——已是最新、有新版本(附发布说明与「安装更新 / 稍后提醒我 / 跳过这个版本」)、或失败原因。手动检查会显示用户之前跳过的版本。
- **每天自动检查一次**:插件默认间隔一天,启动约 10 秒后做当日首次检查。勾选窗口里的「以后自动下载并安装更新」后,后台检查会静默安装,下次启动生效。偏好保存在用户数据目录的 `updater.json`,用户可随时关闭自动检查。
- **原生更新窗口**(中文文案随系统语言,MyGo 内置翻译):发现新版本 → 发布说明(取自 `CHANGELOG.md` 对应版本小节)→ 下载进度条 → **Ed25519 签名校验**(对照编译进应用的公钥,不通过即拒装)→ 替换应用 → 「立即重新启动」或下次启动时生效。
- **不能自更新的情形**:开发构建(`go run`/未打包)与包管理器安装的副本不会后台检查;窗口里会解释原因(如「开发版本不会自行更新。」)。 Windows 上可自更新的是 NSIS 安装器的按用户安装 (`%LOCALAPPDATA%\Programs`),macOS 是 /Applications 或 ~/Applications, Linux 是 install.sh 装到用户目录的副本。
- **文案消歧**:技能详情面板里的「检查更新」是**技能级**操作(预览该技能上游差异);应用菜单里的「检查更新…」是**应用级**自更新。kura 的窗口在空列表时不出现「检查更新」字样,避免混淆。

## 维护者发布一个带自更新的版本(确切步骤)

前提:`gh` CLI 可用(或设 `GH_TOKEN`,且 token 需 `repo` 权限才能写 secret 与 Release);私钥在手(keygen 默认目录 `%APPDATA%\mygo\update-keys\mygo-update.key`, 或密码管理器);`CHANGELOG.md` 存在。

1. **改版本号**:把 `mygo.json` 的 `version` 改成新版本,如 `0.1.0` → `0.2.0`; 同步改 `internal/cli` 的 `Version` 常量(人工保持一致)。
2. **写发布说明**:在 `CHANGELOG.md` 加 `## 0.2.0` 小节。**这一步是强制的**: `CHANGELOG.md` 存在时,`mygo build` 要求其中有所发布版本的 `## <版本号>` 小节,否则签名构建直接报错(`%s has no notes for %s: add a "## %s" section`)。
3. **提交并打标签**:`git commit` 后 `git tag v0.2.0`(标签 = `v` + mygo.json 版本)并推送。
4. **构建并上传**(每平台一次;macOS 的签名与 dmg 须在 macOS 上执行):

```sh
   MYGO_UPDATER_PRIVATE_KEY="$(cat "$APPDATA/mygo/update-keys/mygo-update.key")" \
     go tool mygo build -platform windows/amd64 -upload
```

   `-upload` 把安装器、签名的更新压缩包、delta 与 `update-<目标>.json` 清单上传到 GitHub 仓库 `ArnoChenFx/kura` 上标签为 `v0.2.0` 的**草稿 Release** (不存在则创建;重复上传 `--clobber` 覆盖)。**清单最后上传**,保证应用永远不会读到指向缺失文件的清单。不给私钥时构建照常,但**不生成更新文件** 并打印警告——那样发出的版本无法被自更新发现。
5. **发布**:全部平台就绪后 `gh release edit v0.2.0 --repo ArnoChenFx/kura --draft=false`。发布后该 Release 成为 latest,已安装应用随后的每日检查即可看到 `0.2.0`。
6. **收尾确认**:Releases 页面确认 `update-<目标>.json` 与安装器齐全; 可选:`go tool mygo keygen -force` 换钥前先想清楚——旧应用只认旧公钥。

构建 delta:签名构建会自动下载已发布清单列出的最近 3 个版本做增量包 (`updates.deltas` 可调,`0` 关闭);首个版本或取不到已发布清单时跳过 delta 并说明原因,不影响发布。

## CI 现状(已接入自更新)

`.github/workflows/release.yml` 已接入自更新。它做的事:校验 tag 与 `mygo.json` 版本一致 → 从 `CHANGELOG.md` 提取本版本小节作为发布说明(缺该小节即失败) → 确认 secret `MYGO_UPDATER_PRIVATE_KEY` 已配置(缺即失败) → 建草稿 Release → 每平台 `go tool mygo build -platform <目标> -upload`(安装器
+ 签名更新包 + delta + 清单,清单最后上传) → Windows 行补传裸 `kura.exe` → `publish` job 把草稿转正。要做的准备工作只有一件:

1. 在 GitHub 仓库设置里添加 secret `MYGO_UPDATER_PRIVATE_KEY`,内容是 `mygo-update.key` 的全部内容(单行 base64,不是文件路径)。

其余细节见 `docs/note/ci.md`。

## 设置里的「自动检查更新」开关与左栏的新版本蓝点

kura 自己没有更新逻辑，这一节只说明界面上的两个入口怎么接回插件。

- **「自动检查更新」偏好存在插件的 `updater.json`**（用户数据目录），不是 kura 的 `config.json`：读 `updater.AutomaticChecks()`、写 `updater.SetAutomaticChecks(on)`。这样只有一个真值源，默认值天然是「勾上」（插件默认开），也跟插件自己的调度一致（关掉后插件不再后台检查）。kura 侧只经 `internal/selfupdate` 的转发读写，两个钩子由 `main.go` 在窗口就绪后注入（`app.SetAutoChecks`）；`internal/ui` 不接插件，只拿一个 `func(bool)` 回调。
- **后台查到新版本时，左栏左下角「检查更新」按钮的右上角有一个蓝色小圆点**（颜色取自 `internal/ui/theme.go` 的调色板，浅色/深色各一个值，视图代码里不写十六进制），悬停提示也换成「kura 有新版本可用」。蓝点是绝对定位的子元素，不改变按钮的位置与尺寸，也不吃点击（`PassThrough`）。
- **为什么会多一次查询**：插件没有「发现新版本」的回调，也没有「后台发现新版但只提示、不弹窗」的模式。它只在自己完成一次成功检查时经 `updater.OnChange` 通知 `LastCheck` 变了（也会在用户动开关、回答更新窗口时通知）。所以 kura 在每次 `OnChange` 后自己再调一次 `mygo.Updater.Check`（`selfupdate.WatchUpdateFound`，30 秒超时），才知道「是不是真有新版本」。
  - 用 `LastCheck()` 去重：时间没变就跳过，免得用户动一下开关就白跑一个请求。
  - 一次只跑一个查询；查询失败（断网、清单取不到）**不动**蓝点状态——不因为一次抖动把用户已经看到的提示收回去；开发构建 `mygo.Updater.Check` 返回 `ErrUpdatesDisabled`，也算失败。
  - 查询在后台 goroutine 里跑，结果经 `App.SetUpdateFound` → `Window.Update` 回主线程。
- **蓝点与插件自己的更新窗口是两回事**：后台检查发现新版本且没勾自动安装时，插件仍会自己弹一个「不抢焦点」的更新窗口（现有行为，本任务没有改）。蓝点只是说「有新版本」，两者同时出现是正常的。
- **点「检查更新」按钮会清掉蓝点**：用户已经在看更新窗口了。后台查到「没有新版本」时蓝点也清掉。
## 已验证 / 未验证

**已验证**:

- `go build ./...`、`go vet ./...`、`go tool mygo vet .`、 `CGO_ENABLED=0 go build ./...`、`go test ./...` 全部通过;
- `go tool mygo build -platform windows/amd64` 成功——挂上更新插件后应用仍能正常打包;
- `mygo.json` 只含公钥(`grep -c PRIVATE mygo.json` 为 0),git 历史无任何私钥文件;私钥在仓库外的 keygen 默认目录;
- 测试覆盖:插件挂载冒烟、应用菜单结构(含更新项占位与 Click 透传)、主窗口 Tester 照常构建、开发构建下版本号为空。
- 「自动检查更新」开关与蓝点:设置弹窗里的开关默认勾上、点击后经假钩子写入且界面跟着变(浅色/深色各一遍);蓝点的像素级测试(出现、消失、不改变按钮矩形、点在蓝点上仍触发按钮、浅色/深色);`internal/selfupdate` 的探测逻辑(去重、失败不动状态、一次只跑一个查询)、四个转发函数在插件未挂载时都 panic(另起子进程验证)、开发构建下查询失败; 见 `internal/ui/settings_autocheck_test.go`、`internal/ui/update_dot_test.go`、`internal/selfupdate/watch_test.go`。
- 真实窗口:用 `tools/capture-window.ps1`（需要时可以加 `-LargestWindow`，它按面积挑进程可见顶层窗口里最大的那个，避开插件弹的更新窗口）启动真机窗口:设置弹窗里能看到「自动检查更新」且默认勾上，无障碍树里那个开关的 `ToggleState` 是 `On`;点一下开关，插件的 `updater.json` 里出现 `{"automaticChecks":false}`;把 `mygo.json` 版本降到 0.1.7 打包后真机启动（隔离的 `HOME`/`USERPROFILE`/`APPDATA`/`LOCALAPPDATA`/`TEMP`），启动约 10 秒后插件自动检查发现线上 0.1.8，主窗口左栏更新按钮右上角真的出现蓝点（截图的像素是调色板里的 `#5aa9f0`）。截图是本任务的一次性证据，未入库。
- 自更新端到端:一份已安装副本从 0.1.0 升到 0.1.1(GitHub latest 清单 → 下载 → Ed25519 验签 → 替换 → 重启),2026-10-13 实测;
- `mygo build -upload` 真实上传草稿 Release:v0.1.0 起的每次发布都由 CI 的发布工作流完成(draft-release → 各平台 build → publish),v0.1.5 为最近一次。

**未验证**:

- delta 增量更新是否真的被采用(资产已产出,本机无法确认更新走 delta 还是整包);
- 「自动检查更新」开关与蓝点的真实联网链路只验到两层:开发构建下 `mygo.Updater.Check` 返回 `ErrUpdatesDisabled`(查询失败、不动状态),以及把 `mygo.json` 版本降到 0.1.7 打包后插件真的查到线上 0.1.8、蓝点亮起。用真实公钥验签、下载与替换没有重跑(同上文的自更新端到端);
- 蓝点只在 Windows 真机上看过;macOS / Linux 未验。
- macOS 签名 / 公证与 dmg(需 macOS 环境),macOS / Linux 副本的自更新也未实测。
