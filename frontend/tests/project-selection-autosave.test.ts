import assert from "node:assert/strict";
import test from "node:test";
import { createProjectSelectionAutosave, type ProjectSelection, type ProjectSelectionSaveState } from "../src/project-selection-autosave.ts";

function deferred<T>() {
  let resolve!: (result: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => { resolve = resolvePromise; reject = rejectPromise; });
  return { promise, resolve, reject };
}

const nextTurn = () => new Promise<void>((resolve) => setImmediate(resolve));
const selection = (...skillIds: string[]): ProjectSelection => ({ skillIds, presetIds: [] });

function harness() {
  const writes: { key: string; selection: ProjectSelection }[] = [];
  const requests: ReturnType<typeof deferred<number>>[] = [];
  const saved: { key: string; result: number; selection: ProjectSelection }[] = [];
  const changes: { key: string; state: ProjectSelectionSaveState }[] = [];
  let active = 0;
  let maxActive = 0;
  const autosave = createProjectSelectionAutosave<number>({
    delayMs: 0,
    write: async (key, selection) => {
      writes.push({ key, selection });
      const request = deferred<number>();
      requests.push(request);
      active += 1;
      maxActive = Math.max(maxActive, active);
      try { return await request.promise; } finally { active -= 1; }
    },
    saved: (key, result, selection) => { saved.push({ key, result, selection }); },
    changed: (key, state) => { changes.push({ key, state }); },
  });
  return { autosave, writes, requests, saved, changes, get maxActive() { return maxActive; } };
}

test("same-turn changes coalesce into one write and notify pending synchronously", async () => {
  const h = harness();
  h.autosave.save("project:one", selection("a"));
  h.autosave.save("project:one", { skillIds: ["b"], presetIds: ["preset"] });
  assert.equal(h.autosave.busy, true);
  assert.equal(h.autosave.state("project:one").pending, true);
  assert.equal(h.changes[0].state.pending, true);
  assert.equal(h.writes.length, 0);
  const flushed = h.autosave.flush("project:one");
  await nextTurn();
  assert.deepEqual(h.writes, [{ key: "project:one", selection: { skillIds: ["b"], presetIds: ["preset"] } }]);
  h.requests[0].resolve(1);
  await flushed;
  assert.deepEqual(h.saved, [{ ...h.writes[0], result: 1 }]);
  assert.deepEqual(h.autosave.state("project:one"), { pending: false, error: undefined, draft: undefined });
  assert.equal(h.autosave.busy, false);
});

test("projects and global targets share a serial queue and each key keeps its newest draft", async () => {
  const h = harness();
  h.autosave.save("project:one", selection("a"));
  await nextTurn();
  h.autosave.save("global:codex", selection("global-old"));
  h.autosave.save("project:one", selection("b"));
  h.autosave.save("project:one", selection("c"));
  h.autosave.save("global:codex", selection("global-new"));
  h.requests[0].resolve(1);
  await nextTurn();
  assert.equal(h.saved.length, 0, "obsolete project response is never published");
  assert.deepEqual(h.writes[1], { key: "project:one", selection: selection("c") });
  assert.deepEqual(h.autosave.state("project:one").draft, selection("c"));
  h.requests[1].resolve(2);
  await nextTurn();
  assert.deepEqual(h.writes[2], { key: "global:codex", selection: selection("global-new") });
  assert.deepEqual(h.saved.map((saved) => saved.key), ["project:one"]);
  h.requests[2].resolve(3);
  await h.autosave.idle();
  assert.equal(h.maxActive, 1);
  assert.deepEqual(h.saved.map((saved) => saved.result), [2, 3]);
});

test("flush follows new selections until its key is saved while idle waits for every key", async () => {
  const h = harness();
  let flushed = false;
  let idle = false;
  h.autosave.save("project:one", selection("a"));
  const flushPromise = h.autosave.flush("project:one").then(() => { flushed = true; });
  const idlePromise = h.autosave.idle().then(() => { idle = true; });
  await nextTurn();
  h.autosave.save("project:one", selection("b"));
  h.requests[0].resolve(1);
  await nextTurn();
  assert.equal(flushed, false);
  h.autosave.save("project:two", selection("c"));
  h.requests[1].resolve(2);
  await flushPromise;
  assert.equal(flushed, true);
  assert.equal(idle, false);
  await nextTurn();
  h.requests[2].resolve(3);
  await idlePromise;
  assert.equal(idle, true);
});

