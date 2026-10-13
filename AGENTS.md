# AGENTS.md — kura

这份文件是所有在本仓库工作的 agent（pi、Claude Code、Codex、人类协作者）的共享约定。
**开工前先读这里**；本文件与 `REQUIREMENTS.md` 冲突时，以 `REQUIREMENTS.md` 为需求准绳，以本文件为工程约定。

---

## 1. 项目是什么

kura 是一个**桌面技能管理器**，用 [MyGo](https://github.com/egoist/mygo) 的**原生 `ui` 包**构建，
无 webview、无 Bun/npm/Node。

- 仓库：`E:\Project\kura`，公开：`github.com/ArnoChenFx/kura`
- Go module：`github.com/ArnoChenFx/kura`
- 管理若干个技能根目录：默认的 `~/.agents/skills`（Windows：`C:\Users\ArnoChen\.agents\skills`），
  外加用户在界面里添加的任意文件夹（多根目录）

**用户目标**：让技能目录好管理、看得清、能恢复，并且应用本身能自更新。

---

## 2. 架构与边界

| 包 | 职责 | 约束 |
| --- | --- | --- |
| `internal/store` | 核心领域逻辑：扫描、元数据、哈希、启用/停用/移除/恢复/添加/更新/预览更新、多根目录（`Library`） | **不依赖 UI，不依赖 CLI** |
| `internal/cli` | 无界面命令行入口（`cmd/kura`） | 不依赖 UI；`Run()` 返回退出码，绝不 `os.Exit` |
| `internal/ui` | MyGo 原生窗口：三栏（技能目录 / 卡片网格 / 详情）、搜索与筛选、开关、安装弹窗、更新预览弹窗 | 不直接做磁盘 I/O，一律经 `internal/store` |
| `internal/selfupdate` | 挂载 MyGo 原生更新器插件、应用菜单 | 只做装配，不重写下载/替换逻辑 |
| `main.go` | MyGo 窗口应用的装配（App / Window / 菜单） | 只做装配 |
| `cmd/kura/main.go` | CLI 二进制入口，零逻辑 | 只把 `os.Args` 交给 `cli.Run` |

**依赖方向永远单向**：`ui` / `cli` → `store`。`store` 绝不可 import `ui` 或 `cli`。

### 两个二进制

- **窗口版**：根目录 `main.go`（`go build .` / `go tool mygo build`）
- **CLI 版**：`cmd/kura`（`go build ./cmd/kura`）

两者互不影响，各自构建。改一个不要破坏另一个。

---

## 3. 领域规则（不可违反）

1. **根目录来自配置，绝不硬编码**。默认根目录是 `~/.agents/skills`；其余根目录由用户在界面里添加，列表存在 `~/.kura/config.json` 的 `roots`，经 `store.Library` 读写。`.skill-lock.json` 只描述默认根目录，「导入元数据」只对它开放：界面不给追加的根目录这个按钮，`store.Config.ImportMeta` 对它们返回 `ErrImportDefaultOnly`，命令行 `import-meta` 同样拒绝（退出码 1，含 `--dry-run` 与 `--lock`）。
2. **`~/.agents/.skill-lock.json` 是只读的**：只读入、**绝不重写**。
3. **每个技能目录内写 `.kura-meta.json`**（`metaVersion: 1`），记录来源与时间。它自身**不计入** `contentHash`。
4. **禁用/启用是「移动」**，不是复制、也不是标记位：技能目录在 `~/.agents/skills/` 与 `~/.kura/disabled/` 之间移动。
5. **移除是软删除**：移动到 `~/.kura/trash/`，可恢复。
6. **任何破坏性操作前先快照**（`~/.kura/snapshots/<skill>/<timestamp>/`）。
7. **`contentHash` 稳定**：路径排序后对全部文件求 SHA-256，排除 `.kura-meta.json`，这样未改动的技能哈希不变、漂移可检测。
8. 元数据损坏**不能让应用崩溃**：返回错误、界面保持可用。`config.json` 损坏同理：`LoadLibrary` 返回只含默认根目录的列表并附带错误，界面报告它并继续可用。
9. **每个根目录的状态互相隔离**：默认根目录沿用原有布局（`~/.kura/disabled|trash|snapshots`，已有数据无需迁移）；追加的根目录用 `~/.kura/roots/<id>/disabled|trash|snapshots`（`<id>` 是规范化路径 SHA-256 的前 12 位，见 `store.RootID`）。两个根目录里的同名技能互不干扰。**从列表移除一个根目录只改 `config.json`，绝不动磁盘上的技能。** 命令行的 `--root` 指向列表里的根目录时落到同一处（`cli.resolveConfig` 经 `Library.Config`）。
10. **外观偏好存在 `config.json` 的 `theme`**（`system` / `light` / `dark`；缺省与手改出的未知值都按 `system`，不算错误）。读写只经 `store.Library`（`Theme`、`SetTheme`，`store.NormalizeTheme` 规范化）。`config.json` 的「读—改—写」一律持 `configMu`（`SetTheme` 与 `save` 都已持有）：外观与目录列表是各自的后台任务，不串行会互相覆盖；新增写 `config.json` 的路径必须照做。

11. **检查更新比的是 git 对象哈希，不是时钟，也不为每个技能各 clone 一次**。流程见 `internal/store/upstream.go` 与 `store.PreviewUpdates`：先 `git ls-remote` 比 `source.commit`，变了再做一次只取树的浅 clone（`--filter=blob:none --no-checkout`）比技能子目录的 `source.tree`，仍不同才稀疏检出该子目录逐文件比较。同一仓库（URL 去掉 `.git` 与末尾 `/`，加 ref）的技能共用一次 clone，检查与更新都一样（`PreviewUpdates` / `UpdateMany`，不要在循环里逐个调 `PreviewUpdate` / `Update`）。所有 git 调用走 `runGit` / `gitOutput`（带超时、`GIT_TERMINAL_PROMPT=0`、低速中止），它们经 `gitCommandContext`（`exec_cmd_windows.go` / `exec_cmd_other.go`）启动 git，Windows 下不弹控制台窗口：这是唯一的 git 入口，非测试代码不要再直接 `exec.Command("git", …)`。`Add` 与 `Update` 都要把 `commit` 与 `tree` 写进元数据；元数据没有它们（旧版本安装、由 lock 导入）时退回逐文件比较。

kura 自己的目录：

```
~/.kura/
  disabled/<skill>/              # 已停用的技能
  trash/<skill>/                 # 已移除的技能（带时间戳）
  snapshots/<skill>/<timestamp>/ # 更新前/移除前的快照
  roots/<id>/                    # 追加根目录各自的 disabled/ trash/ snapshots/
  config.json                    # 根目录与偏好（theme：system | light | dark）
```

---

## 4. 编码约定

- **注释与用户可见字符串用中文**；包名、类型、函数、变量名用英文。
- 错误信息统一以 `store: ` 开头，例如 `store: 未找到该技能`。
- **错误要能用 `errors.Is` / `errors.As` 判断**；已知可判定的情况导出哨兵错误（如 `ErrUnknownSkill`），不要只返回裸字符串。
- 路径拼接一律 `filepath.Join`；不用字符串拼。
- 依赖克制：先看标准库与已固定版本的 MyGo；引入新依赖要先说明理由。
- 不要 `panic` 表达可预期的失败（坏元数据、缺文件）；`panic` 只留给编程错误。
- `gofmt` 格式化是硬要求。历史上出现过「整个文件被挤成一行」的提交，**提交前必须 `gofmt -w`**。

### UI 线程契约（最容易出错的地方）

- 视图可能在一次事件里重建多次；**副作用只能经被处理的动作或显式受保护的任务触发**，不要放在无条件渲染路径里。
- **慢 I/O 与重计算绝不放在 UI 线程**；结果通过 `Window.Update` 异步发布回主线程。
- 元素（`ui.Element`）与部件**在下一次构建前就失效**；存应用数据、部件状态、`ui.Handle`，**绝不存元素**。
- 状态更新后调用 `Invalidate` 请求重绘，不要在同一次构建里同步写模型再查询。
- UI 失败用 `ui.NewTester` 复现，要覆盖元素消失后重现的额外帧。
- **切换根目录要作废旧结果**：后台任务启动时记下 `App.gen`，发布时 `gen != a.gen` 就只清 busy、不覆盖当前列表（见 `publishAfterWork`、`reload`）。新增后台路径必须照做。
- 选中以**技能名**为准（`selName`），网格里才用下标（`selIdx`，由 `syncSelection` 对齐）；筛选把选中项藏起来时右栏保持不变。
- **颜色只从 `internal/ui/theme.go` 来**（`kuraScheme` / `palette`），视图代码里不写十六进制。配色取自参考截图：深炭灰底、带边框的卡片、薄荷绿开关、一种低饱和的雾蓝紫强调色。**不要用饱和的蓝紫/靛蓝**（用户明确说过很丑），头像色板也刻意不含蓝紫（`theme_test.go` 守着）。
- **GridView 给每个单元格自己画一层选中框与悬停底**（圆角 8、2px 边：网格有焦点时 `Accent`，无焦点时 `SurfacePressed` 底加 `Border` 边，悬停时 `SurfaceHover` 底），颜色取自构建网格时的主题。kura 的卡片自己画选中与悬停（圆角更大、边 1px），两层叠一起就是「高亮框和卡片边框对不齐」。所以 `viewGrid` 用 `skin.cell`（这四个颜色透明的主题副本）构建网格，卡片里换回真主题（见 `viewGrid`、`newSkin`）。动网格或卡片外框后先跑 `TestCardChromeIsOneAlignedRing`（读真实渲染的像素）。
- **别在要参与网格点击的元素上调 `RightClicked()` / `Clicked()` / `Pressed()`**：它们把元素标成可点击，会吞掉网格单元格要收的左键点击（表现为点卡片选不中）。右键要做的事放进 `ContextMenu` 的构建函数（它只在菜单打开、选了菜单项后各跑一次；右键选中卡片就在那里做）。
- **部件绑定的局部变量在构建结束或查询响应时才被写入**：构建中途直接比较它，拿不到这一次的点击，下一遍构建又从应用状态重新推出，点击就丢了。分段控件用 `Track.Changed()`、开关用 `Changed()` 在同一次构建里取到新值（见 `viewThemeSwitch`、`viewCard` 里的开关），或者直接绑应用的持久字段（`a.filter`、`a.rootSel`）。
- **原生右键菜单**（`Element.ContextMenu`）不出现在截图里。菜单项的 `Chosen()` 在选择后的下一帧报告，副作用只能放在它里面；测试用 `tt.RightClick` / `tt.Menu()` / `tt.ChooseMenuItem`（见 `menus_test.go`）。
- **外观**：`App.themeMode` 决定皮肤（`View` 开头 `kuraScheme.apply(c, a.themeMode)`，跟随系统时读系统外观）。改它一律走 `SetThemeAsync`：立刻换皮肤，后台同步原生外观（`mygo.Theme.SetSource`）并写 `config.json`，连续切换只有最新一次执行，写盘失败只在状态行报告、不回滚皮肤。启动时 `main.go` 在 `App.Run` 之前调 `app.SyncTheme()`。
- 详见 `.agents/skills/mygo-maintenance/SKILL.md`；MyGo 的 API 以**固定版本源码**为准（`go list -m -f '{{.Dir}}' github.com/egoist/mygo`），不要凭记忆猜。

### CLI 退出码（已固定，不要改）

| 码 | 含义 |
| --- | --- |
| 0 | 成功 |
| 1 | 操作失败（技能不存在、同名冲突、IO 错误等） |
| 2 | 用法错误（未知命令、flag 解析失败、参数个数不对） |
| 3 | `doctor` 发现问题，或 `update --check` 发现有待更新 |

`cli.Run` **绝不调用 `os.Exit`**，退出码由 `cmd/kura/main.go` 统一处理。

---

## 5. 验证要求（提交前必须全绿）

每个 agent 在报告完成前**必须贴出原始输出**：

```bash
gofmt -l .                      # 必须为空
go vet ./...                    # 必须干净
go tool mygo vet .              # 必须干净
go test ./...                   # 全包通过
CGO_ENABLED=0 go test ./...     # 全包通过
CGO_ENABLED=0 go build ./...    # 成功
go tool mygo build -platform windows/amd64   # 成功（证明仍能打包）
```

改共享状态时加 `-race`。改布局或平台相关行为时，**必须看真实窗口**，不能只看测试：

```bash
# 构建后启动窗口、截图、关闭；输出 RECT / SAVED / CLOSED
powershell -NoProfile -ExecutionPolicy Bypass -File tools/capture-window.ps1 \
  -Exe <exe绝对路径> -Out <png路径>
```

**诚实报告**：说清哪些验证过、哪些没验证。发布产物、签名更新链路这类本机做不到的，
要明写「未验证」以及原因，不要含糊成「应该没问题」。

### 测试隔离（硬要求）

- 测试**只能用 `t.TempDir()` 造的根目录**，**绝不**碰真实的 `~/.agents/skills`、`~/.kura`。
- 依赖 git 的测试要固定 `GIT_CONFIG_GLOBAL` 等环境，避免受本机配置影响。
- 不要写会联网的测试（GitHub 取源要在测试里注入/替换；UI 里要测 GitHub 安装的失败路径，用 `t.Setenv("PATH", t.TempDir())` 让 git 找不到）。
- UI 测试里文件夹选择框 `App.pickFolder` 必须换成假实现，绝不弹出系统对话框。
- 打开文件/文件夹、在文件管理器里显示、同步原生外观这三个系统调用（`App.openPath` / `reveal` / `setSource`）在 `newAppFor` 里默认换成空实现；要检查调用就用 `fakeShell` / `recordSource`。绝不真的打开窗口或改系统外观。

---

## 6. Windows / 仓库环境的坑

- 本仓库 `core.autocrlf=true`：工作区文件带 CRLF，**`gofmt -l .` 会把既有文件全部列出（假阳性）**。
  判断格式是否真有问题：`tr -d '\r'` 之后再比 `gofmt` 输出，或只看你**新增**的文件。
- 提交时 git 会打印 `LF will be replaced by CRLF` 警告，这是正常的，不是错误。
- Shell 是 Fork 自带 git 的 bash（`LORCA_SHELL`）。命令默认在 `E:\Project\kura` 下执行。

---

## 7. Git 与工作区约定

- **默认不提交、不推送**。只有明确要求时才 `git commit` / `git push`；`git push` 需要用户批准。
- 一个里程碑/任务一个分支，**worktree 必须放在项目内** `E:\Project\kura\.worktrees\<名字>`，
  **绝不要**在 `E:\Project` 根目录下建 `kura-*` 目录。`/.worktrees/` 已在 `.gitignore` 里。
- **只操作自己的 worktree 与 agent 面板**，不碰用户正在用的会话和其他 worktree。
- 提交信息用英文，形如 `feat(ui): ...` / `fix(store): ...` / `ci: ...` / `docs: ...`。
- 临时文件（prompt、日志、截图、watch 脚本）放 `tools/`，不要散落在仓库根或项目外。

---

## 8. 版本与发布

- **`mygo.json` 的 `version` 是应用版本号唯一来源**，不要在代码里另写一份常量。
- 发布标签形如 `vX.Y.Z`；`.github/workflows/release.yml` 会校验标签与 `mygo.json` 的 `version` 完全一致，不一致直接失败。
- 自更新配套：公钥写在 `mygo.json` 的 `updates.publicKey`，私钥作为仓库 secret `MYGO_UPDATER_PRIVATE_KEY`。
- **私钥（`mygo-update.key`）绝不能进入仓库、绝不能提交、绝不能在报告里打印。**

---

## 9. 已知的非目标（v1 不做）

- 在 kura 里编辑 `SKILL.md` 正文
- 同步到 `.claude/skills` 等镜像目录
- 把技能上传回 GitHub
- 技能依赖解析
- 在多个根目录之间复制/同步技能（添加、管理多个根目录本身是支持的）

---

## 10. 动手前的检查清单

- [ ] 读了本文件与 `REQUIREMENTS.md`
- [ ] 确认自己在正确的 worktree 与分支上，没有碰别人的工作区
- [ ] 改 MyGo API 前读了固定版本源码，没有凭记忆猜
- [ ] 破坏性操作前有快照，测试用 `t.TempDir()`
- [ ] 慢 I/O 不在 UI 线程，结果经 `Window.Update` 回主线程
- [ ] `gofmt -l`、`go vet`、`go tool mygo vet`、`go test ./...` 全绿
- [ ] 改了界面就真的启动了窗口看过
- [ ] 报告里写清了「验证过什么」与「什么未验证」
