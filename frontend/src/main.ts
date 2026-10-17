import { installSelects } from "./select";
import { renderMarkdownDocument } from "./markdown";
import { applyInterfaceSettings, defaultFontStyle, loadInterfaceFonts, textSizes } from "./interface";
import { renderSourceFilter, installSourceFilters } from "./source-filter";
import { syncNavigationMarkup } from "./navigation-render";
import { installWorkspaceResize, syncWorkspaceResize } from "./workspace-resize";
import { hostedSourceDomain, hubResultInstalled, sourceDisplayName } from "./source-names";
import { changeSelection } from "./skill-selection";
import { matchesProject, matchesProjectSkill, projectSkillApplied } from "./project-browser";
import { applicationPlanDisposition, hasApplicationChanges } from "./application-plan";
import { createSettingsAutosave } from "./settings-autosave";
import { cloudBackupsList, cloudConflictFields, cloudSyncConfigFields, cloudSyncSection, readCloudResolutions, readCloudSyncConfig } from "./cloud-sync";
import { showFeedback } from "./feedback";
import { getNavLayout, getNavLayoutPreference, installSidebarResize, setNavLayoutPreference, syncSidebarWidth, toggleNavLayout, type NavLayoutPreference } from "./layout";
import { icon, projectIcons, projectIconName, presetIcons, presetIconName } from "./icons";
import { createProjectSelectionAutosave } from "./project-selection-autosave";
import { guidePage, guideTabs, type GuideTab } from "./guide";
import { agentIcon } from "./agent-icons";
import { homeRelativePath } from "./paths";
import { enableNativeLocaleSync, getLocale, getLocalePreference, setLocale, t, tMessage, type LocalePreference, type TranslationVariables } from "./i18n";
import "./style.css";
import "./source-filter.css";
import "./markdown.css";
import { defaultSkillFilters, filterSkills, pendingSkills, skillSource, type SkillFilters } from "./skill-filters";
import { catalogEntries, sourceStatus, sourceStatusDetails, sourceSyncPaths, summarizeSource, type SourceEntry } from "./source-catalog";
import { defaultSettings, projectDirectories, settingsPage } from "./settings";
import { enableNativeThemeSync, getThemePreference, setThemePreference, type ThemePreference } from "./theme";
import { api, type AppSettings, type AppState, type ApplyPlan, type CloudBackup, type SyncConflict, type Source, type ImportResult, type Preset, type Preview, type SearchResult, type SkillListPage, type Skill, type SkillsManagerPreview, type Target, type Update } from "./api";

type Page = "skills" | "sources" | "discover" | "presets" | "projects" | "global" | "settings" | "guide";
type SkillsFilter = "all" | "updates" | "pending";
const nav: { id: Page; title: string; icon: string }[] = [
  { id: "discover", title: "发现", icon: "compass" },
  { id: "sources", title: "来源", icon: "refresh" },
  { id: "skills", title: "技能", icon: "skills" },
  { id: "presets", title: "预设", icon: "layers" },
  { id: "global", title: "全局", icon: "monitor" },
  { id: "projects", title: "项目", icon: "folder" },
  { id: "settings", title: "设置", icon: "settings" },
  { id: "guide", title: "指南", icon: "book" },
];

