import assert from "node:assert/strict";
import test from "node:test";
import type { HubAvailability } from "../src/api.ts";
import { createHubAvailability } from "../src/hub-availability.ts";

const url = (repository: string, skill: string) => `https://skills.sh/${repository}/${skill}`;
const result = (url: string, state: HubAvailability["state"]): HubAvailability => ({ url, state, revision: "current", checkedAt: "2026-10-23T00:00:00Z", reason: "" });
const nextTurn = () => new Promise<void>((resolve) => setImmediate(resolve));

function harness() {
  const calls: { inputs: string[]; cancellations: number; resolve: (value: HubAvailability[]) => void; reject: (error: Error) => void }[] = [];
  const updates: string[][] = [];
  const availability = createHubAvailability({
    check: (inputs) => {
      const call = { inputs, cancellations: 0 } as typeof calls[number];
      const promise = new Promise<HubAvailability[]>((resolve, reject) => { call.resolve = resolve; call.reject = reject; });
      calls.push(call);
      return Object.assign(promise, { cancel: () => { call.cancellations++; } });
    },
    changed: (urls) => updates.push([...urls]),
  });
  return { ...availability, calls, updates };
}

test("a 200-card repository is checked once, deduplicated, before the next repository starts", async () => {
  const h = harness();
  const inputs = Array.from({ length: 200 }, (_, index) => url("team/design", `skill-${index}`));
  const second = url("other/repo", "review");
  const work = h.run([inputs[0], second, ...inputs, "https://skills.sh/site/open.feishu.cn/doc", "https://skills.sh:443/team/design/polish"]);
  assert.equal(h.calls.length, 1);
  assert.deepEqual(h.calls[0].inputs, inputs);
  assert.equal(h.get(second)?.state, "checking");
  h.calls[0].resolve(inputs.map((input) => result(input, "available")));
  await nextTurn();
  assert.equal(h.calls.length, 2);
  assert.deepEqual(h.calls[1].inputs, [second]);
  h.calls[1].resolve([result(second, "missing")]);
  await work;
  assert.equal(h.get(inputs[0])?.state, "available");
  assert.equal(h.get(second)?.state, "missing");
});

test("append retains completed statuses and checks new URLs; a replacement rechecks snapshots", async () => {
  const h = harness();
  const first = url("team/design", "polish"), next = url("team/design", "impeccable");
  const initial = h.run([first]);
  h.calls[0].resolve([result(first, "missing")]); await initial;
  const append = h.run([first, next, next], true);
  assert.equal(h.get(first)?.state, "missing");
  assert.deepEqual(h.calls[1].inputs, [next]);
  h.calls[1].resolve([result(next, "available")]); await append;
  const replacement = h.run([first, next]);
  assert.deepEqual(h.calls[2].inputs, [first, next]);
  h.calls[2].resolve([result(first, "available"), result(next, "available")]); await replacement;
  assert.equal(h.get(first)?.state, "available", "a fresh search or returning to Discover can correct an old missing snapshot");
});

test("replacing a search cancels its request and rejects late results from that generation", async () => {
  const h = harness();
  const oldURL = url("old/repo", "polish"), currentURL = url("current/repo", "review");
  const old = h.run([oldURL]);
  const current = h.run([currentURL]);
  assert.equal(h.calls[0].cancellations, 1);
  h.calls[0].resolve([result(oldURL, "missing")]); await old;
  assert.equal(h.get(oldURL), undefined);
  assert.equal(h.get(currentURL)?.state, "checking");
  h.calls[1].resolve([result(currentURL, "available")]); await current;
  assert.equal(h.get(currentURL)?.state, "available");
});

test("cancel stops queued repositories and a foreground preview can record a newer result", async () => {
  const h = harness();
  const first = url("team/design", "polish"), second = url("other/repo", "review");
  const work = h.run([first, second]);
  h.cancel();
  h.record(result(first, "available"));
  assert.equal(h.calls[0].cancellations, 1);
  assert.equal(h.get(second)?.state, "unknown");
  h.calls[0].resolve([result(first, "missing")]); await work;
  assert.equal(h.calls.length, 1, "cancel does not start another queued repository");
  assert.equal(h.get(first)?.state, "available");
});

test("transport failures and incomplete responses stay unknown rather than becoming missing", async () => {
  const h = harness();
  const first = url("team/design", "polish"), second = url("other/repo", "review");
  const work = h.run([first, second]);
  h.calls[0].reject(new Error("connection interrupted")); await nextTurn();
  assert.equal(h.get(first)?.state, "unknown");
  h.calls[1].resolve([result("https://skills.sh/unrequested/repo/skill", "missing")]); await work;
  assert.equal(h.get(second)?.state, "unknown");
  assert.equal(h.get("https://skills.sh/unrequested/repo/skill"), undefined);
});

test("larger repositories use sequential batches of at most 200 URLs", async () => {
  const h = harness();
  const inputs = Array.from({ length: 405 }, (_, index) => url("team/design", `skill-${index}`));
  const work = h.run(inputs);
  for (let index = 0; index < 3; index++) {
    assert.equal(h.calls.length, index + 1);
    assert.equal(h.calls[index].inputs.length, index === 2 ? 5 : 200);
    h.calls[index].resolve(h.calls[index].inputs.map((input) => result(input, "available")));
    await nextTurn();
  }
  await work;
  assert.equal(h.get(inputs[404])?.state, "available");
});
