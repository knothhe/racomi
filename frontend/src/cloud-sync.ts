import type { CloudBackup, CloudSyncConfig, CloudSyncStatus, SyncConflict } from "./api";
import { getLocale, t, tMessage } from "./i18n";
import { icon } from "./icons";

const e = (value: unknown) => String(value ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);
const date = (value: string) => value && !Number.isNaN(Date.parse(value)) ? new Date(value).toLocaleString(getLocale()) : t("Never synced");

export function defaultCloudSyncConfig(): CloudSyncConfig {
  return { endpoint: "", region: "auto", bucket: "", prefix: "racomi", accessKeyId: "", secretAccessKey: "", sessionToken: "", passphrase: "", pathStyle: true, autoSync: false, intervalMinutes: 5, hasSecretAccessKey: false, hasSessionToken: false, hasPassphrase: false };
}

export function cloudConflictBanner(status?: CloudSyncStatus): string {
  const count = status?.conflicts?.length || 0;
  if (!count) return "";
  return `<aside class="cloud-conflict-banner" role="status" aria-live="polite">
    ${icon("alert")}<div class="cloud-conflict-banner-copy"><strong>${e(t("{0} sync conflicts", { 0: count }))}</strong><p>${e(t("Choose which versions to keep to continue syncing."))}</p></div>
    <button type="button" class="button small" data-action="cloud-conflicts" ${status?.syncing ? "disabled" : ""}>${e(t("Review conflicts"))}</button>
  </aside>`;
}

export function cloudSyncSection(status?: CloudSyncStatus): string {
  const conflicts = status?.conflicts?.length || 0;
  const ready = !!status?.configured;
  return `<section class="settings-section cloud-sync-section" aria-labelledby="settings-cloud-title">
    <div class="backup-section-heading"><h2 id="settings-cloud-title" class="settings-section-title">${e(t("S3 / R2 sync"))}</h2><button type="button" class="button quiet small" data-action="open-cloud-guide">${icon("book")}${e(t("Configuration guide"))}</button></div>
    <div class="settings-group">
      <div class="settings-row"><div class="settings-row-copy cloud-sync-status" aria-live="polite"><strong>${e(t(status?.syncing ? "Syncing…" : conflicts ? "Sync needs review" : ready ? "Ready to sync" : "Sync is not configured"))}</strong>${ready ? `<p>${e(t("Last sync: {0}", { 0: date(status?.lastSyncAt || "") }))} · ${e(t(status?.config.autoSync ? "Auto sync while racomi is open" : "Manual sync"))}</p>` : ""}${status?.lastError ? `<p class="cloud-sync-error" title="${e(tMessage(status.lastError))}">${e(tMessage(status.lastError))}</p>` : ""}</div><div class="cloud-sync-actions"><button type="button" class="button" data-action="configure-cloud-sync">${icon("settings")}${e(t(ready ? "Configure sync" : "Set up sync"))}</button>${ready ? `<button type="button" class="button" data-action="cloud-backups" ${status?.syncing ? "disabled" : ""}>${icon("archive")}${e(t("Cloud backups"))}</button><button type="button" class="button primary" data-action="sync-cloud" ${status?.syncing ? "disabled" : ""}>${icon("refresh")}${e(t("Sync now"))}</button>` : ""}</div></div>
      ${conflicts ? `<div class="settings-row"><div class="settings-row-copy"><strong>${e(t("{0} sync conflicts", { 0: conflicts }))}</strong></div><button type="button" class="button" data-action="cloud-conflicts">${e(t("Review conflicts"))}</button></div>` : ""}
    </div>
  </section>`;
}

