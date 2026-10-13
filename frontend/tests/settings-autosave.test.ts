import assert from "node:assert/strict";
import test from "node:test";
import type { AppSettings } from "../src/api.ts";
import { createSettingsAutosave } from "../src/settings-autosave.ts";

interface Result { id: number; settings: AppSettings }

function initialSettings(): AppSettings {
  return { deploymentMode: "copy", defaultProjectDirectories: ["agents"], defaultGlobalDirectories: ["codex-user"], customProjectDirectories: [] };
}

function copy(settings: AppSettings): AppSettings {
  return { ...settings, defaultProjectDirectories: [...settings.defaultProjectDirectories], defaultGlobalDirectories: [...settings.defaultGlobalDirectories], customProjectDirectories: (settings.customProjectDirectories ?? []).map((directory) => ({ ...directory })) };
}

function deferred<T>() {
  let resolve!: (result: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => { resolve = resolvePromise; reject = rejectPromise; });
  return { promise, resolve, reject };
}

const nextTurn = () => new Promise<void>((resolve) => setImmediate(resolve));

function harness(initial = initialSettings()) {
  let current = initial;
  let active = 0;
  let maxActive = 0;
  const writes: AppSettings[] = [];
  const patches: Partial<AppSettings>[] = [];
  const requests: ReturnType<typeof deferred<Result>>[] = [];
  const starts: boolean[] = [];
  const errors: unknown[] = [];
  const settlements: { result: Result | undefined; busy: boolean }[] = [];
  const autosave = createSettingsAutosave<Result>({
    read: () => current,
    write: async (settings, patch) => {
      writes.push(settings);
      patches.push(patch);
      const request = deferred<Result>();
      requests.push(request);
      active += 1;
      maxActive = Math.max(maxActive, active);
      try { return await request.promise; } finally { active -= 1; }
    },
    settings: (result) => result.settings,
    start: () => { starts.push(autosave.busy); },
    error: (error) => { errors.push(error); },
    settled: (result) => {
      settlements.push({ result, busy: autosave.busy });
      if (result) current = result.settings;
    },
  });
  return {
    autosave, writes, patches, requests, starts, errors, settlements,
    get maxActive() { return maxActive; },
    set current(settings: AppSettings) { current = settings; },
  };
}

test("saves are serialized and patches merge into the latest successful response", async () => {
  const h = harness();
  h.autosave.save({ deploymentMode: "symlink" });
  h.autosave.save({ defaultProjectDirectories: ["claude"] });
  h.autosave.save({ defaultGlobalDirectories: ["cursor-user"] });
  assert.equal(h.autosave.busy, true);
  assert.deepEqual(h.starts, [true]);
  assert.equal(h.writes.length, 1);
  assert.deepEqual(h.writes[0], { ...initialSettings(), deploymentMode: "symlink" });

  // The next patch must retain values normalized by the backend response.
  const normalized: Result = { id: 1, settings: { ...copy(h.writes[0]), defaultGlobalDirectories: ["codex-legacy"] } };
  h.requests[0].resolve(normalized);
  await nextTurn();
  assert.equal(h.writes.length, 2);
  assert.deepEqual(h.writes[1], { ...normalized.settings, defaultProjectDirectories: ["claude"] });
  assert.equal(h.settlements.length, 0);
  h.requests[1].resolve({ id: 2, settings: copy(h.writes[1]) });
  await nextTurn();
  assert.deepEqual(h.writes[2], { ...initialSettings(), deploymentMode: "symlink", defaultProjectDirectories: ["claude"], defaultGlobalDirectories: ["cursor-user"] });
  assert.equal(h.autosave.busy, true);
  assert.equal(h.settlements.length, 0);

  const latest: Result = { id: 3, settings: copy(h.writes[2]) };
  const idle = h.autosave.idle();
  h.requests[2].resolve(latest);
  await idle;
  assert.equal(h.maxActive, 1);
  assert.equal(h.autosave.busy, false);
  assert.deepEqual(h.errors, []);
  assert.deepEqual(h.settlements, [{ result: latest, busy: false }]);
});

