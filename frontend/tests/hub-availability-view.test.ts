import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";
import ts from "typescript";
import type { HubAvailability } from "../src/api.ts";
import { gitBackedHubSkill } from "../src/hub-recovery.ts";

const source = ts.createSourceFile("main.ts", readFileSync(new URL("../src/main.ts", import.meta.url), "utf8"), ts.ScriptTarget.Latest, true);
const names = new Set(["hubAvailabilityPresentation", "updateHubAvailabilityCards", "hubResults"]);
const production = source.statements.filter((node) => ts.isFunctionDeclaration(node) && node.name && names.has(node.name.text)).map((node) => node.getText(source)).join("\n");

const polish = { url: "https://skills.sh/pbakaus/impeccable/polish", source: "pbakaus/impeccable", slug: "pbakaus/impeccable/polish", name: "polish", installs: 1200 };

function harness() {
  const values = new Map<string, HubAvailability>();
  const status = { className: "hub-skill-status checking", title: "" };
  const statusLabel = { textContent: "核验中" }, statusSymbol = { innerHTML: "" }, buttonLabel = { textContent: "预览" };
  const button = { disabled: true, label: buttonLabel };
  const scroller = { scrollTop: 420 }, input = { value: "正在输入" };
  const card = {
    dataset: { hubUrl: polish.url, hubIndex: "0" }, button,
    querySelector: (selector: string) => selector === ".hub-skill-status" ? status : selector === "[data-hub-status-label]" ? statusLabel : selector === "[data-hub-status-symbol]" ? statusSymbol : selector === "[data-hub-preview-label]" ? buttonLabel : null,
  };
  Object.defineProperty(card, "innerHTML", { set: () => assert.fail("availability must not replace the card's contents") });
  const document = { activeElement: button, querySelectorAll: () => [card] };
  const runtime = vm.runInNewContext(ts.transpileModule(`
    let currentPage = "discover", discoverTab = "hub", searchResults = [fixture], hubListing = null, hubResultQuery = "";
    const activeTasks = new Set();
    const t = (value, variables) => value.replace(/\\{(\\w+)\\}/g, (slot, key) => variables?.[key] ?? slot);
    const tMessage = (value) => value, date = (value) => value, skills = () => [], hubResultInstalled = () => true;
    const icon = (name) => '<svg data-icon="' + name + '"></svg>', getLocale = () => "en";
    const e = (value) => String(value).replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('"', '&quot;');
    ${production}
    ({ update: updateHubAvailabilityCards, html: hubResults,
      leave: () => { currentPage = "skills"; }, replace: (value) => { searchResults = [value]; } });
  `, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.None } }).outputText, {
    document, fixture: polish, gitBackedHubSkill, hubAvailability: { get: (url: string) => values.get(url) },
  }) as { update: (urls: string[]) => void; html: () => string; leave: () => void; replace: (value: typeof polish) => void };
  return { ...runtime, values, status, statusLabel, statusSymbol, buttonLabel, button, card, document, scroller, input };
}

test("availability patches status and the existing button without remounting focused cards or resetting scroll and input", () => {
  const h = harness();
  h.values.set(polish.url, { url: polish.url, state: "missing", revision: "current", checkedAt: "2026-10-23T00:00:00Z", reason: "" });
  h.update([polish.url]);
  assert.equal(h.statusLabel.textContent, "上游已移除或改名");
  assert.equal(h.buttonLabel.textContent, "查看仓库当前技能");
  assert.equal(h.status.className, "hub-skill-status missing");
  assert.match(h.status.title, /核验时间：2026-10-23/);
  assert.equal(h.document.activeElement, h.button);
  assert.equal(h.card.button, h.button);
  assert.equal(h.button.disabled, true, "background updates do not re-enable a foreground task's controls");
  assert.equal(h.scroller.scrollTop, 420);
  assert.equal(h.input.value, "正在输入");
  h.values.set(polish.url, { url: polish.url, state: "unknown", revision: "", checkedAt: "", reason: "connection interrupted" });
  h.update([polish.url]);
  assert.equal(h.buttonLabel.textContent, "预览");
  assert.equal(h.statusLabel.textContent, "未能核验");
  assert.equal(h.status.className, "hub-skill-status unknown");
});

test("checking, missing, unavailable and installed indicators coexist without confusing unreadable content with a removed skill", () => {
  const h = harness();
  assert.match(h.html(), /hub-skill-status checking/);
  assert.match(h.html(), /data-hub-preview-label>预览/);
  h.values.set(polish.url, { url: polish.url, state: "unavailable", revision: "current", checkedAt: "", reason: '<script> unsafe reason' });
  const html = h.html();
  assert.match(html, /暂不可用/);
  assert.match(html, /result-installed/);
  assert.match(html, /已安装/);
  assert.match(html, /data-hub-preview-label>查看仓库当前技能/);
  assert.match(html, /title="&lt;script> unsafe reason"/);
  assert.doesNotMatch(html, /上游已移除或改名|<script>/);
});

test("a result from an old search cannot update a card reused by a newer result or another page", () => {
  const h = harness();
  h.values.set(polish.url, { url: polish.url, state: "missing", revision: "current", checkedAt: "", reason: "" });
  h.replace({ ...polish, url: "https://skills.sh/team/other/current" });
  h.update([polish.url]);
  assert.equal(h.buttonLabel.textContent, "预览");
  h.leave();
  h.replace(polish); h.update([polish.url]);
  assert.equal(h.buttonLabel.textContent, "预览");
});
