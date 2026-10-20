# tools/

仓库自用的辅助工具与验证脚本。**这里不放一次性的产物**（日志、截图、补丁、备份）：这类东西用完即删，不入库。

| 路径 | 用途 |
| --- | --- |
| `capture-window.ps1` | 启动构建好的 `kura.exe`、截一张真机窗口图再关掉。界面改动必须用它实际看过（见 `AGENTS.md` 的「验证要求」）。 |
| `capture-multiinstall.ps1` | 中文 Windows 界面下「安装技能」弹窗的真机验证：本地造仓库（不联网），通过 UI Automation 勾选候选、安装与更换仓库，输出截图和文字证据。全程用隔离的 HOME，不碰真实的 `~/.agents` 与 `~/.kura`。 |
| `i18n-migrate/verify_catalog.py`、`keys.json` | 用迁移前的中英文原始取值校验 `locales/*.json`，按规则转换旧 key 并反格式化 ICU 文案；有意新增、删除和修订的文案在脚本中登记。改文案后跑 `python tools/i18n-migrate/verify_catalog.py`。 |

`capture-window.ps1` 用 `-Out` 指定截图文件；`capture-multiinstall.ps1` 用 `-OutDir` 指定输出目录（默认 `tools/multiinstall-window`）。截图、日志与本地测试仓库均为临时材料，验证后删除，不入库。
