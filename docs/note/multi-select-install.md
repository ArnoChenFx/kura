# 安装技能：一个仓库多选安装 + 默认名取 SKILL.md 的 name

本次改动只有两个需求，代码集中在 `internal/store/add.go`、`internal/store/skillmd.go`
与 `internal/ui/install.go`（外加测试与 `tools/capture-multiinstall.ps1` 真机验证脚本）。

## 需求 A：一个仓库里多选安装

- **store 新增 `AddMany(repo, ref string, specs []AddSpec) []AddResult`**（`internal/store/add.go`）。
  `AddSpec{SubPath, Name}` 描述「这个仓库里的哪一个技能、装成什么名字」，
  `AddResult{SubPath, Name, Result, Err}` 与入参一一对应。
  - 同一批只 `cloneUpstream` 一次；子目录树哈希用一次 `u.Trees(subs)`（一个 git 进程）取全，
    再一次 `u.Checkout(...)` 稀疏检出全部选中的目录。
  - 单项失败不影响其他项（同名冲突、上游没有该目录、子目录非法都只让自己失败），
    错误信息里带上子目录（`store: 安装 skills/... 失败: …`）。
  - 复用既有 `installDir`（复制 + 写元数据只此一处）；`Add`（单个）的行为、错误文案与
    `CandidatesError` 语义未改，CLI 与既有测试不受影响。
- **UI 候选列表改成多选**（`internal/ui/install.go`）：`installState.cands` 从 `[]string`
  变成 `[]installCandidate{path, on}`，一行一个 `myui.Checkbox`（默认全不选），
  顶部是「共 N 个技能 + 全选/全不选」；列表放在限高 8 行的滚动容器里（38 个技能的仓库
  不放会把主按钮顶出窗口）。主按钮文案随勾选数变化（0 个：`安装选中的技能`，N 个：
  `安装选中的 N 个`），点击走 `InstallSelectedAsync` → `store.AddMany`。
  - 全部成功：清空表单、重扫、选中最后一个装上的技能，状态行 `已安装 N 个技能`（N=1 时
    仍是 `已安装 <名字>`）。
  - 有失败：弹窗保持打开，状态行 `已安装 k 个，f 个失败`（全失败时 `安装失败`），
    明细留在 `install.err`；装成功的候选从列表里去掉，留下的清掉勾选，用户看清原因再重勾。
  - 「唯一候选自动装」与「手填子目录就装那一个」两条既有路径行为不变（手填子目录时
    主按钮回到 `安装`，装那一个）。

## 需求 B：默认名取 SKILL.md frontmatter 的 name

- 命名优先级：显式名字（UI「安装名称」/CLI `--name`）→ SKILL.md frontmatter 的 `name`
  → 来源目录名（仓库根本身是技能时退回仓库名，避免用到随机的临时 clone 目录名）。
- 读取复用 `internal/store/skillmd.go` 的扫描逻辑（`readFrontmatterField`，
  `readDescription` 与 `readSkillName` 是它的两个包装），不引 YAML 依赖。
- 名字必须能当目录名用，否则静默退回目录名：非空、不是 `.`/`..`、不含 `/` `\` 与 `..`、
  不含 `<>:"|?*` 与控制字符、不以空格或点结尾、不是 Windows 设备名（`con`、`CON.md` 等）。
  没有复用 `cli_support.go` 的 `ValidName`：那个函数守的是「拼进路径不越界」，
  放得过上面这些在 Windows 上建不出目录的名字（判据写在 `validSkillDirName` 的注释里）。
- 元数据的 `id` / `displayName` 跟着装进来的名字走；重名检测与报错文案不变。

## 追加修复：改了「仓库」输入框后旧候选失效（缺陷）

**缺陷**：候选列表与它对应的仓库脱钩。用 A 的仓库发现候选后把输入框改成 B（不点安装），
列表里仍是 A 的路径；勾一个点安装，`InstallSelectedAsync` 拿 A 的路径去 `AddMany(B, …)`，
报出莫名其妙的「仓库内不存在目录 skills/alpha」。