const e = (value: unknown) => String(value ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);
const short = (value: string) => value ? value.slice(0, 8) : "—";
const date = (value: string) => value && !Number.isNaN(new Date(value).getTime()) ? new Date(value).toLocaleString(getLocale(), { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }) : "—";
const arr = <T>(value: T[] | null | undefined): T[] => value || [];
if (location.protocol === "wails:" && /Mac/.test(navigator.platform || navigator.userAgent)) document.body.classList.add("native-mac");
const app = document.querySelector<HTMLDivElement>("#app")!;
const modalRoot = document.querySelector<HTMLDivElement>("#modal-root")!;
// Give the webview a neutral initial focus target. Keyboard navigation retains
// its normal visible focus ring when the user starts tabbing through controls.
app.tabIndex = -1;
app.focus({ preventScroll: true });
let currentPage: Page = "skills";
let data: AppState | null = null;
let stateRevision = 0;
let loadError = "";
let selectedSkill = "";
let bulkSkillSelection = new Set<string>();
let selectedSource = "";
let selectedPreset = "";
let sourceCatalogFilter: "changes" | "all" = "changes";
const sourceSelections = new Map<string, Set<string>>();
let skillsQuery = "";
let skillsFilter: SkillsFilter = "all";
let skillsSort: "asc" | "desc" = "asc";
let skillsSourceFilter = "all";
let presetFilters = defaultSkillFilters();
let presetSkillSelection = new Set<string>();
const presetSkillDetailsOpen = new Set<string>();
let detailTab: "overview" | "document" = "overview";
type DiscoverTab = "hub" | "git" | "local";
let discoverTab: DiscoverTab = "hub";
const pageQueries = { sources: "", presets: "", projects: "", global: "" };
let selectedProject = "";
let visibleProject = "";
let projectsFilter: "all" | "pending" = "all";
let globalFilter: "all" | "pending" = "all";
const projectSkillFilters = new Map<string, { query: string; selectedOnly: boolean }>();
const projectDetailsOpen = new Map<string, Set<string>>();
const preservedScroll = new Map<string, number>();
let selectedGlobalKey = "";
let selectedGlobalKeys = new Set<string>();
let globalDirectoriesInitialized = false;
let settingsBackups = false;
let guideTab: GuideTab = "start";
let settingsDraft: AppSettings | null = null;
let settingsWriteRevision = 0;
let applyingSharedPreferences = false;
let preferencesMigrated = false;
let cloudPollBusy = false;
let loadedCloudSyncAt = "";
let cloudConflictDraft: SyncConflict[] = [];
let cloudBackupDraft: CloudBackup[] = [];
const sourceEditorBaselines = new WeakMap<HTMLFormElement, Source | null>();
const presetEditorBaselines = new WeakMap<HTMLFormElement, Preset | null>();
const documents = new Map<string, { hash: string; text: string; rendered?: { locale: string; wrap: boolean; html: string } }>();
const loadingDocuments = new Set<string>();
const documentErrors = new Map<string, { hash: string; message: string }>();
const rawDocuments = new Set<string>();
let metadataWrap = true;
let modalPreviousFocus: HTMLElement | null = null;
let modalGeneration = 0;
let searchResults: SearchResult[] | null = null;
let searchQuery = "";
let hubResultQuery = "";
let hubListing: Pick<SkillListPage, "page" | "total" | "hasMore"> | null = null;
let preview: Preview | null = null;
type PreviewRequest = { source: string; ref: string; subpath: string; matchName: string; kind: "git" | "local"; startedAt: number; loading: boolean; error: string };
let previewRequest: PreviewRequest | null = null;
let sourceDraft = "";
let localSourceDraft = "";
let refDraft = "";
let subpathDraft = "";
let selectedCandidates = new Set<string>();
let updates: Update[] | null = null;
let plans = new Map<string, ApplyPlan>();
type ApplicationScope = "project" | "global";
type ApplicationSelection = { skillIds: string[]; presetIds: string[] };
const projectDrafts = new Map<string, ApplicationSelection>();
const globalDrafts = new Map<string, ApplicationSelection>();
let activeTasks = new Set<string>();
const taskMessages = new Map<string, { key: string; variables: TranslationVariables }>();
let confirmResolve: ((result: boolean) => void) | null = null;
let skillsManagerPreview: SkillsManagerPreview | null = null;
let skillsManagerImportResult: ImportResult | null = null;
const skillsManagerImportTask = "正在合并 skills-manager 数据库…";
const skillsManagerTasks = ["正在查找 skills-manager 数据库…", "正在选择 skills-manager 数据库…", "正在预览 skills-manager 数据库…", skillsManagerImportTask];
const skillsManagerBusy = () => skillsManagerTasks.some((label) => activeTasks.has(label));
const skills = () => arr(data?.state.skills);
const sources = () => arr(data?.state.sources);
const presets = () => arr(data?.state.presets);
const managedApplications = () => arr(data?.state.targets);
const projects = () => arr(data?.projects);
const globalApplications = () => arr(data?.globalApplications).map((application) => ({ ...application, agentName: application.agentId === "custom" ? t("自定义") : application.agentId === "agents" ? "Agent Skills" : application.agentName, name: application.agentId === "custom" ? application.name : t(application.name), description: t(application.description) }));
const globalPath = (path: string) => homeRelativePath(path, data?.homeDir);
function uniqueApplications<T extends { path: string }>(applications: T[]): T[] {
  const seen = new Set<string>();
  return applications.filter((application) => {
    const path = /Mac|Win/.test(navigator.platform || navigator.userAgent) ? application.path.toLowerCase() : application.path;
    if (seen.has(path)) return false;
    seen.add(path); return true;
  });
}
const selectedIDs = (form: HTMLFormElement, name: string) => [...new FormData(form).getAll(name)].map(String);
const field = (form: HTMLFormElement, name: string) => String(new FormData(form).get(name) || "").trim();
function toast(message: string, error = false) { showFeedback(error ? tMessage(message) : message, error); }
const stateChangingTasks = new Set([
  "正在保存来源…", "正在刷新来源…", "正在同步来源中的技能…", "正在删除来源…",
  "正在重新关联技能…", "正在检查技能更新…", "正在读取刷新结果…", "正在更新技能…", "正在移除技能…", "正在导入所选技能…",
  "正在保存预设…", "正在删除预设…", "正在保存项目…", "正在保存项目选择…", "正在保存全局选择…", "正在移除项目…", "正在保存全局目录…", "正在删除全局目录…", "正在保存项目目录…", "正在删除项目目录…",
  "正在保存设置…", "正在创建本地快照…", "正在应用技能到项目…", "正在应用技能到全局…", "正在恢复快照…", "正在导入备份…", skillsManagerImportTask,
  "Saving sync settings…", "Syncing with cloud…", "Resolving sync conflicts…", "Restoring cloud backup…",
]);
const hasPendingMutation = () => [...activeTasks].some((label) => stateChangingTasks.has(label));
const skillSelectionActions = ["clear-selected-skills", "update-selected-skills", "remove-selected-skills"];
const actionTasks: Record<string, string> = {
  "update-selected-skills": "正在更新技能…", "remove-selected-skills": "正在移除技能…",
  "load-more-hub": "正在搜索 skills.sh…",
  "check-updates": "正在检查技能更新…", "refresh-source": "正在刷新来源…",
  "select-filtered-preset-skills": "正在保存预设…",
  "sync-source": "正在同步来源中的技能…", "hub-preview": "正在读取来源并发现技能…",
  "choose-local-dir": "正在选择技能目录…",
  "select-all-candidates": "正在导入所选技能…",
  "install": "正在导入所选技能…", "plan-project": "正在检查项目变化…", "plan-global": "正在检查全局变化…",
  "apply-project": "正在应用技能到项目…", "apply-global": "正在应用技能到全局…", "create-snapshot": "正在创建本地快照…",
  "sync-cloud": "Syncing with cloud…", "test-cloud-sync": "Testing cloud connection…", "cloud-backups": "Loading cloud backups…",
};
const formTasks: Record<string, string> = {
  "hub-search": "正在搜索 skills.sh…", "source-preview": "正在读取来源并发现技能…",
  "local-preview": "正在读取本地目录并发现技能…",
  "source-editor": "正在保存来源…", "source-relink": "正在重新关联技能…",
  "preset-editor": "正在保存预设…", "project-editor": "正在保存项目…", "global-directory-editor": "正在保存全局目录…", "project-directory-editor": "正在保存项目目录…",
  "backup-export": "正在导出工作空间…", "backup-import": "正在导入备份…",
  "skills-manager-import": "正在预览 skills-manager 数据库…",
  "cloud-sync-editor": "Saving sync settings…", "cloud-conflicts-editor": "Resolving sync conflicts…",
};
const applicationActions = ["plan-project", "apply-project", "plan-global", "apply-global", "clear-application", "retry-selection", "select-global-directories", "default-global-directories", "new-global-directory", "edit-global-directory", "delete-global-directory", "new-project-directory", "edit-project-directory", "delete-project-directory"];
const applicationBusy = () => hasPendingMutation() || ["正在检查项目变化…", "正在检查全局变化…"].some((label) => activeTasks.has(label));
const discoveryBusy = () => ["正在读取来源并发现技能…", "正在读取本地目录并发现技能…", "正在选择技能目录…", "正在导入所选技能…"].some((label) => activeTasks.has(label));
const settingsControlsBusy = () => selectionAutosave.busy || [...activeTasks].some((label) => label !== "正在保存设置…" && (stateChangingTasks.has(label) || ["正在检查项目变化…", "正在检查全局变化…"].includes(label)));
const settingsAutosave = createSettingsAutosave<AppState>({
  read: () => data!.state.settings,
  write: (_settings, patch) => api.saveSettingsPatch(patch),
  settings: (next) => next.state.settings,
  start: () => { activeTasks.add("正在保存设置…"); status(); syncBusyControls(); },
  error: (err) => toast(t("设置保存失败：{0}", { 0: tMessage(err instanceof Error ? err.message : String(err)) }), true),
  settled: (next) => {
    settingsDraft = null;
    activeTasks.delete("正在保存设置…");
    if (next) accept(next); else { if (data) applyInterfaceSettings(data.state.settings); render(); }
  },
});
const selectionAutosave = createProjectSelectionAutosave<AppState>({
  write: async (key, selection) => {
    await settingsAutosave.idle();
    const separator = key.indexOf(":");
    const scope = key.slice(0, separator) as ApplicationScope;
    const id = key.slice(separator + 1);
    return scope === "project" ? api.setProjectSelection(id, selection.skillIds, selection.presetIds) : api.setGlobalSelections(globalBatchKeys(id), selection.skillIds, selection.presetIds);
  },
  saved: (_key, next) => accept(next),
  changed: () => { if (data) render(); },
});
function saveApplicationSelection(scope: ApplicationScope, id: string, selection: ApplicationSelection) {
  selectionDrafts(scope).set(id, selection);
  plans.delete(applicationKey(scope, id));
  const application = findApplication(scope, id);
  if (selectionAutosave.state(applicationKey(scope, id)).pending || !application || applicationDirty(scope, id, application.target) || selectionAutosave.state(applicationKey(scope, id)).error) selectionAutosave.save(applicationKey(scope, id), selection);
  else { selectionDrafts(scope).delete(id); render(); }
}
function globalDirectorySelectionBlocked() {
  // A different batch reads saved targets, so switching before this draft is
  // saved would hide its choices. Keep failed drafts visible for retry too.
  return selectionAutosave.busy || !!selectionAutosave.state(applicationKey("global", selectedGlobalKey)).error;
}
function syncBusyControls() {
  for (const control of document.querySelectorAll<HTMLButtonElement | HTMLInputElement | HTMLSelectElement>("button,input,select")) {
    const form = control.form;
    const selectionForm = form?.classList.contains("application-selection");
    const label = control instanceof HTMLButtonElement ? actionTasks[control.dataset.action || ""] || (control.type === "submit" ? formTasks[form?.id || ""] || (selectionForm ? form?.dataset.scope === "global" ? "正在保存全局选择…" : "正在保存项目选择…" : "") : "") : ["source-preview", "local-preview", "hub-search"].includes(form?.id || "") ? formTasks[form!.id] : control.name === "source-candidate" ? "正在同步来源中的技能…" : control.name === "candidate" ? "正在导入所选技能…" : "";
    const applicationControl = selectionForm || applicationActions.includes(control.dataset.action || "") || ["project-editor", "global-directory-editor", "project-directory-editor"].includes(form?.id || "") || control.name === "global-directory-selection";
    const importerControl = !!control.closest(".modal")?.querySelector("#skills-manager-import");
    const importerBusy = importerControl && skillsManagerBusy() && (control.dataset.action !== "close-modal" || activeTasks.has(skillsManagerImportTask));
    const importerEntryBusy = control.dataset.action === "import-skills-manager" && skillsManagerBusy();
    const skillControl = ["bulk-skill", "bulk-skills-all"].includes(control.name) || ["update-skill", "remove-skill", ...skillSelectionActions].includes(control.dataset.action || "");
    const discoveryControl = control.closest(".discover-content") || control.dataset.action === "discover-tab";
    const globalDirectoryControl = control.name === "global-directory-selection" || ["select-global-directories", "default-global-directories"].includes(control.dataset.action || "");
    const cloudControl = ["cloud-sync-editor", "cloud-conflicts-editor"].includes(form?.id || "") || ["configure-cloud-sync", "sync-cloud", "cloud-conflicts", "cloud-backups", "restore-cloud-backup"].includes(control.dataset.action || "");
    const desktopControl = ["lightweight", "open-at-login"].includes(control.name);
    const busy = (desktopControl && activeTasks.has("正在保存设置…")) || (!!cloudControl && (hasPendingMutation() || !!data?.cloudSync?.syncing || activeTasks.has("Testing cloud connection…"))) || (!!globalDirectoryControl && globalDirectorySelectionBlocked()) || (!!discoveryControl && discoveryBusy()) || (!!skillControl && hasPendingMutation()) || (form?.id === "settings-editor" && settingsControlsBusy()) || importerBusy || importerEntryBusy || (control.dataset.action === "update-skill" && hasPendingMutation()) || (!!label && activeTasks.has(label)) || (!!applicationControl && applicationBusy());
    if (busy) { if (control.dataset.busyDisabled === undefined) control.dataset.busyDisabled = String(control.disabled); control.disabled = true; control.setAttribute("aria-busy", "true"); }
    if (!busy && control.dataset.busyDisabled !== undefined) { control.disabled = control.dataset.busyDisabled === "true"; delete control.dataset.busyDisabled; control.removeAttribute("aria-busy"); }
  }
}
function status() {
  const el = document.querySelector("#activity");
  const label = [...activeTasks][0] || (selectionAutosave.busy ? "正在自动保存…" : "");
  const message = taskMessages.get(label);
  if (el) el.innerHTML = activeTasks.size || selectionAutosave.busy ? `<span class="spinner"></span><span>${e(message ? t(message.key, message.variables) : t(label))}</span>` : t("{0}本地工作空间", { 0: icon("check") });
}
async function task<T>(message: string | { key: string; variables: TranslationVariables }, action: () => Promise<T>): Promise<T | undefined> {
  const label = typeof message === "string" ? message : `${message.key}:${JSON.stringify(message.variables)}`;
  if (activeTasks.has(label)) return;
  if (typeof message !== "string") taskMessages.set(label, message);
  activeTasks.add(label); status(); syncBusyControls();
  try {
    if (stateChangingTasks.has(label)) {
      await selectionAutosave.idle();
      if (settingsAutosave.busy) await settingsAutosave.idle();
    }
    return await action();
  }
  catch (err) { toast(err instanceof Error ? err.message : String(err), true); return undefined; }
  finally { activeTasks.delete(label); taskMessages.delete(label); status(); syncBusyControls(); }
}
function accept(next: AppState, preserveUpdates = false) {
  if (!preserveUpdates && data && JSON.stringify(data.state.skills) !== JSON.stringify(next.state.skills)) updates = null;
  data = next;
  data.state.settings = { ...defaultSettings(), ...data.state.settings };
  data.state.settings.customProjectDirectories = arr(data.state.settings.customProjectDirectories);
  loadedCloudSyncAt = data.cloudSync?.lastSyncAt || "";
  stateRevision++;
  data.state.skills = arr(data.state.skills);
  data.state.sources = arr(data.state.sources);
  bulkSkillSelection = new Set([...bulkSkillSelection].filter((id) => skills().some((skill) => skill.id === id)));
  if (!["all", "local", "linked"].includes(skillsSourceFilter) && !sources().some((source) => source.id === skillsSourceFilter)) skillsSourceFilter = "all";
  for (const source of data.state.sources) source.catalog = arr(source.catalog);
  data.state.presets = arr(data.state.presets);
  data.state.targets = arr(data.state.targets);
  data.snapshots = arr(data.snapshots);
  data.projects = arr(data.projects);
  data.globalApplications = arr(data.globalApplications);
  const selectionRepairs: { key: string; selection?: ApplicationSelection }[] = [];
  for (const [scope, drafts] of [["project", projectDrafts], ["global", globalDrafts]] as const) {
    for (const [id, selection] of drafts) {
      const application = findApplication(scope, id);
      const valid = { skillIds: selection.skillIds.filter((skillID) => skills().some((skill) => skill.id === skillID)), presetIds: selection.presetIds.filter((presetID) => presets().some((preset) => preset.id === presetID)) };
      const matchesSaved = application && (scope === "global" && id.startsWith("batch:") ? globalApplications().filter((item) => globalBatchKeys(id).includes(item.key)).every((item) => sameSelection(valid, savedSelection(item.target))) : sameSelection(valid, savedSelection(application.target)));
      const key = applicationKey(scope, id);
      const save = selectionAutosave.state(key);
      if (!save.pending && (save.error && (!application || matchesSaved) || save.draft && !sameSelection(save.draft, valid))) selectionRepairs.push({ key, selection: application && !matchesSaved ? valid : undefined });
      if (!application || matchesSaved) drafts.delete(id);
      else drafts.set(id, valid);
    }
  }
  if (!skills().some((s) => s.id === selectedSkill)) selectedSkill = skills()[0]?.id || "";
  if (!sources().some((source) => source.id === selectedSource)) selectedSource = sources()[0]?.id || "";
  for (const [id, selection] of sourceSelections) {
    const source = sources().find((item) => item.id === id);
    if (!source) { sourceSelections.delete(id); continue; }
    const available = new Set(sourceEntries(source).filter((entry) => entry.kind === "install" || entry.kind === "update").map((entry) => entry.item.subpath));
    for (const path of selection) if (!available.has(path)) selection.delete(path);
  }
  plans.clear();
  for (const [id, doc] of documents) { if (skills().find((skill) => skill.id === id)?.contentHash !== doc.hash) documents.delete(id); }
  applySharedPreferences();
  render();
  for (const repair of selectionRepairs) {
    selectionAutosave.forget(repair.key);
    if (repair.selection) selectionAutosave.save(repair.key, repair.selection);
  }
  if (detailTab === "document" && selectedSkill) void loadSkillDocument(selectedSkill);
}
function savePreference(patch: Partial<AppSettings>) {
  if (!data || applyingSharedPreferences) return;
  const current = settingsDraft || data.state.settings;
  if (Object.entries(patch).every(([key, value]) => current[key as keyof AppSettings] === value)) return;
  settingsWriteRevision++;
  settingsDraft = { ...(settingsDraft || data.state.settings), ...patch };
  settingsAutosave.save(patch);
}
function applySharedPreferences() {
  if (!data) return;
  const settings = data.state.settings;
  applyInterfaceSettings(settings);
  applyingSharedPreferences = true;
  try {
    if (settings.theme && settings.theme !== getThemePreference()) setThemePreference(settings.theme);
    if (settings.locale && settings.locale !== getLocalePreference()) setLocale(settings.locale);
    if (settings.navLayout && settings.navLayout !== getNavLayoutPreference()) setNavLayoutPreference(settings.navLayout);
  } finally { applyingSharedPreferences = false; }
  if (!preferencesMigrated) {
    preferencesMigrated = true;
    const patch: Partial<AppSettings> = {};
    if (!settings.theme) patch.theme = getThemePreference();
    if (!settings.locale) patch.locale = getLocalePreference();
    if (!settings.navLayout) patch.navLayout = getNavLayoutPreference();
    if (Object.keys(patch).length) void migrateSharedPreferences(patch);
  }
}
async function migrateSharedPreferences(patch: Partial<AppSettings>) {
  const revision = stateRevision, settingsRevision = settingsWriteRevision;
  try {
    const next = await api.saveSettingsPatch(patch, true);
    if (revision === stateRevision && settingsRevision === settingsWriteRevision && !settingsDraft && !hasPendingMutation() && !settingsAutosave.busy && !selectionAutosave.busy) accept(next);
  } catch (err) {
    toast(t("设置保存失败：{0}", { 0: tMessage(err instanceof Error ? err.message : String(err)) }), true);
  }
}
async function pollCloudSync() {
  if (!data || cloudPollBusy || hasPendingMutation() || settingsAutosave.busy || selectionAutosave.busy) return;
  cloudPollBusy = true;
  const revision = stateRevision, settingsRevision = settingsWriteRevision;
  try {
    const next = await api.cloudSyncStatus();
    if (!data || revision !== stateRevision || settingsRevision !== settingsWriteRevision || hasPendingMutation() || settingsAutosave.busy || selectionAutosave.busy) return;
    const changed = JSON.stringify(next) !== JSON.stringify(data.cloudSync);
    data.cloudSync = next;
    if (!next.syncing && next.lastSyncAt !== loadedCloudSyncAt && !settingsDraft) {
      const state = await api.state();
      if (revision === stateRevision && settingsRevision === settingsWriteRevision && !hasPendingMutation() && !settingsAutosave.busy && !selectionAutosave.busy) accept(state);
    } else if (changed && currentPage === "settings" && settingsBackups) {
      const section = app.querySelector(".cloud-sync-section");
      if (section) section.outerHTML = cloudSyncSection(next);
      syncBusyControls();
    }
  } catch { /* A transient poll failure leaves the usable local workspace intact. */ }
  finally { cloudPollBusy = false; }
}
async function mutate(label: string, action: () => Promise<AppState>, message: string) {
  const next = await task(label, action);
  if (next) { accept(next); toast(message); return true; }
  return false;
}
function empty(title: string, description: string, action = "", symbol = "box") {
  return `<div class="empty"><span class="empty-icon">${icon(symbol)}</span><h3>${e(title)}</h3>${description ? `<p>${e(description)}</p>` : ""}${action}</div>`;
}
function button(label: string, action: string, cls = "", attrs = "", symbol = "") {
  return `<button type="button" class="button ${cls}" data-action="${action}" ${attrs}>${symbol ? icon(symbol) : ""}${e(label)}</button>`;
}
function iconButton(label: string, action: string, symbol: string, attrs = "", cls = "") {
  return `<button type="button" class="icon-button ${cls}" data-action="${action}" title="${e(label)}" aria-label="${e(label)}" ${attrs}>${icon(symbol)}</button>`;
}
function heading(title: string, description: string, actions = "") {
  return `<header class="page-heading"><div><h2 class="page-section-title">${e(title)}</h2>${description ? `<p class="page-description">${e(description)}</p>` : ""}</div><div class="heading-actions">${actions}</div></header>`;
}
function searchShortcut() {
  return `<kbd class="search-key" aria-hidden="true">${navigator.platform.includes("Mac") ? "⌘" : "Ctrl"} K</kbd>`;
}
function pageSearch(page: keyof typeof pageQueries, placeholder: string) {
  return `<label class="search-field toolbar-search">${icon("search")}<input data-page-search="${page}" aria-label="${e(placeholder)}" placeholder="${e(placeholder)}" value="${e(pageQueries[page])}">${searchShortcut()}</label>`;
}
function pendingApplicationCount(page: Page) {
  return page === "projects" ? projects().filter((project) => project.pending).length : page === "global" ? uniqueApplications(globalApplications().filter((application) => application.target?.pending)).length : 0;
}
function navigationGroup(page: Page) {
  return page === "global" || page === "projects" ? t("应用部署") : page === "settings" || page === "guide" ? t("工作空间") : t("技能管理");
}
function navigationSubitems(page: Page) {
  if (page !== currentPage) return "";
  const subitem = (label: string, action: string, active: boolean, attrs = "", count?: number) => `<button type="button" class="nav-sub-item ${active ? "active cur" : ""}" data-action="${action}" ${attrs} aria-pressed="${active}"><span>${e(label)}</span>${count === undefined ? "" : `<em>${count}</em>`}</button>`;
  if (page === "discover") return `<div class="nav-sub" data-nav-section="discover">${subitem("Skill Hub", "discover-tab", discoverTab === "hub", 'data-tab="hub"')}${subitem(t("Git 仓库"), "discover-tab", discoverTab === "git", 'data-tab="git"')}${subitem(t("本地目录"), "discover-tab", discoverTab === "local", 'data-tab="local"')}</div>`;
  if (page === "settings") return `<div class="nav-sub settings-nav-sub" data-nav-section="settings">${subitem(t("设置"), "close-backups", !settingsBackups)}${subitem(t("Backup and migration"), "open-backups", settingsBackups)}</div>`;
  if (page === "guide") return `<div class="nav-sub guide-nav-sub" data-nav-section="guide">${guideTabs.map((tab) => subitem(t(tab.label), "guide-tab", guideTab === tab.id, `data-tab="${tab.id}"`)).join("")}</div>`;
  return "";
}
function navigationItem(item: typeof nav[number]) {
  const count = pendingApplicationCount(item.id);
  const countLabel = count ? t(item.id === "projects" ? "{count} 个项目待应用" : "{count} 个全局配置待应用", { count }) : "";
  return `<button class="nav-item ${currentPage === item.id ? "active on" : ""}" type="button" data-page="${item.id}" ${currentPage === item.id ? 'aria-current="page"' : ""}>${icon(item.icon)}<span class="nav-label">${e(t(item.title))}</span>${count ? `<span class="nav-count" title="${e(countLabel)}">${count}</span>` : ""}</button>${navigationSubitems(item.id)}`;
}
function navigation() {
  return `<nav class="main-nav seg" aria-label="${e(t("主导航"))}"><span class="nav-group-label" data-nav-section="skills-group">${e(t("技能管理"))}</span>${nav.filter((item) => ["discover", "sources", "skills", "presets"].includes(item.id)).map(navigationItem).join("")}<span class="nav-group-label" data-nav-section="applications-group">${e(t("应用部署"))}</span>${nav.filter((item) => ["global", "projects"].includes(item.id)).map(navigationItem).join("")}</nav>`;
}
function utilityNavigation() {
  return `<div class="utility-navigation">${nav.filter((item) => item.id === "guide" || item.id === "settings").map((item) => `<button type="button" class="icon-button header-settings utility-nav ${currentPage === item.id ? "active on" : ""}" data-page="${item.id}" title="${e(t(item.title))}" aria-label="${e(t(item.title))}" ${currentPage === item.id ? 'aria-current="page"' : ""}>${icon(item.icon)}<span class="nav-label">${e(t(item.title))}</span></button>${navigationSubitems(item.id)}`).join("")}</div>`;
}
function navigationCrumb() {
  const current = nav.find((item) => item.id === currentPage)!;
  const detail = currentPage === "guide" ? t(guideTabs.find((tab) => tab.id === guideTab)!.label) : currentPage === "discover" ? discoverTab === "hub" ? "Skill Hub" : t(discoverTab === "local" ? "本地目录" : "Git 仓库") : currentPage === "settings" && settingsBackups ? t("Backup and migration") : "";
  return `<span class="crumb-group">${e(navigationGroup(currentPage))}</span><span class="crumb-separator" aria-hidden="true">/</span><span class="crumb-current">${e(t(current.title))}</span>${detail ? `<span class="crumb-separator" aria-hidden="true">/</span><span class="crumb-current">${e(detail)}</span>` : ""}`;
}
function syncNavigationLayout(event?: Event) {
  const sidebar = app.querySelector<HTMLElement>(".sidebar");
  const mainNav = app.querySelector<HTMLElement>(".main-nav");
  const brand = app.querySelector<HTMLElement>(".brand");
  const utilities = app.querySelector<HTMLElement>(".utility-navigation");
  if (!sidebar || !mainNav || !brand || !utilities) return;
  const previousFocus = (event as CustomEvent<{ previousFocus?: Element }> | undefined)?.detail?.previousFocus;
  const focus = previousFocus || document.activeElement;
  const focused = focus instanceof HTMLElement && app.contains(focus) ? focus : null;
  const side = getNavLayout() === "side";
  sidebar.hidden = !side;
  for (const [element, selector] of [[brand, side ? ".side-top" : ".header-leading"], [mainNav, side ? ".side-scroll" : ".header-nav"], [utilities, side ? ".side-foot" : ".header-actions"]] as const) {
    const destination = app.querySelector(selector)!;
    if (element.parentElement !== destination) destination.append(element);
  }
  const crumb = app.querySelector<HTMLElement>(".header-crumb")!;
  crumb.hidden = !side;
  const toggle = app.querySelector<HTMLButtonElement>('[data-action="toggle-sidebar"]')!;
  const label = side ? t("隐藏侧边栏") : t("显示侧边栏");
  const mac = /Mac/.test(navigator.platform || navigator.userAgent);
  toggle.title = `${label} (${mac ? "⌃⌘S" : "Ctrl+Shift+S"})`;
  toggle.setAttribute("aria-label", label);
  toggle.setAttribute("aria-pressed", String(side));
  toggle.setAttribute("aria-keyshortcuts", mac ? "Control+Meta+S" : "Control+Shift+S");
  for (const button of app.querySelectorAll<HTMLButtonElement>("[data-nav-layout-preference]")) button.setAttribute("aria-pressed", String(button.dataset.navLayoutPreference === getNavLayoutPreference()));
  syncSidebarWidth();
  if (focused?.isConnected) {
    const visibleFocus = focused.getClientRects().length ? focused : toggle;
    if (document.activeElement !== visibleFocus) visibleFocus.focus({ preventScroll: true });
  }
}
let navTransition: { skipTransition(): void; finished: Promise<void> } | undefined;
function changeNavigationLayout(change: () => void) {
  navTransition?.skipTransition();
  if (!document.startViewTransition || matchMedia("(prefers-reduced-motion: reduce)").matches) { change(); return; }
  document.documentElement.classList.add("nav-morph");
  const transition = document.startViewTransition(change);
  navTransition = transition;
  void transition.finished.catch(() => {}).finally(() => {
    if (navTransition === transition) { navTransition = undefined; document.documentElement.classList.remove("nav-morph"); }
  });
}
function settingsTabs() {
  return `<div class="tabs settings-tabs" role="tablist" aria-label="${e(t("设置"))}"><button type="button" id="settings-tab-general" class="tab-button ${!settingsBackups ? "active" : ""}" data-action="close-backups" data-tab="settings" role="tab" aria-controls="settings-tab-panel" aria-selected="${!settingsBackups}" tabindex="${!settingsBackups ? 0 : -1}">${e(t("设置"))}</button><button type="button" id="settings-tab-backups" class="tab-button ${settingsBackups ? "active" : ""}" data-action="open-backups" data-tab="backups" role="tab" aria-controls="settings-tab-panel" aria-selected="${settingsBackups}" tabindex="${settingsBackups ? 0 : -1}">${e(t("Backup and migration"))}</button></div>`;
}
function renderSettingsWorkspace(content: HTMLElement) {
  const tabs = content.querySelector(".settings-tabs");
  if (tabs) syncNavigationMarkup(tabs, settingsTabs());
  else content.innerHTML = `${settingsTabs()}<div id="settings-tab-panel" class="settings-tab-panel" role="tabpanel" tabindex="0"></div>`;
  const panel = content.querySelector<HTMLElement>("#settings-tab-panel")!;
  panel.setAttribute("aria-labelledby", settingsBackups ? "settings-tab-backups" : "settings-tab-general");
  panel.innerHTML = settingsBackups ? backupsPage() : settingsPage(settingsDraft || data!.state.settings, globalApplications(), data!.homeDir, data!.desktopPreferences);
}
function guideTabNavigation() {
  return `<div class="tabs guide-tabs" role="tablist" aria-label="${e(t("指南"))}">${guideTabs.map((tab) => `<button type="button" id="guide-tab-${tab.id}" class="tab-button ${guideTab === tab.id ? "active" : ""}" data-action="guide-tab" data-tab="${tab.id}" role="tab" aria-controls="guide-tab-panel" aria-selected="${guideTab === tab.id}" tabindex="${guideTab === tab.id ? 0 : -1}">${e(t(tab.label))}</button>`).join("")}</div>`;
}
function renderGuideWorkspace(content: HTMLElement) {
  const tabs = content.querySelector(".guide-tabs");
  if (tabs) syncNavigationMarkup(tabs, guideTabNavigation());
  else content.innerHTML = `${guideTabNavigation()}<div id="guide-tab-panel" class="guide-tab-panel" role="tabpanel" tabindex="0"></div>`;
  const panel = content.querySelector<HTMLElement>("#guide-tab-panel")!;
  panel.setAttribute("aria-labelledby", `guide-tab-${guideTab}`);
  panel.innerHTML = guidePage(guideTab);
}
function render() {
  const current = nav.find((item) => item.id === currentPage)!;
  const view = currentPage === "guide" ? `guide-${guideTab}` : currentPage === "settings" && settingsBackups ? "backups" : currentPage;
  const oldContent = document.querySelector<HTMLElement>(".workspace");
  const scrollTop = oldContent?.dataset.view === view ? oldContent.scrollTop : 0;
  const focused = document.activeElement instanceof HTMLInputElement && document.activeElement.dataset.pageSearch && oldContent?.dataset.page === currentPage ? { page: document.activeElement.dataset.pageSearch, start: document.activeElement.selectionStart, end: document.activeElement.selectionEnd } : null;
  const activeElement = document.activeElement;
  const scrollFocus = activeElement instanceof HTMLElement && oldContent?.dataset.view === view ? activeElement.dataset.preserveScroll : undefined;
  const selectionFocus = activeElement instanceof HTMLInputElement && activeElement.form?.classList.contains("application-selection") && activeElement.type === "checkbox" ? { scope: activeElement.form.dataset.scope, id: activeElement.form.dataset.id, name: activeElement.name, value: activeElement.value } : null;
  const disclosureFocus = activeElement instanceof HTMLElement && activeElement.tagName === "SUMMARY" && activeElement.parentElement?.dataset.projectDisclosure ? { id: activeElement.parentElement.dataset.id, section: activeElement.parentElement.dataset.projectDisclosure } : null;
  const projectSearchFocus = activeElement instanceof HTMLInputElement && activeElement.dataset.projectSkillSearch ? { id: activeElement.dataset.projectSkillSearch, start: activeElement.selectionStart, end: activeElement.selectionEnd } : null;
  const pageSearchFocus = activeElement instanceof HTMLInputElement && activeElement.dataset.pageSearch && oldContent?.dataset.page === currentPage ? { page: activeElement.dataset.pageSearch, start: activeElement.selectionStart, end: activeElement.selectionEnd } : null;
  for (const element of app.querySelectorAll<HTMLElement>("[data-preserve-scroll]")) preservedScroll.set(element.dataset.preserveScroll!, element.scrollTop);
  // The native toggle event is queued; read disclosures before a search or
  // autosave render can replace them and discard their latest open state.
  for (const details of app.querySelectorAll<HTMLDetailsElement>("details[data-project-disclosure]")) {
    const open = projectDetailsOpen.get(details.dataset.id!) || new Set<string>();
    if (details.open) open.add(details.dataset.projectDisclosure!); else open.delete(details.dataset.projectDisclosure!);
    projectDetailsOpen.set(details.dataset.id!, open);
  }
  const settingsFocus = (activeElement instanceof HTMLInputElement || activeElement instanceof HTMLSelectElement) && activeElement.closest(".settings-page") && oldContent?.dataset.page === currentPage ? { name: activeElement.name, value: activeElement.value } : null;
  const selectFocus = activeElement instanceof HTMLButtonElement ? activeElement.dataset.selectTrigger : undefined;
  const activeButton = app.contains(activeElement) && activeElement instanceof HTMLButtonElement ? { dataset: Object.fromEntries(["page", "action", "id", "scope", "tab", "filter", "themePreference", "navLayoutPreference", "size", "selectTrigger"].map((key) => [key, activeElement.dataset[key]])), form: activeElement.form?.id, type: activeElement.type } : null;
  if (!oldContent) app.innerHTML = `<aside class="sidebar" id="racomi-sidebar" aria-label="${e(t("侧边栏"))}" ${getNavLayout() === "side" ? "" : "hidden"}><div class="side-top" style="--wails-draggable: drag"></div><div class="side-scroll"></div><div class="side-foot"></div><div class="side-edge" data-side-edge role="separator" tabindex="0" aria-orientation="vertical" aria-controls="racomi-sidebar" aria-label="${e(t("调整侧边栏宽度"))}" title="${e(t("调整侧边栏宽度"))}"></div></aside><header class="app-header top"><div class="header-leading"><button type="button" class="icon-button side-toggle" data-action="toggle-sidebar" aria-controls="racomi-sidebar">${icon("layout-side")}</button><div class="brand"><span class="brand-mark"><img src="/appicon.png" alt="" width="20" height="20" draggable="false"></span><strong>racomi</strong></div></div><h1 class="sr-only">${e(t(current.title))}</h1><div class="header-nav">${navigation()}</div><div class="header-crumb">${navigationCrumb()}</div><div class="header-actions actions">${utilityNavigation()}</div></header><main class="workspace" data-page="${currentPage}" data-view="${view}"><div class="page-content" id="page-content" data-page="${currentPage}"></div></main><footer class="statusbar"><span id="activity" aria-live="polite"></span><div class="statusbar-meta"><span>${skills().length}${t(" 个技能 · ")}${presets().length}${t(" 个预设 · ")}${projects().length}${t(" 个项目")}</span></div></footer>`;
  syncNavigationMarkup(app.querySelector(".main-nav")!, navigation());
  syncNavigationMarkup(app.querySelector(".utility-navigation")!, utilityNavigation());
  app.querySelector(".app-header h1")!.textContent = t(current.title);
  app.querySelector(".header-crumb")!.innerHTML = navigationCrumb();
  app.querySelector(".sidebar")!.setAttribute("aria-label", t("侧边栏"));
  const edge = app.querySelector<HTMLElement>("[data-side-edge]")!;
  edge.title = t("调整侧边栏宽度");
  edge.setAttribute("aria-label", edge.title);
  const workspace = app.querySelector<HTMLElement>(".workspace")!;
  workspace.dataset.page = currentPage;
  workspace.dataset.view = view;
  const content = app.querySelector<HTMLElement>("#page-content")!;
  content.dataset.page = currentPage;
  if (currentPage === "guide") renderGuideWorkspace(content);
  else if (currentPage === "settings" && data) renderSettingsWorkspace(content);
  else content.innerHTML = !data ? loadingPage() : currentPage === "skills" ? skillsPage() : currentPage === "sources" ? sourcesPage() : currentPage === "discover" ? discoverPage() : currentPage === "presets" ? presetsPage() : currentPage === "projects" ? projectsPage() : currentPage === "global" ? globalPage() : "";
  app.querySelector(".statusbar-meta > span")!.textContent = `${skills().length}${t(" 个技能 · ")}${presets().length}${t(" 个预设 · ")}${projects().length}${t(" 个项目")}`;
  const selectAll = app.querySelector<HTMLInputElement>('[name="bulk-skills-all"]');
  if (selectAll) selectAll.indeterminate = !selectAll.checked && filteredSkills().some((skill) => bulkSkillSelection.has(skill.id));
  syncNavigationLayout();
  syncWorkspaceResize(t("调整两栏宽度"));
  document.querySelector<HTMLElement>(".workspace")!.scrollTop = scrollTop;
  for (const element of app.querySelectorAll<HTMLElement>("[data-preserve-scroll]")) element.scrollTop = preservedScroll.get(element.dataset.preserveScroll!) || 0;
  if (scrollFocus) [...app.querySelectorAll<HTMLElement>("[data-preserve-scroll]")].find((element) => element.dataset.preserveScroll === scrollFocus)?.focus({ preventScroll: true });
  if (selectionFocus) [...app.querySelectorAll<HTMLInputElement>(".application-selection input")].find((input) => input.form?.dataset.scope === selectionFocus.scope && input.form?.dataset.id === selectionFocus.id && input.name === selectionFocus.name && input.value === selectionFocus.value)?.focus({ preventScroll: true });
  if (disclosureFocus) [...app.querySelectorAll<HTMLElement>("details[data-project-disclosure]")].find((details) => details.dataset.id === disclosureFocus.id && details.dataset.projectDisclosure === disclosureFocus.section)?.querySelector("summary")?.focus({ preventScroll: true });
  if (focused) {
    const input = document.querySelector<HTMLInputElement>(`input[data-page-search="${focused.page}"]`);
    input?.focus({ preventScroll: true });
    if (focused.start !== null && focused.end !== null) input?.setSelectionRange(focused.start, focused.end);
  }
  if (projectSearchFocus) {
    const input = [...app.querySelectorAll<HTMLInputElement>("[data-project-skill-search]")].find((item) => item.dataset.projectSkillSearch === projectSearchFocus.id);
    input?.focus({ preventScroll: true });
    if (projectSearchFocus.start !== null && projectSearchFocus.end !== null) input?.setSelectionRange(projectSearchFocus.start, projectSearchFocus.end);
  }
  status(); syncBusyControls();
  if (pageSearchFocus && !modalRoot.children.length) {
    const input = [...app.querySelectorAll<HTMLInputElement>("input[data-page-search]")].find((input) => input.dataset.pageSearch === pageSearchFocus.page);
    input?.focus({ preventScroll: true });
    if (pageSearchFocus.start !== null && pageSearchFocus.end !== null) input?.setSelectionRange(pageSearchFocus.start, pageSearchFocus.end);
  }
  if (settingsFocus && !modalRoot.children.length) {
    [...app.querySelectorAll<HTMLInputElement | HTMLSelectElement>('.settings-page input,.settings-page select')].find((input) => input.name === settingsFocus.name && input.value === settingsFocus.value)?.focus({ preventScroll: true });
  }
  if (selectFocus) queueMicrotask(() => app.querySelector<HTMLButtonElement>(`[data-select-trigger="${CSS.escape(selectFocus)}"]`)?.focus({ preventScroll: true }));
  if (activeButton && !focused && !modalRoot.children.length) {
    const replacement = [...app.querySelectorAll<HTMLButtonElement>("button")].find((button) => button.getClientRects().length && button.form?.id === activeButton.form && button.type === activeButton.type && Object.entries(activeButton.dataset).every(([key, value]) => button.dataset[key] === value));
    replacement?.focus({ preventScroll: true });
  }
}
function loadingPage() {
  if (loadError) return `${heading(t("连接工作空间"), "")}${empty(t("本地服务未连接"), t("本地服务未连接。{0}", { 0: tMessage(loadError) }), button(t("重新连接"), "reload", "primary", "", "refresh"), "info")}`;
  return `${heading(t("技能"), "")}${empty(t("正在读取本地技能"), "", '<span class="spinner"></span>')}`;
}
function pendingSkillIDs() {
  return pendingSkills(skills(), presets(), managedApplications(), data?.state.settings.deploymentMode || "copy");
}
function updatedSkillIDs() {
  return new Set(arr(updates).filter((update) => update.available && !update.removed && !update.error).map((update) => update.skillId));
}
function filteredSkills(filters: SkillFilters = { query: skillsQuery, source: skillsSourceFilter, status: skillsFilter, sort: skillsSort }) {
  return filterSkills(skills(), sources(), filters, updatedSkillIDs(), pendingSkillIDs(), getLocale());
}
function sourceFilter(name: "skills-source" | "preset-source", selected: string) {
  return renderSourceFilter({ name, selected, label: t("按来源筛选技能"), searchLabel: t("输入来源名称或仓库过滤"), emptyLabel: t("没有匹配的来源"), locale: getLocale(), options: [
    { id: "all", name: t("全部来源") }, { id: "linked", name: t("有来源") }, { id: "local", name: t("本地 · 无来源") },
    ...sources().map((source) => ({ id: source.id, name: sourceDisplayName(source), description: source.repository })),
  ] });
}
function originLabel(skill: Skill) {
  const source = skillSource(skill, sources());
  return source ? sourceDisplayName(source) : t("本地 · 无来源");
}
function skillsPage() {
  const list = filteredSkills();
  if (!list.some((skill) => skill.id === selectedSkill)) selectedSkill = list[0]?.id || "";
  const selected = skills().find((skill) => skill.id === selectedSkill);
  const updateCount = updates?.filter((update) => skills().some((skill) => skill.id === update.skillId) && update.available && !update.removed && !update.error).length || 0;
  const pending = pendingSkillIDs();
  return `${heading(t("技能"), "", button(t("检查更新"), "check-updates", "", "", "refresh") + button(t("导入技能"), "go-discover", "primary", "", "plus"))}<div class="skills-controls"><label class="sort-control"><span>${t("状态")}</span><select class="sort-select" name="skills-status" aria-label="${t("技能状态筛选")}">${([["all", t("全部"), skills().length], ["updates", t("有更新"), updateCount], ["pending", t("待应用"), pending.size]] as const).map(([id, label, count]) => `<option value="${id}" ${skillsFilter === id ? "selected" : ""}>${label} · ${count}</option>`).join("")}</select></label><div class="sort-control source-filter-control"><span>${t("来源")}</span>${sourceFilter("skills-source", skillsSourceFilter)}</div><label class="sort-control"><span>${t("排序")}</span><select class="sort-select" name="skills-sort" aria-label="${t("技能排序")}"><option value="asc" ${skillsSort === "asc" ? "selected" : ""}>${t("名称 A → Z")}</option><option value="desc" ${skillsSort === "desc" ? "selected" : ""}>${t("名称 Z → A")}</option></select></label></div><div class="skills-layout"><section class="panel skill-browser" data-preserve-scroll="skills-browser">${skillsSelectionToolbar()}<div id="skill-list" data-preserve-scroll="skills-list" tabindex="0" role="region" aria-label="${t("技能")}">${skillList()}</div></section><aside class="panel skill-detail" aria-label="${t("技能详情")}">${selected ? skillDetail(selected) : `<div class="skill-overview" tabindex="0" role="region" aria-label="${t("技能详情")}">${!skills().length ? empty(t("开始管理你的技能"), "", button(t("发现技能"), "go-discover", "primary", "", "plus")) : empty(t("没有匹配的技能"), skillsFilter === "updates" && updates === null ? t("先检查更新，再查看可更新的技能。") : t("试试其他筛选条件或关键词。"))}</div>`}</aside></div>`;
}
function skillsSelectionToolbar() {
  const list = filteredSkills();
  const allSelected = list.length > 0 && list.every((skill) => bulkSkillSelection.has(skill.id));
  const updateIDs = updatedSkillIDs();
  const updateCount = [...bulkSkillSelection].filter((id) => updateIDs.has(id)).length;
  return `<div class="panel-toolbar skills-list-toolbar"><label class="skills-select-all" title="${e(t(allSelected ? "取消全选当前筛选结果" : "全选当前筛选结果"))}"><input type="checkbox" name="bulk-skills-all" aria-label="${e(t("全选当前筛选结果"))}" aria-controls="skill-list" ${allSelected ? "checked" : ""} ${list.length ? "" : "disabled"}></label><label class="search-field">${icon("search")}<input id="skills-search" data-page-search="skills" aria-label="${t("搜索已安装技能")}" placeholder="${t("搜索名称、描述或来源")}" value="${e(skillsQuery)}">${searchShortcut()}</label>${bulkSkillSelection.size ? `<div class="skills-bulk-actions"><span id="skills-selection-count" aria-live="polite">${t("已选 {0}", { 0: bulkSkillSelection.size })}</span>${updateCount ? iconButton(t("更新所选（{0}）", { 0: updateCount }), "update-selected-skills", "download") : ""}${iconButton(t("移除所选"), "remove-selected-skills", "trash", "", "danger-quiet")}${iconButton(t("取消选择"), "clear-selected-skills", "close")}</div>` : `<span class="muted" id="skills-count">${list.length}${t(" 个")}</span>`}</div>`;
}
function skillList() {
  const list = filteredSkills();
  const pending = pendingSkillIDs();
  return list.length ? list.map((skill) => {
    const origin = originLabel(skill);
    const current = selectedSkill === skill.id;
    const checked = bulkSkillSelection.has(skill.id);
    const removed = skill.upstreamRemoved || updates?.some((update) => update.skillId === skill.id && update.removed);
    const hasUpdate = updates?.some((update) => update.skillId === skill.id && update.available && !update.removed && !update.error);
    const badges = `${removed ? `<span class="badge amber">${t("上游移除")}</span>` : ""}${hasUpdate ? `<span class="badge amber">${t("有更新")}</span>` : ""}${pending.has(skill.id) ? `<span class="badge">${t("待应用")}</span>` : ""}`;
    return `<div class="skill-list-item ${current ? "current" : ""} ${checked ? "checked" : ""}"><label class="skill-row-selector"><input type="checkbox" name="bulk-skill" value="${e(skill.id)}" aria-label="${e(t("选择技能 {0}", { 0: skill.name }))}" ${checked ? "checked" : ""}></label><button type="button" class="skill-row" data-action="select-skill" data-id="${e(skill.id)}" ${current ? 'aria-current="true"' : ""}><span class="skill-symbol">${icon("box")}</span><strong class="skill-name" title="${e(skill.name)}">${e(skill.name)}</strong><span class="skill-row-origin" title="${e(origin)}">${e(origin)}</span><span class="row-badges">${badges || `<span class="badge">${t("已安装")}</span>`}</span></button></div>`;
  }).join("") : `<div class="small-empty">${!skills().length ? t("尚未安装技能") : t("没有匹配的技能")}</div>`;
}
function skillDetail(skill: Skill) {
  const update = updates?.find((update) => update.skillId === skill.id);
  const removed = skill.upstreamRemoved || update?.removed;
  const source = skillSource(skill, sources());
  const hosted = !!hostedSourceDomain(skill.source);
  return `<div class="detail-header"><div class="detail-title"><span class="detail-symbol">${icon("box")}</span><h2>${e(skill.name)}</h2></div><span class="badge ${removed ? "amber" : "green"}">${removed ? t("上游移除") : t("已安装")}</span></div><div class="detail-tabs tabs" role="tablist" aria-label="${t("技能详情")}"><button type="button" class="tab-button ${detailTab === "overview" ? "active" : ""}" data-action="detail-tab" data-tab="overview" role="tab" aria-selected="${detailTab === "overview"}">${t("概览")}</button><button type="button" class="tab-button ${detailTab === "document" ? "active" : ""}" data-action="detail-tab" data-tab="document" role="tab" aria-selected="${detailTab === "document"}">SKILL.md</button></div>${detailTab === "document" ? skillDocument(skill) : `<div class="skill-overview" role="tabpanel" aria-label="${t("概览")}" tabindex="0" data-preserve-scroll="skill-overview:${e(skill.id)}">${skill.description ? `<p class="detail-description">${e(skill.description)}</p>` : ""}<dl class="metadata">${source ? `<dt>${t("来源")}</dt><dd>${button(sourceDisplayName(source), "open-source", "quiet small source-link", `data-id="${e(source.id)}"`)}</dd>` : `<dt>${t("来源")}</dt><dd>${t("本地 · 无来源")}</dd>`}<dt>${t(hosted ? "来源地址" : "仓库")}</dt><dd class="mono">${e(skill.source || "—")}</dd><dt>${t("技能路径")}</dt><dd class="mono">${e(skill.subpath || ".")}</dd>${hosted ? "" : `<dt>${t("Git 引用")}</dt><dd class="mono">${e(skill.ref || t("默认分支"))}</dd>`}<dt>${t("当前版本")}</dt><dd class="mono" title="${e(skill.revision)}">${e(short(skill.revision))}</dd><dt>${t("安装时间")}</dt><dd>${e(date(skill.installedAt))}</dd><dt>${t("更新时间")}</dt><dd>${e(date(skill.updatedAt))}</dd></dl>${removed ? `<div class="info-note source-retained"><strong>${t("上游已移除此技能")}</strong><p>${t("本地版本与预设引用已保留，可继续使用。如技能被移动或改名，可到来源重新关联。")}</p></div>` : update ? `<div class="update-notice ${update.error ? "error-notice" : ""}"><strong>${update.error ? t("检查失败") : update.available ? t("发现新版本") : t("已是最新版本")}</strong>${update.error || update.available ? `<p>${e(tMessage(update.error || update.summary) || t("新版本 {0}", { 0: short(update.revision) }))}</p>` : ""}${update.available && !update.removed && !update.error ? button(t("更新技能"), "update-skill", "primary small", `data-id="${e(skill.id)}"`, "download") : ""}</div>` : ""}</div>`}<div class="detail-footer">${button(t("移除技能"), "remove-skill", "danger-quiet small", `data-id="${e(skill.id)}"`, "trash")}</div>`;
}
function skillDocument(skill: Skill) {
  const cached = documents.get(skill.id);
  const doc = cached?.hash === skill.contentHash ? cached : undefined;
  const failure = documentErrors.get(skill.id);
  const error = failure?.hash === skill.contentHash ? failure.message : "";
  const loading = loadingDocuments.has(skill.id);
  const raw = rawDocuments.has(skill.id);
  let content: string;
  if (doc) {
    if (!raw && (doc.rendered?.locale !== getLocale() || doc.rendered?.wrap !== metadataWrap)) doc.rendered = { locale: getLocale(), wrap: metadataWrap, html: renderMarkdownDocument(doc.text, { frontmatterLabel: t("元信息"), frontmatterWrap: { label: t("软换行"), enabled: metadataWrap } }) };
    content = raw ? `<pre class="document-raw"><code>${e(doc.text)}</code></pre>` : `<div class="markdown-body">${doc.rendered!.html}</div>`;
  } else if (loading) {
    content = `<div class="small-empty" role="status"><span class="spinner" aria-hidden="true"></span>${t(" 正在读取 SKILL.md")}</div>`;
  } else if (error) {
    content = `<div class="document-error" role="alert"><strong>${t("文档读取失败")}</strong><p>${e(tMessage(error))}</p>${button(t("重试读取"), "read-document", "small", `data-id="${e(skill.id)}"`, "refresh")}</div>`;
  } else {
    content = `<div class="small-empty">${button(t("读取 SKILL.md"), "read-document", "small", `data-id="${e(skill.id)}"`, "refresh")}</div>`;
  }
  return `<div class="document-panel" role="tabpanel" aria-label="SKILL.md" aria-busy="${loading}"><div class="document-toolbar"><span>SKILL.md</span><button type="button" class="document-toggle" data-action="toggle-document-raw" data-id="${e(skill.id)}" role="switch" aria-label="${t("显示原文")}" aria-checked="${raw}" ${doc ? "" : "disabled"}><span>${t("原文")}</span><span class="document-switch" aria-hidden="true"></span></button></div><div class="skill-document" tabindex="0" role="region" aria-label="SKILL.md" data-preserve-scroll="skill-document:${e(skill.id)}:${e(skill.contentHash)}:${raw ? "raw" : "rendered"}">${content}</div></div>`;
}
async function loadSkillDocument(id: string) {
  const skill = skills().find((skill) => skill.id === id);
  if (!skill || loadingDocuments.has(id) || documents.get(id)?.hash === skill.contentHash) return;
  loadingDocuments.add(id);
  documentErrors.delete(id);
  if (currentPage === "skills" && selectedSkill === id) render();
  let error = "";
  const text = await task({ key: "正在读取 {0} 的 SKILL.md…", variables: { 0: skill.name, skillId: id, hash: skill.contentHash } }, async () => {
    try { return await api.readSkillDocument(id); }
    catch (err) { error = err instanceof Error ? err.message : String(err); return undefined; }
  });
  loadingDocuments.delete(id);
  const current = skills().find((item) => item.id === id);
  if (current?.contentHash === skill.contentHash) {
    if (text !== undefined) documents.set(id, { hash: skill.contentHash, text });
    else if (error) documentErrors.set(id, { hash: skill.contentHash, message: error });
  }
  if (currentPage === "skills" && selectedSkill === id) {
    render();
    // A sync may replace the skill while its previous document is in flight.
    // Discard that result and load the version now shown in the inspector.
    if (detailTab === "document" && current && current.contentHash !== skill.contentHash) void loadSkillDocument(id);
  }
}
function sourceEntries(source: Source): SourceEntry[] {
  return catalogEntries(source, skills(), getLocale());
}
function sourceSelection(id: string) {
  if (!sourceSelections.has(id)) sourceSelections.set(id, new Set());
  return sourceSelections.get(id)!;
}
function openSource(id: string) {
  selectedSource = id;
  pageQueries.sources = "";
  sourceCatalogFilter = "all";
  currentPage = "sources";
  render();
}
function sourceSummary(source: Source) {
  return summarizeSource(sourceEntries(source));
}
function scanWarnings(warnings: string[] | undefined) {
  return warnings?.length ? `<details class="source-warnings"><summary>${t("扫描提示 {0}", { 0: warnings.length })}</summary><ul>${warnings.map((warning) => `<li>${e(tMessage(warning))}</li>`).join("")}</ul></details>` : "";
}
function sourcesPage() {
  const query = pageQueries.sources.toLowerCase();
  const list = sources().filter((source) => `${source.name} ${source.repository} ${source.ref} ${source.subpath}`.toLowerCase().includes(query));
  if (!list.some((source) => source.id === selectedSource)) selectedSource = list[0]?.id || "";
  const selected = sources().find((source) => source.id === selectedSource);
  return `${heading(t("来源"), "", button(t("添加来源"), "new-source", "primary", "", "plus"))}${!sources().length ? empty(t("保存你的技能来源"), "", button(t("添加来源"), "new-source", "primary", "", "plus"), "refresh") : `<div class="sources-layout"><section class="panel sources-browser" data-preserve-scroll="sources-browser"><div class="panel-toolbar">${pageSearch("sources", t("搜索来源"))}<span class="muted">${list.length}${t(" 个")}</span></div><div class="source-list" data-preserve-scroll="sources-list" tabindex="0" role="region" aria-label="${t("来源")}">${list.map((source) => {
    const entries = sourceEntries(source);
    const state = sourceStatus(source, entries);
    const status = t(state.label, { 0: state.count });
    const tone = state.tone;
    const description = [t("安装方式：{0}", { 0: t(source.mode === "all" ? "全部订阅" : "挑选安装") }), ...sourceStatusDetails(source, entries).map((detail) => t(detail.key, { 0: detail.count ?? 0 })), ...(source.lastError ? [tMessage(source.lastError)] : [])].join("\n");
    const synced = status === t("已同步");
    const symbol = synced ? icon("check") : tone ? icon("alert") : "";
    return `<button type="button" class="source-row ${selectedSource === source.id ? "selected" : ""}" data-action="select-source" data-id="${e(source.id)}" aria-pressed="${selectedSource === source.id}" aria-describedby="source-status-${e(source.id)}" title="${e(`${sourceDisplayName(source)}\n${description}`)}"><strong class="source-name">${e(sourceDisplayName(source))}</strong><span class="source-status ${tone} ${synced ? "synced" : ""}" title="${e(description)}">${symbol}${e(status)}</span><span hidden id="source-status-${e(source.id)}">${e(description)}</span></button>`;
  }).join("") || `<div class="small-empty">${t("没有匹配的来源")}</div>`}</div></section><section class="panel source-detail" data-preserve-scroll="source-detail:${e(selectedSource)}" tabindex="0" aria-label="${e(selected ? sourceDisplayName(selected) : t("来源"))}">${selected ? sourceDetail(selected) : empty(t("没有匹配的来源"), t("试试其他名称或来源地址。"))}</section></div>`}`;
}
function sourceDetail(source: Source) {
  const hosted = !!hostedSourceDomain(source.repository);
  const { entries, available, installs, changed, removed, skipped, installed } = sourceSummary(source);
  const selection = sourceSelection(source.id);
  const count = source.mode === "all" ? available.length : available.filter((entry) => selection.has(entry.item.subpath)).length;
  const visible = entries.filter((entry) => entry.kind === "skipped" || !entry.item.removed || entry.local);
  const shown = sourceCatalogFilter === "all" ? visible : visible.filter((entry) => entry.kind !== "current");
  const removedLocals = entries.filter((entry) => entry.kind === "removed" && entry.local).map((entry) => entry.local!);
  return `<div class="source-detail-heading"><div><div class="title-line"><h2>${e(sourceDisplayName(source))}</h2><span class="badge">${source.mode === "all" ? t("全部订阅") : t("挑选安装")}</span></div><p class="mono">${e(source.repository)}</p></div><div class="source-actions">${iconButton(t("编辑来源"), "edit-source", "edit", `data-id="${e(source.id)}"`)}${iconButton(t("删除来源"), "delete-source", "trash", `data-id="${e(source.id)}"`, "danger-quiet")}</div></div><dl class="source-metadata">${hosted ? "" : `<dt>${t("引用")}</dt><dd class="mono">${e(source.ref || t("默认分支"))}</dd>`}<dt>${t("扫描范围")}</dt><dd class="mono">${e(source.subpath || t(hosted ? "所有技能" : "整个仓库"))}</dd><dt>${t("上次刷新")}</dt><dd>${e(date(source.refreshedAt))}</dd><dt>${t("目录版本")}</dt><dd class="mono" title="${e(source.revision)}">${e(short(source.revision))}</dd></dl>${source.lastError ? `<div class="source-error" role="status"><strong>${t("刷新失败")}</strong><p>${e(tMessage(source.lastError))}</p><small>${t("保留上次成功获取的目录和本地技能，请重新刷新后同步。")}</small></div>` : ""}${!source.lastError && skipped ? `<div class="source-partial-note" role="status">${t("已跳过 {0} 个技能；已有版本已保留，其他技能可继续同步。", { 0: skipped })}</div>` : ""}${scanWarnings(source.warnings)}<div class="source-refresh-bar"><span class="muted">${source.mode === "all" ? t("同步会安装全部技能，并更新已安装技能。") : t("选择需要安装或更新的技能；全选不会改变订阅模式。")}</span>${button(t("刷新来源"), "refresh-source", "small", `data-id="${e(source.id)}"`, "refresh")}</div>${!source.revision ? `<div class="small-empty">${t("来源已保存。刷新后即可浏览技能与同步变化。")}</div>` : `<div class="source-summary">${source.mode === "selected" ? `<span>${t("已安装 ")}<strong>${installed}</strong></span>` : ""}<span>${t("可安装 ")}<strong>${installs}</strong></span><span>${t("可更新 ")}<strong>${changed}</strong></span><span>${t("上游移除 ")}<strong>${removed}</strong></span>${skipped ? `<span>${t("本次跳过 ")}<strong>${skipped}</strong></span>` : ""}</div><div class="source-catalog-toolbar"><div class="skills-filters" role="group" aria-label="${t("来源目录筛选")}"><button type="button" class="filter-chip ${sourceCatalogFilter === "changes" ? "active" : ""}" data-action="source-filter" data-filter="changes" aria-pressed="${sourceCatalogFilter === "changes"}">${t("可安装 / 更新")}</button><button type="button" class="filter-chip ${sourceCatalogFilter === "all" ? "active" : ""}" data-action="source-filter" data-filter="all" aria-pressed="${sourceCatalogFilter === "all"}">${t("全部技能 ")}<span class="filter-count">${visible.length}</span></button></div><div class="source-catalog-actions">${source.mode === "selected" && available.length ? button(available.every((entry) => selection.has(entry.item.subpath)) ? t("取消全选") : t("全选可同步"), "select-all-source", "quiet small", `data-id="${e(source.id)}" ${source.lastError ? "disabled" : ""}`) : ""}${button(source.mode === "all" ? t("同步全部技能到本地") : t("同步所选技能到本地"), "sync-source", "primary small", `data-id="${e(source.id)}" ${!count || source.lastError ? "disabled" : ""}`, "download")}</div></div><div class="source-catalog">${shown.map((entry) => sourceCatalogRow(source, entry, removedLocals)).join("") || `<div class="small-empty">${entries.length ? t("与本地技能一致，没有待同步的变化。") : t("扫描范围内没有有效的 SKILL.md。")}</div>`}</div><div class="source-sync-footer"><div><strong id="source-sync-count">${source.mode === "all" ? t("待同步 {0} 个技能", { 0: count }) : t("已选择 {0} 个技能", { 0: count })}</strong><small>${t("上游移除的技能保留本地版本；同步后请到“项目”或“全局”手动应用。")}</small></div></div>`}`;
}
function sourceCatalogRow(source: Source, entry: SourceEntry, removedLocals: Skill[]) {
  const { item, local, kind } = entry;
  const selectable = kind === "install" || kind === "update";
  const checked = selectable && (source.mode === "all" || sourceSelection(source.id).has(item.subpath));
  const label = kind === "skipped" ? t("本次跳过") : kind === "removed" ? local ? t("上游移除 · 本地保留") : t("上游移除") : kind === "update" ? t("可更新") : kind === "current" ? t("已同步") : item.new ? t("新增 · 可安装") : t("可安装");
  return `<div class="source-catalog-row ${kind === "removed" ? "source-removed" : ""}"><label class="source-catalog-choice"><input type="checkbox" name="source-candidate" data-source="${e(source.id)}" value="${e(item.subpath)}" aria-label="${t("选择")}${e(item.name)}" ${checked ? "checked" : ""} ${!selectable || source.mode === "all" || source.lastError ? "disabled" : ""}><span><strong>${e(item.name)}</strong>${item.description ? `<small>${e(item.description)}</small>` : ""}<code>${e(item.subpath || ".")}</code>${local && local.name !== item.name ? `<small>${t("本地名称：")}${e(local.name)}</small>` : ""}${kind === "skipped" ? `<small class="source-skip-reason">${e(tMessage(item.skipReason))}</small>${local ? `<small>${t("已保留本地版本。")}</small>` : ""}` : ""}</span></label><div class="source-catalog-status"><span class="badge ${kind === "update" || kind === "removed" || kind === "skipped" ? "amber" : kind === "current" ? "green" : ""}">${label}</span>${kind === "install" && removedLocals.length ? button(t("重新关联…"), "relink-source-skill", "quiet small", `data-id="${e(source.id)}" data-subpath="${e(item.subpath)}" ${source.lastError ? "disabled" : ""}`) : ""}${(kind === "removed" || kind === "skipped") && local ? button(t("查看本地"), "view-local-skill", "quiet small", `data-id="${e(local.id)}"`) : ""}</div></div>`;
}
function sourceEditor(id = "") {
  const source = sources().find((item) => item.id === id);
  const hosted = !!hostedSourceDomain(source?.repository || "");
  modal(source ? t("编辑来源") : t("添加来源"), source ? t("修改名称和安装方式。来源地址、引用和范围如需变更，请添加新来源。") : "", `<input type="hidden" name="entityId" value="${e(source?.id)}"><label class="field"><span>${t("名称")}</span><input name="name" value="${e(source?.name)}" required maxlength="80" placeholder="${t("例如：团队技能")}"></label><label class="field"><span>${t("来源地址")}</span><input name="repository" value="${e(source?.repository)}" required ${source ? "readonly" : ""} placeholder="${t("Git 仓库地址或 skills.sh 技能链接")}"></label><div class="form-grid">${hosted ? `<input type="hidden" name="ref" value="${e(source?.ref)}">` : `<label class="field"><span>${t("分支、标签或 Commit ")}<i>${t("可选")}</i></span><input name="ref" value="${e(source?.ref)}" ${source ? "readonly" : ""} placeholder="${t("默认分支")}"></label>`}<label class="field"><span>${t("扫描子目录 ")}<i>${t("可选")}</i></span><input name="subpath" value="${e(source?.subpath)}" ${source ? "readonly" : ""} placeholder="${t(hosted ? "所有技能" : "整个仓库")}"></label></div><div class="field"><span>${t("安装方式")}</span><div class="source-mode-options"><label class="source-mode-option"><input type="radio" name="mode" value="selected" ${source?.mode !== "all" ? "checked" : ""}><span><strong>${t("挑选安装")}</strong><small>${t("新增技能只提示，手动选择安装和更新。")}</small></span></label><label class="source-mode-option"><input type="radio" name="mode" value="all" ${source?.mode === "all" ? "checked" : ""}><span><strong>${t("全部订阅")}</strong><small>${t("每次同步安装全部技能，包含后续新增技能。")}</small></span></label></div></div>`, `<button class="button primary" type="submit">${t("保存来源")}</button>`, "source-editor");
  sourceEditorBaselines.set(modalRoot.querySelector<HTMLFormElement>("#source-editor")!, source ? structuredClone(source) : null);
}
function relinkSourceEditor(source: Source, subpath: string) {
  const entry = sourceEntries(source).find((item) => item.item.subpath === subpath && item.kind === "install");
  const removed = sourceEntries(source).filter((item) => item.kind === "removed" && item.local);
  if (source.lastError || !entry || !removed.length) return;
  modal(t("重新关联技能"), t("用于上游移动路径或改名。确认后更新本地内容，并保留技能 ID 与预设引用。"), `<input type="hidden" name="sourceId" value="${e(source.id)}"><input type="hidden" name="subpath" value="${e(subpath)}"><label class="field"><span>${t("选择保留的本地技能")}</span><select name="skillId" required>${removed.map((item) => `<option value="${e(item.local!.id)}">${e(item.local!.name)} · ${e(item.local!.subpath || ".")}</option>`).join("")}</select></label><dl class="metadata"><dt>${t("新技能")}</dt><dd>${e(entry.item.name)}</dd><dt>${t("新路径")}</dt><dd class="mono">${e(subpath || ".")}</dd></dl><div class="info-note">${t("关联后，使用该技能的项目与全局配置会标记为待应用，请到相应页面应用变化。")}</div>`, `<button class="button primary" type="submit">${t("关联并更新本地技能")}</button>`, "source-relink");
}
function hubResults() {
  if (searchResults === null) return activeTasks.has("正在搜索 skills.sh…")
    ? empty(t("正在读取 skills.sh 技能…"), "", '<span class="spinner"></span>', "compass")
    : empty(t("发现适合你的技能"), "", "", "compass");
  if (!searchResults.length) return empty(t("没有匹配结果"), t("试试其他关键词，或使用 Git 仓库直接导入。"));
  const count = hubListing ? t("已显示 {0} / {1} 个技能", { 0: searchResults.length, 1: hubListing.total }) : t("{0} 个技能", { 0: searchResults.length });
  return `<div class="hub-results-heading"><h3>${hubResultQuery ? t("搜索结果") : t("skills.sh 默认榜单")}</h3><span>${e(count)}</span></div><div class="results-list">${searchResults.map((result, index) => {
    const installed = hubResultInstalled(result, skills());
    return `<article class="search-result"><div class="result-top"><span class="skill-symbol small-symbol">${icon("box")}</span><div class="result-copy"><div class="result-heading"><strong title="${e(result.name)}">${e(result.name)}</strong></div><p class="mono" title="${e(result.source)}">${e(result.source)}</p></div></div>${result.supported === false ? `<p class="unsupported-reason">${e(tMessage(result.unsupportedReason) || t("此来源暂不支持导入"))}</p>` : ""}<div class="result-footer"><span class="result-installs">${icon("download")}${Number.isFinite(result.installs) ? new Intl.NumberFormat(getLocale(), { notation: "compact" }).format(result.installs) : "—"}</span><div class="result-actions">${installed ? `<span class="result-installed">${icon("check")}${t("已安装")}</span>` : ""}${button(t(result.supported === false ? "暂不支持" : "预览"), "hub-preview", "small", `data-index="${index}" ${result.supported === false ? "disabled" : ""}`, "plus")}</div></div></article>`;
  }).join("")}</div>${hubListing?.hasMore ? `<div class="hub-results-footer">${button(t("加载更多"), "load-more-hub", "small", "", "chevron-down")}</div>` : ""}`;
}
async function searchHub(query: string, loadMore = false) {
  if (activeTasks.has("正在搜索 skills.sh…")) return;
  if (loadMore && (!hubListing?.hasMore || hubResultQuery)) return;
  query = loadMore ? hubResultQuery : query.trim();
  const page = loadMore ? hubListing!.page + 1 : 0;
  if (!loadMore) { searchQuery = query; hubResultQuery = query; searchResults = null; hubListing = null; }
  const result = await task("正在搜索 skills.sh…", async (): Promise<SkillListPage> => {
    if (currentPage === "discover") render();
    if (!query) return api.listSkills(page);
    const results = await api.search(query);
    return { skills: results, total: results.length, hasMore: false, page: 0 };
  });
  if (result) {
    searchResults = loadMore ? [...arr(searchResults), ...arr(result.skills)] : arr(result.skills);
    hubListing = query ? null : result;
  }
  if (currentPage === "discover") render();
}
function ensureHubResults() {
  if (data && currentPage === "discover" && discoverTab === "hub" && searchResults === null) void searchHub(searchQuery);
}
function discoverPage() {
  const tabs = ([["hub", 'Skill Hub <span class="tab-note">skills.sh</span>'], ["git", e(t("Git 仓库"))], ["local", e(t("本地目录"))]] as const).map(([tab, label]) => `<button type="button" id="discover-tab-${tab}" class="tab-button ${discoverTab === tab ? "active" : ""}" data-action="discover-tab" data-tab="${tab}" role="tab" aria-controls="discover-tab-panel" aria-selected="${discoverTab === tab}" tabindex="${discoverTab === tab ? 0 : -1}">${label}</button>`).join("");
  const local = `<section class="panel import-panel" data-preserve-scroll="discover-local"><form id="local-preview"><label class="field"><span>${t("本地技能目录")}</span><div class="input-with-button"><input name="localSource" data-page-search="discover" aria-label="${e(t("本地技能目录"))}" aria-describedby="local-source-help" required placeholder="/Users/you/skills" value="${e(localSourceDraft)}">${iconButton(t("选择目录"), "choose-local-dir", "folder")}</div><small id="local-source-help">${t("选择包含 SKILL.md 的技能目录，或包含多个技能的父目录。")}</small></label><div class="form-footer"><button class="button primary" type="submit">${icon("search")}${t("预览本地技能")}</button></div></form></section>`;
  return `${heading(t("发现"), "")}<div class="tabs discover-tabs" role="tablist" aria-label="${t("导入方式")}">${tabs}</div><div id="discover-tab-panel" class="discover-content ${preview || previewRequest ? "has-preview" : ""}" role="tabpanel" aria-labelledby="discover-tab-${discoverTab}" tabindex="0">${discoverTab === "hub" ? `<section class="panel hub-panel" data-preserve-scroll="discover-hub:${e(hubResultQuery)}"><form id="hub-search" class="inline-form"><label class="search-field">${icon("search")}<input name="query" data-page-search="discover" placeholder="${t("搜索 skills.sh，留空浏览默认榜单")}" value="${e(searchQuery)}" aria-label="${t("搜索 skills.sh")}">${searchShortcut()}</label><button class="button primary" type="submit">${t("搜索")}</button></form><div id="search-results" data-preserve-scroll="discover-results:${e(hubResultQuery)}" tabindex="0" role="region" aria-label="${t("搜索结果")}">${hubResults()}</div></section>` : discoverTab === "local" ? local : `<section class="panel import-panel" data-preserve-scroll="discover-git"><form id="source-preview"><div class="form-grid"><label class="field span-2"><span>${t("仓库地址 ")}<i>${t("必填")}</i></span><input name="source" data-page-search="discover" required placeholder="${t("owner/repo 或 https://github.com/owner/repo")}" value="${e(sourceDraft)}"></label><label class="field"><span>${t("分支、标签或 Commit ")}<i>${t("可选")}</i></span><input name="ref" placeholder="${t("默认分支")}" value="${e(refDraft)}"></label><label class="field"><span>${t("技能子目录 ")}<i>${t("可选")}</i></span><input name="subpath" placeholder="${t("自动扫描仓库")}" value="${e(subpathDraft)}"></label></div><div class="form-footer"><button class="button primary" type="submit">${icon("search")}${t("预览仓库")}</button></div></form></section>`}${preview ? previewPanel() : previewRequest ? previewRequestPanel() : ""}</div>`;
}
function previewWaitHint(request: PreviewRequest, seconds: number) {
  if (request.kind === "local") return t("正在读取本地技能文件，请稍候。");
  if (seconds >= 45) return t("仓库较大或传输较慢时可能需要几分钟，请稍候。超时后可以重试。");
  if (seconds >= 15) return t("仍在等待仓库内容，网络较慢或仓库较大时可能需要更久。");
  return t("正在获取仓库内容并查找技能。首次预览可能需要更长时间。");
}
function updatePreviewWait() {
  if (!previewRequest?.loading) return;
  const seconds = Math.max(0, Math.floor((Date.now() - previewRequest.startedAt) / 1000));
  const elapsed = document.querySelector("#preview-elapsed");
  const hint = document.querySelector("#preview-wait-hint");
  if (elapsed) elapsed.textContent = t("已等待 {0} 秒", { 0: seconds });
  if (hint) hint.textContent = previewWaitHint(previewRequest, seconds);
}
function previewRequestPanel() {
  const request = previewRequest!;
  const seconds = Math.max(0, Math.floor((Date.now() - request.startedAt) / 1000));
  const timedOut = /timed out|timeout|deadline exceeded/i.test(request.error);
  const title = request.loading ? t("正在准备预览…") : timedOut ? t("预览超时") : t("未能完成预览");
  return `<section class="panel preview-panel preview-request" aria-labelledby="preview-request-title" aria-busy="${request.loading}"><div class="preview-heading"><h3 id="preview-request-title">${request.loading ? '<span class="spinner" aria-hidden="true"></span>' : icon("alert")}${title}</h3></div><p class="mono preview-request-source">${e(request.source)}</p>${request.loading ? `<p id="preview-elapsed" class="preview-elapsed" role="timer" aria-live="off">${t("已等待 {0} 秒", { 0: seconds })}</p><p id="preview-wait-hint" role="status">${previewWaitHint(request, seconds)}</p><p class="muted">${t("可以切换到其他页面，预览会继续进行。")}</p>` : `<div class="preview-request-error" role="alert"><p>${request.kind === "local" ? t("请检查目录是否存在且可读取，然后重试。") : timedOut ? t("获取仓库或读取技能内容超过等待时限。仓库大小、传输速度或代理连接都可能影响耗时，请稍后重试。") : t("请检查网络、仓库地址和访问权限后重试。")}</p></div>${button(t("重试预览"), "retry-preview", "primary", "", "refresh")}<details class="preview-error-details"><summary>${t("错误详情")}</summary><pre>${e(tMessage(request.error))}</pre></details>`}</section>`;
}
function previewPanel() {
  const skipped = arr(preview!.skipped);
  return `<section class="panel preview-panel" data-preserve-scroll="discover-preview:${e(preview!.id)}"><div class="preview-heading"><div><h3>${t("选择要导入的技能 ")}<span class="badge">${arr(preview!.candidates).length}</span></h3><p class="mono" title="${e(preview!.source)}">${e(preview!.local ? globalPath(preview!.source) : preview!.source)}${preview!.local ? "" : ` · ${e(short(preview!.revision))}`}</p></div>${button(selectedCandidates.size === arr(preview!.candidates).length ? t("取消全选") : t("全选"), "select-all-candidates", "quiet small", arr(preview!.candidates).length ? "" : "disabled")}</div>${scanWarnings(preview!.warnings)}${skipped.length ? `<div class="preview-skipped" role="status"><strong>${t("本次跳过 {0} 个技能", { 0: skipped.length })}</strong><ul>${skipped.map((item) => `<li><code>${e(item.subpath || ".")}</code><span>${e(tMessage(item.reason))}</span></li>`).join("")}</ul></div>` : ""}${arr(preview!.candidates).length ? `<div class="selection-list" data-preserve-scroll="discover-candidates:${e(preview!.id)}" tabindex="0" role="region" aria-label="${t("选择要导入的技能 ")}">${arr(preview!.candidates).map((candidate) => `<label class="selection-row"><input type="checkbox" name="candidate" value="${e(candidate.subpath)}" ${selectedCandidates.has(candidate.subpath) ? "checked" : ""}><span><strong>${e(candidate.name)}</strong>${candidate.description ? `<small>${e(candidate.description)}</small>` : ""}<code>${e(candidate.subpath || ".")}</code></span></label>`).join("")}</div><div class="form-footer"><span class="muted" id="candidate-count">${t("已选择 ")}${selectedCandidates.size}${t(" 个技能")}</span>${button(t("导入所选技能"), "install", "primary", selectedCandidates.size ? "" : "disabled", "download")}</div>` : `<div class="small-empty">${t(preview!.local ? "没有发现有效的 SKILL.md，请检查所选目录。" : preview!.hosted ? "没有发现有效的 SKILL.md，请检查来源。" : "没有发现有效的 SKILL.md，请检查仓库与子目录。")}</div>`}</section>`;
}
function presetsPage() {
  const list = presets().filter((preset) => `${preset.name} ${preset.description} ${arr(preset.skillIds).map((id) => skills().find((skill) => skill.id === id)?.name || "").join(" ")}`.toLowerCase().includes(pageQueries.presets.toLowerCase()));
  if (!list.some((preset) => preset.id === selectedPreset)) selectedPreset = list[0]?.id || "";
  const selected = list.find((preset) => preset.id === selectedPreset);
  const header = heading(t("预设"), "", button(t("新建预设"), "new-preset", "primary", "", "plus"));
  if (!presets().length) return `${header}${empty(t("创建第一个预设"), "", button(t("新建预设"), "new-preset", "primary", "", "plus"), "layers")}`;
  const rows = list.map((preset) => `<div class="preset-list-item ${selectedPreset === preset.id ? "current" : ""}">
    <button type="button" class="preset-row" data-action="select-preset" data-id="${e(preset.id)}" ${selectedPreset === preset.id ? 'aria-current="true"' : ""}><span class="preset-symbol">${icon(presetIconName(preset.icon))}</span><strong class="preset-name" title="${e(preset.name)}">${e(preset.name)}</strong><span class="preset-count">${t("{0} 个技能", { 0: arr(preset.skillIds).length })}</span></button>
  </div>`).join("");
  return `${header}<div class="presets-layout"><section class="panel presets-browser" data-preserve-scroll="presets-browser"><div class="panel-toolbar">${pageSearch("presets", t("搜索预设"))}<span class="muted">${list.length}${t(" 个")}</span></div><div class="preset-list" data-preserve-scroll="presets-list" tabindex="0" role="region" aria-label="${t("预设")}">${rows || `<div class="small-empty">${t("没有匹配的预设")}</div>`}</div></section><aside class="panel preset-detail" data-preserve-scroll="preset-detail:${e(selectedPreset)}" tabindex="0" aria-label="${t("预设详情")}">${selected ? presetDetail(selected) : empty(t("没有匹配的预设"), t("试试其他筛选条件或关键词。"))}</aside></div>`;
}
function presetDetail(preset: Preset) {
  const actions = `<div class="preset-actions">${iconButton(`${t("编辑预设")} · ${preset.name}`, "edit-preset", "edit", `data-id="${e(preset.id)}"`)}${iconButton(`${t("删除预设")} · ${preset.name}`, "delete-preset", "trash", `data-id="${e(preset.id)}"`, "danger-quiet")}</div>`;
  return `<div class="preset-detail-header"><span class="preset-detail-symbol">${icon(presetIconName(preset.icon))}</span><h2>${e(preset.name)}</h2><span class="badge">${t("{0} 个技能", { 0: arr(preset.skillIds).length })}</span>${actions}</div>${preset.description ? `<p class="preset-description">${e(preset.description)}</p>` : ""}<section class="preset-detail-section"><h3>${t("包含的技能")}</h3><div class="preset-detail-skills">${arr(preset.skillIds).map((id) => {
    const skill = skills().find((skill) => skill.id === id);
    return skill ? `<button type="button" class="preset-detail-skill" data-action="view-local-skill" data-id="${e(id)}"><strong>${e(skill.name)}</strong><small>${e(originLabel(skill))}</small></button>` : `<div class="preset-detail-skill"><strong>${t("已缺失的技能")}</strong></div>`;
  }).join("") || `<div class="small-empty">${t("尚未添加技能")}</div>`}</div></section><p class="preset-usage">${projects().filter((project) => arr(project.enabledPresetIds).includes(preset.id)).length}${t(" 个项目 · ")}${uniqueApplications(globalApplications().filter((application) => arr(application.target?.enabledPresetIds).includes(preset.id))).length}${t(" 个全局配置使用")}</p>`;
}
function targetPaths(target: Target | null) { return arr(target?.paths).length ? arr(target?.paths) : target?.path ? [target.path] : []; }
function projectsPage() {
  const list = projects().filter((project) => {
    if (projectsFilter === "pending" && !project.pending) return false;
    if (!pageQueries.projects.trim()) return true;
    const selection = applicationSelection("project", project.id, project);
    const effective = effectiveSkillIDs(selection);
    return matchesProject(project, pageQueries.projects,
      skills().filter((skill) => effective.has(skill.id)).map((skill) => skill.name),
      presets().filter((preset) => selection.presetIds.includes(preset.id)).map((preset) => preset.name));
  }).sort((a, b) => a.name.localeCompare(b.name, getLocale(), { numeric: true, sensitivity: "base" }));
  if (!projects().some((project) => project.id === selectedProject)) selectedProject = list[0]?.id || "";
  const selected = list.find((project) => project.id === selectedProject) || list[0];
  const header = heading(t("项目"), "", button(t("添加项目"), "new-project", "primary", "", "plus"));
  if (!projects().length) return `${header}${empty(t("添加第一个项目"), "", button(t("添加项目"), "new-project", "primary", "", "plus"), "folder")}`;
  // Keep the explicit selection while searching; clearing the search restores it.
  const selectedID = selected?.id || "";
  visibleProject = selectedID;
  return `${header}<div class="projects-layout"><section class="panel projects-browser" aria-label="${e(t("项目列表"))}"><div class="projects-browser-toolbar">${pageSearch("projects", t("搜索项目、路径或技能"))}<div class="projects-filters" role="group" aria-label="${e(t("项目筛选"))}">${([["all", t("全部"), projects().length], ["pending", t("待应用"), projects().filter((project) => project.pending).length]] as const).map(([filter, label, count]) => `<button type="button" class="filter-chip ${projectsFilter === filter ? "active" : ""}" data-action="filter-projects" data-filter="${filter}" aria-pressed="${projectsFilter === filter}">${label}<span class="filter-count">${count}</span></button>`).join("")}</div></div><div class="project-list" data-preserve-scroll="project-list">${list.map((project) => projectRow(project, selectedID)).join("") || `<div class="small-empty">${t("没有匹配的项目")}</div>`}</div><div class="projects-list-footer">${t("显示 {0} / {1} 个项目", { 0: list.length, 1: projects().length })}</div></section>${selected ? projectCard(selected) : `<section class="panel project-detail project-detail-empty">${empty(t("没有匹配的项目"), t("试试其他筛选条件或关键词。"), button(t("清除筛选"), "clear-project-filters", "small", "", "close"), "search")}</section>`}</div>`;
}
function globalBatchKeys(id = selectedGlobalKey) { return id.startsWith("batch:") ? id.slice(6).split(",").filter(Boolean) : id ? [id] : []; }
function globalBatchID() { return `batch:${[...selectedGlobalKeys].sort().join(",")}`; }
function globalPage() {
  const applications = globalApplications();
  if (!globalDirectoriesInitialized) {
    selectedGlobalKeys = new Set(data?.state.settings.defaultGlobalDirectories || defaultSettings().defaultGlobalDirectories);
    globalDirectoriesInitialized = true;
  }
  selectedGlobalKeys = new Set([...selectedGlobalKeys].filter((key) => applications.some((application) => application.key === key)));
  selectedGlobalKey = globalBatchID();
  const selected = uniqueApplications(applications.filter((application) => selectedGlobalKeys.has(application.key)));
  const application = findApplication("global", selectedGlobalKey);
  const differing = selected.some((item) => !sameSelection(savedSelection(item.target), savedSelection(selected[0]?.target || null)));
  const list = applications.filter((directory) => {
    if (globalFilter === "pending" && !directory.target?.pending) return false;
    const selection = savedSelection(directory.target);
    const effective = effectiveSkillIDs(selection);
    return matchesProject({ name: `${directory.agentName} ${directory.name}`, path: directory.path }, pageQueries.global,
      skills().filter((skill) => effective.has(skill.id)).map((skill) => skill.name),
      presets().filter((preset) => selection.presetIds.includes(preset.id)).map((preset) => preset.name));
  });
  const manage = button(t("管理目录"), "manage-global-directories", "", "", "settings");
  const header = heading(t("全局"), "", manage);
  if (!applications.length) return `${header}${empty(t("没有可用的全局目录"), "", manage, "monitor")}`;
  const allSelected = applications.every((directory) => selectedGlobalKeys.has(directory.key));
  const selectedCount = selectedGlobalKeys.size === selected.length ? t("已选 {0} 个目录", { 0: selected.length }) : t("已选 {0} 项 · {1} 个目录", { 0: selectedGlobalKeys.size, 1: selected.length });
  return `${header}<div class="global-layout">
    <section class="panel global-directory-selector" aria-label="${e(t("全局目录列表"))}">
      <div class="global-directory-heading"><h2>${t("技能目录")}</h2><span class="global-directory-count" title="${e(t("部分 Agent 会读取相同的技能目录。"))}">${selectedCount}</span></div>
      <div class="global-directory-toolbar">${pageSearch("global", t("搜索目录、路径或技能"))}<div class="global-directory-filters" role="group" aria-label="${e(t("全局目录筛选"))}">${([["all", t("全部"), applications.length], ["pending", t("待应用"), applications.filter((directory) => directory.target?.pending).length]] as const).map(([filter, label, count]) => `<button type="button" class="filter-chip ${globalFilter === filter ? "active" : ""}" data-action="filter-global-directories" data-filter="${filter}" aria-pressed="${globalFilter === filter}">${label}<span class="filter-count">${count}</span></button>`).join("")}</div><div class="global-directory-tools">${button(t(allSelected ? "取消全选" : "全选目录"), "select-global-directories", "quiet small")}${button(t("使用默认目录"), "default-global-directories", "quiet small")}</div></div>
      <div class="global-directory-list" data-preserve-scroll="global-directory-list" tabindex="0" role="region" aria-label="${e(t("全局目录列表"))}">${list.map((directory) => `<label class="global-directory-row ${selectedGlobalKeys.has(directory.key) ? "selected" : ""}" title="${e(t(directory.description))}"><input type="checkbox" name="global-directory-selection" value="${e(directory.key)}" ${selectedGlobalKeys.has(directory.key) ? "checked" : ""}>${agentIcon(directory.agentId)}<span class="global-directory-copy"><span class="global-directory-title"><strong title="${e(`${directory.agentName} · ${directory.name}`)}">${e(directory.agentName)}<span> · ${e(directory.name)}</span></strong>${applicationStatus(directory.target)}</span><code title="${e(directory.path)}">${e(globalPath(directory.path))}</code></span></label>`).join("") || `<div class="small-empty">${t("没有匹配的全局目录")}${button(t("清除筛选"), "clear-global-filters", "quiet small", "", "close")}</div>`}</div>
    </section>
    ${selected.length && application ? `<article class="panel application-card project-detail global-detail" data-scope="global" data-id="${e(selectedGlobalKey)}"><div class="global-configuration-heading"><h2>${t("技能配置")}</h2>${differing ? `<span class="badge amber">${t("配置不同")}</span>` : applicationStatus(application.target)}<span class="global-deployment-mode">${data?.state.settings.deploymentMode === "symlink" ? t("软链接") : t("复制")}</span></div>${differing ? `<div class="global-selection-note">${icon("info")}<span>${t("所选目录的技能配置不同，当前显示合并结果。修改选择或预览应用时会统一配置。")}</span></div>` : ""}${applicationBody("global", selectedGlobalKey, application.target, selected.map((directory) => directory.path))}</article>` : `<section class="panel project-detail project-detail-empty global-detail">${empty(t("选择全局目录"), t("勾选一个或多个目录，然后选择技能或预设。"), button(t("使用默认目录"), "default-global-directories", "small"), "monitor")}</section>`}
  </div>`;
}

