# kura CLI JSON 输出契约（里程碑 3）

`kura --json` 的全部输出形状。**稳定承诺**：字段只增不改义、不删除； future UI（里程碑 2/4 的窗口版）可直接消费。来源代码：`internal/cli`。

## 约定

- 成功输出到 stdout（缩进两格 JSON，末尾换行）；
- `--json` 模式下错误输出到 stderr，形状恒为 `{"error": "..."}`；
- 路径一律绝对路径（`resolveConfig` 做 `filepath.Abs`）；
- 数组字段永远输出 `[]`，绝不输出 `null`；
- 环境变量 `KURA_ROOT`/`KURA_HOME` 等价于 `--root`/`--home`，flag 优先。

## list / scan

```json
{
  "root": "C:\\Users\\me\\.agents\\skills",
  "skills": [
    {
      "name": "notes-api",
      "state": "enabled",              // enabled | disabled | trashed
      "hasSkillMd": true,
      "dir": "C:\\Users\\me\\.agents\\skills\\notes-api",
      "sourceType": "github",          // 来自 meta.source.type
      "sourceUrl": "https://github.com/owner/repo.git",
      "ref": "main",
      "subPath": "skills/notes-api",
      "pluginName": "mattpocock-skills",
      "updatedAt": "2026-09-06T13:34:08Z",
      "contentHash": "sha256:3301…",
      "metaError": ""                  // 仅当 .kura-meta.json 损坏时出现
    }
  ]
}
```

带 `omitempty` 的字段在无值时不出现。trashed 条目的目录结构是 `trash/<name>/<stamp>`，容器本身没有 SKILL.md/元数据，因此 `hasSkillMd=false` 且不输出 source* 字段，属于正常现象。

## disable / enable / remove / restore / add / update

对应 `store.Result`：

```json
{
  "id": "beta",
  "state": "disabled",
  "dir": "C:\\Users\\me\\.kura\\disabled\\beta",
  "snapshotDir": "C:\\Users\\me\\.kura\\snapshots\\beta\\20260621-000000"
}
```

`snapshotDir` 仅有快照动作（remove/update）时出现。

### add 遇到多候选仓库（自动发现）

`add --github owner/repo` 不给 `--sub-path` 时由 store 自动发现仓库内的技能：唯一候选直接安装（正常输出）；多个候选时安装不执行，文本模式把候选清单写入 stderr；`--json` 模式给出结构化候选数组（退出码 1，stdout 无输出）：

```json
{
  "error": "store: 仓库 mattpocock/skills 里有 3 个技能，请用 --sub-path 指定其中一个：…",
  "repo": "mattpocock/skills",
  "candidates": [
    "skills/engineering/code-review",
    "skills/engineering/tdd",
    "skills/productivity/grill-me"
  ]
}
```

`candidates` 是相对仓库根的 slash 路径（已排序），每一项都可直接拿来当 `--sub-path` 用。该形状是既有「错误输出到 stderr」约定的扩展：多了一个 `repo` 与 `candidates` 字段，`error` 仍为人可读的完整描述。

## doctor

```json
{
  "root": "C:\\Users\\me\\.agents\\skills",
  "problems": [
    {
      "kind": "hash-drift",
      "skill": "alpha",
      "detail": "内容与元数据不符: 当前 sha256:bbf4…，记录 sha256:3301…（可用 kura update 刷新）"
    }
  ]
}
```

`kind` 的全部取值（稳定枚举）：

| kind | 含义 |
| --- | --- |
| `not-a-dir` | 根/disabled/trash 下与技能同级的普通文件，或目录本身不可读 |
| `no-meta` | 技能文件夹缺少 `.kura-meta.json`（或元数据缺 contentHash） |
| `meta-parse` | `.kura-meta.json` 存在但 JSON 损坏 |
| `hash-drift` | 内容哈希与元数据记录不符（内容漂移） |
| `no-skillmd` | 技能文件夹缺少 SKILL.md |
| `leftover` | disabled/trash 中的残留条目（提醒，可用 enable/restore 处理） |

`skill` 与具体技能无关时为空串。干净根目录输出 `"problems": []` 且退出码 0；发现任何问题时退出码 3。

## import-meta

```json
{
  "root": "C:\\Users\\me\\.agents\\skills",
  "lockPath": "C:\\Users\\me\\.agents\\.skill-lock.json",
  "lockFound": true,
  "dryRun": true,
  "wouldImport": ["locked-skill"]
}
```

`--dry-run` 时用 `wouldImport`（且绝不写文件）；实际导入用 `imported`。

`.skill-lock.json` 只描述默认根目录，所以 `--root` 指向界面里追加的根目录时（含 `--dry-run` 与 `--lock`）直接失败：退出码 1，stdout 无输出，错误按全局约定写到 stderr。

## update --check（里程碑 4）

```json
{
  "skill": "notes-api",
  "sourceType": "github",
  "commit": "3f9c1ab8d2…",               // local 来源无此字段
  "hasChanges": true,
  "oldHash": "sha256:3301…",
  "newHash": "sha256:7072…",
  "files": [
    {
      "path": "SKILL.md",
      "status": "changed",               // added | changed | removed
      "added": 2,
      "removed": 1,
      "oldHash": "<文件级 sha256 十六进制，无前缀>",
      "newHash": "<…>"
    }
  ],
  "unchanged": 4
}
```

`files` 只含差异文件（按路径排序），无变化时为 `[]`；`unchanged` 是两侧一致的文件数。`kura update --check` 绝不修改任何文件。退出码：有待更新 **3**，无变化 **0**，预览失败（技能不存在/元数据损坏/clone 失败等）**1**。

## update --check --all

```json
{
  "root": "C:\\Users\\me\\.agents\\skills",
  "results": [
    {"skill": "good", "check": { …同上，不含 skill 字段之外的部分… }},
    {"skill": "broken", "error": "store: … 元数据不可读 …"}
  ]
}
```

成功条目带 `check`（形状同 `update --check` 减去顶层 skill），失败条目带 `error`；`results` 恒为数组。任一技能有待更新 → 退出码 3。

## 退出码

| 码 | 含义 |
| --- | --- |
| 0 | 成功（doctor/--check 表示“干净/无待更新”） |
| 1 | 操作失败（技能不存在、同名冲突、IO 错误、--check 预览失败等） |
| 2 | 用法错误（未知命令、flag 解析失败、参数个数不对） |
| 3 | doctor 发现问题 / update --check 有待更新 |
