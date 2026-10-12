import type { AppSettings, GlobalApplication } from "./api";
import { getLocalePreference, supportedLocales, t } from "./i18n";
import { icon } from "./icons";
import { agentIcon } from "./agent-icons";
import { homeRelativePath } from "./paths";
import { getThemePreference } from "./theme";
import { getNavLayoutPreference } from "./layout";

export const projectDirectories = [
  { id: "agents", name: "共享技能目录", path: ".agents/skills", description: "Codex 与 Cursor 共用的项目技能目录。" },
  { id: "claude", name: "Claude Code", path: ".claude/skills", description: "Claude Code 的项目技能目录，Cursor 也可兼容读取。" },
  { id: "codex", name: "Codex（兼容）", path: ".codex/skills", description: "保留已有 .codex/skills 配置；新项目建议使用 .agents/skills。" },
  { id: "cursor", name: "Cursor", path: ".cursor/skills", description: "Cursor 的项目技能目录。" },
];

export const defaultSettings = (): AppSettings => ({ deploymentMode: "copy", defaultProjectDirectories: ["agents"], defaultGlobalDirectories: ["codex-user"], customProjectDirectories: [] });
const e = (value: unknown) => String(value ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);
const directoryActions = (kind: "project" | "global", id: string, name: string) => {
  const editLabel = `${kind === "project" ? t("编辑项目目录") : t("编辑全局目录")}: ${name}`;
  const deleteLabel = `${kind === "project" ? t("删除项目目录") : t("删除全局目录")}: ${name}`;
  return `<div class="settings-directory-actions"><button type="button" class="icon-button" data-action="edit-${kind}-directory" data-id="${e(id)}" title="${e(editLabel)}" aria-label="${e(editLabel)}">${icon("edit")}</button><button type="button" class="icon-button danger-quiet" data-action="delete-${kind}-directory" data-id="${e(id)}" title="${e(deleteLabel)}" aria-label="${e(deleteLabel)}">${icon("trash")}</button></div>`;
};

