import type { AppSettings, DesktopPreferences, GlobalApplication } from "./api";
import { getLocalePreference, supportedLocales, t } from "./i18n";
import { icon } from "./icons";
import { agentIcon } from "./agent-icons";
import { homeRelativePath } from "./paths";
import { getThemePreference } from "./theme";
import { getNavLayoutPreference } from "./layout";
import { getInterfaceFonts, textSizes } from "./interface";

export const projectDirectories = [
  { id: "agents", name: "Agent Skills", path: ".agents/skills", description: "" },
  { id: "claude", name: "Claude Code", path: ".claude/skills", description: "" },
  { id: "codex", name: "Codex", path: ".codex/skills", description: "" },
  { id: "cursor", name: "Cursor", path: ".cursor/skills", description: "" },
];

export const defaultSettings = (): AppSettings => ({ deploymentMode: "copy", defaultProjectDirectories: ["agents"], defaultGlobalDirectories: ["codex-user"], customProjectDirectories: [], snapshotRetentionDays: 7, snapshotMaxCount: 100 });
const e = (value: unknown) => String(value ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);
const directoryActions = (kind: "project" | "global", id: string, name: string) => {
  const editLabel = `${kind === "project" ? t("编辑项目目录") : t("编辑全局目录")}: ${name}`;
  const deleteLabel = `${kind === "project" ? t("删除项目目录") : t("删除全局目录")}: ${name}`;
  return `<div class="settings-directory-actions"><button type="button" class="icon-button" data-action="edit-${kind}-directory" data-id="${e(id)}" title="${e(editLabel)}" aria-label="${e(editLabel)}">${icon("edit")}</button><button type="button" class="icon-button danger-quiet" data-action="delete-${kind}-directory" data-id="${e(id)}" title="${e(deleteLabel)}" aria-label="${e(deleteLabel)}">${icon("trash")}</button></div>`;
};

