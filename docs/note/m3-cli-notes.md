# 里程碑 3 开发笔记：headless CLI + first-run metadata pass

日期：2026-10-13　分支：`m3-diff`

## 交付物

- `cmd/kura/main.go`：纯接线（os.Args → cli.Run → os.Exit），零逻辑。
- `internal/cli/`：`cli.go`（分发/flag/usage）、`commands.go`（各命令）、 `doctor.go`（体检）、`output.go`（JSON 形状/错误）、`cli_test.go`（28 项）。
- `internal/store/cli_support.go`：store 为 CLI 补的最小导出面（`RemoveWithStamp`、`ValidStampName`）；`AddOptions.Name` 支持安装时命名。
- `internal/store/ops.go`：`Restore` 后清理 `trash/<name>` 空壳目录（否则 list 会出现幽灵 trashed 条目）。
- JSON 契约文档：`docs/note/cli-json-shape.md`。

## 关键决策与踩坑

1. **全局 flag 可前置可后置**。Go flag 包在第一个位置参数处停止解析，所以 `kura --json list` 需要"前置 FlagSet"先把全局 flag 剥掉，再把值作为子命令 FlagSet 的默认值。两处都解析，flag 与位置参数还能任意交错（`parseArgs` 多轮 Parse，解决 `remove NAME --stamp TS`）。
2. **doctor 退出码与输出模式解耦**：JSON 分支曾提前 return 导致漂移时仍退出 0（测试抓住）。现在 `hasProblems` 统一决定 0/3。
3. **`add --name` 改名方案演进**：先"临时目录中转改名再安装"——元数据的 source.path 记录的是临时目录（不诚实）；后改"先原名安装再 rename"—— 原名冲突时会误报；最终落在 **store 层 `AddOptions.Name` 安装时命名**，元数据如实记录真实来源。前两版代码已全部删除。
4. **trashed 条目在 list 中不告警缺 SKILL.md**：trash/<name>/<stamp> 容器本来就没有这些文件，作为正常结构展示。
5. **doctor 安全性**：损坏 JSON、非目录"技能"、不可读条目一律转为 Problem 记录（kind=meta-parse/not-a-dir），绝不 panic；有测试 `TestDoctorSafeOnHostileRoot` 覆盖。
6. **gofmt 与 CRLF**：本仓库 core.autocrlf=true，检出文件带 CRLF， `gofmt -l` 会列出所有既有文件——只 reformat 自己新建的文件。

## 遗留 / v2 建议

- `import-meta` 的 lock 判定与 `store.ImportMeta` 内部逻辑有轻微重复（dry-run 不写文件无法直接复用），改动 store 时要同步两处。
- `scan` 与 `list` 同义；如 v2 想区分，可让 scan 只扫根目录不带侧目录。