test("a failed patch rolls back its fields while later patches continue", async () => {
  const h = harness();
  h.autosave.save({ deploymentMode: "symlink" });
  h.autosave.save({ defaultProjectDirectories: ["claude"] });
  h.autosave.save({ defaultGlobalDirectories: ["cursor-user"] });
  h.requests[0].resolve({ id: 1, settings: copy(h.writes[0]) });
  await nextTurn();
  const failure = new Error("settings could not be written");
  h.requests[1].reject(failure);
  await nextTurn();
  assert.equal(h.writes.length, 3);
  assert.deepEqual(h.errors, [failure]);
  assert.deepEqual(h.writes[2], { ...initialSettings(), deploymentMode: "symlink", defaultGlobalDirectories: ["cursor-user"] });
  assert.equal(h.autosave.busy, true);
  assert.equal(h.settlements.length, 0);
  const latest: Result = { id: 3, settings: copy(h.writes[2]) };
  h.requests[2].resolve(latest);
  await h.autosave.idle();
  assert.equal(h.maxActive, 1);
  assert.deepEqual(h.settlements, [{ result: latest, busy: false }]);
});

test("an entirely failed batch settles once with undefined and another batch reads fresh settings", async () => {
  const h = harness();
  h.autosave.save({ deploymentMode: "symlink" });
  h.autosave.save({ defaultProjectDirectories: ["claude"] });
  const first = new Error("first failure");
  const second = new Error("second failure");
  h.requests[0].reject(first);
  await nextTurn();
  assert.deepEqual(h.writes[1], { ...initialSettings(), defaultProjectDirectories: ["claude"] });
  h.requests[1].reject(second);
  await h.autosave.idle();
  assert.deepEqual(h.errors, [first, second]);
  assert.deepEqual(h.settlements, [{ result: undefined, busy: false }]);

  h.current = { ...initialSettings(), defaultGlobalDirectories: ["claude-user"] };
  h.autosave.save({ deploymentMode: "symlink" });
  assert.deepEqual(h.starts, [true, true]);
  assert.deepEqual(h.writes[2], { ...initialSettings(), deploymentMode: "symlink", defaultGlobalDirectories: ["claude-user"] });
  h.requests[2].resolve({ id: 3, settings: copy(h.writes[2]) });
  await h.autosave.idle();
  assert.equal(h.settlements.length, 2);
});

test("queued patches and read arrays are snapshots protected from external mutation", async () => {
  const initial = initialSettings();
  const h = harness(initial);
  const patch: Partial<AppSettings> = { defaultProjectDirectories: ["claude"], defaultGlobalDirectories: ["cursor-user"] };
  h.autosave.save({ deploymentMode: "symlink" });
  h.autosave.save(patch);
  initial.defaultProjectDirectories.push("changed-read");
  initial.defaultGlobalDirectories[0] = "changed-read";
  patch.defaultProjectDirectories!.push("changed-patch");
  patch.defaultGlobalDirectories = ["replacement-array"];
  patch.deploymentMode = "copy";
  assert.deepEqual(h.writes[0], { ...initialSettings(), deploymentMode: "symlink" });

  const response: Result = { id: 1, settings: copy(h.writes[0]) };
  h.requests[0].resolve(response);
  await nextTurn();
  assert.deepEqual(h.writes[1], { ...initialSettings(), deploymentMode: "symlink", defaultProjectDirectories: ["claude"], defaultGlobalDirectories: ["cursor-user"] });
  response.settings.defaultProjectDirectories.push("changed-response");
  response.settings.defaultGlobalDirectories.push("changed-response");
  assert.deepEqual(h.writes[1], { ...initialSettings(), deploymentMode: "symlink", defaultProjectDirectories: ["claude"], defaultGlobalDirectories: ["cursor-user"] });
  h.requests[1].resolve({ id: 2, settings: copy(h.writes[1]) });
  await h.autosave.idle();
});

test("unrelated settings patches preserve project templates and backend normalization", async () => {
  const directory = { id: "project-team", name: "Team skills", path: "../shared/skills" };
  const h = harness({ ...initialSettings(), defaultProjectDirectories: [directory.id], customProjectDirectories: [directory] });
  h.autosave.save({ deploymentMode: "symlink" });
  h.autosave.save({ defaultGlobalDirectories: ["claude-user"] });
  assert.deepEqual(h.writes[0].customProjectDirectories, [directory]);
  const normalized = { ...directory, path: "../normalized/skills" };
  h.requests[0].resolve({ id: 1, settings: { ...copy(h.writes[0]), customProjectDirectories: [normalized] } });
  await nextTurn();
  assert.deepEqual(h.writes[1], { ...initialSettings(), deploymentMode: "symlink", defaultProjectDirectories: [directory.id], defaultGlobalDirectories: ["claude-user"], customProjectDirectories: [normalized] });
  h.requests[1].resolve({ id: 2, settings: copy(h.writes[1]) });
  await h.autosave.idle();
});