function sameIDs(a: string[], b: string[]) { return JSON.stringify([...a].sort()) === JSON.stringify([...b].sort()); }
function sameSelection(a: ApplicationSelection, b: ApplicationSelection) { return sameIDs(a.skillIds, b.skillIds) && sameIDs(a.presetIds, b.presetIds); }
function savedSelection(target: Target | null): ApplicationSelection { return { skillIds: arr(target?.enabledSkillIds), presetIds: arr(target?.enabledPresetIds) }; }
function selectionDrafts(scope: ApplicationScope) { return scope === "project" ? projectDrafts : globalDrafts; }
function applicationKey(scope: ApplicationScope, id: string) { return `${scope}:${id}`; }
function applicationSelection(scope: ApplicationScope, id: string, target: Target | null) { return selectionDrafts(scope).get(id) || savedSelection(target); }
function applicationDirty(scope: ApplicationScope, id: string, target: Target | null) {
  const selection = applicationSelection(scope, id, target);
  if (scope === "global" && id.startsWith("batch:")) return globalApplications().filter((item) => globalBatchKeys(id).includes(item.key)).some((item) => !sameSelection(selection, savedSelection(item.target)));
  return !sameSelection(selection, savedSelection(target));
}
function effectiveSkillIDs(selection: ApplicationSelection) {
  const validSkills = new Set(skills().map((skill) => skill.id));
  return new Set([...selection.skillIds, ...presets().filter((preset) => selection.presetIds.includes(preset.id)).flatMap((preset) => arr(preset.skillIds))].filter((id) => validSkills.has(id)));
}
function findApplication(scope: ApplicationScope, id: string): { name: string; path: string; target: Target | null } | undefined {
  if (scope === "project") {
    const project = projects().find((item) => item.id === id);
    return project && { name: project.name, path: targetPaths(project).join("\n"), target: project };
  }
  if (id.startsWith("batch:")) {
    const directories = uniqueApplications(globalApplications().filter((item) => globalBatchKeys(id).includes(item.key)));
    if (!directories.length) return;
    const targets = directories.flatMap((item) => item.target ? [item.target] : []);
    const name = t("{0} 个全局目录", { 0: directories.length });
    const path = directories.map((item) => item.path).join("\n");
    const target: Target | null = targets.length ? { id, name, kind: "global", path, paths: directories.map((item) => item.path), enabledSkillIds: [...new Set(targets.flatMap((item) => arr(item.enabledSkillIds)))], enabledPresetIds: [...new Set(targets.flatMap((item) => arr(item.enabledPresetIds)))], deployments: targets.flatMap((item) => arr(item.deployments).map((deployment) => ({ ...deployment, path: deployment.path || item.path }))), pending: targets.some((item) => item.pending) } : null;
    return { name, path, target };
  }
  const application = globalApplications().find((item) => item.key === id);
  return application && { name: `${application.agentName} · ${application.name}`, path: application.path, target: application.target };
}
function applicationStatus(target: Target | null) {
  if (target?.kind === "project" && data?.projectDirectoryErrors?.[target.id]) return `<span class="badge amber">${t("目录不可用")}</span>`;
  return !target ? `<span class="badge">${t("尚未配置")}</span>` : target.pending ? `<span class="badge amber">${t("待应用")}</span>` : arr(target.deployments).length ? `<span class="badge green">${t("已应用")}</span>` : `<span class="badge">${t("未选择技能")}</span>`;
}
function projectRoot(path: string) { return path.replace(/\/\.(agents|claude|codex|cursor)\/skills\/?$/, ""); }
function projectRow(project: Target, selectedID: string) {
  const dirty = applicationDirty("project", project.id, project);
  const count = effectiveSkillIDs(applicationSelection("project", project.id, project)).size;
  const root = project.projectRoot || projectRoot(project.path);
  return `<button type="button" class="project-row ${selectedID === project.id ? "selected" : ""}" data-action="select-project" data-id="${e(project.id)}" aria-pressed="${selectedID === project.id}"><span class="project-row-top"><span class="project-row-name">${icon(projectIconName(project.icon))}<strong title="${e(project.name)}">${e(project.name)}</strong></span>${dirty ? `<span class="badge">${selectionSaveStatus("project", project.id)}</span>` : applicationStatus(project)}</span><code title="${e(root)}">${e(globalPath(root))}</code><span class="project-row-meta">${t("{0} 个技能 · {1} 个目录", { 0: count, 1: targetPaths(project).length })}</span></button>`;
}
function projectDisclosure(id: string, section: string) { return projectDetailsOpen.get(id)?.has(section) ? "open" : ""; }
function projectCard(project: Target) {
  const root = project.projectRoot || projectRoot(project.path);
  const dirty = applicationDirty("project", project.id, project);
  return `<article class="panel application-card project-detail" data-scope="project" data-id="${e(project.id)}"><div class="application-heading"><button type="button" class="card-icon project-icon-button" data-action="edit-project" data-id="${e(project.id)}" title="${e(t("编辑项目图标"))}" aria-label="${e(t("编辑项目图标"))}">${icon(projectIconName(project.icon))}</button><div><div class="title-line"><h2>${e(project.name)}</h2>${dirty ? `<span class="badge">${selectionSaveStatus("project", project.id)}</span>` : applicationStatus(project)}</div><p class="mono" title="${e(root)}">${e(globalPath(root))}</p></div><div class="application-actions">${iconButton(t("编辑项目"), "edit-project", "edit", `data-id="${e(project.id)}"`)}${iconButton(t("移除项目"), "delete-project", "trash", `data-id="${e(project.id)}"`, "danger-quiet")}</div></div>${data?.projectDirectoryErrors?.[project.id] ? `<div class="project-directory-error" role="status"><strong>${t("项目目录不可用")}</strong><p>${t("目录已删除或无法访问。重新选择目录后，可继续使用已保存的技能配置。")}</p>${button(t("重新选择目录"), "edit-project", "small", `data-id="${e(project.id)}"`, "folder")}</div>` : ""}<details class="project-destinations" data-project-disclosure="directories" data-id="${e(project.id)}" ${projectDisclosure(project.id, "directories")}><summary>${icon("folder")}<span>${t("{0} 个技能目录", { 0: targetPaths(project).length })}</span><span class="badge">${data?.state.settings.deploymentMode === "symlink" ? t("软链接") : t("复制")}</span>${icon("chevron-down")}</summary><div class="application-destination-list">${targetPaths(project).map((path) => `<code title="${e(path)}">${e(path.startsWith(`${root}/`) ? path.slice(root.length + 1) : globalPath(path))}</code>`).join("")}</div></details>${applicationBody("project", project.id, project)}</article>`;
}
function selectionSaveStatus(scope: ApplicationScope, id: string) {
  const state = selectionAutosave.state(applicationKey(scope, id));
  return state.error ? t("自动保存失败") : state.pending ? t("正在自动保存…") : t("已自动保存");
}
function selectionSaveFooter(scope: ApplicationScope, id: string) {
  const state = selectionAutosave.state(applicationKey(scope, id));
  return `<span class="selection-save-status ${state.error ? "save-error" : ""}" role="status" title="${e(state.error instanceof Error ? tMessage(state.error.message) : "")}">${icon(state.error ? "alert" : state.pending ? "refresh" : "check")}<span>${selectionSaveStatus(scope, id)}<small>${t("应用后才会更新技能文件。")}</small></span>${state.error ? button(t("重试"), "retry-selection", "quiet small", `data-scope="${scope}" data-id="${e(id)}"`, "refresh") : ""}</span>`;
}
function presetChoices(selection: ApplicationSelection, key: string) {
  return `<div class="preset-options" data-preserve-scroll="presets:${e(key)}">${presets().map((preset) => {
    const count = t("{0} 个技能", { 0: arr(preset.skillIds).length });
    const title = [preset.name, preset.description, count].filter(Boolean).join("\n");
    return `<label class="preset-option" title="${e(title)}"><input type="checkbox" name="application-preset" value="${e(preset.id)}" aria-label="${e(`${preset.name} · ${count}`)}" ${selection.presetIds.includes(preset.id) ? "checked" : ""}>${icon(presetIconName(preset.icon))}<span class="preset-option-name">${e(preset.name)}</span><small class="preset-option-count" title="${e(count)}" aria-hidden="true">${arr(preset.skillIds).length}</small></label>`;
  }).join("") || `<p class="muted">${t("还没有预设，也可以直接选择技能。")}</p>`}</div>`;
}
function applicationBody(scope: ApplicationScope, id: string, target: Target | null, paths = targetPaths(target)) {
  const uiKey = scope === "project" ? id : applicationKey(scope, id);
  const selection = applicationSelection(scope, id, target);
  const effective = effectiveSkillIDs(selection);
  const fromPresets = effectiveSkillIDs({ skillIds: [], presetIds: selection.presetIds });
  const filters = projectSkillFilters.get(uiKey) || { query: "", selectedOnly: false };
  const visible = new Set(skills().filter((skill) => matchesProjectSkill(skill, filters.query, filters.selectedOnly, effective)).map((skill) => skill.id));
  const plan = plans.get(applicationKey(scope, id));
  const force = plan && applicationPlanDisposition(plan) === "overwrite";
  const directoryUnavailable = scope === "project" && !!data?.projectDirectoryErrors?.[id];
  const enabled = skills().filter((skill) => effective.has(skill.id));
  const removing = [...new Map(arr(target?.deployments).filter((item) => !effective.has(item.skillId)).map((item) => [item.skillId, item.name])).values()];
  return `<form class="application-selection project-selection" data-scope="${scope}" data-id="${e(id)}">
    <div class="project-selection-scroll" data-preserve-scroll="application-detail:${e(scope)}:${e(id)}">
      <section class="project-preset-section"><div class="project-skills-heading"><h3>${icon("layers")}${t("预设")}</h3><span class="muted">${t("已选 {0} 个", { 0: selection.presetIds.length })}</span></div>${presetChoices(selection, applicationKey(scope, id))}</section>
      <section class="project-enabled-section" aria-label="${e(t("已启用技能"))}"><div class="project-skills-heading"><h3>${t("已启用技能")}</h3><span class="muted">${t("{0} 个技能", { 0: effective.size })}</span>${effective.size ? iconButton(t("清空选择"), "clear-application", "close", `data-scope="${scope}" data-id="${e(id)}"`) : ""}</div><div class="project-enabled-skills">${enabled.map((skill) => {
        const applied = projectSkillApplied(skill, paths, arr(target?.deployments), data!.state.settings.deploymentMode);
        const origin = presets().filter((preset) => selection.presetIds.includes(preset.id) && arr(preset.skillIds).includes(skill.id)).map((preset) => preset.name);
        return `<span class="project-enabled-skill ${applied ? "applied" : "pending"}" title="${e(origin.length ? t("来自预设：{0}", { 0: origin.join("、") }) : t("直接选择技能"))}"><span>${e(skill.name)}</span>${!applied ? `<small>${t("待应用")}</small>` : `<span class="sr-only">${t("已应用")}</span>`}</span>`;
      }).join("") || `<p class="muted">${t("选择预设，或展开下方直接选择技能。")}</p>`}</div>${removing.length ? `<p class="project-removing">${t("待移除：{0}", { 0: removing.join("、") })}</p>` : ""}</section>
      <details class="project-direct-skills" data-project-disclosure="skills" data-id="${e(uiKey)}" ${projectDisclosure(uiKey, "skills")}><summary>${icon("skills")}<span>${t("直接选择技能")}</span><small>${t("已选 {0} 个", { 0: selection.skillIds.length })}</small>${icon("chevron-down")}</summary><div class="project-skill-controls"><label class="search-field">${icon("search")}<input name="project-skill-search" data-project-skill-search="${e(uiKey)}" placeholder="${e(t("搜索名称或描述"))}" aria-label="${e(t(scope === "project" ? "搜索项目可用技能" : "搜索全局可用技能"))}" value="${e(filters.query)}"></label><button type="button" class="filter-chip ${filters.selectedOnly ? "active" : ""}" data-action="filter-project-skills" data-id="${e(uiKey)}" aria-pressed="${filters.selectedOnly}">${t("仅已选")}</button></div><div class="application-skills project-skills" data-preserve-scroll="application-skills:${e(scope)}:${e(id)}">${skills().map((skill) => `<label class="application-skill" ${visible.has(skill.id) ? "" : "hidden"}><input type="checkbox" name="application-skill" value="${e(skill.id)}" ${selection.skillIds.includes(skill.id) ? "checked" : ""}><span><strong>${e(skill.name)}</strong>${skill.description ? `<small title="${e(skill.description)}">${e(skill.description)}</small>` : ""}</span>${fromPresets.has(skill.id) ? `<span class="badge">${t("预设已包含")}</span>` : ""}</label>`).join("")}${!skills().length ? `<p class="muted padded">${t("尚未安装技能，请先从“发现”导入。")}</p>` : !visible.size ? `<p class="muted padded">${t("没有匹配的技能，试试其他筛选条件。")}</p>` : ""}</div></details>
      ${plan ? planView(plan, scope) : ""}
    </div><div class="application-bottom project-application-bottom">${selectionSaveFooter(scope, id)}<div>${button(t("预览变化"), `plan-${scope}`, "small", `data-id="${e(id)}" ${directoryUnavailable || !target && !effective.size ? "disabled" : ""}`, "eye")}${button(force ? t("强制覆盖…") : t(scope === "project" ? "应用到项目" : "应用到全局"), `apply-${scope}`, force ? "danger small" : "primary small", `data-id="${e(id)}" ${directoryUnavailable || !target && !effective.size ? "disabled" : ""}`, "arrow")}</div></div></form>`;
}

