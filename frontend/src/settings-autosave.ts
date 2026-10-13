import type { AppSettings } from "./api";

export interface SettingsAutosaveOptions<T> {
  read: () => AppSettings;
  write: (settings: AppSettings, patch: Partial<AppSettings>) => Promise<T>;
  settings: (result: T) => AppSettings;
  start: () => void;
  error: (error: unknown) => void;
  settled: (latestResult: T | undefined) => void;
}

export interface SettingsAutosave {
  save: (patch: Partial<AppSettings>) => void;
  readonly busy: boolean;
  idle: () => Promise<void>;
}

function cloneSettings(settings: AppSettings): AppSettings {
  return {
    ...(settings.textSize ? { textSize: settings.textSize } : {}),
    ...(settings.interfaceFont ? { interfaceFont: settings.interfaceFont } : {}),
    ...(settings.theme ? { theme: settings.theme } : {}),
    ...(settings.locale ? { locale: settings.locale } : {}),
    ...(settings.navLayout ? { navLayout: settings.navLayout } : {}),
    deploymentMode: settings.deploymentMode,
    defaultProjectDirectories: [...settings.defaultProjectDirectories],
    defaultGlobalDirectories: [...settings.defaultGlobalDirectories],
    customProjectDirectories: (settings.customProjectDirectories ?? []).map((directory) => ({ ...directory })),
  };
}

function clonePatch(patch: Partial<AppSettings>): Partial<AppSettings> {
  const snapshot: Partial<AppSettings> = {};
  if (patch.textSize !== undefined) snapshot.textSize = patch.textSize;
  if (patch.interfaceFont !== undefined) snapshot.interfaceFont = patch.interfaceFont;
  if (patch.theme !== undefined) snapshot.theme = patch.theme;
  if (patch.locale !== undefined) snapshot.locale = patch.locale;
  if (patch.navLayout !== undefined) snapshot.navLayout = patch.navLayout;
  if (patch.deploymentMode !== undefined) snapshot.deploymentMode = patch.deploymentMode;
  if (patch.defaultProjectDirectories !== undefined) snapshot.defaultProjectDirectories = [...patch.defaultProjectDirectories];
  if (patch.defaultGlobalDirectories !== undefined) snapshot.defaultGlobalDirectories = [...patch.defaultGlobalDirectories];
  if (patch.customProjectDirectories !== undefined) snapshot.customProjectDirectories = patch.customProjectDirectories.map((directory) => ({ ...directory }));
  return snapshot;
}

// Keep the controls in place until the queue drains, while every write builds
// on the last successful response. A failed patch never leaks into later saves.
export function createSettingsAutosave<T>(options: SettingsAutosaveOptions<T>): SettingsAutosave {
  const queue: Partial<AppSettings>[] = [];
  const idleResolvers: (() => void)[] = [];
  let busy = false;

  function finish(latestResult: T | undefined): void {
    busy = false;
    options.settled(latestResult);
    // A settled callback may start another batch; idle then waits for that one.
    if (!busy) for (const resolve of idleResolvers.splice(0)) resolve();
  }

  async function drain(): Promise<void> {
    let committed: AppSettings;
    try {
      committed = cloneSettings(options.read());
    } catch (error) {
      queue.length = 0;
      options.error(error);
      finish(undefined);
      return;
    }
    let latestResult: T | undefined;
    while (queue.length) {
      const patch = queue.shift()!;
      try {
        const result = await options.write(cloneSettings({ ...committed, ...patch }), clonePatch(patch));
        committed = cloneSettings(options.settings(result));
        latestResult = result;
      } catch (error) {
        options.error(error);
      }
    }
    finish(latestResult);
  }

  return {
    save(patch) {
      queue.push(clonePatch(patch));
      if (busy) return;
      busy = true;
      options.start();
      void drain();
    },
    get busy() { return busy; },
    idle() {
      return busy ? new Promise<void>((resolve) => idleResolvers.push(resolve)) : Promise.resolve();
    },
  };
}
