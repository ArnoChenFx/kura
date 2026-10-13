# 更新日志

kura 遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 的格式:
版本号是唯一的小节标题(`## <version>`),MyGo 更新器在打包时会从本文件提取
对应版本的小节作为发布说明,展示在原生更新窗口里。因此**每个版本的说明必须
以 `## <版本号>` 开头**(MyGo 也能识别 `[<版本号>]` 形式的标题)。

应用版本号的唯一来源是 `mygo.json` 的 `version` 字段(打包时由 MyGo 注入);
`internal/cli` 的 `Version` 常量是 CLI 二进制自己的版本号,不参与自更新,
发版时需人工与 `mygo.json` 保持一致。

## 0.1.0

kura 首个版本:一个管理 agent 技能(Skills)的桌面应用与配套 CLI,
基于 MyGo 原生 UI(无 webview)。

- **技能库**:扫描技能根目录,展示每个技能的名称、描述与启用状态;
  按名称/描述搜索过滤。
- **启用 / 停用**:把技能目录在根目录与 `~/.kura/disabled/` 之间移动,
  状态即位置,绝不复制。
- **移除 / 恢复**:把技能移入 `~/.kura/trash/`(可恢复),移除前自动
  快照到 `~/.kura/snapshots/`。
- **导入元数据**:读取 `~/.agents/.skill-lock.json`(只读,绝不改写),
  把安装来源(GitHub 仓库与仓库内路径,或本地路径)、安装器插件名与安装
  时间收进技能的 `.kura-meta.json`;已有元数据的技能一律跳过。
- **更新预览**:对有安装来源的技能重新拉取上游,以文件级 diff 展示将要
  变更的内容,逐文件审阅后再落盘;更新前同样先快照。
- **诊断**:`kura doctor` 检查根目录、锁定文件与元数据的常见问题。
- **CLI**:与桌面应用同一套能力的无界面入口(`kura list / add / update /
  disable / enable / remove / restore / import-meta / doctor`),输出
  机器可读的 JSON。
- **自更新**(应用级):应用菜单提供「检查更新…」,后台每天自动检查一次;
  下载、Ed25519 签名校验、替换与重启全部由 MyGo 原生更新窗口完成。
  更新以 `vX.Y.Z` 标签的 GitHub Release 发布,详见
  [docs/note/updates.md](docs/note/updates.md)。
