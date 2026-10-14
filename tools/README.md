# tools/

仓库自用的辅助工具与验证脚本。**这里不放一次性的产物**（日志、截图、补丁、备份）：这类东西用完即删，不入库。

| 路径 | 用途 |
| --- | --- |
| `capture-window.ps1` | 启动构建好的 `kura.exe`、截一张真机窗口图再关掉。界面改动必须用它实际看过（见 `AGENTS.md` 的「验证要求」）。 |
| `capture-multiinstall.ps1` | 「安装技能」弹窗的真机端到端验证：本地造仓库（不联网）→ UI Automation 驱动窗口（勾选候选、安装、改仓库）→ 截图 + 文字证据。全程用隔离的 HOME，不碰真实的 `~/.agents` 与 `~/.kura`。 |
| `demo-discover/main.go` | `go run ./tools/demo-discover`：打印「仓库内技能发现」的三种情形（调用真实的 `internal/store` API，在临时目录里构造仓库）。 |
| `watch-agent.sh` | 守望一个 herdr 编码 agent：定期采样窗格内容并算哈希，判断它是真在干活还是卡住。`bash tools/watch-agent.sh <agent 名> <采样次数> <间隔秒>`。 |
| `prompts/review-brief.txt` | 派给独立评审 agent 的提示词模板：只认探针证据、必查的缺陷类别、报告格式。 |

截图脚本的输出目录由 `-OutDir` 指定（默认写到仓库根的 `tools/multiinstall-window`，同样属于临时材料）。
