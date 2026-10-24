import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";
import ts from "typescript";
import type { PreviewCandidate, Source } from "../src/api.ts";
import { catalogEntries, type SourceEntry } from "../src/source-catalog.ts";
import { variantOptions } from "../src/skill-variants.ts";

const source = ts.transpileModule(readFileSync(new URL("../src/select.ts", import.meta.url), "utf8"), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;

// Exercise the real source/preview markup as well as the shared select events.
const main = ts.createSourceFile("main.ts", readFileSync(new URL("../src/main.ts", import.meta.url), "utf8"), ts.ScriptTarget.Latest, true);
const renderFunctions = main.statements.filter((node) => ts.isFunctionDeclaration(node) && node.name && ["sourceCatalogRow", "previewCandidateRow", "variantPicker"].includes(node.name.text)).map((node) => node.getText(main)).join("\n");
const renderers = vm.runInNewContext(ts.transpileModule(`${renderFunctions}\n({ sourceCatalogRow, previewCandidateRow })`, {
  compilerOptions: { target: ts.ScriptTarget.ES2022 },
}).outputText, {
  variantOptions, arr: (value: unknown) => value || [], activeTasks: new Set(), selectedCandidates: new Set(), sourceSelection: () => new Set(),
  t: (value: string) => value, tMessage: (value: string) => value,
  e: (value: unknown) => String(value ?? "").replaceAll("&", "&amp;").replaceAll('"', "&quot;"),
}) as { sourceCatalogRow: (source: Source, entry: SourceEntry, removed: never[]) => string; previewCandidateRow: (candidate: PreviewCandidate) => string };

type SelectSpec = { name: string; label: string; key?: string; value?: string; options?: string[] };

function selectSpec(html: string): SelectSpec {
  const attributes = html.match(/<select\b([^>]+)>/)![1];
  const attribute = (name: string) => attributes.match(new RegExp(`${name}="([^"]*)"`))?.[1].replaceAll("&quot;", '"').replaceAll("&amp;", "&");
  return {
    name: attribute("name")!, label: attribute("aria-label")!, key: attribute("data-select-key"),
    value: html.match(/<option value="([^"]*)" selected/)![1],
    options: [...html.matchAll(/<option value="([^"]*)"/g)].map((match) => match[1]),
  };
}

function candidate(scope: string): PreviewCandidate {
  return {
    name: "captions", description: "", subpath: `${scope}.agents/skills/captions`, revision: "revision", contentHash: "agents",
    variants: [{ name: "captions", description: "", subpath: `${scope}.claude/skills/captions`, revision: "revision", contentHash: "claude" }],
  };
}

function harness(specs: SelectSpec[]) {
  let active: MockElement;
  let body: MockElement;
  let mutations: (records: unknown[]) => void = () => {};
  class MockElement extends EventTarget {
    tagName: string;
    constructor(tagName: string) { super(); this.tagName = tagName; }
    children: MockElement[] = [];
    parentElement: MockElement | null = null;
    dataset: Record<string, string> = {};
    attributes = new Map<string, string>();
    className = "";
    textContent = "";
    disabled = false;
    hidden = false;
    style: Record<string, string> = {};
    classList = {
      contains: (name: string) => this.className.split(" ").includes(name),
      add: (name: string) => { this.className += ` ${name}`; },
    };
    get isConnected(): boolean { return this === body || !!this.parentElement?.isConnected; }
    get nextElementSibling() { return this.parentElement?.children[this.parentElement.children.indexOf(this) + 1] || null; }
    set innerHTML(value: string) { if (value.includes("<span>")) this.append(new MockElement("SPAN")); }
    setAttribute(name: string, value: string) { this.attributes.set(name, value); }
    getAttribute(name: string) { return this.attributes.get(name) ?? null; }
    append(child: MockElement) { child.parentElement = this; this.children.push(child); }
    after(child: MockElement) {
      child.parentElement = this.parentElement;
      this.parentElement!.children.splice(this.parentElement!.children.indexOf(this) + 1, 0, child);
    }
    contains(element: MockElement): boolean { return element === this || this.children.some((child) => child.contains(element)); }
    remove() {
      if (this.contains(active)) active = body;
      this.parentElement!.children.splice(this.parentElement!.children.indexOf(this), 1);
      this.parentElement = null;
    }
    closest() { return null; }
    matches(selector: string) { return this.tagName === "SELECT" && selector === "select"; }
    querySelectorAll(selector: string): MockElement[] {
      const descendants = this.children.flatMap((child) => [child, ...child.querySelectorAll("*")]);
      if (selector === "*") return descendants;
      if (selector === "select") return descendants.filter((child) => child.tagName === "SELECT");
      if (selector === "span") return descendants.filter((child) => child.tagName === "SPAN");
      return descendants.filter((child) => child.tagName === "BUTTON" && !child.disabled && !child.hidden
        && (!selector.includes('aria-selected="true"') || child.getAttribute("aria-selected") === "true"));
    }
    querySelector(selector: string) { return this.querySelectorAll(selector)[0] || null; }
    insertAdjacentHTML() {}
    getBoundingClientRect() { return { left: 100, top: 100, bottom: 132, width: 200, height: 80 }; }
    focus() { active = this; }
    scrollIntoView() {}
  }
  class MockSelect extends MockElement {
    name: string;
    value: string;
    values: string[];
    constructor(spec: SelectSpec) {
      super("SELECT");
      this.name = spec.name;
      this.setAttribute("aria-label", spec.label);
      if (spec.key !== undefined) this.dataset.selectKey = spec.key;
      this.value = spec.value || "agents";
      this.values = spec.options || ["agents", "claude"];
    }
    get options() { return this.values.map((value) => ({ value, text: value, disabled: false, hidden: false, selected: value === this.value })); }
    get selectedOptions() { return this.options.filter((option) => option.value === this.value); }
  }
  body = new MockElement("BODY");
  active = body;
  const document = Object.assign(new EventTarget(), {
    body, documentElement: new MockElement("HTML"),
    createElement: (tag: string) => new MockElement(tag.toUpperCase()),
    querySelectorAll: (selector: string) => body.querySelectorAll(selector),
  });
  Object.defineProperty(document, "activeElement", { get: () => active });
  let selects: MockSelect[] = [];
  function render(next: SelectSpec[]) {
    for (const child of [...body.children]) child.remove();
    selects = next.map((spec) => new MockSelect(spec));
    for (const select of selects) body.append(select);
    queueMicrotask(() => mutations([{ type: "childList", target: body, addedNodes: selects }]));
  }
  render(specs);
  const exports = {} as typeof import("../src/select.ts");
  vm.runInNewContext(source, {
    exports, require: () => ({ icon: () => "" }), document, Event, queueMicrotask,
    window: new EventTarget(), innerWidth: 1000, innerHeight: 700,
    getComputedStyle: () => ({ getPropertyValue: () => "1" }),
    Element: MockElement, Node: MockElement, HTMLSelectElement: MockSelect,
    HTMLInputElement: class extends MockElement {},
    MutationObserver: class {
      constructor(callback: typeof mutations) { mutations = callback; }
      observe() {}
    },
  });
  exports.installSelects(document as unknown as Document);
  return {
    render,
    get selects() { return selects; },
    get activeElement() { return active; },
    trigger: (index: number) => selects[index].nextElementSibling!,
    onChange(index: number, change: (value: string) => void) {
      const select = selects[index];
      select.addEventListener("change", () => change(select.value));
    },
    async choose(index: number) {
      this.trigger(index).dispatchEvent(new Event("click", { cancelable: true }));
      body.children.find((child) => child.className === "select-popup")!.children[1].dispatchEvent(new Event("click"));
      await Promise.resolve();
    },
  };
}

