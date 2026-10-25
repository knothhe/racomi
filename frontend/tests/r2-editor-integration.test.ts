import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import ts from "typescript";

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(done => { resolve = done; });
  return { promise, resolve };
}

function editor() {
  return {
    provider: "r2-oauth", drafts: { s3: { bucket: "manual", secretAccessKey: "manual-draft-secret" }, "r2-oauth": { prefix: "chosen-prefix", passphrase: "chosen-passphrase", autoSync: true, intervalMinutes: 17, r2ConnectionId: "old-candidate", hasPassphrase: false } },
    oauth: { available: true, connected: true, authorizing: false, lastError: "" }, accounts: [], buckets: [], accountsState: "idle", accountsEpoch: 0,
    accountId: "first", jurisdiction: "default", bucket: "", busy: false, error: "", request: 0, disconnectConfirm: false,
  };
}

function controller(initial: ReturnType<typeof editor>, api: Record<string, (...args: any[]) => any>) {
  const source = readFileSync(new URL("../src/main.ts", import.meta.url), "utf8");
  const tree = ts.createSourceFile("main.ts", source, ts.ScriptTarget.Latest, true);
  const names = ["loadR2Accounts", "loadR2Buckets", "refreshR2Editor", "r2EditorAction"];
  const functions = names.map(name => {
    const fn = tree.statements.find(node => ts.isFunctionDeclaration(node) && node.name?.text === name);
    assert.ok(fn, name);
    return fn.getText(tree);
  }).join("\n");
  const code = ts.transpileModule(`
    let cloudEditor = initial, data = null, renders = 0;
    const window = { setTimeout: () => 1, clearTimeout: () => {} };
    const captureCloudSyncDraft = () => {};
    const renderCloudSyncEditor = () => { renders++; };
    const discarded = [];
    const discardR2Candidate = id => { if (id) discarded.push(id); };
    ${functions}
    return { loadR2Buckets, loadR2Accounts, refreshR2Editor, r2EditorAction,
      setCurrent: value => { cloudEditor = value; }, renders: () => renders, discarded };
  `, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.None } }).outputText;
  return new Function("initial", "api", code)(initial, api);
}

test("late bucket response cannot replace the latest account and jurisdiction choices", async () => {
  const current = editor();
  const first = deferred<any[]>(), second = deferred<any[]>();
  const calls: string[][] = [];
  const ui = controller(current, { listR2Buckets: (account: string, jurisdiction: string) => { calls.push([account, jurisdiction]); return calls.length === 1 ? first.promise : second.promise; } });
  const oldRequest = ui.loadR2Buckets(current);
  current.accountId = "second"; current.jurisdiction = "eu";
  const newRequest = ui.loadR2Buckets(current);
  second.resolve([{ name: "new-bucket", jurisdiction: "eu" }]); await newRequest;
  first.resolve([{ name: "old-bucket", jurisdiction: "default" }]); await oldRequest;
  assert.deepEqual(current.buckets, [{ name: "new-bucket", jurisdiction: "eu" }]);
  assert.equal(current.accountId, "second");
  assert.equal(current.jurisdiction, "eu");
  assert.equal(current.busy, false);
});

test("closing an editor prevents a late bucket response from rendering into a new modal", async () => {
  const current = editor();
  const request = deferred<any[]>();
  const ui = controller(current, { listR2Buckets: () => request.promise });
  const loading = ui.loadR2Buckets(current);
  const count = ui.renders();
  ui.setCurrent(editor());
  request.resolve([{ name: "old-bucket" }]); await loading;
  assert.equal(ui.renders(), count);
});

test("credentials arriving after the editor closes are discarded without activating their connection", async () => {
  const current = editor(); current.bucket = "chosen";
  const request = deferred<any>();
  const ui = controller(current, { configureR2Bucket: () => request.promise });
  const configuring = ui.r2EditorAction("configure-r2-bucket");
  ui.setCurrent(null);
  request.resolve({ provider: "r2-oauth", r2ConnectionId: "late-candidate", bucket: "chosen", hasSecretAccessKey: true });
  await configuring;
  assert.deepEqual(ui.discarded, ["late-candidate"]);
  assert.equal(current.drafts["r2-oauth"].r2ConnectionId, "old-candidate");
});

