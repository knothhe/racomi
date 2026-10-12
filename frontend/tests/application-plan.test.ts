import assert from "node:assert/strict";
import test from "node:test";
import { applicationPlanDisposition, hasApplicationChanges } from "../src/application-plan.ts";

test("ordinary changes remain a normal apply", () => {
  const plan = { add: ["review"], conflicts: [], overwrite: [], blockingConflicts: [] };
  assert.equal(applicationPlanDisposition(plan), "ready");
  assert.equal(hasApplicationChanges(plan), true);
  assert.equal(hasApplicationChanges({ conflicts: [] }), false);
});

test("local content conflicts offer force only when the backend provides reviewed paths", () => {
  const plan = { conflicts: ["review: locally modified"], overwrite: ["/project/.agents/skills/review"], blockingConflicts: [] };
  assert.equal(applicationPlanDisposition(plan), "overwrite");
  assert.equal(hasApplicationChanges(plan), true);
  assert.equal(applicationPlanDisposition({ ...plan, overwrite: [] }), "blocked");
});

test("a duplicate deployment name blocks force even alongside an overwritable conflict", () => {
  const collision = "review: multiple skills have the same deployment name";
  assert.equal(applicationPlanDisposition({ conflicts: [collision], overwrite: ["/skills/review"], blockingConflicts: [collision] }), "blocked");
  assert.equal(applicationPlanDisposition({ conflicts: [], blockingConflicts: [collision] }), "blocked");
});

test("conflicts from older backends fail closed without an explicit classification", () => {
  assert.equal(applicationPlanDisposition({ conflicts: ["unknown conflict"] }), "blocked");
  assert.equal(applicationPlanDisposition({ conflicts: ["unknown conflict"], overwrite: ["/skills/review"] }), "blocked");
});