function planView(plan: ApplyPlan, scope: ApplicationScope) {
  const disposition = applicationPlanDisposition(plan);
  const conflicts = arr(plan.conflicts);
  const overwrite = arr(plan.overwrite);
  return `<div class="plan"><h3>${t("应用预览")}</h3>${arr(plan.directories).length ? `<div class="plan-directories">${arr(plan.directories).map((directory) => `<div><code title="${e(directory.path)}">${e(scope === "global" ? globalPath(directory.path) : directory.path)}</code><span>${t("新增 ")}${arr(directory.add).length}${t(" · 更新 ")}${arr(directory.update).length}${t(" · 移除 ")}${arr(directory.remove).length}${arr(directory.conflicts).length ? t(" · {0} 个冲突", { 0: arr(directory.conflicts).length }) : ""}</span></div>`).join("")}</div>` : ""}<div class="plan-columns">${[[t("新增"), plan.add, "plan-add"], [t("更新"), plan.update, "plan-update"], [t("移除"), plan.remove, "plan-remove"]].map(([title, names, cls]) => `<div class="${cls}"><strong>${title} <span>${(names as string[] || []).length}</span></strong>${arr(names as string[]).length ? `<ul>${arr(names as string[]).map((n) => `<li>${e(n)}</li>`).join("")}</ul>` : `<p>${t("无变化")}</p>`}</div>`).join("")}</div>${conflicts.length ? `<div class="conflict-box"><strong>${disposition === "overwrite" ? t("以下冲突可通过强制覆盖处理") : t("以下冲突需要先处理，暂不能应用")}</strong><ul>${conflicts.map((c) => `<li>${e(conflictMessage(c))}</li>`).join("")}</ul></div>` : `<p class="plan-safe">${t("没有发现冲突，可以应用以上变化。")}</p>`}${overwrite.length && disposition === "overwrite" ? `<div class="plan-overwrite"><strong>${t("将覆盖或删除的现有内容")}</strong><ul>${overwrite.map((path) => `<li><code>${e(path)}</code></li>`).join("")}</ul><p>${t("强制覆盖会替换同名内容，并删除待移除项中的本地修改，无法恢复。请先自行备份需要保留的文件。")}</p></div>` : ""}</div>`;
}

