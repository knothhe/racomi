import { renderSourceFilter, installSourceFilters } from "./source-filter";
import { changeSelection } from "./skill-selection";
import { createSettingsAutosave } from "./settings-autosave";
import { showFeedback } from "./feedback";
import { icon } from "./icons";
import { guidePage } from "./guide";
import { agentIcon } from "./agent-icons";
import { homeRelativePath } from "./paths";
import { enableNativeLocaleSync, getLocale, setLocale, t, tMessage, type LocalePreference, type TranslationVariables } from "./i18n";
import "./style.css";
import "./source-filter.css";
import { defaultSkillFilters, filterSkills, pendingSkills, skillSource, type SkillFilters } from "./skill-filters";
import { catalogEntries, sourceSyncPaths, summarizeSource, type SourceEntry } from "./source-catalog";
import { defaultSettings, projectDirectories, settingsPage } from "./settings";
import { enableNativeThemeSync, getThemePreference, setThemePreference, type ThemePreference } from "./theme";
import { api, type AppSettings, type AppState, type ApplyPlan, type Source, type ImportResult, type Preset, type Preview, type SearchResult, type Skill, type SkillsManagerPreview, type Target, type Update } from "./api";

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
let currentPage: Page = "skills";
let data: AppState | null = null;
let stateRevision = 0;
let loadError = "";
let selectedSkill = "";
let bulkSkillSelection = new Set<string>();
let selectedSource = "";
let sourceCatalogFilter: "changes" | "all" = "changes";
const sourceSelections = new Map<string, Set<string>>();
let skillsQuery = "";
let skillsFilter: SkillsFilter = "all";
let skillsSort: "asc" | "desc" = "asc";
let skillsSourceFilter = "all";
let presetFilters = defaultSkillFilters();
let presetSkillSelection = new Set<string>();
let detailTab: "overview" | "document" = "overview";
let discoverTab: "hub" | "git" = "hub";
const pageQueries = { sources: "", presets: "", projects: "", backups: "" };
let selectedGlobalKey = "";
let selectedGlobalKeys = new Set<string>();
let globalDirectoriesInitialized = false;
let settingsBackups = false;
let settingsDraft: AppSettings | null = null;
let settingsWriteRevision = 0;
const documents = new Map<string, { hash: string; text: string }>();
const loadingDocuments = new Set<string>();
let modalPreviousFocus: HTMLElement | null = null;
let modalGeneration = 0;
let searchResults: SearchResult[] | null = null;
let searchQuery = "";
let preview: Preview | null = null;
let sourceDraft = "";
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
const globalApplications = () => arr(data?.globalApplications).map((application) => ({ ...application, agentName: application.agentId === "custom" ? t("自定义") : application.agentName, name: application.agentId === "custom" ? application.name : t(application.name), description: t(application.description) }));
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
]);
const hasPendingMutation = () => [...activeTasks].some((label) => stateChangingTasks.has(label));
const skillSelectionActions = ["select-all-skills", "invert-skills", "clear-skill-selection", "update-selected-skills", "remove-selected-skills"];
const actionTasks: Record<string, string> = {
  "update-selected-skills": "正在更新技能…", "remove-selected-skills": "正在移除技能…",
  "check-updates": "正在检查技能更新…", "refresh-source": "正在刷新来源…",
  "sync-source": "正在同步来源中的技能…", "hub-preview": "正在读取仓库并发现技能…",
  "install": "正在导入所选技能…", "plan-project": "正在检查项目变化…", "plan-global": "正在检查全局变化…",
  "apply-project": "正在应用技能到项目…", "apply-global": "正在应用技能到全局…", "create-snapshot": "正在创建本地快照…",
};
const formTasks: Record<string, string> = {
  "hub-search": "正在搜索 skills.sh…", "source-preview": "正在读取仓库并发现技能…",
  "source-editor": "正在保存来源…", "source-relink": "正在重新关联技能…",
  "preset-editor": "正在保存预设…", "project-editor": "正在保存项目…", "global-directory-editor": "正在保存全局目录…", "project-directory-editor": "正在保存项目目录…",
  "backup-export": "正在导出工作空间…", "backup-import": "正在导入备份…",
  "skills-manager-import": "正在预览 skills-manager 数据库…",
};
const applicationActions = ["plan-project", "apply-project", "plan-global", "apply-global", "clear-application", "reset-application", "select-global-directories", "default-global-directories", "new-global-directory", "edit-global-directory", "delete-global-directory", "new-project-directory", "edit-project-directory", "delete-project-directory"];
const applicationBusy = () => hasPendingMutation() || ["正在检查项目变化…", "正在检查全局变化…"].some((label) => activeTasks.has(label));
const settingsControlsBusy = () => [...activeTasks].some((label) => label !== "正在保存设置…" && (stateChangingTasks.has(label) || ["正在检查项目变化…", "正在检查全局变化…"].includes(label)));
const settingsAutosave = createSettingsAutosave<AppState>({
  read: () => data!.state.settings,
  write: (settings) => api.saveSettings(settings),
  settings: (next) => next.state.settings,
  start: () => { activeTasks.add("正在保存设置…"); status(); syncBusyControls(); },
  error: (err) => toast(t("设置保存失败：{0}", { 0: tMessage(err instanceof Error ? err.message : String(err)) }), true),
  settled: (next) => {
    settingsDraft = null;
    activeTasks.delete("正在保存设置…");
    if (next) accept(next); else render();
  },
});
function syncBusyControls() {
  for (const control of document.querySelectorAll<HTMLButtonElement | HTMLInputElement | HTMLSelectElement>("button,input,select")) {
    const form = control.form;
    const selectionForm = form?.classList.contains("application-selection");
    const label = control instanceof HTMLButtonElement ? actionTasks[control.dataset.action || ""] || (control.type === "submit" ? formTasks[form?.id || ""] || (selectionForm ? form?.dataset.scope === "global" ? "正在保存全局选择…" : "正在保存项目选择…" : "") : "") : ["source-preview", "hub-search"].includes(form?.id || "") ? formTasks[form!.id] : control.name === "source-candidate" ? "正在同步来源中的技能…" : control.name === "candidate" ? "正在导入所选技能…" : "";
    const applicationControl = selectionForm || applicationActions.includes(control.dataset.action || "") || ["project-editor", "global-directory-editor", "project-directory-editor"].includes(form?.id || "") || control.name === "global-directory-selection";
    const importerControl = !!control.closest(".modal")?.querySelector("#skills-manager-import");
    const importerBusy = importerControl && skillsManagerBusy() && (control.dataset.action !== "close-modal" || activeTasks.has(skillsManagerImportTask));
    const importerEntryBusy = control.dataset.action === "import-skills-manager" && skillsManagerBusy();
    const skillControl = control.name === "bulk-skill" || ["update-skill", "remove-skill", ...skillSelectionActions].includes(control.dataset.action || "");
    const busy = (!!skillControl && hasPendingMutation()) || (form?.id === "settings-editor" && settingsControlsBusy()) || importerBusy || importerEntryBusy || (control.dataset.action === "update-skill" && hasPendingMutation()) || (!!label && activeTasks.has(label)) || (!!applicationControl && applicationBusy());
    if (busy) { if (control.dataset.busyDisabled === undefined) control.dataset.busyDisabled = String(control.disabled); control.disabled = true; control.setAttribute("aria-busy", "true"); }
    if (!busy && control.dataset.busyDisabled !== undefined) { control.disabled = control.dataset.busyDisabled === "true"; delete control.dataset.busyDisabled; control.removeAttribute("aria-busy"); }
  }
}
function status() {
  const el = document.querySelector("#activity");
  const label = [...activeTasks][0];
  const message = taskMessages.get(label);
  if (el) el.innerHTML = activeTasks.size ? `<span class="spinner"></span><span>${e(message ? t(message.key, message.variables) : t(label))}</span>` : t("{0}本地工作空间", { 0: icon("check") });
}
async function task<T>(message: string | { key: string; variables: TranslationVariables }, action: () => Promise<T>): Promise<T | undefined> {
  const label = typeof message === "string" ? message : `${message.key}:${JSON.stringify(message.variables)}`;
  if (activeTasks.has(label)) return;
  if (typeof message !== "string") taskMessages.set(label, message);
  activeTasks.add(label); status(); syncBusyControls();
  try {
    if (stateChangingTasks.has(label) && settingsAutosave.busy) await settingsAutosave.idle();
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
  for (const [scope, drafts] of [["project", projectDrafts], ["global", globalDrafts]] as const) {
    for (const [id, selection] of drafts) {
      const application = findApplication(scope, id);
      const valid = { skillIds: selection.skillIds.filter((skillID) => skills().some((skill) => skill.id === skillID)), presetIds: selection.presetIds.filter((presetID) => presets().some((preset) => preset.id === presetID)) };
      const matchesSaved = application && (scope === "global" && id.startsWith("batch:") ? globalApplications().filter((item) => globalBatchKeys(id).includes(item.key)).every((item) => sameSelection(valid, savedSelection(item.target))) : sameSelection(valid, savedSelection(application.target)));
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
  render();
  if (detailTab === "document" && selectedSkill) void loadSkillDocument(selectedSkill);
}
async function mutate(label: string, action: () => Promise<AppState>, message: string) {
  const next = await task(label, action);
  if (next) { accept(next); toast(message); return true; }
  return false;
}
function empty(title: string, description: string, action = "", symbol = "box") {
  return `<div class="empty"><span class="empty-icon">${icon(symbol)}</span><h3>${e(title)}</h3><p>${e(description)}</p>${action}</div>`;
}
function button(label: string, action: string, cls = "", attrs = "", symbol = "") {
  return `<button type="button" class="button ${cls}" data-action="${action}" ${attrs}>${symbol ? icon(symbol) : ""}${e(label)}</button>`;
}
function heading(title: string, description: string, actions = "") {
  return `<header class="page-heading"><div><h2 class="page-section-title">${e(title)}</h2><p class="page-description">${e(description)}</p></div><div class="heading-actions">${actions}</div></header>`;
}
function pageSearch(page: keyof typeof pageQueries, placeholder: string) {
  return `<label class="search-field toolbar-search">${icon("search")}<input data-page-search="${page}" aria-label="${e(placeholder)}" placeholder="${e(placeholder)}" value="${e(pageQueries[page])}"></label>`;
}
function pendingApplicationCount(page: Page) {
  return page === "projects" ? projects().filter((project) => project.pending).length : page === "global" ? uniqueApplications(globalApplications().filter((application) => application.target?.pending)).length : 0;
}
function render() {
  const current = nav.find((item) => item.id === currentPage)!;
  const view = currentPage === "settings" && settingsBackups ? "backups" : currentPage;
  const oldContent = document.querySelector<HTMLElement>(".workspace");
  const scrollTop = oldContent?.dataset.view === view ? oldContent.scrollTop : 0;
  const focused = document.activeElement instanceof HTMLInputElement && document.activeElement.dataset.pageSearch && oldContent?.dataset.page === currentPage ? { page: document.activeElement.dataset.pageSearch, start: document.activeElement.selectionStart, end: document.activeElement.selectionEnd } : null;
  const activeElement = document.activeElement;
  const settingsFocus = (activeElement instanceof HTMLInputElement || activeElement instanceof HTMLSelectElement) && activeElement.closest(".settings-page") && oldContent?.dataset.page === currentPage ? { name: activeElement.name, value: activeElement.value } : null;
  const activeButton = app.contains(activeElement) && activeElement instanceof HTMLButtonElement ? { dataset: Object.fromEntries(["page", "action", "id", "scope", "tab", "filter", "themePreference"].map((key) => [key, activeElement.dataset[key]])), form: activeElement.form?.id, type: activeElement.type } : null;
  app.innerHTML = `<header class="app-header top"><div class="brand"><span class="brand-mark"><img src="/appicon.png" alt="" width="30" height="30" draggable="false"></span><strong>racomi</strong></div><h1 class="sr-only">${e(t(current.title))}</h1><nav class="main-nav seg" aria-label="${t("主导航")}">${nav.filter((item) => item.id !== "settings" && item.id !== "guide").map((item) => `<button class="nav-item ${currentPage === item.id ? "active on" : ""}" type="button" data-page="${item.id}" ${currentPage === item.id ? 'aria-current="page"' : ""}>${e(t(item.title))}${pendingApplicationCount(item.id) ? `<span class="nav-count" title="${e(t(item.id === "projects" ? "{count} 个项目待应用" : "{count} 个全局配置待应用", { count: pendingApplicationCount(item.id) }))}">${pendingApplicationCount(item.id)}</span>` : ""}</button>`).join("")}</nav><div class="header-actions actions"><button type="button" class="icon-button header-settings ${currentPage === "guide" ? "active" : ""}" data-page="guide" aria-label="${e(t("指南"))}" title="${e(t("指南"))}" ${currentPage === "guide" ? 'aria-current="page"' : ""}>${icon("book")}</button><button type="button" class="icon-button header-settings ${currentPage === "settings" ? "active" : ""}" data-page="settings" aria-label="${e(t("设置"))}" title="${e(t("设置"))}" ${currentPage === "settings" ? 'aria-current="page"' : ""}>${icon("settings")}</button></div></header><main class="workspace" data-page="${currentPage}" data-view="${view}"><div class="page-content" id="page-content" data-page="${currentPage}">${currentPage === "guide" ? guidePage() : !data ? loadingPage() : currentPage === "skills" ? skillsPage() : currentPage === "sources" ? sourcesPage() : currentPage === "discover" ? discoverPage() : currentPage === "presets" ? presetsPage() : currentPage === "projects" ? projectsPage() : currentPage === "global" ? globalPage() : settingsBackups ? `${button(t("返回设置"), "close-backups", "quiet settings-back", "", "arrow")}${backupsPage()}` : settingsPage(settingsDraft || data.state.settings, globalApplications(), data.dataDir, data.homeDir)}</div></main><footer class="statusbar"><span id="activity" aria-live="polite"></span><div class="statusbar-meta"><span>${skills().length}${t(" 个技能 · ")}${presets().length}${t(" 个预设 · ")}${projects().length}${t(" 个项目")}</span><button type="button" class="search-shortcut" data-action="focus-search" aria-label="${t("搜索当前页面")}">${t("搜索 ")}<kbd>${navigator.platform.includes("Mac") ? "⌘" : "Ctrl"} K</kbd></button></div></footer>`;
  document.querySelector<HTMLElement>(".workspace")!.scrollTop = scrollTop;
  if (focused) {
    const input = document.querySelector<HTMLInputElement>(`input[data-page-search="${focused.page}"]`);
    input?.focus({ preventScroll: true });
    if (focused.start !== null && focused.end !== null) input?.setSelectionRange(focused.start, focused.end);
  }
  status(); syncBusyControls();
  if (settingsFocus && !modalRoot.children.length) {
    [...app.querySelectorAll<HTMLInputElement | HTMLSelectElement>('.settings-page input,.settings-page select')].find((input) => input.name === settingsFocus.name && input.value === settingsFocus.value)?.focus({ preventScroll: true });
  }
  if (activeButton && !focused && !modalRoot.children.length) {
    const replacement = [...app.querySelectorAll<HTMLButtonElement>("button")].find((button) => button.form?.id === activeButton.form && button.type === activeButton.type && Object.entries(activeButton.dataset).every(([key, value]) => button.dataset[key] === value));
    replacement?.focus({ preventScroll: true });
  }
}
function loadingPage() {
  if (loadError) return `${heading(t("连接工作空间"), t("连接 racomi 本地桌面服务。"))}${empty(t("本地服务未连接"), t("本地服务未连接。{0}", { 0: tMessage(loadError) }), button(t("重新连接"), "reload", "primary", "", "refresh"), "info")}`;
  return `${heading(t("技能"), t("集中管理技能，按需应用到项目。"))}${empty(t("正在读取本地技能"), t("首次使用时，可从 skills.sh 或 Git 仓库导入技能。"), '<span class="spinner"></span>')}`;
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
    ...sources().map((source) => ({ id: source.id, name: source.name, description: source.repository })),
  ] });
}
function originLabel(skill: Skill) {
  return skillSource(skill, sources())?.name || t("本地 · 无来源");
}
function skillsPage() {
  const list = filteredSkills();
  if (!list.some((skill) => skill.id === selectedSkill)) selectedSkill = list[0]?.id || "";
  const selected = skills().find((skill) => skill.id === selectedSkill);
  const updateCount = updates?.filter((update) => skills().some((skill) => skill.id === update.skillId) && update.available && !update.removed && !update.error).length || 0;
  const pending = pendingSkillIDs();
  return `${heading(t("技能"), t("保存技能原件。更新后，到“项目”或“全局”手动应用。"), button(t("检查更新"), "check-updates", "", "", "refresh") + button(t("导入技能"), "go-discover", "primary", "", "plus"))}<div class="skills-controls"><div class="skills-filters" role="group" aria-label="${t("技能筛选")}">${([["all", t("全部"), skills().length], ["updates", t("有更新"), updateCount], ["pending", t("待应用"), pending.size]] as const).map(([id, label, count]) => `<button type="button" class="filter-chip ${skillsFilter === id ? "active" : ""}" data-action="filter-skills" data-filter="${id}" aria-pressed="${skillsFilter === id}">${label}<span class="filter-count">${count}</span></button>`).join("")}</div><div class="sort-control source-filter-control"><span>${t("来源")}</span>${sourceFilter("skills-source", skillsSourceFilter)}</div><label class="sort-control"><span>${t("排序")}</span><select class="sort-select" name="skills-sort" aria-label="${t("技能排序")}"><option value="asc" ${skillsSort === "asc" ? "selected" : ""}>${t("名称 A → Z")}</option><option value="desc" ${skillsSort === "desc" ? "selected" : ""}>${t("名称 Z → A")}</option></select></label></div><div class="skills-layout"><section class="panel skill-browser"><div class="panel-toolbar"><label class="search-field">${icon("search")}<input id="skills-search" data-page-search="skills" aria-label="${t("搜索已安装技能")}" placeholder="${t("搜索名称、描述或来源")}" value="${e(skillsQuery)}"></label><span class="muted" id="skills-count">${list.length}${t(" 个")}</span></div>${skillsSelectionToolbar()}<div id="skill-list">${skillList()}</div></section><aside class="panel skill-detail">${selected ? skillDetail(selected) : !skills().length ? empty(t("开始管理你的技能"), t("从 skills.sh 发现技能，或导入 Git 仓库。"), button(t("发现技能"), "go-discover", "primary", "", "plus")) : empty(t("没有匹配的技能"), skillsFilter === "updates" && updates === null ? t("先检查更新，再查看可更新的技能。") : t("试试其他筛选条件或关键词。"))}</aside></div>`;
}
function skillsSelectionToolbar() {
  const list = filteredSkills();
  const allSelected = list.length > 0 && list.every((skill) => bulkSkillSelection.has(skill.id));
  const updateIDs = updatedSkillIDs();
  const updateCount = [...bulkSkillSelection].filter((id) => updateIDs.has(id)).length;
  return `<div class="skills-selection-toolbar"><div class="skills-selection-summary"><span id="skills-selection-count" aria-live="polite">${t("已选择 {0} 个技能", { 0: bulkSkillSelection.size })}</span><div class="skills-selection-buttons">${button(t(allSelected ? "取消全选" : "全选"), "select-all-skills", "quiet small", list.length ? "" : "disabled")}${button(t("反选"), "invert-skills", "quiet small", list.length ? "" : "disabled")}${button(t("清空选择"), "clear-skill-selection", "quiet small", bulkSkillSelection.size ? "" : "disabled")}</div></div><div class="skills-bulk-actions">${button(t("更新所选（{0}）", { 0: updateCount }), "update-selected-skills", "small", updateCount ? "" : "disabled", "download")}${button(t("移除所选"), "remove-selected-skills", "danger-quiet small", bulkSkillSelection.size ? "" : "disabled")}</div><p class="muted selection-scope-hint">${t("全选与反选仅作用于当前筛选结果，切换筛选会保留已选技能。")}</p></div>`;
}
function skillList() {
  const list = filteredSkills();
  const pending = pendingSkillIDs();
  return list.length ? list.map((skill) => `<div class="skill-list-item ${bulkSkillSelection.has(skill.id) ? "checked" : ""}"><label class="skill-row-selector"><input type="checkbox" name="bulk-skill" value="${e(skill.id)}" aria-label="${e(t("选择技能 {0}", { 0: skill.name }))}" ${bulkSkillSelection.has(skill.id) ? "checked" : ""}></label><button type="button" class="skill-row ${selectedSkill === skill.id ? "selected" : ""}" data-action="select-skill" data-id="${e(skill.id)}" aria-pressed="${selectedSkill === skill.id}"><span class="skill-symbol">${icon("box")}</span><span class="skill-copy"><strong>${e(skill.name)}</strong><span>${e(skill.description || t("暂无描述"))}</span><small class="skill-origin ${skillSource(skill, sources()) ? "linked" : "local"}">${e(originLabel(skill))}</small></span><span class="row-badges">${skill.upstreamRemoved || updates?.some((update) => update.skillId === skill.id && update.removed) ? `<span class="badge amber">${t("上游移除")}</span>` : ""}${updates?.some((update) => update.skillId === skill.id && update.available && !update.removed && !update.error) ? `<span class="badge amber">${t("有更新")}</span>` : ""}${pending.has(skill.id) ? `<span class="badge">${t("待应用")}</span>` : ""}</span><span class="row-arrow">›</span></button></div>`).join("") : `<div class="small-empty">${!skills().length ? t("尚未安装技能") : t("没有匹配的技能")}</div>`;
}
function skillDetail(skill: Skill) {
  const update = updates?.find((update) => update.skillId === skill.id);
  const removed = skill.upstreamRemoved || update?.removed;
  const source = skillSource(skill, sources());
  const doc = documents.get(skill.id);
  return `<div class="detail-header"><div class="detail-title"><span class="detail-symbol">${icon("box")}</span><h2>${e(skill.name)}</h2></div><span class="badge ${removed ? "amber" : "green"}">${removed ? t("上游移除") : t("已安装")}</span></div><div class="skill-origin-bar">${source ? `${button(t("查看来源"), "open-source", "small", `data-id="${e(source.id)}"`, "refresh")}<span>${e(source.name)}</span>` : `<span class="badge">${t("本地 · 无来源")}</span><span>${t("此技能未关联来源，可独立使用。")}</span>`}</div><div class="detail-tabs tabs" role="tablist" aria-label="${t("技能详情")}"><button type="button" class="tab-button ${detailTab === "overview" ? "active" : ""}" data-action="detail-tab" data-tab="overview" role="tab" aria-selected="${detailTab === "overview"}">${t("概览")}</button><button type="button" class="tab-button ${detailTab === "document" ? "active" : ""}" data-action="detail-tab" data-tab="document" role="tab" aria-selected="${detailTab === "document"}">SKILL.md</button></div>${detailTab === "document" ? `<div class="skill-document" role="tabpanel" aria-label="SKILL.md">${doc?.hash === skill.contentHash ? `<pre><code>${e(doc.text)}</code></pre>` : `<div class="small-empty">${loadingDocuments.has(skill.id) ? `<span class="spinner"></span>${t(" 正在读取 SKILL.md")}` : button(t("读取 SKILL.md"), "read-document", "small", `data-id="${e(skill.id)}"`, "refresh")}</div>`}</div>` : `<p class="detail-description">${e(skill.description || t("该技能未提供描述。"))}</p><dl class="metadata">${source ? `<dt>${t("来源")}</dt><dd>${button(source.name, "open-source", "quiet small source-link", `data-id="${e(source.id)}"`)}</dd>` : `<dt>${t("来源")}</dt><dd>${t("本地 · 无来源")}</dd>`}<dt>${t("仓库")}</dt><dd class="mono">${e(skill.source || "—")}</dd><dt>${t("技能路径")}</dt><dd class="mono">${e(skill.subpath || ".")}</dd><dt>${t("Git 引用")}</dt><dd class="mono">${e(skill.ref || t("默认分支"))}</dd><dt>${t("当前版本")}</dt><dd class="mono" title="${e(skill.revision)}">${e(short(skill.revision))}</dd><dt>${t("安装时间")}</dt><dd>${e(date(skill.installedAt))}</dd><dt>${t("更新时间")}</dt><dd>${e(date(skill.updatedAt))}</dd></dl>${removed ? `<div class="info-note source-retained"><strong>${t("上游已移除此技能")}</strong><p>${t("本地版本与预设引用已保留，可继续使用。如技能被移动或改名，可到来源重新关联。")}</p></div>` : update ? `<div class="update-notice ${update.error ? "error-notice" : ""}"><strong>${update.error ? t("检查失败") : update.available ? t("发现新版本") : t("已是最新版本")}</strong><p>${e(tMessage(update.error || update.summary) || (update.available ? t("新版本 {0}", { 0: short(update.revision) }) : t("与仓库版本一致。")))}</p>${update.available && !update.removed && !update.error ? button(t("更新技能"), "update-skill", "primary small", `data-id="${e(skill.id)}"`, "download") : ""}</div>` : `<div class="info-note">${t("更新本地技能后，请到“项目”或“全局”预览变化并手动应用。")}</div>`}`}<div class="detail-footer">${button(t("移除技能"), "remove-skill", "danger-quiet small", `data-id="${e(skill.id)}"`)}</div>`;
}
async function loadSkillDocument(id: string) {
  const skill = skills().find((skill) => skill.id === id);
  if (!skill || loadingDocuments.has(id) || documents.get(id)?.hash === skill.contentHash) return;
  loadingDocuments.add(id);
  if (currentPage === "skills" && selectedSkill === id) render();
  const text = await task({ key: "正在读取 {0} 的 SKILL.md…", variables: { 0: skill.name } }, () => api.readSkillDocument(id));
  loadingDocuments.delete(id);
  if (text !== undefined && skills().find((item) => item.id === id)?.contentHash === skill.contentHash) documents.set(id, { hash: skill.contentHash, text });
  if (currentPage === "skills" && selectedSkill === id) render();
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
  return `${heading(t("来源"), t("刷新仓库查看变化，再同步本地技能。技能应用目录仍需手动应用。"), button(t("添加来源"), "new-source", "primary", "", "plus"))}${!sources().length ? empty(t("保存你的技能来源"), t("添加 Git 仓库后，可挑选安装或订阅整个仓库。没有安装技能也能保存来源。"), button(t("添加来源"), "new-source", "primary", "", "plus"), "refresh") : `<div class="sources-layout"><section class="panel sources-browser"><div class="panel-toolbar">${pageSearch("sources", t("搜索来源"))}<span class="muted">${list.length}${t(" 个")}</span></div><div>${list.map((source) => {
    const summary = sourceSummary(source);
    return `<button type="button" class="source-row ${selectedSource === source.id ? "selected" : ""}" data-action="select-source" data-id="${e(source.id)}" aria-pressed="${selectedSource === source.id}"><span class="skill-symbol">${icon("refresh")}</span><span class="skill-copy"><strong>${e(source.name)}</strong><small>${e(source.repository)}</small><span>${source.lastError ? t("刷新失败 · 保留上次目录") : !source.revision ? t("尚未刷新") : t("可安装 {0} · 可更新 {1} · 上游移除 {2}", { 0: summary.installs, 1: summary.changed, 2: summary.removed })}</span>${!source.lastError && summary.skipped ? `<span class="source-partial">${t("部分刷新 · 跳过 {0}", { 0: summary.skipped })}</span>` : ""}</span><span class="badge">${source.mode === "all" ? t("整库订阅") : t("挑选安装")}</span></button>`;
  }).join("") || `<div class="small-empty">${t("没有匹配的来源")}</div>`}</div></section><section class="panel source-detail">${selected ? sourceDetail(selected) : empty(t("没有匹配的来源"), t("试试其他名称或仓库地址。"))}</section></div>`}`;
}
function sourceDetail(source: Source) {
  const { entries, available, installs, changed, removed, skipped } = sourceSummary(source);
  const selection = sourceSelection(source.id);
  const count = source.mode === "all" ? available.length : available.filter((entry) => selection.has(entry.item.subpath)).length;
  const visible = entries.filter((entry) => entry.kind === "skipped" || !entry.item.removed || entry.local);
  const shown = sourceCatalogFilter === "all" ? visible : visible.filter((entry) => entry.kind !== "current");
  const removedLocals = entries.filter((entry) => entry.kind === "removed" && entry.local).map((entry) => entry.local!);
  return `<div class="source-detail-heading"><div><div class="title-line"><h2>${e(source.name)}</h2><span class="badge">${source.mode === "all" ? t("整库订阅") : t("挑选安装")}</span></div><p class="mono">${e(source.repository)}</p></div><div class="source-actions">${button(t("编辑"), "edit-source", "quiet small", `data-id="${e(source.id)}"`)}${button(t("删除"), "delete-source", "quiet small danger-quiet", `data-id="${e(source.id)}"`)}</div></div><dl class="source-metadata"><dt>${t("引用")}</dt><dd class="mono">${e(source.ref || t("默认分支"))}</dd><dt>${t("扫描范围")}</dt><dd class="mono">${e(source.subpath || t("整个仓库"))}</dd><dt>${t("上次刷新")}</dt><dd>${e(date(source.refreshedAt))}</dd><dt>${t("目录版本")}</dt><dd class="mono" title="${e(source.revision)}">${e(short(source.revision))}</dd></dl>${source.lastError ? `<div class="source-error" role="status"><strong>${t("刷新失败")}</strong><p>${e(tMessage(source.lastError))}</p><small>${t("保留上次成功获取的目录和本地技能，请重新刷新后同步。")}</small></div>` : ""}${!source.lastError && skipped ? `<div class="source-partial-note" role="status">${t("已跳过 {0} 个技能；已有版本已保留，其他技能可继续同步。", { 0: skipped })}</div>` : ""}${scanWarnings(source.warnings)}<div class="source-refresh-bar"><span class="muted">${source.mode === "all" ? t("同步会安装全部技能，并更新已安装技能。") : t("选择需要安装或更新的技能；全选不会改变订阅模式。")}</span>${button(t("刷新来源"), "refresh-source", "small", `data-id="${e(source.id)}"`, "refresh")}</div>${!source.revision ? `<div class="small-empty">${t("来源已保存。刷新后即可浏览技能与同步变化。")}</div>` : `<div class="source-summary"><span>${t("可安装 ")}<strong>${installs}</strong></span><span>${t("可更新 ")}<strong>${changed}</strong></span><span>${t("上游移除 ")}<strong>${removed}</strong></span>${skipped ? `<span>${t("本次跳过 ")}<strong>${skipped}</strong></span>` : ""}</div><div class="source-catalog-toolbar"><div class="skills-filters" role="group" aria-label="${t("来源目录筛选")}"><button type="button" class="filter-chip ${sourceCatalogFilter === "changes" ? "active" : ""}" data-action="source-filter" data-filter="changes" aria-pressed="${sourceCatalogFilter === "changes"}">${t("查看变化")}</button><button type="button" class="filter-chip ${sourceCatalogFilter === "all" ? "active" : ""}" data-action="source-filter" data-filter="all" aria-pressed="${sourceCatalogFilter === "all"}">${t("全部技能 ")}<span class="filter-count">${visible.length}</span></button></div>${source.mode === "selected" && available.length ? button(available.every((entry) => selection.has(entry.item.subpath)) ? t("取消全选") : t("全选变化"), "select-all-source", "quiet small", `data-id="${e(source.id)}" ${source.lastError ? "disabled" : ""}`) : ""}</div><div class="source-catalog">${shown.map((entry) => sourceCatalogRow(source, entry, removedLocals)).join("") || `<div class="small-empty">${entries.length ? t("与本地技能一致，没有待同步的变化。") : t("扫描范围内没有有效的 SKILL.md。")}</div>`}</div><div class="source-sync-footer"><div><strong id="source-sync-count">${source.mode === "all" ? t("待同步 {0} 个技能", { 0: count }) : t("已选择 {0} 个技能", { 0: count })}</strong><small>${t("上游移除的技能保留本地版本；同步后请到“项目”或“全局”手动应用。")}</small></div>${button(source.mode === "all" ? t("同步全部技能到本地") : t("同步所选技能到本地"), "sync-source", "primary small", `data-id="${e(source.id)}" ${!count || source.lastError ? "disabled" : ""}`, "download")}</div>`}`;
}
function sourceCatalogRow(source: Source, entry: SourceEntry, removedLocals: Skill[]) {
  const { item, local, kind } = entry;
  const selectable = kind === "install" || kind === "update";
  const checked = selectable && (source.mode === "all" || sourceSelection(source.id).has(item.subpath));
  const label = kind === "skipped" ? t("本次跳过") : kind === "removed" ? local ? t("上游移除 · 本地保留") : t("上游移除") : kind === "update" ? t("可更新") : kind === "current" ? t("已同步") : item.new ? t("新增 · 可安装") : t("可安装");
  return `<div class="source-catalog-row ${kind === "removed" ? "source-removed" : ""}"><label class="source-catalog-choice"><input type="checkbox" name="source-candidate" data-source="${e(source.id)}" value="${e(item.subpath)}" aria-label="${t("选择")}${e(item.name)}" ${checked ? "checked" : ""} ${!selectable || source.mode === "all" || source.lastError ? "disabled" : ""}><span><strong>${e(item.name)}</strong><small>${e(item.description || t("暂无描述"))}</small><code>${e(item.subpath || ".")}</code>${local && local.name !== item.name ? `<small>${t("本地名称：")}${e(local.name)}</small>` : ""}${kind === "skipped" ? `<small class="source-skip-reason">${e(tMessage(item.skipReason))}</small>${local ? `<small>${t("已保留本地版本。")}</small>` : ""}` : ""}</span></label><div class="source-catalog-status"><span class="badge ${kind === "update" || kind === "removed" || kind === "skipped" ? "amber" : kind === "current" ? "green" : ""}">${label}</span>${kind === "install" && removedLocals.length ? button(t("重新关联…"), "relink-source-skill", "quiet small", `data-id="${e(source.id)}" data-subpath="${e(item.subpath)}" ${source.lastError ? "disabled" : ""}`) : ""}${(kind === "removed" || kind === "skipped") && local ? button(t("查看本地"), "view-local-skill", "quiet small", `data-id="${e(local.id)}"`) : ""}</div></div>`;
}
function sourceEditor(id = "") {
  const source = sources().find((item) => item.id === id);
  modal(source ? t("编辑来源") : t("添加来源"), source ? t("修改名称和安装方式。仓库地址、引用和范围如需变更，请添加新来源。") : t("保存 Git 仓库作为持续追踪的技能来源。"), `<input type="hidden" name="entityId" value="${e(source?.id)}"><label class="field"><span>${t("名称")}</span><input name="name" value="${e(source?.name)}" required maxlength="80" placeholder="${t("例如：团队技能")}"></label><label class="field"><span>${t("仓库地址")}</span><input name="repository" value="${e(source?.repository)}" required ${source ? "readonly" : ""} placeholder="${t("owner/repo 或 https://github.com/owner/repo")}"></label><div class="form-grid"><label class="field"><span>${t("分支、标签或 Commit ")}<i>${t("可选")}</i></span><input name="ref" value="${e(source?.ref)}" ${source ? "readonly" : ""} placeholder="${t("默认分支")}"></label><label class="field"><span>${t("扫描子目录 ")}<i>${t("可选")}</i></span><input name="subpath" value="${e(source?.subpath)}" ${source ? "readonly" : ""} placeholder="${t("整个仓库")}"></label></div><div class="field"><span>${t("安装方式")}</span><div class="source-mode-options"><label class="source-mode-option"><input type="radio" name="mode" value="selected" ${source?.mode !== "all" ? "checked" : ""}><span><strong>${t("挑选安装")}</strong><small>${t("新增技能只提示，手动选择安装和更新。")}</small></span></label><label class="source-mode-option"><input type="radio" name="mode" value="all" ${source?.mode === "all" ? "checked" : ""}><span><strong>${t("整库订阅")}</strong><small>${t("每次同步安装全部技能，包含后续新增技能。")}</small></span></label></div></div><div class="info-note">${t("刷新只读取仓库变化。同步修改本地技能，项目与全局配置需另行手动应用。")}</div>`, `<button class="button primary" type="submit">${t("保存来源")}</button>`, "source-editor");
}
function relinkSourceEditor(source: Source, subpath: string) {
  const entry = sourceEntries(source).find((item) => item.item.subpath === subpath && item.kind === "install");
  const removed = sourceEntries(source).filter((item) => item.kind === "removed" && item.local);
  if (source.lastError || !entry || !removed.length) return;
  modal(t("重新关联技能"), t("用于上游移动路径或改名。确认后更新本地内容，并保留技能 ID 与预设引用。"), `<input type="hidden" name="sourceId" value="${e(source.id)}"><input type="hidden" name="subpath" value="${e(subpath)}"><label class="field"><span>${t("选择保留的本地技能")}</span><select name="skillId" required>${removed.map((item) => `<option value="${e(item.local!.id)}">${e(item.local!.name)} · ${e(item.local!.subpath || ".")}</option>`).join("")}</select></label><dl class="metadata"><dt>${t("新技能")}</dt><dd>${e(entry.item.name)}</dd><dt>${t("新路径")}</dt><dd class="mono">${e(subpath || ".")}</dd></dl><div class="info-note">${t("关联后，使用该技能的项目与全局配置会标记为待应用，请到相应页面应用变化。")}</div>`, `<button class="button primary" type="submit">${t("关联并更新本地技能")}</button>`, "source-relink");
}
function discoverPage() {
  return `${heading(t("发现"), t("从 skills.sh 或 Git 仓库预览并导入技能。"))}<div class="tabs discover-tabs" role="tablist" aria-label="${t("导入方式")}"><button type="button" class="tab-button ${discoverTab === "hub" ? "active" : ""}" data-action="discover-tab" data-tab="hub" role="tab" aria-selected="${discoverTab === "hub"}">Skill Hub <span class="tab-note">skills.sh</span></button><button type="button" class="tab-button ${discoverTab === "git" ? "active" : ""}" data-action="discover-tab" data-tab="git" role="tab" aria-selected="${discoverTab === "git"}">${t("Git 仓库")}</button></div><div class="discover-content" role="tabpanel">${discoverTab === "hub" ? `<section class="panel hub-panel"><form id="hub-search" class="inline-form"><label class="search-field">${icon("search")}<input name="query" data-page-search="discover" placeholder="${t("搜索 skills.sh，例如 frontend、review")}" required value="${e(searchQuery)}" aria-label="${t("搜索 skills.sh")}"></label><button class="button primary" type="submit">${t("搜索")}</button></form><div id="search-results">${searchResults === null ? empty(t("发现适合你的技能"), t("输入关键词搜索 skills.sh，预览仓库内容后再导入。"), "", "compass") : !searchResults.length ? empty(t("没有匹配结果"), t("试试其他关键词，或使用 Git 仓库直接导入。")) : `<div class="results-list">${searchResults.map((result, index) => `<article class="search-result"><span class="skill-symbol small-symbol">${icon("box")}</span><div class="result-copy"><div class="result-heading"><strong>${e(result.name)}</strong>${result.supported === false ? `<span class="badge">${t("暂不支持")}</span>` : ""}</div><p class="mono">${e(result.source)}</p>${result.supported === false ? `<p class="unsupported-reason">${e(tMessage(result.unsupportedReason) || t("此来源暂不支持 Git 导入"))}</p>` : ""}</div><div class="result-footer"><span class="result-installs">${Number.isFinite(result.installs) ? new Intl.NumberFormat(getLocale(), { notation: "compact" }).format(result.installs) : "—"}${t(" 次安装")}</span>${button(t("预览"), "hub-preview", "small", `data-index="${index}" ${result.supported === false ? "disabled" : ""}`, "arrow")}</div></article>`).join("")}</div>`}</div></section>` : `<section class="panel import-panel"><form id="source-preview"><div class="form-grid"><label class="field span-2"><span>${t("仓库地址 ")}<i>${t("必填")}</i></span><input name="source" data-page-search="discover" required placeholder="${t("owner/repo 或 https://github.com/owner/repo")}" value="${e(sourceDraft)}"></label><label class="field"><span>${t("分支、标签或 Commit ")}<i>${t("可选")}</i></span><input name="ref" placeholder="${t("默认分支")}" value="${e(refDraft)}"></label><label class="field"><span>${t("技能子目录 ")}<i>${t("可选")}</i></span><input name="subpath" placeholder="${t("自动扫描仓库")}" value="${e(subpathDraft)}"></label></div><div class="form-footer"><span class="muted">${t("读取 SKILL.md，确认后导入。")}</span><button class="button primary" type="submit">${icon("search")}${t("预览仓库")}</button></div></form></section>`}${preview ? previewPanel() : ""}</div>`;
}
function previewPanel() {
  const skipped = arr(preview!.skipped);
  return `<section class="panel preview-panel"><div class="preview-heading"><div><h3>${t("选择要导入的技能 ")}<span class="badge">${arr(preview!.candidates).length}</span></h3><p class="mono">${e(preview!.source)} · ${e(short(preview!.revision))}</p></div>${button(selectedCandidates.size === arr(preview!.candidates).length ? t("取消全选") : t("全选"), "select-all-candidates", "quiet small", arr(preview!.candidates).length ? "" : "disabled")}</div>${scanWarnings(preview!.warnings)}${skipped.length ? `<div class="preview-skipped" role="status"><strong>${t("本次跳过 {0} 个技能", { 0: skipped.length })}</strong><ul>${skipped.map((item) => `<li><code>${e(item.subpath || ".")}</code><span>${e(tMessage(item.reason))}</span></li>`).join("")}</ul></div>` : ""}${arr(preview!.candidates).length ? `<div class="selection-list">${arr(preview!.candidates).map((candidate) => `<label class="selection-row"><input type="checkbox" name="candidate" value="${e(candidate.subpath)}" ${selectedCandidates.has(candidate.subpath) ? "checked" : ""}><span><strong>${e(candidate.name)}</strong><small>${e(candidate.description || t("暂无描述"))}</small><code>${e(candidate.subpath || ".")}</code></span></label>`).join("")}</div><div class="form-footer"><span class="muted" id="candidate-count">${t("已选择 ")}${selectedCandidates.size}${t(" 个技能")}</span>${button(t("导入所选技能"), "install", "primary", selectedCandidates.size ? "" : "disabled", "download")}</div>` : `<div class="small-empty">${t("没有发现有效的 SKILL.md，请检查仓库与子目录。")}</div>`}</section>`;
}
function presetsPage() {
  const list = presets().filter((preset) => `${preset.name} ${preset.description} ${arr(preset.skillIds).map((id) => skills().find((skill) => skill.id === id)?.name || "").join(" ")}`.toLowerCase().includes(pageQueries.presets.toLowerCase()));
  return `${heading(t("预设"), t("将常用技能组合在一起，用于项目或全局配置。"), pageSearch("presets", t("搜索预设")) + button(t("新建预设"), "new-preset", "primary", "", "plus"))}<div class="page-note">${icon("layers")}<span>${t("预设与直接选择的技能合并，相同技能只应用一次。")}</span></div>${!presets().length ? empty(t("创建第一个预设"), t("为常用技能命名一个预设，例如“日常开发”。"), button(t("新建预设"), "new-preset", "primary", "", "plus"), "layers") : !list.length ? `<div class="small-empty">${t("没有匹配的预设")}</div>` : `<div class="preset-list">${list.map((preset) => `<article class="panel preset-card"><span class="card-icon">${icon("layers")}</span><div class="preset-content"><div class="title-line"><h2>${e(preset.name)}</h2><span class="badge">${arr(preset.skillIds).length}${t(" 个技能")}</span></div><p class="card-description">${e(preset.description || t("暂无描述"))}</p><div class="skill-tags">${arr(preset.skillIds).map((id) => `<span>${e(skills().find((skill) => skill.id === id)?.name || t("已缺失的技能"))}</span>`).join("") || `<span class="muted">${t("尚未添加技能")}</span>`}</div></div><div class="preset-meta"><span class="muted">${projects().filter((project) => arr(project.enabledPresetIds).includes(preset.id)).length}${t(" 个项目 · ")}${uniqueApplications(globalApplications().filter((application) => arr(application.target?.enabledPresetIds).includes(preset.id))).length}${t(" 个全局配置使用")}</span><div class="card-footer">${button(t("编辑"), "edit-preset", "small", `data-id="${e(preset.id)}"`)}${button(t("删除"), "delete-preset", "quiet small danger-quiet", `data-id="${e(preset.id)}"`)}</div></div></article>`).join("")}</div>`}`;
}
function targetPaths(target: Target | null) { return arr(target?.paths).length ? arr(target?.paths) : target?.path ? [target.path] : []; }
function projectsPage() {
  const list = projects().filter((project) => `${project.name} ${project.projectRoot || project.path} ${targetPaths(project).join(" ")} ${arr(project.enabledSkillIds).map((id) => skills().find((skill) => skill.id === id)?.name || "").join(" ")} ${arr(project.enabledPresetIds).map((id) => presets().find((preset) => preset.id === id)?.name || "").join(" ")}`.toLowerCase().includes(pageQueries.projects.toLowerCase()));
  return `${heading(t("项目"), t("为项目选择技能与多个目录，预览后应用。"), pageSearch("projects", t("搜索项目")) + button(t("添加项目"), "new-project", "primary", "", "plus"))}<div class="page-note">${icon("folder")}<span>${t("内置目录与自定义目录可同时使用。当前应用方式：")}${data?.state.settings.deploymentMode === "symlink" ? t("软链接") : t("复制")}${t("。")}</span></div>${!projects().length ? empty(t("添加第一个项目"), t("选择项目目录，然后直接挑选技能或使用预设。"), button(t("添加项目"), "new-project", "primary", "", "plus"), "folder") : !list.length ? `<div class="small-empty">${t("没有匹配的项目")}</div>` : `<div class="application-list">${list.map(projectCard).join("")}</div>`}`;
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
  return `${heading(t("全局"), t("多选技能目录，将同一组技能或预设应用到所有选中目录。"))}${!applications.length ? empty(t("没有可用的全局目录"), t("请到设置中添加全局目录。"), "", "monitor") : `<section class="panel global-controls global-multi-controls"><div class="label-line"><h3>${t("选择技能目录 ")}<span class="muted">${t("可多选")}</span></h3><div>${button(t("全选目录"), "select-global-directories", "quiet small")}${button(t("使用默认目录"), "default-global-directories", "quiet small")}</div></div><div class="settings-directory-grid global-directory-grid">${applications.map((directory) => `<label class="settings-choice global-directory-choice ${selectedGlobalKeys.has(directory.key) ? "selected" : ""}"><input type="checkbox" name="global-directory-selection" value="${e(directory.key)}" ${selectedGlobalKeys.has(directory.key) ? "checked" : ""}>${agentIcon(directory.agentId)}<span><strong>${e(directory.agentName)} · ${e(directory.name)}</strong><code title="${e(directory.path)}">${e(globalPath(directory.path))}</code><small>${applicationStatus(directory.target)}</small></span></label>`).join("")}</div></section><div class="page-note">${icon("info")}<span>${t("部分 Agent 会读取相同的技能目录。当前应用方式：")}${data?.state.settings.deploymentMode === "symlink" ? t("软链接") : t("复制")}${t("。")}${differing ? t("所选目录的技能配置不同，保存后将统一为下方选择。") : ""}</span></div>${selected.length && application ? `<article class="panel application-card" data-scope="global" data-id="${e(selectedGlobalKey)}"><div class="application-heading"><span class="card-icon">${icon("monitor")}</span><div><div class="title-line"><h2>${t("应用到 ")}${selected.length}${t(" 个全局目录")}</h2>${applicationStatus(application.target)}</div><div class="application-destination-list">${selected.map((directory) => `<code title="${e(directory.path)}">${e(globalPath(directory.path))}</code>`).join("")}</div></div></div>${applicationBody("global", selectedGlobalKey, application.target)}</article>` : empty(t("选择全局目录"), t("勾选一个或多个目录，然后选择技能或预设。"), "", "monitor")}`}`;
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
    const target: Target | null = targets.length ? { id, name, kind: "global", path, paths: directories.map((item) => item.path), enabledSkillIds: [...new Set(targets.flatMap((item) => arr(item.enabledSkillIds)))], enabledPresetIds: [...new Set(targets.flatMap((item) => arr(item.enabledPresetIds)))], deployments: targets.flatMap((item) => arr(item.deployments)), pending: targets.some((item) => item.pending) } : null;
    return { name, path, target };
  }
  const application = globalApplications().find((item) => item.key === id);
  return application && { name: `${application.agentName} · ${application.name}`, path: application.path, target: application.target };
}
function applicationStatus(target: Target | null) {
  return !target ? `<span class="badge">${t("尚未配置")}</span>` : target.pending ? `<span class="badge amber">${t("待应用")}</span>` : arr(target.deployments).length ? `<span class="badge green">${t("已应用")}</span>` : `<span class="badge">${t("未选择技能")}</span>`;
}
function projectRoot(path: string) { return path.replace(/\/\.(agents|claude|codex|cursor)\/skills\/?$/, ""); }
function projectCard(project: Target) {
  return `<article class="panel application-card" data-scope="project" data-id="${e(project.id)}"><div class="application-heading"><span class="card-icon">${icon("folder")}</span><div><div class="title-line"><h2>${e(project.name)}</h2>${applicationStatus(project)}</div><p class="mono">${e(project.projectRoot || projectRoot(project.path))}</p><div class="application-destination-list">${targetPaths(project).map((path) => `<code>${e(path)}</code>`).join("")}</div></div><div class="application-actions">${button(t("编辑项目"), "edit-project", "quiet small", `data-id="${e(project.id)}"`)}${button(t("移除"), "delete-project", "quiet small danger-quiet", `data-id="${e(project.id)}"`)}</div></div>${applicationBody("project", project.id, project)}</article>`;
}
function applicationBody(scope: ApplicationScope, id: string, target: Target | null) {
  const selection = applicationSelection(scope, id, target);
  const dirty = applicationDirty(scope, id, target);
  const effective = effectiveSkillIDs(selection);
  const fromPresets = effectiveSkillIDs({ skillIds: [], presetIds: selection.presetIds });
  const plan = plans.get(applicationKey(scope, id));
  const disabled = dirty || !target;
  return `<div class="application-body"><form class="application-selection" data-scope="${scope}" data-id="${e(id)}"><div class="label-line"><h3>${t("直接选择技能")}</h3><span>${skills().length}${t(" 个可用")}</span></div><div class="application-skills">${skills().map((skill) => `<label class="application-skill"><input type="checkbox" name="application-skill" value="${e(skill.id)}" ${selection.skillIds.includes(skill.id) ? "checked" : ""}><span><strong>${e(skill.name)}</strong><small>${e(skill.description || t("暂无描述"))}</small></span>${fromPresets.has(skill.id) ? `<span class="badge">${t("预设已包含")}</span>` : ""}</label>`).join("") || `<p class="muted padded">${t("尚未安装技能，请先从“发现”导入。")}</p>`}</div><div class="application-preset-section"><div class="label-line"><h3>${t("使用预设 ")}<span class="muted">${t("可选")}</span></h3><span>${t("与所选技能合并")}</span></div><div class="preset-options">${presets().map((preset) => `<label class="preset-option"><input type="checkbox" name="application-preset" value="${e(preset.id)}" ${selection.presetIds.includes(preset.id) ? "checked" : ""}><span>${e(preset.name)}<small>${arr(preset.skillIds).length}${t(" 个技能")}</small></span></label>`).join("") || `<p class="muted">${t("还没有预设，也可以直接选择技能。")}</p>`}</div></div><div class="form-footer application-selection-footer"><span class="selection-summary">${t("共 ")}<strong>${effective.size}</strong>${t(" 个技能 ")}<small>${t("重复技能只应用一次 · 已应用 ")}${new Set(arr(target?.deployments).map((item) => item.skillId)).size}${t(" 个")}</small></span><div>${effective.size ? button(t("清空选择"), "clear-application", "quiet small", `data-scope="${scope}" data-id="${e(id)}"`) : ""}${selectionDrafts(scope).has(id) ? button(t("放弃修改"), "reset-application", "quiet small", `data-scope="${scope}" data-id="${e(id)}"`) : ""}<button class="button small" type="submit" ${dirty ? "" : "disabled"}>${scope === "project" ? t("保存项目选择") : t("保存全局选择")}</button></div></div></form>${plan ? planView(plan, scope) : ""}</div><div class="application-bottom"><span class="muted">${dirty ? t("选择尚未保存，请先保存再预览。") : !target ? t("选择技能后保存，再预览并应用到全局。") : t("配置已保存；预览后应用才会更新技能文件。")}</span><div>${button(t("预览变化"), `plan-${scope}`, "", `data-id="${e(id)}" ${disabled ? "disabled" : ""}`, "search")}${button(scope === "project" ? t("应用到项目") : t("应用到全局"), `apply-${scope}`, "primary", `data-id="${e(id)}" ${disabled ? "disabled" : ""}`, "arrow")}</div></div>`;
}
function planView(plan: ApplyPlan, scope: ApplicationScope) {
  return `<div class="plan"><h3>${t("应用预览")}</h3>${arr(plan.directories).length ? `<div class="plan-directories">${arr(plan.directories).map((directory) => `<div><code title="${e(directory.path)}">${e(scope === "global" ? globalPath(directory.path) : directory.path)}</code><span>${t("新增 ")}${arr(directory.add).length}${t(" · 更新 ")}${arr(directory.update).length}${t(" · 移除 ")}${arr(directory.remove).length}${arr(directory.conflicts).length ? t(" · {0} 个冲突", { 0: arr(directory.conflicts).length }) : ""}</span></div>`).join("")}</div>` : ""}<div class="plan-columns">${[[t("新增"), plan.add, "plan-add"], [t("更新"), plan.update, "plan-update"], [t("移除"), plan.remove, "plan-remove"]].map(([title, names, cls]) => `<div class="${cls}"><strong>${title} <span>${(names as string[] || []).length}</span></strong>${arr(names as string[]).length ? `<ul>${arr(names as string[]).map((n) => `<li>${e(n)}</li>`).join("")}</ul>` : `<p>${t("无变化")}</p>`}</div>`).join("")}</div>${arr(plan.conflicts).length ? `<div class="conflict-box"><strong>${t("以下冲突需要先处理，暂不能应用")}</strong><ul>${arr(plan.conflicts).map((c) => `<li>${e(conflictMessage(c))}</li>`).join("")}</ul></div>` : `<p class="plan-safe">${t("没有发现冲突，可以应用以上变化。")}</p>`}</div>`;
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
  const snapshots = arr(data?.snapshots);
  const list = snapshots.filter((snapshot) => `${date(snapshot.createdAt)} ${snapshotReason(snapshot.reason)} ${snapshot.reason} ${snapshot.id}`.toLowerCase().includes(pageQueries.backups.toLowerCase()));
  return `${heading(t("备份与恢复"), t("本地快照保留历史，ZIP 压缩包用于迁移。"), button(t("创建快照"), "create-snapshot", "primary", "", "plus"))}<div class="backup-grid"><section class="panel backup-card"><div class="backup-card-heading"><span class="card-icon">${icon("upload")}</span><h2>${t("导出工作空间")}</h2></div><p>${t("技能文件、来源与预设保存为 ZIP。本机项目与全局配置不随包导出。")}</p>${button(t("导出 ZIP"), "export-backup", "", "", "upload")}</section><section class="panel backup-card"><div class="backup-card-heading"><span class="card-icon">${icon("download")}</span><h2>${t("从压缩包恢复")}</h2></div><p>${t("恢复技能与预设后，重新配置项目与全局技能并手动应用。")}</p>${button(t("导入 ZIP"), "import-backup", "", "", "download")}</section>${skillsManagerCard()}</div><section class="panel snapshots-panel"><div class="panel-toolbar"><div><h2>${t("本地快照")}</h2><p>${t("恢复不会直接修改已应用的技能文件。")}</p></div>${pageSearch("backups", t("搜索时间或快照原因"))}<span class="badge">${list.length}${t(" 个快照")}</span></div>${!snapshots.length ? empty(t("还没有快照"), t("创建一份快照，为当前技能留下可恢复的版本。"), button(t("创建快照"), "create-snapshot", "", "", "archive"), "archive") : !list.length ? `<div class="small-empty">${t("没有匹配的快照")}</div>` : `<div class="snapshot-table"><div class="table-head"><span>${t("创建时间 / 原因")}</span><span>${t("内容")}</span><span></span></div>${list.map((snapshot) => `<div class="snapshot-row"><div><strong>${e(date(snapshot.createdAt))}</strong><small>${e(snapshotReason(snapshot.reason))}</small></div><span>${snapshot.skillCount}${t(" 个技能 ")}<span class="muted">·</span> ${snapshot.presetCount}${t(" 个预设")}</span>${button(t("恢复"), "restore-snapshot", "small", `data-id="${e(snapshot.id)}"`)}</div>`).join("")}</div>`}</section><div class="storage-note">${icon("archive")}<div><strong>${t("本地数据目录")}</strong><code>${e(data?.dataDir || "—")}</code></div></div>`;
}
function skillsManagerCard() {
  return `<section class="panel backup-card skills-manager-card"><div class="skills-manager-card-heading"><div class="backup-card-heading"><span class="card-icon">${icon("download")}</span><h2>${t("从 skills-manager 导入")}</h2></div>${button(t("导入数据库"), "import-skills-manager", "", "", "download")}</div><p>${t("选择 skills-manager 的 .db 数据库，预览后合并技能、预设与来源。保留 racomi 现有数据，导入前自动创建快照。")}</p>${skillsManagerImportResult ? `<div class="skills-manager-result" role="status"><strong>${icon("check")}${t("最近一次导入已完成")}</strong>${skillsManagerResultView(skillsManagerImportResult)}</div>` : ""}</section>`;
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
  modalRoot.innerHTML = `<div class="modal-backdrop"><section class="modal" role="dialog" aria-modal="true" aria-labelledby="modal-title" aria-describedby="modal-description" tabindex="-1"><div class="modal-heading"><div><h2 id="modal-title">${e(title)}</h2><p id="modal-description">${e(description)}</p></div><button type="button" class="icon-button" data-action="close-modal" aria-label="${t("关闭")}">${icon("close")}</button></div>${formID ? `<form id="${formID}">` : ""}<div class="modal-body">${body}</div><div class="modal-footer">${button(t("取消"), "close-modal")}${footer}</div>${formID ? "</form>" : ""}</section></div>`;
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
function presetEditor(id = "") {
  const p = presets().find((p) => p.id === id);
  presetFilters = defaultSkillFilters();
  presetSkillSelection = new Set(arr(p?.skillIds).filter((id) => skills().some((skill) => skill.id === id)));
  modal(p ? t("编辑预设") : t("新建预设"), t("将常用技能组织成一个可复用的预设。"), `<input type="hidden" name="entityId" value="${e(p?.id)}"><label class="field"><span>${t("名称")}</span><input name="name" value="${e(p?.name)}" required placeholder="${t("例如：日常开发")}" maxlength="80"></label><label class="field"><span>${t("描述 ")}<i>${t("可选")}</i></span><textarea name="description" rows="2" placeholder="${t("这个预设适合什么场景？")}">${e(p?.description)}</textarea></label><div class="label-line"><h3>${t("选择技能")}</h3><span id="preset-selection-count"></span></div><div class="preset-skill-filters"><label class="search-field">${icon("search")}<input name="preset-search" aria-label="${t("搜索已安装技能")}" placeholder="${t("搜索名称、描述或来源")}"></label><div class="preset-filter-row"><div class="preset-source-field"><span>${t("来源")}</span>${sourceFilter("preset-source", presetFilters.source)}</div><label><span>${t("状态")}</span><select class="sort-select" name="preset-status" aria-label="${t("技能筛选")}"><option value="all">${t("全部")}</option><option value="updates">${t("有更新")}</option><option value="pending">${t("待应用")}</option></select></label><label><span>${t("排序")}</span><select class="sort-select" name="preset-sort" aria-label="${t("技能排序")}"><option value="asc">${t("名称 A → Z")}</option><option value="desc">${t("名称 Z → A")}</option></select></label></div></div><div class="preset-selection-actions"><span id="preset-filter-count" class="muted"></span><div class="preset-selection-buttons">${button(t("全选"), "select-filtered-preset-skills", "quiet small")}${button(t("反选"), "invert-filtered-preset-skills", "quiet small")}</div></div><div id="preset-skill-list" class="selection-list modal-selection"></div><p class="muted preset-selection-hint">${t("全选与反选仅作用于当前筛选结果，切换筛选会保留已选技能。")}</p>`, `<button class="button primary" type="submit">${t("保存预设")}</button>`, "preset-editor");
  renderPresetSkills();
}
function renderPresetSkills() {
  const form = modalRoot.querySelector<HTMLFormElement>("#preset-editor");
  if (!form) return;
  const container = form.querySelector<HTMLElement>("#preset-skill-list")!;
  const scrollTop = container.scrollTop;
  const focusedSkill = document.activeElement instanceof HTMLInputElement && document.activeElement.name === "skill" ? document.activeElement.value : "";
  const list = filteredSkills(presetFilters);
  form.querySelector("#preset-selection-count")!.setAttribute("aria-live", "polite");
  form.querySelector("#preset-selection-count")!.textContent = t("已选择 {0} 个技能", { 0: presetSkillSelection.size });
  form.querySelector("#preset-filter-count")!.textContent = t("显示 {0} / {1} 个技能", { 0: list.length, 1: skills().length });
  const select = form.querySelector<HTMLButtonElement>('[data-action="select-filtered-preset-skills"]')!;
  select.disabled = !list.length;
  select.textContent = list.length && list.every((skill) => presetSkillSelection.has(skill.id)) ? t("取消全选") : t("全选");
  form.querySelector<HTMLButtonElement>('[data-action="invert-filtered-preset-skills"]')!.disabled = !list.length;
  form.querySelector("#preset-skill-list")!.innerHTML = list.map((skill) => `<label class="selection-row"><input type="checkbox" name="skill" value="${e(skill.id)}" ${presetSkillSelection.has(skill.id) ? "checked" : ""}><span><strong>${e(skill.name)}</strong><small>${e(skill.description)}</small><small class="skill-origin">${e(originLabel(skill))}</small></span></label>`).join("") || `<p class="muted padded">${skills().length ? t("没有匹配的技能，试试其他筛选条件。") : t("尚未安装技能。你也可以先创建空的预设。")}</p>`;
  container.scrollTop = scrollTop;
  if (focusedSkill) [...container.querySelectorAll<HTMLInputElement>('input[name="skill"]')].find((input) => input.value === focusedSkill)?.focus({ preventScroll: true });
}
function projectEditor(id = "") {
  const project = projects().find((item) => item.id === id);
  const root = project ? project.projectRoot || projectRoot(project.path) : "";
  const deployed = !!arr(project?.deployments).length;
  const defaults = data?.state.settings.defaultProjectDirectories || ["agents"];
  const directoryIDs = project?.directoryIds !== undefined ? arr(project.directoryIds) : project ? projectDirectories.filter((directory) => targetPaths(project).some((path) => path === `${root}/${directory.path}`)).map((directory) => directory.id) : defaults;
  const templates = arr(data?.state.settings.customProjectDirectories);
  const snapshots = arr(project?.customPaths);
  const matchingSnapshot = (path: string) => snapshots.find((snapshot) => resolveProjectPath(root, snapshot) === resolveProjectPath(root, path));
  const customOptions = [
    ...templates.map((directory) => ({ name: directory.name, path: matchingSnapshot(directory.path) || directory.path, checked: project ? matchingSnapshot(directory.path) !== undefined : defaults.includes(directory.id) })),
    ...snapshots.filter((path) => !templates.some((directory) => resolveProjectPath(root, directory.path) === resolveProjectPath(root, path))).map((path) => ({ name: t("当前项目目录"), path, checked: true })),
  ];
  modal(project ? t("编辑项目") : t("添加项目"), t("选择项目根目录与技能目录，保存后为该项目挑选技能。"), `<input type="hidden" name="entityId" value="${e(project?.id)}"><label class="field"><span>${t("项目根目录")}</span><div class="input-with-button"><input name="path" value="${e(root)}" required ${deployed ? "readonly" : ""} placeholder="/Users/you/Projects/my-project">${button(t("选择目录"), "choose-project-dir", "small", deployed ? "disabled" : "")}</div><small>${deployed ? t("修改根目录前，请先清空技能选择，保存并应用以移除已应用技能。") : t("选择项目文件夹，然后勾选一个或多个技能目录。")}</small></label><label class="field"><span>${t("项目名称 ")}<i>${t("可选")}</i></span><input name="name" value="${e(project?.name)}" placeholder="${t("默认使用项目文件夹名称")}" maxlength="80"></label><fieldset class="settings-fieldset project-directory-options"><legend>${t("项目技能目录 · 可多选")}</legend><div class="settings-directory-grid">${projectDirectories.map((directory) => `<label class="settings-choice"><input type="checkbox" name="project-directory" value="${directory.id}" ${directoryIDs.includes(directory.id) ? "checked" : ""}>${agentIcon(directory.id)}<span><strong>${e(directory.name)}</strong><code>${e(directory.path)}</code></span></label>`).join("")}${customOptions.map((directory) => `<label class="settings-choice"><input type="checkbox" name="project-custom-path" value="${e(directory.path)}" ${directory.checked ? "checked" : ""}>${agentIcon("custom")}<span><strong>${e(directory.name)}</strong><code>${e(directory.path)}</code></span></label>`).join("")}</div><p class="muted">${t("自定义目录在设置中管理。")}</p></fieldset><div class="info-note"><strong>${t("应用目录")}</strong><div id="project-destination" class="application-destination-list"></div><p>${t("当前应用方式：")}${data?.state.settings.deploymentMode === "symlink" ? t("软链接") : t("复制")}${t("。保存项目仅记录配置，预览后应用才会更新技能文件。")}</p></div>`, `<button class="button primary" type="submit">${t("保存项目")}</button>`, "project-editor");
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
  modal(directory ? t("编辑项目目录") : t("添加项目目录"), t("保存可复用的项目目录，供项目选择。"), `<input type="hidden" name="entityId" value="${e(directory?.id)}"><label class="field"><span>${t("名称")}</span><input name="name" value="${e(directory?.name)}" required maxlength="80" placeholder="${e(t("例如：团队技能目录"))}"></label><label class="field"><span>${t("技能目录")}</span><input name="path" value="${e(directory?.path)}" required placeholder=".my-agent/skills"><small>${directory && absoluteProjectTemplatePath(directory.path) ? t("历史绝对路径可保留；修改路径时请填写相对路径。") : t("填写相对项目根目录的位置，例如 .my-agent/skills。")}</small></label><div class="info-note">${t("修改模板不会更改项目已保存的应用目录。")}</div>`, `<button class="button primary" type="submit">${t("保存目录")}</button>`, "project-directory-editor");
}

function globalDirectoryEditor(id = "") {
  const application = globalApplications().find((item) => item.key === id && item.agentId === "custom");
  if (id && !application) return;
  const deployed = !!arr(application?.target?.deployments).length;
  modal(application ? t("编辑全局目录") : t("添加自定义目录"), t("选择技能实际存放的目录，保存后可与内置目录一起多选应用。"), `<input type="hidden" name="entityId" value="${e(application?.key)}"><label class="field"><span>${t("名称")}</span><input name="name" value="${e(application?.name)}" required maxlength="80" placeholder="${e(t("例如：团队技能目录"))}"></label><label class="field"><span>${t("技能目录")}</span><div class="input-with-button"><input name="path" value="${e(application?.path)}" required ${deployed ? "readonly" : ""} placeholder="/Users/you/.my-agent/skills">${button(t("选择目录"), "choose-global-dir", "small", deployed ? "disabled" : "")}</div><small>${deployed ? t("修改目录前，请先清空技能选择，保存并应用。") : t("填写绝对路径，或选择一个文件夹。")}</small></label>`, `<button class="button primary" type="submit">${t("保存目录")}</button>`, "global-directory-editor");
}

function backupEditor(importing: boolean, path = "") {
  modal(importing ? t("导入工作空间备份") : t("导出工作空间"), importing ? t("恢复技能与预设后，请为项目与全局重新选择技能并手动应用。") : t("压缩包包含技能文件、来源和预设，不包含本机项目与全局配置。"), `<label class="field"><span>${importing ? t("备份 ZIP 文件") : t("导出 ZIP 路径")}</span><div class="input-with-button"><input name="backupPath" value="${e(path)}" required placeholder="/Users/you/Backups/racomi.zip">${button(t("选择文件"), "choose-backup-file", "small", `data-mode="${importing ? "import" : "export"}"`)}</div><small>${t("可直接填写绝对路径，或使用文件选择器。")}</small></label>`, `<button class="button ${importing ? "danger" : "primary"}" type="submit">${importing ? t("下一步") : t("导出备份")}</button>`, importing ? "backup-import" : "backup-export");
}
async function skillsManagerEditor() {
  skillsManagerPreview = null;
  modal(t("导入 skills-manager 数据库"), t("先预览数据库内容，再确认合并到当前工作空间。"), `<label class="field"><span>${t("skills-manager 数据库")}</span><div class="input-with-button"><input name="skillsManagerPath" required placeholder="${t("选择或填写 .db 文件的绝对路径")}">${button(t("选择文件"), "choose-skills-manager-file", "small")}</div><small>${t("自动建议 skills-manager 默认数据库路径，也可选择其他 .db 文件。")}</small></label><div class="info-note"><strong>${t("合并导入并保留现有数据")}</strong><p>${t("导入前自动创建快照。旧数据库与已应用的技能保持不变；旧项目配置和符号链接部署不会自动接管，之后请在“项目”或“全局”中配置并手动应用。")}</p></div><div id="skills-manager-summary" aria-live="polite"></div>`, `<button class="button" type="submit">${t("预览数据库")}</button>` + button(t("合并导入"), "confirm-skills-manager-import", "primary", "hidden"), "skills-manager-import");
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
    enableNativeThemeSync(); enableNativeLocaleSync();
  } catch (err) {
    if (revision === stateRevision && settingsRevision === settingsWriteRevision && !settingsAutosave.busy) { loadError = err instanceof Error ? err.message : String(err); render(); }
  }
}
async function doPreview(source: string, ref: string, subpath: string, matchName = "") {
  if (activeTasks.has("正在读取仓库并发现技能…")) return;
  sourceDraft = source; refDraft = ref; subpathDraft = subpath;
  preview = null; selectedCandidates.clear();
  if (currentPage === "discover") render();
  const result = await task("正在读取仓库并发现技能…", () => api.preview(source, ref, subpath));
  if (result) {
    preview = result;
    selectedCandidates = new Set(arr(result.candidates).filter((c) => matchName ? c.name === matchName || c.subpath.split("/").pop() === matchName : true).map((c) => c.subpath));
    if (matchName && !selectedCandidates.size) toast(t("仓库预览已完成，请根据名称手动选择技能。"));
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
  const input = ev.target as HTMLInputElement;
  if (input.id === "skills-search") {
    skillsQuery = input.value;
    render();
    if (detailTab === "document" && selectedSkill) void loadSkillDocument(selectedSkill);
  }
  if (input.dataset.pageSearch && ["sources", "presets", "projects", "backups"].includes(input.dataset.pageSearch)) {
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
});
app.addEventListener("change", (ev) => {
  const input = ev.target as HTMLInputElement;
  if (input.name === "bulk-skill") {
    if (hasPendingMutation()) { render(); return; }
    if (input.checked) bulkSkillSelection.add(input.value); else bulkSkillSelection.delete(input.value);
    render();
    [...app.querySelectorAll<HTMLInputElement>('[name="bulk-skill"]')].find((item) => item.value === input.value)?.focus({ preventScroll: true });
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
  if (input.name === "locale-preference") { setLocale(input.value as LocalePreference); render(); }
  if (input.name === "global-directory-selection") {
    if (input.checked) selectedGlobalKeys.add(input.value); else selectedGlobalKeys.delete(input.value);
    selectedGlobalKey = globalBatchID();
    render();
    [...document.querySelectorAll<HTMLInputElement>('[name="global-directory-selection"]')].find((item) => item.value === input.value)?.focus({ preventScroll: true });
  }
  if (input.form?.classList.contains("application-selection")) {
    const form = input.form;
    const scope = form.dataset.scope as ApplicationScope;
    const id = form.dataset.id!;
    selectionDrafts(scope).set(id, { skillIds: selectedIDs(form, "application-skill"), presetIds: selectedIDs(form, "application-preset") });
    plans.delete(applicationKey(scope, id));
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
      if (selectAll) selectAll.textContent = available.every((entry) => selection.has(entry.item.subpath)) ? t("取消全选") : t("全选变化");
    }
  }
});
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
  else if (input.name === "preset-status") presetFilters.status = input.value as SkillsFilter;
  else if (input.name === "preset-sort") presetFilters.sort = input.value === "desc" ? "desc" : "asc";
  else return;
  renderPresetSkills();
});
document.addEventListener("submit", async (ev) => {
  const form = ev.target as HTMLFormElement;
  const formID = form.getAttribute("id") || "";
  if (formID === "settings-editor") { ev.preventDefault(); return; }
  if (!["hub-search", "source-preview", "source-editor", "source-relink", "preset-editor", "project-editor", "global-directory-editor", "project-directory-editor", "backup-import", "backup-export", "skills-manager-import"].includes(formID) && !form.classList.contains("application-selection")) return;
  ev.preventDefault();
  const submit = form.querySelector<HTMLButtonElement>('button[type="submit"]');
  if (submit?.disabled) return;
  if (submit) submit.disabled = true;
  try {
    if (formID === "hub-search") {
      searchQuery = field(form, "query");
      const result = await task("正在搜索 skills.sh…", () => api.search(searchQuery));
      if (result) { searchResults = result; if (currentPage === "discover") render(); }
    } else if (formID === "source-preview") {
      await doPreview(field(form, "source"), field(form, "ref"), field(form, "subpath"));
    } else if (formID === "source-editor") {
      const id = field(form, "entityId");
      const existing = sources().find((source) => source.id === id);
      const source: Source = { id, name: field(form, "name"), repository: field(form, "repository"), ref: field(form, "ref"), subpath: field(form, "subpath"), mode: field(form, "mode") === "all" ? "all" : "selected", revision: existing?.revision || "", refreshedAt: existing?.refreshedAt || "", lastError: existing?.lastError || "", catalog: arr(existing?.catalog), warnings: existing?.warnings };
      const previousIDs = new Set(sources().map((item) => item.id));
      const next = await task("正在保存来源…", () => api.saveSource(source));
      if (next) {
        if (existing && existing.mode !== source.mode) sourceSelections.delete(id);
        selectedSource = id || arr(next.state.sources).find((item) => !previousIDs.has(item.id))?.id || "";
        currentPage = "sources"; pageQueries.sources = "";
        accept(next); closeSubmittedModal(form);
        toast(existing ? t("来源已保存，点击同步时使用新的安装方式。") : t("来源已保存，刷新后查看仓库技能。"));
      }
    } else if (formID === "source-relink") {
      const id = field(form, "sourceId");
      if (await mutate("正在重新关联技能…", () => api.relinkSourceSkill(field(form, "skillId"), id, field(form, "subpath")), t("技能已关联并更新，预设引用已保留，相关项目与全局配置需手动应用。"))) { updates = null; closeSubmittedModal(form); }
    } else if (formID === "preset-editor") {
      const preset: Preset = { id: field(form, "entityId"), name: field(form, "name"), description: field(form, "description"), skillIds: [...presetSkillSelection].filter((id) => skills().some((skill) => skill.id === id)) };
      if (await mutate("正在保存预设…", () => api.savePreset(preset), t("预设已保存，相关项目与全局配置需手动应用。"))) closeSubmittedModal(form);
    } else if (formID === "project-editor") {
      if (applicationBusy()) { toast(t("工作空间正在更新，请完成后保存项目。"), true); return; }
      const existing = projects().find((project) => project.id === field(form, "entityId"));
      const path = field(form, "path");
      if (!path) { toast(t("请选择项目根目录。"), true); return; }
      const name = field(form, "name") || projectRoot(path).replace(/\/+$/, "").split("/").pop() || t("项目");
      const directoryIds = selectedIDs(form, "project-directory");
      const customPaths = [...new Set(selectedIDs(form, "project-custom-path"))];
      if (!directoryIds.length && !customPaths.length) { toast(t("请至少选择一个项目技能目录。"), true); return; }
      const project: Target = { id: field(form, "entityId"), name, kind: "project", path, directoryIds, customPaths, enabledSkillIds: arr(existing?.enabledSkillIds), enabledPresetIds: arr(existing?.enabledPresetIds), deployments: arr(existing?.deployments), pending: existing?.pending || false };
      const previousIDs = new Set(projects().map((item) => item.id));
      const next = await task("正在保存项目…", () => api.saveProject(project));
      if (next) {
        const showResult = form.isConnected && modalRoot.contains(form);
        const created = !existing ? arr(next.projects).find((item) => !previousIDs.has(item.id)) : undefined;
        if (showResult && created) { currentPage = "projects"; pageQueries.projects = ""; }
        accept(next); closeSubmittedModal(form); toast(t("项目已保存，可以直接选择技能或使用预设。"));
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
        return api.saveSettings({ ...settings, customProjectDirectories });
      }, t("项目目录已保存。"))) closeSubmittedModal(form);
    } else if (formID === "global-directory-editor") {
      if (applicationBusy()) { toast(t("工作空间正在更新，请完成后保存目录。"), true); return; }
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
        if (next) { projectDrafts.clear(); globalDrafts.clear(); updates = null; accept(next); toast(t("工作空间已从备份恢复，请重新配置项目与全局技能。")); }
        else if (!modalRoot.children.length) backupEditor(true, path);
      } else if (!modalRoot.children.length) backupEditor(true, path);
    } else if (form.classList.contains("application-selection")) {
      if (applicationBusy()) { toast(t("工作空间正在更新，请完成后保存选择。"), true); return; }
      const scope = form.dataset.scope as ApplicationScope;
      const id = form.dataset.id!;
      const skillIDs = selectedIDs(form, "application-skill"), presetIDs = selectedIDs(form, "application-preset");
      await mutate(scope === "project" ? "正在保存项目选择…" : "正在保存全局选择…", () => scope === "project" ? api.setProjectSelection(id, skillIDs, presetIDs) : api.setGlobalSelections(globalBatchKeys(id), skillIDs, presetIDs), t("{0}选择已保存，预览后应用才会生效。", { 0: scope === "project" ? t("项目") : t("全局") }));
    }
  } finally { if (submit) submit.disabled = false; }
});
document.addEventListener("click", async (ev) => {
  const target = (ev.target as Element).closest<HTMLButtonElement>("button");
  if (!target || target.disabled) return;
  if (target.dataset.themePreference) { setThemePreference(target.dataset.themePreference as ThemePreference); render(); return; }
  if (target.dataset.page) {
    currentPage = target.dataset.page as Page; render();
    if (currentPage === "skills" && detailTab === "document" && selectedSkill) void loadSkillDocument(selectedSkill);
    return;
  }
  const action = target.dataset.action;
  const id = target.dataset.id || "";
  if (!action) return;
  if (action === "focus-search") { focusSearch(); return; }
  if (action === "select-filtered-preset-skills" || action === "invert-filtered-preset-skills") {
    presetSkillSelection = changeSelection(presetSkillSelection, filteredSkills(presetFilters).map((skill) => skill.id), action === "invert-filtered-preset-skills" ? "invert" : "toggle-all");
    renderPresetSkills(); return;
  }
  if (["select-all-skills", "invert-skills", "clear-skill-selection"].includes(action)) {
    if (hasPendingMutation()) return;
    bulkSkillSelection = action === "clear-skill-selection" ? new Set() : changeSelection(bulkSkillSelection, filteredSkills().map((skill) => skill.id), action === "invert-skills" ? "invert" : "toggle-all");
    render(); return;
  }
  if (action === "open-backups" || action === "close-backups") { settingsBackups = action === "open-backups"; render(); return; }
  if (action === "filter-skills") {
    skillsFilter = target.dataset.filter as SkillsFilter; render();
    if (detailTab === "document" && selectedSkill) void loadSkillDocument(selectedSkill);
    return;
  }
  if (action === "discover-tab") { discoverTab = target.dataset.tab as "hub" | "git"; render(); return; }
  if (action === "source-filter") { sourceCatalogFilter = target.dataset.filter === "all" ? "all" : "changes"; render(); return; }
  if (action === "detail-tab") {
    detailTab = target.dataset.tab as "overview" | "document"; render();
    if (detailTab === "document") void loadSkillDocument(selectedSkill);
    return;
  }
  if (["plan-project", "apply-project", "plan-global", "apply-global"].includes(action)) {
    if (applicationBusy()) { toast(t("工作空间正在更新，请完成后重新预览并应用。"), true); return; }
    const scope: ApplicationScope = action.endsWith("global") ? "global" : "project";
    const saved = findApplication(scope, id);
    if (!saved?.target || applicationDirty(scope, id, saved.target)) { toast(t("请先保存当前技能和预设选择，再预览或应用。"), true); return; }
  }
  if (action === "select-global-directories" || action === "default-global-directories") {
    if (applicationBusy()) return;
    selectedGlobalKeys = new Set(action === "select-global-directories" ? globalApplications().map((item) => item.key) : data?.state.settings.defaultGlobalDirectories || []);
    selectedGlobalKey = globalBatchID(); render(); return;
  }
  if (action === "clear-application" || action === "reset-application") {
    const scope = target.dataset.scope as ApplicationScope;
    if (action === "clear-application") selectionDrafts(scope).set(id, { skillIds: [], presetIds: [] });
    else selectionDrafts(scope).delete(id);
    plans.delete(applicationKey(scope, id)); render(); return;
  }
  if (action === "close-modal") { closeModal(); return; }
  if (action === "confirm-modal") { closeModal(true); return; }
  target.disabled = true;
  try {
    if (action === "reload") await load();
    else if (action === "go-discover") { currentPage = "discover"; render(); }
    else if (action === "select-source") { selectedSource = id; render(); }
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
      if (source && await confirm(t("删除来源"), t("确定删除“{0}”？已安装技能及其预设引用会保留，并解除与此来源的关联。", { 0: source.name }), "", t("删除来源"), true)) {
        updates = null;
        await mutate("正在删除来源…", () => api.deleteSource(id), t("来源已删除，已安装技能已保留。"));
      }
    } else if (action === "relink-source-skill") {
      const source = sources().find((item) => item.id === id);
      if (source) relinkSourceEditor(source, target.dataset.subpath || "");
    } else if (action === "view-local-skill") { selectedSkill = id; skillsQuery = ""; skillsFilter = "all"; skillsSourceFilter = "all"; currentPage = "skills"; render(); if (detailTab === "document") void loadSkillDocument(id); }
    else if (action === "select-skill") { selectedSkill = id; render(); if (detailTab === "document") void loadSkillDocument(id); }
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
      if (r && r.supported !== false) await doPreview(r.source, "", "", r.slug?.split("/").pop() || r.name);
    } else if (action === "select-all-candidates") {
      selectedCandidates = selectedCandidates.size === arr(preview?.candidates).length ? new Set() : new Set(arr(preview?.candidates).map((c) => c.subpath)); render();
    } else if (action === "install" && preview && selectedCandidates.size) {
      if (await mutate("正在导入所选技能…", () => api.install(preview!.id, [...selectedCandidates]), t("所选技能已导入本地。"))) { preview = null; selectedCandidates.clear(); currentPage = "skills"; render(); }
    } else if (action === "delete-preset") {
      const p = presets().find((p) => p.id === id);
      if (p && await confirm(t("删除预设"), t("确定删除“{0}”？技能原件会保留，相关项目与全局配置需重新应用。", { 0: p.name }), "", t("删除预设"), true)) await mutate("正在删除预设…", () => api.deletePreset(id), t("预设已删除。") );
    } else if (action === "delete-project") {
      if (applicationBusy()) { toast(t("工作空间正在更新，请完成后移除项目。"), true); return; }
      const project = projects().find((item) => item.id === id);
      if (project && arr(project.deployments).length) { toast(t("请先清空该项目的技能与预设选择，保存并应用以移除已应用技能，再移除项目。"), true); return; }
      if (project && await confirm(t("移除项目"), t("确定停止管理“{0}”？此项目当前没有由 racomi 应用的技能。", { 0: project.name }), "", t("移除项目"), true)) {
        if (applicationBusy()) { toast(t("工作空间正在更新，请完成后移除项目。"), true); return; }
        await mutate("正在移除项目…", () => api.deleteProject(id), t("项目已移除。"));
      }
    } else if (action === "delete-project-directory") {
      const directory = data?.state.settings.customProjectDirectories.find((item) => item.id === id);
      if (!directory) return;
      if (await confirm(t("删除项目目录"), t("删除后不再出现在可选目录中，现有项目保留已保存的路径。"), `<p><strong>${e(directory.name)}</strong></p><p class="mono">${e(directory.path)}</p>`, t("删除目录"), true)) {
        await mutate("正在删除项目目录…", async () => {
          const settings = data!.state.settings;
          return api.saveSettings({ ...settings, customProjectDirectories: settings.customProjectDirectories.filter((item) => item.id !== id) });
        }, t("项目目录已删除。"));
      }
    } else if (action === "delete-global-directory") {
      if (applicationBusy()) { toast(t("工作空间正在更新，请完成后删除目录。"), true); return; }
      const application = globalApplications().find((item) => item.key === id && item.agentId === "custom");
      if (!application) return;
      if (arr(application.target?.deployments).length) { toast(t("请先清空该目录的技能与预设选择，保存并应用，再删除目录。"), true); return; }
      if (await confirm(t("删除全局目录"), t("确定停止管理“{0}”？此目录当前没有由 racomi 应用的技能。", { 0: application.name }), `<p class="mono" title="${e(application.path)}">${e(globalPath(application.path))}</p>`, t("删除目录"), true)) {
        if (applicationBusy()) { toast(t("工作空间正在更新，请完成后删除目录。"), true); return; }
        const previousID = selectedGlobalKey;
        const draft = globalDrafts.get(previousID);
        const next = await task("正在删除全局目录…", () => api.deleteGlobalDirectory(id));
        if (next) {
          if (selectedGlobalKeys.delete(id) && draft) globalDrafts.set(globalBatchID(), draft);
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
      if (modalGeneration !== dialogGeneration || currentPage !== (scope === "project" ? "projects" : "global") || scope === "global" && selectedGlobalKey !== id) { toast(t("预览已完成，请回到相应配置确认应用。")); return; }
      if (arr(plan.conflicts).length) { toast(t("技能目录存在冲突，请先处理预览中列出的文件。"), true); return; }
      if (!arr(plan.add).length && !arr(plan.update).length && !arr(plan.remove).length) { toast(t("没有需要应用的变化。")); return; }
      const label = scope === "project" ? t("应用到项目") : t("应用到全局");
      if (await confirm(label, t("将为“{0}”应用以下技能变化。", { 0: application.name }), `<div class="application-confirm"><strong>${e(application.name)}</strong><code title="${e(application.path)}">${e(scope === "global" ? application.path.split("\n").map(globalPath).join("\n") : application.path)}</code></div>${planView(plan, scope)}`, t("确认应用"))) {
        const latest = findApplication(scope, id);
        if (revision !== stateRevision || applicationBusy() || !latest || applicationDirty(scope, id, latest.target)) { toast(t("工作空间已变化，请重新预览后应用。"), true); return; }
        await mutate(scope === "project" ? "正在应用技能到项目…" : "正在应用技能到全局…", () => scope === "project" ? api.applyProject(id) : api.applyGlobals(globalBatchKeys(id)), t("{0} 的技能已应用。", { 0: application.name }));
      }
    } else if (action === "choose-project-dir") {
      const input = modalRoot.querySelector<HTMLInputElement>('#project-editor input[name="path"]');
      const path = await task("正在选择项目目录…", () => api.chooseDirectory());
      if (path && input?.isConnected) { input.value = path; updateProjectDestination(); }
    } else if (action === "choose-global-dir") {
      const input = modalRoot.querySelector<HTMLInputElement>('#global-directory-editor input[name="path"]');
      const path = await task("正在选择技能目录…", () => api.chooseDirectory());
      if (path && input?.isConnected) { input.value = path; updateProjectDestination(); }
    } else if (action === "create-snapshot") await mutate("正在创建本地快照…", () => api.createSnapshot(), t("本地快照已创建。") );
    else if (action === "restore-snapshot") {
      if (await confirm(t("恢复本地快照"), t("将恢复技能、来源与预设，并清空项目与全局已保存的技能选择。已应用的技能文件不会直接改动，之后请重新选择并应用。"), "", t("恢复快照"), true)) {
        const next = await task("正在恢复快照…", () => api.restoreSnapshot(id));
        if (next) { projectDrafts.clear(); globalDrafts.clear(); updates = null; accept(next); toast(t("快照已恢复，请为项目与全局重新选择技能并手动应用。")); }
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
  const input = modalRoot.children.length ? modalRoot.querySelector<HTMLInputElement>('input:not([type="hidden"]):not([type="checkbox"])') : document.querySelector<HTMLInputElement>("input[data-page-search]");
  input?.focus();
  input?.select();
}
document.addEventListener("racomi:locale-change", () => render());
document.addEventListener("racomi:theme-change", () => {
  for (const button of document.querySelectorAll<HTMLButtonElement>("button[data-theme-preference]")) {
    const active = button.dataset.themePreference === getThemePreference();
    button.classList.toggle("active", active);
    if (button.closest(".settings-theme-options")) button.classList.toggle("selected", active);
    button.setAttribute("aria-pressed", String(active));
  }
});
document.addEventListener("keydown", (ev) => {
  if (ev.isComposing || ev.keyCode === 229) return;
  if ((ev.metaKey || ev.ctrlKey) && ev.key.toLowerCase() === "k") { ev.preventDefault(); focusSearch(); return; }
  if (ev.key === "Escape" && modalRoot.children.length) { ev.preventDefault(); if (!activeTasks.has(skillsManagerImportTask)) closeModal(); return; }
  if (["ArrowLeft", "ArrowRight", "Home", "End"].includes(ev.key) && document.activeElement?.getAttribute("role") === "tab") {
    const tabs = [...document.activeElement.parentElement!.querySelectorAll<HTMLButtonElement>('[role="tab"]')];
    const current = tabs.indexOf(document.activeElement as HTMLButtonElement);
    const index = ev.key === "Home" ? 0 : ev.key === "End" ? tabs.length - 1 : (current + (ev.key === "ArrowLeft" ? -1 : 1) + tabs.length) % tabs.length;
    ev.preventDefault(); tabs[index]?.click();
    const activeTab = document.querySelector<HTMLButtonElement>(currentPage === "discover" ? '.discover-tabs [aria-selected="true"]' : '.detail-tabs [aria-selected="true"]');
    activeTab?.focus();
  }
  if (ev.key === "Tab" && modalRoot.children.length) {
    const focusable = [...modalRoot.querySelectorAll<HTMLElement>('button:not(:disabled),input:not([type="hidden"]):not(:disabled),textarea:not(:disabled),select:not(:disabled),summary')].filter((element) => element.getClientRects().length > 0);
    const first = focusable[0], last = focusable[focusable.length - 1];
    if (ev.shiftKey && (document.activeElement === first || !modalRoot.contains(document.activeElement))) { ev.preventDefault(); last?.focus(); }
    if (!ev.shiftKey && (document.activeElement === last || !modalRoot.contains(document.activeElement))) { ev.preventDefault(); first?.focus(); }
  }
});
installSourceFilters();
void load();
