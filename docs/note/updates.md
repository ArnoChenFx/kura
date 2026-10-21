# kura 自更新(里程碑 6)

kura 的应用自更新完全交给 MyGo 更新器(`plugins/updater` + `native.Plugin`), kura 自己不写任何下载、校验、替换代码，只决定后台检查的节奏和左栏蓝点（见「后台检查与左栏蓝点」）。本文说明各部分如何配合、用户看到什么、维护者怎么发一个带自更新的版本,以及哪些部分**尚未验证**。

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

kura 这边的接点(都在本里程碑落盘):

- `internal/selfupdate.Install()`——`main.go` 在 `mygo.App.Run()` **之前**调用, 挂载 `native.Plugin`(原生 UI 的更新窗口,kura 全程无 webview),并关掉插件自己的后台调度(`DisableAutomaticChecks`)。整个进程只能调一次。
- `internal/selfupdate.AppMenu()`——`mygo.App.WhenReady` 回调里(主线程)调用, 构建应用菜单:`kura` 子菜单 = 关于面板(role)+「关于 kura」(原生消息框, 显示 `mygo.json` 的版本号)+ 分隔线 +「检查更新…」(`updater.MenuItem()`)+ 分隔线 +「退出 kura」,另挂编辑/窗口菜单保住标准快捷键。「检查更新…」先熄灭左栏的蓝点，再打开更新窗口。
- `internal/selfupdate.Prepare` / `Background`(`watch.go`)——kura 自己的后台检查：`Prepare` 在 `mygo.App.Run()` 之前定下「自动检查更新」偏好（并迁移旧值），`Background.Start` 在窗口就绪后排定检查，只调 `mygo.Updater.Check` 问更新源，结果只用来亮灭蓝点。详见下文「后台检查与左栏蓝点」。
- `mygo.json` 的 `updates` 节与 `version` 字段——版本号的唯一来源是 `mygo.json` 的 `version`:`mygo build` 把它注入 `App.Version()`,更新器拿它和清单版本比较;「关于 kura」弹窗显示的也是它。注意 `internal/cli` 里另有一个 `Version` 常量(CLI 是独立二进制,不参与自更新),发版时需人工与 `mygo.json` 保持一致。

