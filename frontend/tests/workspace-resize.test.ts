import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";
import ts from "typescript";
import { splitBounds } from "../src/workspace-resize.ts";

type ResizeModule = typeof import("../src/workspace-resize.ts");
const source = ts.transpileModule(readFileSync(new URL("../src/workspace-resize.ts", import.meta.url), "utf8"), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;

class MockElement {
  dataset: Record<string, string> = {};
  attributes = new Map<string, string>();
  children: MockElement[] = [];
  parentElement: MockElement | null = null;
  id = "";
  className = "";
  title = "";
  hidden = false;
  tabIndex = -1;
  isConnected = true;
  clientWidth = 1000;
  captures = new Set<number>();
  properties = new Map<string, string>();
  classes = new Set<string>();
  style = { left: "", setProperty: (name: string, value: string) => this.properties.set(name, value) };
  classList = { add: (name: string) => this.classes.add(name), remove: (name: string) => this.classes.delete(name) };
  setAttribute(name: string, value: string) { this.attributes.set(name, value); }
  removeAttribute(name: string) {
    this.attributes.delete(name);
    if (name === "data-workspace-split") delete this.dataset.workspaceSplit;
  }
  get firstElementChild() { return this.children[0] || null; }
  querySelector() { return this.children.find((child) => child.dataset.workspaceSplitEdge) || null; }
  append(child: MockElement) { child.parentElement = this; this.children.push(child); }
  closest(selector: string) { return selector === "[data-workspace-split-edge]" && this.dataset.workspaceSplitEdge ? this : null; }
  getBoundingClientRect() { return { width: this.clientWidth, left: 100 }; }
  setPointerCapture(id: number) { this.captures.add(id); }
  hasPointerCapture(id: number) { return this.captures.has(id); }
  releasePointerCapture(id: number) { this.captures.delete(id); }
  focus() {}
}

function harness(options: { key?: string; width?: number; storage?: Record<string, string>; storageFails?: boolean } = {}) {
  const key = options.key || "skills";
  const stored = new Map(Object.entries(options.storage || {}));
  const body = new MockElement();
  let layout = new MockElement();
  layout.clientWidth = options.width || 1000;
  layout.append(new MockElement()); layout.append(new MockElement());
  const window = new EventTarget();
  const document = Object.assign(new EventTarget(), {
    body, createElement: () => new MockElement(),
    querySelectorAll: (selector: string) => selector === (key === "discover" ? ".discover-content.has-preview" : `.${key}-layout`) ? [layout] : [],
  });
  const localStorage = {
    getItem(name: string) { if (options.storageFails) throw new Error("blocked"); return stored.get(name) ?? null; },
    setItem(name: string, value: string) { if (options.storageFails) throw new Error("blocked"); stored.set(name, value); },
    removeItem(name: string) { if (options.storageFails) throw new Error("blocked"); stored.delete(name); },
  };
  const exports = {} as ResizeModule;
  const frames: (() => void)[] = [];
  vm.runInNewContext(source, {
    exports, window, document, localStorage, Element: MockElement,
    requestAnimationFrame: (callback: () => void) => { frames.push(callback); return frames.length; },
    getComputedStyle: (element: MockElement) => ({
      gridTemplateColumns: element.clientWidth <= (key === "projects" ? 620 : 760) ? `${element.clientWidth}px` : `${(element.clientWidth - 16) * .6}px ${(element.clientWidth - 16) * .4}px`,
      columnGap: "16px", paddingLeft: "0px", paddingRight: "0px",
    }),
  });
  function dispatch(target: EventTarget, type: string, fields: Record<string, unknown>) {
    const event = new Event(type, { cancelable: true });
    for (const [name, value] of Object.entries(fields)) Object.defineProperty(event, name, { value });
    target.dispatchEvent(event);
    return event;
  }
  exports.installWorkspaceResize(); exports.syncWorkspaceResize("Resize columns");
  return {
    api: exports, body, stored,
    get layout() { return layout; },
    get edge() { return layout.querySelector()!; },
    get width() { return Number.parseFloat(layout.properties.get("--workspace-columns")!); },
    pointer(type: string, clientX: number, pointerId = 1, button = 0) { return dispatch(document, type, { target: layout.querySelector(), clientX, pointerId, button }); },
    key(name: string, shiftKey = false) { return dispatch(document, "keydown", { target: layout.querySelector(), key: name, shiftKey }); },
    reset() { dispatch(document, "dblclick", { target: layout.querySelector() }); },
    resize(width: number) { layout.clientWidth = width; exports.syncWorkspaceResize(); },
    replace() {
      layout.isConnected = false;
      layout = new MockElement(); layout.append(new MockElement()); layout.append(new MockElement());
      exports.syncWorkspaceResize();
    },
    storage(name: string | null) { dispatch(window, "storage", { key: name }); },
  };
}

test("split bounds preserve both minimum widths and fit smaller workspaces", () => {
  assert.deepEqual(splitBounds(1000, 260, 300), { min: 260, max: 700 });
  const small = splitBounds(280, 260, 300);
  assert.equal(small.min, 130); assert.equal(small.max, 130);
  assert.deepEqual(splitBounds(-10, 260, 300), { min: 0, max: 0 });
});

test("pointer dragging previews widths, persists on release and cancels on Escape", () => {
  const h = harness();
  const initial = h.width;
  assert.equal(h.pointer("pointerdown", initial + 108).defaultPrevented, true);
  assert.equal(h.edge.hasPointerCapture(1), true);
  assert.equal(h.edge.classes.has("is-resizing"), true);
  h.pointer("pointermove", initial + 208, 2);
  assert.equal(h.width, initial, "another pointer cannot resize the panes");
  h.pointer("pointermove", initial + 208);
  assert.ok(h.width > initial);
  assert.equal(h.stored.size, 0, "a preview is not persisted");
  h.key("Escape");
  assert.equal(h.width, initial);
  assert.equal(h.edge.hasPointerCapture(1), false);
  assert.equal(h.edge.classes.has("is-resizing"), false, "Escape clears the drag highlight");
  h.pointer("pointerdown", initial + 108); h.pointer("pointermove", initial + 150); h.pointer("pointerup", initial + 150);
  assert.equal(h.stored.size, 1);
  assert.equal(h.body.classes.has("workspace-split-drag"), false);
  assert.equal(h.edge.classes.has("is-resizing"), false, "the drag highlight ends on release");
  const reloaded = harness({ storage: Object.fromEntries(h.stored) });
  assert.equal(reloaded.width, h.width);
});

test("pointer cancellation and capture loss clear the drag feedback without saving widths", () => {
  for (const ending of ["pointercancel", "lostpointercapture"]) {
    const h = harness();
    const initial = h.width;
    h.pointer("pointerdown", initial + 108);
    h.pointer("pointermove", initial + 200);
    h.pointer(ending, initial + 200);
    assert.equal(h.edge.classes.has("is-resizing"), false);
    assert.equal(h.body.classes.has("workspace-split-drag"), false);
    assert.equal(h.width, initial);
    assert.equal(h.stored.size, 0);
  }
});

test("keyboard resizing clamps both panes, reports separator values and resets to the default", () => {
  const h = harness();
  const initial = h.width;
  h.key("ArrowLeft"); assert.equal(h.width, initial - 8);
  h.key("ArrowRight", true); assert.equal(h.width, initial + 24);
  h.key("Home"); assert.equal(h.width, 260);
  h.key("End"); assert.equal(h.width, 684);
  assert.equal(h.edge.attributes.get("aria-valuenow"), "70");
  assert.equal(h.edge.attributes.get("aria-controls"), "workspace-pane-skills");
  h.reset(); assert.equal(h.width, initial); assert.equal(h.stored.size, 0);
});

test("stacked panes hide their separator while preserving the saved proportion", () => {
  const h = harness();
  h.key("Home");
  const stored = h.stored.get("racomi.workspaceSplit.skills");
  h.resize(720);
  assert.equal(h.edge.hidden, true); assert.equal(h.edge.tabIndex, -1);
  assert.equal(h.layout.dataset.workspaceSplit, undefined);
  h.resize(1200);
  assert.equal(h.edge.hidden, false); assert.equal(h.edge.tabIndex, 0);
  assert.equal(h.width, (1200 - 16) * Number(stored));
  const project = harness({ key: "projects", width: 700 });
  assert.equal(project.edge.hidden, false, "projects retain their narrower horizontal breakpoint");
  project.resize(600); assert.equal(project.edge.hidden, true);
});

test("page renders, external preference updates and blocked storage retain usable controls", () => {
  const h = harness();
  h.key("Home"); h.replace(); assert.equal(h.width, 260);
  h.pointer("pointerdown", 368); h.replace();
  assert.equal(h.body.classes.has("workspace-split-drag"), false);
  h.stored.set("racomi.workspaceSplit.skills", ".5"); h.storage("racomi.workspaceSplit.skills");
  assert.equal(h.width, 492);
  const unavailable = harness({ storageFails: true });
  unavailable.key("Home"); unavailable.replace(); assert.equal(unavailable.width, 260);
  const single = harness({ key: "global" });
  assert.equal(single.layout.querySelector(), null, "the single-column Global page is not registered");
});
