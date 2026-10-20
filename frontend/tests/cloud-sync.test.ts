import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import ts from "typescript";
import type { CloudSyncConfig, CloudSyncStatus, SyncConflict } from "../src/api.ts";

function module() {
  const exports: Record<string, (...args: any[]) => any> = {};
  const code = ts.transpileModule(readFileSync(new URL("../src/cloud-sync.ts", import.meta.url), "utf8"), { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText;
  class FormValues {
    private values: Record<string, string>;
    constructor(values: Record<string, string>) { this.values = values; }
    get(name: string) { return this.values[name] ?? null; }
    has(name: string) { return Object.hasOwn(this.values, name); }
  }
  new Function("exports", "require", "FormData", code)(exports, (name: string) => {
    if (name === "./i18n") return { getLocale: () => "en", tMessage: (value: string) => value, t: (key: string, values: Record<string, string | number> = {}) => key.replace(/\{(\w+)\}/g, (slot, name) => Object.hasOwn(values, name) ? String(values[name]) : slot) };
    if (name === "./icons") return { icon: () => "<svg></svg>" };
    throw new Error(`Unexpected import: ${name}`);
  }, FormValues);
  return exports;
}

test("sync configuration never renders saved secret values, even if a response includes them", () => {
  const ui = module();
  const config: CloudSyncConfig = { ...ui.defaultCloudSyncConfig(), endpoint: "https://example.com", accessKeyId: 'id"<&', secretAccessKey: "DO-NOT-RENDER-SECRET", sessionToken: "DO-NOT-RENDER-TOKEN", passphrase: "DO-NOT-RENDER-PASSPHRASE", hasSecretAccessKey: true, hasSessionToken: true, hasPassphrase: true };
  const html = ui.cloudSyncConfigFields(config);
  for (const secret of [config.secretAccessKey, config.sessionToken, config.passphrase]) assert.equal(html.includes(secret), false);
  assert.match(html, /Saved; leave blank to keep/);
  assert.match(html, /id&quot;&lt;&amp;/);
  assert.match(html, /minlength="12"/);
  assert.match(html, /name="clearSessionToken" type="checkbox">/);
  assert.match(html, /do not enter a Cloudflare API token/);
  assert.doesNotMatch(ui.cloudSyncConfigFields(ui.defaultCloudSyncConfig()), /name="clearSessionToken"/);
});

test("configuration preserves passphrase bytes and submits empty saved secrets without copying them", () => {
  const ui = module();
  const saved = { ...ui.defaultCloudSyncConfig(), secretAccessKey: "saved-secret", sessionToken: "saved-token", passphrase: "saved-passphrase", hasPassphrase: true };
  const input = { endpoint: " https://example.com ", region: " auto ", bucket: "bucket", prefix: "personal", accessKeyId: "id", secretAccessKey: "", sessionToken: "", passphrase: "  twelve character secret  ", pathStyle: "on", intervalMinutes: "7" };
  const config = ui.readCloudSyncConfig(input, saved);
  assert.equal(config.passphrase, input.passphrase);
  assert.equal(config.secretAccessKey, "");
  assert.equal(config.sessionToken, "");
  assert.equal(config.clearSessionToken, false);
  assert.equal(config.endpoint, "https://example.com");
  assert.equal(config.autoSync, false);
  assert.equal(config.pathStyle, true);
  assert.equal(config.intervalMinutes, 7);
});

test("clearing a saved session token is explicit and takes priority over a replacement", () => {
  const ui = module();
  const saved = { ...ui.defaultCloudSyncConfig(), hasSessionToken: true };
  const input = { sessionToken: "replacement-token", clearSessionToken: "on" };
  const config = ui.readCloudSyncConfig(input, saved);
  assert.equal(config.clearSessionToken, true);
  assert.equal(config.sessionToken, "");
  const replacement = ui.readCloudSyncConfig({ sessionToken: "replacement-token" }, config);
  assert.equal(replacement.clearSessionToken, false);
  assert.equal(replacement.sessionToken, "replacement-token");
});

test("conflict review labels deletions and requires an explicit choice for every record", () => {
  const ui = module();
  const conflicts: SyncConflict[] = [
    { key: "skill/a", kind: "skill", name: "<script>bad</script>", local: null, remote: { name: "remote" } },
    { key: "preset/b", kind: "preset", name: "Preset", local: { skillIds: ["a"] }, remote: null },
  ];
  const html = ui.cloudConflictFields(conflicts);
  assert.match(html, /Keep local deletion/);
  assert.match(html, /Keep cloud deletion/);
  assert.match(html, /type="button" class="button small" data-action="choose-all-cloud-local"/);
  assert.match(html, /type="button" class="button small" data-action="choose-all-cloud-remote"/);
  assert.match(html, /&lt;script&gt;bad&lt;\/script&gt;/);
  assert.doesNotMatch(html, /<script>/);
  assert.equal(ui.readCloudResolutions({ "resolution:skill/a": "local" }, conflicts), null);
  assert.equal(ui.readCloudResolutions({ "resolution:skill/a": "local", "resolution:preset/b": "unexpected" }, conflicts), null);
  assert.deepEqual(ui.readCloudResolutions({ "resolution:skill/a": "local", "resolution:preset/b": "remote" }, conflicts), { "skill/a": "local", "preset/b": "remote" });
});

test("the global banner remains visible for conflicts outside settings and hides after resolution", () => {
  const ui = module();
  const status: CloudSyncStatus = { configured: true, syncing: false, config: ui.defaultCloudSyncConfig(), lastSyncAt: "", lastError: "", conflicts: [
    { key: "skill/a", kind: "skill", name: "Skill", local: null, remote: {} },
    { key: "preset/b", kind: "preset", name: "Preset", local: {}, remote: null },
  ] };
  assert.equal(ui.cloudConflictBanner(), "");
  assert.equal(ui.cloudConflictBanner({ ...status, conflicts: [] }), "");
  const html = ui.cloudConflictBanner(status);
  assert.match(html, /role="status" aria-live="polite"/);
  assert.match(html, /2 sync conflicts/);
  assert.match(html, /data-action="cloud-conflicts"/);
  assert.doesNotMatch(html, /disabled/);
  assert.match(ui.cloudConflictBanner({ ...status, syncing: true }), /data-action="cloud-conflicts" disabled/);
});

test("bulk conflict choices fill every resolution without submitting and remain individually editable", () => {
  const ui = module();
  let changes = 0;
  const changed = (event: Event) => { assert.equal(event.type, "change"); assert.equal(event.bubbles, true); changes++; };
  const selects = [{ value: "", dispatchEvent: changed }, { value: "remote", dispatchEvent: changed }];
  let submitted = false;
  const form = {
    querySelectorAll(selector: string) {
      assert.equal(selector, 'select[name^="resolution:"]');
      return selects;
    },
    submit() { submitted = true; },
    requestSubmit() { submitted = true; },
  };
  ui.setCloudResolutionChoice(form, "local");
  assert.deepEqual(selects.map((select) => select.value), ["local", "local"]);
  selects[1].value = "remote";
  assert.deepEqual(selects.map((select) => select.value), ["local", "remote"]);
  ui.setCloudResolutionChoice(form, "remote");
  assert.deepEqual(selects.map((select) => select.value), ["remote", "remote"]);
  assert.equal(submitted, false);
  assert.equal(changes, 4);
  assert.doesNotMatch(ui.cloudConflictFields([]), /choose-all-cloud/);
});

test("cloud restore entries show the concrete version and counts for review", () => {
  const ui = module();
  const html = ui.cloudBackupsList([{ id: 'revision"<1>', createdAt: "2026-10-12T01:00:00Z", skillCount: 7, presetCount: 2 }]);
  assert.match(html, /7 skills · 2 presets/);
  assert.match(html, /data-id="revision&quot;&lt;1&gt;"/);
  assert.match(ui.cloudBackupsList([]), /No cloud backups yet/);
});
