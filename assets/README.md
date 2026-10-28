# racomi 图标

当前图标是一只侧身的线条浣熊：圆亮的大眼睛、纽扣鼻和小微笑，用一对深色小手把一本封面带星光的技能书抱在胸前，身后是黑白相间的环纹尾巴。线条统一为 14 单位粗细，大块实心墨色只用在眼睛、鼻子、小手、脚和尾环上。

图标相关的资产都放在本目录：`design/` 是设计源文件，其余是由它们生成、供打包和运行时使用的资源。

## 源文件（`design/`）

所有 SVG 都使用 512 × 512 的 viewBox，底板为 424 单位的圆角方形。

- `racomi-icon.svg`、`racomi-icon-dark.svg`：浅色和深色应用图标。
- `racomi-symbol.svg`：透明背景的单色标志，墨色继承 `currentColor`。
- `racomi-avatar.svg`、`racomi-avatar-dark.svg`：小尺寸版，只有头部和环纹尾巴并放大。32 px 及以下的场景（favicon、应用标题栏品牌标）使用它，因为全身图在这些尺寸下认不出来。
- `racomi-icon-preview.png`：浅色、深色、单色、小尺寸版以及 64、32、16 px 的预览。
- `racomi-mark.png`：上一版几何浣熊头像的原稿，保留用于设计追溯。

## 派生资源

- `macos/AppIcon.icon/Assets/racomi.png`：Icon Composer 前景层。由 `design/racomi-symbol.svg` 的图形按 `viewBox="44 44 424 424"` 渲染为 1024 px 黑色透明 PNG，与 SVG 底板上的位置一致。`icon.json` 定义明暗底色与前景色。
- `appicon.png`、`appicon-dark.png`：1024 px，底板占 820 px，四周保留 10% 透明边距。
- `macos/racomi.icns`、`macos/Assets.car`：兼容图标和原生系统资源。
- `dev-icons/`：由 `scripts/dev_icon.py` 从 `appicon.png` 派生，见 `docs/release/README.md`。
- `frontend/public/appicon.png`：128 px，由 `appicon.png` 缩放得到。
- `frontend/public/brand-mark.svg`、`website/public/favicon.svg`：复制自 `design/racomi-avatar.svg`。
- `website/public/appicon.svg`、`website/public/appicon.png`：复制自 `design/racomi-icon.svg` 和 `appicon.png`。

macOS 上运行 `make icons`（脚本为 `build/macos/generate-icons.sh`），会由 Icon Composer 文档重新生成 `Assets.car`、`appicon*.png` 和 `racomi.icns`。这一步需要 Xcode 的 `ictool`、`actool` 以及 macOS 的 `sips`、`iconutil`。在没有 Xcode 的环境中，可以用 Chrome 等浏览器把 SVG 渲染成 PNG，再用 Pillow 写出 ICNS，但 `Assets.car` 只能在 macOS 上生成。

实现参考 [magpie 的图标生成与原生明暗处理](https://github.com/yetone/magpie/tree/612b759d2141cccb4f477645c9d765c0878476b0/build)。