test("changing the second same-name source variant restores focus to its own group after rendering", async () => {
  const source: Source = { id: "source", name: "Source", repository: "team/repo", ref: "main", subpath: ".", mode: "selected", revision: "revision", refreshedAt: "", lastError: "", catalog: ["", "nested/"].flatMap((scope) => {
    const item = candidate(scope);
    return [item, ...item.variants!].map((variant) => ({ ...variant, contentHash: variant.contentHash!, removed: false, new: false }));
  }) };
  const specs = (choices?: Map<string, string>) => catalogEntries(source, [], "en", choices).map((entry) => selectSpec(renderers.sourceCatalogRow(source, entry, [])));
  const h = harness(specs());
  h.onChange(1, (value) => h.render(specs(new Map([["nested/.agents/skills/captions", value]]))));
  await h.choose(1);
  assert.equal(h.selects[0].value, ".agents/skills/captions");
  assert.equal(h.selects[1].value, "nested/.claude/skills/captions");
  assert.equal(h.activeElement, h.trigger(1), "keyboard focus must stay with the changed skill");
  assert.notEqual(h.trigger(0).dataset.selectTrigger, h.trigger(1).dataset.selectTrigger, "page renders must distinguish same-name triggers");
});

test("preview variant focus follows its stable group when the chosen candidate path changes", async () => {
  const candidates = [candidate(""), candidate("nested/")];
  const specs = () => candidates.map((item) => selectSpec(renderers.previewCandidateRow(item)));
  const h = harness(specs());
  const originalTrigger = h.trigger(1).dataset.selectTrigger;
  h.onChange(1, (value) => {
    const options = variantOptions(candidates[1]);
    const chosen = options.find((option) => option.subpath === value)!;
    candidates[1] = { ...chosen, variants: options.filter((option) => option !== chosen).map(({ variants: _variants, ...option }) => option) };
    h.render(specs());
  });
  await h.choose(1);
  assert.equal(h.selects[1].value, "nested/.claude/skills/captions");
  assert.equal(h.trigger(1).dataset.selectTrigger, originalTrigger, "changing the selected path must not change its focus identity");
  assert.equal(h.activeElement, h.trigger(1));
});

test("a select that stays connected retains focus even when another select has the same label", async () => {
  const h = harness(Array.from({ length: 2 }, () => ({ name: "variant", label: "Choose variant" })));
  await h.choose(1);
  assert.equal(h.activeElement, h.trigger(1));
});

test("a language select keeps keyboard focus when its translated label changes", async () => {
  const h = harness([{ name: "locale-preference", label: "语言" }]);
  h.onChange(0, (value) => h.render([{ name: "locale-preference", label: "Language", value }]));
  await h.choose(0);
  assert.equal(h.activeElement, h.trigger(0));
});