test("template objects are isolated from callers and failed template edits do not affect later saves", async () => {
  const saved = { id: "project-team", name: "Team skills", path: "../shared/skills" };
  const replacement = { id: "project-other", name: "Other skills", path: ".other/skills" };
  const initial = { ...initialSettings(), customProjectDirectories: [{ ...saved }] };
  const patch: Partial<AppSettings> = { customProjectDirectories: [{ ...replacement }] };
  const h = harness(initial);
  h.autosave.save({ deploymentMode: "symlink" });
  h.autosave.save(patch);
  h.autosave.save({ defaultGlobalDirectories: ["cursor-user"] });
  initial.customProjectDirectories[0].name = "changed-read";
  initial.customProjectDirectories.push({ ...replacement });
  patch.customProjectDirectories![0].path = "changed-patch";
  patch.customProjectDirectories!.push({ ...saved });
  assert.deepEqual(h.writes[0].customProjectDirectories, [saved]);

  const response: Result = { id: 1, settings: copy(h.writes[0]) };
  h.requests[0].resolve(response);
  await nextTurn();
  assert.deepEqual(h.writes[1].customProjectDirectories, [replacement]);
  response.settings.customProjectDirectories[0].path = "changed-response";
  h.writes[1].customProjectDirectories[0].name = "changed-write";
  const failure = new Error("template edit could not be written");
  h.requests[1].reject(failure);
  await nextTurn();
  assert.deepEqual(h.errors, [failure]);
  assert.deepEqual(h.writes[2], { ...initialSettings(), deploymentMode: "symlink", defaultGlobalDirectories: ["cursor-user"], customProjectDirectories: [saved] });
  h.requests[2].resolve({ id: 3, settings: copy(h.writes[2]) });
  await h.autosave.idle();
  assert.equal(h.maxActive, 1);
  assert.equal(h.settlements.at(-1)?.result?.id, 3);
});

test("legacy settings without a template field are normalized before writing", async () => {
  const legacy = initialSettings();
  delete (legacy as Partial<AppSettings>).customProjectDirectories;
  const h = harness(legacy);
  h.autosave.save({ deploymentMode: "symlink" });
  h.autosave.save({ defaultProjectDirectories: ["codex"] });
  assert.deepEqual(h.writes[0], { ...initialSettings(), deploymentMode: "symlink" });
  h.requests[0].resolve({ id: 1, settings: { ...legacy, deploymentMode: "symlink" } });
  await nextTurn();
  assert.deepEqual(h.writes[1], { ...initialSettings(), deploymentMode: "symlink", defaultProjectDirectories: ["codex"] });
  h.requests[1].resolve({ id: 2, settings: copy(h.writes[1]) });
  await h.autosave.idle();
});

test("saving does not mutate caller settings or patches, including after a write mutates its input", async () => {
  const initial = initialSettings();
  const initialBefore = copy(initial);
  const patch: Partial<AppSettings> = { defaultProjectDirectories: ["claude"] };
  const patchBefore = structuredClone(patch);
  const h = harness(initial);
  h.autosave.save(patch);
  h.autosave.save({ defaultGlobalDirectories: ["cursor-user"] });
  h.writes[0].defaultProjectDirectories.push("write-mutated");
  h.writes[0].defaultGlobalDirectories.push("write-mutated");
  h.requests[0].reject(new Error("failed after input mutation"));
  await nextTurn();
  assert.deepEqual(initial, initialBefore);
  assert.deepEqual(patch, patchBefore);
  assert.deepEqual(h.writes[1], { ...initialSettings(), defaultGlobalDirectories: ["cursor-user"] });
  h.requests[1].resolve({ id: 2, settings: copy(h.writes[1]) });
  await h.autosave.idle();
  assert.deepEqual(initial, initialBefore);
  assert.deepEqual(patch, patchBefore);
});

