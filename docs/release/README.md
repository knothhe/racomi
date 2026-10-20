# 发布指南

在仓库根目录运行以下命令。发布配置见 [`.release.json`](../../.release.json)，命令入口见 [Makefile](../../Makefile)。

## 本地打包

```sh
make package                         # bin/racomi.app，本机架构
make dist                            # 本机架构 ZIP 和 SHA-256
make dist VERSION=0.2.0 BUILD_NUMBER=2 # 指定本地打包版本，不提交或推送
```

`make dist` 保留原来的本地发版包构建流程，在 `bin/releases/` 生成 ZIP、SHA-256，ZIP 中包含安装说明。`make package`、`make dist` 和 `make install` 默认读取 `frontend/package.json` 中的版本。

## 版本发布

版本发布使用 h2-semver-release 流程，唯一版本源为 `frontend/package.json`，升级时同步 `frontend/package-lock.json` 的包版本。先将发布脚本、配置及其他改动提交，保持工作区干净，在 `main` 分支运行：

```sh
# 首次发布当前版本（当前为 0.1.0，不创建版本提交）
make release-initial                         # 预览
make release-initial RELEASE_FLAGS=--apply   # 检查、打标签并推送

# 后续升级
make release VERSION_BUMP=patch                         # 预览
make release VERSION_BUMP=patch RELEASE_FLAGS=--apply   # 正式执行
# VERSION_BUMP 也支持 minor、major 或明确版本，例如 0.2.0-rc.1
```

默认 `VERSION_BUMP=patch`；不带 `--apply` 只预览，不运行检查或修改文件。正式执行前会检查工作区、版本及远程冲突，运行 `make check`；升级时更新版本并创建版本提交，然后创建 annotated tag，原子推送 `main` 和该标签到 `origin`，因此也会推送分支上尚未推送的提交。首次发版要求本地、远程及 GitHub 上均无同名发布；需要已登录的 GitHub CLI（`gh`）查询已有 Release，包括可见草稿。远程历史需已在本地，若落后或分叉，先获取并整合。

## GitHub Actions

推送 `vX.Y.Z`（也支持预发布及 build metadata）标签后，现有 [Release workflow](../../.github/workflows/release.yml) 校验标签与版本一致，继续检查、构建和发布 Apple Silicon、Intel 两种架构；带 `-` 预发布标识的标签发布为预发布版。`.release.json` 中 `github_release: false`，GitHub Release 与附件均由 Actions 创建，请保留此配置并避免使用本地 `--github-release` 覆盖它。`make install` 仍只构建并安装本机应用。

## 失败后续推

若标签已创建但推送失败，核对状态并解决原因后，按已有精确版本续推，不再次升级：

```sh
make release VERSION_BUMP=0.2.0 RELEASE_FLAGS="--resume --apply"
```

续推要求版本一致、annotated tag 仍指向当前 HEAD；首次标签须保留初始发布标记，升级须保留对应版本提交。可用 `make test-release` 在临时 Git 仓库中验证发布流程。更多命令见 `make help`。
