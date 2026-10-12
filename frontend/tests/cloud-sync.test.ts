import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import ts from "typescript";
import type { CloudSyncConfig, SyncConflict } from "../src/api.ts";

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
});

test("configuration preserves passphrase bytes and submits empty saved secrets without copying them", () => {
  const ui = module();
  const saved = { ...ui.defaultCloudSyncConfig(), secretAccessKey: "saved-secret", sessionToken: "saved-token", passphrase: "saved-passphrase", hasPassphrase: true };
  const input = { endpoint: " https://example.com ", region: " auto ", bucket: "bucket", prefix: "personal", accessKeyId: "id", secretAccessKey: "", sessionToken: "", passphrase: "  twelve character secret  ", pathStyle: "on", intervalMinutes: "7" };
  const config = ui.readCloudSyncConfig(input, saved);
  assert.equal(config.passphrase, input.passphrase);
  assert.equal(config.secretAccessKey, "");
  assert.equal(config.sessionToken, "");
  assert.equal(config.endpoint, "https://example.com");
  assert.equal(config.autoSync, false);
  assert.equal(config.pathStyle, true);
  assert.equal(config.intervalMinutes, 7);
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
  assert.match(html, /&lt;script&gt;bad&lt;\/script&gt;/);
  assert.doesNotMatch(html, /<script>/);
  assert.equal(ui.readCloudResolutions({ "resolution:skill/a": "local" }, conflicts), null);
  assert.equal(ui.readCloudResolutions({ "resolution:skill/a": "local", "resolution:preset/b": "unexpected" }, conflicts), null);
  assert.deepEqual(ui.readCloudResolutions({ "resolution:skill/a": "local", "resolution:preset/b": "remote" }, conflicts), { "skill/a": "local", "preset/b": "remote" });
});

test("cloud restore entries show the concrete version and counts for review", () => {
  const ui = module();
  const html = ui.cloudBackupsList([{ id: 'revision"<1>', createdAt: "2026-10-12T01:00:00Z", skillCount: 7, presetCount: 2 }]);
  assert.match(html, /7 skills · 2 presets/);
  assert.match(html, /data-id="revision&quot;&lt;1&gt;"/);
  assert.match(ui.cloudBackupsList([]), /No cloud backups yet/);
});
