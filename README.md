# racomi

轻量本地 Agent Skill 管理器：从 skills.sh 或 Git 导入技能，按预设组织，并应用到 Codex、Claude Code、Cursor 的项目或全局目录。

- 支持 skills.sh 的 Git 与非 Git 托管来源（如 lark-doc），挑选安装或订阅全部技能，手动同步更新。
- 按来源选择全部已安装技能，与多个预设及直接选择的技能合并去重，支持多目录、复制与软链接部署。
- 提供本地快照、ZIP 备份恢复和 skills-manager 数据导入。
- 可选 S3 / Cloudflare R2 加密双向同步，跨设备共享来源、技能、预设及设置，并支持云端历史恢复。
- 支持侧边栏／顶部导航切换、明暗主题、文字缩放、界面字体，以及简体中文、繁體中文和 English。
- macOS 可开启轻量模式与开机自启。

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
- [第三方许可](frontend/public/THIRD_PARTY_NOTICES.txt)（Agent 品牌图标来自 Magpie／Lobe Icons；通用操作图标为本地 SVG，Markdown 解析使用 markdown-it）