密钥现状:密钥对用 `go tool mygo keygen` 生成,keygen 的默认输出目录是 `%APPDATA%\mygo\update-keys\`(Windows;macOS/Linux 为 `~/Library/Application Support/mygo/update-keys` 与 `~/.config/mygo/update-keys`), 即 `mygo-update.key`(私钥)与 `mygo-update.pub`(公钥)。该目录在仓库之外, 不受 `.gitignore` 管辖;**密钥文件也一样不能复制进仓库**。公钥内容已抄进 `mygo.json` 的 `updates.publicKey`。仓库与全部 git 历史中没有任何私钥文件。 CI 侧应把**私钥内容**存为仓库 secret `MYGO_UPDATER_PRIVATE_KEY`(名字与 mygo 读取的环境变量一致),详见下文「CI 现状」。

> 版本记录:2026-10-13 用 `go tool mygo keygen` 重新生成过一次密钥对——此前 > `mygo.json` 里的公钥已找不到对应的私钥文件（keygen 的 `-o` 指向了临时目录， > 随目录删除而丢失），而当时尚未发布任何 Release、也没有已安装的应用持有旧公钥， > 换钥零代价。此后请固定使用 keygen 默认目录并做好备份。

## 用户看到什么

- **应用菜单「检查更新…」**:点开立即检查,窗口当场给出结果——已是最新、有新版本(附发布说明与「安装更新 / 稍后提醒我 / 跳过这个版本」)、或失败原因。手动检查会显示用户之前跳过的版本。打开窗口时左栏的蓝点同时熄灭。
- **每天自动检查一次**:kura 自己排定检查,启动约 10 秒后查一次,之后每 24 小时一次。查的时候不打开任何窗口:查到新版本只在左栏「检查更新」按钮右上角亮一个蓝点(悬停提示「kura 有新版本可用」),不弹窗、不抢焦点。「自动检查更新」在设置里,默认开,存在 `~/.kura/config.json` 的 `autoCheckUpdates`,随时可以关。细节见下文「后台检查与左栏蓝点」。
- **以后自动下载并安装更新**:在更新窗口里勾上它之后(偏好存在插件的 `updater.json`),插件接管后台检查:查到就静默下载、验签、替换,下次启动生效。这一档 kura 不再查、也不亮蓝点,因为用户已经选了静默安装。
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

## 后台检查与左栏蓝点

kura 的后台检查在 `internal/selfupdate/watch.go`（`Background`）。这一节说明它为什么这样做、三种模式、时间表、蓝点的亮与灭，以及它和插件的边界。

**为什么不用插件自己的调度。** 插件在后台查到新版本、又没有勾「以后自动下载并安装更新」时，会自己打开更新窗口；非用户发起的检查也一样，会抢焦点、闪任务栏。插件没有「只提示、不弹窗」的一档，而它的调度开关就是「自动检查更新」偏好本身。所以 `Install()` 挂载插件时就关掉它的调度（`DisableAutomaticChecks: true`），后台检查改由 kura 排定，只调用 MyGo 公开的 `mygo.Updater.Check` 问一次更新源。没有 fork，也没有改 MyGo 的源码。

**三种模式。** 由「自动检查更新」与插件里的「以后自动下载并安装更新」共同决定：

| 模式 | 条件 | 行为 |
| --- | --- | --- |
| 关 | 「自动检查更新」关 | kura 不查，插件的调度也关着。 |
| kura 检查（默认） | 「自动检查更新」开，且没勾「以后自动下载并安装更新」 | kura 按时间表查；查到且没被跳过的新版本就亮蓝点。插件的调度关着，它不会自己弹窗。 |
| 插件静默安装 | 「自动检查更新」开，且勾了「以后自动下载并安装更新」 | 插件自己查、静默下载、替换，下次启动生效。kura 不查，也不亮蓝点。 |

不能自我更新的构建（`mygo.Updater.Enabled()` 为假，即开发构建与包管理器安装）什么也不做：不排定检查，不写插件的文件，蓝点不亮。

**时间表。** 首次检查在启动约 10 秒后，与插件一致：先让窗口出来，不和启动抢资源。之后每 24 小时一次，间隔从「上一次成功检查」算起。检查失败（断网、清单取不到）后最晚一小时再试。单次检查 30 秒超时，任何时候只跑一个。系统睡醒后会重新核对是否到期。

**装完新版本会重新查一次。** `config.json` 的 `updateCheckedAt` 旁边还记着那条检查时间对应的应用版本（`updateCheckedVersion`，检查成功时从 `mygo.App.Version()` 取）。只有「记录的版本 == 当前运行版本」时才用这条时间推迟检查；版本变了，这条时间与插件的 `lastCheck` 都不再采信，启动约 10 秒后就查一次——刚装完新版本的用户不用再等一整天（原来手动检查并装上新版后，插件刚刷新的 `lastCheck` 会把后台检查推到 24 小时后，重启也不会查）。旧版本留下的 `config.json` 只有时间、没有版本，同样按「版本未知」处理，不采信，宁可多查一次。版本没变时行为不变：「上一次」仍取 `config.json` 的 `updateCheckedAt` 与插件的 `lastCheck` 中较新的一个，所以用户在更新窗口里手动检查成功，仍会推迟下一次后台检查。开发构建与 `go test` 里 `mygo.App.Version()` 是空串（版本未知），此时退回纯按时间排期，下一次检查最多被推到一天后，不会退化成每 10 秒查一次更新源。

**蓝点的亮与灭。**

- 查到新版本，且它不是用户跳过的版本：亮。
- 查到已是最新：灭。
- 检查失败：不动。一次网络抖动不该把用户已经看到的提示收回去。
- 点左栏「检查更新」或应用菜单「检查更新…」：立刻灭。打开窗口就是「用户知道了」。
- 「稍后提醒我」之后不会马上亮回来。下一次后台检查仍发现同一个版本时才亮，与插件一致。
- 关掉「自动检查更新」，或切到「插件静默安装」：灭。

**跳过的版本。** 用户在更新窗口里选「跳过这个版本」，插件把版本号记在 `updater.json` 的 `skippedVersion`。kura 读这个值：被跳过的那个版本不亮蓝点，比它更新的版本照常亮。

**偏好放在 `config.json`。** 「自动检查更新」原来存在插件的 `updater.json`。搬出来，是因为插件的 `automaticChecks` 同时是它的调度开关，而「kura 检查」模式下这个开关必须关着，否则插件会自己弹窗。偏好若还留在那里，界面上的开关和实际的调度就会互相打架。现在 `config.json` 的 `autoCheckUpdates` 是唯一真值（缺省为开）。插件的 `automaticChecks` 只是由它派生的调度开关（等于「偏好且勾了自动下载」），kura 只在派生值变化时才写插件的文件。

**迁移。** 升级后第一次启动，如果 `config.json` 还没有 `autoCheckUpdates`，`selfupdate.Prepare` 就把插件 `updater.json` 里的 `automaticChecks` 迁移进去：从未设过或为 `true` 则为开，为 `false` 则为关（见 `store.ResolveAutoCheck`）。迁移只做这一次，之后不再看插件的这一项，否则派生值会被当成用户的选择。`Prepare` 必须赶在插件启动之前完成，否则插件会按旧值排定自己的检查。`config.json` 损坏时偏好按缺省（开）处理，错误记入日志，文件原样保留，不覆盖用户手改的内容。

**它查到了什么。** `mygo.Updater.Check` 只读更新清单：版本号、说明、下载地址与签名字段都必须存在。它不下载安装包，Ed25519 签名在真正安装时才校验。所以蓝点表示「清单里有更新的版本」，不表示已经验过签。这与插件原来的更新窗口是同一层信息。

**「以后自动下载并安装更新」与「不弹窗」。** 两者不冲突。勾上之后，插件的后台检查是静默的：它下载、验签、替换，成功或失败都不打开窗口（插件只在用户发起的检查、或窗口已经打开时才显示）。kura 在这一档不查、不亮蓝点，因为用户已经选了静默安装。代价是这一档下查到新版本时界面上没有任何提示，要等下次启动才生效。想要「提示但不自动装」，应关掉「以后自动下载并安装更新」，这时蓝点会亮。

**线程与测试边界。** 检查在定时器自己的 goroutine 里做。结果经 `App.SetUpdateFound` → `publish` 投递回主线程（`Window.Update`，不阻塞）；点按钮与菜单项都在主线程上熄灭蓝点。`host` 接口把插件与更新源隔开：生产实现只做转发，测试注入假的 host 与假时钟，不挂插件、不联网。`updater.*` 与 `mygo.Updater.Check` 只出现在生产代码里。

**界面开关。** 设置弹窗里的「自动检查更新」即时生效，与界面语言一样。`App.SetAutoCheckAsync` 在后台写 `config.json`，然后调用 `Background.SetAutomaticChecks` 通知后台。连续切换只有最新一次写盘；写盘失败只在状态行报告，开关不回滚。

**蓝点的外观。** 颜色取自 `internal/ui/theme.go` 的调色板（浅色、深色各一个值），视图代码里不写十六进制。蓝点是按钮上的绝对定位子元素，不改变按钮的位置与尺寸，也不吃点击（`PassThrough`）。

## 已验证 / 未验证

**提交前的自动化验证：**

- `gofmt -l .` 为空；`go vet ./...`、`go tool mygo vet .`、`go tool mygo i18n check`（460 条消息）、`python3 tools/i18n-migrate/verify_catalog.py`（bad = 0）通过。
- `go build ./...`、`go test ./...`、`CGO_ENABLED=0 go build ./...`、`CGO_ENABLED=0 go test ./...`、`go tool mygo build -platform windows/amd64` 通过。
- `go test -race` 覆盖 `internal/selfupdate`、`internal/store`、`internal/ui`，无竞态报告。
- 测试覆盖：后台查到新版本只亮蓝点、不开窗口；点按钮与菜单打开窗口并熄灭；关掉「自动检查更新」后不再查；跳过的版本不亮；静默安装模式交给插件；不能自我更新的构建什么也不做；旧偏好迁移与损坏的配置；蓝点不改变按钮布局。（`internal/selfupdate/watch_test.go`、`internal/ui/update_dot_test.go`、`internal/ui/settings_autocheck_test.go`、`internal/store/autocheck_test.go`）

**Linux 真实打包运行（本次改动，2026-10-18）：**

- 打包时把 `mygo.json` 的 `version` 降到 0.1.9，更新源指向本机 HTTPS（自签证书，用 `SSL_CERT_FILE` 信任），清单声称是 0.1.10。清单与安装包都是测试用的假内容，签名是占位值，所以真正安装会在验签处失败。每次启动都用隔离的 `HOME` 与 `XDG_*` 目录，不碰真实的 `~/.kura`。
- 启动约 10 秒后，更新源收到一次请求，左栏「检查更新」按钮右上角出现蓝点，没有任何窗口打开。
- 点按钮：更新窗口出现，蓝点熄灭。
- 在更新窗口里点「稍后提醒我」：蓝点不回来。
- 点「跳过这个版本」，重启后（同一版本）不亮蓝点。
- 关掉「自动检查更新」后启动：打开开关之前没有任何请求。打开开关后约 10 秒再查，蓝点亮起。
- 勾选「以后自动下载并安装更新」后重启：插件自己在约 10 秒时查询，安装包 404 后静默失败，没有窗口，也没有蓝点。
- 未打包的开发构建：启动后没有任何请求。
- 对照：同一环境下旧版本（HEAD）启动约 10 秒后，插件自己弹出了更新窗口，没有任何点击。
- 截图与录屏是本次的一次性证据，未入库。

**更早的验证（本次改动之前）：**

- 2026-10-13，一份已安装副本从 0.1.0 升到 0.1.1（GitHub latest 清单 → 下载 → Ed25519 验签 → 替换 → 重启）。
- `mygo build -upload` 真实上传草稿 Release：v0.1.0 起的每次发布都由 CI 的发布工作流完成，v0.1.5 为最近一次。
- 上一版（0.1.9）的蓝点在 Windows 真机上看过（降到 0.1.7，线上为 0.1.8，启动约 10 秒后亮起）。本次把插件的调度关掉之后，Windows 上还没有重跑。

**未验证：**

- Windows 与 macOS 真机：本次改动只在 Linux 上运行过。`tools/capture-window.ps1` 只能在 Windows 上跑。
- 焦点与任务栏：Linux 上只验证了「没有窗口出现」。「不抢焦点、不闪任务栏」在 Windows 与 macOS 上未实测。代码路径上，后台结果只改一个布尔值并请求重绘，不调用任何窗口显示接口。
- 真实签名的下载、替换与重启：未验证，清单与安装包都是测试用的假内容。增量（delta）更新是否被采用也未确认。
- 包管理器安装（如 `.deb`）：只有单元测试（`Enabled` 为假），没有真实安装的副本。
- macOS 签名、公证与 dmg，以及 macOS / Linux 副本的自更新，仍未实测。
