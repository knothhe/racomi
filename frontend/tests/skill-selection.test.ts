import assert from "node:assert/strict";
import { test } from "node:test";
import { changeSelection } from "../src/skill-selection.ts";

test("select all fills partial results, then clears only the visible selection", () => {
  const original = new Set(["hidden", "a"]);
  const selected = changeSelection(original, ["a", "b"], "toggle-all");
  assert.deepEqual([...selected].sort(), ["a", "b", "hidden"]);
  assert.deepEqual([...changeSelection(selected, ["a", "b"], "toggle-all")], ["hidden"]);
  assert.deepEqual([...original], ["hidden", "a"]);
});

test("invert flips each filtered result and preserves selections outside the filter", () => {
  const original = new Set(["hidden", "a"]);
  const inverted = changeSelection(original, ["a", "b"], "invert");
  assert.deepEqual([...inverted].sort(), ["b", "hidden"]);
  assert.deepEqual([...changeSelection(inverted, ["a", "b"], "invert")].sort(), [...original].sort());
});

test("empty results leave selections intact for either action", () => {
  for (const action of ["toggle-all", "invert"] as const) {
    assert.deepEqual([...changeSelection(new Set(["hidden"]), [], action)], ["hidden"]);
  }
});