const snapshotReasons: Record<string, string> = {
  "before installing or updating skills": "安装或更新技能前", "before deleting skill": "移除技能前",
  "before saving preset": "保存预设前", "before deleting preset": "删除预设前",
  "before saving target": "保存应用配置前", "before deleting target": "移除应用配置前",
  "before changing target presets": "保存技能选择前", "before changing target selection": "保存技能选择前", "before restoring skills": "恢复工作空间前",
  "before saving source": "保存来源前", "before deleting source": "删除来源前",
  "before refreshing source": "刷新来源前", "before synchronizing source": "同步来源前",
  "before relinking source skill": "重新关联技能前", "before applying target": "应用技能前",
  "before applying targets": "应用技能前", "before changing target selections": "保存技能选择前",
  "before importing external skills": "导入外部技能前",
};
const snapshotReason = (reason: string) => t(snapshotReasons[reason] || reason || "手动快照");
function conflictMessage(message: string) { return tMessage(message); }
function backupsPage() {
  const list = arr(data?.snapshots);
  const settings = settingsDraft || data!.state.settings;
  return `${heading(t("Backup and migration"), "")}
    ${cloudSyncSection(data?.cloudSync)}
    <section class="settings-section backup-transfer-section" aria-labelledby="backup-transfer-title">
      <h2 id="backup-transfer-title" class="settings-section-title">${t("迁移")}</h2>
      <div class="settings-group">
        <div class="settings-row"><strong>${t("工作空间 ZIP")}</strong><div class="backup-transfer-actions">${button(t("导出 ZIP"), "export-backup", "", "", "upload")}${button(t("导入 ZIP"), "import-backup", "", "", "download")}</div></div>
        ${skillsManagerRow()}
      </div>
    </section>
    <section class="panel snapshots-panel" aria-labelledby="local-snapshots-title">
      <div class="panel-toolbar"><div class="snapshot-heading"><h2 id="local-snapshots-title">${t("本地快照")}</h2><span class="badge">${list.length}${t(" 个快照")}</span></div>${button(t("创建快照"), "create-snapshot", "primary small", "", "plus")}</div>
      <div class="snapshot-retention"><label><span>${t("保留天数")}</span><input type="number" name="snapshotRetentionDays" min="1" max="3650" step="1" required value="${settings.snapshotRetentionDays || 7}"><span>${t("天")}</span></label><label><span>${t("最多保留")}</span><input type="number" name="snapshotMaxCount" min="1" max="10000" step="1" required value="${settings.snapshotMaxCount || 100}"><span>${t("条快照")}</span></label><small>${t("调整后立即清理，恢复时会先保存当前状态。")}</small></div>
      ${!list.length ? `<div class="small-empty">${t("还没有快照")}</div>` : `<div class="snapshot-table"><div class="table-head"><span>${t("创建时间 / 原因")}</span><span>${t("内容")}</span><span></span></div>${list.map((snapshot) => `<div class="snapshot-row"><div><strong>${e(date(snapshot.createdAt))}</strong><small>${e(snapshotReason(snapshot.reason))}</small></div><span>${snapshot.skillCount}${t(" 个技能 ")}<span class="muted">·</span> ${snapshot.presetCount}${t(" 个预设")}</span>${button(t("恢复"), "restore-snapshot", "small", `data-id="${e(snapshot.id)}"`, "restore")}</div>`).join("")}</div>`}
    </section>
    <details class="backup-storage"><summary>${t("本地数据目录")}</summary><code>${e(data?.dataDir || "—")}</code></details>`;
}
function skillsManagerRow() {
  return `<div class="settings-row skills-manager-row"><div class="settings-row-copy"><strong>${t("从 skills-manager 导入")}</strong>${skillsManagerImportResult ? `<div class="skills-manager-result" role="status"><strong>${icon("check")}${t("最近一次导入已完成")}</strong>${skillsManagerResultView(skillsManagerImportResult)}</div>` : ""}</div>${button(t("导入数据库"), "import-skills-manager", "", "", "download")}</div>`;
}
function skillsManagerResultView(result: ImportResult) {
  return `<table class="import-result-table"><thead><tr><th scope="col">${t("内容")}</th><th scope="col">${t("新增")}</th><th scope="col">${t("跳过")}</th></tr></thead><tbody>${[[t("技能"), result.addedSkills, result.skippedSkills], [t("预设"), result.addedPresets, result.skippedPresets], [t("来源"), result.addedSources, result.skippedSources]].map(([name, added, skipped]) => `<tr><th scope="row">${e(name)}</th><td>${e(added)}</td><td>${e(skipped)}</td></tr>`).join("")}</tbody></table>`;
}
function closeModal(result = false) {
  if (modalRoot.querySelector("#skills-manager-import")) skillsManagerPreview = null;
  const previous = modalPreviousFocus;
  const resolve = confirmResolve;
  modalRoot.innerHTML = "";
  app.inert = false;
  modalPreviousFocus = null;
  confirmResolve = null;
  queueMicrotask(() => {
    if (modalRoot.children.length) return;
    if (previous?.isConnected && !(previous instanceof HTMLButtonElement && previous.disabled)) previous.focus({ preventScroll: true });
    else {
      const replacement = previous instanceof HTMLButtonElement ? [...app.querySelectorAll<HTMLButtonElement>("button")].find((button) => button.dataset.action === previous.dataset.action && button.dataset.id === previous.dataset.id && button.dataset.page === previous.dataset.page) : null;
      (replacement || app.querySelector<HTMLButtonElement>('button[data-page][aria-current="page"]'))?.focus({ preventScroll: true });
    }
  });
  resolve?.(result);
}
function closeSubmittedModal(form: HTMLFormElement) { if (form.isConnected && modalRoot.contains(form)) closeModal(); }
function modal(title: string, description: string, body: string, footer: string, formID = "") {
  closeModal();
  modalGeneration++;
  modalPreviousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  modalRoot.innerHTML = `<div class="modal-backdrop"><section class="modal" role="dialog" aria-modal="true" aria-labelledby="modal-title" ${description ? 'aria-describedby="modal-description"' : ""} tabindex="-1"><div class="modal-heading"><div><h2 id="modal-title">${e(title)}</h2>${description ? `<p id="modal-description">${e(description)}</p>` : ""}</div><button type="button" class="icon-button" data-action="close-modal" aria-label="${t("关闭")}" title="${t("关闭")}">${icon("close")}</button></div>${formID ? `<form id="${formID}">` : ""}<div class="modal-body">${body}</div><div class="modal-footer">${button(t("取消"), "close-modal")}${footer}</div>${formID ? "</form>" : ""}</section></div>`;
  app.inert = true;
  syncBusyControls();
  const first = modalRoot.querySelector<HTMLElement>('input:not([type="hidden"]):not([type="checkbox"]):not([type="radio"]):not(:disabled),select:not(:disabled)') || modalRoot.querySelector<HTMLElement>('.modal-footer [data-action="close-modal"]');
  first?.focus();
}
function confirm(title: string, description: string, body = "", label = t("确认"), danger = false): Promise<boolean> {
  modal(title, description, body, button(label, "confirm-modal", danger ? "danger" : "primary"));
  modalRoot.querySelector<HTMLElement>('.modal-footer [data-action="close-modal"]')?.focus();
  return new Promise((resolve) => { confirmResolve = resolve; });
}
function cloudSyncEditor() {
  modal(t("S3 / R2 sync"), t("Connect a private bucket and use the same encryption passphrase on every device."), cloudSyncConfigFields(data?.cloudSync?.config), `<button type="button" class="button" data-action="test-cloud-sync">${t("Test connection")}</button><button class="button primary" type="submit">${t("Save sync settings")}</button>`, "cloud-sync-editor");
}
function cloudConflictsEditor() {
  cloudConflictDraft = structuredClone(arr(data?.cloudSync?.conflicts));
  if (!cloudConflictDraft.length) { toast(t("No sync conflicts to review.")); return; }
  modal(t("Review sync conflicts"), t("Choose which version to keep for each conflict. Deletions are shown explicitly."), cloudConflictFields(cloudConflictDraft), `<button class="button primary" type="submit">${t("Apply choices and sync")}</button>`, "cloud-conflicts-editor");
}
async function showCloudBackups() {
  const backups = await task("Loading cloud backups…", () => api.listCloudBackups());
  if (!backups) return;
  cloudBackupDraft = backups;
  modal(t("Cloud backups"), t("Restore sources, skill files, presets, and shared settings from an earlier sync. This creates a new sync version."), cloudBackupsList(backups), "");
}
function presetEditor(id = "") {
  const p = presets().find((p) => p.id === id);
  presetFilters = defaultSkillFilters();
  presetSkillDetailsOpen.clear();
  presetSkillSelection = new Set(arr(p?.skillIds).filter((id) => skills().some((skill) => skill.id === id)));
  modal(p ? t("编辑预设") : t("新建预设"), "", `<input type="hidden" name="entityId" value="${e(p?.id)}"><label class="field"><span>${t("名称")}</span><input name="name" value="${e(p?.name)}" required placeholder="${t("例如：日常开发")}" maxlength="80"></label><label class="field"><span>${t("描述 ")}<i>${t("可选")}</i></span><textarea name="description" rows="2" placeholder="${t("这个预设适合什么场景？")}">${e(p?.description)}</textarea></label>${entityIconPicker("preset", p?.icon)}<div class="label-line preset-selection-heading"><h3>${t("选择技能")}</h3><div class="preset-selection-tools"><span id="preset-selection-count" aria-live="polite"></span>${iconButton(t("全选当前筛选结果"), "select-filtered-preset-skills", "check-square", 'aria-controls="preset-skill-list" aria-pressed="false"')}</div></div><div class="preset-skill-filters"><label class="search-field">${icon("search")}<input name="preset-search" aria-label="${t("搜索已安装技能")}" placeholder="${t("搜索名称、描述或来源")}">${searchShortcut()}</label>${sourceFilter("preset-source", presetFilters.source)}</div><div id="preset-skill-list" class="selection-list modal-selection"></div>`, `<button class="button primary" type="submit">${t("保存预设")}</button>`, "preset-editor");
  presetEditorBaselines.set(modalRoot.querySelector<HTMLFormElement>("#preset-editor")!, p ? structuredClone(p) : null);
  renderPresetSkills();
}
function renderPresetSkills() {
  const form = modalRoot.querySelector<HTMLFormElement>("#preset-editor");
  if (!form) return;
  const container = form.querySelector<HTMLElement>("#preset-skill-list")!;
  const scrollTop = container.scrollTop;
  const active = document.activeElement;
  const focusedSkill = active instanceof HTMLInputElement && active.name === "skill" ? active.value : "";
  const focusedDescription = active instanceof HTMLElement && active.tagName === "SUMMARY" && container.contains(active) ? active.parentElement?.dataset.presetSkill : undefined;
  for (const details of container.querySelectorAll<HTMLDetailsElement>("details[data-preset-skill]")) {
    if (details.open) presetSkillDetailsOpen.add(details.dataset.presetSkill!); else presetSkillDetailsOpen.delete(details.dataset.presetSkill!);
  }
  const list = filteredSkills(presetFilters);
  const allSelected = list.length > 0 && list.every((skill) => presetSkillSelection.has(skill.id));
  const selectAll = form.querySelector<HTMLButtonElement>('[data-action="select-filtered-preset-skills"]')!;
  selectAll.disabled = !list.length || activeTasks.has("正在保存预设…");
  const selectionLabel = t(allSelected ? "取消全选当前筛选结果" : "全选当前筛选结果");
  selectAll.title = selectionLabel;
  selectAll.setAttribute("aria-label", selectionLabel);
  selectAll.setAttribute("aria-pressed", String(allSelected));
  form.querySelector("#preset-selection-count")!.textContent = t("已选择 {0} 个技能", { 0: presetSkillSelection.size });
  container.innerHTML = list.map((skill) => `<div class="preset-skill-row"><label class="preset-skill-selector"><input type="checkbox" name="skill" value="${e(skill.id)}" aria-label="${e(t("选择技能 {0}", { 0: skill.name }))}" ${presetSkillSelection.has(skill.id) ? "checked" : ""}></label><details class="preset-skill-detail" data-preset-skill="${e(skill.id)}" ${presetSkillDetailsOpen.has(skill.id) ? "open" : ""}><summary><strong title="${e(skill.name)}">${e(skill.name)}</strong><span class="preset-skill-origin" title="${e(originLabel(skill))}">${e(originLabel(skill))}</span></summary><p class="preset-skill-description">${e(skill.description || t("该技能未提供描述。"))}</p></details></div>`).join("") || `<p class="muted padded">${skills().length ? t("没有匹配的技能，试试其他筛选条件。") : t("尚未安装技能。你也可以先创建空的预设。")}</p>`;
  container.scrollTop = scrollTop;
  if (focusedSkill) [...container.querySelectorAll<HTMLInputElement>('input[name="skill"]')].find((input) => input.value === focusedSkill)?.focus({ preventScroll: true });
  if (focusedDescription) [...container.querySelectorAll<HTMLElement>("details[data-preset-skill]")].find((details) => details.dataset.presetSkill === focusedDescription)?.querySelector("summary")?.focus({ preventScroll: true });
}
function entityIconPicker(kind: "project" | "preset", value?: string) {
  const labels = {
    folder: t("文件夹"), monitor: t("桌面"), layers: t("图层"), book: t("书籍"), compass: t("指南针"), box: t("方盒"), skills: t("技能"), archive: t("归档"), link: t("链接"), settings: t("设置"),
    code: t("代码"), terminal: t("终端"), "git-branch": t("分支"), bug: t("缺陷"), shield: t("安全"), database: t("数据库"), server: t("服务器"), cloud: t("云端"), globe: t("网络"), rocket: t("火箭"), zap: t("闪电"), sparkles: t("星光"),
    cpu: t("芯片"), wrench: t("工具"), palette: t("调色盘"), pen: t("钢笔"), "file-text": t("文档"), chart: t("图表"), flask: t("实验"), lightbulb: t("灵感"), target: t("目标"), puzzle: t("拼图"), heart: t("爱心"), users: t("团队"),
  };
  const choices = kind === "preset" ? presetIcons : projectIcons;
  const selected = kind === "preset" ? presetIconName(value) : projectIconName(value);
  const defaultIcon = choices[0];
  return `<fieldset class="entity-icon-field"><legend>${t(kind === "preset" ? "预设图标" : "项目图标")}</legend><div class="entity-icon-options">${choices.map((name) => {
    const label = name === defaultIcon ? t("默认图标") : labels[name];
    return `<label title="${e(label)}"><input class="sr-only" type="radio" name="icon" value="${name === defaultIcon ? "" : name}" aria-label="${e(label)}" ${selected === name ? "checked" : ""}><span>${icon(name)}</span></label>`;
  }).join("")}</div></fieldset>`;
}
function projectEditor(id = "") {
  const project = projects().find((item) => item.id === id);
  const root = project ? project.projectRoot || projectRoot(project.path) : "";
  const defaults = data?.state.settings.defaultProjectDirectories || ["agents"];
  const directoryIDs = project?.directoryIds !== undefined ? arr(project.directoryIds) : project ? projectDirectories.filter((directory) => targetPaths(project).some((path) => path === `${root}/${directory.path}`)).map((directory) => directory.id) : defaults;
  const templates = arr(data?.state.settings.customProjectDirectories);
  const snapshots = arr(project?.customPaths);
  const matchingSnapshot = (path: string) => snapshots.find((snapshot) => resolveProjectPath(root, snapshot) === resolveProjectPath(root, path));
  const customOptions = [
    ...templates.map((directory) => ({ name: directory.name, path: matchingSnapshot(directory.path) || directory.path, checked: project ? matchingSnapshot(directory.path) !== undefined : defaults.includes(directory.id) })),
    ...snapshots.filter((path) => !templates.some((directory) => resolveProjectPath(root, directory.path) === resolveProjectPath(root, path))).map((path) => ({ name: t("当前项目目录"), path, checked: true })),
  ];
  modal(project ? t("编辑项目") : t("添加项目"), "", `<input type="hidden" name="entityId" value="${e(project?.id)}"><label class="field"><span>${t("项目根目录")}</span><div class="input-with-button"><input name="path" value="${e(root)}" required aria-describedby="project-path-help project-path-error" placeholder="/Users/you/Projects/my-project">${iconButton(t("选择目录"), "choose-project-dir", "folder")}</div><small id="project-path-help">${t("可直接输入路径，或选择目录；保存时检查目录是否存在。")}</small><small id="project-path-error" class="field-error" role="alert" hidden></small>${arr(project?.deployments).length ? `<small>${t("更改根目录后，请重新预览并应用；仍存在的旧目录会列出待移除的受管技能。")}</small>` : ""}</label><label class="field"><span>${t("项目名称 ")}<i>${t("可选")}</i></span><input name="name" value="${e(project?.name)}" placeholder="${t("默认使用项目文件夹名称")}" maxlength="80"></label>${entityIconPicker("project", project?.icon)}<fieldset class="settings-fieldset project-directory-options"><legend>${t("项目技能目录 · 可多选")}</legend><div class="settings-directory-grid">${projectDirectories.map((directory) => `<label class="settings-choice" title="${e(t(directory.description))}"><input type="checkbox" name="project-directory" value="${directory.id}" ${directoryIDs.includes(directory.id) ? "checked" : ""}>${agentIcon(directory.id)}<span><strong>${e(t(directory.name))}</strong><code>${e(directory.path)}</code></span></label>`).join("")}${customOptions.map((directory) => `<label class="settings-choice"><input type="checkbox" name="project-custom-path" value="${e(directory.path)}" ${directory.checked ? "checked" : ""}>${agentIcon("custom")}<span><strong>${e(directory.name)}</strong><code>${e(directory.path)}</code></span></label>`).join("")}</div><p class="muted">${t("自定义目录在设置中管理。")}</p></fieldset><div class="info-note"><strong>${t("应用目录")}</strong><div id="project-destination" class="application-destination-list"></div><p>${t("当前应用方式：")}${data?.state.settings.deploymentMode === "symlink" ? t("软链接") : t("复制")}${t("。保存项目仅记录配置，预览后应用才会更新技能文件。")}</p></div>`, `<button class="button primary" type="submit">${t("保存项目")}</button>`, "project-editor");
  updateProjectDestination();
}
function resolveProjectPath(root: string, path: string) {
  if (/^(?:[\\/]|[A-Za-z]:[\\/]|~(?:[\\/]|$))/.test(path)) return path;
  const parts: string[] = [];
  for (const part of `${root}/${path}`.split("/")) {
    if (part === "." || !part) continue;
    if (part === "..") parts.pop(); else parts.push(part);
  }
  return `${root.startsWith("/") ? "/" : ""}${parts.join("/")}`;
}
function updateProjectDestination() {
  const form = modalRoot.querySelector<HTMLFormElement>("#project-editor");
  if (!form) return;
  const path = projectRoot(field(form, "path").replace(/\/+$/, ""));
  const chosen = selectedIDs(form, "project-directory");
  const customPaths = selectedIDs(form, "project-custom-path").map((path) => path.trim()).filter(Boolean);
  const paths = [...projectDirectories.filter((directory) => chosen.includes(directory.id)).map((directory) => `${path}/${directory.path}`), ...customPaths.map((customPath) => resolveProjectPath(path, customPath))];
  const feedback = form.querySelector<HTMLElement>("#project-path-error");
  if (feedback) feedback.hidden = true;
  form.querySelector<HTMLInputElement>('[name="path"]')?.removeAttribute("aria-invalid");
  const destination = form.querySelector("#project-destination");
  if (destination) destination.innerHTML = path ? paths.map((path) => `<code>${e(path)}</code>`).join("") || `<span class="muted">${t("请至少选择一个技能目录")}</span>` : `<span class="muted">${t("选择项目后显示")}</span>`;
}
function absoluteProjectTemplatePath(path: string) {
  const absolute = /^(?:[\\/]|~(?:[\\/]|$)|[A-Za-z]:)/;
  if (absolute.test(path)) return true;
  const parts: string[] = [];
  for (const part of path.split("/")) {
    if (!part || part === ".") continue;
    if (part === ".." && parts.length && parts[parts.length - 1] !== "..") parts.pop(); else parts.push(part);
  }
  return absolute.test(parts.join("/"));
}
function projectDirectoryEditor(id = "") {
  const directory = data?.state.settings.customProjectDirectories.find((item) => item.id === id);
  if (id && !directory) return;
  modal(directory ? t("编辑项目目录") : t("添加项目目录"), "", `<input type="hidden" name="entityId" value="${e(directory?.id)}"><label class="field"><span>${t("名称")}</span><input name="name" value="${e(directory?.name)}" required maxlength="80" placeholder="${e(t("例如：团队技能目录"))}"></label><label class="field"><span>${t("技能目录")}</span><input name="path" value="${e(directory?.path)}" required placeholder=".my-agent/skills"><small>${directory && absoluteProjectTemplatePath(directory.path) ? t("历史绝对路径可保留；修改路径时请填写相对路径。") : t("填写相对项目根目录的位置，例如 .my-agent/skills。")}</small></label><div class="info-note">${t("修改模板不会更改项目已保存的应用目录。")}</div>`, `<button class="button primary" type="submit">${t("保存目录")}</button>`, "project-directory-editor");
}

