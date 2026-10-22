import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";
import ts from "typescript";

function evaluate(name: string, imports: Record<string, unknown> = {}) {
  const exports = {};
  const source = readFileSync(new URL(`../src/${name}`, import.meta.url), "utf8");
  const code = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText;
  vm.runInNewContext(code, {
    exports,
    require: (name: string) => { assert.ok(Object.hasOwn(imports, name), `unexpected import: ${name}`); return imports[name]; },
    document: { documentElement: { dataset: {} }, dispatchEvent: () => {} },
    window: { addEventListener: () => {} },
    navigator: { language: "en" },
    localStorage: { getItem: () => null, setItem: () => {} },
    CustomEvent: class {},
  });
  return exports;
}

function setup() {
  return evaluate("i18n.ts", {
    "./locales/en": evaluate("locales/en.ts"),
    "./locales/zh-CN": evaluate("locales/zh-CN.ts"),
    "./locales/zh-TW": evaluate("locales/zh-TW.ts"),
    "./locales/backend": evaluate("locales/backend.ts"),
    "./api": { api: { setLocale: () => Promise.resolve() } },
  }) as { setLocale: (locale: string) => void; tMessage: (message: string) => string };
}

test("skills.sh selection diagnostics distinguish missing, ambiguous, and legacy index entries in all locales", () => {
  const i = setup();
  const missing = 'skills.sh skill "polish" was not found in the current repository revision; the index may be outdated or the skill may have been renamed or removed; preview the repository to see available skills';
  const multiple = 'skills.sh skill "wanted" matches multiple repository skills: "skills/first", "skills/second"; specify a skill subdirectory';
  const legacy = 'skills.sh skill "polish" did not match exactly one repository skill';
  const expected = [
    ["en", 'skills.sh skill "polish" was not found in the current repository revision. The index may be outdated, or the skill may have been renamed or removed. Preview the repository to see available skills.', 'skills.sh skill "wanted" matches multiple repository skills: "skills/first", "skills/second". Specify a skill subdirectory.', 'skills.sh skill "polish" could not be uniquely matched to a repository skill. Preview the repository to see available skills.'],
    ["zh-CN", '当前仓库版本中未找到 skills.sh 技能 "polish"。索引可能已过期，或技能已改名、移除。请预览仓库查看可用技能。', 'skills.sh 技能 "wanted" 匹配到多个仓库技能："skills/first", "skills/second"。请指定技能子目录。', 'skills.sh 技能 "polish" 未能唯一匹配仓库中的技能。请预览仓库查看可用技能。'],
    ["zh-TW", '目前儲存庫版本中找不到 skills.sh 技能 "polish"。索引可能已過期，或技能已改名、移除。請預覽儲存庫查看可用技能。', 'skills.sh 技能 "wanted" 符合多個儲存庫技能："skills/first", "skills/second"。請指定技能子目錄。', 'skills.sh 技能 "polish" 無法唯一對應到儲存庫中的技能。請預覽儲存庫查看可用技能。'],
  ];
  for (const [locale, missingTranslation, multipleTranslation, legacyTranslation] of expected) {
    i.setLocale(locale);
    assert.equal(i.tMessage(missing), missingTranslation);
    assert.equal(i.tMessage(multiple), multipleTranslation);
    assert.equal(i.tMessage(legacy), legacyTranslation);
  }
});

test("unavailable skills translate nested reasons and preserve quoted names, paths, and unknown diagnostics", () => {
  const i = setup();
  const name = JSON.stringify('source not found: 用户 " is unavailable: "');
  const path = JSON.stringify('skills/目录: source not found/"draft"/link');
  const message = `skills.sh skill ${name} is unavailable: unsupported skill content at ${path}: symlink is not supported`;
  const translations = [
    ["en", `skills.sh skill ${name} is unavailable: Unsupported skill content at ${path}: symbolic links are not supported`, "The selected skill could not be read"],
    ["zh-CN", `skills.sh 技能 ${name} 不可用：技能内容 ${path} 不受支持：含符号链接`, "无法读取所选技能"],
    ["zh-TW", `skills.sh 技能 ${name} 無法使用：技能內容 ${path} 不受支援：含符號連結`, "無法讀取所選技能"],
  ];
  for (const [locale, translation, fallback] of translations) {
    i.setLocale(locale);
    assert.equal(i.tMessage(message), translation);
    assert.equal(i.tMessage("the selected skill could not be read"), fallback);
  }
  i.setLocale("zh-CN");
  assert.equal(i.tMessage('skills.sh skill "wanted" is unavailable: the selected skill could not be read'), 'skills.sh 技能 "wanted" 不可用：无法读取所选技能');
  const unknown = "custom diagnostic: disk quota exceeded (source not found)";
  assert.equal(i.tMessage(`skills.sh skill ${name} is unavailable: ${unknown}`), `skills.sh 技能 ${name} 不可用：${unknown}`);
  i.setLocale("en");
  assert.equal(i.tMessage(translations[2][1]), translations[0][1]);
});