**修法**：`installState` 记下发现这批候选时的 `candRepo` / `candRef`（`installOptions()`
规范化后的 owner/repo 与 ref），新增纯判断 `candsLive()`：
`opts.Repo != "" && opts.Repo == candRepo && opts.Ref == candRef`。

- 判据是**比较**，不是「输入框 Changed 就清候选」：输入框每帧重建，Changed 随每次击键
  触发，打到一半就清会让列表乱抖；用户在列表里勾选时表单没变，候选不该动。
  副作用不在渲染路径里：`candsLive()` 只判断，真正的清空发生在动作里
  （`submitInstall` / `InstallSelectedAsync`）。附带：把仓库改回原值，候选与勾选自然又有效。
- 界面上 `candsLive()` 为假时**不渲染候选列表**，改为提示「仓库或分支已改，点「安装」
  重新发现技能。」，主按钮回到「安装」；`canInstall()` / `installButtonLabel()` 同判据。
- 点主按钮统一走新的 `submitInstall()`：失效候选先丢掉，再按表单走（候选有效就装勾选的，
  手填子目录就装那一个，否则重新发现）。导出的 `InstallSelectedAsync()` 自己也守一道：
  候选失效就直接丢掉并重新发现，绝不把它们送给别的仓库（防止绕过界面的调用）。

**测试**：`TestInstallModalRepoEditInvalidatesCandidates`（按缺陷复现的 4 步，最后一步
直接勾上内存里的旧候选再点安装，既考显示也考点击路径的保护）、
`TestInstallModalRefEditInvalidatesCandidates`（改分支/标签同理，并钉住「改回原值又有效」）。
两条在把 `candsLive()` 退回修复前行为后都会失败（含「拿 A 的候选去装 B」那条错误文案）。

`AddMany` 收到同一个子目录两次不去重（调用方的问题）：第二项报「根目录已存在同名技能」，
给了不同 `Name` 时是「同一份内容装成两个技能」的合法用法；由
`TestAddManyDuplicateSubPathFailsSecond` 钉住（见 `AddMany` 注释）。

## 追加修复 2：在途改表单不能再自动装（缺陷）

**缺陷 1**：发现还在后台跑的时候，用户把「仓库」从 A 改成 B。完成回调走单候选分支时
写的是 `s.subPath = cands[0]`（A 的路径）、再 `startInstall(a, s.installOptions())`——
`installOptions()` 读的是**当前表单**（B）。于是用户没点第二次安装就被装了一次：B 里
恰好有同名目录就装上了 B 的内容，没有就报「仓库内不存在目录 …」。

**修法**：完成回调在 `gen` 检查之后、`err` 检查之前，按**发起这次发现时的
`repo`/`ref`** 与当前表单显式比较（不能用 `candsLive()`：单候选时 `s.cands` 是空的，
`candsLive()` 恒为假），不一致就把这次结果**整份丢掉**——不填候选、不写 `subPath`、
不自动安装、不报错（状态行换成中性提示「仓库或分支已改，点「安装」重新发现技能。」
且 `statusErr=false`）；用户下一次点「安装」按新表单重新发现。

**为什么要丢掉而不是重试**：重试会把「改仓库」这一个动作变成一次隐式安装（用户只是
在改输入，没表达要装什么）；而且重试要再转一次后台，状态机更复杂。丢掉后下一次
显式动作再发现，语义最干净。

顺手把同一类的另一种在途改动也挡下：发现期间用户自己填了「子目录」时，单候选分支
不再覆盖他的输入、也不自动装（不让他的显式输入被后台结果搽掉；「手填子目录就装那
一个」这条既有行为不变）。

