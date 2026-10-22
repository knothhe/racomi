import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";
import ts from "typescript";
import { createHubAvailability } from "../src/hub-availability.ts";

const main = ts.createSourceFile("main.ts", readFileSync(new URL("../src/main.ts", import.meta.url), "utf8"), ts.ScriptTarget.Latest, true);
const functions = new Set(["searchHub", "ensureHubResults", "syncBusyControls", "checkVisibleHubAvailability"]);
const variables = new Set(["searchResults", "searchQuery", "hubResultQuery", "hubListing", "hubSearchGeneration", "hubSearchRequest", "activeTasks", "actionTasks", "formTasks", "skillSelectionActions", "applicationActions", "discoveryBusy"]);
const production = main.statements.filter((statement) => {
  if (ts.isFunctionDeclaration(statement)) return !!statement.name && functions.has(statement.name.text);
  if (ts.isVariableStatement(statement)) return statement.declarationList.declarations.some((declaration) => ts.isIdentifier(declaration.name) && variables.has(declaration.name.text));
  if (!ts.isExpressionStatement(statement) || !ts.isCallExpression(statement.expression)) return false;
  const call = statement.expression;
  return (call.expression.getText(main) === "app.addEventListener" && call.arguments[0]?.getText(main) === '"input"')
    || (call.expression.getText(main) === "document.addEventListener" && call.arguments[0]?.getText(main) === '"submit"');
}).map((statement) => statement.getText(main)).join("\n");

interface Result { name: string; url?: string }
interface Listing { skills: Result[]; total: number; hasMore: boolean; page: number }
interface Pending {
  method: string; argument: string | number; cancellations: number;
  resolve: (result: Result[] | Listing) => void; reject: (error: Error) => void;
}
interface TestEvent { target: unknown; preventDefault: () => void; isComposing?: boolean }

class Control {
  disabled = false;
  dataset: Record<string, string> = {};
  attributes = new Map<string, string>();
  name = "";
  type = "";
  id = "";
  value = "";
  form: ReturnType<typeof hubForm> | null = null;
  closest(selector: string) { return selector === ".discover-content" ? {} : null; }
  setAttribute(name: string, value: string) { this.attributes.set(name, value); }
  removeAttribute(name: string) { this.attributes.delete(name); }
}
class Button extends Control {}

function hubForm(input: Control, submit: Button) {
  return {
    id: "hub-search", dataset: {}, classList: { contains: () => false },
    getAttribute: (name: string) => name === "id" ? "hub-search" : null,
    querySelector: () => submit, input,
  };
}

function harness() {
  let input = new Control();
  const submit = new Button(), more = new Button();
  const results = { innerHTML: "" };
  const form = hubForm(input, submit);
  input.form = form; input.name = "query"; input.dataset.pageSearch = "discover";
  submit.form = form; submit.type = "submit";
  more.dataset.action = "load-more-hub";
  const pending: Pending[] = [], errors: [string, boolean][] = [];
  const checks: { cancellations: number; resolve: (value: unknown[]) => void }[] = [];
  const availability = createHubAvailability({
    check: () => {
      const check = { cancellations: 0 } as typeof checks[number];
      const promise = new Promise<never>((resolve) => { check.resolve = resolve; });
      checks.push(check);
      return Object.assign(promise, { cancel: () => { check.cancellations++; } });
    },
    changed: () => {},
  });
  const listeners = new Map<string, (event: TestEvent) => void | Promise<void>>();
  let renders = 0;
  const request = (method: string, argument: string | number) => {
    const entry = { method, argument, cancellations: 0 } as Pending;
    const promise = new Promise<Result[] | Listing>((resolve, reject) => { entry.resolve = resolve; entry.reject = reject; });
    pending.push(entry);
    return Object.assign(promise, { cancel: () => { entry.cancellations++; } });
  };
  const runtime = vm.runInNewContext(ts.transpileModule(`
    let data = {}, currentPage = "discover", discoverTab = "hub", preview = null, previewRequest = null;
    const hasPendingMutation = () => false, applicationBusy = () => false, settingsControlsBusy = () => false;
    const globalDirectorySelectionBlocked = () => false;
    const status = () => {}, arr = (value) => value || [];
    const field = (form) => form.input.value.trim();
    const hubResults = () => searchResults === null ? "loading" : searchResults.map((result) => result.name).join(",");
    const render = () => { recordRender(searchQuery); resultRegion.innerHTML = hubResults(); syncBusyControls(); };
    ${production}
    ({
      searchHub, ensureHubResults,
      navigate: (page, tab) => { currentPage = page; discoverTab = tab; },
      snapshot: () => ({ results: searchResults, query: searchQuery, resultQuery: hubResultQuery, listing: hubListing, busy: activeTasks.has("正在搜索 skills.sh…") }),
    });
  `, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.None } }).outputText, {
    Error, HTMLButtonElement: Button,
    hubAvailability: availability,
    recordRender: (query: string) => {
      renders++;
      input = new Control();
      input.form = form; input.name = "query"; input.dataset.pageSearch = "discover"; input.value = query;
      form.input = input;
    },
    resultRegion: results, toast: (message: string, error: boolean) => errors.push([message, error]),
    api: { search: (query: string) => request("search", query), listSkills: (page: number) => request("listSkills", page) },
    app: { addEventListener: (name: string, listener: typeof listeners extends Map<string, infer T> ? T : never) => listeners.set(`app:${name}`, listener) },
    document: {
      querySelector: (selector: string) => selector === "#search-results" ? results : null,
      querySelectorAll: () => [input, submit, more],
      addEventListener: (name: string, listener: typeof listeners extends Map<string, infer T> ? T : never) => listeners.set(`document:${name}`, listener),
    },
  }) as {
    searchHub: (query: string, loadMore?: boolean) => Promise<void>; ensureHubResults: () => void;
    navigate: (page: string, tab: string) => void;
    snapshot: () => { results: Result[] | null; query: string; resultQuery: string; listing: Listing | null; busy: boolean };
  };
  return {
    ...runtime, availability, checks, pending, errors, get input() { return input; }, submit, more, results, renders: () => renders,
    type: (value: string, isComposing = false) => { assert.equal(input.disabled, false); input.value = value; listeners.get("app:input")!({ target: input, isComposing, preventDefault() {} }); },
    search: () => listeners.get("document:submit")!({ target: form, preventDefault() {} }) as Promise<void>,
  };
}

