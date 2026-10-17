# 技能市场（skills.sh）：接口来源与未验证的部分

kura 的「技能市场」页面浏览 [skills.sh](https://skills.sh) 的榜单，点开技能看详情，并把它装进当前技能目录。本文记录每个接口的来源、它在哪里被核对过，以及**还没有在线核对过**的部分。

## 为什么是这些接口

- **不用官方 v1 接口**。skills.sh 的文档（`/docs/api`，经搜索摘要读到）把榜单放在 `GET /api/v1/skills`，参数 `view`（`all-time` / `trending` / `hot`）、`page`、`per_page`，但 v1 路由需要 Bearer 密钥（GitHub 上 vercel-labs/skills#1053 反馈没带密钥会得到 401，密钥要申请）。一个桌面应用不能要求用户先申请密钥，所以用的是**不带密钥、不带版本号**的 `/api/skills/...` 路径。
- 这些路径是**从两个开源客户端的代码里读来的**，不是官方文档化的契约。kura 的解析因此从宽：缺字段的条目跳过，结构不对的整页报错，错误原样显示在页面里（带重试）。

## 接口清单与来源

| 用途 | 请求 | 来源 | 核对状态 |
| --- | --- | --- | --- |
| 总榜 / 热度 / 趋势的一页 | `GET {base}/api/skills/{view}/{page}`，`page` 从 0 开始，`view` ∈ `all-time`、`hot`、`trending` | `all-time` 来自 mastra-ai/skills-api 的 `src/scraper/scrape.ts`（分页抓取 `https://skills.sh/api/skills/all-time/{page}`，响应 `{skills,total,hasMore,page}`）；`hot` 与 `trending` 是按同一命名推断的 | **未核对**：只有 `all-time` 有外部代码在用（快照日期 2026-01-30）；`hot`/`trending` 没有任何代码证据 |
| 技能详情：快照里的文件 | `GET {base}/api/download/{owner}/{repo}/{slug}`，`owner`/`repo` 小写，`slug` 由技能名按 `toSkillSlug` 算出 | vercel-labs/skills 的 `src/blob.ts`（`fetchSkillDownload`，响应 `{files:[{path,contents}],hash}`，`path` 相对技能目录） | **未核对**：CLI 在线上用它，但本环境无法访问 skills.sh，没有实际请求过 |
| 详情页的「在 skills.sh 上查看」 | `https://skills.sh/{owner}/{repo}/{skillId}` | vercel-labs/skills 的 `skills/find-skills/SKILL.md` 里的示例链接（`https://skills.sh/vercel-labs/agent-skills/react-best-practices`） | 路径形状有示例佐证，未在线打开核对 |
| 详情页的「在 GitHub 上查看」 | `https://github.com/{owner}/{repo}` | 技能的来源仓库 | 直接由来源推出，无需核对 |
| 装进目录 | `git clone` 来源仓库，按名字找技能（见下） | kura 自己的安装路径（`store.AddSkillByName`） | 本地测试覆盖（`httptest` 与本地镜像仓库），不联网 |

来源代码的版本：

- vercel-labs/skills @ `13e4063a1cf913f5606d57d42ab83a86f5001e04`（`src/find.ts`、`src/blob.ts`、`src/skills.ts`）
- mastra-ai/skills-api @ `6e6474c807a251526b8282fd53fd8227de92cbbe`（`src/scraper/scrape.ts`）

`vercel-labs/skills` 的 `find.ts` 还调用了 `GET /api/search?q=&limit=`（返回 `{skills:[{id,name,installs,source}]}`），kura 目前没用它：页面没有搜索框。

## 按名字安装的规则

榜单给的是 `owner/repo` 与技能名，不给仓库里的子目录。`store.AddSkillByName` 只 clone 一次，按 `npx skills add --skill` 的规则对应技能（不分大小写）：

1. SKILL.md 写了 `name` 的技能，只按 `name` 比（目录名不参与）；
2. 没写 `name` 的技能，按目录名比，仓库根的技能按仓库名比；
3. 恰好一个命中就装它；没有命中报 `ErrSkillNotInRepo`；多个命中报 `*CandidatesError`，页面原样显示候选列表，不替用户挑。

安装名取榜单上的名字；它不能当目录名用时退回 `installDir` 的默认命名。元数据（来源、commit、子目录树哈希）与「安装」弹窗装的完全一样，之后的「检查更新」照常工作。

## 在线核对的办法

在能访问 skills.sh 的机器上执行（这一步没有在本环境做过）：

```sh
curl -s "https://skills.sh/api/skills/all-time/0" | head -c 600
curl -s "https://skills.sh/api/skills/hot/0" | head -c 600        # 未核对
curl -s "https://skills.sh/api/skills/trending/0" | head -c 600   # 未核对
curl -s "https://skills.sh/api/download/vercel-labs/agent-skills/vercel-react-best-practices" | head -c 600
```

期望：前两条返回 `{"skills":[...],"total":...,"hasMore":...,"page":0}`，`skills` 的每条有 `source`、`skillId`、`name`、`installs`；第三条是 `{"files":[...],"hash":"..."}`。如果 `hot` 或 `trending` 返回 404，页面会在对应分段里显示「无法加载榜单：… HTTP 404」，其他榜单不受影响。核对之后，把结论写回本文的表格。

## 本环境的限制

开发沙箱的出口代理拒绝 `www.skills.sh`（`CONNECT tunnel failed, response 403`，`connect_rejected`），所以上面的接口没有一个在本环境里被实际请求过。测试全部用 `httptest` 本地服务器，按上表的形状构造响应，不联网。

## 页面行为

- 列表页：分段「总榜 / 热度 / 趋势」（默认总榜，即唯一有外部代码佐证的榜单）；「加载更多」接着读下一页；读失败保留原因并可重试；每个榜单各自缓存，切回来不重读。
- 详情页：头像、名字、来源、安装次数；`description`（SKILL.md frontmatter 里的）；两个外部链接（在后台用系统浏览器打开）；文件列表；SKILL.md 全文（沿用全文页的 Markdown 视图，跳过 frontmatter，超大源码截断）。快照按技能缓存，读失败可重试。
- 排列：列表与卡片网格共用技能区的排列偏好（`config.json` 的 `view`，在市场页面顶部切换）。卡片网格的列数随窗口宽度变；两种排列都是虚拟的，只构建看得见的行。
- 翻译：详情页的「原文 / 译文」开关与本地详情页是同一个全局开关，描述与 SKILL.md 走同一套翻译设置、缓存与失败规则（只显示、只在内存里、未配置不发请求）。市场的译文 key 带 `market:` / `mdoc:` 前缀，同名的本地技能不共用译文。榜单与卡片没有描述，不翻译。
- 安装：列表行与详情页的按钮都调用 `InstallMarketSkillAsync`，装进当前技能目录；同一时间只装一个；装的途中切换了目录，旧结果不改选中（同安装弹窗的 `gen` 守卫）。
- 榜单、快照都只在内存里，不落盘，也不写进任何技能目录。
