# racomi

轻量本地 Agent Skill 管理器：从 skills.sh 或 Git 导入技能，按预设组织，并应用到 Codex、Claude Code、Cursor 的项目或全局目录。

- 导入 skills.sh（Git 与非 Git 来源）技能，可挑选安装或订阅全部，手动同步更新；标记上游已移除或改名的技能。
- 来源、预设与单个技能合并去重，支持多目录、复制与软链接部署。
- 本地快照、ZIP 备份恢复，可导入 skills-manager 数据。
- 可选 S3 / Cloudflare R2 加密同步与云端历史恢复；R2 支持 OAuth 自动配置，无需 racomi 账号。
- 明暗主题、Omarchy 系统配色（Linux）、文字缩放、界面字体，简体中文／繁體中文／English。
- 轻量模式（托盘常驻）、开机自启，官方正式版支持签名应用内更新。

## 安装

从 [最新 Release](https://github.com/knothhe/racomi/releases/latest) 下载对应平台的包及同名 `.sha256`。导入和刷新 Git 来源需要本机安装 Git。

| 平台 | 文件 | 要求 |
| --- | --- | --- |
| macOS 13+ (Apple Silicon) | `racomi-*-aarch64-macos.zip` | — |
| Windows 10/11 x64 | `racomi-*-x86_64-windows.zip` | [WebView2 Runtime](https://developer.microsoft.com/microsoft-edge/webview2/) |
| Linux x86_64 | `racomi-*-x86_64-linux.tar.gz` | WebKit2GTK 4.1（Arch `webkit2gtk-4.1`，Debian／Ubuntu `libwebkit2gtk-4.1-0`，Fedora `webkit2gtk4.1`） |

- **macOS**：推荐用 Homebrew 安装：

  ```sh
  brew install --cask knothhe/tap/racomi
  ```

  之后的升级由应用内更新完成（`brew upgrade --cask --greedy racomi` 亦可）。也可以手动安装：`shasum -a 256 -c racomi-*-macos.zip.sha256` 校验后，将 `racomi.app` 拖入 `/Applications` 或 `~/Applications`。应用仅有 ad-hoc 签名、未经公证，若被拦截，到“系统设置 → 隐私与安全性”选择“仍要打开”，或执行 `xattr -dr com.apple.quarantine /Applications/racomi.app`。详见 [安装说明](build/macos/INSTALL.txt)。
- **Windows**：解压后运行 `racomi.exe`（目录需当前用户可写）。EXE 尚未做 Authenticode 签名。
- **Linux**：一条命令完成安装，适用于 Arch、Debian／Ubuntu、Fedora、openSUSE 及其衍生发行版：

  ```sh
  curl -fsSL https://github.com/knothhe/racomi/releases/latest/download/install.sh | sh
  ```

  脚本按 `/etc/os-release` 选择包管理器，补装缺少的 WebKit2GTK 4.1 与 Git（仅这一步使用 `sudo`），下载最新正式版并校验 SHA-256，安装到 `~/.local/bin/racomi`，同时写入桌面入口和图标。之后的升级由应用内更新完成。参数写在 `sh -s --` 后：`--version X.Y.Z` 安装指定版本，`--skip-deps` 跳过依赖安装（无法识别的发行版需先手动安装上表中的依赖），`--uninstall` 卸载（保留数据）；`RACOMI_BIN_DIR` 可修改安装目录。也可以手动安装：

  ```sh
  sha256sum -c racomi-*-x86_64-linux.tar.gz.sha256
  tar -xzf racomi-*-x86_64-linux.tar.gz
  install -m 755 racomi-*-x86_64-linux/racomi ~/.local/bin/racomi
  ```

  在“设置 → 外观 → 主题风格”可选择 Omarchy，未设置主题风格时，检测到有效配色会自动采用。运行中会跟随 Omarchy 换色；明暗模式仍可单独选择，配色不可用时回退为默认主题。

  托盘需要桌面支持 StatusNotifierItem（如 KDE、waybar）；开机自启使用 XDG autostart。

正式版在“设置 → 关于”检查更新（默认每天一次，可关闭）。卸载时删除应用即可，本地数据与已部署技能会保留；如需移除部署，请先在应用中清空选择并应用。

## 从源码构建

需要 **Go 1.26.8+、Node.js 22.12+、npm、GNU Make、Git**，另加平台工具链：

- **macOS**：Xcode 或 Command Line Tools（`xcode-select --install`）。
- **Windows**：x86_64 MinGW-w64 GCC 与 windres（如 `winget install BrechtSanders.WinLibs.POSIX.UCRT`）。没有 Make 时可用 `scripts/windows.ps1 -Action <target>`。
- **Linux**：C 编译器、pkg-config，以及 GTK 3 + WebKit2GTK 4.1 或 GTK 4 + WebKitGTK 6.0 开发包（自动检测，可用 `GTK=3|4` 指定）。

```sh
make doctor      # 检查构建环境
make install     # 构建并安装独立 Dev 版
make uninstall   # 卸载，保留数据与部署
make dist        # 生成 bin/releases 下的发布包与 SHA-256
```

默认安装 Dev 版，其应用标识、数据目录、自启与图标都与正式版分开；`CHANNEL=stable` 安装正式身份（不接入官方更新）。可用 `APP_DIR`、`BIN_DIR` 等自定义位置，卸载时传入相同参数。Windows 上软链接部署需要开发者模式，否则使用默认的复制部署。

## 开发

技术栈：Go + Wails v3，TypeScript + Vite。

```sh
make deps        # 安装锁定依赖
make check       # 前端构建、类型检查、Go vet 与测试
```

在两个终端分别运行 `make frontend-dev` 和 `make dev`（前端端口 `9245`，可用 `VITE_PORT` 修改）；Go 代码修改后需重启应用。浏览器中的 [内存测试工作空间](http://127.0.0.1:9245/tests/browser.html?projects=40) 不访问本机数据，刷新即重置。

## 更多资料

- [功能与数据规则](docs/requirements.md)
- [S3 / R2 同步配置指南](docs/s3-r2-sync.md)
- [R2 OAuth 发行者配置](docs/r2-oauth-poc.md)
- [发布指南](docs/release/README.md)
- [图标设计与生成](build/design/README.md)
- [第三方许可](frontend/public/THIRD_PARTY_NOTICES.txt)
