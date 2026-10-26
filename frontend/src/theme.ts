import { api, type OmarchyTheme } from "./api";
import { resolveTheme, type ThemePreference, type ThemeStyle } from "./omarchy";
export type { ThemePreference, ThemeStyle } from "./omarchy";

const storageKey = "racomi.theme";
const styleStorageKey = "racomi.themeStyle";
const systemTheme = window.matchMedia("(prefers-color-scheme: dark)");

function readPreference(): ThemePreference {
  try {
    const saved = localStorage.getItem(storageKey);
    if (saved === "light" || saved === "dark") return saved;
  } catch { /* Theme still works when browser storage is unavailable. */ }
  return "system";
}
function readStyle(): ThemeStyle | null {
  try {
    const saved = localStorage.getItem(styleStorageKey);
    if (saved === "default" || saved === "omarchy") return saved;
  } catch { /* Keep automatic detection without browser storage. */ }
  return null;
}

let preference = readPreference();
let style = readStyle();
let palette: OmarchyTheme | null = null;
let nativeReady = false;
let polling = false;
let refreshing = false;
let appliedVariables: string[] = [];

function syncNative() {
  if (nativeReady) void api.setTheme(preference, getThemeStyle()).catch(() => { /* Browser mode still keeps its CSS theme. */ });
}

function apply() {
  const { theme, background, variables } = resolveTheme(preference, getThemeStyle(), palette, systemTheme.matches);
  const root = document.documentElement;
  for (const name of appliedVariables) root.style.removeProperty(name);
  for (const [name, value] of Object.entries(variables)) root.style.setProperty(name, value);
  appliedVariables = Object.keys(variables);
  root.dataset.theme = theme;
  root.dataset.themePreference = preference;
  root.dataset.themeStyle = getThemeStyle();
  root.style.colorScheme = theme;
  root.style.backgroundColor = background;
  document.querySelector<HTMLMetaElement>('meta[name="theme-color"]')?.setAttribute("content", background);
  document.dispatchEvent(new CustomEvent("racomi:theme-change", { detail: { preference, theme } }));
  syncNative();
}

export function getThemePreference(): ThemePreference { return preference; }
export function getThemeStyle(): ThemeStyle { return style ?? (palette ? "omarchy" : "default"); }

export async function refreshOmarchyTheme() {
  if (refreshing) return;
  refreshing = true;
  try {
    const next = await api.omarchyTheme();
    if (JSON.stringify(next) !== JSON.stringify(palette)) { palette = next; apply(); }
  } catch { /* An IPC failure keeps the last palette; a missing file returns null. */ }
  finally { refreshing = false; }
}

export function enableNativeThemeSync(platform?: string) {
  nativeReady = true;
  syncNative();
  if (platform === "linux" && !polling) {
    polling = true;
    // Reopen the palette path to handle both file edits and active-theme symlinks.
    // The interval belongs to the WebView and disappears when it is released.
    window.setInterval(() => { if (!document.hidden) void refreshOmarchyTheme(); }, 2000);
    window.addEventListener("focus", () => { void refreshOmarchyTheme(); });
    document.addEventListener("visibilitychange", () => { if (!document.hidden) void refreshOmarchyTheme(); });
  }
}

export function setThemePreference(next: ThemePreference) {
  preference = next;
  try { localStorage.setItem(storageKey, next); } catch { /* Keep the choice for this session. */ }
  apply();
}

export function setThemeStyle(next: ThemeStyle) {
  if (style === next) return;
  style = next;
  try { localStorage.setItem(styleStorageKey, next); } catch { /* Keep the choice for this session. */ }
  apply();
  document.dispatchEvent(new CustomEvent("racomi:theme-style-change"));
}

apply();
systemTheme.addEventListener("change", () => { if (preference === "system") apply(); });
window.addEventListener("storage", (event) => {
  if (event.key === storageKey || event.key === styleStorageKey || event.key === null) {
    preference = readPreference(); style = readStyle(); apply();
    document.dispatchEvent(new CustomEvent("racomi:theme-style-change"));
  }
});