function globalDirectoryEditor(id = "") {
  const application = globalApplications().find((item) => item.key === id && item.agentId === "custom");
  if (id && !application) return;
  const deployed = !!arr(application?.target?.deployments).length;
  modal(application ? t("编辑全局目录") : t("添加自定义目录"), "", `<input type="hidden" name="entityId" value="${e(application?.key)}"><label class="field"><span>${t("名称")}</span><input name="name" value="${e(application?.name)}" required maxlength="80" placeholder="${e(t("例如：团队技能目录"))}"></label><label class="field"><span>${t("技能目录")}</span><div class="input-with-button"><input name="path" value="${e(application?.path)}" required ${deployed ? "readonly" : ""} placeholder="/Users/you/.my-agent/skills">${iconButton(t("选择目录"), "choose-global-dir", "folder", deployed ? "disabled" : "")}</div>${deployed ? `<small>${t("修改目录前，请先清空技能选择并应用。")}</small>` : ""}</label>`, `<button class="button primary" type="submit">${t("保存目录")}</button>`, "global-directory-editor");
}

function backupEditor(importing: boolean, path = "") {
  modal(importing ? t("导入工作空间备份") : t("导出工作空间"), importing ? t("恢复技能与预设后，请为项目与全局重新选择技能并手动应用。") : t("压缩包包含技能文件、来源和预设，不包含本机项目与全局配置。"), `<label class="field"><span>${importing ? t("备份 ZIP 文件") : t("导出 ZIP 路径")}</span><div class="input-with-button"><input name="backupPath" value="${e(path)}" required placeholder="/Users/you/Backups/racomi.zip">${iconButton(t("选择文件"), "choose-backup-file", "folder", `data-mode="${importing ? "import" : "export"}"`)}</div></label>`, `<button class="button ${importing ? "danger" : "primary"}" type="submit">${importing ? t("下一步") : t("导出备份")}</button>`, importing ? "backup-import" : "backup-export");
}
async function skillsManagerEditor() {
  skillsManagerPreview = null;
  modal(t("导入 skills-manager 数据库"), "", `<label class="field"><span>${t("skills-manager 数据库")}</span><div class="input-with-button"><input name="skillsManagerPath" required placeholder="${t("选择或填写 .db 文件的绝对路径")}">${iconButton(t("选择文件"), "choose-skills-manager-file", "folder")}</div></label><div class="info-note"><strong>${t("合并导入并保留现有数据")}</strong><p>${t("导入前自动创建快照。旧数据库与已应用的技能保持不变；旧项目配置和符号链接部署不会自动接管，之后请在“项目”或“全局”中配置并手动应用。")}</p></div><div id="skills-manager-summary" aria-live="polite"></div>`, `<button class="button" type="submit">${t("预览数据库")}</button>` + button(t("合并导入"), "confirm-skills-manager-import", "primary", "hidden"), "skills-manager-import");
  const input = modalRoot.querySelector<HTMLInputElement>('input[name="skillsManagerPath"]')!;
  const path = await task("正在查找 skills-manager 数据库…", () => api.defaultSkillsManagerPath());
  if (path && input.isConnected && !input.value) input.value = path;
}
function invalidateSkillsManagerPreview(message = t("路径已更改，请重新预览数据库。")) {
  skillsManagerPreview = null;
  const form = modalRoot.querySelector<HTMLFormElement>("#skills-manager-import");
  if (!form) return;
  form.querySelector<HTMLButtonElement>('[data-action="confirm-skills-manager-import"]')!.hidden = true;
  form.querySelector<HTMLButtonElement>('button[type="submit"]')!.textContent = t("预览数据库");
  form.querySelector("#skills-manager-summary")!.textContent = message;
}
function showSkillsManagerPreview(form: HTMLFormElement, result: SkillsManagerPreview) {
  skillsManagerPreview = result;
  form.querySelector<HTMLInputElement>('input[name="skillsManagerPath"]')!.value = result.databasePath;
  form.querySelector("#skills-manager-summary")!.innerHTML = `<div class="import-preview"><div class="label-line"><h3>${t("数据库预览")}</h3><span>${t("合并时会跳过重复项")}</span></div><div class="import-counts">${[[t("技能"), result.skillCount], [t("预设"), result.presetCount], [t("来源"), result.sourceCount]].map(([name, count]) => `<div><strong>${e(count)}</strong><span>${e(name)}</span></div>`).join("")}</div>${arr(result.skillNames).length ? `<details class="import-names"><summary>${t("查看技能名称")}</summary><ul>${arr(result.skillNames).map((name) => `<li>${e(name)}</li>`).join("")}</ul></details>` : ""}${arr(result.presetNames).length ? `<details class="import-names"><summary>${t("查看预设名称")}</summary><ul>${arr(result.presetNames).map((name) => `<li>${e(name)}</li>`).join("")}</ul></details>` : ""}${arr(result.warnings).length ? `<div class="import-warnings"><strong>${icon("info")}${t("导入提示")}</strong><ul>${arr(result.warnings).map((warning) => `<li>${e(tMessage(warning))}</li>`).join("")}</ul></div>` : ""}</div>`;
  form.querySelector<HTMLButtonElement>('button[type="submit"]')!.textContent = t("重新预览");
  const importButton = form.querySelector<HTMLButtonElement>('[data-action="confirm-skills-manager-import"]')!;
  importButton.hidden = false;
  importButton.focus();
}
async function load() {
  loadError = ""; render();
  const revision = stateRevision, settingsRevision = settingsWriteRevision;
  try {
    const next = await api.state();
    if (revision === stateRevision && settingsRevision === settingsWriteRevision && !settingsAutosave.busy) accept(next);
    ensureHubResults();
    enableNativeThemeSync(); enableNativeLocaleSync();
  } catch (err) {
    if (revision === stateRevision && settingsRevision === settingsWriteRevision && !settingsAutosave.busy) { loadError = err instanceof Error ? err.message : String(err); render(); }
  }
}
async function doPreview(source: string, ref: string, subpath: string, matchName = "", kind: "git" | "local" = "git") {
  if (discoveryBusy()) return;
  if (kind === "local") localSourceDraft = source;
  else { sourceDraft = source; refDraft = ref; subpathDraft = subpath; }
  preview = null; selectedCandidates.clear();
  const request: PreviewRequest = { source, ref, subpath, matchName, kind, startedAt: Date.now(), loading: true, error: "" };
  previewRequest = request;
  const timer = window.setInterval(updatePreviewWait, 1000);
  try {
    const result = await task(kind === "local" ? "正在读取本地目录并发现技能…" : "正在读取来源并发现技能…", async () => {
      if (currentPage === "discover") {
        render();
        document.querySelector(".preview-panel")?.scrollIntoView({ behavior: "auto", block: "start" });
      }
      try { return await (kind === "local" ? api.previewLocal(source) : api.preview(source, ref, subpath)); }
      catch (err) { request.error = err instanceof Error ? err.message : String(err); return undefined; }
    });
    if (result) {
      preview = result;
      previewRequest = null;
      selectedCandidates = new Set(arr(result.candidates).filter((c) => matchName ? c.name === matchName || c.subpath.split("/").pop() === matchName : true).map((c) => c.subpath));
      if (matchName && !selectedCandidates.size) toast(t("来源预览已完成，请根据名称手动选择技能。"));
    }
  } finally {
    window.clearInterval(timer);
    request.loading = false;
    if (currentPage === "discover") {
      render();
      const panel = document.querySelector<HTMLElement>(".preview-panel");
      panel?.scrollIntoView({ behavior: "auto", block: "start" });
      const heading = panel?.querySelector("h3");
      if (heading && !modalRoot.children.length) { heading.tabIndex = -1; heading.focus({ preventScroll: true }); }
    }
  }
}

