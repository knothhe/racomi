import { Call } from "@wailsio/runtime";

export interface Skill {
  id: string; name: string; description: string; source: string; ref: string;
  subpath: string; revision: string; contentHash: string; installedAt: string; updatedAt: string;
  sourceId: string; upstreamRemoved: boolean;
}
export interface SourceSkill {
  name: string; description: string; subpath: string; revision: string;
  contentHash: string; removed: boolean; new: boolean; skipReason?: string;
}
export interface Source {
  id: string; name: string; repository: string; ref: string; subpath: string;
  mode: "selected" | "all"; revision: string; refreshedAt: string; lastError: string;
  catalog: SourceSkill[]; warnings?: string[];
}
export interface Preset { id: string; name: string; description: string; skillIds: string[]; icon?: string }
export interface Target {
  id: string; name: string; kind: string; path: string; enabledSkillIds: string[]; enabledPresetIds: string[];
  icon?: string;
  paths?: string[]; projectRoot?: string; directoryIds?: string[]; customPaths?: string[];
  deployments: { skillId: string; name: string; contentHash: string; path?: string; mode?: "copy" | "symlink" }[]; pending: boolean;
}
export interface ProjectDirectoryTemplate { id: string; name: string; path: string }
export interface AppSettings {
  deploymentMode: "copy" | "symlink";
  defaultProjectDirectories: string[];
  defaultGlobalDirectories: string[];
  customProjectDirectories: ProjectDirectoryTemplate[];
}
export interface GlobalApplication {
  key: string; agentId: string; agentName: string; name: string; path: string; description: string; target: Target | null;
}
export interface Snapshot { id: string; createdAt: string; reason: string; skillCount: number; presetCount: number }
export interface DirectoryOption { id: string; name: string; path: string; kind: "global" | "project"; description: string }
export interface AppState {
  directoryOptions: DirectoryOption[];
  dataDir: string;
  homeDir?: string;
  projects: Target[];
  globalApplications: GlobalApplication[];
  state: { settings: AppSettings; schema: number; skills: Skill[]; sources: Source[]; presets: Preset[]; targets: Target[] };
  snapshots: Snapshot[];
}
export interface Preview {
  local?: boolean;
  id: string; source: string; ref: string; revision: string;
  candidates: { name: string; description: string; subpath: string; revision: string }[];
  warnings?: string[];
  skipped?: { subpath: string; reason: string }[];
}
export interface SearchResult { name: string; slug: string; source: string; installs: number; url: string; supported?: boolean; unsupportedReason?: string }
export interface SkillListPage { skills: SearchResult[]; total: number; hasMore: boolean; page: number }
export interface Update { skillId: string; revision: string; summary: string; available: boolean; removed: boolean; error: string }
export interface ApplyPlan {
  targetId: string; add: string[]; update: string[]; remove: string[]; conflicts: string[];
  overwrite?: string[]; blockingConflicts?: string[];
  directories?: { targetId: string; path: string; add: string[]; update: string[]; remove: string[]; conflicts: string[]; overwrite?: string[]; blockingConflicts?: string[]; fingerprint?: string }[];
}
export interface SkillsManagerPreview {
  id: string; databasePath: string; skillCount: number; presetCount: number; sourceCount: number;
  warnings: string[]; skillNames: string[]; presetNames: string[];
}
export interface ImportResult {
  addedSkills: number; skippedSkills: number; addedPresets: number; skippedPresets: number;
  addedSources: number; skippedSources: number;
}

