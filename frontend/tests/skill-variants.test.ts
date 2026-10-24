import assert from "node:assert/strict";
import test from "node:test";
import { distributionKey, distributionPriority, distributionTier, selectedVariant, variantOptions } from "../src/skill-variants.ts";

test("distribution identity uses the declared name and retains repository and child scopes", () => {
  assert.equal(distributionKey("captions-overlay", ".agents/skills/captions-overlay"), distributionKey("captions-overlay", ".claude/skills/generated-copy"));
  assert.equal(distributionKey("captions-overlay", "skills/captions-overlay"), distributionKey("captions-overlay", ".agents/skills/captions-overlay"));
  assert.notEqual(distributionKey("shared", "tests/.agents/skills/shared"), distributionKey("shared", ".agents/skills/shared"));
  assert.notEqual(distributionKey("shared", "skills/one/shared"), distributionKey("shared", "skills/two/shared"));
  assert.notEqual(distributionKey("shared", "examples/first"), distributionKey("shared", "examples/second"));
  assert.equal(distributionKey("shared", "skills/.curated/shared"), distributionKey("shared", "skills/.experimental/shared"));
  assert.equal(distributionKey("shared", "skills/.curated/shared"), distributionKey("shared", ".agents/skills/shared"));
  assert.notEqual(distributionKey("first", ".agents/skills/shared"), distributionKey("second", ".agents/skills/shared"));
});

test("distribution tiers identify same-location ambiguity and canonical preference matches the backend", () => {
  assert.equal(distributionTier("skills/first"), distributionTier("skills/second"));
  assert.notEqual(distributionTier("skills/first"), distributionTier(".agents/skills/first"));
  assert.notEqual(distributionTier("tests/.agents/skills/first"), distributionTier(".agents/skills/first"));
  assert.ok(distributionPriority("skills/shared") < distributionPriority(".agents/skills/shared"));
  assert.ok(distributionPriority(".agents/skills/shared") < distributionPriority(".claude/skills/shared"));
  assert.ok(distributionPriority(".claude/skills/shared") < distributionPriority("examples/shared"));
});

test("identical copies expose one choice while retaining the chosen physical path", () => {
  const canonical = { subpath: ".agents/skills/shared", contentHash: "same", variants: [
    { subpath: ".claude/skills/shared", contentHash: "same" },
    { subpath: ".codex/skills/shared", contentHash: "different" },
  ] };
  assert.deepEqual(variantOptions(canonical).map((item) => item.subpath), [".agents/skills/shared", ".codex/skills/shared"]);
  assert.deepEqual(variantOptions(canonical, ".claude/skills/shared").map((item) => item.subpath), [".claude/skills/shared", ".codex/skills/shared"]);
  assert.equal(selectedVariant(canonical, ".claude/skills/shared").subpath, ".claude/skills/shared");
  assert.equal(selectedVariant(canonical, "missing").subpath, canonical.subpath);
});

test("old previews with unknown hashes keep every physical option", () => {
  const canonical = { subpath: ".agents/skills/shared", variants: [{ subpath: ".claude/skills/shared" }] };
  assert.equal(variantOptions(canonical).length, 2);
  assert.deepEqual(variantOptions({ subpath: "shared" }), [{ subpath: "shared" }]);
});