test("a failed save preserves its latest draft, rejects flush and can retry", async () => {
  const h = harness();
  const draft = { skillIds: ["a"], presetIds: ["preset"] };
  h.autosave.save("project:one", draft);
  const failure = new Error("disk unavailable");
  const flushed = assert.rejects(h.autosave.flush("project:one"), (error) => error === failure);
  await nextTurn();
  h.requests[0].reject(failure);
  await flushed;
  await h.autosave.idle();
  assert.equal(h.autosave.busy, false);
  assert.deepEqual(h.autosave.state("project:one"), { pending: false, error: failure, draft });
  await assert.rejects(h.autosave.flush("project:one"), (error) => error === failure);
  assert.equal(h.saved.length, 0);

  h.autosave.retry("project:one");
  assert.equal(h.autosave.state("project:one").error, undefined);
  assert.equal(h.autosave.state("project:one").pending, true);
  await nextTurn();
  assert.deepEqual(h.writes[1].selection, draft);
  h.requests[1].resolve(2);
  await h.autosave.flush("project:one");
  assert.deepEqual(h.saved.map((saved) => saved.result), [2]);
  assert.equal(h.autosave.state("project:one").draft, undefined);
});

test("an older failed request cannot block a newer selection or report its stale error", async () => {
  const h = harness();
  h.autosave.save("project:one", selection("a"));
  await nextTurn();
  h.autosave.save("project:one", selection("b"));
  const flushed = h.autosave.flush("project:one");
  h.requests[0].reject(new Error("old failure"));
  await nextTurn();
  assert.equal(h.autosave.state("project:one").error, undefined);
  assert.deepEqual(h.writes[1].selection, selection("b"));
  h.requests[1].resolve(2);
  await flushed;
  assert.deepEqual(h.saved.map((saved) => saved.result), [2]);
});

test("one failed key does not block other keys and idle resolves with the failed draft retained", async () => {
  const h = harness();
  h.autosave.save("project:one", selection("a"));
  h.autosave.save("project:two", selection("b"));
  await nextTurn();
  h.requests[0].reject(new Error("first project failed"));
  await nextTurn();
  assert.deepEqual(h.writes[1], { key: "project:two", selection: selection("b") });
  h.requests[1].resolve(2);
  await h.autosave.idle();
  assert.equal(h.autosave.state("project:one").pending, false);
  assert.deepEqual(h.autosave.state("project:one").draft, selection("a"));
  assert.deepEqual(h.saved.map((saved) => saved.key), ["project:two"]);
  await assert.rejects(h.autosave.flush("project:one"), /first project failed/);
});

test("caller, write and status mutations cannot change queued or retry selections", async () => {
  const h = harness();
  const draft = { skillIds: ["a"], presetIds: ["preset"] };
  h.autosave.save("project:one", draft);
  draft.skillIds.push("external");
  draft.presetIds[0] = "external";
  h.autosave.state("project:one").draft!.skillIds.push("state mutation");
  h.changes[0].state.draft!.presetIds.push("notification mutation");
  await nextTurn();
  assert.deepEqual(h.writes[0].selection, { skillIds: ["a"], presetIds: ["preset"] });
  h.writes[0].selection.skillIds.push("write mutation");
  h.requests[0].reject(new Error("retry"));
  await h.autosave.idle();
  assert.deepEqual(h.autosave.state("project:one").draft, { skillIds: ["a"], presetIds: ["preset"] });
  h.autosave.retry("project:one");
  await nextTurn();
  assert.deepEqual(h.writes[1].selection, { skillIds: ["a"], presetIds: ["preset"] });
  h.requests[1].resolve(2);
  await h.autosave.idle();
  assert.deepEqual(draft, { skillIds: ["a", "external"], presetIds: ["external"] });
});

