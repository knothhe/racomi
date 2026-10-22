import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";
import ts from "typescript";

function guideModule() {
  const exports: { guidePage?: (tab?: string) => string; guideTabs?: { id: string; label: string }[] } = {};
  const code = ts.transpileModule(readFileSync(new URL("../src/guide.ts", import.meta.url), "utf8"), { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText;
  new Function("exports", "require", code)(exports, (name: string) => {
    if (name === "./i18n") return { t: (key: string) => key };
    if (name === "./icons") return { icon: () => "<svg></svg>" };
    throw new Error(`Unexpected import: ${name}`);
  });
  return { guidePage: exports.guidePage!, guideTabs: exports.guideTabs! };
}

test("the guide initially shows the workflow and first steps without the other tabs' content", () => {
  const html = guideModule().guidePage();
  assert.match(html, /id="guide-workflow-title"/);
  assert.match(html, /id="guide-start-title"/);
  assert.doesNotMatch(html, /id="guide-(cloud|questions)-title"/);
});

test("sync and FAQ guide tabs render only their own content and keep the configuration shortcut", () => {
  const { guidePage } = guideModule();
  const sync = guidePage("sync");
  assert.match(sync, /id="guide-cloud-title"/);
  assert.match(sync, /r2\.cloudflarestorage\.com/);
  assert.match(sync, /data-action="open-cloud-settings"/);
  assert.doesNotMatch(sync, /id="guide-(workflow|start|questions)-title"/);
  const faq = guidePage("faq");
  assert.match(faq, /id="guide-questions-title"/);
  assert.doesNotMatch(faq, /id="guide-(workflow|start|cloud)-title"/);
});

const main = ts.createSourceFile("main.ts", readFileSync(new URL("../src/main.ts", import.meta.url), "utf8"), ts.ScriptTarget.Latest, true);
const functions = new Set(["navigationSubitems", "guideTabNavigation"]);
const production = main.statements.filter((statement) => ts.isFunctionDeclaration(statement) && statement.name && functions.has(statement.name.text)).map((statement) => statement.getText(main)).join("\n");
function eventStatements(event: string) {
  const statement = main.statements.find((statement) => ts.isExpressionStatement(statement) && ts.isCallExpression(statement.expression) && statement.expression.expression.getText(main) === "document.addEventListener" && statement.expression.arguments[0]?.getText(main) === JSON.stringify(event)) as ts.ExpressionStatement;
  const callback = (statement.expression as ts.CallExpression).arguments[1] as ts.ArrowFunction;
  return (callback.body as ts.Block).statements;
}
const clickActions = new Set(['target.dataset.page', 'action === "open-cloud-guide"', 'action === "guide-tab"', 'action === "open-cloud-settings"']);
const clickProduction = eventStatements("click").filter((statement) => ts.isIfStatement(statement) && clickActions.has(statement.expression.getText(main))).map((statement) => statement.getText(main)).join("\n");
const keyboardProduction = eventStatements("keydown").find((statement) => ts.isIfStatement(statement) && statement.expression.getText(main).startsWith('["ArrowLeft"'))!.getText(main);

function navigationHarness() {
  let rendered = 0;
  let scrolled = false;
  const focusTargets: string[] = [];
  const document = { activeElement: null as any, querySelector: (selector: string): any => ({ scrollIntoView: () => { scrolled = true; }, focus: () => { focusTargets.push(selector); } }) };
  const code = `
    let currentPage = "guide", guideTab = "start", settingsBackups = false;
    const t = (key) => key, e = (value) => value;
    const render = () => recordRender();
    const ensureHubResults = () => {};
    const hubAvailability = { cancel: () => {} };
    const app = { querySelectorAll: () => [] };
    ${production}
    function navigate(target) { const action = target.dataset.action; ${clickProduction} }
    function keydown(ev) { ${keyboardProduction} }
    ({ navigationSubitems, guideTabNavigation, navigate, keydown, state: () => ({ currentPage, guideTab, settingsBackups }) });
  `;
  const runtime = vm.runInNewContext(ts.transpileModule(code, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, { guideTabs: guideModule().guideTabs, recordRender: () => { rendered++; }, document }) as {
    navigationSubitems: (page: string) => string; guideTabNavigation: () => string;
    navigate: (target: unknown) => void; keydown: (event: unknown) => void;
    state: () => { currentPage: string; guideTab: string; settingsBackups: boolean };
  };
  const click = (dataset: Record<string, string>) => runtime.navigate({ dataset, isConnected: true });
  return { ...runtime, click, document, focusTargets, rendered: () => rendered, scrolled: () => scrolled };
}

test("guide top and side navigation share the selected tab and expose one keyboard entry", () => {
  const h = navigationHarness();
  h.click({ action: "guide-tab", tab: "sync" });
  const top = h.guideTabNavigation();
  assert.match(top, /role="tablist"/);
  assert.equal((top.match(/role="tab"/g) || []).length, 3);
  assert.equal((top.match(/aria-selected="true"/g) || []).length, 1);
  assert.equal((top.match(/tabindex="0"/g) || []).length, 1);
  assert.match(top, /id="guide-tab-sync"[^>]*aria-controls="guide-tab-panel"[^>]*aria-selected="true"[^>]*tabindex="0"/);
  assert.match(h.navigationSubitems("guide"), /data-tab="sync" aria-pressed="true"/);
  assert.equal(h.navigationSubitems("settings"), "");
});

test("guide shortcuts open sync directly and returning to Guide preserves the chosen tab", () => {
  const h = navigationHarness();
  h.click({ action: "open-cloud-guide" });
  assert.equal(h.state().guideTab, "sync");
  assert.deepEqual(h.focusTargets, ["#guide-tab-panel"]);
  h.click({ action: "guide-tab", tab: "faq" });
  h.click({ page: "settings" });
  h.click({ page: "guide" });
  assert.equal(h.state().guideTab, "faq");
  const renders = h.rendered();
  h.click({ action: "guide-tab", tab: "invalid" });
  assert.equal(h.rendered(), renders);
  h.click({ action: "open-cloud-settings" });
  assert.equal(h.state().currentPage, "settings");
  assert.equal(h.state().settingsBackups, true);
  assert.equal(h.scrolled(), true);
  assert.deepEqual(h.focusTargets, ["#guide-tab-panel", "#settings-tab-panel"]);
});

test("guide keyboard tabs support arrow wrapping and Home/End with focus following selection", () => {
  const h = navigationHarness();
  const tabs = ["start", "sync", "faq"].map((id) => ({
    id, getAttribute: (name: string) => name === "role" ? "tab" : null,
    parentElement: { querySelectorAll: () => tabs },
    click: () => h.click({ action: "guide-tab", tab: id }),
    focus: () => { h.document.activeElement = tabs.find((tab) => tab.id === id); },
  }));
  h.document.querySelector = (selector: string) => {
    assert.equal(selector, '.guide-tabs [aria-selected="true"]');
    return tabs.find((tab) => tab.id === h.state().guideTab);
  };
  h.document.activeElement = tabs[0];
  let prevented = 0;
  for (const [key, expected] of [["ArrowLeft", "faq"], ["ArrowRight", "start"], ["End", "faq"], ["Home", "start"], ["ArrowRight", "sync"]]) {
    h.keydown({ key, preventDefault: () => { prevented++; } });
    assert.equal(h.state().guideTab, expected);
    assert.equal(h.document.activeElement.id, expected);
  }
  assert.equal(prevented, 5);
});
