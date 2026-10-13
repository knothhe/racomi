import type { AppSettings } from "./api";
import { api } from "./api";

export const textSizes = [100, 110, 125, 150] as const;
export const interfaceFonts = ["system", "sans", "serif", "mono"] as const;
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
  document.documentElement.style.setProperty("--font", fonts[font] || fonts.system);
  document.documentElement.dataset.interfaceFont = font;
  document.documentElement.style.setProperty("--ui-scale", String(size / 100));
  if (location.protocol === "wails:") {
    if (size !== nativeSize) {
      nativeSize = size;
      void api.setTextSize(size).catch(() => { nativeSize = 0; });
    }
  } else {
    document.documentElement.style.setProperty("--browser-scale", String(size / 100));
  }
}
