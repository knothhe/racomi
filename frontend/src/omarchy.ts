import type { OmarchyTheme } from "./api";

export type ThemePreference = "light" | "dark" | "system";
export type ThemeStyle = "default" | "omarchy";

const channels = (color: string) => [1, 3, 5].map((offset) => parseInt(color.slice(offset, offset + 2), 16));
function mix(a: string, b: string, amount: number) {
  const other = channels(b);
  return `#${channels(a).map((value, index) => Math.round(value * (1 - amount) + other[index] * amount).toString(16).padStart(2, "0")).join("")}`;
}
function luminance(color: string) {
  const [r, g, b] = channels(color).map((value) => {
    const v = value / 255;
    return v <= .04045 ? v / 12.92 : ((v + .055) / 1.055) ** 2.4;
  });
  return .2126 * r + .7152 * g + .0722 * b;
}
export function contrast(a: string, b: string) {
  const x = luminance(a), y = luminance(b);
  return (Math.max(x, y) + .05) / (Math.min(x, y) + .05);
}
function readable(color: string, backgrounds: string[], dark: boolean) {
  const target = dark ? "#ffffff" : "#000000";
  for (let step = 0; step <= 20; step++) {
    const candidate = mix(color, target, step / 20);
    if (backgrounds.every((bg) => contrast(candidate, bg) >= 4.5)) return candidate;
  }
  return target;
}

export function resolveTheme(preference: ThemePreference, style: ThemeStyle, palette: OmarchyTheme | null, systemDark: boolean) {
  const active = style === "omarchy" ? palette : null;
  const dark = preference === "system" ? (active?.dark ?? systemDark) : preference === "dark";
  const theme = dark ? "dark" : "light";
  let background = dark ? "#1a1a1e" : "#f4f4f6";
  const variables: Record<string, string> = {};
  if (!active) return { theme, background, variables };

  const colors = active.colors;
  let foreground = dark ? "#ededf1" : "#1c1c21";
  let surface = dark ? "#232327" : "#ffffff";
  if (dark === active.dark) {
    background = colors.background;
    foreground = colors.foreground;
    surface = colors.dark_background || mix(background, foreground, .04);
    Object.assign(variables, {
      "--bg": background, "--card": surface, "--card-2": mix(background, foreground, .02),
      "--pill": mix(surface, foreground, .06), "--pill-hover": mix(surface, foreground, .10),
      "--seg-track": mix(background, foreground, .06), "--seg-thumb": mix(surface, foreground, .13),
      "--line": mix(surface, foreground, .20), "--line-2": mix(surface, foreground, .12),
      "--fg": foreground, "--fg-2": mix(foreground, background, .15),
      "--muted": mix(foreground, background, .25), "--ctl-fg": mix(foreground, background, .25),
      "--faint": mix(foreground, background, .65), "--pop-bg": surface,
    });
  }
  const soft = mix(surface, colors.accent, dark ? .14 : .08);
  const selection = dark === active.dark && colors.selection && contrast(foreground, colors.selection) >= 4.5
    ? colors.selection : mix(surface, colors.accent, .15);
  const accent = readable(colors.accent, [background, surface, soft, selection], dark);
  Object.assign(variables, {
    "--accent": accent, "--accent-soft": soft, "--sel": selection,
    "--accent-fg": contrast("#ffffff", accent) > contrast("#000000", accent) ? "#ffffff" : "#000000",
    "--green-soft": mix(background, dark ? "#6fd99b" : "#137543", .12),
    "--red-soft": mix(background, dark ? "#f28b82" : "#c33737", .12),
    "--amber-soft": mix(background, dark ? "#f2b544" : "#a14b08", .12),
  });
  return { theme, background, variables };
}
