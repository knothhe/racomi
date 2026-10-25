import type { CloudBackup, CloudSyncConfig, CloudSyncStatus, CloudSyncProvider, R2OAuthStatus, R2Account, R2Bucket, SyncConflict } from "./api";
import { getLocale, t, tMessage } from "./i18n";
import { icon } from "./icons";

const e = (value: unknown) => String(value ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);
const date = (value: string) => value && !Number.isNaN(Date.parse(value)) ? new Date(value).toLocaleString(getLocale()) : t("Never synced");

export function defaultCloudSyncConfig(): CloudSyncConfig {
  return { provider: "s3", endpoint: "", region: "auto", bucket: "", prefix: "racomi", accessKeyId: "", secretAccessKey: "", sessionToken: "", passphrase: "", pathStyle: true, autoSync: false, intervalMinutes: 5, hasSecretAccessKey: false, hasSessionToken: false, hasPassphrase: false };
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
      <div class="settings-row"><div class="settings-row-copy cloud-sync-status" aria-live="polite"><strong>${e(t(status?.syncing ? "Syncing…" : conflicts ? "Sync needs review" : ready ? "Ready to sync" : "Sync is not configured"))}</strong>${ready ? `<p>${e(t(status?.config.provider === "r2-oauth" ? "Cloudflare R2" : "Manual S3"))} · ${e(t("Last sync: {0}", { 0: date(status?.lastSyncAt || "") }))} · ${e(t(status?.config.autoSync ? "Auto sync while racomi is open" : "Manual sync"))}</p>` : ""}${status?.lastError ? `<p class="cloud-sync-error" title="${e(tMessage(status.lastError))}">${e(tMessage(status.lastError))}</p>` : ""}</div><div class="cloud-sync-actions"><button type="button" class="button" data-action="configure-cloud-sync">${icon("settings")}${e(t(ready ? "Configure sync" : "Set up sync"))}</button>${ready ? `<button type="button" class="button" data-action="cloud-backups" ${status?.syncing ? "disabled" : ""}>${icon("archive")}${e(t("Cloud backups"))}</button><button type="button" class="button primary" data-action="sync-cloud" ${status?.syncing ? "disabled" : ""}>${icon("refresh")}${e(t("Sync now"))}</button>` : ""}</div></div>
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
  const provider = saved?.provider === "r2-oauth" ? "r2-oauth" : "s3";
  const destination = provider === "r2-oauth" ? {
    endpoint: saved?.endpoint || "", region: "auto", bucket: saved?.bucket || "", accessKeyId: saved?.accessKeyId || "",
    secretAccessKey: "", sessionToken: "", clearSessionToken: false, pathStyle: true,
  } : {
    endpoint: value("endpoint"), region: value("region"), bucket: value("bucket"), accessKeyId: value("accessKeyId"),
    secretAccessKey: String(values.get("secretAccessKey") || ""), sessionToken: clearSessionToken ? "" : String(values.get("sessionToken") || ""), clearSessionToken, pathStyle: values.has("pathStyle"),
  };
  return { ...defaultCloudSyncConfig(), ...saved, ...destination, provider, prefix: value("prefix"), passphrase: String(values.get("passphrase") || ""), autoSync: values.has("autoSync"), intervalMinutes: Number(value("intervalMinutes")) };
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

export function cloudSyncDrafts(status?: CloudSyncStatus): Record<CloudSyncProvider, CloudSyncConfig> {
  const active = status?.config;
  const clean = (value: CloudSyncConfig | undefined, provider: CloudSyncProvider): CloudSyncConfig => ({ ...defaultCloudSyncConfig(), ...value, provider, secretAccessKey: "", sessionToken: "", passphrase: "" });
  return {
    s3: clean(status?.manualConfig || (active?.provider !== "r2-oauth" ? active : undefined), "s3"),
    "r2-oauth": clean(status?.r2Config || (active?.provider === "r2-oauth" ? active : undefined), "r2-oauth"),
  };
}

export interface R2EditorView {
  oauth?: R2OAuthStatus; accounts: R2Account[]; buckets: R2Bucket[];
  accountId: string; jurisdiction: string; bucket: string; busy: boolean; error: string;
}

export function cloudSyncMethodFields(provider: CloudSyncProvider): string {
  return `<div class="cloud-sync-methods" role="group" aria-label="${e(t("Connection method"))}"><button type="button" class="button ${provider === "r2-oauth" ? "selected" : ""}" data-action="cloud-provider" data-provider="r2-oauth" aria-pressed="${provider === "r2-oauth"}">${e(t("Cloudflare R2"))}</button><button type="button" class="button ${provider === "s3" ? "selected" : ""}" data-action="cloud-provider" data-provider="s3" aria-pressed="${provider === "s3"}">${e(t("Manual S3"))}</button></div><p class="cloud-sync-method-note">${e(t("Each method keeps its own settings. Changes take effect only after saving."))}</p>`;
}

export function r2SyncConfigFields(config: CloudSyncConfig, view: R2EditorView): string {
  const oauth = view.oauth;
  const authorizing = !!oauth?.authorizing;
  const unconfigured = oauth?.lastError === "Cloudflare sign-in is not configured in this build. You can use manual S3 configuration.";
  const error = view.error || (!oauth?.available && unconfigured ? "" : oauth?.lastError || "");
  const disabled = view.busy ? "disabled" : "";
  const saved = (present: boolean) => e(t(present ? "Saved; leave blank to keep" : "Not saved"));
  const message = !oauth ? t("Checking Cloudflare connection…") : !oauth.available ? t(unconfigured || !oauth.lastError ? "Cloudflare connection is not enabled in this build. You can use Manual S3." : "Cloudflare connection is unavailable. You can use Manual S3.") : authorizing ? t("Finish authorization in your browser. This window will update automatically.") : oauth.connected ? t("Choose a private bucket. racomi creates credentials that can access only that bucket.") : t("Authorize in your browser to choose a private bucket. racomi manages the access credentials.");
  const title = authorizing ? t("Waiting for browser authorization") : oauth?.connected ? t("Cloudflare connected") : t("Connect Cloudflare");
  return `<section class="r2-connection" aria-live="polite"><div class="r2-connection-heading">${icon(authorizing ? "refresh" : "cloud")}<strong>${e(title)}</strong></div><p>${e(message)}</p>${oauth?.available ? `<div class="r2-connection-actions">${authorizing ? `<button type="button" class="button" data-action="cancel-r2-oauth" ${disabled}>${e(t("Cancel authorization"))}</button>` : `<button type="button" class="button ${oauth.connected ? "" : "primary"}" data-action="start-r2-oauth" ${disabled}>${e(t(oauth.connected ? "Authorize again" : "Connect Cloudflare"))}</button>`}</div>` : ""}</section>
    ${error ? `<p class="cloud-sync-error r2-editor-error" role="alert">${e(tMessage(error))}</p>` : ""}
    ${oauth?.connected ? `<div class="cloud-sync-form-grid r2-bucket-fields"><label class="field cloud-sync-wide"><span>${e(t("Cloudflare account"))}</span><select name="r2Account" ${disabled}><option value="">${e(t("Choose an account"))}</option>${view.accounts.map(account => `<option value="${e(account.id)}" ${account.id === view.accountId ? "selected" : ""}>${e(account.name || account.id)}</option>`).join("")}</select></label><label class="field"><span>${e(t("Jurisdiction"))}</span><select name="r2Jurisdiction" ${disabled}>${[["default", t("Default jurisdiction")], ["eu", t("European Union")], ["us", t("United States")], ["fedramp", "FedRAMP"]].map(([id, label]) => `<option value="${id}" ${id === view.jurisdiction ? "selected" : ""}>${e(label)}</option>`).join("")}</select></label><label class="field"><span>${e(t("Bucket"))}</span><select name="r2Bucket" ${disabled || !view.accountId ? "disabled" : ""}><option value="">${e(t("Choose a bucket"))}</option>${view.buckets.map(bucket => `<option value="${e(bucket.name)}" ${bucket.name === view.bucket ? "selected" : ""}>${e(bucket.name)}</option>`).join("")}</select></label><div class="cloud-sync-wide"><button type="button" class="button" data-action="configure-r2-bucket" ${view.busy || !view.accountId || !view.bucket ? "disabled" : ""}>${e(t(view.busy ? "Preparing R2 credentials…" : "Use this bucket"))}</button></div></div>` : ""}
    ${config.hasSecretAccessKey && config.bucket ? `<div class="r2-prepared" role="status">${icon("check")}<div><strong>${e(t("R2 credentials ready"))}</strong><p>${e(config.bucket)}</p><code>${e(config.endpoint)}</code><small>${e(t("Test the connection and save to use this bucket for sync."))}</small></div></div>` : ""}
    <div class="cloud-sync-form-grid"><label class="field cloud-sync-wide"><span>${e(t("Object prefix"))}</span><input name="prefix" value="${e(config.prefix)}" placeholder="racomi" spellcheck="false"><small>${e(t("Devices using the same prefix share a library. Use another prefix to keep libraries separate."))}</small></label><label class="field cloud-sync-wide"><span>${e(t("Encryption passphrase"))}</span><input name="passphrase" type="password" minlength="12" placeholder="${saved(config.hasPassphrase)}" ${config.hasPassphrase ? "" : "required"} autocomplete="new-password"><small>${e(t("Use the same encryption passphrase on every device. Cloudflare authorization does not replace it."))}</small></label><label class="cloud-sync-check"><input name="autoSync" type="checkbox" ${config.autoSync ? "checked" : ""}><span>${e(t("Enable automatic two-way sync"))}<small>${e(t("Runs only while racomi is open."))}</small></span></label><label class="field"><span>${e(t("Sync interval (minutes)"))}</span><input name="intervalMinutes" type="number" value="${e(config.intervalMinutes || 5)}" min="1" max="1440" step="1" required></label></div>`;
}
