# racomi 图标

源图采用已确认的黑白浣熊设计，保留耳朵、眼罩、两枚圆点眼睛和鼻子。

- `racomi-mark.png`：最初确认的白底原稿，保留用于设计追溯。
- `../macos/AppIcon.icon/Assets/racomi.png`：通过内置 imagegen 提取的透明黑色前景，经尺寸转换保存为 1024px。
- `../macos/AppIcon.icon/icon.json`：可编辑的 Apple 原生图标文档，定义明暗底色与前景色。
- `../appicon.png`、`../appicon-dark.png`：Icon Composer 导出的明暗图标，加入四周 10% 透明边距。
- `../macos/racomi.icns`、`../macos/Assets.car`：兼容图标和原生系统资源。

实现参考 [magpie 的图标生成与原生明暗处理](https://github.com/yetone/magpie/tree/612b759d2141cccb4f477645c9d765c0878476b0/build)。底板、圆角、系统渲染和导出均由 Apple Icon Composer 控制；各尺寸来自同一个 `.icon` 文档。运行 `make icons` 重新生成，使用 Xcode 的 `ictool`、`actool` 和 macOS 的 `sips`、`iconutil`。应用包保留原生资源控制 Dock 外观。

## 前景提取提示

使用内置 imagegen 工具，编辑目标为已确认的原稿，透明背景开启。最终采用的提示如下：

```text
Use case: background-extraction.
Asset type: foreground image layer for an Apple Icon Composer AppIcon.icon document.
Input image: exact approved racomi raccoon artwork.
Primary request: Extract this exact black raccoon line drawing onto a genuinely transparent background, removing ALL white pixels of the original background and all white interior face regions. Preserve all black line geometry, two black circular eye dots and rounded triangular nose without redesigning the animal.
Composition: square 1024 by 1024 RGBA transparent canvas. Place the front-facing raccoon centrally and optically balanced. The raccoon outline must occupy approximately 80 percent of canvas width; preserve the original aspect ratio. Smooth clean antialiased black line art, thin uniform strokes, no white matte around edges.
Strict constraints: the entire background and every non-black interior area must be transparent alpha. This is ONLY the black foreground raccoon glyph, with no white tile, no square, no rounded-square backing, no shadow, no color, no gradients, no text, no watermark, no checkerboard pattern. Preserve the two eyes.
```
