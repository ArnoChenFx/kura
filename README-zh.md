# kura

一个桌面技能管理器（agent skills manager），用 [MyGo](https://github.com/egoist/mygo) 的原生 `ui` 包构建：没有 webview，不需要 Node，也不需要 Bun。

[English README](README.md)

## 它能做什么

- **同时管理多个技能根目录**。默认根目录是 `~/.agents/skills`，永远排在列表第一位；任何其他文件夹都能在界面里加进来。每个根目录有各自的停用/回收站/快照数据，不同根目录里的同名技能互不干扰。
- **安装技能**：来源可以是 GitHub 仓库（`owner/repo` 或粘贴链接，可指定子目录、分支与安装名），也可以是本地文件夹。仓库里的技能会被自动发现，一个仓库能一次装上多个；其中一个失败不影响其余。默认名字取 `SKILL.md` 里的 `name` 字段。
- **启用与停用靠移动**，不是复制，也不是技能里的标记位：目录在根目录与 `~/.kura/disabled/` 之间往返。移除则是移进 `~/.kura/trash/`，随时可以恢复；从回收站永久删除要先确认，并且会先写一份内容快照。
- **按 git 对象哈希检查更新**，不看时钟也不看文件时间：上游没变就一个文件都不下载。同一个仓库的技能共用一次 clone；已装内容被本地手改过的技能会被报成「本地有改动」，而不是「有更新」。
- **查看技能全文并翻译**。详情栏把 `SKILL.md` 渲染成 Markdown，还能把段落翻成一个全局目标语言，围栏代码块整块保留。译文只在内存里：不写进技能目录，也**绝不** 改动 `SKILL.md`。
- **中英双语界面**。第一次打开跟随系统语言（系统是中文就用中文，其余一律英文），也可以在设置弹窗里固定；命令行有 `--lang` 与 `KURA_LANG`。
- **三栏窗口**（技能目录、技能卡片、详情）。栏宽可拖，目录列表可拖动排序，技能区可切换卡片网格与两行列表，还有搜索框和带计数的 全部/启用/停用/回收站 筛选。右键菜单用系统原生菜单，外观跟随系统或固定在浅色/深色。
- **应用自更新**，由 MyGo 更新器完成：应用菜单里有「检查更新」，后台每天还会自动检查一次；下载的更新包通过 Ed25519 验签后才安装。
- **自动发现本机 agent 目录**。启动时把已经存在的全局技能目录加进列表（Claude Code、 Codex、Cursor、Gemini CLI、Pi 等，见 `internal/store/agentdirs.go`）；你从列表里移除过的目录不会再被加回来。

## 安装

到 [Releases](https://github.com/ArnoChenFx/kura/releases) 页面下载。发布产物的命名如下：

- Windows：`kura.Setup.<version>.amd64.exe`（按用户安装的安装器）、`kura.exe`（免安装）
- macOS：`kura.<version>.arm64.dmg`、`kura.<version>.amd64.dmg`
- Linux：`kura_<version>_{amd64,arm64}.deb`、`kura-<version>-linux-<arch>.tar.gz`，或 `install.sh`（为当前用户装到 `~/.local/kura.app`，不需要 root）

这些产物装的是窗口版；命令行是独立二进制，从源码构建（见下一节），或者取 CI 每次运行附带的 `kura-cli-<os>-<arch>` artifact。

## 命令行

`cmd/kura` 是与窗口版共用同一套 store 的无界面 CLI；`kura help` 打印完整用法。

```sh
kura list                  # 或 scan：根目录里的全部技能及其状态
kura add --local PATH      # 从本地文件夹安装
kura add --github owner/repo [--sub-path P] [--ref R] [--name N]
kura update NAME           # 按记录的来源重新拉取（先快照）
kura update --check NAME   # 预览会改什么；--all 检查全部技能
kura disable NAME          # 也支持 enable / remove / restore
kura import-meta           # 只适用于默认根目录；读 ~/.agents/.skill-lock.json
kura doctor                # 只读体检
```

退出码：`0` 成功，`1` 操作失败，`2` 用法错误，`3` `doctor` 发现问题或 `update --check` 发现有待更新。每个命令都支持 `--json`（出错时错误以 `{"error": "..."}` 输出到 stderr）以及 `--root` / `--home`（或环境变量 `KURA_ROOT` / `KURA_HOME`）；JSON 形状见 [docs/note/cli-json-shape.md](docs/note/cli-json-shape.md)。

## 文件放在哪

| 路径 | 内容 |
| --- | --- |
| `~/.agents/skills/` | 默认技能根目录（Windows：`%USERPROFILE%\.agents\skills`） |
| `~/.kura/config.json` | 追加的根目录、外观（`theme`）、技能区排列（`view`） |
| `~/.kura/disabled/`、`trash/<skill>/<stamp>/`、`snapshots/<skill>/<stamp>/` | 已停用的技能、已移除的技能（可恢复），以及更新前与移除前写下的快照 |
| `~/.kura/roots/<id>/` | 每个追加根目录自己的那三个目录；`<id>` 是规范化路径 SHA-256 的前 12 位十六进制 |
| `~/.kura/settings.json` | 翻译 API 的地址、密钥与模型，以及全局目标语言 |
| `~/.agents/.skill-lock.json` | `kura import-meta` 的只读输入；kura 绝不重写它 |
| `<skill>/.kura-meta.json` | 每个技能的元数据：来源、commit 与树哈希、时间、内容哈希 |

`settings.json` 里的翻译 API 密钥是**明文**保存的（权限 `0600`）：这是本地单人应用刻意选的方案。**不要把它用在公用机器上。**

## 从源码构建

需要 Go 1.27 或更新（`go.mod` 写的是 `go 1.27.2`），除此之外什么都不需要：没有前端构建步骤，Node、Bun、npm 一概不参与。

```sh
go build .            # 窗口版
go build ./cmd/kura   # 无界面 CLI
go tool mygo build    # 打包；交叉打包用 -platform windows/amd64 之类

gofmt -l .            # CI 会跑的检查——改动应该让这些保持全绿
go vet ./...
go tool mygo vet .
go test ./...
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go build ./...
```

## 参与开发与文档

[AGENTS.md](AGENTS.md) 是仓库的工程约定（包边界、UI 线程契约、验证要求、测试隔离）； [REQUIREMENTS.md](REQUIREMENTS.md) 是 kura 必须做到什么，含边界情况与非目标。 [CHANGELOG.md](CHANGELOG.md) 是发布说明的来源，且每个版本都要有 `## <版本号>` 标题，因为更新器会展示那一小节。开发笔记都在 [docs/note/](docs/note/)。

## 现状

kura 处于 `0.1.x`。窗口里的文字目前只有中文。发布产物没有 Developer ID 或 Authenticode 签名，也没有公证，因此 macOS 与 Windows 可能弹出未签名应用的常见提示。自更新链路尚未端到端验证，见 [docs/note/updates.md](docs/note/updates.md)。

## 许可

本仓库未声明许可证。
