# kura 自更新(里程碑 6)

kura 的应用自更新完全交给 MyGo 更新器(`plugins/updater` + `native.Plugin`),
kura 自己不写任何下载、校验、替换代码。本文说明各部分如何配合、用户看到什么、
维护者怎么发一个带自更新的版本,以及哪些部分**尚未验证**。

机制参考:`go list -m -f '{{.Dir}}' github.com/egoist/mygo` 下的
`docs/updates.md`、`docs/plugins/updater.md` 与 `cmd/mygo/updates.go`(v0.4.6)。

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

- `internal/selfupdate.Install()`——`main.go` 在 `mygo.App.Run()` **之前**调用,
  挂载 `native.Plugin`(原生 UI 的更新窗口,kura 全程无 webview)。整个进程
  只能调一次。
- `internal/selfupdate.AppMenu()`——`mygo.App.WhenReady` 回调里(主线程)调用,
  构建应用菜单:`kura` 子菜单 = 关于面板(role)+「关于 kura」(原生消息框,
  显示 `mygo.json` 的版本号)+ 分隔线 +「检查更新…」(`updater.MenuItem()`)+
  分隔线 +「退出 kura」,另挂编辑/窗口菜单保住标准快捷键。
- `mygo.json` 的 `updates` 节与 `version` 字段——版本号的唯一来源是
  `mygo.json` 的 `version`:`mygo build` 把它注入 `App.Version()`,更新器拿它
  和清单版本比较;「关于 kura」弹窗显示的也是它。注意 `internal/cli` 里另有
  一个 `Version` 常量(CLI 是独立二进制,不参与自更新),发版时需人工与
  `mygo.json` 保持一致。

密钥现状:密钥对用 `go tool mygo keygen` 生成,keygen 的默认输出目录是
`%APPDATA%\mygo\update-keys\`(Windows;macOS/Linux 为
`~/Library/Application Support/mygo/update-keys` 与 `~/.config/mygo/update-keys`),
即 `mygo-update.key`(私钥)与 `mygo-update.pub`(公钥)。该目录在仓库之外,
不受 `.gitignore` 管辖;**密钥文件也一样不能复制进仓库**。公钥内容已抄进
`mygo.json` 的 `updates.publicKey`。仓库与全部 git 历史中没有任何私钥文件。
CI 侧应把**私钥内容**存为仓库 secret `MYGO_UPDATER_PRIVATE_KEY`(名字与
mygo 读取的环境变量一致),详见下文「CI 现状」。

> 版本记录:2026-10-13 用 `go tool mygo keygen` 重新生成过一次密钥对——此前
> `mygo.json` 里的公钥已找不到对应的私钥文件（keygen 的 `-o` 指向了临时目录，
> 随目录删除而丢失），而当时尚未发布任何 Release、也没有已安装的应用持有旧公钥，
> 换钥零代价。此后请固定使用 keygen 默认目录并做好备份。

## 用户看到什么

- **应用菜单「检查更新…」**:点开立即检查,窗口当场给出结果——已是最新、
  有新版本(附发布说明与「安装更新 / 稍后提醒我 / 跳过这个版本」)、或失败
  原因。手动检查会显示用户之前跳过的版本。
- **每天自动检查一次**:插件默认间隔一天,启动约 10 秒后做当日首次检查。
  勾选窗口里的「以后自动下载并安装更新」后,后台检查会静默安装,下次启动
  生效。偏好保存在用户数据目录的 `updater.json`,用户可随时关闭自动检查。
- **原生更新窗口**(中文文案随系统语言,MyGo 内置翻译):发现新版本 →
  发布说明(取自 `CHANGELOG.md` 对应版本小节)→ 下载进度条 → **Ed25519
  签名校验**(对照编译进应用的公钥,不通过即拒装)→ 替换应用 →
  「立即重新启动」或下次启动时生效。
- **不能自更新的情形**:开发构建(`go run`/未打包)与包管理器安装的副本
  不会后台检查;窗口里会解释原因(如「开发版本不会自行更新。」)。
  Windows 上可自更新的是 NSIS 安装器的按用户安装
  (`%LOCALAPPDATA%\Programs`),macOS 是 /Applications 或 ~/Applications,
  Linux 是 install.sh 装到用户目录的副本。
- **文案消歧**:技能详情面板里的「检查更新」是**技能级**操作(预览该技能
  上游差异);应用菜单里的「检查更新…」是**应用级**自更新。kura 的窗口在
  空列表时不出现「检查更新」字样,避免混淆。

## 维护者发布一个带自更新的版本(确切步骤)

前提:`gh` CLI 可用(或设 `GH_TOKEN`,且 token 需 `repo` 权限才能写 secret
与 Release);私钥在手(keygen 默认目录 `%APPDATA%\mygo\update-keys\mygo-update.key`,
或密码管理器);`CHANGELOG.md` 存在。

1. **改版本号**:把 `mygo.json` 的 `version` 改成新版本,如 `0.1.0` → `0.2.0`;
   同步改 `internal/cli` 的 `Version` 常量(人工保持一致)。
2. **写发布说明**:在 `CHANGELOG.md` 加 `## 0.2.0` 小节。**这一步是强制的**:
   `CHANGELOG.md` 存在时,`mygo build` 要求其中有所发布版本的 `## <版本号>`
   小节,否则签名构建直接报错(`%s has no notes for %s: add a "## %s" section`)。