export function settingsPage(settings: AppSettings, applications: GlobalApplication[], dataDir: string, homeDir?: string) {
  const themeOptions = ([ ["light", "Light", "sun"], ["dark", "Dark", "moon"], ["system", "Follow system", "monitor"] ] as const).map(([id, label, symbol]) => `<button type="button" class="theme-option settings-icon-option" data-theme-preference="${id}" title="${e(t(label))}" aria-label="${e(t(label))}" aria-pressed="${getThemePreference() === id}">${icon(symbol)}</button>`).join("");
  const navLayoutOptions = ([ ["auto", "自动", "layout-auto"], ["side", "侧边栏", "layout-side"], ["top", "顶部导航", "layout-top"] ] as const).map(([id, label, symbol]) => `<button type="button" class="theme-option settings-icon-option" data-nav-layout-preference="${id}" title="${e(t(label))}" aria-label="${e(t(label))}" aria-pressed="${getNavLayoutPreference() === id}">${icon(symbol)}</button>`).join("");
  const modeOptions = ([ ["copy", "Copy", "copy"], ["symlink", "Symbolic link", "link"] ] as const).map(([id, label, symbol]) => `<label class="theme-option settings-icon-option" title="${e(t(label))}"><input class="sr-only" type="radio" name="deploymentMode" value="${id}" ${settings.deploymentMode === id ? "checked" : ""} aria-label="${e(t(label))}" aria-describedby="settings-deployment-help">${icon(symbol)}</label>`).join("");
  const projectOptions = [...projectDirectories.map((directory) => ({ ...directory, name: t(directory.name), description: t(directory.description), custom: false })), ...settings.customProjectDirectories.map((directory) => ({ ...directory, description: "", custom: true }))].map((directory) => `<div class="settings-directory-entry"><label class="settings-directory-option"><input type="checkbox" name="defaultProjectDirectories" value="${e(directory.id)}" ${settings.defaultProjectDirectories.includes(directory.id) ? "checked" : ""}>${agentIcon(directory.custom ? "custom" : directory.id)}<span><strong ${directory.description ? `title="${e(directory.description)}"` : ""}>${e(directory.name)}</strong><code title="${e(directory.path)}">${e(directory.path)}</code></span></label>${directory.custom ? directoryActions("project", directory.id, directory.name) : ""}</div>`).join("");
  const globalOptions = applications.map((directory) => `<div class="settings-directory-entry"><label class="settings-directory-option" title="${e(directory.description)}"><input type="checkbox" name="defaultGlobalDirectories" value="${e(directory.key)}" ${settings.defaultGlobalDirectories.includes(directory.key) ? "checked" : ""}>${agentIcon(directory.agentId)}<span><strong>${e(directory.agentName)} · ${e(directory.name)}</strong><code title="${e(directory.path)}">${e(homeRelativePath(directory.path, homeDir))}</code></span></label>${directory.agentId === "custom" ? directoryActions("global", directory.key, directory.name) : ""}</div>`).join("");

  return `<div class="settings-page">
    <header class="page-heading"><div><h2 class="page-section-title">${e(t("Settings"))}</h2><p class="page-description">${e(t("Customize appearance, language, and skill deployment defaults."))}</p></div></header>
    <div class="settings-layout">
      <section class="settings-section" aria-labelledby="settings-appearance-title">
        <h2 id="settings-appearance-title" class="settings-section-title">${e(t("Appearance"))}</h2>
        <div class="settings-group">
          <div class="settings-row"><div class="settings-row-copy"><strong>${e(t("Theme"))}</strong><p>${e(t("Choose a theme or follow your system appearance."))}</p></div><div class="theme-picker settings-theme-options settings-icon-switch" role="group" aria-label="${e(t("Theme"))}">${themeOptions}</div></div>
          <div class="settings-row"><div class="settings-row-copy"><strong>${e(t("导航布局"))}</strong><p>${e(t("根据窗口宽度自动切换，或固定使用侧边栏与顶部导航。"))}</p></div><div class="theme-picker settings-nav-layout-options settings-icon-switch" role="group" aria-label="${e(t("导航布局"))}">${navLayoutOptions}</div></div>
          <label class="settings-row"><span class="settings-row-copy"><strong>${e(t("Language"))}</strong><span class="settings-row-description">${e(t("Choose the language used throughout racomi."))}</span></span><select name="locale-preference" aria-label="${e(t("Language"))}"><option value="system" ${getLocalePreference() === "system" ? "selected" : ""}>${e(t("Follow system"))}</option>${supportedLocales.map((locale) => `<option value="${locale.id}" ${getLocalePreference() === locale.id ? "selected" : ""}>${e(locale.label)}</option>`).join("")}</select></label>
        </div>
      </section>
      <form id="settings-editor" class="settings-section" aria-labelledby="settings-deployment-title">
        <h2 id="settings-deployment-title" class="settings-section-title">${e(t("Deployment"))}</h2>
        <div class="settings-group">
          <div class="settings-row"><div class="settings-row-copy"><strong>${e(t("Deployment method"))}</strong><p id="settings-deployment-help">${e(t("Copy keeps separate files; symbolic links reference a fixed skill version."))} ${e(t("Apply again after updates or switching modes."))}</p></div><div class="theme-picker settings-mode-options settings-icon-switch" role="radiogroup" aria-label="${e(t("Deployment method"))}">${modeOptions}</div></div>
          <div class="settings-row settings-directory-row"><div class="settings-row-copy"><div class="settings-directory-heading"><strong id="settings-project-title">${e(t("Default project directories"))}</strong><button type="button" class="icon-button settings-directory-add" data-action="new-project-directory" title="${e(t("添加项目目录"))}" aria-label="${e(t("添加项目目录"))}">${icon("plus")}</button></div><p>${e(t("勾选新项目默认目录。修改自定义目录不会改变现有项目。"))}</p></div><div class="settings-directory-manager"><div class="settings-directory-controls" role="group" aria-labelledby="settings-project-title">${projectOptions}</div></div></div>
          <div class="settings-row settings-directory-row"><div class="settings-row-copy"><div class="settings-directory-heading"><strong id="settings-global-title">${e(t("Default global directories"))}</strong><button type="button" class="icon-button settings-directory-add" data-action="new-global-directory" title="${e(t("添加全局目录"))}" aria-label="${e(t("添加全局目录"))}">${icon("plus")}</button></div><p>${e(t("The Global page starts with these directories selected."))}</p></div><div class="settings-directory-manager"><div class="settings-directory-controls" role="group" aria-labelledby="settings-global-title">${globalOptions}</div></div></div>
        </div>
      </form>
      <section class="settings-section" aria-labelledby="settings-data-title">
        <h2 id="settings-data-title" class="settings-section-title">${e(t("Data and backup"))}</h2>
        <div class="settings-group">
          <div class="settings-row"><div class="settings-row-copy"><strong>${e(t("Backup and migration"))}</strong><p>${e(t("Manage local snapshots and workspace ZIP backups."))}</p></div><button type="button" class="button" data-action="open-backups">${icon("archive")}${e(t("Open backups"))}</button></div>
          <div class="settings-row settings-storage"><strong>${e(t("Local data directory"))}</strong><code title="${e(dataDir)}">${e(dataDir)}</code></div>
        </div>
      </section>
    </div>
  </div>`;
}