test("preparing a new R2 destination preserves common drafts and never edits the manual profile", async () => {
  const current = editor(); current.bucket = "chosen";
  const manual = structuredClone(current.drafts.s3);
  const ui = controller(current, { configureR2Bucket: async () => ({ provider: "r2-oauth", r2ConnectionId: "new-candidate", bucket: "chosen", hasSecretAccessKey: true, prefix: "default", passphrase: "", autoSync: false, intervalMinutes: 5 }) });
  await ui.r2EditorAction("configure-r2-bucket");
  assert.equal(current.drafts["r2-oauth"].r2ConnectionId, "new-candidate");
  assert.equal(current.drafts["r2-oauth"].prefix, "chosen-prefix");
  assert.equal(current.drafts["r2-oauth"].passphrase, "chosen-passphrase");
  assert.equal(current.drafts["r2-oauth"].autoSync, true);
  assert.equal(current.drafts["r2-oauth"].intervalMinutes, 17);
  assert.deepEqual(current.drafts.s3, manual);
  assert.deepEqual(ui.discarded, ["old-candidate"]);
});

test("denied account access is attempted once across repeated status refreshes", async () => {
  const current = editor();
  let accountCalls = 0;
  const denied = "Cloudflare credential access was denied. Automatic retries have stopped. Connect Cloudflare again to retry.";
  const ui = controller(current, {
    getR2OAuthStatus: async () => ({ available: true, connected: true, authorizing: false, lastError: "" }),
    listR2Accounts: async () => { accountCalls++; throw new Error(denied); },
  });
  for (let i = 0; i < 4; i++) await ui.refreshR2Editor(current);
  assert.equal(accountCalls, 1);
  assert.equal(current.accountsState, "failed");
  assert.equal(current.error, denied);
});

test("an account request in progress and a completed empty result are not automatically repeated", async () => {
  const current = editor();
  const pending = deferred<any[]>();
  let accountCalls = 0;
  const ui = controller(current, {
    getR2OAuthStatus: async () => ({ available: true, connected: true, authorizing: false, lastError: "" }),
    listR2Accounts: () => { accountCalls++; return pending.promise; },
  });
  const loading = ui.loadR2Accounts(current);
  await ui.refreshR2Editor(current);
  assert.equal(accountCalls, 1);
  assert.equal(current.accountsState, "loading");
  pending.resolve([]);
  await loading;
  await ui.refreshR2Editor(current);
  assert.equal(accountCalls, 1);
  assert.equal(current.accountsState, "loaded");
});

test("authorization polling does not read credentials from an older connected authorization", async () => {
  const current = editor();
  current.oauth.authorizing = true;
  let credentialCalls = 0;
  const ui = controller(current, {
    getR2OAuthStatus: async () => ({ available: true, connected: true, authorizing: true, lastError: "" }),
    listR2Accounts: async () => { credentialCalls++; return []; },
    listR2Buckets: async () => { credentialCalls++; return []; },
  });
  for (let i = 0; i < 4; i++) await ui.refreshR2Editor(current);
  await ui.loadR2Accounts(current);
  await ui.loadR2Buckets(current);
  assert.equal(credentialCalls, 0);
  assert.equal(current.accountsState, "idle");
});

test("manual S3 configuration does not load Cloudflare accounts or buckets", async () => {
  const current = editor();
  current.provider = "s3";
  let credentialCalls = 0;
  const ui = controller(current, {
    getR2OAuthStatus: async () => ({ available: true, connected: true, authorizing: false, lastError: "" }),
    listR2Accounts: async () => { credentialCalls++; return []; },
    listR2Buckets: async () => { credentialCalls++; return []; },
  });
  for (let i = 0; i < 4; i++) await ui.refreshR2Editor(current);
  await ui.loadR2Accounts(current);
  await ui.loadR2Buckets(current);
  assert.equal(credentialCalls, 0);
});

test("a successful new authorization allows one fresh account attempt", async () => {
  const current = editor();
  let accountCalls = 0;
  const ui = controller(current, {
    getR2OAuthStatus: async () => ({ available: true, connected: true, authorizing: false, lastError: "" }),
    listR2Accounts: async () => { accountCalls++; throw new Error("access denied"); },
  });
  await ui.refreshR2Editor(current);
  assert.equal(accountCalls, 1);
  current.oauth.authorizing = true;
  await ui.refreshR2Editor(current);
  assert.equal(accountCalls, 2);
  for (let i = 0; i < 3; i++) await ui.refreshR2Editor(current);
  assert.equal(accountCalls, 2);
  assert.equal(current.accountsState, "failed");
});

test("an unsuccessful new authorization does not rearm a denied account request", async () => {
  const current = editor();
  current.accountsState = "failed";
  current.oauth.authorizing = true;
  let accountCalls = 0;
  const ui = controller(current, {
    getR2OAuthStatus: async () => ({ available: true, connected: true, authorizing: false, lastError: "Cloudflare sign-in was not authorized. You can retry." }),
    listR2Accounts: async () => { accountCalls++; return []; },
  });
  for (let i = 0; i < 4; i++) await ui.refreshR2Editor(current);
  assert.equal(accountCalls, 0);
  assert.equal(current.accountsState, "failed");
});
