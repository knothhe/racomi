import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";
import ts from "typescript";

type LayoutModule = typeof import("../src/layout.ts");
const source = ts.transpileModule(readFileSync(new URL("../src/layout.ts", import.meta.url), "utf8"), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;

class MockElement {
  dataset: Record<string, string> = {};
  attributes = new Map<string, string>();
  classes = new Set<string>();
  captures = new Set<number>();
  properties = new Map<string, string>();
  tabIndex = -1;
  style = {
    setProperty: (name: string, value: string) => this.properties.set(name, value),
    getPropertyValue: (name: string) => this.properties.get(name) ?? "",
  };
  classList = { add: (name: string) => this.classes.add(name), remove: (name: string) => this.classes.delete(name) };
  setAttribute(name: string, value: string) { this.attributes.set(name, value); }
  closest(selector: string) { return selector === "[data-side-edge]" && this.attributes.has("data-side-edge") ? this : null; }
  setPointerCapture(id: number) { this.captures.add(id); }
  hasPointerCapture(id: number) { return this.captures.has(id); }
  releasePointerCapture(id: number) { this.captures.delete(id); }
}

function harness(options: { width?: number; storage?: Record<string, string>; storageFails?: boolean; rendered?: boolean } = {}) {
  const stored = new Map(Object.entries(options.storage ?? {}));
  const root = new MockElement();
  const body = new MockElement();
  const window = Object.assign(new EventTarget(), { innerWidth: options.width ?? 1200 });
  const media = Object.assign(new EventTarget(), { matches: window.innerWidth >= 1024 });
  const newEdge = () => { const edge = new MockElement(); edge.setAttribute("data-side-edge", ""); return edge; };
  let edge: MockElement | undefined = options.rendered === false ? undefined : newEdge();
  const sidebar = { getBoundingClientRect: () => ({ width: Number.parseFloat(root.style.getPropertyValue("--side-w")) }) };
  const document = Object.assign(new EventTarget(), {
    documentElement: root, body,
    querySelector: (selector: string) => selector === "[data-side-edge]" ? edge : selector === "#racomi-sidebar" && edge ? sidebar : null,
  });
  const layoutEvents: { layout: string; preference: string }[] = [];
  const widthEvents: number[] = [];
  document.addEventListener("racomi:nav-layout-change", (event) => layoutEvents.push((event as CustomEvent).detail));
  document.addEventListener("racomi:sidebar-resize", (event) => widthEvents.push((event as CustomEvent).detail.width));
  const localStorage = {
    getItem(key: string) { if (options.storageFails) throw new Error("blocked"); return stored.get(key) ?? null; },
    setItem(key: string, value: string) { if (options.storageFails) throw new Error("blocked"); stored.set(key, value); },
    removeItem(key: string) { if (options.storageFails) throw new Error("blocked"); stored.delete(key); },
  };
  const exports = {} as LayoutModule;
  vm.runInNewContext(source, { exports, window: Object.assign(window, { matchMedia: () => media }), document, localStorage, CustomEvent, Element: MockElement });
  function dispatch(target: EventTarget, type: string, fields: Record<string, unknown> = {}) {
    const event = new Event(type, { cancelable: true });
    for (const [key, value] of Object.entries(fields)) Object.defineProperty(event, key, { value });
    target.dispatchEvent(event);
    return event;
  }
  return {
    api: exports, root, body, stored, media, layoutEvents, widthEvents,
    get edge() { return edge!; },
    get width() { return Number.parseFloat(root.style.getPropertyValue("--side-w")); },
    render() { edge = newEdge(); return edge; },
    resize(width: number) {
      window.innerWidth = width;
      const changed = media.matches !== (width >= 1024);
      media.matches = width >= 1024;
      if (changed) media.dispatchEvent(new Event("change"));
      window.dispatchEvent(new Event("resize"));
    },
    storage(key: string | null) { dispatch(window, "storage", { key }); },
    pointer(type: string, clientX: number, pointerId = 1, target = edge, button = 0) {
      return dispatch(document, type, { target, clientX, pointerId, button });
    },
    key(key: string, shiftKey = false) { return dispatch(document, "keydown", { target: edge, key, shiftKey }); },
    resetWidth() { dispatch(document, "dblclick", { target: edge }); },
  };
}

test("automatic navigation follows the viewport, while a chosen layout survives resizing and reloads", () => {
  const h = harness({ width: 1024 });
  assert.equal(h.api.getNavLayout(), "side");
  assert.equal(h.root.dataset.navLayoutPreference, "auto");
  h.resize(1023);
  assert.equal(h.api.getNavLayout(), "top");
  h.api.toggleNavLayout();
  assert.equal(h.api.getNavLayoutPreference(), "side");
  h.resize(800);
  assert.equal(h.root.dataset.navLayout, "side");
  assert.equal(h.width, 184);
  const reloaded = harness({ width: 800, storage: Object.fromEntries(h.stored) });
  assert.equal(reloaded.api.getNavLayout(), "side");
  h.api.setNavLayoutPreference("top");
  h.resize(1600);
  assert.equal(h.api.getNavLayout(), "top");
  h.api.setNavLayoutPreference("auto");
  assert.equal(h.api.getNavLayout(), "side");
  assert.equal(h.stored.has("racomi.navLayout"), false);
  assert.equal(h.layoutEvents.at(-1)?.preference, "auto");
});

test("unavailable storage does not break layout choices or resizing for the current session", () => {
  const h = harness({ width: 800, storageFails: true });
  h.api.toggleNavLayout();
  h.api.installSidebarResize();
  h.key("ArrowRight");
  assert.equal(h.width, 192);
  h.resize(1200);
  assert.equal(h.api.getNavLayout(), "side");
  assert.equal(h.width, 192);
  h.api.setNavLayoutPreference("auto");
  h.resize(900);
  assert.equal(h.api.getNavLayout(), "top");
});

test("saved widths are constrained for a narrow window and restored when it grows", () => {
  const h = harness({ storage: { "racomi.sideWidth": "310", "racomi.navLayout": "side" } });
  h.resize(700);
  assert.equal(h.width, 252);
  assert.equal(h.stored.get("racomi.sideWidth"), "310");
  h.resize(1200);
  assert.equal(h.width, 310);
  h.api.installSidebarResize();
  assert.equal(h.edge.attributes.get("role"), "separator");
  assert.equal(h.edge.tabIndex, 0);
  assert.equal(h.edge.attributes.get("aria-controls"), "racomi-sidebar");
  assert.equal(h.edge.attributes.get("aria-orientation"), "vertical");
  h.key("End");
  assert.equal(h.width, 320);
  h.resize(700);
  assert.equal(h.edge.attributes.get("aria-valuemax"), "252");
  assert.equal(h.edge.attributes.get("aria-valuenow"), "252");
  h.key("Home");
  assert.equal(h.width, 168);
  h.key("ArrowLeft", true);
  assert.equal(h.width, 168);
});

test("dragging captures the pointer, previews the width and saves only on release", () => {
  const h = harness();
  h.api.installSidebarResize();
  assert.equal(h.pointer("pointerdown", 210).defaultPrevented, true);
  assert.equal(h.edge.hasPointerCapture(1), true);
  assert.equal(h.body.classes.has("side-drag"), true);
  assert.equal(h.edge.classes.has("is-resizing"), true);
  h.pointer("pointermove", 500, 2);
  assert.equal(h.width, 212, "an unrelated pointer cannot resize the sidebar");
  h.pointer("pointermove", 270);
  assert.equal(h.width, 272, "the pointer keeps its initial offset within the edge");
  assert.equal(h.stored.has("racomi.sideWidth"), false);
  h.pointer("pointerup", 270);
  assert.equal(h.stored.get("racomi.sideWidth"), "272");
  assert.equal(h.edge.hasPointerCapture(1), false);
  assert.equal(h.body.classes.has("side-drag"), false);
  assert.equal(h.edge.classes.has("is-resizing"), false, "the drag highlight ends on release");
  assert.equal(h.widthEvents.at(-1), 272);
});

test("pulling past the collapse threshold switches to top navigation and retains the width before the drag", () => {
  const h = harness({ storage: { "racomi.sideWidth": "260" } });
  h.api.installSidebarResize();
  h.pointer("pointerdown", 260);
  h.pointer("pointermove", 180);
  h.pointer("pointermove", 104);
  assert.equal(h.api.getNavLayout(), "side");
  assert.equal(h.width, 168);
  h.pointer("pointermove", 103);
  assert.equal(h.api.getNavLayout(), "top");
  assert.equal(h.stored.get("racomi.sideWidth"), "260");
  assert.equal(h.body.classes.has("side-drag"), false);
  h.api.toggleNavLayout();
  assert.equal(h.width, 260);
});

test("Escape, pointer cancellation and losing capture restore the original width without saving a preview", () => {
  for (const ending of ["Escape", "pointercancel", "lostpointercapture"]) {
    const h = harness({ storage: { "racomi.sideWidth": "240" } });
    h.api.installSidebarResize();
    h.pointer("pointerdown", 240);
    h.pointer("pointermove", 300);
    assert.equal(h.width, 300);
    if (ending === "Escape") assert.equal(h.key(ending).defaultPrevented, true);
    else h.pointer(ending, 300);
    assert.equal(h.width, 240);
    assert.equal(h.stored.get("racomi.sideWidth"), "240");
    assert.equal(h.body.classes.has("side-drag"), false);
    assert.equal(h.edge.classes.has("is-resizing"), false, `${ending} clears the drag highlight`);
    assert.equal(h.edge.hasPointerCapture(1), false);
    h.pointer("pointerup", 300);
    assert.equal(h.width, 240, "late pointerup cannot commit a cancelled drag");
  }
});

test("delegated controls survive initial empty DOM and later renders without duplicate keyboard handling", () => {
  const h = harness({ rendered: false });
  h.api.installSidebarResize();
  h.render();
  h.api.syncSidebarWidth();
  assert.equal(h.edge.attributes.get("aria-valuenow"), "212");
  h.api.installSidebarResize();
  h.key("ArrowRight");
  assert.equal(h.width, 220);
  h.render();
  h.api.syncSidebarWidth();
  h.key("ArrowLeft", true);
  assert.equal(h.width, 188);
  h.resetWidth();
  assert.equal(h.width, 212);
  assert.equal(h.stored.has("racomi.sideWidth"), false);
  h.api.setNavLayoutPreference("top");
  assert.equal(h.key("ArrowRight").defaultPrevented, false);
  assert.equal(h.width, 212);
});

test("storage events synchronize other windows and invalid preferences fall back to automatic layout", () => {
  const h = harness({ storage: { "racomi.navLayout": "invalid", "racomi.sideWidth": "NaN" } });
  assert.equal(h.api.getNavLayoutPreference(), "auto");
  assert.equal(h.width, 212);
  h.stored.set("racomi.navLayout", "top");
  h.storage("racomi.navLayout");
  assert.equal(h.api.getNavLayout(), "top");
  h.stored.set("racomi.sideWidth", "900");
  h.storage("racomi.sideWidth");
  assert.equal(h.width, 320);
  h.stored.clear();
  h.storage(null);
  assert.equal(h.api.getNavLayout(), "side");
  assert.equal(h.width, 212);
});
