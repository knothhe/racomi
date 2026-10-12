import assert from "node:assert/strict";
import test from "node:test";
import { icon, projectIconName, projectIcons, presetIconName, presetIcons } from "../src/icons.ts";

test("legacy and unrecognized project icons use the default folder", () => {
  for (const candidate of [undefined, "", "removed-icon", "trash", "__proto__", '<img src=x onerror="alert(1)">']) {
    assert.equal(projectIconName(candidate), "folder");
  }
});

test("every project icon choice resolves and renders a known decorative SVG", () => {
  assert.equal(new Set(projectIcons).size, projectIcons.length);
  assert.ok(projectIcons.includes("folder"));
  for (const candidate of projectIcons) {
    assert.equal(projectIconName(candidate), candidate);
    const markup = icon(candidate);
    assert.match(markup, /^<svg\b/);
    assert.match(markup, /aria-hidden="true"/);
    assert.match(markup, /focusable="false"/);
    assert.match(markup, /stroke="currentColor"/);
    assert.doesNotMatch(markup, /undefined|\[object Object\]/);
    if (candidate !== "box") assert.notEqual(markup, icon("unknown-icon"));
  }
});

test("unknown general icon keys safely fall back without injecting their names into markup", () => {
  for (const candidate of ["unknown-icon", "__proto__", "constructor", '<script>alert("icon")</script>']) {
    assert.equal(icon(candidate), icon("box"));
  }
});

test("preset icons expand the existing project choices while keeping the layers default", () => {
  assert.equal(presetIcons[0], "layers");
  assert.equal(new Set(presetIcons).size, presetIcons.length);
  assert.ok(presetIcons.length >= 30);
  for (const candidate of projectIcons) assert.ok(presetIcons.includes(candidate));
  for (const candidate of ["code", "shield", "palette", "database", "file-text", "users"] as const) assert.ok(presetIcons.includes(candidate));
  for (const candidate of [undefined, "", "future-icon", "trash", "__proto__", '<img src=x onerror="alert(1)">']) {
    assert.equal(presetIconName(candidate), "layers");
  }
  for (const candidate of presetIcons) {
    assert.equal(presetIconName(candidate), candidate);
    assert.match(icon(candidate), /^<svg\b/);
    if (candidate !== "box") assert.notEqual(icon(candidate), icon("unknown-icon"));
  }
  // Extra preset choices do not change the interpretation of existing projects.
  assert.equal(projectIconName("rocket"), "folder");
});
