import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";
import ts from "typescript";
import { gitBackedHubSkill, recoverableHubSkill } from "../src/hub-recovery.ts";
import { createHubAvailability } from "../src/hub-availability.ts";

const source = ts.createSourceFile("main.ts", readFileSync(new URL("../src/main.ts", import.meta.url), "utf8"), ts.ScriptTarget.Latest, true);
const names = new Set(["doPreview", "task", "previewRequestPanel", "previewWaitHint", "updatePreviewWait", "closePreview", "button", "iconButton", "captureListScroll", "restoreListScroll", "renderPreview", "scanWarnings", "checkVisibleHubAvailability"]);
const production = source.statements.filter((node) => ts.isFunctionDeclaration(node) && node.name && names.has(node.name.text)).map((node) => node.getText(source)).join("\n");
const panelProduction = source.statements.find((node) => ts.isFunctionDeclaration(node) && node.name?.text === "previewPanel")!.getText(source);
const eventProduction = source.statements.filter((node) => ts.isExpressionStatement(node) && ts.isCallExpression(node.expression)
  && (node.expression.expression.getText(source) === "document.addEventListener" && node.expression.arguments[0]?.getText(source) === '"click"'
    || node.expression.expression.getText(source) === "app.addEventListener" && node.expression.arguments[0]?.getText(source) === '"change"'))
  .map((node) => node.getText(source)).join("\n");

