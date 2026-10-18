import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";
import ts from "typescript";
import { createProjectSelectionAutosave, type ProjectSelectionAutosave } from "../src/project-selection-autosave.ts";

// Exercise the real selection handlers and workspace-response reconciliation,
// since serializing the queue alone does not prevent switching batch drafts.
const mainSource = ts.createSourceFile("main.ts", readFileSync(new URL("../src/main.ts", import.meta.url), "utf8"), ts.ScriptTarget.Latest, true);
const functionNames = new Set([
  "accept", "saveApplicationSelection", "globalDirectorySelectionBlocked", "syncBusyControls",
  "sameIDs", "sameSelection", "savedSelection", "selectionDrafts", "applicationKey", "applicationSelection",
  "applicationDirty", "findApplication", "globalBatchKeys", "globalBatchID", "targetPaths",
]);
const production = mainSource.statements.filter((statement) => {
  if (ts.isFunctionDeclaration(statement)) return !!statement.name && functionNames.has(statement.name.text);
  if (ts.isVariableStatement(statement)) return statement.declarationList.declarations.some((declaration) => declaration.name.getText(mainSource) === "selectionAutosave");
  if (!ts.isExpressionStatement(statement) || !ts.isCallExpression(statement.expression)) return false;
  const call = statement.expression;
  return (call.expression.getText(mainSource) === "app.addEventListener" && call.arguments[0]?.getText(mainSource) === '"change"')
    || (call.expression.getText(mainSource) === "document.addEventListener" && call.arguments[0]?.getText(mainSource) === '"click"');
}).map((statement) => statement.getText(mainSource)).join("\n");

class Control {
  dataset: Record<string, string> = {};
  disabled = false;
  name = "";
  value = "";
  checked = false;
  form = undefined;
  attributes = new Map<string, string>();
  closest(_selector: string): Control | null { return null; }
  setAttribute(name: string, value: string) { this.attributes.set(name, value); }
  removeAttribute(name: string) { this.attributes.delete(name); }
  focus() {}
}
class Input extends Control {}
class Button extends Control {
  closest(selector: string): Control | null { return selector === "button" ? this : null; }
}

function deferred() {
  let resolve!: () => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<void>((accept, decline) => { resolve = accept; reject = decline; });
  return { promise, resolve, reject };
}

function harness() {
  const settings = { deploymentMode: "copy", defaultProjectDirectories: ["agents"], defaultGlobalDirectories: ["A"], customProjectDirectories: [] };
  const backend = {
    state: { settings, skills: [{ id: "X" }, { id: "Y" }], sources: [{ id: "source-first" }, { id: "source-second" }], presets: [], targets: [] }, projects: [], snapshots: [],
    globalApplications: ["A", "B"].map((key) => ({
      key, agentName: key, name: key, path: `/test/${key}`,
      target: { id: key, name: key, kind: "global", path: `/test/${key}`, enabledSkillIds: [] as string[], enabledPresetIds: [] as string[], enabledSourceIds: [] as string[], deployments: [], pending: false },
    })),
  };
  const controls = ["A", "B"].map((key) => Object.assign(new Input(), { name: "global-directory-selection", value: key }));
  const all = Object.assign(new Button(), { dataset: { action: "select-global-directories" } });
  const defaults = Object.assign(new Button(), { dataset: { action: "default-global-directories" } });
  const requests: ReturnType<typeof deferred>[] = [];
  const writes: { keys: string[]; skillIds: string[]; sourceIds: string[] }[] = [];
  const listeners = new Map<string, (event: { target: Control }) => void | Promise<void>>();
  const setup = `
    const projectDrafts = new Map(), globalDrafts = new Map(), plans = new Map();
    let selectedGlobalKeys = new Set(["A", "B"]), selectedGlobalKey = "batch:A,B";
    let data = initial, stateRevision = 0, updates = null, selectedSkill = "", selectedSource = "", loadedCloudSyncAt = "";
    let bulkSkillSelection = new Set(), skillsSourceFilter = "all";
    const documents = new Map(), sourceSelections = new Map(), detailTab = "overview";
    const arr = (value) => value || [], t = (value) => value;
    const defaultSettings = () => initial.state.settings;
    const skills = () => data.state.skills, presets = () => data.state.presets;
    const projects = () => data.projects, sources = () => data.state.sources;
    const globalApplications = () => data.globalApplications, uniqueApplications = (value) => value;
    const settingsAutosave = { idle: () => Promise.resolve() };
    const applySharedPreferences = () => {};
    const activeTasks = new Set(), actionTasks = {}, formTasks = {}, skillSelectionActions = [], applicationActions = [];
    const applicationBusy = () => false, hasPendingMutation = () => false, discoveryBusy = () => false;
    const skillsManagerBusy = () => false, settingsControlsBusy = () => false;
    const api = { setGlobalSelections: write };
    function render() {
      selectedGlobalKey = globalBatchID();
      for (const control of directoryControls) control.checked = selectedGlobalKeys.has(control.value);
      syncBusyControls();
    }
  `;
  const context = {
    createProjectSelectionAutosave, initial: structuredClone(backend), directoryControls: controls,
    HTMLInputElement: Input, HTMLButtonElement: Button, HTMLSelectElement: Control,
    app: { addEventListener: (type: string, listener: (event: { target: Control }) => void) => listeners.set(`app:${type}`, listener) },
    document: {
      addEventListener: (type: string, listener: (event: { target: Control }) => Promise<void>) => listeners.set(`document:${type}`, listener),
      querySelectorAll: (selector: string) => selector === "button,input,select" ? [...controls, all, defaults] : controls,
    },
    write: async (keys: string[], skillIds: string[], presetIds: string[], sourceIds: string[] = []) => {
      writes.push({ keys, skillIds, sourceIds });
      const request = deferred(); requests.push(request);
      await request.promise;
      for (const application of backend.globalApplications) if (keys.includes(application.key)) {
        application.target.enabledSkillIds = skillIds; application.target.enabledPresetIds = presetIds; application.target.enabledSourceIds = sourceIds;
      }
      return structuredClone(backend);
    },
  };
  const compiled = ts.transpileModule(`${setup}\n${production}\nrender();\n({ saveApplicationSelection, selectionAutosave, selection: () => [...selectedGlobalKeys], displayed: () => applicationSelection("global", selectedGlobalKey, findApplication("global", selectedGlobalKey)?.target) });`, {
    compilerOptions: { target: ts.ScriptTarget.ES2022 },
  }).outputText;
  const runtime = vm.runInNewContext(compiled, context) as {
    saveApplicationSelection: (scope: string, id: string, selection: { skillIds: string[]; presetIds: string[]; sourceIds?: string[] }) => void;
    selectionAutosave: ProjectSelectionAutosave;
    selection: () => string[];
    displayed: () => { skillIds: string[]; sourceIds: string[] };
  };
  return {
    runtime, controls, all, defaults, requests, writes, backend,
    saveX() { runtime.saveApplicationSelection("global", "batch:A,B", { skillIds: ["X"], presetIds: [] }); return runtime.selectionAutosave.flush("global:batch:A,B"); },
    uncheckB() { listeners.get("app:change")!({ target: Object.assign(new Input(), { name: "global-directory-selection", value: "B", checked: false }) }); },
    click(action: string) { return listeners.get("document:click")!({ target: Object.assign(new Button(), { dataset: { action } }) }); },
  };
}

