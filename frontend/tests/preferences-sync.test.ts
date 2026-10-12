import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";
import ts from "typescript";

const main = ts.createSourceFile("main.ts", readFileSync(new URL("../src/main.ts", import.meta.url), "utf8"), ts.ScriptTarget.Latest, true);
const functions = new Set(["savePreference", "migrateSharedPreferences"]);
const production = main.statements.filter((statement) => ts.isFunctionDeclaration(statement) && statement.name && functions.has(statement.name.text)).map((statement) => statement.getText(main)).join("\n");

function harness() {
  const writes: unknown[] = [];
  const accepted: unknown[] = [];
  const requests: { patch: unknown; onlyMissing: boolean; resolve: (value: unknown) => void }[] = [];
  const code = `
    let data = { state: { settings: { theme: "light", locale: "en", navLayout: "auto" } } };
    let settingsDraft = null, applyingSharedPreferences = false;
    let stateRevision = 1, settingsWriteRevision = 0;
    const settingsAutosave = { save: (patch) => writes.push(patch), busy: false };
    const selectionAutosave = { busy: false };
    const hasPendingMutation = () => false;
    const accept = (next) => accepted.push(next);
    const toast = () => {}, t = (value) => value, tMessage = (value) => value;
    ${production}
    ({ savePreference, migrateSharedPreferences });
  `;
  const runtime = vm.runInNewContext(ts.transpileModule(code, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, {
    writes, accepted,
    api: { saveSettingsPatch: (patch: unknown, onlyMissing: boolean) => new Promise((resolve) => requests.push({ patch, onlyMissing, resolve })) },
  }) as { savePreference: (patch: unknown) => void; migrateSharedPreferences: (patch: unknown) => Promise<void> };
  return { ...runtime, writes, accepted, requests };
}

test("a theme change back to the saved value keeps the final choice while an earlier write is pending", () => {
  const h = harness();
  h.savePreference({ theme: "dark" });
  h.savePreference({ theme: "light" });
  assert.deepEqual(h.writes, [{ theme: "dark" }, { theme: "light" }]);
  h.savePreference({ theme: "light" });
  assert.equal(h.writes.length, 2, "the current draft is deduplicated");
});

test("preference migration fills only missing fields and accepts the current backend response", async () => {
  const h = harness();
  const migrated = h.migrateSharedPreferences({ locale: "zh-CN" });
  assert.equal(h.requests[0].onlyMissing, true);
  assert.deepEqual(h.requests[0].patch, { locale: "zh-CN" });
  const response = { state: { settings: { locale: "en" } } };
  h.requests[0].resolve(response);
  await migrated;
  assert.deepEqual(h.accepted, [response]);
});

test("migration cannot replace preferences edited while its request was waiting", async () => {
  const h = harness();
  const migrated = h.migrateSharedPreferences({ theme: "light" });
  h.savePreference({ theme: "dark" });
  h.requests[0].resolve({ state: { settings: { theme: "light" } } });
  await migrated;
  assert.deepEqual(h.accepted, []);
  assert.deepEqual(h.writes, [{ theme: "dark" }]);
});