**测试**：`TestInstallModalInFlightRepoEditDiscardsDiscovery`（单候选，B 里故意放同名
目录——修复前会「装成功」）、`TestInstallModalInFlightRepoEditDropsMultiCandidates`
（多候选也整份丢掉，钉死行为）、`TestInstallModalInFlightSubPathEditSkipsAutoInstall`
（不覆盖用户填的子目录）。三条都把 `spawn` 换成「只记账不执行」，在发现回来之前改表单，
然后放行；退回修复前时三条都失败（含「装了 B 的 solo-a」与「spawn=2」）。

## 追加修复 2：无头截图用例退化

`TestRenderInstallCandidatesShot` 只填了 `cands`、没填 `candRepo`/`candRef`，加了
`candsLive()` 之后渲染出来的是失效提示而不是候选列表（上一轮报告里的
`headless-12-candidates.png` 也已经不存在：它当时被真机验证脚本的「清空输出目录」
顺带删了）。已修：

- 用例里 `candRepo`/`candRef` 与 `repo` 一起填，并在编码前断言帧内必须有
  `共 12 个技能` / `安装选中的 2 个` / `skills/engineering/tdd`（再退化会直接失败，而不是
  静静存下一张废图）；
- `tools/capture-multiinstall.ps1` 开头不再 `Remove-Item -Recurse` 整个输出目录，
  只清它自己造的东西（fixtures 与它要写的 PNG），不会再把旁边的截图删掉；
- 产物是临时的验证材料，不入库（见下面的验证一节）。

## 验证

- `go test ./...`（含 `-race`）、`CGO_ENABLED=0 go test ./...`、`go vet ./...`、
  `go tool mygo vet .`、`gofmt`（去掉 CRLF 后逐文件比对）、两个 build 全绿。
- 真机窗口：`powershell -NoProfile -ExecutionPolicy Bypass -File tools/capture-multiinstall.ps1
  -Exe build/windows-amd64/kura.exe -OutDir <输出目录>`
  （隔离 HOME + 本地 bare 仓库经 `insteadOf` 冒充 github，不联网；UI Automation 驱动真实窗口）。
  产物（写到 `<输出目录>`，属于临时验证材料、不入库）：`candidates-1-checked.png`（勾选一个、按钮
  `安装选中的 1 个`）、`candidates-2-checked.png` / `multi-installed.png`（勾两个一次装完）、
  `installed.png` 与 `installed-test-driven-development.meta.json`（目录名 `tdd` 的技能按
  frontmatter 的 `name` 装成 `test-driven-development`）、`stale-invalidated.png` /
  `rediscovered-b.png` / `stale-recovered.png`（改仓库后候选失效 → 重新发现 → 装上）。
- 无头渲染（`KURA_UI_SHOTS`）：

  ```bash
  KURA_UI_SHOTS=$PWD/<输出目录> go test -count=1 -run TestRenderInstallCandidatesShot ./internal/ui/
  ```

  产物 `install-candidates.png`（写到 `<输出目录>`）：12 个候选、`共 12 个技能`、
  勾选 2 个、主按钮 `安装选中的 2 个`、限高 8 行的滚动区。注意 `KURA_UI_SHOTS` 是相对
  测试包目录（`internal/ui`）解析的（`shots_test.go` 的既有约定），要写绝对路径才会
  落到仓库根的 `tools/` 下。

## 未验证

- 没有真的联网拉过 38 个技能的仓库（`mattpocock/skills`）再看真机窗口：候选列表的
  滚动只由 12 候选的无头渲染（`TestRenderInstallCandidatesShot` 的产物）与 3 候选
  的真机窗口截图覆盖。
- 在途改「分支/标签」的真机路径没单独跑：与在途改仓库同一套判据，由
  `TestInstallModalInFlightRepoEditDiscardsDiscovery`（仓库）+
  `TestInstallModalRefEditInvalidatesCandidates`（分支）覆盖。
- 没测 Windows 之外的平台（`validSkillDirName` 的 Windows 判据是保守起见额外加的，
  在别的平台上会多退几次到目录名）。