test("the queue accepts patches added by an error callback without starting or settling early", async () => {
  const writes: AppSettings[] = [];
  const failures: unknown[] = [];
  let starts = 0;
  const settlements: (Result | undefined)[] = [];
  const failure = new Error("retry with a different field");
  const autosave = createSettingsAutosave<Result>({
    read: initialSettings,
    write: async (settings) => {
      writes.push(settings);
      if (writes.length === 1) throw failure;
      return { id: writes.length, settings };
    },
    settings: (result) => result.settings,
    start: () => { starts += 1; },
    error: (error) => {
      failures.push(error);
      autosave.save({ defaultProjectDirectories: ["codex"] });
    },
    settled: (result) => { settlements.push(result); },
  });
  autosave.save({ deploymentMode: "symlink" });
  await autosave.idle();
  assert.equal(starts, 1);
  assert.deepEqual(failures, [failure]);
  assert.equal(writes.length, 2);
  assert.deepEqual(writes[1], { ...initialSettings(), defaultProjectDirectories: ["codex"] });
  assert.equal(settlements.length, 1);
  assert.equal(settlements[0]?.id, 2);
});

test("settled receives the last successful response even when the tail of the queue fails", async () => {
  const h = harness();
  h.autosave.save({ deploymentMode: "symlink" });
  h.autosave.save({ defaultProjectDirectories: ["claude"] });
  h.autosave.save({ defaultGlobalDirectories: ["cursor-user"] });
  const successful: Result = { id: 1, settings: copy(h.writes[0]) };
  h.requests[0].resolve(successful);
  await nextTurn();
  h.requests[1].reject(new Error("middle failure"));
  await nextTurn();
  assert.equal(h.settlements.length, 0);
  h.requests[2].reject(new Error("tail failure"));
  await h.autosave.idle();
  assert.equal(h.errors.length, 2);
  assert.deepEqual(h.settlements, [{ result: successful, busy: false }]);
});

test("shared appearance preferences survive unrelated settings saves and serialize as patches", async () => {
  const h = harness({ ...initialSettings(), theme: "dark", locale: "zh-TW", navLayout: "top" });
  h.autosave.save({ deploymentMode: "symlink" });
  h.autosave.save({ theme: "light", locale: "en", navLayout: "side" });
  assert.deepEqual(h.patches[0], { deploymentMode: "symlink" });
  assert.deepEqual(h.writes[0], { ...initialSettings(), deploymentMode: "symlink", theme: "dark", locale: "zh-TW", navLayout: "top" });
  h.requests[0].resolve({ id: 1, settings: copy(h.writes[0]) });
  await nextTurn();
  assert.deepEqual(h.patches[1], { theme: "light", locale: "en", navLayout: "side" });
  assert.deepEqual(h.writes[1], { ...initialSettings(), deploymentMode: "symlink", theme: "light", locale: "en", navLayout: "side" });
  h.requests[1].resolve({ id: 2, settings: copy(h.writes[1]) });
  await h.autosave.idle();
});

test("a queued settings write sends only its explicit patch after cloud changed unrelated preferences", async () => {
  let server = { ...initialSettings(), theme: "light" as const, locale: "en" as const, navLayout: "auto" as const } as AppSettings;
  const patches: Partial<AppSettings>[] = [];
  const autosave = createSettingsAutosave<AppSettings>({
    read: () => ({ ...initialSettings(), theme: "dark", locale: "zh-CN", navLayout: "side" }),
    write: async (_staleSettings, patch) => {
      patches.push(patch);
      // A remote commit arrived while this form was still based on old settings.
      server = { ...server, ...patch };
      return server;
    },
    settings: (result) => result,
    start() {}, error(error) { throw error; }, settled() {},
  });
  autosave.save({ deploymentMode: "symlink" });
  await autosave.idle();
  assert.deepEqual(patches, [{ deploymentMode: "symlink" }]);
  assert.deepEqual(server, { ...initialSettings(), deploymentMode: "symlink", theme: "light", locale: "en", navLayout: "auto" });
});

test("queued font and text size edits keep both preferences in later saves", async () => {
  const h = harness();
  h.autosave.save({ textSize: 125 });
  h.autosave.save({ interfaceFont: "serif" });
  assert.deepEqual(h.patches[0], { textSize: 125 });
  h.requests[0].resolve({ id: 1, settings: copy(h.writes[0]) });
  await nextTurn();
  assert.equal(h.writes[1].textSize, 125);
  assert.equal(h.writes[1].interfaceFont, "serif");
  assert.deepEqual(h.patches[1], { interfaceFont: "serif" });
  h.requests[1].resolve({ id: 2, settings: copy(h.writes[1]) });
  await h.autosave.idle();
  assert.equal(h.settlements[0].result?.settings.textSize, 125);
  assert.equal(h.settlements[0].result?.settings.interfaceFont, "serif");
});
