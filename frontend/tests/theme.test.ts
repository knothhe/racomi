import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";
import ts from "typescript";
import { contrast, resolveTheme } from "../src/omarchy.ts";
import type { OmarchyTheme } from "../src/api.ts";

type ThemeModule = typeof import("../src/theme.ts");
const dark: OmarchyTheme = { dark: true, colors: { background: "#112233", foreground: "#eeeeee", accent: "#88aaff", dark_background: "#0f1e2d", selection: "#30445c" } };
const light: OmarchyTheme = { dark: false, colors: { background: "#eff1f5", foreground: "#4c4f69", accent: "#7287fd" } };

test("Omarchy determines system appearance and respects explicit light/dark", () => {
  for (const palette of [dark, light]) {
    const system = resolveTheme("system", "omarchy", palette, !palette.dark);
    assert.equal(system.theme, palette.dark ? "dark" : "light");
    assert.equal(system.background, palette.colors.background);
    assert.equal(system.variables["--fg"], palette.colors.foreground);
    const opposite = resolveTheme(palette.dark ? "light" : "dark", "omarchy", palette, palette.dark);
    assert.equal(opposite.background, palette.dark ? "#f4f4f6" : "#1a1a1e");
    assert.equal(opposite.variables["--bg"], undefined);
    for (const resolved of [system, opposite]) {
      assert.ok(contrast(resolved.variables["--accent"], resolved.variables["--accent-fg"]) >= 4.5, "readable primary button");
      assert.ok(contrast(resolved.variables["--accent"], resolved.variables["--sel"]) >= 4.5, "readable selected label");
    }
  }
  assert.deepEqual(resolveTheme("system", "default", dark, false), { theme: "light", background: "#f4f4f6", variables: {} });
  assert.deepEqual(resolveTheme("system", "omarchy", null, true), { theme: "dark", background: "#1a1a1e", variables: {} });
});

function harness(saved: Record<string, string> = {}) {
  const storage = new Map(Object.entries(saved));
  const properties = new Map<string, string>();
  const root = { dataset: {} as Record<string, string>, style: {
    backgroundColor: "", colorScheme: "",
    setProperty: (key: string, value: string) => properties.set(key, value),
    removeProperty: (key: string) => properties.delete(key),
  } };
  const timers: (() => void)[] = [];
  const media = Object.assign(new EventTarget(), { matches: false });
  const window = Object.assign(new EventTarget(), { matchMedia: () => media, setInterval: (callback: () => void) => { timers.push(callback); return timers.length; } });
  const document = Object.assign(new EventTarget(), { hidden: false, documentElement: root, querySelector: () => null });
  const native: unknown[][] = [];
  let next: OmarchyTheme | null = dark, fail = false, reads = 0;
  const exports = {} as ThemeModule;
  const code = ts.transpileModule(readFileSync(new URL("../src/theme.ts", import.meta.url), "utf8"), { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText;
  vm.runInNewContext(code, {
    exports, window, document, CustomEvent,
    localStorage: { getItem: (key: string) => storage.get(key), setItem: (key: string, value: string) => storage.set(key, value) },
    require: (name: string) => {
      if (name === "./omarchy") return { resolveTheme };
      assert.equal(name, "./api");
      return { api: { setTheme: async (...args: unknown[]) => { native.push(args); }, omarchyTheme: async () => { reads++; if (fail) throw Error("IPC unavailable"); return next; } } };
    },
  });
  return { api: exports, root, properties, storage, window, document, timers, native, reads: () => reads, setPalette: (value: OmarchyTheme | null) => { next = value; }, fail: () => { fail = true; } };
}

test("detection, live switches, appearance overrides and fallback remove stale CSS", async () => {
  const h = harness();
  await h.api.refreshOmarchyTheme();
  assert.equal(h.api.getThemeStyle(), "omarchy");
  assert.equal(h.root.dataset.theme, "dark", "Omarchy overrides the portal preference");
  h.api.setThemeStyle("omarchy");
  h.api.enableNativeThemeSync("linux"); h.api.enableNativeThemeSync("linux");
  assert.equal(h.timers.length, 1);
  h.setPalette(light); await h.api.refreshOmarchyTheme();
  assert.equal(h.root.dataset.theme, "light");
  assert.equal(h.root.style.backgroundColor, light.colors.background);
  h.api.setThemePreference("dark");
  assert.equal(h.root.dataset.theme, "dark");
  assert.equal(h.properties.has("--bg"), false);
  assert.deepEqual(h.native.at(-1), ["dark", "omarchy"]);
  h.setPalette(null); await h.api.refreshOmarchyTheme();
  assert.equal(h.properties.size, 0);
  assert.equal(h.api.getThemeStyle(), "omarchy", "fallback preserves preference");
  h.setPalette(dark); await h.api.refreshOmarchyTheme();
  assert.equal(h.properties.get("--bg"), dark.colors.background);
  h.api.setThemeStyle("default");
  assert.equal(h.properties.size, 0);
  assert.equal(h.storage.get("racomi.themeStyle"), "default");
});

test("an explicit default survives later detection and reloading", async () => {
  const h = harness();
  h.api.setThemeStyle("default");
  await h.api.refreshOmarchyTheme();
  assert.equal(h.api.getThemeStyle(), "default");
  assert.equal(h.properties.size, 0);
  const reloaded = harness({ "racomi.themeStyle": "default" });
  await reloaded.api.refreshOmarchyTheme();
  assert.equal(reloaded.api.getThemeStyle(), "default");
});

test("unchanged palettes and IPC failures retain the current theme without repainting", async () => {
  const h = harness(); await h.api.refreshOmarchyTheme();
  let updates = 0;
  h.document.addEventListener("racomi:theme-change", () => { updates++; });
  await h.api.refreshOmarchyTheme(); h.fail(); await h.api.refreshOmarchyTheme();
  assert.equal(updates, 0);
  assert.equal(h.properties.get("--bg"), dark.colors.background);
});

test("Linux polls once per interval and resumes hidden pages on visibility or focus", async () => {
  const browser = harness(); browser.api.enableNativeThemeSync();
  assert.equal(browser.timers.length, 0);
  const h = harness(); h.api.enableNativeThemeSync("linux");
  h.document.hidden = true; h.timers[0]();
  assert.equal(h.reads(), 0);
  h.document.hidden = false; h.document.dispatchEvent(new Event("visibilitychange"));
  await new Promise<void>((resolve) => setImmediate(resolve));
  assert.equal(h.reads(), 1);
  assert.equal(h.root.dataset.theme, "dark");
  h.setPalette(light); h.window.dispatchEvent(new Event("focus"));
  await new Promise<void>((resolve) => setImmediate(resolve));
  assert.equal(h.root.dataset.theme, "light");
});
