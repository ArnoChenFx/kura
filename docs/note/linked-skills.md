# 链接形式的技能，与左栏只显示存在的目录

分支 `feat/linked-skills`，基线 `cb14e74`（v0.1.22）。本文记录这次改动的背景、Go 在 Windows 上的实测行为、各处的取舍，以及哪些验证过、哪些没有。行为需求以 `REQUIREMENTS.md` 的「Linked skills」小节为准，工程约定以 `AGENTS.md` 的领域规则 14 为准。

## 背景

用户的 `~/.agents/skills` 里有 13 个条目，kura 只显示 10 个。对不上的 3 个（`cherry-browser`、`cherry-computer`、`ego-browser`）都是 **Windows 联接点（junction）**，由 CherryStudio 等程序建到别处的真实目录。同一个原因让 `kura doctor` 误报「根目录下存在同名普通文件，技能必须是目录」。

根因：Go 在 Windows 上把联接点的目录项报成 `ModeIrregular`（既不是 `ModeDir` 也不是 `ModeSymlink`），而扫描、计数、`doctor` 都用 `e.IsDir()` 过滤，整条被丢掉。顺带查到：`filepath.WalkDir` 不会进入联接点，所以 `HashDir` / `copyDir` 在链接目录上直接失败，「删除 → 写快照 → 移入回收站」这条链对链接技能整个失效。

## 实测事实（Go 1.27.4，Windows 11，真机）

| 操作 | 联接点 | 目录符号链接 |
| --- | --- | --- |
| `os.Lstat` / `DirEntry.Type()` | `ModeIrregular`，`IsDir()` 为 false | `ModeSymlink` |
| `os.Stat` | 跟随，目标是目录时 `IsDir()` 为 true | 同左 |
| `os.Readlink` | 返回目标的绝对路径 | 返回创建时写的目标（可能是相对路径） |
| `filepath.EvalSymlinks` | **无效，返回自身** | 正常解析 |
| `filepath.WalkDir(link)` | 只回调根一次，`IsDir()` 为 false，不进入 | 同左（根是 Lstat 的结果） |
| `os.Rename`（同卷） | 移动链接本身，目标不动 | 同左 |
| `os.Rename`（跨卷） | `ERROR_NOT_SAME_DEVICE`（17） | 同左 |
| `os.RemoveAll(link)`、`RemoveAll(含链接的目录)` | 只摘链接，**不穿透**到目标 | 同左 |
| 断链的 `Lstat` / `Stat` / `ReadDir` | `Lstat` 成功（irregular），`Stat` 失败 | 同左 |

## 设计与取舍

**判定只有一处。** `store.isSkillDirEntry`（导出版 `IsSkillDirEntry`，供 `internal/cli` 的 `doctor` 用）：`e.IsDir()`；或链接且 `os.Stat` 成功并是目录。`scanDir`、`CountEnabled`、`doctor` 的根/disabled/trash 三处、`Purge` 的时间戳层、`latestSubdir` 都走它。别处不再写 `e.IsDir()`。

**遍历用 `walkTree`，不用 `filepath.WalkDir`。** 用 `os.ReadDir` 递归，遍历根自己与树里的链接目录都当目录进入；回调约定（`SkipDir` / `SkipAll`、先目录后内容、按文件名排序）与 `WalkDir` 一致，调用方几乎原样迁移。`HashDir`、`copyTree`、`preview.go` 的三个遍历、`snapshots.go` 的指纹/差异/复制都改用它。唯一保留 `WalkDir` 的是 `validateSnapshotFiles`：历史副本必须是 kura 自己写下的真实目录，拒绝一切链接，不能跟随。

**防环：沿路径记真实目录，不用 `os.SameFile`。** 每进入一层目录就把它规范化的真实路径（Windows 忽略大小写）记进「当前路径」集合；链接解析后落在这个集合里（指回了祖先）就整条跳过，不回调也不进入。两个链接指向同一个目录（菱形）不是环，两处都遍历，否则复制出来的快照会悄悄少一份内容。

没用 `os.SameFile` 是因为它在两种情况下的失败方式不对：某些网络盘 / FUSE 挂载的文件索引是 0，所有目录都会被判成「同一个」，整棵子树被静默丢掉——快照悄悄不完整，是最坏的失败。路径键的失败方式是**漏判**，漏判之后读写仍然沿着沿原路径拼出来的路径走（经过链接也有效），无限下钻会撞上系统自己的链接层数上限（Linux 40、Windows 63），以读目录失败收场——失败是响亮的。

**不可信来源不进入树里的链接。** `git clone` 的产物（`SourceGitHub`）用 `walkOpts.noSubLinks`：只解析根，树里的链接目录当文件项、不进入。否则恶意仓库里一个 `evil -> /` 就能让复制把整个磁盘拖进技能目录；以前它因为「目录当文件读」而报错失败，现在保持这个旧行为。用户自己的目录（已安装的技能、本地来源）照常进入。

**快照是穿透复制的真实内容。** `copyDir` 经 `walkTree`，链接技能的快照里没有任何链接。测试（`TestRemoveLinkedSkillWritesRealSnapshot`）断言快照是真实目录、`HashDir` 与目标一致。

**移动的是链接本身，跨卷不展开。** `moveDir` 的跨卷回退是「复制 → 改名 → 删源」，对链接会把它展开成真目录、悄悄把「移动」变成「复制内容」。所以先 `Lstat`：链接走 `moveLinkAcrossVolumes`——符号链接在目的地用同一目标重建（相对目标先换成绝对路径）再摘源，摘源失败则回滚；联接点标准库造不出来，返回 `storeErrLinkCrossVolume`，源原样保留。

