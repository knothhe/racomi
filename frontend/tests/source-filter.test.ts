import assert from "node:assert/strict";
import test from "node:test";
import { filterSourceOptions, renderSourceFilter, sourcePopupPosition } from "../src/source-filter.ts";

const options = [
  { id: "all", name: "全部来源" },
  { id: "local", name: "本地技能" },
  { id: "design", name: "Design team", description: "https://github.com/Example/UI-Skills" },
  { id: "code", name: "代码工具", description: "example/coding" },
];

test("source search matches names and repository text without changing option order", () => {
  assert.deepEqual(filterSourceOptions(options, "  DESIGN  ").map((option) => option.id), ["design"]);
  assert.deepEqual(filterSourceOptions(options, "example/").map((option) => option.id), ["design", "code"]);
  assert.deepEqual(filterSourceOptions(options, "代码").map((option) => option.id), ["code"]);
  assert.deepEqual(filterSourceOptions(options, "missing"), []);
  assert.deepEqual(filterSourceOptions(options, "  "), options);
});

test("source control keeps the selected value and escapes source content in text and attributes", () => {
  const html = renderSourceFilter({ name: "preset-source", selected: "unsafe", options: [{ id: "unsafe", name: 'Design <tools> "team"', description: 'repo/" onfocus="alert(1)' }], label: "Source", searchLabel: "Search sources", emptyLabel: "No sources" });
  assert.match(html, /type="hidden" name="preset-source" value="unsafe"/);
  assert.match(html, /aria-selected="true"/);
  assert.match(html, /Design &lt;tools&gt; &quot;team&quot;/);
  assert.match(html, /repo\/&quot; onfocus=&quot;alert\(1\)/);
  assert.doesNotMatch(html, /<tools>| onfocus="/);
  assert.match(html, /role="combobox"[^>]*aria-controls="source-filter-preset-source-options"/);
});

test("source popup opens below with enough room and flips above near the viewport bottom", () => {
  const anchor = { left: 200, right: 390, top: 100, bottom: 132, width: 190 };
  const below = sourcePopupPosition(anchor, { width: 800, height: 600 }, 280);
  assert.equal(below.placement, "below");
  assert.equal(below.left, 200);
  assert.equal(below.top, 137);
  assert.equal(below.width, 280);
  const above = sourcePopupPosition({ ...anchor, top: 500, bottom: 532 }, { width: 800, height: 600 }, 280);
  assert.equal(above.placement, "above");
  assert.equal(above.top, 215);
  assert.ok(above.top + 280 < 500);
});

test("source popup stays inside horizontal edges and scrolls when neither side fits", () => {
  const anchor = { left: 660, right: 790, top: 240, bottom: 272, width: 130 };
  const constrained = sourcePopupPosition(anchor, { width: 800, height: 500 }, 300);
  assert.equal(constrained.left, 508);
  assert.equal(constrained.placement, "above");
  assert.equal(constrained.maxHeight, 223);
  assert.equal(constrained.top, 12);
  const narrow = sourcePopupPosition({ ...anchor, left: -20 }, { width: 260, height: 500 }, 300);
  assert.equal(narrow.left, 12);
  assert.equal(narrow.width, 236);
});

test("source popup respects the visible viewport offset and short search results", () => {
  const anchor = { left: 180, right: 500, top: 350, bottom: 382, width: 320 };
  const layout = sourcePopupPosition(anchor, { width: 600, height: 400, left: 100, top: 200 }, 90);
  assert.equal(layout.placement, "below");
  assert.equal(layout.top, 387);
  assert.equal(layout.width, 320);
  assert.ok(layout.left >= 112 && layout.left + layout.width <= 688);
  assert.ok(layout.top + 90 <= 588);
});
