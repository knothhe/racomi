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
  id: string; name: string; kind: string; path: string; enabledSkillIds: string[]; enabledPresetIds: string[]; enabledSourceIds?: string[];
  icon?: string; createdAt?: string; updatedAt?: string;
  paths?: string[]; projectRoot?: string; directoryIds?: string[]; customPaths?: string[];
  deployments: { skillId: string; name: string; contentHash: string; path?: string; mode?: "copy" | "symlink" }[]; pending: boolean;
}
export interface ProjectDirectoryTemplate { id: string; name: string; path: string }
export interface AppSettings {
  textSize?: 100 | 110 | 125 | 150;
  interfaceFont?: string;
  interfaceFontStyle?: string;
  snapshotRetentionDays?: number;
  snapshotMaxCount?: number;
  themeStyle?: "default" | "omarchy";
  theme?: "light" | "dark" | "system";
  locale?: "zh-CN" | "zh-TW" | "en" | "system";
  navLayout?: "auto" | "side" | "top";
  deploymentMode: "copy" | "symlink";
  defaultProjectDirectories: string[];
  defaultGlobalDirectories: string[];
  customProjectDirectories: ProjectDirectoryTemplate[];
}
export interface GlobalApplication {
  key: string; agentId: string; agentName: string; name: string; path: string; description: string; target: Target | null;
}
export interface Snapshot { id: string; createdAt: string; reason: string; skillCount: number; presetCount: number }
export interface InterfaceFontFamily { family: string; styles: { name: string; postscriptName: string }[] }
export type CloudSyncProvider = "s3" | "r2-oauth";
export interface R2OAuthStatus {
  available: boolean; connected: boolean; authorizing: boolean; lastError: string;
  accountId?: string; bucket?: string; jurisdiction?: string;
}
export interface R2Account { id: string; name: string }
export interface R2Bucket { name: string; jurisdiction: string }
export interface CloudSyncConfig {
  provider?: CloudSyncProvider;
  r2ConnectionId?: string; accountId?: string; jurisdiction?: string;
  endpoint: string; region: string; bucket: string; prefix: string;
  accessKeyId: string; secretAccessKey: string; sessionToken: string; passphrase: string;
  clearSessionToken?: boolean;
  pathStyle: boolean; autoSync: boolean; intervalMinutes: number;
  hasSecretAccessKey: boolean; hasSessionToken: boolean; hasPassphrase: boolean;
}
export interface SyncConflict { key: string; kind: string; name: string; local: unknown; remote: unknown }
export interface CloudSyncStatus {
  config: CloudSyncConfig; configured: boolean; syncing: boolean;
  manualConfig?: CloudSyncConfig; r2Config?: CloudSyncConfig; r2OAuth?: R2OAuthStatus;
  lastSyncAt: string; lastError: string; conflicts: SyncConflict[];
}
export interface CloudBackup { id: string; createdAt: string; skillCount: number; presetCount: number }
export interface DirectoryOption { id: string; name: string; path: string; kind: "global" | "project"; description: string }
export interface OmarchyTheme { colors: Record<string, string>; dark: boolean }
export interface DesktopPreferences { lightweight: boolean; openAtLogin: boolean; supported: boolean; platform: string }
export interface BuildInfo { name: string; id: string; version: string; number: string; commit: string; dirty: boolean; channel: string; executor: string; os: string; arch: string; repository: string }
export interface UpdaterState { available: boolean; reason: string; automatic: boolean; canCheck: boolean }
export interface AppState {
  desktopPreferences?: DesktopPreferences;
  cloudSync: CloudSyncStatus;
  directoryOptions: DirectoryOption[];
  dataDir: string;
  homeDir?: string;
  projects: Target[];
  projectDirectoryErrors?: Record<string, string>;
  globalApplications: GlobalApplication[];
  state: { settings: AppSettings; schema: number; skills: Skill[]; sources: Source[]; presets: Preset[]; targets: Target[] };
  snapshots: Snapshot[];
}
export interface PreviewCandidate {
  name: string; description: string; subpath: string; revision: string;
  contentHash?: string; variants?: PreviewCandidate[];
}
export interface Preview {
  local?: boolean;
  hosted?: boolean;
  id: string; source: string; ref: string; revision: string;
  candidates: PreviewCandidate[];
  warnings?: string[];
  skipped?: { subpath: string; reason: string }[];
}
export interface SearchResult { name: string; slug: string; source: string; installs: number; url: string; supported?: boolean; unsupportedReason?: string }
export interface HubAvailability { url: string; state: "available" | "missing" | "unavailable" | "unknown"; revision: string; checkedAt: string; reason: string }
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
export function invoke<T>(method: string, ...args: unknown[]): Promise<T> & { cancel?: () => Promise<void> } {
  // Return the runtime promise directly to preserve request cancellation.
  return Call.ByName(`racomi/internal/desktop.Service.${method}`, ...args) as Promise<T>;
}
export const api = {
 buildInfo: () => invoke<BuildInfo>("GetBuildInfo"),
 updaterState: () => invoke<UpdaterState>("GetUpdaterState"),
 checkApplicationUpdates: () => invoke<void>("CheckApplicationUpdates"),
 setAutomaticUpdates: (on: boolean) => invoke<UpdaterState>("SetAutomaticUpdates", on),
 frontendReady: () => invoke<number>("FrontendReady"),
 completeLifecycle: (id: number, page: number, allow: boolean) => invoke<boolean>("CompleteLifecycle", id, page, allow),
  listInterfaceFonts: () => invoke<InterfaceFontFamily[]>("ListInterfaceFonts"),
  state: () => invoke<AppState>("GetState"),
  cloudSyncStatus: () => invoke<CloudSyncStatus>("GetCloudSyncStatus"),
  getR2OAuthStatus: () => invoke<R2OAuthStatus>("GetR2OAuthStatus"),
  startR2OAuth: () => invoke<R2OAuthStatus>("StartR2OAuth"),
  cancelR2OAuth: () => invoke<R2OAuthStatus>("CancelR2OAuth"),
  disconnectR2OAuth: () => invoke<R2OAuthStatus>("DisconnectR2OAuth"),
  discardR2Connection: (connectionID: string) => invoke<void>("DiscardR2Connection", connectionID),
  listR2Accounts: () => invoke<R2Account[]>("ListR2Accounts"),
  listR2Buckets: (accountID: string, jurisdiction: string) => invoke<R2Bucket[]>("ListR2Buckets", accountID, jurisdiction),
  configureR2Bucket: (accountID: string, bucket: string, jurisdiction: string) => invoke<CloudSyncConfig>("ConfigureR2Bucket", accountID, bucket, jurisdiction),
  saveCloudSyncConfig: (config: CloudSyncConfig) => invoke<AppState>("SaveCloudSyncConfig", config),
  testCloudSyncConnection: (config: CloudSyncConfig) => invoke<void>("TestCloudSyncConnection", config),
  syncCloud: () => invoke<AppState>("SyncCloud"),
  resolveCloudSync: (resolutions: Record<string, "local" | "remote">) => invoke<AppState>("ResolveCloudSync", resolutions),
  listCloudBackups: () => invoke<CloudBackup[]>("ListCloudBackups"),
  restoreCloudBackup: (id: string) => invoke<AppState>("RestoreCloudBackup", id),
  readSkillDocument: (id: string) => invoke<string>("ReadSkillDocument", id),
  setTextSize: (size: number) => invoke<void>("SetTextSize", size),
  setLightweight: (on: boolean) => invoke<AppState>("SetLightweight", on),
  setOpenAtLogin: (on: boolean) => invoke<AppState>("SetOpenAtLogin", on),
  omarchyTheme: () => invoke<OmarchyTheme | null>("GetOmarchyTheme"),
  setTheme: (preference: "light" | "dark" | "system", style: "default" | "omarchy") => invoke<void>("SetTheme", preference, style),
  setLocale: (locale: "zh-CN" | "zh-TW" | "en") => invoke<void>("SetLocale", locale),
  saveSettings: (settings: AppSettings) => invoke<AppState>("SaveSettings", settings),
  saveSettingsPatch: (patch: Partial<AppSettings>, onlyMissing = false) => invoke<AppState>("SaveSettingsPatch", patch, onlyMissing),
  search: (query: string) => invoke<SearchResult[]>("Search", query),
  listSkills: (page: number) => invoke<SkillListPage>("ListSkills", page),
  checkHubAvailability: (inputs: string[]) => invoke<HubAvailability[]>("CheckHubAvailability", inputs),
  preview: (source: string, ref: string, subpath: string) => invoke<Preview>("Preview", source, ref, subpath),
  previewHubRepository: (source: string, ref: string, subpath: string) => invoke<Preview>("PreviewHubRepository", source, ref, subpath),
  previewLocal: (path: string) => invoke<Preview>("PreviewLocal", path),
  install: (id: string, paths: string[]) => invoke<AppState>("Install", id, paths),
  saveSource: (source: Source, expected: Source | null = null) => invoke<AppState>("SaveSourceChecked", source, expected),
  deleteSource: (id: string) => invoke<AppState>("DeleteSource", id),
  refreshSource: (id: string) => invoke<AppState>("RefreshSource", id),
  syncSource: (id: string, revision: string, paths: string[]) => invoke<AppState>("SyncSource", id, revision, paths),
  relinkSourceSkill: (skillId: string, sourceId: string, subpath: string) => invoke<AppState>("RelinkSourceSkill", skillId, sourceId, subpath),
  checkUpdates: () => invoke<Update[]>("CheckUpdates"),
  updateSkill: (id: string) => invoke<AppState>("UpdateSkill", id),
  updateSkills: (ids: string[]) => invoke<AppState>("UpdateSkills", ids),
  removeSkills: (ids: string[]) => invoke<AppState>("RemoveSkills", ids),
  removeSkill: (id: string) => invoke<AppState>("RemoveSkill", id),
  savePreset: (preset: Preset, expected: Preset | null = null) => invoke<AppState>("SavePresetChecked", preset, expected),
  deletePreset: (id: string) => invoke<AppState>("DeletePreset", id),
  saveProject: (project: Target) => invoke<AppState>("SaveProject", project),
  deleteProject: (id: string) => invoke<AppState>("DeleteProject", id),
  saveGlobalDirectory: (target: Target) => invoke<AppState>("SaveGlobalDirectory", target),
  deleteGlobalDirectory: (id: string) => invoke<AppState>("DeleteGlobalDirectory", id),
  setProjectSelection: (id: string, skillIDs: string[], presetIDs: string[], sourceIDs: string[] = []) => invoke<AppState>("SetProjectSelectionWithSources", id, skillIDs, presetIDs, sourceIDs),
  reorderProjects: (ids: string[]) => invoke<AppState>("ReorderProjects", ids),
  planProjects: (ids: string[]) => invoke<ApplyPlan>("PlanProjects", ids),
  applyProjects: (ids: string[]) => invoke<AppState>("ApplyProjects", ids),
  forceApplyProjects: (ids: string[], plan: ApplyPlan) => invoke<AppState>("ForceApplyProjects", ids, plan),
  planProject: (id: string) => invoke<ApplyPlan>("PlanProject", id),
  applyProject: (id: string) => invoke<AppState>("ApplyProject", id),
  forceApplyProject: (id: string, plan: ApplyPlan) => invoke<AppState>("ForceApplyProject", id, plan),
  setGlobalSelection: (key: string, skillIDs: string[], presetIDs: string[], sourceIDs: string[] = []) => invoke<AppState>("SetGlobalSelectionWithSources", key, skillIDs, presetIDs, sourceIDs),
  setGlobalSelections: (keys: string[], skillIDs: string[], presetIDs: string[], sourceIDs: string[] = []) => invoke<AppState>("SetGlobalSelectionsWithSources", keys, skillIDs, presetIDs, sourceIDs),
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
