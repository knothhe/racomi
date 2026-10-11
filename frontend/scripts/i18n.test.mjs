import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import ts from "typescript";
import { spawnSync } from "node:child_process";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const source = (name) => fs.readFileSync(path.join(root, "src", name), "utf8");

function evaluate(name, imports = {}, browser = {}) {
  const exports = {};
  const code = ts.transpileModule(source(name), { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText;
  new Function("exports", "require", "document", "window", "navigator", "localStorage", code)(exports, (key) => {
    assert.ok(Object.hasOwn(imports, key), `unexpected import: ${key}`);
    return imports[key];
  }, browser.document, browser.window, browser.navigator, browser.localStorage);
  return exports;
}

const en = evaluate("locales/en.ts").en;
const zhCN = evaluate("locales/zh-CN.ts").zhCN;
const zhTW = evaluate("locales/zh-TW.ts").zhTW;

function setup({ language = "en-US", saved = null, storageFails = false } = {}) {
  const listeners = {}, events = [], native = [];
  const store = new Map(saved ? [["racomi.locale", saved]] : []);
  const document = { documentElement: { lang: "", dataset: {} }, dispatchEvent(event) { events.push(event); } };
  const window = { addEventListener(name, callback) { listeners[name] = callback; } };
  const navigator = { language, languages: [language] };
  const localStorage = { getItem(key) { if (storageFails) throw new Error("unavailable"); return store.get(key) ?? null; }, setItem(key, value) { if (storageFails) throw new Error("unavailable"); store.set(key, value); } };
  const imports = { "./locales/en": { en }, "./locales/zh-CN": { zhCN }, "./locales/zh-TW": { zhTW }, "./api": { api: { setLocale(locale) { native.push(locale); return Promise.resolve(); } } } };
  if (fs.existsSync(path.join(root, "src/locales/backend.ts"))) imports["./locales/backend"] = evaluate("locales/backend.ts");
  const module = evaluate("i18n.ts", imports, { document, window, navigator, localStorage });
  return { ...module, listeners, events, native, store, navigator, document };
}

test("locale dictionaries have matching keys, no duplicate keys, and matching interpolation slots", () => {
  const keys = Object.keys(en).sort();
  assert.deepEqual(Object.keys(zhCN).sort(), keys);
  assert.deepEqual(Object.keys(zhTW).sort(), keys);
  const slots = (value) => [...new Set(value.match(/\{\w+\}/g) || [])].sort();
  for (const name of ["en", "zh-CN", "zh-TW"]) {
    const text = source(`locales/${name}.ts`);
    const names = [...text.matchAll(/^  ("(?:[^"\\]|\\.)*"):/gm)].map((match) => JSON.parse(match[1]));
    assert.equal(new Set(names).size, names.length, `${name}: duplicate keys`);
  }
  for (const key of keys) {
    assert.deepEqual(slots(en[key]), slots(zhCN[key]), `${key}: simplified slots`);
    assert.deepEqual(slots(en[key]), slots(zhTW[key]), `${key}: traditional slots`);
    assert.ok(en[key].length && zhCN[key].length && zhTW[key].length, `${key}: empty translation`);
  }
});

test("product names use the agreed vocabulary without old-name aliases", () => {
  const names = ["发现", "来源", "技能", "预设", "全局", "项目", "设置", "指南"];
  assert.deepEqual(names.map((name) => en[name]), ["Discover", "Sources", "Skills", "Presets", "Global", "Projects", "Settings", "Guide"]);
  assert.deepEqual(names.map((name) => zhCN[name]), names);
  assert.deepEqual(names.map((name) => zhTW[name]), ["發現", "來源", "技能", "預設", "全域", "專案", "設定", "指南"]);
  for (const dictionary of [en, zhCN, zhTW]) {
    for (const oldName of ["数据源", "技能库", "Preset", "技能组合", "使用指南"]) assert.equal(Object.hasOwn(dictionary, oldName), false, `${oldName}: obsolete alias`);
  }
});

test("all literal UI translation keys and authored guide text have catalog entries", () => {
  const missing = [];
  for (const name of ["main.ts", "settings.ts", "guide.ts"]) {
    const text = source(name);
    const tree = ts.createSourceFile(name, text, ts.ScriptTarget.Latest, true);
    const checkArgument = (node) => {
      if (ts.isStringLiteral(node) && !Object.hasOwn(en, node.text)) missing.push(`${name}: ${node.text}`);
      if (ts.isConditionalExpression(node)) { checkArgument(node.whenTrue); checkArgument(node.whenFalse); }
    };
    const visit = (node) => {
      if (ts.isCallExpression(node) && node.expression.getText(tree) === "t" && node.arguments[0]) checkArgument(node.arguments[0]);
      // Guide copy also lives in structured steps and FAQ entries.
      if (name === "guide.ts" && ts.isStringLiteral(node) && /[\u3400-\u9fff]/.test(node.text)) checkArgument(node);
      ts.forEachChild(node, visit);
    };
    visit(tree);
  }
  assert.deepEqual(missing, []);
});

test("system language distinguishes traditional Chinese regions and script tags", () => {
  const i = setup();
  for (const tag of ["zh-TW", "zh-HK", "zh-MO", "zh-Hant", "zh-Hant-TW"]) assert.equal(i.systemLocale(tag), "zh-TW");
  for (const tag of ["zh", "zh-CN", "zh-SG", "zh-Hans"]) assert.equal(i.systemLocale(tag), "zh-CN");
  assert.equal(i.systemLocale("fr-FR"), "en");
});

test("switching locale persists, updates html language and emits live change events", () => {
  const i = setup({ language: "en-US", saved: "zh-CN" });
  assert.equal(i.getLocale(), "zh-CN");
  assert.equal(i.t("技能"), "技能");
  i.setLocale("zh-TW");
  assert.equal(i.t("技能"), "技能");
  assert.equal(i.document.documentElement.lang, "zh-TW");
  assert.equal(i.store.get("racomi.locale"), "zh-TW");
  assert.equal(i.events.at(-1).detail.locale, "zh-TW");
  i.setLocale("en");
  assert.equal(i.t("技能"), "Skills");
  assert.equal(i.t("新版本 {0}", { 0: "a$b&c" }), "New version a$b&c");
  assert.equal(i.t("用户提供的技能名称"), "用户提供的技能名称");
  assert.equal(i.t("constructor"), "constructor");
});

test("system fallback, browser language changes and cross-window preference changes work", () => {
  const i = setup({ language: "zh-HK", saved: "bad-value" });
  assert.equal(i.getLocalePreference(), "system");
  assert.equal(i.getLocale(), "zh-TW");
  i.navigator.languages = ["en-GB"];
  i.listeners.languagechange();
  assert.equal(i.getLocale(), "en");
  i.store.set("racomi.locale", "zh-CN");
  i.listeners.storage({ key: "racomi.locale" });
  assert.equal(i.getLocale(), "zh-CN");
  i.setLocale("system");
  assert.equal(i.getLocale(), "en");
});

test("language works without local storage and native dialogs track the resolved locale", () => {
  const i = setup({ language: "zh-CN", storageFails: true });
  i.enableNativeLocaleSync();
  assert.equal(i.native.at(-1), "zh-CN");
  i.setLocale("en");
  assert.equal(i.getLocalePreference(), "en");
  assert.equal(i.native.at(-1), "en");
});

test("backend templates translate errors while preserving dynamic names, paths and diagnostic details", () => {
  const i = setup({ saved: "en" });
  assert.equal(i.tMessage("来源不存在"), "Source not found");
  assert.equal(i.tMessage("技能 用户自定义技能 的内容校验失败"), "Content verification failed for skill 用户自定义技能");
  assert.equal(i.tMessage("无法打开备份：/Users/用户名/我的备份.zip: permission denied"), "Cannot open backup: /Users/用户名/我的备份.zip: permission denied");
  assert.equal(i.tMessage("A custom diagnostic that has no translation"), "A custom diagnostic that has no translation");
  i.setLocale("zh-TW");
  assert.equal(i.tMessage("source not found"), "來源不存在");
  assert.equal(i.tMessage('skipped repository entry "AGENTS.md": symlink is not supported'), '已跳過儲存庫項目 "AGENTS.md"：不支援符號連結');
  assert.equal(i.tMessage('unsupported skill content at "skills/用户技能/link": symlink is not supported'), '技能內容 "skills/用户技能/link" 不受支援：含符號連結');
  assert.equal(i.tMessage("skills/用户技能/SKILL.md: missing YAML frontmatter"), "skills/用户技能/SKILL.md：缺少 YAML 中繼資料");
  assert.equal(i.tMessage("技能 用户自定义技能 的内容校验失败"), "技能 用户自定义技能 的內容驗證失敗");
  assert.equal(i.tMessage("File selection is available only in the desktop app"), "檔案選擇僅在桌面應用程式中可用");
  assert.equal(i.tMessage("__proto__"), "__proto__");
});

test("the authored UI transformer is idempotent after render-time localization", () => {
  const result = spawnSync(process.execPath, ["scripts/localize-ui.mjs", "src/main.ts", "--check"], { cwd: root, encoding: "utf8" });
  assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`);
});

test("backend templates have complete locales and matching placeholders", () => {
  const { backendMessages } = evaluate("locales/backend.ts");
  const slots = (value) => [...new Set(value.match(/\{\w+\}/g) || [])].sort();
  for (const [key, translations] of Object.entries(backendMessages)) {
    assert.deepEqual(Object.keys(translations).sort(), ["en", "zh-CN", "zh-TW"]);
    for (const [locale, value] of Object.entries(translations)) assert.deepEqual(slots(key), slots(value), `${locale}: ${key}`);
  }
});
