# CI / CD 说明

## 工作流一览

### `.github/workflows/ci.yml`（名称 CI）

**触发**：每次 push、每个 pull_request。

**任务**：

- `test`：在 ubuntu-latest 和 windows-latest 各跑一遍
  `gofmt -l .`（非空即失败）、`go vet ./...`、`go tool mygo vet .`、
  `go test ./...`、`CGO_ENABLED=0 go test ./...`、`go build ./...`。
- `build-cli`：ubuntu-latest 上交叉编译 `./cmd/kura`（`CGO_ENABLED=0`），
  目标 linux/amd64、linux/arm64、windows/amd64、darwin/amd64、darwin/arm64，
  每个目标上传一个 workflow artifact，名称 `kura-cli-<os>-<arch>`。

### `.github/workflows/release.yml`（名称 Release）

**触发**：push `v*` 标签。

**任务**：

- `draft-release`：校验 tag（`v` 去掉后）与 `mygo.json` 的 `version` 完全一致，
  不一致直接失败；一致则创建草稿 Release（`--generate-notes`）。
- `build`：每个平台一行 matrix，并行执行 `go tool mygo build -platform <目标>`：
  - macOS (arm64 / amd64)：macos-latest（dmg 制作需要 macOS）。
  - Windows (amd64)：ubuntu-latest，先 `apt-get install nsis`。
  - Linux (amd64 / arm64)：ubuntu-latest。
  每行把 `build/<os>-<arch>/` 下的产物上传到草稿 Release：
  `*.dmg`、`*Setup*.exe`、`kura.exe`、`*.deb`、`kura-*-linux-*.tar.gz`。
- `publish`：`gh release edit --draft=false` 把草稿发布为正式 Release。

**发布内容**：Windows `kura Setup <version> amd64.exe` + `kura.exe`，
macOS `kura <version> <arch>.dmg`，Linux `*.deb` 和
`kura-<version>-linux-<arch>.tar.gz`（内含 install.sh）。

**为什么不用 `mygo build -upload`**：`-upload` 需要 `mygo.json` 配置 `updates`
（GitHub 仓库或 S3）才有上传目标，kura 目前没有该配置（里程碑 6 才加自动更新），
所以用 `gh release create/upload` + 工作流内置 `GITHUB_TOKEN` 完成发布。

**未接入**：`MYGO_UPDATER_PRIVATE_KEY` 更新签名密钥属于里程碑 6，本工作流
不依赖任何 secret。

## 如何发版

1. 更新 `mygo.json` 里的 `version`（如 `0.1.0` → `0.2.0`）。
2. 提交：`git commit -am "chore: bump version to 0.2.0"`。
3. 打标签并推送：`git tag v0.2.0 && git push origin v0.2.0`。
4. 等待 Release 工作流跑完：先在 Actions 页面确认全部 job 绿色，
   然后到 Releases 页面检查草稿内容（产物齐全、说明正确），正式发布需由
   工作流的 `publish` job 自动完成（跑完即发布），无需手动操作。
5. 若某个平台失败：修复后 re-run 失败的 job，`gh release upload --clobber`
   会覆盖同名文件，`publish` 在全部成功后才执行。

## 注意

- 本项目无前端依赖：工作流中不出现 Bun/npm/Node。
- CLI 二进制与桌面应用是两个独立二进制；CI 的 CLI 交叉编译产物
  （`kura-cli-*`）仅作为 workflow artifacts 存档，不进 Release。
