import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";
import ts from "typescript";
import { applicationPlanDisposition, hasApplicationChanges } from "../src/application-plan.ts";
import { createProjectSelectionAutosave, type ProjectSelection, type ProjectSelectionAutosave } from "../src/project-selection-autosave.ts";

// Run the production batch functions with the real autosave queue, task guard
// and workspace reconciliation. Deferred backend responses expose ordering and
// stale reviews that isolated selection/plan helpers cannot exercise.
const mainSource = ts.createSourceFile("main.ts", readFileSync(new URL("../src/main.ts", import.meta.url), "utf8"), ts.ScriptTarget.Latest, true);
const functionNames = new Set([
  "previewProjectBatch", "applyReviewedProjectBatch", "task", "accept", "saveApplicationSelection",
  "sameIDs", "sameSelection", "savedSelection", "selectionDrafts", "applicationKey", "applicationSelection",
  "applicationDirty", "findApplication", "globalBatchKeys", "targetPaths",
]);
const variableNames = new Set(["selectionAutosave", "stateChangingTasks", "hasPendingMutation", "applicationBusy"]);
const production = mainSource.statements.filter((statement) => {
  if (ts.isFunctionDeclaration(statement)) return !!statement.name && functionNames.has(statement.name.text);
  return ts.isVariableStatement(statement) && statement.declarationList.declarations.some((declaration) => variableNames.has(declaration.name.getText(mainSource)));
}).map((statement) => statement.getText(mainSource)).join("\n");

function deferred() {
  let resolve!: () => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<void>((accept, decline) => { resolve = accept; reject = decline; });
  return { promise, resolve, reject };
}
const nextTurn = () => new Promise<void>((resolve) => setImmediate(resolve));
const plain = <T>(value: T): T => structuredClone(value);
const selection = (skill: string, preset: string, source: string): ProjectSelection => ({ skillIds: [skill], presetIds: [preset], sourceIds: [source] });
const initialSelections = [selection("alpha", "preset-first", "source-first"), selection("beta", "preset-second", "source-second"), selection("untouched", "preset-other", "source-other")];

type TestPlan = {
  add: string[]; update: string[]; remove: string[]; conflicts: string[]; blockingConflicts: string[]; overwrite: string[];
  directories: { targetId: string; path: string; add: string[] }[];
};
const readyPlan = (): TestPlan => ({
  add: ["alpha", "beta"], update: [], remove: [], conflicts: [], blockingConflicts: [], overwrite: [],
  directories: ["first", "second"].map((id) => ({ targetId: id, path: `/projects/${id}/.agents/skills`, add: [id === "first" ? "alpha" : "beta"] })),
});