**永久删除只删链接。** `removeEntry`：`Lstat` 确认是链接（且 `Readlink` 读得出目标）就 `os.Remove`，否则才 `RemoveAll`。`Purge` 逐个时间戳副本调用它，最后再删容器目录。`RemoveAll` 本来就不穿透（上表），这里让「只删链接」成为读得到的保证。读不出目标的重解析点（云盘占位目录之类）按普通目录删，否则 `os.Remove` 会因为「非空」删不掉。

**更新与历史快照恢复作用在真实目录上。** `realDir`：`os.Readlink` 取目标，相对目标按链接所在目录换算（`filepath.EvalSymlinks` 对联接点无效，不能用），链接指向链接最多跟 32 跳。`replaceSkill` 把快照、`copyTree`、删除、改名、写元数据全放在真实目录上，暂存目录 `.kura-new` 建在真实目录旁边（与它同卷，最后的 `Rename` 才是原子的），链接保持不动；「失败从快照恢复」保留，恢复成功后顺手清掉暂存目录（链接场景下它在别的程序的目录旁边，不能留着）。`restoreSnapshot` 同理。

链接可以指到任何地方，而替换会先删光目标，所以动手前过 `checkReplaceTarget`：目标是盘符根、或把 kura 自己的技能根目录/数据目录包含在内（链接指回了上级目录，甚至指回根目录本身）就拒绝，`TestUpdateRefusesUnsafeLinkTarget` 守着「什么都不能被删」。普通（非链接）技能不走这条，行为与以前完全一致。

**历史快照里的「当前副本」。** `snapshotTarget` 以前拒绝从配置根开始的任一链接。现在只放行**技能目录自己**是链接（它必须解析到存在的目录），根到技能目录之间的各级仍必须是真实目录；当前副本里允许链接目录（会被穿透复制），仍拒绝指向文件的链接、断链和设备等特殊文件（`validateCurrentFiles`）。历史副本一侧一条没放松：`validateSnapshotFiles` 与 `copySnapshotTree(…, strict=true)` 仍拒绝任何链接。

**`doctor` 对断链保持沉默。** 断链按「目录不存在」处理：不显示、不计数；`doctor` 也不把它报成「同名普通文件」（那句话对它不对），根、disabled、trash 三处一致。（`doctor` 的问题类别是稳定契约，没有为它新增类别。）

**左栏只显示存在的目录。** 判定口径是 `os.Stat` 成功且是目录（`store.DirExists`，链接目录算存在）。存在与否不在每帧里现查：随扫描在后台判定、缓存在 `App.rootExists`。

- 当前目录：`applyScan` 复用它原来就要做的那次 `os.Stat`，不在 UI 线程上新增磁盘访问；
- 其他目录：`reload` 第二阶段与计数一起算（死掉的网络盘 `Stat` 可能卡很久，绝不能放在 UI 线程或每帧）；
- 没检查过的按存在处理，启动时左栏不会先空一下；不存在的目录同时清掉它的计数徽标；
- `DiscoverAgentRootsAsync` 新加的目录记为存在，`RemoveRootAsync` 删掉记录。

选中的目录不存在时，`ensureRootSelection` 切到列表里第一个**已确认存在**的目录（默认目录排第一，所以优先）。一个都不存在时保留现状，左栏给一句提示（`uiRootsNoneAvailable`），界面照常渲染。

**调试中发现的坑：不能在第一阶段做回退。** 最初我在 `reload` 第一阶段（当前目录扫完）就调用 `ensureRootSelection`。此时其他目录的「存在」还是上一次的旧缓存，所以在「所有目录都不存在」时，选中项会先跳到一个「以为还在」的默认目录，再发现它也没了，最后停在一个与原选择无关的位置——违反「一个都不存在时保留现状」。改成只在第二阶段（所有目录都拿到新鲜结果之后）决定，`TestAllRootsMissingShowsHintAndStaysUsable` 守着。

## 被改写的既有用例

`TestMissingExtraRootShowsBannerAndCanBeRemoved`（`internal/ui/roots_test.go`）原来要求「已添加的目录在磁盘上消失后显示提示，且仍可从列表里移除」。新需求是左栏只出现存在的目录，这条与它直接冲突，改写成 `TestMissingExtraRootIsHiddenAndSelectionFallsBack`：消失的目录不再出现、选中项回到默认目录、计数不留残留、`config.json` 仍记着它（只是隐藏，不是遗忘）。

## 验证了什么

全部用例用 `forEachLinkKind` 在 Windows 上同时跑联接点（`mklink /J`，不需要管理员）与目录符号链接两种；非 Windows 只跑符号链接。验证命令与原始输出见最终报告。本机有 C: 与 D:/E: 多个卷，跨卷用例（`TestMoveLinkAcrossVolumes`、`TestDisableLinkedSkillAcrossVolumes`）是真跑的，不是跳过。

## 没验证 / 已知限制

- 只在 Windows 上验证过；macOS / Linux 上同样的符号链接用例没有跑过。
- **联接点不能跨卷移动**：kura 数据目录与技能根目录不在同一个卷时，停用/移入回收站一个联接点技能会失败（技能留在原处），而不是悄悄复制内容。符号链接在系统允许用户创建时可以重建（Windows 需要开发者模式或提升权限）。
- **更新/历史恢复/导入元数据会写进链接指向的目录**，那可能属于别的程序（用户明确允许）。
- **已消失的目录没法从界面移除**：它被隐藏了，也就没有右键菜单。它仍在 `config.json` 里，目录回来后刷新就重新出现。
- 链接技能在卡片/详情上没有「这是链接」的标记，路径显示的是链接自己的路径，不是真实目录。
