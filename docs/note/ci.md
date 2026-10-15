# CI / CD 说明

## 工作流一览

### `.github/workflows/ci.yml`（名称 CI）

**触发**：每次 push、每个 pull_request。

**任务**：

- `test`：在 ubuntu-latest 和 windows-latest 各跑一遍 `gofmt -l .`（非空即失败）、`go vet ./...`、`go tool mygo vet .`、`go tool mygo i18n check`（catalogs 的 key、占位符类型与译文）、`go test ./...`、`CGO_ENABLED=0 go test ./...`、`go build ./...`。
- `build-cli`：ubuntu-latest 上交叉编译 `./cmd/kura`（`CGO_ENABLED=0`），目标 linux/amd64、linux/arm64、windows/amd64、darwin/amd64、darwin/arm64，每个目标上传一个 workflow artifact，名称 `kura-cli-<os>-<arch>`。

### `.github/workflows/release.yml`（名称 Release）

**触发**：push `v*` 标签。

**任务**：

- `draft-release`：校验 tag（`v` 去掉后）与 `mygo.json` 的 `version` 完全一致，不一致直接失败；再从 `CHANGELOG.md` 提取本版本的 `## <版本号>` 小节作为发布说明（缺该小节时 `mygo build` 会直接报错，这里提前拦住）；确认仓库 secret `MYGO_UPDATER_PRIVATE_KEY` 已配置；最后创建草稿 Release（`--notes-file`）。
- `build`：每个平台一行 matrix，并行执行 `go tool mygo build -platform <目标> -upload`：
  - macOS (arm64 / amd64)：macos-latest（dmg 制作需要 macOS）。
  - Windows (amd64)：ubuntu-latest，先 `apt-get install nsis`。
  - Linux (amd64+arm64)：ubuntu-latest，**一个 job 里一次构建两个架构**（`-platform "linux/amd64,linux/arm64"`）。两个架构共用同一份 `install.sh`；拆成两个并行 job 时，`gh release upload --clobber` 的删除+创建会在并发下撞 `HTTP 422 ReleaseAsset.name already exists`（v0.1.6 实际发生，重跑单 job 才通过）。放在同一个 job 里，mygo 的 `uploads()` 会按文件名去重，只传一份。
  - 每行把安装器与更新文件（`*Setup*.exe`、`*.dmg`、`*.deb`、 `kura-<版本>-<目标>.tar.gz`、`*.delta`、`update-<目标>.json`）上传到草稿 Release；Windows 行另外补传裸 `kura.exe`（`-upload` 不收它，但旧工作流有，保住便携版）。**清单由 mygo 最后上传**，避免应用读到指向缺失文件的清单。
- `publish`：`gh release edit --draft=false` 把草稿发布为正式 Release。

**发布内容**：Windows `kura Setup <version> amd64.exe` + `kura.exe`， macOS `kura <version> <arch>.dmg`，Linux `*.deb` 和 `kura-<version>-linux-<arch>.tar.gz`（内含 install.sh），以及供自更新用的签名更新压缩包、delta 与 `update-<目标>.json` 清单。

**依赖的 secret**：`MYGO_UPDATER_PRIVATE_KEY` = `go tool mygo keygen` 生成的 `mygo-update.key` 全文（单行 base64，不是文件路径）。它让每个平台的产物多出 **签名的**更新包与清单——没有它，`mygo build` 仍会成功但**不产出更新文件**，发出去的版本无法被自更新发现。所以工作流把它设为硬性前提（缺失即失败）。

**为什么用 `mygo build -upload` 而不是 `gh release upload`**：`mygo.json` 已配置 `updates`（`updates.github` + `updates.publicKey`），`-upload` 有明确上传目标，且由 mygo 自己保证「安装器先、清单最后」的上传顺序（`cmd/mygo/publish.go` 的 `uploads()` 把 manifest 单独放到最后一批）。手写 glob 很容易把顺序弄反，让应用读到指向不存在文件的清单。


## 如何发版

前提：仓库 secret `MYGO_UPDATER_PRIVATE_KEY` 已设置（内容为 `mygo keygen` 生成的 `mygo-update.key` 全文；keygen 默认写在 `%APPDATA%\mygo\update-keys\`）。没有它工作流会在 `draft-release` 阶段即失败。

1. 更新 `mygo.json` 里的 `version`（如 `0.1.0` → `0.2.0`），并同步 `internal/cli` 的 `Version` 常量（人工保持一致）。
2. 在 `CHANGELOG.md` 加 `## 0.2.0` 小节——**必需**，否则签名构建直接报错。
3. 提交并推送 main：`git commit -am "chore: bump version to 0.2.0" && git push`。
4. 打标签并推送：`git tag v0.2.0 && git push origin v0.2.0`。
5. 等待 Release 工作流跑完：先在 Actions 页面确认全部 job 绿色，然后到 Releases 页面检查草稿内容（安装器、`update-<目标>.json` 与更新包齐全，说明正确）。工作流的 `publish` job 会自动把草稿转正，无需手动操作。
6. 若某个平台失败：修复后 re-run 失败的 job。`gh release upload --clobber` （以及 mygo 的 `-upload`）会覆盖同名文件，`publish` 在全部成功后才执行。


## 注意

- 本项目无前端依赖：工作流中不出现 Bun/npm/Node。
- CLI 二进制与桌面应用是两个独立二进制；CI 的 CLI 交叉编译产物（`kura-cli-*`）仅作为 workflow artifacts 存档，不进 Release。
- Release 工作流只依赖一个 secret（`MYGO_UPDATER_PRIVATE_KEY`），其余用工作流内置 `GITHUB_TOKEN`（需 `permissions: contents: write`）。