export function cloudSyncConfigFields(value?: CloudSyncConfig): string {
  const config = { ...defaultCloudSyncConfig(), ...value };
  const saved = (present: boolean) => e(t(present ? "Saved; leave blank to keep" : "Not saved"));
  return `<div class="cloud-sync-form-grid">
    <label class="field cloud-sync-wide"><span>${e(t("S3 endpoint"))}</span><input name="endpoint" type="url" value="${e(config.endpoint)}" placeholder="https://&lt;ACCOUNT_ID&gt;.r2.cloudflarestorage.com" required spellcheck="false"><small>${e(t("Use an HTTPS storage API endpoint, without a bucket or object path. HTTP is allowed only for a local loopback service."))}</small></label>
    <label class="field"><span>${e(t("Bucket"))}</span><input name="bucket" value="${e(config.bucket)}" placeholder="racomi-sync" required spellcheck="false"></label>
    <label class="field"><span>${e(t("Region"))}</span><input name="region" value="${e(config.region)}" placeholder="auto" required spellcheck="false"><small>${e(t("R2: auto. AWS S3: the bucket region, such as us-east-1."))}</small></label>
    <label class="field cloud-sync-wide"><span>${e(t("Object prefix"))}</span><input name="prefix" value="${e(config.prefix)}" placeholder="racomi" spellcheck="false"><small>${e(t("Devices using the same prefix share a library. Use another prefix to keep libraries separate."))}</small></label>
    <label class="field cloud-sync-wide"><span>Access Key ID</span><input name="accessKeyId" value="${e(config.accessKeyId)}" required autocomplete="off" spellcheck="false"></label>
    <label class="field cloud-sync-wide"><span>Secret Access Key</span><input name="secretAccessKey" type="password" placeholder="${saved(config.hasSecretAccessKey)}" ${config.hasSecretAccessKey ? "" : "required"} autocomplete="new-password"><small>${e(t("Changing the endpoint or access key requires entering its secret again."))}</small></label>
    <label class="field cloud-sync-wide"><span>${e(t("Session token (optional)"))}</span><input name="sessionToken" type="password" placeholder="${saved(config.hasSessionToken)}" autocomplete="new-password"><small>${e(t("Only for temporary credentials. Leave blank for long-lived R2 keys; do not enter a Cloudflare API token."))}</small></label>
    ${config.hasSessionToken ? `<label class="cloud-sync-check cloud-sync-wide"><input name="clearSessionToken" type="checkbox"><span>${e(t("Clear saved session token"))}<small>${e(t("Testing leaves saved settings unchanged. Save to remove the token; any replacement entered above is ignored."))}</small></span></label>` : ""}
    <label class="field cloud-sync-wide"><span>${e(t("Encryption passphrase"))}</span><input name="passphrase" type="password" minlength="12" placeholder="${saved(config.hasPassphrase)}" ${config.hasPassphrase ? "" : "required"} autocomplete="new-password"><small>${e(t("Use at least 12 characters and store a copy safely. Every device needs the same passphrase; losing it makes cloud data unreadable."))}</small></label>
    <label class="cloud-sync-check cloud-sync-wide"><input name="pathStyle" type="checkbox" ${config.pathStyle ? "checked" : ""}><span>${e(t("Use path-style addressing"))}<small>${e(t("Recommended for R2 and most compatible storage. Disable for AWS virtual-hosted addressing."))}</small></span></label>
    <label class="cloud-sync-check"><input name="autoSync" type="checkbox" ${config.autoSync ? "checked" : ""}><span>${e(t("Enable automatic two-way sync"))}<small>${e(t("Runs only while racomi is open."))}</small></span></label>
    <label class="field"><span>${e(t("Sync interval (minutes)"))}</span><input name="intervalMinutes" type="number" value="${e(config.intervalMinutes || 5)}" min="1" max="1440" step="1" required></label>
    <p class="info-note cloud-sync-wide">${e(t("Credentials and the passphrase are stored only in this device’s private configuration file. Saved secrets are never displayed here."))}</p>
  </div>`;
}