const nextTurn = () => new Promise<void>((resolve) => setImmediate(resolve));
const listing = (name: string, page = 0, hasMore = true): Listing => ({ skills: [{ name }], total: 3, hasMore, page });

test("initial Hub loading keeps search editable and submission replaces the default listing", async () => {
  const h = harness();
  h.ensureHubResults();
  h.ensureHubResults();
  assert.equal(h.pending.length, 1, "automatic ensures do not restart a running query");
  assert.equal(h.pending[0].method, "listSkills");
  assert.equal(h.pending[0].argument, 0);
  assert.equal(h.snapshot().busy, true);
  assert.equal(h.input.disabled, false);
  assert.equal(h.submit.disabled, false);
  h.type("  design  ");
  assert.equal(h.snapshot().query, "  design  ");
  const search = h.search();
  assert.equal(h.pending[0].cancellations, 1);
  assert.equal(h.pending[1].method, "search");
  assert.equal(h.pending[1].argument, "design");
  assert.equal(h.submit.disabled, false, "the submit handler permits another search during loading");
  h.ensureHubResults();
  assert.equal(h.pending.length, 2);
  h.pending[0].reject(new Error("cancelled initial listing"));
  await nextTurn();
  assert.equal(h.snapshot().busy, true);
  assert.deepEqual(h.errors, [], "cancelled listing failures stay silent");
  h.pending[1].resolve([{ name: "design result" }]);
  await search;
  assert.equal(h.snapshot().busy, false);
  assert.deepEqual([...h.snapshot().results!], [{ name: "design result" }]);
  assert.equal(h.snapshot().listing, null);
  assert.equal(h.input.disabled, false);
  assert.equal(h.submit.disabled, false);
});

test("query completion updates results without replacing an input during IME composition", async () => {
  const h = harness();
  h.type("design");
  const search = h.search();
  h.type("设计技", true);
  const composingInput = h.input, renders = h.renders();
  assert.equal(h.snapshot().query, "design", "the real input listener defers draft updates during composition");
  assert.equal(h.results.innerHTML, "loading");
  h.pending[0].resolve([{ name: "design result" }]);
  await search;
  assert.equal(h.renders(), renders, "completion updates only the result region");
  assert.equal(h.input, composingInput, "the composing input stays mounted");
  assert.equal(h.input.value, "设计技");
  assert.equal(h.results.innerHTML, "design result");
  assert.equal(h.input.disabled, false);
  h.type("设计技能");
  const next = h.search();
  assert.equal(h.pending[1].argument, "设计技能");
  h.pending[1].resolve([{ name: "Chinese query result" }]);
  await next;
  assert.equal(h.results.innerHTML, "Chinese query result");
});

test("a background Hub query does not rerender another import tab or page on completion", async () => {
  for (const [page, tab] of [["discover", "git"], ["discover", "local"], ["skills", "hub"]]) {
    const h = harness();
    const search = h.searchHub("background");
    h.navigate(page, tab);
    const renders = h.renders();
    h.results.innerHTML = "current form";
    h.pending[0].resolve([{ name: "background result" }]);
    await search;
    assert.equal(h.renders(), renders, `${page}/${tab} retains its active form`);
    assert.equal(h.results.innerHTML, "current form");
    assert.deepEqual([...h.snapshot().results!], [{ name: "background result" }]);
    assert.equal(h.snapshot().busy, false);
  }
});

