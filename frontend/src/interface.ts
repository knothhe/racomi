import type { AppSettings, InterfaceFontFamily } from "./api";
import { api } from "./api";
import { isDesktopRuntime } from "./desktop-runtime";

export const textSizes = [100, 110, 125, 150] as const;
export const interfaceFonts = ["system", "sans", "serif", "mono"] as const;
let installedFonts: InterfaceFontFamily[] = ["Arial", "Avenir Next", "Georgia", "Helvetica Neue", "Menlo", "PingFang SC", "Songti SC"].map((family) => ({ family, styles: [] }));
export function getInterfaceFonts() { return installedFonts; }
export async function loadInterfaceFonts() {
  const available = await api.listInterfaceFonts();
  if (available.length) installedFonts = available;
}
export function defaultFontStyle(family: string) {
  const styles = installedFonts.find((font) => font.family === family)?.styles || [];
  return styles.find((style) => /^(regular|normal|book|roman)$/i.test(style.name))?.postscriptName || "system";
}
const fonts = {
  system: '-apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
  sans: '"Helvetica Neue", Arial, "PingFang SC", "Microsoft YaHei", sans-serif',
  serif: '"New York", "Songti SC", "Noto Serif CJK SC", Georgia, serif',
  mono: 'ui-monospace, "SF Mono", Menlo, "PingFang SC", monospace',
};
let nativeSize = 0;
export function applyInterfaceSettings(settings: AppSettings) {
  const size = settings.textSize || 100;
  const font = settings.interfaceFont || "system";
  let face = document.querySelector<HTMLStyleElement>("#interface-font-face");
  if (!face) { face = document.createElement("style"); face.id = "interface-font-face"; document.head.append(face); }
  const style = settings.interfaceFontStyle || "system";
  const legacy = Object.hasOwn(fonts, font) ? fonts[font as keyof typeof fonts] : undefined;
  face.textContent = !legacy && style !== "system" ? `@font-face { font-family: "racomi-interface-choice"; src: local(${JSON.stringify(style)}); }` : "";
  const family = legacy || `${style !== "system" ? '"racomi-interface-choice", ' : ""}${JSON.stringify(font)}, ${fonts.system}`;
  document.documentElement.style.setProperty("--font", family);
  document.documentElement.dataset.interfaceFont = font;
  document.documentElement.style.setProperty("--ui-scale", String(size / 100));
  if (isDesktopRuntime()) {
    if (size !== nativeSize) {
      nativeSize = size;
      void api.setTextSize(size).catch(() => { nativeSize = 0; });
    }
  } else {
    document.documentElement.style.setProperty("--browser-scale", String(size / 100));
  }
}