function harness(options: { previewOpen?: boolean; actualPanel?: boolean; localizeMessage?: (value: string) => string } = {}) {
  let now = 1000;
  let nextTimer = 1;
  const timers = new Map<number, () => void>();
  const elapsed = { textContent: "" }, hint = { textContent: "" };
  const calls: unknown[][] = [];
  const focused: { selector: string; preventScroll: boolean }[] = [];
  const listeners = new Map<string, (event: { target: unknown; preventDefault: () => void }) => Promise<void>>();
  const hubScroller = { dataset: { preserveScroll: "hub-results" }, scrollTop: 0 };
  const install = { disabled: true }, candidateCount = { textContent: "" }, selectAll = { textContent: "" };
  let renders = 0;
  const panel = { outerHTML: "existing preview", querySelector: () => null };
  const pending: { resolve: (value: unknown) => void; reject: (error: Error) => void }[] = [];
  const checks: { cancellations: number; resolve: (value: unknown[]) => void }[] = [];
  const availability = createHubAvailability({
    check: () => {
      const item = { cancellations: 0 } as typeof checks[number];
      const promise = new Promise<never>((resolve) => { item.resolve = resolve; });
      checks.push(item);
      return Object.assign(promise, { cancel: () => { item.cancellations++; } });
    },
    changed: () => {},
  });
  const api = Object.fromEntries(["preview", "previewLocal", "previewHubRepository"].map((name) => [name, (...args: unknown[]) => {
    calls.push([name, ...args]);
    return new Promise((resolve, reject) => pending.push({ resolve, reject }));
  }]));
  const context = vm.createContext({
    api, Error, gitBackedHubSkill, recoverableHubSkill, hubAvailability: availability, localizeMessage: options.localizeMessage || ((value: string) => value), Date: { now: () => now },
    recordRender: () => { renders++; }, setThemePreference: () => {}, ensureHubResults: () => {},
    syncRenderedControls: (html: string) => { install.disabled = /data-action="install" disabled/.test(html); },
    window: { setInterval: (callback: () => void) => { const id = nextTimer++; timers.set(id, callback); return id; }, clearInterval: (id: number) => timers.delete(id) },
    app: { addEventListener: (name: string, listener: (event: { target: unknown; preventDefault: () => void }) => Promise<void>) => listeners.set(`app:${name}`, listener) },
    document: {
      addEventListener: (name: string, listener: (event: { target: unknown; preventDefault: () => void }) => Promise<void>) => listeners.set(`document:${name}`, listener),
      querySelectorAll: (selector: string) => selector === "[data-preserve-scroll]" ? [hubScroller] : [],
      querySelector: (selector: string) => options.previewOpen && [".discover-content > .preview-panel", ".preview-panel"].includes(selector) ? panel : selector === "#preview-elapsed" ? elapsed : selector === "#preview-wait-hint" ? hint
        : selector === '[data-action="install"]' ? install : selector === "#candidate-count" ? candidateCount : selector === '[data-action="select-all-candidates"]' ? selectAll
        : selector === ".hub-panel" ? { dataset: { preserveScroll: "hub-panel" }, scrollTop: 0, querySelectorAll: () => [hubScroller] }
        : selector.includes('data-action="hub-preview"') || selector.includes('data-page-search="discover"')
          ? { focus: (options: { preventScroll: boolean }) => focused.push({ selector, preventScroll: options.preventScroll }), closest: () => ({ focus: (options: { preventScroll: boolean }) => focused.push({ selector, preventScroll: options.preventScroll }) }) } : null,
    },
  });
  vm.runInContext(ts.transpileModule(`
    let preview = null, previewRequest = null, previewReturnTo = null, previewMissingSkill = "", previewRepositoryRecovery = false, currentPage = "discover";
    let discoverTab = "hub", searchResults = [], detailTab = "overview", selectedSkill = "";
    const preservedScroll = new Map();
    let sourceDraft = "", refDraft = "", subpathDraft = "", localSourceDraft = "";
    let selectedCandidates = new Set();
    const activeTasks = new Set(), taskMessages = new Map(), stateChangingTasks = new Set();
    const discoveryBusy = () => activeTasks.size > 0;
    const status = () => {}, syncBusyControls = () => {}, toast = () => {};
    const arr = (value) => value || [], tMessage = localizeMessage;
    const t = (value, variables) => value.replace(/\\{(\\w+)\\}/g, (slot, key) => variables?.[key] ?? slot);
    const e = (value) => String(value).replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll('"', "&quot;");
    const icon = () => '', short = (value) => value?.slice(0, 8) || "—", globalPath = (value) => value;
    const modalRoot = { children: [] };
    let html = "";
    ${options.actualPanel ? panelProduction : 'const previewPanel = () => "candidates";'}
    const render = () => { recordRender(); html = preview ? previewPanel() : previewRequest ? previewRequestPanel() : ""; syncRenderedControls(html); };
    ${production}
    ${eventProduction}
    globalThis.run = doPreview;
    globalThis.close = closePreview;
    globalThis.setResults = (results) => { searchResults = results; };
    globalThis.snapshot = () => ({ html, preview, request: previewRequest, missing: previewMissingSkill, selected: [...selectedCandidates], busy: activeTasks.size, page: currentPage, tab: discoverTab, returnTo: previewReturnTo });
    globalThis.leave = () => { currentPage = "skills"; };
    globalThis.returnToDiscover = () => { currentPage = "discover"; render(); };
  `, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.None } }).outputText, context);
  return {
    close: context.close as () => void,
    setResults: context.setResults as (results: { source: string; slug: string; name: string }[]) => void,
    focused, panel, hubScroller, install, candidateCount, selectAll, renders: () => renders,
    availability, checks,
    click: (dataset: Record<string, string>, disabled = false) => {
      const target = { dataset, disabled, closest: () => target };
      return listeners.get("document:click")!({ target, preventDefault() {} });
    },
    choose: (value: string, checked: boolean) => listeners.get("app:change")!({ target: { name: "candidate", value, checked }, preventDefault() {} }),
    run: context.run as (...args: unknown[]) => Promise<void>,
    snapshot: context.snapshot as () => { html: string; preview: unknown; request: { source: string; ref: string; subpath: string; loading: boolean; error: string; kind: string; selectionMode: string } | null; missing: string; selected: string[]; busy: number; page: string; tab: string; returnTo: unknown },
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
  assert.match(h.hint.textContent, /仓库较大或传输较慢/);
  h.pending[0].reject(new Error("cannot clone repository: repository download timed out after 300 seconds: context deadline exceeded"));
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

test("skills.sh selection errors show localized diagnostics in the primary alert", async () => {
  const errors = [
    'skills.sh skill "polish" was not found in the current repository revision; the index may be outdated or the skill may have been renamed or removed; preview the repository to see available skills',
    'skills.sh skill "wanted" matches multiple repository skills: "skills/first", "skills/second"; specify a skill subdirectory',
    'skills.sh skill "timeout<script>" is unavailable: unsupported skill content at "skills/<script>": symlink is not supported',
    'skills.sh skill "polish" did not match exactly one repository skill',
  ];
  for (const message of errors) {
    const h = harness({ localizeMessage: (value) => `翻译后的诊断：${value}` });
    const work = h.run("team/repo", "", "", "polish");
    h.pending[0].reject(new Error(message));
    await work;
    const html = h.snapshot().html;
    const alert = html.match(/<div class="preview-request-error" role="alert">([\s\S]*?)<\/div>/)?.[1];
    assert.ok(alert, "the primary alert remains visible");
    assert.match(alert, /翻译后的诊断：skills.sh skill &quot;/);
    assert.doesNotMatch(html, /请检查网络|访问权限|预览超时|<script>/);
    if (message.includes("<script>")) assert.match(alert, /&lt;script>/);
    assert.match(html, /重试预览/);
  }
});

test("non-selection repository errors keep their connection guidance", async () => {
  const h = harness();
  const work = h.run("team/repo", "", "");
  h.pending[0].reject(new Error("cannot clone repository: permission denied"));
  await work;
  assert.match(h.snapshot().html, /请检查网络、仓库地址和访问权限后重试/);
});

test("closing a pending preview ignores its late success or failure", async () => {
  for (const succeeds of [true, false]) {
    const h = harness();
    const work = h.run("team/repo", "", "");
    assert.match(h.snapshot().html, /data-action="close-preview"/);
    h.close();
    assert.equal(h.snapshot().html, "");
    assert.equal(h.snapshot().request, null);
    if (succeeds) h.pending[0].resolve({ id: "dismissed", candidates: [{ name: "review", subpath: "skills/review" }] });
    else h.pending[0].reject(new Error("late failure"));
    await work;
    h.returnToDiscover();
    assert.equal(h.snapshot().html, "");
    assert.equal(h.snapshot().preview, null);
    assert.equal(h.snapshot().request, null);
    assert.deepEqual([...h.snapshot().selected], []);
    assert.equal(h.snapshot().busy, 0);
    assert.equal(h.timers.size, 0);
  }
});

test("closing completed or failed previews clears choices and allows a new preview", async () => {
  for (const succeeds of [true, false]) {
    const h = harness();
    const first = h.run("team/first", "", "");
    if (succeeds) h.pending[0].resolve({ id: "first", candidates: [{ name: "review", subpath: "skills/review" }] });
    else h.pending[0].reject(new Error("preview failed"));
    await first;
    h.close();
    assert.equal(h.snapshot().preview, null);
    assert.equal(h.snapshot().request, null);
    assert.deepEqual([...h.snapshot().selected], []);
    const next = h.run("team/next", "", "");
    h.pending[1].resolve({ id: "next", candidates: [{ name: "next", subpath: "skills/next" }] });
    await next;
    assert.equal(h.snapshot().html, "candidates");
    assert.deepEqual([...h.snapshot().selected], ["skills/next"]);
  }
});

test("closing returns focus to the originating card after results reorder, or search if it disappeared", async () => {
  const origin = { source: "team/repo", slug: "review", name: "Review" };
  const other = { source: "other/repo", slug: "review", name: "Review" };
  for (const remains of [true, false]) {
    const h = harness();
    const work = h.run("team/repo", "", "", "", "git", { key: JSON.stringify([origin.source, origin.slug]), scroll: [] });
    h.pending[0].resolve({ id: "ready", candidates: [{ name: "Review", subpath: "skills/review" }] });
    await work;
    h.setResults(remains ? [other, origin] : [other]);
    h.close();
    assert.deepEqual(h.focused.at(-1), {
      selector: remains ? '[data-action="hub-preview"][data-index="1"]' : '[data-page-search="discover"]',
      preventScroll: true,
    });
  }
});

test("switching previews updates only the inspector during loading and completion", async () => {
  for (const succeeds of [true, false]) {
    const h = harness({ previewOpen: true });
    const work = h.run("team/next", "", "");
    assert.match(h.panel.outerHTML, /正在准备预览/);
    assert.equal(h.renders(), 0, "opening another card keeps the existing list mounted");
    if (succeeds) h.pending[0].resolve({ id: "next", candidates: [{ name: "next", subpath: "skills/next" }] });
    else h.pending[0].reject(new Error("next preview failed"));
    await work;
    if (succeeds) assert.equal(h.panel.outerHTML, "candidates");
    else assert.match(h.panel.outerHTML, /未能完成预览/);
    assert.equal(h.renders(), 0, "finishing a preview also keeps the list mounted");
  }
});

const missingPolish = 'skills.sh skill "polish" was not found in the current repository revision; the index may be outdated or the skill may have been renamed or removed; preview the repository to see available skills';
const polishURL = "https://skills.sh/pbakaus/impeccable/polish";
const currentRepository = {
  id: "current-repository", source: "https://github.com/pbakaus/impeccable.git", ref: "release", revision: "1234567890abcdef",
  candidates: [{ name: "impeccable", description: "Current repository skill", subpath: ".agents/skills/impeccable", revision: "1234567890abcdef" }],
};

test("the missing-skill recovery action previews the original repository and requires a manual selection", async () => {
  const h = harness({ actualPanel: true });
  const origin = { source: "pbakaus/impeccable", slug: "pbakaus/impeccable/polish", name: "polish" };
  const returnTo = { key: JSON.stringify([origin.source, origin.slug]), scroll: [{ key: "hub-results", top: 420 }] };
  h.setResults([origin]);
  const first = h.run(polishURL, "release", "skills-scope", "polish", "git", returnTo);
  h.pending[0].reject(new Error(missingPolish));
  await first;
  assert.match(h.snapshot().html, /data-action="preview-hub-repository"/);
  assert.match(h.snapshot().html, /查看仓库可用技能/);
  assert.match(h.snapshot().html, /未找到技能/);
  const recovery = h.click({ action: "preview-hub-repository" });
  assert.deepEqual(h.calls, [["preview", polishURL, "release", "skills-scope"], ["previewHubRepository", polishURL, "release", "skills-scope"]]);
  assert.equal(h.snapshot().page, "discover");
  assert.equal(h.snapshot().tab, "hub");
  assert.equal(h.snapshot().request?.selectionMode, "none");
  assert.equal(h.snapshot().returnTo, returnTo);
  assert.match(h.snapshot().html, /正在读取仓库技能/);
  assert.match(h.snapshot().html, /正在读取仓库并查找当前可用技能/);
  await h.click({ action: "preview-hub-repository" });
  assert.equal(h.calls.length, 2, "a second click cannot start another recovery while loading");
  h.pending[1].resolve(currentRepository);
  await recovery;
  assert.deepEqual([...h.snapshot().selected], []);
  assert.equal(h.snapshot().missing, "polish");
  assert.match(h.snapshot().html, /未找到 polish，请选择当前仓库中的技能/);
  assert.match(h.snapshot().html, /name="candidate" value=".agents\/skills\/impeccable" >/);
  assert.match(h.snapshot().html, /data-action="install" disabled/);
  await h.choose(".agents/skills/impeccable", true);
  assert.equal(h.install.disabled, false);
  assert.equal(h.candidateCount.textContent, "已选择 1 个技能");
  await h.click({ themePreference: "dark" });
  assert.match(h.snapshot().html, /未找到 polish，请选择当前仓库中的技能/);
  assert.match(h.snapshot().html, /name="candidate" value=".agents\/skills\/impeccable" checked/);
  assert.doesNotMatch(h.snapshot().html, /data-action="install" disabled/);
  await h.click({ page: "skills" });
  await h.click({ page: "discover" });
  assert.match(h.snapshot().html, /未找到 polish，请选择当前仓库中的技能/);
  assert.equal(h.calls.length, 2, "page changes, theme changes, and manual selection never install a skill");
  h.setResults([{ source: "other/repo", slug: "other", name: "other" }, origin]);
  await h.click({ action: "close-preview" });
  assert.equal(h.snapshot().missing, "");
  assert.equal(h.hubScroller.scrollTop, 420);
  assert.deepEqual(h.focused.at(-1), { selector: '[data-action="hub-preview"][data-index="1"]', preventScroll: true });
});

test("recovery retries keep the repository API and empty selection mode", async () => {
  const h = harness({ actualPanel: true });
  const first = h.run(polishURL, "release", "skill-scope", "polish");
  h.pending[0].reject(new Error(missingPolish));
  await first;
  const recovery = h.click({ action: "preview-hub-repository" });
  h.pending[1].reject(new Error("cannot clone repository: connection interrupted"));
  await recovery;
  assert.equal(h.snapshot().request?.kind, "hub-repository");
  assert.equal(h.snapshot().request?.selectionMode, "none");
  assert.doesNotMatch(h.snapshot().html, /data-action="preview-hub-repository"/);
  const retry = h.click({ action: "retry-preview" });
  assert.deepEqual(h.calls[2], ["previewHubRepository", polishURL, "release", "skill-scope"]);
  await h.click({ page: "skills" });
  h.pending[2].resolve({ ...currentRepository, candidates: [{ name: "polish", subpath: "skills/polish" }] });
  await retry;
  assert.deepEqual([...h.snapshot().selected], [], "even a candidate matching the missing name stays unselected");
  await h.click({ page: "discover" });
  assert.match(h.snapshot().html, /data-action="install" disabled/);
  assert.match(h.snapshot().html, /请选择当前仓库中的技能/);
  assert.doesNotMatch(h.snapshot().html, /未找到 polish/);
  assert.equal(h.availability.get(polishURL)?.state, "missing", "repository-wide candidates do not prove the exact selector is usable");
  assert.equal(h.snapshot().request, null);
  const next = h.run("team/other", "", "");
  assert.equal(h.snapshot().missing, "", "starting another preview clears the recovery notice immediately");
  h.pending[3].resolve(currentRepository);
  await next;
  assert.doesNotMatch(h.snapshot().html, /preview-recovery-note|未找到 polish/);
});

test("recovery remains safe after navigation, closure, late completion, and a new ordinary preview", async () => {
  for (const succeeds of [true, false]) {
    const h = harness({ actualPanel: true });
    const first = h.run(polishURL, "", "", "polish");
    h.pending[0].reject(new Error(missingPolish));
    await first;
    const recovery = h.click({ action: "preview-hub-repository" });
    await h.click({ page: "skills" });
    await h.click({ themePreference: "dark" });
    await h.click({ page: "discover" });
    assert.match(h.snapshot().html, /正在读取仓库技能/);
    await h.click({ action: "close-preview" });
    if (succeeds) h.pending[1].resolve(currentRepository);
    else h.pending[1].reject(new Error("late repository failure"));
    await recovery;
    await h.click({ page: "discover" });
    assert.equal(h.snapshot().preview, null);
    assert.equal(h.snapshot().request, null);
    assert.equal(h.snapshot().missing, "");
    assert.equal(h.snapshot().html, "");
    const next = h.run("team/next", "", "");
    h.pending[2].resolve({ ...currentRepository, id: "next", candidates: [{ name: "next", subpath: "skills/next" }] });
    await next;
    assert.deepEqual([...h.snapshot().selected], ["skills/next"], "ordinary previews retain their default selection");
    assert.doesNotMatch(h.snapshot().html, /preview-recovery-note|未找到 polish/);
  }
});

test("unsafe URLs and other diagnostics cannot reach the repository recovery API through the click route", async () => {
  const cases = [
    [polishURL, 'skills.sh skill "polish" did not match exactly one repository skill'],
    [polishURL, 'skills.sh skill "polish" is unavailable: symlink is not supported'],
    [polishURL, "cannot clone repository: permission denied"],
    ["https://skills.sh/site/open.feishu.cn/lark-doc", missingPolish],
    ["https://skills.sh:443/pbakaus/impeccable/polish", missingPolish],
    ["https://skills.sh/pbakaus/impeccable/%70olish", missingPolish],
  ];
  for (const [source, error] of cases) {
    const h = harness();
    const first = h.run(source, "", "", "polish");
    h.pending[0].reject(new Error(error));
    await first;
    assert.doesNotMatch(h.snapshot().html, /data-action="preview-hub-repository"/);
    await h.click({ action: "preview-hub-repository" });
    assert.equal(h.calls.length, 1, source);
  }
});

test("confirmed missing and unreadable cards open current repository skills directly through their existing click action", async () => {
  for (const state of ["missing", "unavailable"] as const) {
    const h = harness({ actualPanel: true });
    const item = { source: "pbakaus/impeccable", slug: "pbakaus/impeccable/polish", name: "polish", url: polishURL };
    h.setResults([item]);
    h.availability.record({ url: polishURL, state, revision: "current", checkedAt: "", reason: state === "unavailable" ? "unreadable content" : "" });
    const work = h.click({ action: "hub-preview", index: "0" });
    assert.deepEqual(h.calls, [["previewHubRepository", polishURL, "", ""]]);
    assert.equal(h.snapshot().request?.selectionMode, "none");
    h.pending[0].resolve(currentRepository); await work;
    assert.deepEqual([...h.snapshot().selected], []);
    assert.match(h.snapshot().html, /请选择当前仓库中的技能/);
    if (state === "missing") assert.match(h.snapshot().html, /未找到 polish/);
    else assert.doesNotMatch(h.snapshot().html, /未找到 polish/);
  }
});

test("ambiguous repository candidates never turn an unavailable card into an available preview", async () => {
  const h = harness({ actualPanel: true });
  const source = "https://skills.sh/team/repo/ambiguous";
  h.setResults([{ source: "team/repo", slug: "team/repo/ambiguous", name: "ambiguous", url: source }]);
  h.availability.record({ url: source, state: "unavailable", revision: "current", checkedAt: "", reason: 'skills.sh skill "ambiguous" matches multiple repository skills: ".agents/skills/first", ".agents/skills/second"; specify a skill subdirectory' });
  const recovery = h.click({ action: "hub-preview", index: "0" });
  h.pending[0].resolve({ ...currentRepository, candidates: [
    { name: "ambiguous", subpath: ".agents/skills/first" },
    { name: "ambiguous", subpath: ".agents/skills/second" },
  ] });
  await recovery;
  assert.equal(h.availability.get(source)?.state, "unavailable");
  assert.deepEqual([...h.snapshot().selected], []);
  assert.doesNotMatch(h.snapshot().html, /未找到 ambiguous/);
  assert.match(h.snapshot().html, /data-action="install" disabled/);
  await h.click({ action: "close-preview" });
  assert.equal(h.availability.get(source)?.state, "checking", "closing rechecks the exact selector after a repository preview");
  h.checks[0].resolve([{ url: source, state: "unavailable", revision: "current", checkedAt: "", reason: "ambiguous selector" }]);
  await new Promise<void>((resolve) => setImmediate(resolve));
  assert.equal(h.availability.get(source)?.state, "unavailable");
  const again = h.click({ action: "hub-preview", index: "0" });
  assert.equal(h.calls[1][0], "previewHubRepository");
  h.pending[1].resolve(currentRepository); await again;
});

test("a filtered repository preview with only one visible candidate cannot prove an ambiguous selector available", async () => {
  const h = harness({ actualPanel: true });
  const source = "https://skills.sh/team/repo/ambiguous";
  h.setResults([{ source: "team/repo", slug: "team/repo/ambiguous", name: "ambiguous", url: source }]);
  h.availability.record({ url: source, state: "unavailable", revision: "current", checkedAt: "", reason: "ambiguous selector" });
  const recovery = h.click({ action: "hub-preview", index: "0" });
  h.pending[0].resolve({ ...currentRepository, candidates: [{ name: "ambiguous", subpath: ".agents/skills/first" }], skipped: [{ subpath: ".agents/skills/second", reason: "skill content exceeds the 100 MiB preview limit" }] });
  await recovery;
  assert.equal(h.availability.get(source)?.state, "unavailable");
  assert.doesNotMatch(h.snapshot().html, /未找到 ambiguous/);
});

test("foreground previews and navigation cancel checks; closing resumes only unfinished visible repositories", async () => {
  const h = harness({ actualPanel: true });
  const otherURL = "https://skills.sh/other/repo/review";
  h.setResults([
    { source: "pbakaus/impeccable", slug: "pbakaus/impeccable/polish", name: "polish", url: polishURL },
    { source: "other/repo", slug: "other/repo/review", name: "review", url: otherURL },
  ]);
  const check = h.availability.run([polishURL, otherURL]);
  const preview = h.run(polishURL, "", "", "polish");
  assert.equal(h.checks[0].cancellations, 1);
  h.checks[0].resolve([{ url: polishURL, state: "missing", revision: "old", checkedAt: "", reason: "" }]); await check;
  h.pending[0].resolve({ ...currentRepository, candidates: [{ name: "polish", subpath: "skills/polish" }] }); await preview;
  assert.equal(h.availability.get(polishURL)?.state, "available");
  await h.click({ action: "close-preview" });
  assert.equal(h.checks.length, 2, "closing resumes the other repository that was canceled before it started");
  assert.equal(h.availability.get(polishURL)?.state, "available", "completed foreground proof remains available");
  await h.click({ page: "skills" });
  assert.equal(h.checks[1].cancellations, 1);
  h.checks[1].resolve([{ url: otherURL, state: "missing", revision: "old", checkedAt: "", reason: "" }]);
  await new Promise<void>((resolve) => setImmediate(resolve));
  assert.equal(h.availability.get(otherURL)?.state, "unknown", "a canceled late result cannot mark another page's card missing");
});