function harness(options: { plan?: TestPlan; deferPlan?: boolean; failApplyOnce?: boolean } = {}) {
  const settings = { deploymentMode: "copy", customProjectDirectories: [] };
  const targets = ["first", "second", "other"].map((id, index) => ({
    id, name: id, kind: "project", projectRoot: `/projects/${id}`, path: `/projects/${id}/.agents/skills`,
    enabledSkillIds: initialSelections[index].skillIds, enabledPresetIds: initialSelections[index].presetIds,
    enabledSourceIds: initialSelections[index].sourceIds, pending: true, deployments: [],
  }));
  const backend = {
    state: { settings, skills: ["alpha", "beta", "gamma", "untouched"].map((id) => ({ id })), sources: ["first", "second", "other"].map((id) => ({ id: `source-${id}` })), presets: ["first", "second", "other"].map((id) => ({ id: `preset-${id}` })), targets },
    projects: targets, snapshots: [], globalApplications: [],
  };
  const events: string[] = [];
  const saves: { id: string; selection: ProjectSelection; gate: ReturnType<typeof deferred> }[] = [];
  const plans: { ids: string[]; selections: ProjectSelection[] }[] = [];
  const applications: { kind: "normal" | "force"; ids: string[]; reviewed?: TestPlan; selections: ProjectSelection[] }[] = [];
  const toasts: { message: string; error: boolean }[] = [];
  const dialogs: { body: string; footer: string }[] = [];
  const planGate = options.deferPlan ? deferred() : undefined;
  let failedApply = false, closed = 0;
  const savedSelections = () => backend.projects.map((project) => ({ skillIds: [...project.enabledSkillIds], presetIds: [...project.enabledPresetIds], sourceIds: [...project.enabledSourceIds] }));
  const apply = async (kind: "normal" | "force", ids: string[], reviewed?: TestPlan) => {
    applications.push({ kind, ids: plain(ids), reviewed, selections: savedSelections() });
    if (options.failApplyOnce && !failedApply) { failedApply = true; throw new Error("deployment disk unavailable"); }
    for (const project of backend.projects) if (ids.includes(project.id)) project.pending = false;
    return plain(backend);
  };
  const api = {
    setProjectSelection: async (id: string, skillIds: string[], presetIds: string[], sourceIds: string[]) => {
      const gate = deferred();
      const draft = plain({ skillIds, presetIds, sourceIds });
      saves.push({ id, selection: draft, gate }); events.push(`save:${id}`);
      await gate.promise;
      const project = backend.projects.find((target) => target.id === id)!;
      project.enabledSkillIds = draft.skillIds; project.enabledPresetIds = draft.presetIds; project.enabledSourceIds = draft.sourceIds;
      events.push(`saved:${id}`);
      return plain(backend);
    },
    planProjects: async (ids: string[]) => {
      plans.push({ ids: plain(ids), selections: savedSelections() }); events.push("plan");
      await planGate?.promise;
      return plain(options.plan || readyPlan());
    },
    applyProjects: (ids: string[]) => apply("normal", ids),
    forceApplyProjects: (ids: string[], reviewed: TestPlan) => apply("force", ids, reviewed),
  };
  const setup = `
    const projectDrafts = new Map(), globalDrafts = new Map(), plans = new Map();
    let data = initial, stateRevision = 0, updates = null, selectedSkill = "", selectedSource = "", loadedCloudSyncAt = "";
    let bulkSkillSelection = new Set(), bulkProjectSelection = new Set(["second", "first"]), skillsSourceFilter = "all";
    let reviewedProjectBatch = null, currentPage = "projects", modalGeneration = 0;
    const documents = new Map(), sourceSelections = new Map(), detailTab = "overview";
    const arr = (value) => value || [], e = (value) => String(value), t = (value) => value;
    const defaultSettings = () => initial.state.settings;
    const skills = () => data.state.skills, presets = () => data.state.presets, sources = () => data.state.sources;
    const projects = () => data.projects, globalApplications = () => data.globalApplications;
    const settingsAutosave = { busy: false, idle: () => Promise.resolve() };
    const activeTasks = new Set(), taskMessages = new Map(), skillsManagerImportTask = "import";
    const render = () => {}, status = () => {}, syncBusyControls = () => {}, applySharedPreferences = () => {};
    const globalPath = (value) => value, projectRoot = (value) => value;
    const planView = () => "<div>production batch plan</div>";
    const button = (label, action, style, attributes) => action + ":" + label + ":" + (attributes || "");
    const modal = (title, description, body, footer) => { modalGeneration++; showDialog(body, footer); };
    const app = {};
  `;
  const compiled = ts.transpileModule(`${setup}\n${production}\n
    const flush = selectionAutosave.flush;
    selectionAutosave.flush = (key) => { events.push("flush:" + key); return flush(key); };
    ({ previewProjectBatch, applyReviewedProjectBatch, selectionAutosave,
      save: (id, value) => saveApplicationSelection("project", id, value),
      accept, review: () => reviewedProjectBatch,
      selection: () => [...bulkProjectSelection],
      clearSelection: () => { bulkProjectSelection.clear(); reviewedProjectBatch = null; },
    });`, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;
  const runtime = vm.runInNewContext(compiled, {
    initial: plain(backend), api, events, createProjectSelectionAutosave, applicationPlanDisposition, hasApplicationChanges,
    toast: (message: string, error = false) => toasts.push({ message, error }),
    showDialog: (body: string, footer: string) => { dialogs.push({ body, footer }); },
    closeModal: () => { closed++; },
  } as object) as Runtime;
  return { runtime, backend, events, saves, plans, applications, dialogs, toasts, planGate, savedSelections, get closed() { return closed; } };
}

type Runtime = {
  previewProjectBatch: () => Promise<void>;
  applyReviewedProjectBatch: () => Promise<void>;
  selectionAutosave: ProjectSelectionAutosave;
  save: (id: string, value: ProjectSelection) => void;
  accept: (next: unknown) => void;
  review: () => { ids: string[]; plan: TestPlan; revision: number } | null;
  selection: () => string[];
  clearSelection: () => void;
};

test("project batch flushes each project's own configuration before preview and normal application", async () => {
  const h = harness();
  h.runtime.save("first", selection("gamma", "preset-first", "source-first"));
  h.runtime.save("second", selection("alpha", "preset-second", "source-second"));
  const preview = h.runtime.previewProjectBatch();
  await nextTurn();
  assert.deepEqual(h.events, ["flush:project:first", "save:first"]);
  assert.equal(h.plans.length, 0);
  h.saves[0].gate.resolve(); await nextTurn();
  assert.deepEqual(h.events, ["flush:project:first", "save:first", "saved:first", "flush:project:second", "save:second"]);
  assert.equal(h.plans.length, 0, "preview must wait for the second project's saved selection");
  h.saves[1].gate.resolve(); await preview;
  assert.deepEqual(h.plans[0].ids, ["first", "second"]);
  const expected = [selection("gamma", "preset-first", "source-first"), selection("alpha", "preset-second", "source-second"), initialSelections[2]];
  assert.deepEqual(h.plans[0].selections, expected);
  assert.deepEqual(plain(h.runtime.review()?.ids), ["first", "second"]);
  assert.match(h.dialogs[0].body, /\/projects\/first/);
  assert.match(h.dialogs[0].body, /\/projects\/second/);
  assert.doesNotMatch(h.dialogs[0].body, /\/projects\/other/);
  await h.runtime.applyReviewedProjectBatch();
  assert.deepEqual(h.applications.map(({ kind, ids, selections }) => ({ kind, ids, selections })), [{ kind: "normal", ids: ["first", "second"], selections: expected }]);
  assert.deepEqual(h.savedSelections(), expected, "batch application must not spread one project's configuration to another");
  assert.equal(h.backend.projects[2].pending, true, "unselected projects stay untouched");
  assert.equal(h.runtime.review(), null);
  assert.equal(h.closed, 1);
});

test("failed project selection save prevents any batch preview or application", async () => {
  const h = harness();
  h.runtime.save("first", selection("gamma", "preset-first", "source-first"));
  const preview = h.runtime.previewProjectBatch();
  await nextTurn(); h.saves[0].gate.reject(new Error("selection disk unavailable")); await preview;
  await h.runtime.applyReviewedProjectBatch();
  assert.equal(h.plans.length, 0);
  assert.equal(h.applications.length, 0);
  assert.equal(h.dialogs.length, 0);
  assert.equal(h.runtime.review(), null);
  assert.deepEqual(plain(h.runtime.selectionAutosave.state("project:first").draft), selection("gamma", "preset-first", "source-first"));
  assert.ok(h.toasts.some((toast) => toast.error && /选择未能自动保存/.test(toast.message)));
});

test("leaving project selection during autosave cancels the pending batch preview", async () => {
  const h = harness();
  h.runtime.save("first", selection("gamma", "preset-first", "source-first"));
  const preview = h.runtime.previewProjectBatch();
  await nextTurn();
  assert.equal(h.saves.length, 1);
  h.runtime.clearSelection();
  h.saves[0].gate.resolve();
  await preview;
  await h.runtime.applyReviewedProjectBatch();
  assert.equal(h.plans.length, 0, "an abandoned batch must not start planning after saving");
  assert.equal(h.dialogs.length, 0);
  assert.equal(h.runtime.review(), null);
  assert.equal(h.applications.length, 0);
  assert.deepEqual(h.savedSelections()[0], selection("gamma", "preset-first", "source-first"), "leaving batch selection must not cancel configuration autosave");
});

test("workspace changes while planning discard the batch review", async () => {
  const h = harness({ deferPlan: true });
  const preview = h.runtime.previewProjectBatch();
  await nextTurn(); assert.equal(h.plans.length, 1);
  h.runtime.accept(plain(h.backend));
  h.planGate!.resolve(); await preview;
  await h.runtime.applyReviewedProjectBatch();
  assert.equal(h.dialogs.length, 0);
  assert.equal(h.runtime.review(), null);
  assert.equal(h.applications.length, 0);
  assert.ok(h.toasts.some((toast) => toast.error && /工作空间已变化/.test(toast.message)));
});

test("an accepted batch review cannot write after project state becomes stale", async () => {
  const h = harness();
  await h.runtime.previewProjectBatch();
  assert.ok(h.runtime.review());
  h.backend.projects.splice(0, 1);
  h.runtime.accept(plain(h.backend));
  await h.runtime.applyReviewedProjectBatch();
  assert.equal(h.applications.length, 0);
  assert.deepEqual(plain(h.runtime.selection()), ["second"], "deleted selected projects are reconciled before applying");
  assert.ok(h.toasts.some((toast) => toast.error && /工作空间已变化/.test(toast.message)));
});

test("overwritable batches pass the exact reviewed plan and selected IDs to the force API", async () => {
  const plan = { ...readyPlan(), conflicts: ["local modification"], overwrite: ["/projects/second/.agents/skills/beta"] };
  const h = harness({ plan });
  await h.runtime.previewProjectBatch();
  const review = h.runtime.review()!;
  assert.match(h.dialogs[0].footer, /确认强制覆盖/);
  await h.runtime.applyReviewedProjectBatch();
  assert.equal(h.applications.length, 1);
  assert.equal(h.applications[0].kind, "force");
  assert.equal(h.applications[0].reviewed, review.plan, "force must receive the exact plan the user reviewed");
  assert.deepEqual(h.applications[0].ids, ["first", "second"]);
  assert.deepEqual(h.applications[0].selections, initialSelections);
});

test("failed application retains its reviewed batch and permits retry", async () => {
  const h = harness({ failApplyOnce: true });
  await h.runtime.previewProjectBatch();
  const review = h.runtime.review();
  await h.runtime.applyReviewedProjectBatch();
  assert.equal(h.runtime.review(), review);
  assert.equal(h.closed, 0, "a failed apply must leave the review available");
  assert.equal(h.backend.projects[0].pending, true);
  assert.ok(h.toasts.some((toast) => toast.error && /deployment disk unavailable/.test(toast.message)));
  await h.runtime.applyReviewedProjectBatch();
  assert.equal(h.applications.length, 2);
  assert.deepEqual(h.applications[0].ids, h.applications[1].ids);
  assert.equal(h.runtime.review(), null);
  assert.equal(h.closed, 1);
});
