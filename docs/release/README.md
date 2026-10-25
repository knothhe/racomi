# 发布指南

在仓库根目录运行以下命令。发布配置见 [`.release.json`](../../.release.json)，命令入口见 [Makefile](../../Makefile)。

## 本地打包

```sh
make package                         # bin/dev/racomi Dev.app，本机架构
make dist                            # Dev ZIP 和 SHA-256
make dist CHANNEL=stable             # 本地正式身份包，不接入官方更新
make dist CHANNEL=stable VERSION=0.1.1 BUILD_NUMBER=2 # 指定本地包版本，不提交或推送
```

上述命令自动识别宿主操作系统：Windows x64 上 `make package` 生成 `bin/racomi.exe`，`make dist` 生成 `racomi-<版本>-x86_64-windows.zip` 及 `.sha256`，包含安装说明与第三方许可。Windows 同样默认 `CHANNEL=dev`，注入版本、构建号、提交、工作区状态、通道和执行环境，`make install` 默认创建独立 racomi Dev 安装与快捷方式；构建依赖及安装路径见 [README](../../README.md#windows-安装与构建)。`BUILD_NUMBER` 用于 Windows 文件版本的第四段，须为 0–65535。

`make dist` 保留原来的本地发版包构建流程，在 `bin/releases/` 生成 ZIP、SHA-256，ZIP 中包含安装说明。`make package`、`make dist` 和 `make install` 默认读取 `frontend/package.json` 中的版本。

## 版本发布

版本发布使用 h2-semver-release 流程，唯一版本源为 `frontend/package.json`，升级时同步 `frontend/package-lock.json` 的包版本。先将发布脚本、配置及其他改动提交，保持工作区干净，在 `main` 分支运行：

```sh
# 首次发布当前版本（不创建版本提交）
make release-initial                         # 预览
make release-initial RELEASE_FLAGS=--apply   # 检查、打标签并推送

# 后续升级
make release VERSION_BUMP=patch                         # 预览
make release VERSION_BUMP=patch RELEASE_FLAGS=--apply   # 正式执行
# VERSION_BUMP 也支持 minor、major 或明确版本，例如 0.1.2-rc.1
```

当前沿用 `0.1.x`，兼容改动默认使用 patch。默认 `VERSION_BUMP=patch`；不带 `--apply` 只预览，不运行检查或修改文件。正式执行前会检查工作区、版本及远程冲突，运行 `make check`；升级时更新版本并创建版本提交，然后创建 annotated tag，原子推送 `main` 和该标签到 `origin`，因此也会推送分支上尚未推送的提交。首次发版要求本地、远程及 GitHub 上均无同名发布；需要已登录的 GitHub CLI（`gh`）查询已有 Release，包括可见草稿。远程历史需已在本地，若落后或分叉，先获取并整合。

## GitHub Actions

推送 `vX.Y.Z`（也支持预发布及 build metadata）标签后，[Release workflow](../../.github/workflows/release.yml) 校验标签与版本一致，检查、构建并发布 macOS Apple Silicon、Linux x86_64 与 Windows x64 包；macOS Intel、Windows arm64 和 Linux arm64 暂不进入此工作流；带 `-` 预发布标识的标签发布为预发布版。`.release.json` 中 `github_release: false`，GitHub Release 与附件均由 Actions 创建，请保留此配置并避免使用本地 `--github-release` 覆盖它。`make install` 仍只构建并安装本机应用。

仓库只有这一个工作流，只由版本 tag 触发；普通 push 与 PR 不运行 CI，提交前请在本地运行 `make check`。

## 失败后续推

若标签已创建但推送失败，核对状态并解决原因后，按已有精确版本续推，不再次升级：

```sh
make release VERSION_BUMP=0.1.2 RELEASE_FLAGS="--resume --apply"
```

续推要求版本一致、annotated tag 仍指向当前 HEAD；首次标签须保留初始发布标记，升级须保留对应版本提交。可用 `make test-release` 在临时 Git 仓库中验证发布流程。更多命令见 `make help`。

## 构建身份与自动更新

macOS 本地 Make 默认 `CHANNEL=dev`，输出位于 `bin/dev/`。`CHANNEL=stable` 和 `CHANNEL=preview` 使用正式应用身份，分别输出到对应通道目录；本地源码构建仍标记为 `executor=local`。构建时从唯一版本源派生版本，并注入构建号、实际提交、工作区状态、通道和执行环境。可执行文件 `--build-info`、最终 Info.plist 和设置中的关于页对应同一份构建信息。不要手动将本地包标记为 CI 成品；正式 CI 构建要求工作区干净。

稳定 Release 使用 Sparkle 2.9.5，下载版本与 SHA-256 固定在 `build/updates.json`。它也是更新 Feed URL 和公钥的唯一配置来源。更新复用普通 Apple Silicon ZIP，默认自动检查、关闭自动下载，安装由原生窗口确认，退出前处理保存和未提交编辑。Dev 没有正式 Feed，预览版和本地源码构建使用手动更新；不支持的平台、未安装的副本或身份不匹配会显示原因。

macOS 安装确认后由 Sparkle 请求正常退出，`Service.ShouldQuit` 完成保存屏障后才允许退出和替换应用。取消或保存失败时保留当前编辑；从“检查应用更新…”重新打开待安装窗口可再次尝试。不要用 `shouldPostponeRelaunchForUpdate` 持有初次安装回调：Sparkle 的初次安装按钮只调用一次，保存取消后会留下没有动作的按钮。原生委托与前端退出重试回归测试包含在 `go test ./internal/desktop` 和 `npm test --prefix frontend` 中。

`SPARKLE_PRIVATE_KEY` 已配置为仓库 Actions Secret，只交给 tag 构建中的签名步骤。公钥可以提交；私钥不得加入源码、artifact 或日志。本机备份在 macOS 钥匙串的 Sparkle `com.knothhe.racomi` account 中，不依赖共享默认签名账户。更换密钥须设计旧客户端的信任迁移，不能直接覆盖公钥。

CI 先完成包签名检查，再使用 Sparkle 的 `generate_appcast --maximum-deltas 0` 对完整 ZIP 签名，生成并签署 `appcast.xml`。发布前以嵌入的公钥校验 ZIP 的 Ed25519 签名、版本、构建号、下载地址和大小。发布 job 收齐确切附件后创建草稿，全部就绪才公开，并回读公开附件比较内容。重跑仅恢复同 tag 的草稿，已存在的附件内容不同或 Release 已公开时停止，不覆盖历史资产。稳定 Feed 使用最新稳定 Release 的 `appcast.xml` 附件，预览 Release 不参与。

Linux job 在 Ubuntu 24.04 上以 GTK 3 + WebKit2GTK 4.1 运行 `make check` 并打包 `racomi-*-x86_64-linux.tar.gz`。Windows job 在 Windows 2022 runner 上使用 UCRT64 MinGW GCC／windres，保留 CGO 并静态链接编译器运行库，另做原生窗口生命周期验收和产物检查。

Sparkle 签名工具只在 macOS 上提供，因此 macOS job 依赖另两个 job：稳定 tag 时下载 Linux 与 Windows 包、核对 SHA-256，由 `scripts/create-update-feed.sh` 用同一私钥签名，经 `scripts/update-feed.mjs` 生成并以嵌入公钥校验 `update.json`（版本、提交、下载地址、大小、SHA-256 与签名）。`update.json` 按 `linux-amd64`／`windows-amd64` 列出资产，Linux 与 Windows 客户端读取最新稳定 Release 的同一份文件，下载地址固定为对应 tag 的附件；macOS 仍使用 Sparkle 的 `appcast.xml`。私钥只进入 macOS 签名步骤。发布 job 要求三平台包齐备、再次校验 Feed 后才公开草稿。

Windows 客户端还核对解压后 EXE 的版本、提交、通道、架构与 CI 身份，保存后退出；独立助手等待旧进程释放 EXE 后使用 Windows ReplaceFile 创建备份并替换，再重启，失败保留／恢复旧程序。更新目录须可写；助手错误记录在同目录 `.racomi-update-helper-*.exe.log`。此签名不等于 Authenticode，当前 EXE 未接入 Authenticode。

更新签名独立于 Apple Developer ID／公证。当前宿主包仍为 ad-hoc 签名，尚未接入 Developer ID 和公证。`update.json` 从 v0.1.6 开始发布；更早的 Linux／Windows 版本读取旧 Feed 名称，需手动安装一次新版本。

## 本地验证

`make check` 覆盖前端、字典、保存队列、Go 服务、安装归属与失败回退、Dev／正式共存、自启登记清理及更新签名拒绝。安装测试使用临时 HOME 和目标目录。原生验证显式指定 `RACOMI_DATA_DIR` 的临时资料，独立资料目录使用独立单实例锁，避免唤起日常实例；主应用的系统偏好与缓存还需通过测试子进程的 `CFFIXED_USER_HOME` 隔离。Sparkle 独立安装助手仍可能使用系统用户缓存中的随机暂存目录，按安装日志确认归属。浏览器内存工作空间只能验证前端。原生关于入口不依赖技能库初始化成功，损坏数据不会被自动重置。

Launch Services 不保留自定义资料环境。使用 `RACOMI_DATA_DIR` 或 `CFFIXED_USER_HOME` 的实例会禁止自动重启；Sparkle 2.9.5 也会因此推迟立即安装。隔离验收时下载并确认更新后，正常退出以完成已暂存更新的安装，再用原来的环境重新启动。普通正式安装仍使用 Sparkle 的安装与自动重启流程。

2026-10-24 已完成 macOS Apple Silicon 的公开发布与隔离升级验收：

- [v0.1.2 发布 CI](https://github.com/knothhe/racomi/actions/runs/40089737596) 的检查、arm64 构建、签名和发布均通过；[正式 Release](https://github.com/knothhe/racomi/releases/tag/v0.1.2) 包含完整 ZIP、SHA-256 和签名 Feed。
- 基线来自 [手动 CI artifact](https://github.com/knothhe/racomi/actions/runs/40088558429)：0.1.1、build 4、提交 `b1c2f84`，包含原生状态 JSON 布尔值修复。它不是较早发布的 v0.1.1 包。
- 在 `.cache/update-verification/` 的独立 HOME、偏好及资料中，通过应用默认公开 Feed 检出 0.1.2，下载并由 Sparkle 验签、解包、暂存；确认后正常退出，安装到同一隔离应用路径。
- 安装后构建信息为 0.1.2、build 5、提交 `e011bd0`、`stable / ci / darwin / arm64`，深层代码签名校验通过。另用固定公钥独立验证公开 Feed 与同一 ZIP 的 Ed25519 签名、版本、构建号、地址和大小，并确认篡改后的 Feed 验签失败。
- 用原环境重新启动后，关于页显示新版本，Light 主题、125% 字体及完整测试状态保持一致，再次检查提示已是最新。验收未覆盖普通资料环境的自动重启、Developer ID 或公证。

Dev 图标由 `scripts/dev_icon.py` 从正式 PNG 派生，提交的生成资源供无 Python 环境的普通构建使用。修改图源时在有 Python 3.9+／Pillow 10.1+ 的环境运行：

```sh
python3 scripts/dev_icon.py --input build/appicon.png --output-dir build/dev-icons --formats png,icns --sizes 1024 --force
```