3. **提交并打标签**:`git commit` 后 `git tag v0.2.0`(标签 = `v` + mygo.json
   版本)并推送。
4. **构建并上传**(每平台一次;macOS 的签名与 dmg 须在 macOS 上执行):

   ```sh
   MYGO_UPDATER_PRIVATE_KEY="$(cat "$APPDATA/mygo/update-keys/mygo-update.key")" \
     go tool mygo build -platform windows/amd64 -upload
   ```

   `-upload` 把安装器、签名的更新压缩包、delta 与 `update-<目标>.json` 清单
   上传到 GitHub 仓库 `ArnoChenFx/kura` 上标签为 `v0.2.0` 的**草稿 Release**
   (不存在则创建;重复上传 `--clobber` 覆盖)。**清单最后上传**,保证应用
   永远不会读到指向缺失文件的清单。不给私钥时构建照常,但**不生成更新文件**
   并打印警告——那样发出的版本无法被自更新发现。
5. **发布**:全部平台就绪后
   `gh release edit v0.2.0 --repo ArnoChenFx/kura --draft=false`。发布后该
   Release 成为 latest,已安装应用随后的每日检查即可看到 `0.2.0`。
6. **收尾确认**:Releases 页面确认 `update-<目标>.json` 与安装器齐全;
   可选:`go tool mygo keygen -force` 换钥前先想清楚——旧应用只认旧公钥。

构建 delta:签名构建会自动下载已发布清单列出的最近 3 个版本做增量包
(`updates.deltas` 可调,`0` 关闭);首个版本或取不到已发布清单时跳过 delta
并说明原因,不影响发布。

## CI 现状(已接入自更新)

`.github/workflows/release.yml` 已接入自更新。它做的事:校验 tag 与
`mygo.json` 版本一致 → 从 `CHANGELOG.md` 提取本版本小节作为发布说明(缺该
小节即失败) → 确认 secret `MYGO_UPDATER_PRIVATE_KEY` 已配置(缺即失败) →
建草稿 Release → 每平台 `go tool mygo build -platform <目标> -upload`(安装器
+ 签名更新包 + delta + 清单,清单最后上传) → Windows 行补传裸 `kura.exe` →
`publish` job 把草稿转正。要做的准备工作只有一件:

1. 在 GitHub 仓库设置里添加 secret `MYGO_UPDATER_PRIVATE_KEY`,内容是
   `mygo-update.key` 的全部内容(单行 base64,不是文件路径)。

其余细节见 `docs/note/ci.md`。

## 已验证 / 未验证

**已验证**(本里程碑证据见下):

- `go build ./...`、`go vet ./...`、`go tool mygo vet .`、
  `CGO_ENABLED=0 go build ./...`、`go test ./...` 全部通过;
- `go tool mygo build -platform windows/amd64` 成功——挂上更新插件后应用
  仍能正常打包;
- `mygo.json` 只含公钥(`grep -c PRIVATE mygo.json` 为 0),git 历史无任何
  私钥文件;私钥在仓库外的 keygen 默认目录;
- 测试覆盖:插件挂载冒烟、应用菜单结构(含更新项占位与 Click 透传)、
  主窗口 Tester 照常构建、开发构建下版本号为空。

**UNVERIFIED(自更新端到端未验证;需要一次打过标签、签过名的发布)**:

- 已安装应用从 GitHub latest 清单发现新版本 → 下载 → 验签 → 替换 → 重启
  的完整链路;
- `mygo build -upload` 真实上传到 GitHub 草稿 Release;
- delta 增量更新路径(需要至少两个已发布版本);
- macOS 签名 / 公证与 dmg(需 macOS 环境);
- CI 接入后的发布工作流(本里程碑未改 release.yml)。
