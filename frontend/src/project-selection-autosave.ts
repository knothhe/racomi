export interface ProjectSelection {
  skillIds: string[];
  presetIds: string[];
}

export interface ProjectSelectionSaveState {
  pending: boolean;
  error: unknown | undefined;
  draft: ProjectSelection | undefined;
}

export interface ProjectSelectionAutosaveOptions<T> {
  delayMs?: number;
  write: (key: string, selection: ProjectSelection) => Promise<T>;
  saved: (key: string, result: T, selection: ProjectSelection) => void;
  changed: (key: string, state: ProjectSelectionSaveState) => void;
}

export interface ProjectSelectionAutosave {
  save: (key: string, selection: ProjectSelection) => void;
  retry: (key: string) => void;
  state: (key: string) => ProjectSelectionSaveState;
  flush: (key: string) => Promise<void>;
  idle: () => Promise<void>;
  forget: (key: string) => void;
  clear: () => void;
  readonly busy: boolean;
}

interface Entry {
  key: string;
  revision: number;
  draft: ProjectSelection | undefined;
  error: unknown | undefined;
  failed: boolean;
  queued: boolean;
  readyAt: number;
  waiters: { resolve: () => void; reject: (error: unknown) => void }[];
}

function clone(selection: ProjectSelection): ProjectSelection {
  return { skillIds: [...selection.skillIds], presetIds: [...selection.presetIds] };
}

function same(left: ProjectSelection, right: ProjectSelection): boolean {
  return JSON.stringify([...left.skillIds].sort()) === JSON.stringify([...right.skillIds].sort())
    && JSON.stringify([...left.presetIds].sort()) === JSON.stringify([...right.presetIds].sort());
}

// Keys may identify projects or global targets. Writes share one queue because
// their responses can contain the entire workspace; parallel responses could
// otherwise replace newer state. Each key retains only its latest selection.
export function createProjectSelectionAutosave<T>(options: ProjectSelectionAutosaveOptions<T>): ProjectSelectionAutosave {
  const entries = new Map<string, Entry>();
  const queue: Entry[] = [];
  const idleResolvers: (() => void)[] = [];
  let active: Entry | undefined;
  let scheduled = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const delayMs = Math.max(0, options.delayMs ?? 180);

  function pending(entry: Entry): boolean { return entry.queued || active === entry; }
  function busy(): boolean { return scheduled || timer !== undefined || !!active || queue.length > 0; }
  function state(entry?: Entry): ProjectSelectionSaveState {
    return { pending: entry ? pending(entry) : false, error: entry?.error, draft: entry?.draft && clone(entry.draft) };
  }
  function notify(entry: Entry): void { options.changed(entry.key, state(entry)); }
  function settle(entry: Entry): void {
    if (pending(entry)) return;
    for (const waiter of entry.waiters.splice(0)) {
      if (entry.failed) waiter.reject(entry.error);
      else waiter.resolve();
    }
  }
  function settleIdle(): void {
    if (!busy()) for (const resolve of idleResolvers.splice(0)) resolve();
  }
  function schedule(): void {
    if (active || scheduled) return;
    if (timer !== undefined) { clearTimeout(timer); timer = undefined; }
    if (!queue.length) return;
    const delay = Math.max(0, queue[0].readyAt - Date.now());
    if (delay > 0) {
      timer = setTimeout(() => { timer = undefined; schedule(); }, delay);
      return;
    }
    scheduled = true;
    queueMicrotask(() => {
      scheduled = false;
      void drain();
    });
  }
  function enqueue(entry: Entry): void {
    // Replacing a queued selection is a newer user action. Move its key behind
    // other keys, which may update overlapping global directories.
    if (entry.queued) queue.splice(queue.indexOf(entry), 1);
    entry.queued = true;
    queue.push(entry);
    if (entry.waiters.length) for (const queued of queue) queued.readyAt = 0;
    schedule();
    notify(entry);
  }
  async function drain(): Promise<void> {
    const entry = queue.shift();
    if (!entry) { settleIdle(); return; }
    entry.queued = false;
    active = entry;
    const revision = entry.revision;
    const selection = clone(entry.draft!);
    let result: T | undefined;
    let error: unknown;
    let failed = false;
    try { result = await options.write(entry.key, clone(selection)); }
    catch (failure) { error = failure; failed = true; }
    active = undefined;

    // A response for an older selection must not clear its replacement draft
    // or publish an obsolete workspace snapshot, including after page changes.
    if (revision === entry.revision) {
      entry.failed = failed;
      entry.error = error;
      if (!failed) {
        entry.draft = undefined;
        options.saved(entry.key, result as T, clone(selection));
      }
    }
    notify(entry);
    // Callbacks may have queued another selection, which flush must also wait for.
    settle(entry);
    schedule();
    settleIdle();
  }

  return {
    save(key, selection) {
      let entry = entries.get(key);
      if (!entry) {
        entry = { key, revision: 0, draft: undefined, error: undefined, failed: false, queued: false, readyAt: 0, waiters: [] };
        entries.set(key, entry);
      }
      // Completed writes are not a cache of backend state: another key can
      // update overlapping targets. The caller checks persisted no-op changes.
      const previous = entry.draft;
      if (!entry.failed && previous && same(previous, selection)
        && (entry.queued ? queue.at(-1) === entry : active === entry && !queue.length)) return;
      entry.revision += 1;
      entry.draft = clone(selection);
      entry.error = undefined;
      entry.failed = false;
      entry.readyAt = entry.waiters.length ? 0 : Date.now() + delayMs;
      enqueue(entry);
    },
    retry(key) {
      const entry = entries.get(key);
      if (!entry?.draft || pending(entry)) return;
      entry.error = undefined;
      entry.failed = false;
      entry.readyAt = 0;
      enqueue(entry);
    },
    state(key) { return state(entries.get(key)); },
    flush(key) {
      const entry = entries.get(key);
      if (!entry) return Promise.resolve();
      if (!pending(entry)) return entry.failed ? Promise.reject(entry.error) : Promise.resolve();
      // Flush also skips delays for earlier writes needed to reach this key.
      const index = queue.indexOf(entry);
      if (index >= 0) for (const queued of queue.slice(0, index + 1)) queued.readyAt = 0;
      schedule();
      return new Promise<void>((resolve, reject) => entry.waiters.push({ resolve, reject }));
    },
    idle() { return busy() ? new Promise<void>((resolve) => idleResolvers.push(resolve)) : Promise.resolve(); },
    forget(key) {
      const entry = entries.get(key);
      if (!entry) return;
      if (pending(entry)) throw new Error("Wait for selection saves before forgetting a target.");
      entries.delete(key);
      options.changed(key, state());
    },
    clear() {
      if (busy()) throw new Error("Wait for selection saves before clearing their state.");
      const keys = [...entries.keys()];
      entries.clear();
      for (const key of keys) options.changed(key, state());
    },
    get busy() { return busy(); },
  };
}
