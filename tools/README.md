# tools/

仓库自用的辅助工具与验证脚本。**这里不放一次性的产物**（日志、截图、补丁、备份）：这类东西用完即删，不入库。

| 路径 | 用途 |
| --- | --- |
| `capture-window.ps1` | 启动构建好的 `kura.exe`、截一张真机窗口图再关掉。界面改动必须用它实际看过（见 `AGENTS.md` 的「验证要求」）。 |
| `capture-multiinstall.ps1` | 中文 Windows 界面下「安装技能」弹窗的真机验证：本地造仓库（不联网），通过 UI Automation 勾选候选、安装与更换仓库，输出截图和文字证据。全程用隔离的 HOME，不碰真实的 `~/.agents` 与 `~/.kura`。 |
| `capture-links.ps1` | 链接形式的技能（联接点）与「左栏只显示存在的目录」的真机验证：在隔离的 home 里（`USERPROFILE`/`HOME`/`APPDATA`/`LOCALAPPDATA`/`TEMP`/`TMP` 都指向新建的临时目录）摆出「10 个真目录 + 3 个联接点 + 1 个断链」的默认技能根，再加一个存在、一个不存在的额外根，交给 `capture-window.ps1` 启动并截图，结束后把临时目录删干净（联接点先摘链接，不穿透）。用法：`powershell -NoProfile -ExecutionPolicy Bypass -File tools/capture-links.ps1 -Exe <kura.exe 绝对路径> -Out <png 路径>`。 |
| `capture-sort.ps1` | 技能排序（按修改时间新的在前 / 旧的在前 / 按名称）的真机验证：在隔离的 home 里（`USERPROFILE`/`HOME`/`APPDATA`/`LOCALAPPDATA`/`TEMP`/`TMP` 都指向新建的临时目录）摆 8 个技能，用 `SetLastWriteTime` 把 mtime 摆成已知顺序（名称顺序与两种时间顺序两两不同），逐个场景写 `~/.kura/config.json`（`sort`、`theme`、`view`；`autoCheckUpdates=false`，截图期间不联网）并交给 `capture-window.ps1` 截图（默认进 `tools/sort-window/`，已被忽略），输出每个场景的预期顺序（EXPECT 行），结束后核对技能 mtime 未被应用改动并删净临时目录。截图期间窗口会在桌面最上面十来秒，机器如果正在被人用，点击、滚轮可能落到窗口里：点了会落盘的开关（主题/排列/排序）会被脚本通过 `config.json` 被改写发现并自动重拍（`-MaxAttempts`），筛选标签、滚动这类不落盘的状态发现不了，要看图；只重拍个别场景用 `-Only "name-light,oldest-dark"`。用法：`powershell -NoProfile -ExecutionPolicy Bypass -File tools/capture-sort.ps1 -Exe <kura.exe 绝对路径>`。 |
| `i18n-migrate/verify_catalog.py`、`keys.json` | 用迁移前的中英文原始取值校验 `locales/*.json`，按规则转换旧 key 并反格式化 ICU 文案；有意新增、删除和修订的文案在脚本中登记。改文案后跑 `python tools/i18n-migrate/verify_catalog.py`。 |

`capture-window.ps1` 用 `-Out` 指定截图文件；`capture-multiinstall.ps1` 用 `-OutDir` 指定输出目录（默认 `tools/multiinstall-window`）。截图、日志与本地测试仓库均为临时材料，验证后删除，不入库。
