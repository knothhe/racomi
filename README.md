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

## Windows 安装与构建

支持 **Windows 10/11 x64**，运行需要 Microsoft Edge WebView2 Runtime；缺少时从 [Microsoft 官方页面](https://developer.microsoft.com/microsoft-edge/webview2/) 安装。导入和刷新 Git 来源仍需 Git。Windows 版暂不提供 macOS 专属的轻量模式与开机自启。

发布包为 `racomi-*-x86_64-windows.zip`，解压后运行 `racomi.exe`。可用 PowerShell 的 `Get-FileHash <ZIP 文件路径> -Algorithm SHA256` 与同名 `.sha256` 文件比较。更新前退出应用并替换 EXE；删除便携版文件即可卸载，保留用户数据与已部署技能。当前 EXE 未做代码签名。

从源码构建需要 **Go 1.26.8+、Node.js 22.12+、npm、Git、GNU Make、x86_64 MinGW-w64 GCC 和 windres**。例如在 PowerShell 中安装构建工具：

```powershell
winget install --id GoLang.Go -e
winget install --id OpenJS.NodeJS.LTS -e
winget install --id Git.Git -e
winget install --id GnuWin32.Make -e
winget install --id BrechtSanders.WinLibs.POSIX.UCRT -e
```

安装后重新打开终端，并确保这些工具已加入 `PATH`（GnuWin32 Make 通常位于 `C:\Program Files (x86)\GnuWin32\bin`）。也可以用 MinGW 自带的 `mingw32-make` 执行相同目标。在仓库根目录运行：

```powershell
make doctor      # 检查 Windows 构建环境
make deps        # 首次构建安装依赖
make build       # 生成 bin/racomi.exe
make dist        # 生成 bin/releases 下的 ZIP 和 SHA-256
make install     # 自动安装依赖、构建、安装并创建开始菜单快捷方式
make uninstall   # 卸载已安装应用与快捷方式，保留数据和技能部署
```

`make` 根据当前操作系统自动选择 Windows 或 macOS 流程。Windows 默认安装路径为 `%LOCALAPPDATA%\Programs\racomi\racomi.exe`，可用 `make install APP_DIR="D:/Apps/racomi"` 自定义，卸载时使用相同参数。安装更新前请先退出应用。Windows 当前构建架构为 `amd64`，保留 CGO 以支持 SQLite 数据导入；EXE 已嵌入图标、版本信息和 DPI／长路径清单，无需附带 MinGW DLL。

没有 Make 时也可直接执行 `powershell -NoProfile -ExecutionPolicy Bypass -File scripts/windows.ps1 -Action install`，其他目标通过 `-Action build`、`-Action dist` 等选择。软链接部署需要 Windows 开发者模式或相应权限，普通账号可使用默认的复制部署；`make check` 在无软链接权限时会跳过对应测试。

Windows 的 `make check` 和 `make test` 显式使用 `-ldflags="-s=false -w=false"`，保留测试程序的符号表与 DWARF 调试信息。本机排查发现，Go 默认剥离调试信息的临时测试 EXE 会触发 Smart App Control 拦截，保留调试信息后可以通过检查。此设置仅用于测试构建，不修改系统安全策略或增加排除项；发布 EXE 仍使用原来的体积优化参数。其他设备若仍提示应用控制拦截，可检查事件查看器的 `Microsoft-Windows-CodeIntegrity/Operational` 日志（事件 3077）；这不是所有应用控制策略的通用放行保证，详见 [Microsoft 的说明](https://learn.microsoft.com/en-us/windows/apps/develop/smart-app-control/overview)。

## 开发与发布

技术栈为 Go + Wails v3、TypeScript + Vite + CSS。开发环境要求同源码安装。

```sh
make deps        # 安装锁定依赖
make check       # 前端构建、类型检查、Go vet 和测试
```

在两个终端分别运行 `make frontend-dev` 和 `make dev`。前端默认端口为 `9245`，可通过 `VITE_PORT` 修改；Go 代码修改后需重启桌面应用。

浏览器验证入口为 [内存测试工作空间](http://127.0.0.1:9245/tests/browser.html?projects=40)，包含 40 个项目，不访问本机技能或部署目录，刷新即可重置。实际管理功能需要桌面后端。

本地打包、首次发版、版本升级及失败续推见 [发布指南](docs/release/README.md)。

目前支持 macOS 和 Windows x64，没有自动升级。云同步使用用户自己的 S3 兼容存储，无需 racomi 账号。

## 更多资料

- [功能与数据规则](docs/requirements.md)
- [S3 / R2 同步配置指南](docs/s3-r2-sync.md)
- [安装与卸载说明](build/macos/INSTALL.txt)
- [图标设计与生成](build/design/README.md)
- [第三方许可](frontend/public/THIRD_PARTY_NOTICES.txt)（Agent 品牌图标来自 Magpie／Lobe Icons；通用操作图标为本地 SVG，Markdown 解析使用 markdown-it）
