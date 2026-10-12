import assert from "node:assert/strict";
import test from "node:test";
import { syncNavigationElement } from "../src/navigation-render.ts";

class NavigationNode {
  tagName: string;
  nodeType: number;
  nodeValue: string | null;
  parent: NavigationNode | null = null;
  childNodes: NavigationNode[] = [];
  values = new Map<string, string>();
  mutations = 0;
  constructor(tagName: string, attributes: Record<string, string> = {}, children: (NavigationNode | string)[] = []) {
    this.tagName = tagName;
    this.nodeType = tagName ? 1 : 3;
    this.nodeValue = tagName ? null : attributes.text;
    this.values = new Map(tagName ? Object.entries(attributes) : []);
    for (const child of children) this.insertBefore(typeof child === "string" ? new NavigationNode("", { text: child }) : child, null);
    this.mutations = 0;
  }
  get attributes() { return [...this.values].map(([name, value]) => ({ name, value })); }
  getAttribute(name: string) { return this.values.get(name) ?? null; }
  hasAttribute(name: string) { return this.values.has(name); }
  setAttribute(name: string, value: string) { this.values.set(name, value); this.mutations++; }
  removeAttribute(name: string) { this.values.delete(name); this.mutations++; }
  insertBefore(node: NavigationNode, before: NavigationNode | null) {
    if (node.parent) node.parent.removeChild(node);
    this.childNodes.splice(before ? this.childNodes.indexOf(before) : this.childNodes.length, 0, node);
    node.parent = this;
    this.mutations++;
  }
  removeChild(node: NavigationNode) {
    this.childNodes.splice(this.childNodes.indexOf(node), 1);
    node.parent = null;
    this.mutations++;
  }
  cloneNode() {
    return new NavigationNode(this.tagName, this.tagName ? Object.fromEntries(this.values) : { text: this.nodeValue || "" }, this.childNodes.map((child) => child.cloneNode()));
  }
  isEqualNode(other: NavigationNode): boolean {
    return this.tagName === other.tagName && this.nodeValue === other.nodeValue
      && this.attributes.length === other.attributes.length && this.attributes.every(({ name, value }) => other.getAttribute(name) === value)
      && this.childNodes.length === other.childNodes.length && this.childNodes.every((child, index) => child.isEqualNode(other.childNodes[index]));
  }
}

const node = (tag: string, attributes: Record<string, string> = {}, children: (NavigationNode | string)[] = []) => new NavigationNode(tag, attributes, children);
const page = (id: string, active = false, count?: number) => node("BUTTON", { "data-page": id, class: active ? "nav-item active" : "nav-item", ...(active ? { "aria-current": "page" } : {}) }, [node("SPAN", {}, [id]), ...(count ? [node("SPAN", { class: "nav-count" }, [String(count)])] : [])]);
const sub = (active: string) => node("DIV", { "data-nav-section": "skills", class: "nav-sub" }, ["all", "updates", "pending"].map((filter) => node("BUTTON", { "data-action": "filter-skills", "data-filter": filter, "aria-pressed": String(active === filter) }, [filter])));
const sync = (current: NavigationNode, next: NavigationNode) => syncNavigationElement(current as unknown as Element, next as unknown as Element);

test("primary navigation keeps existing controls when inserting or removing a subnavigation", () => {
  const discover = page("discover", true), skills = page("skills"), global = page("global", false, 2);
  const navigation = node("NAV", {}, [discover, skills, global]);
  sync(navigation, node("NAV", {}, [page("discover"), page("skills", true), sub("all"), page("global", false, 1)]));
  assert.equal(navigation.childNodes[0], discover);
  assert.equal(navigation.childNodes[1], skills);
  assert.equal(navigation.childNodes[3], global);
  assert.equal(discover.hasAttribute("aria-current"), false);
  assert.equal(skills.getAttribute("aria-current"), "page");
  assert.equal(global.childNodes[1].childNodes[0].nodeValue, "1");
  sync(navigation, node("NAV", {}, [page("discover"), page("skills"), page("global", true)]));
  assert.deepEqual(navigation.childNodes, [discover, skills, global]);
  assert.equal(global.childNodes.length, 1, "a count returning to zero removes the badge");
});

test("secondary navigation updates the active state without remounting its container or buttons", () => {
  const submenu = sub("all"), controls = [...submenu.childNodes];
  const navigation = node("NAV", {}, [page("skills", true), submenu]);
  sync(navigation, node("NAV", {}, [page("skills", true), sub("pending")]));
  assert.equal(navigation.childNodes[1], submenu);
  assert.deepEqual(submenu.childNodes, controls);
  assert.equal(controls[0].getAttribute("aria-pressed"), "false");
  assert.equal(controls[2].getAttribute("aria-pressed"), "true");
  const mutations = [navigation.mutations, submenu.mutations, ...controls.map((control) => control.mutations)];
  sync(navigation, node("NAV", {}, [page("skills", true), sub("pending")]));
  assert.deepEqual([navigation.mutations, submenu.mutations, ...controls.map((control) => control.mutations)], mutations, "an unrelated page render leaves navigation unchanged");
});

test("locale changes retain controls while updating visible and accessible labels", () => {
  const button = node("BUTTON", { "data-page": "settings", "aria-label": "设置", title: "设置" }, [node("SPAN", {}, ["设置"])]);
  const label = button.childNodes[0], text = label.childNodes[0];
  sync(button, node("BUTTON", { "data-page": "settings", "aria-label": "Settings", title: "Settings" }, [node("SPAN", {}, ["Settings"])]));
  assert.equal(button.childNodes[0], label);
  assert.equal(label.childNodes[0], text);
  assert.equal(text.nodeValue, "Settings");
  assert.equal(button.getAttribute("aria-label"), "Settings");
});

test("settings tabs retain focus targets while moving their selected and keyboard entry states", () => {
  const settingsTabs = (backups: boolean) => node("DIV", { class: "tabs settings-tabs", role: "tablist" }, [false, true].map((isBackups) => node("BUTTON", {
    role: "tab", "data-action": isBackups ? "open-backups" : "close-backups", "data-tab": isBackups ? "backups" : "settings",
    "aria-controls": "settings-tab-panel", "aria-selected": String(isBackups === backups), tabindex: isBackups === backups ? "0" : "-1",
  }, [isBackups ? "Backup and migration" : "Settings"])));
  const tabs = settingsTabs(false), controls = [...tabs.childNodes];
  sync(tabs, settingsTabs(true));
  assert.deepEqual(tabs.childNodes, controls);
  assert.deepEqual(controls.map((control) => control.getAttribute("aria-selected")), ["false", "true"]);
  assert.deepEqual(controls.map((control) => control.getAttribute("tabindex")), ["-1", "0"]);
  assert.ok(controls.every((control) => control.getAttribute("aria-controls") === "settings-tab-panel"));
});