test("older searches cannot overwrite the latest results or clear its loading state", async () => {
  for (const olderFinishesFirst of [true, false]) {
    const h = harness();
    h.type("old");
    const old = h.search();
    h.type("new");
    const latest = h.search();
    assert.equal(h.pending.length, 2);
    assert.equal(h.pending[0].cancellations, 1);
    if (olderFinishesFirst) {
      const renders = h.renders();
      h.pending[0].resolve([{ name: "old result" }]);
      await old;
      assert.equal(h.renders(), renders, "stale completion does not rerender the active search");
      assert.equal(h.snapshot().results, null);
      assert.equal(h.snapshot().busy, true);
      assert.equal(h.more.disabled, true);
    }
    h.pending[1].resolve([{ name: "new result" }]);
    await latest;
    if (!olderFinishesFirst) {
      h.pending[0].resolve([{ name: "old result" }]);
      await old;
    }
    assert.deepEqual([...h.snapshot().results!], [{ name: "new result" }]);
    assert.equal(h.snapshot().resultQuery, "new");
    assert.equal(h.snapshot().busy, false);
    assert.deepEqual(h.errors, []);
  }
});

test("pagination remains serialized and a fresh search cancels an in-flight next page", async () => {
  const h = harness();
  const initial = h.searchHub("");
  await h.searchHub("", true);
  assert.equal(h.pending.length, 1);
  h.pending[0].resolve(listing("first"));
  await initial;
  const second = h.searchHub("", true);
  await h.searchHub("", true);
  assert.equal(h.pending.length, 2);
  assert.equal(h.pending[1].argument, 1);
  h.pending[1].resolve(listing("second", 1));
  await second;
  assert.deepEqual([...h.snapshot().results!], [{ name: "first" }, { name: "second" }]);
  const third = h.searchHub("", true);
  assert.equal(h.pending[2].argument, 2);
  const latest = h.searchHub("replacement");
  assert.equal(h.pending[2].cancellations, 1);
  h.pending[3].resolve([{ name: "replacement" }]);
  await latest;
  h.pending[2].resolve(listing("stale page", 2, false));
  await third;
  assert.deepEqual([...h.snapshot().results!], [{ name: "replacement" }]);
  assert.equal(h.snapshot().listing, null);
  await h.searchHub("", true);
  assert.equal(h.pending.length, 4, "keyword results cannot load default-list pages");
});

test("only the current failure is reported and a subsequent search can recover", async () => {
  const h = harness();
  const stale = h.searchHub("stale"), failed = h.searchHub("failed");
  h.pending[1].reject(new Error("current failure"));
  await failed;
  h.pending[0].reject(new Error("stale failure"));
  await stale;
  assert.deepEqual(h.errors, [["current failure", true]]);
  assert.equal(h.snapshot().busy, false);
  const retry = h.searchHub("retry");
  assert.equal(h.pending[1].cancellations, 0, "settled requests are cleared");
  h.pending[2].resolve([{ name: "recovered" }]);
  await retry;
  assert.deepEqual([...h.snapshot().results!], [{ name: "recovered" }]);
  assert.equal(h.snapshot().busy, false);
});

test("search results render before background checks and a new query cancels the previous check generation", async () => {
  const h = harness();
  const oldURL = "https://skills.sh/team/old/polish", currentURL = "https://skills.sh/team/current/review";
  const old = h.searchHub("old");
  h.pending[0].resolve([{ name: "old result", url: oldURL }]); await old;
  assert.equal(h.results.innerHTML, "old result");
  assert.equal(h.snapshot().busy, false);
  assert.equal(h.checks.length, 1);
  assert.equal(h.input.disabled, false);
  const fresh = h.searchHub("current");
  assert.equal(h.checks[0].cancellations, 1);
  h.checks[0].resolve([{ url: oldURL, state: "missing", revision: "old", checkedAt: "", reason: "" }]);
  h.pending[1].resolve([{ name: "current result", url: currentURL }]); await fresh;
  assert.equal(h.results.innerHTML, "current result");
  assert.equal(h.availability.get(oldURL), undefined);
  assert.equal(h.availability.get(currentURL)?.state, "checking");
  h.checks[1].resolve([{ url: currentURL, state: "available", revision: "current", checkedAt: "", reason: "" }]);
  await nextTurn();
  assert.equal(h.availability.get(currentURL)?.state, "available");
  assert.equal(h.results.innerHTML, "current result");
});
