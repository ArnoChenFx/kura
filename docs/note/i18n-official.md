# i18n 迁移：自写 i18n → MyGo 官方 ARB catalogs

迁移前的方案：`internal/i18n`（`T`/`Tf`/`NewError`/`Errorf`/`Wrapf` + 进程级语言）加四个包各自的 `catalog.go`（`map[key]string`，中文与英文两份），共 422 条文案、461 个调用点。

迁移后的方案：MyGo v0.5.1 的官方方案——`locales/*.json`（ARB + ICU MessageFormat）加 `go tool mygo generate` 生成类型化 Go 包 `locales`（`locales.Xxx(...)`）。

额外收益：MyGo 自己的控件、菜单、文件对话框文案也本地化了（`locales/catalog_gen.go` 导入 `_ "github.com/egoist/mygo/locales/zh-Hans"`），这是自写方案做不到的。

## 两条实测硬约束（不要绕）

1. **key 必须扁平**：字母开头、只含字母数字与下划线。点分 key（`ui.status.installFailed`）会被 `mygo generate` 直接拒绝（`no library has the namespace ui`）。只有 `mygo.<key>` 前缀（改 MyGo 自己的文案）允许带点。
2. **只能有一个 catalogs 目录**（`mygo.json` 的 `i18n.dir`，这里是 `locales`），四份 `catalog.go` 必须合并成一份。

## 转换规则

- key：去掉点、后段首字母大写，拼成 camelCase。`ui.status.installFailed` → `uiStatusInstallFailed`；生成的函数名 = key 首字母大写（`locales.UiStatusInstallFailed`）。
- 占位符：`%s`/`%d`/`%q`/`%v` → ICU 命名占位。名字取语义（`name`/`path`/`count`/`reason`…），在 `@<key>.placeholders` 里声明类型（`String`/`int`/`float64`/`Object`）。
- `%d` 写 `{count, number, ::group-off}`：避免 ICU 默认加千分位，与旧的 `%d` 逐字一致。
- `%q` 的引号直接写进文案（`"{name}"`）。代价：旧 `%q` 会转义正文（命令名里带 `"` 或换行时输出 `\"`、`\n`），新文案是直接插值。只在畸形输入下看得出来，有意接受。
- `cli.usage.text`（`--help` 全文）里含字面 `{` `}`（JSON 形状示例），按 ICU 规则逐个转义成 `'{'` `'}'`。

## 代码侧保留的薄层

- `internal/i18n` 只留：语言解析（`Normalize`/`FromLocale`/`Resolve`/`System`）、进程语言口径（`Set`/`Current`）、错误层（`NewError`/`Errorf`/`Wrapf`/`WithSentinel`）、`CJKLiterals` 守卫工具。**错误身份与「文字现取」都保住了**：错误对象存 key（`errors.Is` 跨语言成立）与一个渲染闭包，`Error()` 每次按当时语言取文字。
- 语言：窗口应用由 `mygo.App.Locale()`/`SetLocale()` 决定（`main.go` 的 `app.SyncLang()`，用户切换走 `SetLangAsync`；菜单这类「设一次」的文字接 `mygo.App.OnLocaleChanged`）。CLI 没有 MyGo 应用，用 `i18n.Set` 换掉官方 locale func。
- `config.json` 的 `lang`（`system`|`zh`|`en`）继续有效，映射成 BCP 47（`zh-Hans`/`en`）。
- **「跟随系统」由 kura 自己解**（`internal/ui/app.go` 的 `localeForPref`）：固定语言直接是标签，`system` 走 `i18n.Resolve` + 系统 locale，判据与 CLI 一致。不要把 `system` 直接交给 MyGo 的 `SetLocale("")`——MyGo 要求语言标签匹配，系统的 `zh-TW`/`zh-Hant`/`zh-HK` 匹配不上唯一的 `zh-Hans` 候选，会落到 catalogs 的 source 语言，于是同一台机器上 CLI 中文、窗口英文（迁移前是中文，属行为回退）。代价：跟随系统不再在运行中实时变（改了系统语言要重启），每次启动会重新解一遍。

## 迁移统计

422 个旧 key → 421 个新 key：421 条一一对应，1 条有意删除（没有合并）。

- 对照表 `tools/i18n-migrate/mapping.tsv`（old_key / new_key / go_func / pkg，422 行 + 表头）；`tools/i18n-migrate/verify_catalog.py` 里另有一张 `DELETED` 表登记有意删除的 key，并断言它们确实不在新 catalogs 里。
- 删除的那条：`ui.markdown.moreLines`（「…（另有 N 行未显示）」）。它唯一的调用点在自写 markdown 渲染器的「代码块行数上限」里，随该渲染器一起退役（官方 `ui/markdown` 没有行数上限）。留着也没有调用点，`mygo i18n check` 不会报「没人用的 key」，所以由上面的 `DELETED` 表登记。
- 迁移完成后 `tools/i18n-migrate/` 只留可复用的回归件：`keys.json`（旧四份 `catalog.go` 的原始取值与动词序列）、`mapping.tsv`（old_key → new_key 对照表）、`verify_catalog.py`（把新 ICU 文案反格式化回旧 `%s`/`%d`/`%q` 后逐字比对，并登记有意删除的 key）。一次性的抽取/生成脚本（`extract.py`、`names.py`、`desc.py`、`build.py` 等）用完即删，见 `tools/README.md`。

## 守卫的替代

- key 一致性 / 占位符类型 / 译文缺漏：`go tool mygo i18n check`（已进 CI，见 `.github/workflows/ci.yml`）。
- 「非测试代码不硬编码中文」：保留等价测试 `internal/i18n/i18n_test.go` 的 `TestNoHardcodedCJK`（扫 `internal/` 的 Go 字面量），另有 `internal/ui` 的 `TestUISourceHasNoHardcodedCJK`（扫本包目录）。旧的 `catalog.go` 白名单随之删除。
  - 这两个守卫扫目录靠 `i18n.CJKLiterals`；它必须能扫**相对路径**（`internal/ui` 的守卫传的是 `"."`）。`WalkDir` 的第一个条目就是根目录自己，早期实现看到名字以 `.` 开头就 `SkipDir`，于是 `CJKLiterals(".")` 一个文件都不看、静默返回空——把中文探针塞进 `view.go` 那个守卫照样 PASS。现在根条目不再被当成「点开头目录」，并有自测 `TestCJKLiteralsFindsFilesInTheGivenDir` 钉住。
- 中文取值逐字不变：`internal/store/catalog_test.go` 与 `internal/cli/catalog_test.go` 的 `TestChineseTextIsVerbatim` 钉住若干条。
- 偏好到语言的映射：`internal/ui/catalog_test.go` 的 `TestLocaleForPref` 与 `TestLocaleForPrefFallsBackToATranslatedLanguage`。
- MyGo 自己的控件文案也跟着语言变：`internal/ui` 的 `TestMyGoControlsFollowTheWindowLocale`（查找栏的 Done / 完成）。

## 未验证

- 真实发布产物与签名更新链路未在本地重跑（需要 `MYGO_UPDATER_PRIVATE_KEY`）；与本次迁移无关（见 `docs/note/updates.md`）。
- Windows 上窗口不画菜单栏（`TitleBarHidden`），所以截图里看不到 MyGo 菜单的中英文；MyGo 自己的文案改由 headless 渲染的查找栏截图与上面的测试证明。
- macOS / Linux 的窗口未启动（本机做不到）；繁体中文系统的窗口语言由 `TestLocaleForPref` 覆盖，没有在真实繁体系统上跑过。