// Kept in one place so desktop bindings can evolve without touching the views.
export async function invoke<T>(method: string, ...args: unknown[]): Promise<T> {
  return Call.ByName(`racomi/internal/desktop.Service.${method}`, ...args) as Promise<T>;
}
export const api = {
  state: () => invoke<AppState>("GetState"),
  readSkillDocument: (id: string) => invoke<string>("ReadSkillDocument", id),
  setTheme: (preference: "light" | "dark" | "system") => invoke<void>("SetTheme", preference),
  setLocale: (locale: "zh-CN" | "zh-TW" | "en") => invoke<void>("SetLocale", locale),
  saveSettings: (settings: AppSettings) => invoke<AppState>("SaveSettings", settings),
  search: (query: string) => invoke<SearchResult[]>("Search", query),
  listSkills: (page: number) => invoke<SkillListPage>("ListSkills", page),
  preview: (source: string, ref: string, subpath: string) => invoke<Preview>("Preview", source, ref, subpath),
  previewLocal: (path: string) => invoke<Preview>("PreviewLocal", path),
  install: (id: string, paths: string[]) => invoke<AppState>("Install", id, paths),
  saveSource: (source: Source) => invoke<AppState>("SaveSource", source),
  deleteSource: (id: string) => invoke<AppState>("DeleteSource", id),
  refreshSource: (id: string) => invoke<AppState>("RefreshSource", id),
  syncSource: (id: string, revision: string, paths: string[]) => invoke<AppState>("SyncSource", id, revision, paths),
  relinkSourceSkill: (skillId: string, sourceId: string, subpath: string) => invoke<AppState>("RelinkSourceSkill", skillId, sourceId, subpath),
  checkUpdates: () => invoke<Update[]>("CheckUpdates"),
  updateSkill: (id: string) => invoke<AppState>("UpdateSkill", id),
  updateSkills: (ids: string[]) => invoke<AppState>("UpdateSkills", ids),
  removeSkills: (ids: string[]) => invoke<AppState>("RemoveSkills", ids),
  removeSkill: (id: string) => invoke<AppState>("RemoveSkill", id),
  savePreset: (preset: Preset) => invoke<AppState>("SavePreset", preset),
  deletePreset: (id: string) => invoke<AppState>("DeletePreset", id),
  saveProject: (project: Target) => invoke<AppState>("SaveProject", project),
  deleteProject: (id: string) => invoke<AppState>("DeleteProject", id),
  saveGlobalDirectory: (target: Target) => invoke<AppState>("SaveGlobalDirectory", target),
  deleteGlobalDirectory: (id: string) => invoke<AppState>("DeleteGlobalDirectory", id),
  setProjectSelection: (id: string, skillIDs: string[], presetIDs: string[]) => invoke<AppState>("SetProjectSelection", id, skillIDs, presetIDs),
  planProject: (id: string) => invoke<ApplyPlan>("PlanProject", id),
  applyProject: (id: string) => invoke<AppState>("ApplyProject", id),
  forceApplyProject: (id: string, plan: ApplyPlan) => invoke<AppState>("ForceApplyProject", id, plan),
  setGlobalSelection: (key: string, skillIDs: string[], presetIDs: string[]) => invoke<AppState>("SetGlobalSelection", key, skillIDs, presetIDs),
  setGlobalSelections: (keys: string[], skillIDs: string[], presetIDs: string[]) => invoke<AppState>("SetGlobalSelections", keys, skillIDs, presetIDs),
  planGlobals: (keys: string[]) => invoke<ApplyPlan>("PlanGlobals", keys),
  applyGlobals: (keys: string[]) => invoke<AppState>("ApplyGlobals", keys),
  forceApplyGlobals: (keys: string[], plan: ApplyPlan) => invoke<AppState>("ForceApplyGlobals", keys, plan),
  planGlobal: (key: string) => invoke<ApplyPlan>("PlanGlobal", key),
  applyGlobal: (key: string) => invoke<AppState>("ApplyGlobal", key),
  forceApplyGlobal: (key: string, plan: ApplyPlan) => invoke<AppState>("ForceApplyGlobal", key, plan),
  createSnapshot: () => invoke<AppState>("CreateSnapshot"),
  restoreSnapshot: (id: string) => invoke<AppState>("RestoreSnapshot", id),
  exportBackup: (path: string) => invoke<string>("ExportBackup", path),
  importBackup: (path: string) => invoke<AppState>("ImportBackup", path),
  defaultSkillsManagerPath: () => invoke<string>("DefaultSkillsManagerPath"),
  chooseSkillsManagerFile: () => invoke<string>("ChooseSkillsManagerFile"),
  previewSkillsManager: (path: string) => invoke<SkillsManagerPreview>("PreviewSkillsManager", path),
  importSkillsManager: (previewID: string) => invoke<{ app: AppState; result: ImportResult }>("ImportSkillsManager", previewID),
  chooseDirectory: () => invoke<string>("ChooseDirectory"),
  chooseExportFile: () => invoke<string>("ChooseExportFile"),
  chooseImportFile: () => invoke<string>("ChooseImportFile"),
};