const nextTurn = () => new Promise<void>((resolve) => setImmediate(resolve));

test("pending global autosave blocks checkbox, select-all and default directory changes", async () => {
  const h = harness();
  const saved = h.saveX();
  assert.ok([...h.controls, h.all, h.defaults].every((control) => control.disabled), "debounced saves immediately disable directory controls");
  await nextTurn();
  h.uncheckB();
  await h.click("select-global-directories");
  await h.click("default-global-directories");
  assert.deepEqual([...h.runtime.selection()], ["A", "B"]);
  assert.deepEqual([...h.runtime.displayed().skillIds], ["X"], "the pending batch draft remains visible");
  h.requests[0].resolve(); await saved;
  assert.ok([...h.controls, h.all, h.defaults].every((control) => !control.disabled));
  h.uncheckB();
  assert.deepEqual([...h.runtime.selection()], ["A"]);
  assert.deepEqual([...h.runtime.displayed().skillIds], ["X"], "the new batch starts from the saved X selection");
  h.runtime.saveApplicationSelection("global", "batch:A", { skillIds: [...h.runtime.displayed().skillIds, "Y"], presetIds: [] });
  const savedY = h.runtime.selectionAutosave.flush("global:batch:A");
  await nextTurn(); h.requests[1].resolve(); await savedY;
  assert.deepEqual(h.backend.globalApplications[0].target.enabledSkillIds, ["X", "Y"]);
  assert.deepEqual(h.backend.globalApplications[1].target.enabledSkillIds, ["X"]);
});

test("failed global autosave keeps its batch and draft visible until retry succeeds", async () => {
  const h = harness();
  const failed = assert.rejects(h.saveX(), /disk unavailable/);
  await nextTurn(); h.requests[0].reject(new Error("disk unavailable")); await failed;
  assert.equal(h.runtime.selectionAutosave.busy, false);
  assert.ok([...h.controls, h.all, h.defaults].every((control) => control.disabled));
  h.uncheckB(); await h.click("select-global-directories"); await h.click("default-global-directories");
  assert.deepEqual([...h.runtime.selection()], ["A", "B"]);
  assert.deepEqual([...h.runtime.displayed().skillIds], ["X"]);
  assert.match(String(h.runtime.selectionAutosave.state("global:batch:A,B").error), /disk unavailable/);
  h.runtime.selectionAutosave.retry("global:batch:A,B");
  const retried = h.runtime.selectionAutosave.flush("global:batch:A,B");
  await nextTurn(); h.requests[1].resolve(); await retried;
  assert.ok([...h.controls, h.all, h.defaults].every((control) => !control.disabled));
  await h.click("default-global-directories");
  assert.deepEqual([...h.runtime.selection()], ["A"]);
  await h.click("select-global-directories");
  assert.deepEqual([...h.runtime.selection()], ["A", "B"]);
});

test("source-only global selections save to every directory and survive batch switching", async () => {
  const h = harness();
  h.runtime.saveApplicationSelection("global", "batch:A,B", { skillIds: [], presetIds: [], sourceIds: ["source-first"] });
  const saved = h.runtime.selectionAutosave.flush("global:batch:A,B");
  await nextTurn();
  assert.deepEqual([...h.writes[0].sourceIds], ["source-first"]);
  h.requests[0].resolve(); await saved;
  for (const application of h.backend.globalApplications) assert.deepEqual([...application.target.enabledSourceIds], ["source-first"]);
  h.uncheckB();
  assert.deepEqual([...h.runtime.displayed().sourceIds], ["source-first"]);
  h.runtime.saveApplicationSelection("global", "batch:A", { skillIds: [], presetIds: [], sourceIds: ["source-second"] });
  const changed = h.runtime.selectionAutosave.flush("global:batch:A");
  await nextTurn(); h.requests[1].resolve(); await changed;
  await h.click("select-global-directories");
  assert.deepEqual([...h.runtime.displayed().sourceIds].sort(), ["source-first", "source-second"]);
});