export function readCloudSyncConfig(form: HTMLFormElement, saved?: CloudSyncConfig): CloudSyncConfig {
  const values = new FormData(form);
  const value = (name: string) => String(values.get(name) || "").trim();
  const clearSessionToken = values.has("clearSessionToken");
  return { ...defaultCloudSyncConfig(), ...saved, endpoint: value("endpoint"), region: value("region"), bucket: value("bucket"), prefix: value("prefix"), accessKeyId: value("accessKeyId"), secretAccessKey: String(values.get("secretAccessKey") || ""), sessionToken: clearSessionToken ? "" : String(values.get("sessionToken") || ""), clearSessionToken, passphrase: String(values.get("passphrase") || ""), pathStyle: values.has("pathStyle"), autoSync: values.has("autoSync"), intervalMinutes: Number(value("intervalMinutes")) };
}

export function cloudConflictFields(conflicts: SyncConflict[]): string {
  const record = (value: unknown) => value == null ? `<p class="cloud-sync-deleted">${e(t("Deleted"))}</p>` : `<pre>${e(JSON.stringify(value, null, 2))}</pre>`;
  return `${conflicts.length ? `<div class="cloud-conflict-bulk"><div class="cloud-conflict-bulk-actions"><button type="button" class="button small" data-action="choose-all-cloud-local">${e(t("Keep all local versions"))}</button><button type="button" class="button small" data-action="choose-all-cloud-remote">${e(t("Keep all cloud versions"))}</button></div><p>${e(t("You can adjust individual choices before applying. Choosing a deleted version deletes that item."))}</p></div>` : ""}<div class="cloud-conflict-list">${conflicts.map((conflict, index) => `<section class="cloud-conflict"><h3>${e(conflict.name || conflict.key)}</h3><div class="cloud-conflict-versions"><details><summary>${e(t("Local version"))}</summary>${record(conflict.local)}</details><details><summary>${e(t("Cloud version"))}</summary>${record(conflict.remote)}</details></div><label class="field"><span>${e(t("Version to keep"))}</span><select name="resolution:${e(conflict.key)}" required aria-label="${e(t("Version to keep for {0}", { 0: conflict.name || String(index + 1) }))}"><option value="">${e(t("Choose a version"))}</option><option value="local">${e(t(conflict.local == null ? "Keep local deletion" : "Keep local version"))}</option><option value="remote">${e(t(conflict.remote == null ? "Keep cloud deletion" : "Keep cloud version"))}</option></select></label></section>`).join("")}</div>`;
}

export function setCloudResolutionChoice(form: HTMLFormElement, side: "local" | "remote"): void {
  form.querySelectorAll<HTMLSelectElement>('select[name^="resolution:"]').forEach((select) => {
    select.value = side;
    select.dispatchEvent(new Event("change", { bubbles: true }));
  });
}

export function readCloudResolutions(form: HTMLFormElement, conflicts: SyncConflict[]): Record<string, "local" | "remote"> | null {
  const values = new FormData(form);
  const resolutions = conflicts.map((conflict) => [conflict.key, values.get(`resolution:${conflict.key}`)] as const);
  if (!resolutions.length || resolutions.some(([, value]) => value !== "local" && value !== "remote")) return null;
  return Object.fromEntries(resolutions) as Record<string, "local" | "remote">;
}

export function cloudBackupsList(backups: CloudBackup[]): string {
  return backups.length ? `<div class="cloud-backup-list">${backups.map((backup) => `<article class="cloud-backup-row"><div><strong>${e(date(backup.createdAt))}</strong><p>${e(t("{0} skills · {1} presets", { 0: backup.skillCount, 1: backup.presetCount }))}</p><code>${e(backup.id)}</code></div><button type="button" class="button" data-action="restore-cloud-backup" data-id="${e(backup.id)}">${e(t("Restore"))}</button></article>`).join("")}</div>` : `<p class="muted">${e(t("No cloud backups yet. Complete a sync to create the first version."))}</p>`;
}