test("flush includes a replacement queued by the saved callback", async () => {
  const writes: ProjectSelection[] = [];
  const requests: ReturnType<typeof deferred<number>>[] = [];
  const autosave = createProjectSelectionAutosave<number>({
    delayMs: 0,
    write: (_key, draft) => { writes.push(draft); const request = deferred<number>(); requests.push(request); return request.promise; },
    saved: (_key, result) => { if (result === 1) autosave.save("project:one", selection("replacement")); },
    changed: () => {},
  });
  autosave.save("project:one", selection("a"));
  let finished = false;
  const flushed = autosave.flush("project:one").then(() => { finished = true; });
  await nextTurn();
  requests[0].resolve(1);
  await nextTurn();
  assert.equal(finished, false);
  assert.deepEqual(writes[1], selection("replacement"));
  requests[1].resolve(2);
  await flushed;
});

test("new changes retry a failed key and clearing forgotten targets resets draft and error", async () => {
  const h = harness();
  await h.autosave.flush("unknown");
  h.autosave.retry("unknown");
  h.autosave.forget("unknown");
  h.autosave.save("project:one", selection("a"));
  assert.throws(() => h.autosave.forget("project:one"), /Wait for selection saves/);
  assert.throws(() => h.autosave.clear(), /Wait for selection saves/);
  await nextTurn();
  h.requests[0].reject(new Error("first failure"));
  await h.autosave.idle();
  h.autosave.save("project:one", selection("b"));
  assert.equal(h.autosave.state("project:one").error, undefined);
  await nextTurn();
  h.requests[1].reject(new Error("second failure"));
  await h.autosave.idle();
  h.autosave.forget("project:one");
  assert.deepEqual(h.autosave.state("project:one"), { pending: false, error: undefined, draft: undefined });
  await h.autosave.flush("project:one");
  h.autosave.save("global:one", selection("c"));
  await nextTurn();
  h.requests[2].reject(new Error("third failure"));
  await h.autosave.idle();
  h.autosave.clear();
  assert.deepEqual(h.autosave.state("global:one"), { pending: false, error: undefined, draft: undefined });
  assert.equal(h.changes.at(-1)?.state.error, undefined);
});

test("debounce holds rapid selections and flush skips delays for preceding queued keys", async () => {
  const writes: { key: string; selection: ProjectSelection }[] = [];
  const autosave = createProjectSelectionAutosave<number>({
    delayMs: 10_000,
    write: async (key, selection) => { writes.push({ key, selection }); return writes.length; },
    saved: () => {},
    changed: () => {},
  });
  autosave.save("project:one", selection("a"));
  autosave.save("global:one", selection("b"));
  await nextTurn();
  assert.equal(writes.length, 0);
  assert.equal(autosave.busy, true);
  autosave.save("project:one", selection("latest"));
  await autosave.flush("project:one");
  assert.deepEqual(writes, [{ key: "global:one", selection: selection("b") }, { key: "project:one", selection: selection("latest") }]);
  await autosave.idle();
  assert.equal(autosave.busy, false, "flush cancels the debounce timer");
});

test("debounced changes save automatically without needing flush", async () => {
  const writes: ProjectSelection[] = [];
  const autosave = createProjectSelectionAutosave<number>({
    delayMs: 1,
    write: async (_key, selection) => { writes.push(selection); return writes.length; },
    saved: () => {},
    changed: () => {},
  });
  autosave.save("project:one", selection("a"));
  autosave.save("project:one", selection("b"));
  await autosave.idle();
  assert.deepEqual(writes, [selection("b")]);
});

test("identical pending selections are deduplicated regardless of order", async () => {
  const h = harness();
  h.autosave.save("project:one", selection("a", "b"));
  h.autosave.save("project:one", selection("b", "a"));
  await nextTurn();
  h.autosave.save("project:one", selection("a", "b"));
  h.requests[0].resolve(1);
  await h.autosave.idle();
  assert.equal(h.writes.length, 1);
  assert.equal(h.saved.length, 1, "a duplicate cannot make the in-flight response obsolete");
});

