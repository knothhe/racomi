import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";
import ts from "typescript";

const source = ts.createSourceFile("main.ts", readFileSync(new URL("../src/main.ts", import.meta.url), "utf8"), ts.ScriptTarget.Latest, true);
const names = new Set(["loadSkillDocument", "skillDocument"]);
const production = source.statements.filter((node) => ts.isFunctionDeclaration(node) && node.name && names.has(node.name.text)).map((node) => node.getText(source)).join("\n");

function harness() {
  const pending: { resolve: (text: string) => void; reject: (error: Error) => void }[] = [];
  const calls: string[] = [];
  const context = vm.createContext({
    api: { readSkillDocument: (id: string) => {
      calls.push(id);
      return new Promise<string>((resolve, reject) => pending.push({ resolve, reject }));
    } },
  });
  vm.runInContext(ts.transpileModule(`
    let currentPage = "skills", selectedSkill = "alpha", detailTab = "document";
    let installed = [{ id: "alpha", name: "Alpha", contentHash: "v1" }];
    const skills = () => installed;
    const documents = new Map(), loadingDocuments = new Set(), documentErrors = new Map(), rawDocuments = new Set();
    const e = (value) => String(value ?? "").replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll('"', "&quot;");
    const t = (value) => value, tMessage = (value) => value, getLocale = () => "zh-CN";
    const button = (label, action) => '<button data-action="' + action + '">' + label + '</button>';
    const renderMarkdownDocument = (text) => '<h1>' + e(text.replace(/^# /, "")) + '</h1>';
    const task = async (_message, action) => { try { return await action(); } catch { return undefined; } };
    const render = () => {};
    ${production}
    globalThis.load = loadSkillDocument;
    globalThis.html = () => skillDocument(installed[0]);
    globalThis.snapshot = () => ({ docs: [...documents], loading: [...loadingDocuments], errors: [...documentErrors] });
    globalThis.update = (hash) => { installed[0] = { ...installed[0], contentHash: hash }; };
    globalThis.raw = (on) => { if (on) rawDocuments.add("alpha"); else rawDocuments.delete("alpha"); };
    globalThis.leave = () => { selectedSkill = "beta"; };
  `, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context);
  return {
    calls, pending,
    load: context.load as (id: string) => Promise<void>,
    html: context.html as () => string,
    snapshot: context.snapshot as () => { docs: [string, { hash: string; text: string }][]; loading: string[]; errors: unknown[] },
    update: context.update as (hash: string) => void,
    raw: context.raw as (on: boolean) => void,
    leave: context.leave as () => void,
  };
}

test("local document reads deduplicate, cache by content hash, and switch modes without rereading", async () => {
  const h = harness();
  const work = h.load("alpha");
  await h.load("alpha");
  assert.equal(h.calls.length, 1);
  assert.match(h.html(), /aria-busy="true"/);
  h.pending[0].resolve("# Authored <text>");
  await work;
  assert.match(h.html(), /<h1>Authored &lt;text>/);
  assert.match(h.html(), /aria-checked="false"/);
  h.raw(true);
  assert.match(h.html(), /<pre[^>]*><code># Authored &lt;text>/);
  assert.match(h.html(), /aria-checked="true"/);
  h.raw(false);
  await h.load("alpha");
  assert.equal(h.calls.length, 1);
  h.update("v2");
  const next = h.load("alpha");
  h.pending[1].resolve("# Updated");
  await next;
  assert.equal(h.snapshot().docs[0][1].text, "# Updated");
});

test("read failures stay visible with safe error text and can be retried", async () => {
  const h = harness();
  const first = h.load("alpha");
  h.pending[0].reject(new Error("Cannot read <script>"));
  await first;
  assert.equal(h.snapshot().loading.length, 0);
  assert.match(h.html(), /文档读取失败/);
  assert.match(h.html(), /Cannot read &lt;script>/);
  assert.match(h.html(), /重试读取/);
  const retry = h.load("alpha");
  h.pending[1].resolve("# Recovered");
  await retry;
  assert.match(h.html(), /Recovered/);
  assert.equal(h.snapshot().errors.length, 0);
});

test("a skill updated during a pending read loads its new content without showing the stale document", async () => {
  const h = harness();
  const first = h.load("alpha");
  h.update("v2");
  await h.load("alpha");
  h.pending[0].resolve("# Old");
  await first;
  assert.equal(h.snapshot().docs.length, 0);
  assert.equal(h.calls.length, 2);
  h.pending[1].resolve("# Current");
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(h.snapshot().docs[0][1].hash, "v2");
  assert.match(h.html(), /Current/);
});

test("finishing a read for a previous selection never renders it as another skill's content", async () => {
  const h = harness();
  const first = h.load("alpha");
  h.leave();
  h.pending[0].resolve("# Alpha");
  await first;
  assert.equal(h.snapshot().docs[0][0], "alpha");
  assert.equal(h.calls.length, 1);
});