app.addEventListener("input", (ev) => {
  if ((ev as InputEvent).isComposing) return;
  const input = ev.target as HTMLInputElement;
  if (input.dataset.projectSkillSearch) {
    const id = input.dataset.projectSkillSearch;
    const filters = projectSkillFilters.get(id) || { query: "", selectedOnly: false };
    projectSkillFilters.set(id, { ...filters, query: input.value });
    render();
  }
  if (input.id === "skills-search") {
    skillsQuery = input.value;
    render();
    if (detailTab === "document" && selectedSkill) void loadSkillDocument(selectedSkill);
  }
  if (input.dataset.pageSearch && ["sources", "presets", "projects", "global"].includes(input.dataset.pageSearch)) {
    const page = input.dataset.pageSearch as keyof typeof pageQueries;
    pageQueries[page] = input.value;
    const start = input.selectionStart, end = input.selectionEnd;
    render();
    const replacement = document.querySelector<HTMLInputElement>(`input[data-page-search="${page}"]`);
    replacement?.focus();
    if (start !== null && end !== null) replacement?.setSelectionRange(start, end);
  }
  if (input.form?.id === "hub-search") {
    searchQuery = input.value;
  }
  if (input.form?.id === "source-preview") {
    sourceDraft = field(input.form, "source"); refDraft = field(input.form, "ref"); subpathDraft = field(input.form, "subpath");
  }
  if (input.form?.id === "local-preview") localSourceDraft = input.value;
});
app.addEventListener("compositionend", (ev) => {
  const input = ev.target;
  if (input instanceof HTMLInputElement && (input.dataset.projectSkillSearch || input.dataset.pageSearch)) input.dispatchEvent(new Event("input", { bubbles: true }));
});
app.addEventListener("keydown", (ev) => {
  if (ev.key === "Enter" && !ev.isComposing && ev.keyCode !== 229 && (ev.target as HTMLInputElement).dataset.projectSkillSearch) ev.preventDefault();
});
app.addEventListener("change", async (ev) => {
  const input = ev.target as HTMLInputElement;
  if (input.name === "bulk-skill") {
    if (hasPendingMutation()) { render(); return; }
    if (input.checked) bulkSkillSelection.add(input.value); else bulkSkillSelection.delete(input.value);
    render();
    [...app.querySelectorAll<HTMLInputElement>('[name="bulk-skill"]')].find((item) => item.value === input.value)?.focus({ preventScroll: true });
  }
  if (input.name === "bulk-skills-all") {
    if (hasPendingMutation()) { render(); return; }
    bulkSkillSelection = changeSelection(bulkSkillSelection, filteredSkills().map((skill) => skill.id), "toggle-all");
    render();
    app.querySelector<HTMLInputElement>('[name="bulk-skills-all"]')?.focus({ preventScroll: true });
  }
  if (input.name === "skills-status") {
    skillsFilter = input.value as SkillsFilter;
    render();
    if (detailTab === "document" && selectedSkill) void loadSkillDocument(selectedSkill);
  }
  if (input.name === "skills-source") {
    skillsSourceFilter = input.value; render();
    if (detailTab === "document" && selectedSkill) void loadSkillDocument(selectedSkill);
  }
  if (input.name === "skills-sort") {
    skillsSort = input.value === "desc" ? "desc" : "asc";
    render();
  }
  if (input.form?.id === "settings-editor") {
    if (settingsControlsBusy()) { render(); return; }
    const settings: AppSettings = { ...(settingsDraft || data!.state.settings), deploymentMode: field(input.form, "deploymentMode") === "symlink" ? "symlink" : "copy", defaultProjectDirectories: selectedIDs(input.form, "defaultProjectDirectories"), defaultGlobalDirectories: selectedIDs(input.form, "defaultGlobalDirectories") };
    if (!settings.defaultProjectDirectories.length || !settings.defaultGlobalDirectories.length) {
      input.checked = true;
      toast(t("项目与全局默认目录都需要至少选择一个。"), true);
      return;
    }
    const key = input.name as keyof AppSettings;
    if (!["deploymentMode", "defaultProjectDirectories", "defaultGlobalDirectories"].includes(key)) return;
    settingsDraft = settings;
    settingsWriteRevision++;
    settingsAutosave.save({ [key]: settings[key] });
  }
  if (input.name === "interface-font") {
    savePreference({ interfaceFont: input.value, interfaceFontStyle: defaultFontStyle(input.value) });
    applyInterfaceSettings(settingsDraft || data!.state.settings);
    render();
  }
  if (input.name === "interface-font-style") {
    savePreference({ interfaceFontStyle: input.value });
    applyInterfaceSettings(settingsDraft || data!.state.settings);
  }
  if (["snapshotRetentionDays", "snapshotMaxCount"].includes(input.name)) {
    if (!input.reportValidity()) return;
    if (settingsControlsBusy()) { render(); return; }
    savePreference({ [input.name]: input.valueAsNumber });
  }
  if (["lightweight", "open-at-login"].includes(input.name)) {
    const on = input.checked;
    input.disabled = true;
    const next = await task("正在保存设置…", () => input.name === "lightweight" ? api.setLightweight(on) : api.setOpenAtLogin(on));
    if (next) accept(next); else render();
  }
  if (input.name === "locale-preference") { setLocale(input.value as LocalePreference); render(); }
  if (input.name === "global-directory-selection") {
    if (applicationBusy() || globalDirectorySelectionBlocked()) { render(); return; }
    if (input.checked) selectedGlobalKeys.add(input.value); else selectedGlobalKeys.delete(input.value);
    selectedGlobalKey = globalBatchID();
    render();
    [...document.querySelectorAll<HTMLInputElement>('[name="global-directory-selection"]')].find((item) => item.value === input.value)?.focus({ preventScroll: true });
  }
  if (input.form?.classList.contains("application-selection") && ["application-skill", "application-preset"].includes(input.name)) {
    const form = input.form;
    const scope = form.dataset.scope as ApplicationScope;
    const id = form.dataset.id!;
    if (applicationBusy()) { render(); return; }
    saveApplicationSelection(scope, id, { skillIds: selectedIDs(form, "application-skill"), presetIds: selectedIDs(form, "application-preset") });
    const name = input.name, value = input.value;
    render();
    const replacement = [...document.querySelectorAll<HTMLInputElement>(".application-selection input")].find((item) => item.form?.dataset.scope === scope && item.form?.dataset.id === id && item.name === name && item.value === value);
    replacement?.focus({ preventScroll: true });
  }
  if (input.name === "candidate") {
    if (input.checked) selectedCandidates.add(input.value); else selectedCandidates.delete(input.value);
    const count = document.querySelector("#candidate-count");
    if (count) count.textContent = t("已选择 {0} 个技能", { 0: selectedCandidates.size });
    const install = document.querySelector<HTMLButtonElement>('[data-action="install"]');
    if (install) install.disabled = !selectedCandidates.size;
    const selectAll = document.querySelector<HTMLButtonElement>('[data-action="select-all-candidates"]');
    if (selectAll) selectAll.textContent = selectedCandidates.size === arr(preview?.candidates).length ? t("取消全选") : t("全选");
  }
  if (input.name === "source-candidate") {
    const selection = sourceSelection(input.dataset.source!);
    const source = sources().find((item) => item.id === input.dataset.source);
    const selectable = source && !source.lastError && source.mode === "selected" && sourceEntries(source).some((entry) => entry.item.subpath === input.value && (entry.kind === "install" || entry.kind === "update"));
    if (input.checked && selectable) selection.add(input.value); else selection.delete(input.value);
    if (source) {
      const available = sourceEntries(source).filter((entry) => entry.kind === "install" || entry.kind === "update");
      const count = available.filter((entry) => selection.has(entry.item.subpath)).length;
      const label = document.querySelector("#source-sync-count");
      if (label) label.textContent = t("已选择 {0} 个技能", { 0: count });
      const sync = document.querySelector<HTMLButtonElement>('[data-action="sync-source"]');
      if (sync) sync.disabled = !count || !!source.lastError;
      const selectAll = document.querySelector<HTMLButtonElement>('[data-action="select-all-source"]');
      if (selectAll) selectAll.textContent = available.every((entry) => selection.has(entry.item.subpath)) ? t("取消全选") : t("全选可同步");
    }
  }
});
app.addEventListener("toggle", (ev) => {
  const details = ev.target;
  if (!(details instanceof HTMLDetailsElement)) return;
  if (!details.dataset.projectDisclosure) return;
  const open = projectDetailsOpen.get(details.dataset.id!) || new Set<string>();
  if (details.open) open.add(details.dataset.projectDisclosure); else open.delete(details.dataset.projectDisclosure);
  projectDetailsOpen.set(details.dataset.id!, open);
}, true);
modalRoot.addEventListener("change", (ev) => {
  if (["project-directory", "project-custom-path"].includes((ev.target as HTMLInputElement).name)) updateProjectDestination();
});
modalRoot.addEventListener("input", (ev) => {
  const input = ev.target as HTMLInputElement;
  if (input.name === "preset-search") { presetFilters.query = input.value; renderPresetSkills(); }
  if (input.name === "path") updateProjectDestination();
  if (input.name === "skillsManagerPath") invalidateSkillsManagerPreview();
});
modalRoot.addEventListener("change", (ev) => {
  const input = ev.target as HTMLInputElement;
  if (input.form?.id !== "preset-editor") return;
  if (input.name === "skill") {
    if (input.checked) presetSkillSelection.add(input.value); else presetSkillSelection.delete(input.value);
  } else if (input.name === "preset-source") presetFilters.source = input.value;
  else return;
  renderPresetSkills();
});
document.addEventListener("submit", async (ev) => {
  const form = ev.target as HTMLFormElement;
  const formID = form.getAttribute("id") || "";
  if (formID === "settings-editor" || form.classList.contains("application-selection")) { ev.preventDefault(); return; }
  if (!["hub-search", "source-preview", "local-preview", "source-editor", "source-relink", "preset-editor", "project-editor", "global-directory-editor", "project-directory-editor", "backup-import", "backup-export", "skills-manager-import", "cloud-sync-editor", "cloud-conflicts-editor"].includes(formID) && !form.classList.contains("application-selection")) return;
  ev.preventDefault();
  const submit = form.querySelector<HTMLButtonElement>('button[type="submit"]');
  if (submit?.disabled) return;
  if (submit) submit.disabled = true;
  try {
    if (formID === "cloud-sync-editor") {
      const config = readCloudSyncConfig(form, data?.cloudSync?.config);
      const next = await task("Saving sync settings…", () => api.saveCloudSyncConfig(config));
      if (next) { accept(next); closeSubmittedModal(form); toast(t("Sync settings saved. You can now sync this device.")); }
    } else if (formID === "cloud-conflicts-editor") {
      const resolutions = readCloudResolutions(form, cloudConflictDraft);
      if (!resolutions) { toast(t("Choose a version for every conflict."), true); return; }
      const next = await task("Resolving sync conflicts…", () => api.resolveCloudSync(resolutions));
      if (next) { accept(next); closeSubmittedModal(form); toast(t(next.cloudSync.conflicts?.length ? "Cloud changed again. Review the latest conflicts." : "Two-way sync completed.")); }
    } else if (formID === "hub-search") {
      await searchHub(field(form, "query"));
    } else if (formID === "source-preview") {
      await doPreview(field(form, "source"), field(form, "ref"), field(form, "subpath"));
    } else if (formID === "local-preview") {
      await doPreview(field(form, "localSource"), "", "", "", "local");
    } else if (formID === "source-editor") {
      const id = field(form, "entityId");
      const existing = sources().find((source) => source.id === id);
      const source: Source = { id, name: field(form, "name"), repository: field(form, "repository"), ref: field(form, "ref"), subpath: field(form, "subpath"), mode: field(form, "mode") === "all" ? "all" : "selected", revision: existing?.revision || "", refreshedAt: existing?.refreshedAt || "", lastError: existing?.lastError || "", catalog: arr(existing?.catalog), warnings: existing?.warnings };
      const previousIDs = new Set(sources().map((item) => item.id));
      const next = await task("正在保存来源…", () => api.saveSource(source, sourceEditorBaselines.get(form) ?? null));
      if (next) {
        if (existing && existing.mode !== source.mode) sourceSelections.delete(id);
        selectedSource = id || arr(next.state.sources).find((item) => !previousIDs.has(item.id))?.id || "";
        currentPage = "sources"; pageQueries.sources = "";
        accept(next); closeSubmittedModal(form);
        toast(existing ? t("来源已保存，点击同步时使用新的安装方式。") : t("来源已保存，刷新后查看技能。"));
      }
    } else if (formID === "source-relink") {
      const id = field(form, "sourceId");
      if (await mutate("正在重新关联技能…", () => api.relinkSourceSkill(field(form, "skillId"), id, field(form, "subpath")), t("技能已关联并更新，预设引用已保留，相关项目与全局配置需手动应用。"))) { updates = null; closeSubmittedModal(form); }
    } else if (formID === "preset-editor") {
      const preset: Preset = { id: field(form, "entityId"), name: field(form, "name"), icon: field(form, "icon"), description: field(form, "description"), skillIds: [...presetSkillSelection] };
      const previousIDs = new Set(presets().map((item) => item.id));
      const next = await task("正在保存预设…", () => api.savePreset(preset, presetEditorBaselines.get(form) ?? null));
      if (next) {
        const showResult = form.isConnected && modalRoot.contains(form);
        if (showResult) {
          selectedPreset = preset.id || arr(next.state.presets).find((item) => !previousIDs.has(item.id))?.id || "";
          currentPage = "presets"; pageQueries.presets = "";
        }
        accept(next); closeSubmittedModal(form);
        toast(t("预设已保存，相关项目与全局配置需手动应用。"));
      }
    } else if (formID === "project-editor") {
      if (applicationBusy()) { toast(t("工作空间正在更新，请完成后保存项目。"), true); return; }
      await selectionAutosave.idle();
      const existing = projects().find((project) => project.id === field(form, "entityId"));
      const path = field(form, "path");
      if (!path) { toast(t("请选择项目根目录。"), true); return; }
      const name = field(form, "name") || projectRoot(path).replace(/\/+$/, "").split("/").pop() || t("项目");
      const directoryIds = selectedIDs(form, "project-directory");
      const customPaths = [...new Set(selectedIDs(form, "project-custom-path"))];
      if (!directoryIds.length && !customPaths.length) { toast(t("请至少选择一个项目技能目录。"), true); return; }
      const project: Target = { id: field(form, "entityId"), name, icon: field(form, "icon"), kind: "project", path, directoryIds, customPaths, enabledSkillIds: arr(existing?.enabledSkillIds), enabledPresetIds: arr(existing?.enabledPresetIds), deployments: arr(existing?.deployments), pending: existing?.pending || false };
      const previousIDs = new Set(projects().map((item) => item.id));
      const next = await task("正在保存项目…", async () => {
        try { return await api.saveProject(project); }
        catch (error) {
          const message = tMessage(error instanceof Error ? error.message : String(error));
          const feedback = form.querySelector<HTMLElement>("#project-path-error");
          if (feedback) { feedback.textContent = message; feedback.hidden = false; }
          form.querySelector<HTMLInputElement>('[name="path"]')?.setAttribute("aria-invalid", "true");
          throw error;
        }
      });
      if (next) {
        const showResult = form.isConnected && modalRoot.contains(form);
        const created = !existing ? arr(next.projects).find((item) => !previousIDs.has(item.id)) : undefined;
        if (showResult && created) { currentPage = "projects"; pageQueries.projects = ""; projectsFilter = "all"; selectedProject = created.id; }
        accept(next); closeSubmittedModal(form); toast(t("项目已保存，可以选择预设或补充技能。"));
        if (showResult && created) queueMicrotask(() => {
          if (currentPage !== "projects" || modalRoot.children.length) return;
          const card = [...document.querySelectorAll<HTMLElement>('.application-card[data-scope="project"]')].find((item) => item.dataset.id === created.id);
          card?.scrollIntoView({ behavior: "auto", block: "start" });
          const heading = card?.querySelector<HTMLElement>("h2");
          if (heading) { heading.tabIndex = -1; heading.focus({ preventScroll: true }); }
        });
      }
    } else if (formID === "project-directory-editor") {
      const randomID = globalThis.crypto?.randomUUID?.() || `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
      const id = field(form, "entityId") || `project-dir-${randomID}`;
      const directory = { id, name: field(form, "name"), path: field(form, "path") };
      if (await mutate("正在保存项目目录…", async () => {
        const settings = data!.state.settings;
        const existing = settings.customProjectDirectories.find((item) => item.id === id);
        if (absoluteProjectTemplatePath(directory.path) && directory.path !== existing?.path) throw new Error(t("项目目录必须使用相对项目根目录的路径。"));
        const customProjectDirectories = existing ? settings.customProjectDirectories.map((item) => item.id === id ? directory : item) : [...settings.customProjectDirectories, directory];
        return api.saveSettingsPatch({ customProjectDirectories });
      }, t("项目目录已保存。"))) closeSubmittedModal(form);
    } else if (formID === "global-directory-editor") {
      if (applicationBusy()) { toast(t("工作空间正在更新，请完成后保存目录。"), true); return; }
      await selectionAutosave.idle();
      const id = field(form, "entityId");
      const existing = globalApplications().find((item) => item.key === id && item.agentId === "custom")?.target;
      const target: Target = { id, name: field(form, "name"), kind: "global", path: field(form, "path"), enabledSkillIds: arr(existing?.enabledSkillIds), enabledPresetIds: arr(existing?.enabledPresetIds), deployments: arr(existing?.deployments), pending: existing?.pending || false };
      if (await mutate("正在保存全局目录…", () => api.saveGlobalDirectory(target), t("全局目录已保存。"))) closeSubmittedModal(form);
    } else if (formID === "skills-manager-import") {
      if (skillsManagerBusy()) return;
      const path = field(form, "skillsManagerPath");
      invalidateSkillsManagerPreview("正在读取数据库内容…");
      const result = await task("正在预览 skills-manager 数据库…", () => api.previewSkillsManager(path));
      if (form.isConnected && modalRoot.contains(form)) {
        if (result && field(form, "skillsManagerPath") === path) showSkillsManagerPreview(form, result);
        else invalidateSkillsManagerPreview(t("未能完成预览，请检查路径后重试。"));
      }
    } else if (formID === "backup-export") {
      const path = field(form, "backupPath");
      const result = await task("正在导出工作空间…", () => api.exportBackup(path));
      if (result) { closeSubmittedModal(form); toast(t("备份已导出：{0}", { 0: result })); }
    } else if (formID === "backup-import") {
      const path = field(form, "backupPath");
      const approved = await confirm(t("确认恢复备份"), t("将替换当前技能、来源与预设。本机项目与全局配置不会从备份导入，恢复后请为项目与全局重新选择技能并手动应用。"), `<p class="mono">${e(path)}</p>`, t("导入并恢复"), true);
      if (approved) {
        const next = await task("正在导入备份…", () => api.importBackup(path));
        if (next) { selectionAutosave.clear(); projectDrafts.clear(); globalDrafts.clear(); updates = null; accept(next); toast(t("工作空间已从备份恢复，请重新配置项目与全局技能。")); }
        else if (!modalRoot.children.length) backupEditor(true, path);
      } else if (!modalRoot.children.length) backupEditor(true, path);

    }
  } finally { if (submit) submit.disabled = false; }
});
// Chapter links scroll only the document; native fragment navigation can also
// move overflow-hidden ancestors and hide the inspector's fixed controls.
app.addEventListener("click", (ev) => {
  const link = (ev.target as Element).closest<HTMLAnchorElement>('.skill-document .markdown-body a[href^="#"]');
  if (!link) return;
  ev.preventDefault();
  const scroller = link.closest<HTMLElement>(".skill-document")!;
  let id = link.getAttribute("href")!.slice(1);
  try { id = decodeURIComponent(id); } catch { return; }
  const heading = scroller.querySelector<HTMLElement>(`#${CSS.escape(id)}`);
  if (!heading) return;
  scroller.scrollTo({ top: heading.offsetTop - (parseFloat(getComputedStyle(scroller).paddingTop) || 0) });
  heading.tabIndex = -1;
  heading.focus({ preventScroll: true });
});
document.addEventListener("click", async (ev) => {
  const target = (ev.target as Element).closest<HTMLButtonElement>("button");
  if (!target || target.disabled) return;
  if (target.dataset.navLayoutPreference) { changeNavigationLayout(() => setNavLayoutPreference(target.dataset.navLayoutPreference as NavLayoutPreference)); return; }
  if (target.dataset.action === "toggle-sidebar") { changeNavigationLayout(toggleNavLayout); return; }
  if (target.dataset.themePreference) { setThemePreference(target.dataset.themePreference as ThemePreference); render(); return; }
  if (target.dataset.page) {
    currentPage = target.dataset.page as Page; render(); ensureHubResults();
    if (currentPage === "skills" && detailTab === "document" && selectedSkill) void loadSkillDocument(selectedSkill);
    return;
  }
  const action = target.dataset.action;
  const id = target.dataset.id || "";
  if (!action) return;
  if (action === "open-cloud-guide") { guideTab = "sync"; currentPage = "guide"; render(); document.querySelector<HTMLElement>("#guide-tab-panel")?.focus({ preventScroll: true }); return; }
  if (action === "guide-tab") {
    if (!guideTabs.some((tab) => tab.id === target.dataset.tab)) return;
    guideTab = target.dataset.tab as GuideTab;
    render();
    if (!target.isConnected) [...app.querySelectorAll<HTMLButtonElement>('.guide-tabs [aria-selected="true"], .guide-nav-sub [aria-pressed="true"]')].find((button) => button.getClientRects().length)?.focus({ preventScroll: true });
    return;
  }
  if (action === "set-text-size") {
    const size = Number(target.dataset.size) as AppSettings["textSize"];
    if (textSizes.includes(size!)) { savePreference({ textSize: size }); applyInterfaceSettings(settingsDraft || data!.state.settings); render(); }
    return;
  }
  if (action === "open-cloud-settings") { currentPage = "settings"; settingsBackups = true; render(); document.querySelector<HTMLElement>("#settings-tab-panel")?.focus({ preventScroll: true }); document.querySelector("#settings-cloud-title")?.scrollIntoView({ block: "start" }); return; }
  if (action === "configure-cloud-sync") { cloudSyncEditor(); return; }
  if (action === "cloud-conflicts") { cloudConflictsEditor(); return; }
  if (action === "cloud-backups") { await showCloudBackups(); return; }
  if (action === "test-cloud-sync") {
    const form = modalRoot.querySelector<HTMLFormElement>("#cloud-sync-editor");
    if (!form || !form.reportValidity()) return;
    const config = readCloudSyncConfig(form, data?.cloudSync?.config);
    const success = await task("Testing cloud connection…", async () => { await api.testCloudSyncConnection(config); return true; });
    if (success) toast(t("Connection successful. The bucket supports sync reads and writes."));
    return;
  }
  if (action === "sync-cloud") {
    const next = await task("Syncing with cloud…", () => api.syncCloud());
    if (next) {
      accept(next);
      if (next.cloudSync.conflicts?.length) cloudConflictsEditor();
      else toast(t("Two-way sync completed."));
    }
    return;
  }
  if (action === "restore-cloud-backup") {
    const backup = cloudBackupDraft.find((backup) => backup.id === id);
    if (!backup) return;
    const description = t("Restore {0} skills and {1} presets from {2}? A local snapshot is created first. Project paths and deployment files stay on this device; apply changes manually afterward.", { 0: backup.skillCount, 1: backup.presetCount, 2: date(backup.createdAt) });
    if (!await confirm(t("Restore cloud backup"), description, `<p class="mono">${e(backup.id)}</p>`, t("Restore and sync"), true)) { await showCloudBackups(); return; }
    const next = await task("Restoring cloud backup…", () => api.restoreCloudBackup(id));
    if (next) { accept(next); toast(t("Cloud backup restored and published as a new sync version. Review pending applications.")); }
    return;
  }
  if (action === "retry-selection") { if (!applicationBusy()) selectionAutosave.retry(applicationKey(target.dataset.scope as ApplicationScope, id)); return; }
  if (action === "select-skill") { selectedSkill = id; detailTab = "overview"; render(); return; }
  if (action === "select-source") { selectedSource = id; render(); return; }
  if (action === "select-preset") { selectedPreset = id; render(); return; }
  if (action === "select-all-candidates") {
    if (!preview || activeTasks.has("正在导入所选技能…")) return;
    selectedCandidates = selectedCandidates.size === arr(preview.candidates).length ? new Set() : new Set(arr(preview.candidates).map((candidate) => candidate.subpath));
    render(); return;
  }
  if (action === "select-filtered-preset-skills") {
    if (!modalRoot.querySelector("#preset-editor") || activeTasks.has("正在保存预设…")) return;
    presetSkillSelection = changeSelection(presetSkillSelection, filteredSkills(presetFilters).map((skill) => skill.id), "toggle-all");
    renderPresetSkills(); return;
  }
  if (action === "select-project") { selectedProject = id; render(); return; }
  if (action === "filter-projects" || action === "clear-project-filters") {
    projectsFilter = action === "filter-projects" && target.dataset.filter === "pending" ? "pending" : "all";
    if (action === "clear-project-filters") pageQueries.projects = "";
    render(); return;
  }
  if (action === "manage-global-directories") {
    currentPage = "settings"; settingsBackups = false; render();
    document.querySelector("#settings-global-title")?.closest(".settings-row")?.scrollIntoView({ block: "center" });
    document.querySelector<HTMLButtonElement>('[data-action="new-global-directory"]')?.focus({ preventScroll: true });
    return;
  }
  if (action === "filter-global-directories" || action === "clear-global-filters") {
    globalFilter = action === "filter-global-directories" && target.dataset.filter === "pending" ? "pending" : "all";
    if (action === "clear-global-filters") pageQueries.global = "";
    render(); return;
  }
  if (action === "filter-project-skills") {
    const filters = projectSkillFilters.get(id) || { query: "", selectedOnly: false };
    projectSkillFilters.set(id, { ...filters, selectedOnly: !filters.selectedOnly });
    render(); return;
  }
  if (action === "open-backups" || action === "close-backups") {
    settingsBackups = action === "open-backups";
    render();
    if (!target.isConnected) [...app.querySelectorAll<HTMLButtonElement>('.settings-tabs [aria-selected="true"], .settings-nav-sub [aria-pressed="true"]')].find((button) => button.getClientRects().length)?.focus({ preventScroll: true });
    return;
  }
  if (action === "clear-selected-skills") { bulkSkillSelection.clear(); render(); return; }
  if (action === "retry-preview" && previewRequest && !previewRequest.loading) {
    const { source, ref, subpath, matchName, kind } = previewRequest;
    await doPreview(source, ref, subpath, matchName, kind); return;
  }
  if (action === "load-more-hub") { await searchHub("", true); return; }
  if (action === "discover-tab") {
    if (discoveryBusy() || !["hub", "git", "local"].includes(target.dataset.tab || "")) return;
    discoverTab = target.dataset.tab as DiscoverTab;
    render(); ensureHubResults();
    if (!target.isConnected) document.querySelector<HTMLButtonElement>(`#discover-tab-${discoverTab}`)?.focus({ preventScroll: true });
    return;
  }
  if (action === "source-filter") { sourceCatalogFilter = target.dataset.filter === "all" ? "all" : "changes"; render(); return; }
  if (action === "toggle-metadata-wrap") {
    ev.preventDefault();
    metadataWrap = !metadataWrap;
    target.setAttribute("aria-pressed", String(metadataWrap));
    target.closest<HTMLElement>(".markdown-frontmatter")!.dataset.wrap = String(metadataWrap);
    return;
  }
  if (action === "toggle-document-raw") {
    if (rawDocuments.has(id)) rawDocuments.delete(id); else rawDocuments.add(id);
    render();
    return;
  }
  if (action === "detail-tab") {
    detailTab = target.dataset.tab as "overview" | "document"; render();
    if (detailTab === "document") void loadSkillDocument(selectedSkill);
    return;
  }
  if (["plan-project", "apply-project", "plan-global", "apply-global"].includes(action)) {
    if (applicationBusy()) { toast(t("工作空间正在更新，请完成后重新预览并应用。"), true); return; }
    const scope: ApplicationScope = action.endsWith("global") ? "global" : "project";
    const desired = findApplication(scope, id);
    const key = applicationKey(scope, id);
    // A multi-directory view can start with differing saved selections. Save
    // the displayed union before previewing it, without changing deployed files.
    if (desired && applicationDirty(scope, id, desired.target) && !selectionAutosave.state(key).error) saveApplicationSelection(scope, id, applicationSelection(scope, id, desired.target));
    try { await selectionAutosave.flush(key); }
    catch { toast(t("选择未能自动保存，请重试后再预览或应用。"), true); return; }
    await selectionAutosave.idle();
    if (applicationBusy()) { toast(t("工作空间正在更新，请完成后重新预览并应用。"), true); return; }
    const saved = findApplication(scope, id);
    if (!saved?.target || applicationDirty(scope, id, saved.target)) { toast(t("选择未能自动保存，请重试后再预览或应用。"), true); return; }
  }
  if (action === "select-global-directories" || action === "default-global-directories") {
    if (applicationBusy() || globalDirectorySelectionBlocked()) return;
    selectedGlobalKeys = new Set(action === "select-global-directories" ? globalApplications().every((item) => selectedGlobalKeys.has(item.key)) ? [] : globalApplications().map((item) => item.key) : data?.state.settings.defaultGlobalDirectories || []);
    selectedGlobalKey = globalBatchID(); render(); return;
  }
  if (action === "clear-application") {
    const scope = target.dataset.scope as ApplicationScope;
    if (applicationBusy()) return;
    saveApplicationSelection(scope, id, { skillIds: [], presetIds: [] });
    return;
  }
  if (action === "close-modal") { closeModal(); return; }
  if (action === "confirm-modal") { closeModal(true); return; }
  target.disabled = true;
  try {
    if (action === "reload") await load();
    else if (action === "go-discover") { currentPage = "discover"; render(); ensureHubResults(); }
    else if (action === "open-source") openSource(id);
    else if (action === "new-source" || action === "edit-source") sourceEditor(id);
    else if (action === "select-all-source") {
      const source = sources().find((item) => item.id === id);
      if (source && source.mode === "selected" && !source.lastError) {
        const paths = sourceEntries(source).filter((entry) => entry.kind === "install" || entry.kind === "update").map((entry) => entry.item.subpath);
        const selection = sourceSelection(id);
        sourceSelections.set(id, paths.every((path) => selection.has(path)) ? new Set() : new Set(paths));
        render();
      }
    } else if (action === "refresh-source") {
      const next = await task("正在刷新来源…", () => api.refreshSource(id));
      if (next) {
        updates = null; accept(next);
        const source = sources().find((item) => item.id === id);
        if (source?.lastError) toast(t("刷新失败：{0}", { 0: tMessage(source.lastError) }), true);
        else if (source && sourceSummary(source).skipped) toast(t("来源已部分刷新，跳过的技能已保留；其他技能可继续同步。"));
        else toast(t("来源已刷新。查看变化后同步本地技能。"));
      }
    } else if (action === "sync-source") {
      const source = sources().find((item) => item.id === id);
      if (source && source.revision && !source.lastError) {
        const paths = sourceSyncPaths(source, sourceEntries(source), sourceSelection(id));
        const next = await task("正在同步来源中的技能…", () => api.syncSource(id, source.revision, paths));
        if (next) { updates = null; sourceSelections.delete(id); accept(next); toast(t("技能已同步。上游移除的技能已保留，相关项目与全局配置需手动应用。")); }
      }
    } else if (action === "delete-source") {
      const source = sources().find((item) => item.id === id);
      if (source && await confirm(t("删除来源"), t("确定删除“{0}”？已安装技能及其预设引用会保留，并解除与此来源的关联。", { 0: sourceDisplayName(source) }), "", t("删除来源"), true)) {
        updates = null;
        await mutate("正在删除来源…", () => api.deleteSource(id), t("来源已删除，已安装技能已保留。"));
      }
    } else if (action === "relink-source-skill") {
      const source = sources().find((item) => item.id === id);
      if (source) relinkSourceEditor(source, target.dataset.subpath || "");
    } else if (action === "view-local-skill") { selectedSkill = id; skillsQuery = ""; skillsFilter = "all"; skillsSourceFilter = "all"; currentPage = "skills"; render(); if (detailTab === "document") void loadSkillDocument(id); }
    else if (action === "read-document") await loadSkillDocument(id);
    else if (action === "new-preset" || action === "edit-preset") presetEditor(id);
    else if (action === "new-project" || action === "edit-project") projectEditor(id);
    else if (action === "new-global-directory" || action === "edit-global-directory") globalDirectoryEditor(id);
    else if (action === "new-project-directory" || action === "edit-project-directory") projectDirectoryEditor(id);
    else if (action === "check-updates") {
      const result = await task("正在检查技能更新…", () => api.checkUpdates());
      if (result) {
        updates = result;
        const next = await task("正在读取刷新结果…", () => api.state());
        if (next) accept(next, true); else render();
        const available = result.filter((update) => update.available && !update.removed && !update.error).length;
        const removed = result.filter((update) => update.removed).length;
        toast(result.some((update) => update.error) ? t("更新检查完成，部分来源检查失败。") : available ? t("发现 {0} 个技能更新{1}。", { 0: available, 1: removed ? t("，{0} 个技能已被上游移除并保留本地版本", { 0: removed }) : "" }) : removed ? t("{0} 个技能已被上游移除，本地版本已保留。", { 0: removed }) : t("技能更新检查完成，可到来源查看新增技能。"));
      }
    } else if (action === "update-skill") {
      if (hasPendingMutation()) { toast(t("工作空间正在更新，请完成后重新检查技能更新。"), true); return; }
      const revision = stateRevision;
      const update = updates?.find((u) => u.skillId === id);
      if (!update?.available || update.removed || update.error) return;
      if (await confirm(t("更新技能"), t("更新只修改本地技能，项目与全局配置需要另行手动应用。"), `<p>${e(tMessage(update.summary) || t("将更新到版本 {0}", { 0: short(update.revision) }))}</p>`, t("更新技能"))) {
        if (revision !== stateRevision || hasPendingMutation()) { toast(t("技能来源已变化，请重新检查更新。"), true); return; }
        if (await mutate("正在更新技能…", () => api.updateSkill(id), t("技能已更新，相关项目与全局配置需手动应用。"))) updates = updates?.filter((u) => u.skillId !== id) || null;
        render();
      }
    } else if (action === "update-selected-skills" || action === "remove-selected-skills") await operateSelectedSkills(action === "update-selected-skills" ? "update" : "remove");
    else if (action === "remove-skill") {
      const s = skills().find((s) => s.id === id);
      if (presets().some((preset) => arr(preset.skillIds).includes(id)) || managedApplications().some((application) => arr(application.enabledSkillIds).includes(id))) {
        toast(t("此技能仍在使用。请先从相关预设、项目或全局选择中取消它，再从本地移除。"), true); return;
      }
      if (s && await confirm(t("移除技能"), t("确定移除本地技能“{0}”？已应用的旧版本将在下次应用时移除。", { 0: s.name }), "", t("移除技能"), true)) await mutate("正在移除技能…", () => api.removeSkill(id), t("技能已移除。"));
    } else if (action === "hub-preview") {
      const r = searchResults?.[Number(target.dataset.index)];
      if (r && r.supported !== false) await doPreview(r.url || r.source, "", "", r.slug?.split("/").pop() || r.name);
    } else if (action === "install" && preview && selectedCandidates.size) {
      if (await mutate("正在导入所选技能…", () => api.install(preview!.id, [...selectedCandidates]), t("所选技能已导入本地。"))) { preview = null; selectedCandidates.clear(); currentPage = "skills"; render(); }
    } else if (action === "delete-preset") {
      const p = presets().find((p) => p.id === id);
      if (p && await confirm(t("删除预设"), t("确定删除“{0}”？技能原件会保留，相关项目与全局配置需重新应用。", { 0: p.name }), "", t("删除预设"), true)) await mutate("正在删除预设…", () => api.deletePreset(id), t("预设已删除。") );
    } else if (action === "delete-project") {
      if (applicationBusy()) { toast(t("工作空间正在更新，请完成后移除项目。"), true); return; }
      const project = projects().find((item) => item.id === id);
      if (project && arr(project.deployments).length) { toast(t("请先清空该项目的技能与预设选择并应用以移除已应用技能，再移除项目。"), true); return; }
      if (project && await confirm(t("移除项目"), t("确定停止管理“{0}”？此项目当前没有由 racomi 应用的技能。", { 0: project.name }), "", t("移除项目"), true)) {
        if (applicationBusy()) { toast(t("工作空间正在更新，请完成后移除项目。"), true); return; }
        if (await mutate("正在移除项目…", () => api.deleteProject(id), t("项目已移除。"))) { projectDrafts.delete(id); selectionAutosave.forget(applicationKey("project", id)); }
      }
    } else if (action === "delete-project-directory") {
      const directory = data?.state.settings.customProjectDirectories.find((item) => item.id === id);
      if (!directory) return;
      if (await confirm(t("删除项目目录"), t("删除后不再出现在可选目录中，现有项目保留已保存的路径。"), `<p><strong>${e(directory.name)}</strong></p><p class="mono">${e(directory.path)}</p>`, t("删除目录"), true)) {
        await mutate("正在删除项目目录…", async () => {
          const settings = data!.state.settings;
          return api.saveSettingsPatch({ customProjectDirectories: settings.customProjectDirectories.filter((item) => item.id !== id) });
        }, t("项目目录已删除。"));
      }
    } else if (action === "delete-global-directory") {
      if (applicationBusy()) { toast(t("工作空间正在更新，请完成后删除目录。"), true); return; }
      const application = globalApplications().find((item) => item.key === id && item.agentId === "custom");
      if (!application) return;
      if (arr(application.target?.deployments).length) { toast(t("请先清空该目录的技能与预设选择并应用，再删除目录。"), true); return; }
      if (await confirm(t("删除全局目录"), t("确定停止管理“{0}”？此目录当前没有由 racomi 应用的技能。", { 0: application.name }), `<p class="mono" title="${e(application.path)}">${e(globalPath(application.path))}</p>`, t("删除目录"), true)) {
        if (applicationBusy()) { toast(t("工作空间正在更新，请完成后删除目录。"), true); return; }
        const next = await task("正在删除全局目录…", () => api.deleteGlobalDirectory(id));
        if (next) {
          selectedGlobalKeys.delete(id);
          for (const key of globalDrafts.keys()) if (globalBatchKeys(key).includes(id)) { globalDrafts.delete(key); selectionAutosave.forget(applicationKey("global", key)); }
          accept(next); toast(t("全局目录已删除。"));
        }
      }
    } else if (["plan-project", "apply-project", "plan-global", "apply-global"].includes(action)) {
      const scope: ApplicationScope = action.endsWith("global") ? "global" : "project";
      const application = findApplication(scope, id);
      if (!application?.target) return;
      const revision = stateRevision;
      const dialogGeneration = modalGeneration;
      const plan = await task(scope === "project" ? "正在检查项目变化…" : "正在检查全局变化…", () => scope === "project" ? api.planProject(id) : api.planGlobals(globalBatchKeys(id)));
      if (!plan) return;
      const current = findApplication(scope, id);
      if (revision !== stateRevision || applicationBusy() || !current || applicationDirty(scope, id, current.target)) { toast(t("工作空间已变化，请重新预览后应用。"), true); return; }
      plans.set(applicationKey(scope, id), plan); render();
      if (action.startsWith("plan-")) return;
      if (modalGeneration !== dialogGeneration || currentPage !== (scope === "project" ? "projects" : "global") || (scope === "global" ? selectedGlobalKey !== id : visibleProject !== id)) { toast(t("预览已完成，请回到相应配置确认应用。")); return; }
      const disposition = applicationPlanDisposition(plan);
      if (disposition === "blocked") { toast(t("技能目录存在冲突，请先处理预览中列出的文件。"), true); return; }
      if (!hasApplicationChanges(plan)) { toast(t("没有需要应用的变化。")); return; }
      const force = disposition === "overwrite";
      const label = force ? t("强制覆盖冲突内容") : scope === "project" ? t("应用到项目") : t("应用到全局");
      const description = force ? t("将为“{0}”强制应用以下变化，并覆盖或删除列出的现有内容。", { 0: application.name }) : t("将为“{0}”应用以下技能变化。", { 0: application.name });
      if (await confirm(label, description, `<div class="application-confirm"><strong>${e(application.name)}</strong><code title="${e(application.path)}">${e(scope === "global" ? application.path.split("\n").map(globalPath).join("\n") : application.path)}</code></div>${planView(plan, scope)}`, force ? t("确认强制覆盖") : t("确认应用"), force)) {
        const latest = findApplication(scope, id);
        if (revision !== stateRevision || applicationBusy() || !latest || applicationDirty(scope, id, latest.target)) { toast(t("工作空间已变化，请重新预览后应用。"), true); return; }
        await mutate(scope === "project" ? "正在应用技能到项目…" : "正在应用技能到全局…", () => {
          const saved = findApplication(scope, id);
          if (revision !== stateRevision || !saved || applicationDirty(scope, id, saved.target)) throw new Error(t("工作空间已变化，请重新预览后应用。"));
          if (force) return scope === "project" ? api.forceApplyProject(id, plan) : api.forceApplyGlobals(globalBatchKeys(id), plan);
          return scope === "project" ? api.applyProject(id) : api.applyGlobals(globalBatchKeys(id));
        }, t("{0} 的技能已应用。", { 0: application.name }));
      }
    } else if (action === "choose-project-dir") {
      const input = modalRoot.querySelector<HTMLInputElement>('#project-editor input[name="path"]');
      const path = await task("正在选择项目目录…", () => api.chooseDirectory());
      if (path && input?.isConnected) { input.value = path; updateProjectDestination(); }
    } else if (action === "choose-global-dir") {
      const input = modalRoot.querySelector<HTMLInputElement>('#global-directory-editor input[name="path"]');
      const path = await task("正在选择技能目录…", () => api.chooseDirectory());
      if (path && input?.isConnected) { input.value = path; updateProjectDestination(); }
    } else if (action === "choose-local-dir") {
      const input = app.querySelector<HTMLInputElement>('#local-preview input[name="localSource"]');
      const path = await task("正在选择技能目录…", () => api.chooseDirectory());
      if (path && input?.isConnected) { localSourceDraft = path; input.value = path; input.focus({ preventScroll: true }); }
    } else if (action === "create-snapshot") await mutate("正在创建本地快照…", () => api.createSnapshot(), t("本地快照已创建。") );
    else if (action === "restore-snapshot") {
      if (await confirm(t("恢复本地快照"), t("将恢复技能、来源与预设，并清空项目与全局已保存的技能选择。已应用的技能文件不会直接改动，之后请重新选择并应用。"), "", t("恢复快照"), true)) {
        const next = await task("正在恢复快照…", () => api.restoreSnapshot(id));
        if (next) { selectionAutosave.clear(); projectDrafts.clear(); globalDrafts.clear(); updates = null; accept(next); toast(t("快照已恢复，请为项目与全局重新选择技能并手动应用。")); }
      }
    } else if (action === "import-skills-manager") await skillsManagerEditor();
    else if (action === "choose-skills-manager-file") {
      if (skillsManagerBusy()) return;
      const input = modalRoot.querySelector<HTMLInputElement>('input[name="skillsManagerPath"]');
      const path = await task("正在选择 skills-manager 数据库…", () => api.chooseSkillsManagerFile());
      if (path && input?.isConnected) { input.value = path; invalidateSkillsManagerPreview(); }
    } else if (action === "confirm-skills-manager-import") {
      const form = modalRoot.querySelector<HTMLFormElement>("#skills-manager-import");
      const preview = skillsManagerPreview;
      if (!form || !preview || skillsManagerBusy()) return;
      if (hasPendingMutation()) { toast(t("工作空间正在更新，请完成后再合并导入。"), true); return; }
      const imported = await task(skillsManagerImportTask, () => api.importSkillsManager(preview.id));
      if (imported) {
        skillsManagerImportResult = imported.result;
        accept(imported.app); closeSubmittedModal(form);
        const result = imported.result;
        toast(t("导入完成：新增 {0} 个技能、{1} 个预设、{2} 个来源；跳过 {3} 个重复项。", { 0: result.addedSkills, 1: result.addedPresets, 2: result.addedSources, 3: result.skippedSkills + result.skippedPresets + result.skippedSources }));
      }
    } else if (action === "export-backup") backupEditor(false);
    else if (action === "import-backup") backupEditor(true);
    else if (action === "choose-backup-file") {
      const importing = target.dataset.mode === "import";
      const input = modalRoot.querySelector<HTMLInputElement>('input[name="backupPath"]');
      const path = await task("正在选择备份文件…", () => importing ? api.chooseImportFile() : api.chooseExportFile());
      if (path && input?.isConnected) input.value = path;
    }
  } finally { target.disabled = false; }
});
async function operateSelectedSkills(operation: "update" | "remove") {
  if (hasPendingMutation()) { toast(t("工作空间正在更新，请完成后重新检查技能更新。"), true); return; }
  const updateIDs = updatedSkillIDs();
  const list = skills().filter((skill) => bulkSkillSelection.has(skill.id) && (operation === "remove" || updateIDs.has(skill.id)));
  if (!list.length) return;
  if (operation === "remove") {
    const used = list.filter((skill) => presets().some((preset) => arr(preset.skillIds).includes(skill.id)) || managedApplications().some((application) => arr(application.enabledSkillIds).includes(skill.id)));
    if (used.length) { toast(t("以下技能仍在使用，请先从预设、项目或全局选择中取消：{0}", { 0: used.map((skill) => skill.name).join("、") }), true); return; }
  }
  const revision = stateRevision;
  const update = operation === "update";
  const title = update ? t("批量更新技能") : t("批量移除技能");
  const description = update ? t("更新所选的 {0} 个可更新技能，相关项目与全局配置需另行手动应用。", { 0: list.length }) : t("确定移除所选的 {0} 个本地技能？已应用的旧版本将在下次应用时移除。", { 0: list.length });
  const content = `<ul class="bulk-confirm-list">${list.map((skill) => `<li>${e(skill.name)}</li>`).join("")}</ul>`;
  if (!await confirm(title, description, content, update ? t("更新所选") : t("移除所选"), !update)) return;
  if (revision !== stateRevision || hasPendingMutation()) { toast(t("工作空间已变化，请重新选择技能后操作。"), true); return; }
  const ids = list.map((skill) => skill.id);
  const next = await task(update ? "正在更新技能…" : "正在移除技能…", () => update ? api.updateSkills(ids) : api.removeSkills(ids));
  if (next) {
    if (update) updates = updates?.filter((item) => !ids.includes(item.skillId)) || null;
    for (const id of ids) bulkSkillSelection.delete(id);
    accept(next, update);
    toast(update ? t("已更新 {0} 个技能，相关项目与全局配置需手动应用。", { 0: ids.length }) : t("已移除 {0} 个技能。", { 0: ids.length }));
  }
}
function focusSearch() {
  const root = modalRoot.children.length ? modalRoot : app;
  const input = [...root.querySelectorAll<HTMLInputElement>(modalRoot.children.length ? 'input[type="search"], input[name="preset-search"]' : '.search-field input[data-page-search]')].find((item) => !item.disabled && item.getClientRects().length > 0);
  if (!input) return false;
  input.focus();
  input.select();
  return true;
}
document.addEventListener("racomi:nav-layout-change", () => { syncNavigationLayout(); savePreference({ navLayout: getNavLayoutPreference() }); });
document.addEventListener("racomi:locale-change", () => { savePreference({ locale: getLocalePreference() }); render(); });
document.addEventListener("racomi:theme-change", () => {
  savePreference({ theme: getThemePreference() });
  for (const button of document.querySelectorAll<HTMLButtonElement>("button[data-theme-preference]")) {
    const active = button.dataset.themePreference === getThemePreference();
    button.classList.toggle("active", active);
    if (button.closest(".settings-theme-options")) button.classList.toggle("selected", active);
    button.setAttribute("aria-pressed", String(active));
  }
});
document.addEventListener("keydown", (ev) => {
  if (ev.isComposing || ev.keyCode === 229) return;
  const mac = /Mac/.test(navigator.platform || navigator.userAgent);
  const sidebarShortcut = mac ? ev.ctrlKey && ev.metaKey && !ev.shiftKey : ev.ctrlKey && ev.shiftKey && !ev.metaKey;
  if (sidebarShortcut && !ev.altKey && !ev.repeat && ev.code === "KeyS" && !modalRoot.children.length) { ev.preventDefault(); changeNavigationLayout(toggleNavLayout); return; }
  if (ev.key === "Enter" && ev.target instanceof HTMLInputElement && ev.target.name === "preset-search" && ev.target.form?.id === "preset-editor") { ev.preventDefault(); return; }
  if ((mac ? ev.metaKey : ev.ctrlKey) && !ev.altKey && !ev.shiftKey && ev.key.toLowerCase() === "k" && focusSearch()) { ev.preventDefault(); return; }
  if (ev.key === "Escape" && modalRoot.children.length) { ev.preventDefault(); if (!activeTasks.has(skillsManagerImportTask)) closeModal(); return; }
  if (["ArrowLeft", "ArrowRight", "Home", "End"].includes(ev.key) && document.activeElement?.getAttribute("role") === "tab") {
    const tabs = [...document.activeElement.parentElement!.querySelectorAll<HTMLButtonElement>('[role="tab"]')];
    const current = tabs.indexOf(document.activeElement as HTMLButtonElement);
    const index = ev.key === "Home" ? 0 : ev.key === "End" ? tabs.length - 1 : (current + (ev.key === "ArrowLeft" ? -1 : 1) + tabs.length) % tabs.length;
    ev.preventDefault(); tabs[index]?.click();
    const activeTab = document.querySelector<HTMLButtonElement>(currentPage === "guide" ? '.guide-tabs [aria-selected="true"]' : currentPage === "discover" ? '.discover-tabs [aria-selected="true"]' : currentPage === "settings" ? '.settings-tabs [aria-selected="true"]' : '.detail-tabs [aria-selected="true"]');
    activeTab?.focus();
  }
  if (ev.key === "Tab" && modalRoot.children.length) {
    const focusable = [...modalRoot.querySelectorAll<HTMLElement>('button:not(:disabled),input:not([type="hidden"]):not(:disabled),textarea:not(:disabled),select:not(:disabled),summary')].filter((element) => element.tabIndex >= 0 && element.getClientRects().length > 0);
    const first = focusable[0], last = focusable[focusable.length - 1];
    if (ev.shiftKey && (document.activeElement === first || !modalRoot.contains(document.activeElement))) { ev.preventDefault(); last?.focus(); }
    if (!ev.shiftKey && (document.activeElement === last || !modalRoot.contains(document.activeElement))) { ev.preventDefault(); first?.focus(); }
  }
});
installSourceFilters();
installSelects();
installSidebarResize();
installWorkspaceResize();
void load();
void loadInterfaceFonts().then(() => { if (currentPage === "settings" && !settingsBackups) render(); }).catch(() => { /* Keep the saved font and the standard font choices available. */ });
window.setInterval(() => { void pollCloudSync(); }, 5000);

window.addEventListener("keydown", (event) => {
  if (location.protocol !== "wails:" || !data || event.altKey || !(navigator.platform.includes("Mac") ? event.metaKey : event.ctrlKey)) return;
  const step = ["=", "+"].includes(event.key) ? 1 : ["-", "_"].includes(event.key) ? -1 : event.key === "0" ? 0 : null;
  if (step === null) return;
  event.preventDefault();
  const current = (settingsDraft || data.state.settings).textSize || 100;
  const size = step === 0 ? 100 : textSizes[Math.max(0, Math.min(textSizes.length - 1, textSizes.indexOf(current) + step))];
  savePreference({ textSize: size }); applyInterfaceSettings(settingsDraft || data.state.settings); render();
});
