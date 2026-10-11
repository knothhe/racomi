import { api } from "./api";

export type ThemePreference = "light" | "dark" | "system";

const storageKey = "racomi.theme";
const systemTheme = window.matchMedia("(prefers-color-scheme: dark)");
const colors = { light: "#f4f4f6", dark: "#1a1a1e" };

function readPreference(): ThemePreference {
  try {
    const saved = localStorage.getItem(storageKey);
    if (saved === "light" || saved === "dark") return saved;
  } catch { /* Theme still works when browser storage is unavailable. */ }
  return "system";
}

let preference = readPreference();
let nativeReady = false;

function syncNative() {
  if (nativeReady) void api.setTheme(preference).catch(() => { /* Browser mode still keeps its CSS theme. */ });
}

function apply() {
  const theme = preference === "system" ? (systemTheme.matches ? "dark" : "light") : preference;
  document.documentElement.dataset.theme = theme;
  document.documentElement.dataset.themePreference = preference;
  document.documentElement.style.colorScheme = theme;
  document.documentElement.style.backgroundColor = colors[theme];
  document.querySelector<HTMLMetaElement>('meta[name="theme-color"]')?.setAttribute("content", colors[theme]);
  document.dispatchEvent(new CustomEvent("racomi:theme-change", { detail: { preference, theme } }));
  syncNative();
}

export function getThemePreference(): ThemePreference { return preference; }

export function enableNativeThemeSync() { nativeReady = true; syncNative(); }

export function setThemePreference(next: ThemePreference) {
  preference = next;
  try { localStorage.setItem(storageKey, next); } catch { /* Keep the choice for this session. */ }
  apply();
}

apply();
systemTheme.addEventListener("change", () => { if (preference === "system") apply(); });
window.addEventListener("storage", (event) => {
  if (event.key === storageKey || event.key === null) { preference = readPreference(); apply(); }
});
