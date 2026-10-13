import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";
import ts from "typescript";
import { hostedSourceDomain } from "../src/source-names.ts";

const main = ts.createSourceFile("main.ts", readFileSync(new URL("../src/main.ts", import.meta.url), "utf8"), ts.ScriptTarget.Latest, true);
const production = main.statements.filter((statement) => {
  if (ts.isFunctionDeclaration(statement)) return !!statement.name && ["presetEditor", "sourceEditor"].includes(statement.name.text);
  if (!ts.isExpressionStatement(statement) || !ts.isCallExpression(statement.expression)) return false;
  return statement.expression.expression.getText(main) === "document.addEventListener" && statement.expression.arguments[0]?.getText(main) === '"submit"';
}).map((statement) => statement.getText(main)).join("\n");

function harness() {
  const records = {
    presets: [{ id: "preset-a", name: "Original", description: "Original description", skillIds: ["skill-a", "skill-b"] }],
    sources: [{ id: "source-a", name: "Original source", repository: "owner/repo", ref: "main", subpath: ".", mode: "selected", revision: "original", refreshedAt: "", lastError: "", catalog: [] }],
    skills: [{ id: "skill-a" }, { id: "skill-b" }],
  };
  const writes: { kind: string; value: any; expected: any }[] = [];
  let submit!: (event: any) => Promise<void>;
  const setup = `
    const sourceEditorBaselines = new WeakMap(), presetEditorBaselines = new WeakMap();
    const presets = () => records.presets, sources = () => records.sources, skills = () => records.skills;
    const arr = (value) => value || [], e = (value) => String(value || ""), t = (value) => value;
    const presetSkillDetailsOpen = new Set();
    let presetFilters = {}, presetSkillSelection = new Set(), currentForm;
    const defaultSkillFilters = () => ({}), entityIconPicker = () => "", sourceFilter = () => "", iconButton = () => "", icon = () => "";
    function modal(_title, _description, body, _footer, id) {
      currentForm = { id, body, values: {}, getAttribute: () => id, classList: { contains: () => false }, querySelector: () => null };
    }
    const modalRoot = { querySelector: () => currentForm };
    const renderPresetSkills = () => {};
    const searchShortcut = () => "";
    const field = (form, name) => form.values[name] || "";
    const task = (_label, action) => action();
    ${production}
    ({ presetEditor, sourceEditor, form: () => currentForm });
  `;
  const runtime = vm.runInNewContext(ts.transpileModule(setup, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, {
    records, structuredClone, hostedSourceDomain,
    document: { addEventListener: (_type: string, listener: typeof submit) => { submit = listener; } },
    api: {
      savePreset: async (value: any, expected: any) => { writes.push({ kind: "preset", value, expected }); },
      saveSource: async (value: any, expected: any) => { writes.push({ kind: "source", value, expected }); },
    },
  }) as { presetEditor: (id?: string) => void; sourceEditor: (id?: string) => void; form: () => any };
  return { runtime, records, writes, submit: (form = runtime.form()) => submit({ target: form, preventDefault() {} }) };
}

test("preset submission uses its opening baseline and retains selections deleted by background sync", async () => {
  const h = harness();
  const original = structuredClone(h.records.presets[0]);
  h.runtime.presetEditor("preset-a");
  const form = h.runtime.form();
  form.values = { entityId: "preset-a", name: "My edited preset", description: "Local edit" };
  h.records.presets[0].description = "Remote edit";
  h.records.presets[0].skillIds = ["skill-a"];
  h.records.skills = [{ id: "skill-a" }];
  await h.submit();
  assert.deepEqual(h.writes[0].expected, original);
  assert.deepEqual([...h.writes[0].value.skillIds], ["skill-a", "skill-b"]);
  assert.equal(h.writes[0].value.description, "Local edit");
});

test("source submission keeps its baseline when the source was deleted remotely", async () => {
  const h = harness();
  const original = structuredClone(h.records.sources[0]);
  h.runtime.sourceEditor("source-a");
  h.runtime.form().values = { entityId: "source-a", name: "My source", repository: "owner/repo", ref: "main", subpath: ".", mode: "all" };
  h.records.sources = [];
  await h.submit();
  assert.deepEqual(h.writes[0].expected, original);
  assert.equal(h.writes[0].value.id, "source-a");
});

test("hosted source editing keeps source fields and omits the Git ref control", () => {
  const h = harness();
  h.records.sources[0].repository = "https://open.feishu.cn";
  h.records.sources[0].ref = "";
  h.runtime.sourceEditor("source-a");
  const body = h.runtime.form().body;
  assert.match(body, /来源地址/);
  assert.match(body, /name="repository" value="https:\/\/open.feishu.cn" required readonly/);
  assert.match(body, /type="hidden" name="ref" value=""/);
  assert.doesNotMatch(body, /分支、标签或 Commit|默认分支|整个仓库/);
});

test("new records send a null baseline and separate modal instances cannot replace an old form baseline", async () => {
  const h = harness();
  const original = structuredClone(h.records.presets[0]);
  h.runtime.presetEditor("preset-a");
  const oldForm = h.runtime.form();
  oldForm.values = { entityId: "preset-a", name: "Original edited" };
  h.runtime.presetEditor();
  h.runtime.form().values = { name: "New preset" };
  await h.submit();
  assert.equal(h.writes[0].expected, null);
  await h.submit(oldForm);
  assert.deepEqual(h.writes[1].expected, original);
});