test("completed selections are written again because another key may have changed overlapping targets", async () => {
  const h = harness();
  h.autosave.save("global:A", selection("x"));
  await nextTurn();
  h.requests[0].resolve(1);
  await h.autosave.idle();
  h.autosave.save("global:A,B", selection("y"));
  await nextTurn();
  h.requests[1].resolve(2);
  await h.autosave.idle();
  h.autosave.save("global:A", selection("x"));
  await nextTurn();
  assert.deepEqual(h.writes[2], { key: "global:A", selection: selection("x") });
  h.requests[2].resolve(3);
  await h.autosave.idle();
  assert.equal(h.saved.length, 3);
});

test("coalescing preserves the last user action across overlapping global batches", async () => {
  const h = harness();
  h.autosave.save("global:A", selection("x"));
  h.autosave.save("global:A,B", selection("y"));
  h.autosave.save("global:A", selection("z"));
  await nextTurn();
  assert.deepEqual(h.writes[0], { key: "global:A,B", selection: selection("y") });
  h.requests[0].resolve(1);
  await nextTurn();
  assert.deepEqual(h.writes[1], { key: "global:A", selection: selection("z") });
  h.requests[1].resolve(2);
  await h.autosave.idle();
  assert.equal(h.maxActive, 1);
});

test("a repeated in-flight selection is queued again after an intervening overlapping key", async () => {
  const h = harness();
  h.autosave.save("global:A", selection("x"));
  await nextTurn();
  h.autosave.save("global:A,B", selection("y"));
  h.autosave.save("global:A", selection("x"));
  h.requests[0].resolve(1);
  await nextTurn();
  assert.equal(h.saved.length, 0, "the first A response precedes the newest A intent");
  assert.deepEqual(h.writes[1], { key: "global:A,B", selection: selection("y") });
  h.requests[1].resolve(2);
  await nextTurn();
  assert.deepEqual(h.writes[2], { key: "global:A", selection: selection("x") });
  h.requests[2].resolve(3);
  await h.autosave.idle();
  assert.deepEqual(h.saved.map((saved) => saved.result), [2, 3]);
});

test("an identical queued choice moves behind a newer overlapping key", async () => {
  const h = harness();
  h.autosave.save("global:A", selection("x"));
  h.autosave.save("global:A,B", selection("y"));
  h.autosave.save("global:A", selection("x"));
  await nextTurn();
  assert.equal(h.writes[0].key, "global:A,B");
  h.requests[0].resolve(1);
  await nextTurn();
  assert.equal(h.writes[1].key, "global:A");
  h.requests[1].resolve(2);
  await h.autosave.idle();
});

test("source-only changes coalesce and snapshot source arrays before saving", async () => {
  const h = harness();
  const sourceIds = ["first"];
  h.autosave.save("project:one", { skillIds: [], presetIds: [], sourceIds });
  sourceIds.push("outside-mutation");
  assert.deepEqual(h.autosave.state("project:one").draft?.sourceIds, ["first"]);
  h.autosave.save("project:one", { skillIds: [], presetIds: [], sourceIds: ["second"] });
  const saved = h.autosave.flush("project:one");
  await nextTurn();
  assert.deepEqual(h.writes[0].selection.sourceIds, ["second"]);
  h.requests[0].resolve(1);
  await saved;
  h.autosave.save("project:one", { skillIds: [], presetIds: [], sourceIds: [] });
  const cleared = h.autosave.flush("project:one");
  await nextTurn();
  assert.deepEqual(h.writes[1].selection.sourceIds, []);
  h.requests[1].resolve(2);
  await cleared;
});

test("source selections survive a save failure and retry", async () => {
  const h = harness();
  const draft = { skillIds: ["direct"], presetIds: ["preset"], sourceIds: ["source"] };
  h.autosave.save("global:A", draft);
  const failed = assert.rejects(h.autosave.flush("global:A"), /disk unavailable/);
  await nextTurn();
  h.requests[0].reject(new Error("disk unavailable"));
  await failed;
  assert.deepEqual(h.autosave.state("global:A").draft, draft);
  h.autosave.retry("global:A");
  const retried = h.autosave.flush("global:A");
  await nextTurn();
  assert.deepEqual(h.writes[1].selection, draft);
  h.requests[1].resolve(1);
  await retried;
  assert.deepEqual(h.saved[0].selection, draft);
});
