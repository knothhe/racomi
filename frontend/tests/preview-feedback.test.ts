import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";
import ts from "typescript";

const source = ts.createSourceFile("main.ts", readFileSync(new URL("../src/main.ts", import.meta.url), "utf8"), ts.ScriptTarget.Latest, true);
const names = new Set(["doPreview", "task", "previewRequestPanel", "previewWaitHint", "updatePreviewWait"]);
const production = source.statements.filter((node) => ts.isFunctionDeclaration(node) && node.name && names.has(node.name.text)).map((node) => node.getText(source)).join("\n");

function harness() {
  let now = 1000;
  const timers = new Map<number, () => void>();
  const elapsed = { textContent: "" }, hint = { textContent: "" };
  const calls: unknown[][] = [];
  const pending: { resolve: (value: unknown) => void; reject: (error: Error) => void }[] = [];
  const api = Object.fromEntries(["preview", "previewLocal"].map((name) => [name, (...args: unknown[]) => {
    calls.push([name, ...args]);
    return new Promise((resolve, reject) => pending.push({ resolve, reject }));
  }]));
  const context = vm.createContext({
    api, Date: { now: () => now },
    window: { setInterval: (callback: () => void) => { timers.set(1, callback); return 1; }, clearInterval: (id: number) => timers.delete(id) },
    document: { querySelector: (selector: string) => selector === "#preview-elapsed" ? elapsed : selector === "#preview-wait-hint" ? hint : null },
  });
  vm.runInContext(ts.transpileModule(`
    let preview = null, previewRequest = null, currentPage = "discover";
    let sourceDraft = "", refDraft = "", subpathDraft = "", localSourceDraft = "";
    let selectedCandidates = new Set();
    const activeTasks = new Set(), taskMessages = new Map(), stateChangingTasks = new Set();
    const discoveryBusy = () => activeTasks.size > 0;
    const status = () => {}, syncBusyControls = () => {}, toast = () => {};
    const arr = (value) => value || [], tMessage = (value) => value;
    const t = (value, variables) => value.replace(/\\{(\\w+)\\}/g, (slot, key) => variables?.[key] ?? slot);
    const e = (value) => String(value).replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll('"', "&quot;");
    const button = (label) => '<button>' + label + '</button>', icon = () => '';
    const modalRoot = { children: [] };
    let html = "";
    const render = () => { html = preview ? "candidates" : previewRequest ? previewRequestPanel() : ""; };
    ${production}
    globalThis.run = doPreview;
    globalThis.snapshot = () => ({ html, preview, request: previewRequest, selected: [...selectedCandidates], busy: activeTasks.size });
    globalThis.leave = () => { currentPage = "skills"; };
    globalThis.returnToDiscover = () => { currentPage = "discover"; render(); };
  `, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.None } }).outputText, context);
  return {
    run: context.run as (...args: unknown[]) => Promise<void>,
    snapshot: context.snapshot as () => { html: string; preview: unknown; request: { loading: boolean; error: string } | null; selected: string[]; busy: number },
    leave: context.leave as () => void, returnToDiscover: context.returnToDiscover as () => void,
    advance: (seconds: number) => { now += seconds * 1000; for (const tick of timers.values()) tick(); },
    timers, elapsed, hint, calls, pending,
  };
}

test("preview immediately shows loading, updates long-wait guidance, and retains failures", async () => {
  const h = harness();
  const work = h.run("team/repo", "release", "skills", "wanted");
  assert.match(h.snapshot().html, /正在准备预览/);
  assert.match(h.snapshot().html, /已等待 0 秒/);
  assert.equal(h.snapshot().busy, 1);
  await h.run("duplicate", "", "");
  assert.equal(h.calls.length, 1);
  h.advance(16);
  assert.equal(h.elapsed.textContent, "已等待 16 秒");
  assert.match(h.hint.textContent, /仍在等待仓库内容/);
  h.advance(30);
  assert.match(h.hint.textContent, /检查网络或代理/);
  h.pending[0].reject(new Error("cannot clone repository: Git operation timed out: context deadline exceeded"));
  await work;
  assert.equal(h.timers.size, 0);
  assert.equal(h.snapshot().busy, 0);
  assert.match(h.snapshot().html, /预览超时/);
  assert.match(h.snapshot().html, /重试预览/);
  assert.match(h.snapshot().html, /错误详情/);
  assert.equal(h.snapshot().request?.loading, false);
  const retry = h.run("team/repo", "release", "skills", "wanted");
  assert.doesNotMatch(h.snapshot().html, /预览超时/);
  h.pending[1].resolve({ id: "reviewed", candidates: [{ name: "wanted", subpath: "skills/wanted" }, { name: "other", subpath: "skills/other" }] });
  await retry;
  assert.equal(h.snapshot().html, "candidates");
  assert.deepEqual([...h.snapshot().selected], ["skills/wanted"]);
  assert.equal(h.snapshot().request, null);
  assert.equal(h.timers.size, 0);
});

test("local preview errors stay readable after navigating away and do not inject HTML", async () => {
  const h = harness();
  const work = h.run("/local/<skills>", "", "", "", "local");
  h.advance(46);
  assert.match(h.hint.textContent, /本地技能文件/);
  assert.doesNotMatch(h.hint.textContent, /网络/);
  h.leave();
  h.pending[0].reject(new Error("Cannot read <script>"));
  await work;
  assert.equal(h.timers.size, 0);
  h.returnToDiscover();
  assert.match(h.snapshot().html, /目录是否存在且可读取/);
  assert.match(h.snapshot().html, /&lt;script>/);
  assert.doesNotMatch(h.snapshot().html, /<script>|检查网络/);
});
