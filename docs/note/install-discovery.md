# 安装 GitHub 仓库时自动发现技能（修复记录）

日期：2026-02-14　分支：`fix-install-discovery`　worktree：`.worktrees/fix-discover`

## 缺陷

`kura add --github mattpocock/skills` 报
`store: 来源目录缺少 SKILL.md: …\kura-clone-NNN`。原因：这类「技能集合仓库」
的技能不在仓库根，而在 `skills/<category>/<name>/SKILL.md`（第 3 层），
旧逻辑只会把仓库根（或显式 `--sub-path`）当技能目录。

## 方案

### store 层（`internal/store/discover.go`，新增）

- `DiscoverSkills(dir)`：对已 clone 的本地仓库目录做只读发现，输出相对
  仓库根的 slash 路径。优先级：
  1. 仓库根有 `SKILL.md` → 整仓即一个技能（返回 `{"."}`）；
  2. `.claude-plugin/plugin.json` 的 `skills` 数组（形如 `./skills/x/y`）
     → 清单优先（作者显式声明要发布的技能，能排除 `in-progress` 等草稿；
     条目指向不存在路径时忽略而不是报错——陈旧声明不该卡死整个安装；
     清单 JSON 损坏时回退扫描）；
  3. 否则递归扫描：每个含 `SKILL.md` 的目录都是一个候选。跳过
     `.git`/`node_modules`/构建产物等（`skipDiscoverDir`）；深度上限 6、
     候选上限 64（`maxDiscoverDepth` / `maxDiscoverSkills`），超限返回
     `ErrTooManySkills`——截断报错比静默丢候选安全；命中技能后不再下钻。
- `CandidatesError`：多候选时由 `Add` 返回，`errors.As` 可还原 `Repo`
  与 `Paths`，`Error()` 给出可读中文清单。
- `Config.DiscoverRepoSkills(repo, ref)`：UI 用的只读入口，复用
  `resolveGitHub` 的 clone 与临时目录清理，不另起一套网络边界。clone 现在只取树，
  发现只检出各级 `SKILL.md` 与根目录的插件清单（`upstream.CheckoutDiscovery`），
  `Add` 选定技能之后才检出它的目录，仓库里与技能无关的文件不下载。
- `Add` 的行为变化（`add.go`）：GitHub 来源且**未显式给 `--sub-path`** 时
  先发现再装——恰好一个候选直接装（仓库根是技能时默认名取仓库名尾段，
  避免拿到随机的 `kura-upstream-NNN` 临时目录名，且元数据 `SubPath` 记录
  发现结果，后续 `Update` 可照常重拉）；多候选返回 `CandidatesError`；
  零候选沿用旧文案「来源目录缺少 SKILL.md」。显式 `--sub-path` 时一律
  照装，行为与旧版完全一致（向后兼容）。

### CLI（`internal/cli/commands.go`）

- `add` 遇到 `CandidatesError`：文本模式把候选清单打到 stderr；`--json`
  输出 `{"error", "repo", "candidates"}` 到 stderr。退出码沿用 1
  （操作失败）。`--help` 与 `--sub-path` 的 flag 说明已更新。

### UI（`internal/ui/install.go`）

- 只填 `owner/repo`（子目录既没填、也没从粘贴链接解析出来）时，点「安装」
  先走 `DiscoverRepoSkills`：唯一候选直接装；多候选弹窗保持打开、出现
  候选列表（`viewInstallCandidates`，ButtonBase 自建行，点击填进
  「子目录」输入框并高亮），用户再点「安装」即按显式子目录装；
  零候选/clone 失败显示原因。粘贴 tree/blob 链接解析出的子目录等同
  用户指定，不触发发现。发现与安装都走 `spawn` 后台 + `publish` 回
  主线程，切换目录时用 `gen` 丢弃过期结果。

## 测试

- `internal/store/discover_test.go`：三层布局扫描、清单优先/缺失条目/
  文件条目/损坏回退、深度上限、候选超限、仓库根即技能、单候选直装、
  多候选报错、零候选文案、显式 `--sub-path` 兼容。
- `internal/cli/discover_test.go`：多候选退出码 1、文本与 JSON 形状、
  挑选候选后装上、清单单候选直装。
- `internal/ui/install_discover_test.go`：弹窗多候选流程（列表出现 →
  点击候选 → 装上）、唯一候选直装、零候选报错、blob 链接子目录直接装。
  全部本地构造仓库（`symbolic-ref HEAD → refs/heads/main` 保证分支存在，
  不用 `init --branch`：部分 git 版本还不支持它）。
