# racomi

轻量本地 Agent Skill 管理器：从 skills.sh 或 Git 导入技能，按预设组织，并应用到 Codex、Claude Code、Cursor 的项目或全局目录。

- 管理 Git 来源，挑选安装或订阅整库，手动同步更新。
- 组合多个预设，按需补充直接选择的技能，支持多目录、复制与软链接部署。
- 提供本地快照、ZIP 备份恢复和 skills-manager 数据导入。
- 可选 S3 / Cloudflare R2 加密双向同步，跨设备共享来源、技能、预设及设置，并支持云端历史恢复。
- 支持侧边栏／顶部导航切换、明暗主题、简体中文、繁體中文和 English。

## macOS 安装

需要 **macOS 13+**。从 [最新 Release](https://github.com/knothhe/racomi/releases/latest) 下载 ZIP 和同名 `.sha256` 文件：

| Mac | 下载文件 |
| --- | --- |
| Apple Silicon | `racomi-*-aarch64-macos.zip` |
| Intel | `racomi-*-x86_64-macos.zip` |

在下载目录校验后解压，将 **racomi.app** 拖入 `/Applications` 或 `~/Applications`：

```sh
shasum -a 256 -c racomi-*-macos.zip.sha256
```

使用发布包无需 Go、Node.js 或 Xcode；导入和刷新 Git 来源需要本机已安装 Git。

### 首次打开

发布包仅有 ad-hoc 签名，尚无 Developer ID 签名和 Apple 公证。若 macOS 提示“无法验证开发者”或“已损坏”，先确认官方来源和校验结果，再尝试打开一次，到“系统设置 → 隐私与安全性”选择“仍要打开”。可信下载仍因隔离属性受阻时，可针对应用清除一次标记：

```sh
xattr -dr com.apple.quarantine /Applications/racomi.app
```

若安装在用户目录，路径改为 `"$HOME/Applications/racomi.app"`。详见 [安装说明](build/macos/INSTALL.txt) 与 [Apple 官方说明](https://support.apple.com/en-us/102445)。

更新前先退出应用，再替换 `.app`；卸载时退出并移到废纸篓。两者均保留本地数据与已部署技能。如需移除部署，请在卸载前清空对应选择，等待自动保存后预览并应用。

### 从源码安装

需要 **Go 1.26.8+、Node.js 22.12+、npm、make、Git**，以及 Xcode 或 Command Line Tools（`xcode-select --install`）。macOS 不自带 Go 和 Node.js。在仓库目录运行：

```sh
make doctor      # 检查构建环境
make install     # 自动安装依赖、构建并安装应用
make uninstall   # 卸载应用和启动命令，保留数据与部署
```

默认安装到 `~/Applications/racomi.app`，启动命令为 `~/.local/bin/racomi`。更新时退出应用后重新运行 `make install`。可通过 `APP_DIR` 和 `BIN_DIR` 自定义位置，卸载时使用相同参数。

## 使用

1. 在“发现”浏览 skills.sh 默认榜单、输入关键词搜索，或预览 Git 仓库与本地技能目录，选择技能导入；搜索词留空可返回榜单，并可加载更多。Git 来源后续可在“来源”刷新清单并同步更新；本地目录导入为独立技能。
2. 在“项目”添加根目录，或在“全局”选择 Agent 目录，再选用预设；零散技能可展开“直接选择技能”补充。自定义目录在“设置”管理。
3. 选择自动保存，然后 **预览变化 → 应用**。当前启用技能为预设与直接选择的并集，相同技能只应用一次。

默认 `.agents/skills` 项目目录与 `~/.agents/skills` 用户目录由 Codex 和 Cursor 共享；Claude Code 使用 `.claude/skills`，Cursor 也有 `.cursor/skills` 专用目录。`.codex/skills` 保留为兼容选项，新配置建议使用共享目录。Cursor 还兼容读取默认的 Claude Code 与 Codex 目录，因此选择不同目录不代表技能隔离。目录依据 [Codex](https://learn.chatgpt.com/docs/build-skills#where-codex-loads-local-skills)、[Claude Code](https://code.claude.com/docs/en/skills) 和 [Cursor](https://cursor.com/docs/skills#skill-directories) 官方说明。

导航布局默认跟随窗口宽度：达到 1024px 时使用侧边栏，较窄时使用顶部导航；可在“设置 → 外观 → 导航布局”选择自动、侧边栏或顶部导航。顶部按钮可显示或收起侧边栏，快捷键为 macOS 的 Control + Command + S，其他平台的 Ctrl + Shift + S。拖拽侧边栏边缘或聚焦边缘后用键盘调整宽度，并可重置；主题、语言和导航布局可参与云同步，宽度保留在本机。布局与交互参考 [magpie main 的 74b30cb 版本](https://github.com/yetone/magpie/commit/74b30cb)。

刷新只更新来源清单，同步只更新本地技能；点击应用才修改目标目录。本地技能或预设更新后需重新应用。遇到同名未托管内容或本地修改时，普通应用会停止；预览会列出受影响的完整路径，可确认强制覆盖。强制覆盖会替换同名内容，也会删除已取消选择的本地修改，无法恢复，请先自行备份。重复技能名称、损坏内容或无效目录仍会阻止应用。

备份、恢复和 skills-manager 导入位于“设置 → 备份与迁移”，顶部导航模式可通过设置页的二级 tab 切换。本地快照目前不设数量或保留时间上限，也不会自动清理；未变化的技能文件共享存储。恢复后需重新选择技能并手动应用。数据默认保存在 `~/Library/Application Support/racomi`，可用 `RACOMI_DATA_DIR` 指定其他绝对路径。

### S3 / R2 双向同步

在“设置 → 备份与迁移 → S3 / R2 同步”连接自己的私有桶。R2 端点填写 `https://<ACCOUNT_ID>.r2.cloudflarestorage.com`，区域填 `auto`，开启路径式寻址；填写桶范围的 Access Key ID、Secret Access Key 和至少 12 个字符的加密口令，测试连接、保存后立即同步。AWS S3 与其他兼容服务也可使用。

各设备须使用相同端点、桶、前缀和加密口令。来源、实际技能文件、预设、主题／语言／导航布局及共享部署默认设置双向合并；同一记录同时修改或修改与删除冲突时，逐条选择保留版本。自动同步可选，仅在应用打开时运行。项目路径、绝对目录、部署记录和访问凭据保留在本机，同步后仍需手动应用技能。

云端历史可在设置中查看并恢复，恢复会创建新的同步版本供其他设备接收。远端内容经过客户端加密；口令和凭据存于本机私有配置文件，不进入共享数据和 ZIP，请额外保存口令。完整配置、权限示例、恢复与故障说明见 [S3 / R2 配置指南](docs/s3-r2-sync.md)，应用内“指南 → 同步与备份”也可离线阅读。

## 开发与发布

技术栈为 Go + Wails v3、TypeScript + Vite + CSS。开发环境要求同源码安装。

```sh
make deps        # 安装锁定依赖
make check       # 前端构建、类型检查、Go vet 和测试
```

在两个终端分别运行 `make frontend-dev` 和 `make dev`。前端默认端口为 `9245`，可通过 `VITE_PORT` 修改；Go 代码修改后需重启桌面应用。

浏览器验证入口为 [内存测试工作空间](http://127.0.0.1:9245/tests/browser.html?projects=40)，包含 40 个项目，不访问本机技能或部署目录，刷新即可重置。实际管理功能需要桌面后端。

```sh
make package                         # bin/racomi.app，本机架构
make release VERSION=0.2.0 BUILD_NUMBER=2
```

`make release` 在 `bin/releases/` 生成本机架构 ZIP、SHA-256 和安装说明。推送 `vX.Y.Z` 标签后，[Release workflow](.github/workflows/release.yml) 自动检查并发布 Apple Silicon、Intel 两种架构；带版本后缀的标签发布为预发布版。更多命令见 `make help`。

目前仅支持 macOS，没有自动升级。云同步使用用户自己的 S3 兼容存储，无需 racomi 账号。

## 更多资料

- [功能与数据规则](docs/requirements.md)
- [S3 / R2 同步配置指南](docs/s3-r2-sync.md)
- [安装与卸载说明](build/macos/INSTALL.txt)
- [图标设计与生成](build/design/README.md)
- [第三方许可](frontend/public/THIRD_PARTY_NOTICES.txt)（Agent 品牌图标来自 Magpie／Lobe Icons；通用操作图标为本地 SVG）
