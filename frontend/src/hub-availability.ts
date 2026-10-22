import type { HubAvailability } from "./api";
import { gitBackedHubSkill } from "./hub-recovery.ts";

export type HubCardAvailability = Omit<HubAvailability, "state"> & { state: HubAvailability["state"] | "checking" };
type CheckRequest = Promise<HubAvailability[]> & { cancel?: () => Promise<void> | void };

const pendingState = (url: string, state: "checking" | "unknown"): HubCardAvailability => ({ url, state, revision: "", checkedAt: "", reason: "" });

// One request per repository, in display order. This queue is independent of
// foreground discovery tasks so users can keep typing and preview immediately.
export function createHubAvailability(options: {
  check: (urls: string[]) => CheckRequest;
  changed: (urls: readonly string[]) => void;
}) {
  let generation = 0;
  let request: CheckRequest | null = null;
  const values = new Map<string, HubCardAvailability>();
  const completed = new Set<string>();

  function cancel() {
    generation++;
    const canceled = request;
    request = null;
    if (canceled?.cancel) {
      try { void Promise.resolve(canceled.cancel()).catch(() => {}); } catch { /* A completed request may no longer be cancellable. */ }
    }
    const changed: string[] = [];
    for (const [url, value] of values) {
      if (value.state !== "checking") continue;
      values.set(url, pendingState(url, "unknown"));
      changed.push(url);
    }
    if (changed.length) options.changed(changed);
  }

  async function run(inputs: readonly string[], preserve = false) {
    cancel();
    const current = generation;
    if (!preserve) { values.clear(); completed.clear(); }
    const groups = new Map<string, Set<string>>();
    for (const url of inputs) {
      const spec = gitBackedHubSkill(url);
      if (!spec || completed.has(url)) continue;
      const group = groups.get(spec.repository) || new Set<string>();
      group.add(url); groups.set(spec.repository, group);
      values.set(url, pendingState(url, "checking"));
    }
    const pending = [...groups.values()].flatMap((urls) => [...urls]);
    if (pending.length) options.changed(pending);
    const batches = [...groups.values()].flatMap((urls) => {
      const group = [...urls];
      return Array.from({ length: Math.ceil(group.length / 200) }, (_, index) => group.slice(index * 200, (index + 1) * 200));
    });
    for (const batch of batches) {
      if (current !== generation) return;
      let result: HubAvailability[] = [];
      try {
        request = options.check(batch);
        result = await request;
      } catch { /* A transport failure leaves availability unknown, never missing. */ }
      if (current !== generation) return;
      request = null;
      const returned = new Map(result.filter((value) => batch.includes(value.url) && ["available", "missing", "unavailable", "unknown"].includes(value.state)).map((value) => [value.url, value]));
      for (const url of batch) {
        values.set(url, returned.get(url) || pendingState(url, "unknown"));
        completed.add(url);
      }
      options.changed(batch);
    }
  }

  function record(value: HubAvailability) {
    if (!gitBackedHubSkill(value.url)) return;
    values.set(value.url, value);
    completed.add(value.url);
    options.changed([value.url]);
  }

  return { run, cancel, record, invalidate: (url: string) => { completed.delete(url); }, get: (url: string) => values.get(url) };
}