export function settingsPage(settings: AppSettings, applications: GlobalApplication[], homeDir?: string, desktop?: DesktopPreferences) {
  const family = settings.interfaceFont || "system";
  const fontFamilies = getInterfaceFonts();
  const legacyFonts = [["system", t("系统字体")], ["sans", t("无衬线")], ["serif", t("衬线")], ["mono", t("等宽")]];
  const fontOptions = [...legacyFonts.filter(([id]) => id === "system" || id === family), ...fontFamilies.map((font) => [font.family, font.family])];
  if (!fontOptions.some(([id]) => id === family)) fontOptions.push([family, family]);
  const styles = fontFamilies.find((font) => font.family === family)?.styles || [];
  const style = settings.interfaceFontStyle || "system";
  const styleOptions = [["system", t("默认样式")], ...styles.map((item) => [item.postscriptName, item.name])];
  if (style !== "system" && !styleOptions.some(([id]) => id === style)) styleOptions.push([style, style]);
  const themeOptions = ([ ["light", "Light", "sun"], ["dark", "Dark", "moon"], ["system", "Follow system", "monitor"] ] as const).map(([id, label, symbol]) => `<button type="button" class="theme-option settings-icon-option" data-theme-preference="${id}" title="${e(t(label))}" aria-label="${e(t(label))}" aria-pressed="${getThemePreference() === id}">${icon(symbol)}</button>`).join("");
  const navLayoutOptions = ([ ["auto", "自动", "layout-auto"], ["side", "侧边栏", "layout-side"], ["top", "顶部导航", "layout-top"] ] as const).map(([id, label, symbol]) => `<button type="button" class="theme-option settings-icon-option" data-nav-layout-preference="${id}" title="${e(t(label))}" aria-label="${e(t(label))}" aria-pressed="${getNavLayoutPreference() === id}">${icon(symbol)}</button>`).join("");
  const modeOptions = ([ ["copy", "Copy", "copy"], ["symlink", "Symbolic link", "link"] ] as const).map(([id, label, symbol]) => `<label class="theme-option settings-icon-option" title="${e(t(label))}"><input class="sr-only" type="radio" name="deploymentMode" value="${id}" ${settings.deploymentMode === id ? "checked" : ""} aria-label="${e(t(label))}" aria-describedby="settings-deployment-help">${icon(symbol)}</label>`).join("");
  const projectOptions = [...projectDirectories.map((directory) => ({ ...directory, name: t(directory.name), description: t(directory.description), custom: false })), ...settings.customProjectDirectories.map((directory) => ({ ...directory, description: "", custom: true }))].map((directory) => `<div class="settings-directory-entry"><label class="settings-directory-option"><input type="checkbox" name="defaultProjectDirectories" value="${e(directory.id)}" ${settings.defaultProjectDirectories.includes(directory.id) ? "checked" : ""}>${agentIcon(directory.custom ? "custom" : directory.id)}<span><strong ${directory.description ? `title="${e(directory.description)}"` : ""}>${e(directory.name)}</strong><code title="${e(directory.path)}">${e(directory.path)}</code></span></label>${directory.custom ? directoryActions("project", directory.id, directory.name) : ""}</div>`).join("");
  const globalOptions = applications.map((directory) => `<div class="settings-directory-entry"><label class="settings-directory-option"><input type="checkbox" name="defaultGlobalDirectories" value="${e(directory.key)}" ${settings.defaultGlobalDirectories.includes(directory.key) ? "checked" : ""}>${agentIcon(directory.agentId)}<span><strong>${e(directory.agentId === "custom" ? directory.name : directory.agentName)}</strong><code title="${e(directory.path)}">${e(homeRelativePath(directory.path, homeDir))}</code></span></label>${directory.agentId === "custom" ? directoryActions("global", directory.key, directory.name) : ""}</div>`).join("");

  return `<div class="settings-page">
    <header class="page-heading"><div><h2 class="page-section-title">${e(t("Settings"))}</h2></div></header>
    <div class="settings-layout">
      <section class="settings-section" aria-labelledby="settings-appearance-title">
        <h2 id="settings-appearance-title" class="settings-section-title">${e(t("Appearance"))}</h2>
        <div class="settings-group">
          <div class="settings-row"><div class="settings-row-copy"><strong>${e(t("Theme"))}</strong></div><div class="theme-picker settings-theme-options settings-icon-switch" role="group" aria-label="${e(t("Theme"))}">${themeOptions}</div></div>
          <div class="settings-row"><div class="settings-row-copy"><strong>${e(t("导航布局"))}</strong></div><div class="theme-picker settings-nav-layout-options settings-icon-switch" role="group" aria-label="${e(t("导航布局"))}">${navLayoutOptions}</div></div>
          <label class="settings-row"><span class="settings-row-copy"><strong>${e(t("Language"))}</strong></span><select name="locale-preference" aria-label="${e(t("Language"))}"><option value="system" ${getLocalePreference() === "system" ? "selected" : ""}>${e(t("Follow system"))}</option>${supportedLocales.map((locale) => `<option value="${locale.id}" ${getLocalePreference() === locale.id ? "selected" : ""}>${e(locale.label)}</option>`).join("")}</select></label>
          <div class="settings-row"><strong>${e(t("文字大小"))}</strong><div class="theme-picker settings-text-sizes" role="group" aria-label="${e(t("文字大小"))}">${textSizes.map((size) => `<button type="button" class="theme-option" data-action="set-text-size" data-size="${size}" aria-pressed="${(settings.textSize || 100) === size}">${size}%</button>`).join("")}</div></div>
          <div class="settings-row settings-font-row"><strong>${e(t("界面字体"))}</strong><div class="settings-font-controls"><div class="settings-font-selects"><select name="interface-font" data-select-search="${e(t("搜索字体…"))}" aria-label="${e(t("界面字体"))}">${fontOptions.map(([id, label]) => `<option value="${e(id)}" ${family === id ? "selected" : ""}>${e(label)}</option>`).join("")}</select><select name="interface-font-style" aria-label="${e(t("字体样式"))}" ${legacyFonts.some(([id]) => id === family) || !styles.length ? "disabled" : ""}>${styleOptions.map(([id, label]) => `<option value="${e(id)}" ${style === id ? "selected" : ""}>${e(label)}</option>`).join("")}</select></div><span class="settings-font-preview" aria-label="${e(t("字体预览"))}">Aa 0123 · 中文</span></div></div>
        </div>
      </section>
      ${desktop?.supported ? `<section class="settings-section" aria-labelledby="settings-desktop-title"><h2 id="settings-desktop-title" class="settings-section-title">${e(t("应用"))}</h2><div class="settings-group"><label class="settings-row"><span class="settings-row-copy"><strong>${e(t("轻量模式"))}</strong><span class="settings-row-description">${e(t(desktop.platform !== "darwin" ? "关闭窗口一分钟后释放页面，从托盘图标或再次启动 racomi 重新打开。" : "关闭窗口一分钟后释放页面，点击 Dock 图标重新打开。"))}</span></span><input class="settings-toggle" role="switch" type="checkbox" name="lightweight" aria-label="${e(t("轻量模式"))}" ${desktop.lightweight ? "checked" : ""}></label><label class="settings-row"><strong>${e(t("开机自启"))}</strong><input class="settings-toggle" role="switch" type="checkbox" name="open-at-login" aria-label="${e(t("开机自启"))}" ${desktop.openAtLogin ? "checked" : ""}></label></div></section>` : ""}
      <form id="settings-editor" class="settings-section" aria-labelledby="settings-deployment-title">
        <h2 id="settings-deployment-title" class="settings-section-title">${e(t("Deployment"))}</h2>
        <div class="settings-group">
          <div class="settings-row"><div class="settings-row-copy"><strong>${e(t("Deployment method"))}</strong><p id="settings-deployment-help">${e(t("Apply again after updates or switching modes."))}</p></div><div class="theme-picker settings-mode-options settings-icon-switch" role="radiogroup" aria-label="${e(t("Deployment method"))}">${modeOptions}</div></div>
          <div class="settings-row settings-directory-row"><div class="settings-row-copy"><div class="settings-directory-heading"><strong id="settings-project-title">${e(t("Default project directories"))}</strong><button type="button" class="icon-button settings-directory-add" data-action="new-project-directory" title="${e(t("添加项目目录"))}" aria-label="${e(t("添加项目目录"))}">${icon("plus")}</button></div></div><div class="settings-directory-manager"><div class="settings-directory-controls" data-preserve-scroll="settings-project-directories" tabindex="0" role="group" aria-labelledby="settings-project-title">${projectOptions}</div></div></div>
          <div class="settings-row settings-directory-row"><div class="settings-row-copy"><div class="settings-directory-heading"><strong id="settings-global-title">${e(t("Default global directories"))}</strong><button type="button" class="icon-button settings-directory-add" data-action="new-global-directory" title="${e(t("添加全局目录"))}" aria-label="${e(t("添加全局目录"))}">${icon("plus")}</button></div></div><div class="settings-directory-manager"><div class="settings-directory-controls" data-preserve-scroll="settings-global-directories" tabindex="0" role="group" aria-labelledby="settings-global-title">${globalOptions}</div></div></div>
        </div>
      </form>

    </div>
  </div>`;
}
