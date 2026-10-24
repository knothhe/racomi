import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";
import ts from "typescript";
import { isDesktopRuntime } from "../src/desktop-runtime.ts";

const nativeOrigins = ["wails://wails.localhost/", "http://wails.localhost/", "https://wails.localhost/"];
const browserOrigins = ["http://127.0.0.1:9245/tests/browser.html", "http://localhost:9245/", "https://wails.localhost.example.com/", "file:///test/index.html"];

test("desktop origin detection covers all native schemes and keeps browser previews separate", () => {
  for (const url of nativeOrigins) assert.equal(isDesktopRuntime(new URL(url)), true, url);
  for (const url of browserOrigins) assert.equal(isDesktopRuntime(new URL(url)), false, url);
});

function moduleSource(name: string) {
  return ts.transpileModule(readFileSync(new URL(`../src/${name}`, import.meta.url), "utf8"), {
    compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS },
  }).outputText;
}

class Element {
  textContent = "";
  innerHTML = "";
  inert = false;
  isConnected = true;
  id = "";
  children: Element[] = [];
  onclick: (() => void) | undefined;
  nodes = new Map<string, Element>();
  focus() {}
  append(child: Element) { this.children.push(child); }
  replaceChildren() { this.children = []; this.innerHTML = ""; }
  addEventListener() {}
  querySelector(selector: string) {
    if (!this.nodes.has(selector)) this.nodes.set(selector, new Element());
    return this.nodes.get(selector)!;
  }
}

function lifecycleHarness(url: string) {
  const handlers = new Map<string, (event: { data: unknown }) => unknown>();
  const requests: [number, number, boolean][] = [];
  const body = Object.assign(new Element(), { dataset: {} as Record<string, string> });
  const app = Object.assign(new Element(), { id: "app" });
  const modal = new Element();
  const location = new URL(url);
  let ready = 0;
  const exports = {} as { installLifecycle: (options: any) => void };
  const imports: Record<string, unknown> = {
    "@wailsio/runtime": { Events: { On: (name: string, handler: (event: { data: unknown }) => unknown) => { handlers.set(name, handler); return () => handlers.delete(name); } } },
    "./desktop-runtime": { isDesktopRuntime: () => isDesktopRuntime(location) },
    "./api": { api: {
      frontendReady: async () => { ready++; return 1; },
      completeLifecycle: async (id: number, page: number, allow: boolean) => { requests.push([id, page, allow]); return allow; },
    } },
    "./i18n": { t: (value: string) => value },
  };
  vm.runInNewContext(moduleSource("lifecycle.ts"), {
    exports, location, HTMLElement: Element,
    require: (name: string) => { assert.ok(Object.hasOwn(imports, name), name); return imports[name]; },
    document: { body, activeElement: app, createElement: () => new Element(), querySelector: (selector: string) => selector === "#app" ? app : modal },
    window: { addEventListener() {} }, setTimeout,
  });
  return { install: exports.installLifecycle, handlers, requests, body, app, ready: () => ready };
}

for (const url of nativeOrigins) {
  test(`real lifecycle handler registers readiness and waits for saves before quitting at ${url}`, async () => {
    const h = lifecycleHarness(url);
    let finishSave!: () => void;
    const saving = new Promise<void>(resolve => { finishSave = resolve; });
    h.install({ flush: () => saving, busy: () => false, failed: () => false, dirty: () => false, retry() {}, discard() {}, error: assert.fail });
    assert.equal(h.ready(), 1, "the backend must receive FrontendReady");
    const handler = h.handlers.get("racomi:lifecycle");
    assert.ok(handler, "close/update events must have a frontend listener");
    const closing = handler({ data: { id: 7, page: 3, action: "quit" } });
    assert.equal(h.body.dataset.lifecycleBlocked, "true");
    assert.deepEqual(h.requests, [], "an unfinished save cannot authorize quitting");
    finishSave();
    await closing;
    assert.deepEqual(h.requests, [[7, 3, true]]);
    h.handlers.get("racomi:lifecycle-resumed")!({ data: null });
    assert.equal(h.app.inert, false);
  });
}

test("a failed save in the Windows WebView refuses update installation and keeps the page usable", async () => {
  const h = lifecycleHarness("http://wails.localhost/");
  const errors: string[] = [];
  h.install({ flush: async () => { throw Error("disk full"); }, busy: () => false, failed: () => false, dirty: () => false, retry() {}, discard() {}, error: (message: string) => errors.push(message) });
  await h.handlers.get("racomi:lifecycle")!({ data: { id: 8, page: 3, action: "update" } });
  assert.deepEqual(h.requests, [[8, 3, false]]);
  assert.equal(h.body.dataset.lifecycleBlocked, "false");
  assert.equal(h.app.inert, false);
  assert.equal(errors.length, 1);
  assert.match(errors[0], /disk full/);
});

test("browser fixtures do not register native lifecycle calls", () => {
  for (const url of browserOrigins) {
    const h = lifecycleHarness(url);
    h.install({});
    assert.equal(h.ready(), 0);
    assert.equal(h.handlers.size, 0);
  }
});

test("Windows native text size uses the backend zoom while browser previews retain CSS scaling", () => {
  for (const url of ["http://wails.localhost/", "http://127.0.0.1:9245/"]) {
    const location = new URL(url);
    const zoom: number[] = [];
    const styles = new Map<string, string>();
    const exports = {} as { applyInterfaceSettings: (settings: any) => void };
    const imports: Record<string, unknown> = {
      "./api": { api: { setTextSize: async (size: number) => { zoom.push(size); } } },
      "./desktop-runtime": { isDesktopRuntime: () => isDesktopRuntime(location) },
    };
    vm.runInNewContext(moduleSource("interface.ts"), {
      exports, location,
      require: (name: string) => imports[name],
      document: {
        querySelector: () => ({ textContent: "" }),
        documentElement: { dataset: {}, style: { setProperty: (key: string, value: string) => styles.set(key, value) } },
      },
    });
    exports.applyInterfaceSettings({ textSize: 125, interfaceFont: "system" });
    if (isDesktopRuntime(location)) {
      assert.deepEqual(zoom, [125]);
      assert.equal(styles.has("--browser-scale"), false);
    } else {
      assert.deepEqual(zoom, []);
      assert.equal(styles.get("--browser-scale"), "1.25");
    }
  }
});
