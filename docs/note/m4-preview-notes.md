# 里程碑 4 笔记：diff 视图 + 更新评审

日期：2026-02-14（工作分支 `m4-update-preview`，基于合并后的 main）

## godiff 调研结论

`github.com/egoist/godiff` **不可作为库复用**。核实方式：拉取其 go.mod 与仓库树——

- go.mod：`module github.com/egoist/godiff`，依赖 `egoist/mygo` + `alecthomas/chroma/v2`，
  还声明了 `tool github.com/egoist/mygo/cmd/mygo`（应用工程特征）；
- 仓库树：`main.go`、`window.go`、`diffview.go`、`app_test.go`、`mygo.json`、`internal/agent/`……
  全部是包 `main` 的 GUI 应用文件，没有可导入的库包，也没有导出的 diff API。

因此**文件级 diff 为自行实现**（`internal/store/preview.go`）：unified hunk 不是 v1 必需，
输出为文件清单 + 每文件 added/removed 行数（LCS 最小编辑距离，口径同
`git diff --numstat`；超过 400 万格的文件退化为多重集差以防爆内存）。

## store：PreviewUpdate

- 签名：`(*Config).PreviewUpdate(name) (*UpdatePreview, error)`；
- github 来源复用 Update 的 `refetchGitHub`（浅 clone 进 os.MkdirTemp 并清理），
  local 来源直接只读来源目录；
- 产出：每文件 `added/removed/changed` + 文件级新旧 sha256 + 目录级
  `OldHash/NewHash`（与 `Meta.ContentHash` 同口径，NewHash 额外排除 `.git`，
  因此与 Update 完成后的 `HashDir` 结果一致——有跨函数测试锁定该契约）；
- 上游 commit 通过 `gitHeadStaging`（clone 目录内 rev-parse）取得；
- 只读保证：不写快照、不动技能目录、不改元数据（有逐字节指纹测试锁定）。

已知取舍：

- 行数统计不感知 git 的 autocrlf；克隆产物与本地内容行尾不一致时会显示为整文件
  变更。测试里用 `GIT_CONFIG_GLOBAL` 指向空文件隔离了本机 `autocrlf=true`，
  真实用户场景中 kura 安装的内容与上游 clone 的行尾处理一致，通常不出现。
- `.kura-meta.json` 与 `.git` 在两侧都跳过（元数据是 kura 自管文件，不属于技能内容）。

## UI

- 详情面板新增「检查更新」（仅 enabled 技能），工具栏新增「全部检查」；
- 预览跑在 `spawn`（后台）里，经 `publish` 回 UI 线程，沿用 app.go 的线程约定；
- 评审面板用 `myui.Modal`：单技能模式逐文件一行（状态/路径/±行数/新哈希前 8 位），
  无变化显示「与上游一致」并禁用应用；全部检查模式每技能一小节
  （有变化列文件摘要、无变化标「已是最新」、失败显示错误），底部
  应用/关闭；Apply 调 `Update`（全部检查时逐个应用有变化的技能），
  完成后重扫并关闭面板；
- 检查期间防重入：`checking/checkingAll/review.applyBusy` 三个门槛。

## CLI

- `kura update --check <NAME>`：退出码 3 有待更新 / 0 无变化 / 1 预览失败；
- `kura update --check --all`：逐技能预览，失败条目不中断整体；任一技能有变化 → 3；
- 用法错误（缺 NAME、--all 带 NAME）退出 2；
- JSON 形状见 `docs/note/cli-json-shape.md` 的「update --check」一节。

## 测试与验证

- store：7 个 PreviewUpdate 测试（changed/added/removed 一次覆盖、无变化、
  漂移、只读指纹、NewHash≈Update 后哈希、local 来源、错误路径），
  全部用 t.TempDir 里的本地 git 仓库 + insteadOf 重写，无网络；
- ui：4 个 Tester 测试（面板显示与应用、无变化面板、失败进状态行、
  全部检查汇总），local 来源驱动，无 git 依赖；
- cli：3 个测试（--check 退出码与 JSON 形状、--all 汇总、坏技能不阻塞 + 用法错误）。

教训：git 的 `url.<base>.insteadOf` 允许多条规则匹配同一 URL 且选择不可靠——
测试里重镜像 bare 时必须先 unset 旧 key（store 与 cli 测试都踩过）。
