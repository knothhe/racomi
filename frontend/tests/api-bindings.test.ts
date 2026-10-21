import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import vm from "node:vm";
import ts from "typescript";

test("frontend desktop calls resolve to exported Service methods", () => {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
  const directory = path.join(root, "internal/desktop");
  const serviceMethods = new Set(fs.readdirSync(directory)
    .filter((name) => name.endsWith(".go") && !name.endsWith("_test.go"))
    .flatMap((name) => [...fs.readFileSync(path.join(directory, name), "utf8").matchAll(/func\s+\(\w+\s+\*Service\)\s+([A-Z]\w*)\(/g)].map((match) => match[1])));
  const source = fs.readFileSync(path.join(root, "frontend/src/api.ts"), "utf8");
  const tree = ts.createSourceFile("api.ts", source, ts.ScriptTarget.Latest, true);
  const calls: string[] = [];
  function visit(node: ts.Node) {
    if (ts.isCallExpression(node) && node.expression.getText(tree) === "invoke") {
      const method = node.arguments[0];
      assert.ok(method && ts.isStringLiteral(method), "desktop methods must be explicit literals");
      calls.push(method.text);
    }
    ts.forEachChild(node, visit);
  }
  visit(tree);
  assert.ok(calls.length > 0, "desktop calls were found");
  for (const method of calls) assert.ok(serviceMethods.has(method), `${method}: missing desktop Service method`);
});

test("Hub bindings preserve the desktop request cancellation method", async () => {
  const source = ts.createSourceFile("api.ts", fs.readFileSync(new URL("../src/api.ts", import.meta.url), "utf8"), ts.ScriptTarget.Latest, true);
  const production = source.statements.filter((statement) => ts.isFunctionDeclaration(statement) && statement.name?.text === "invoke"
    || ts.isVariableStatement(statement) && statement.declarationList.declarations.some((declaration) => declaration.name.getText(source) === "api"))
    .map((statement) => statement.getText(source).replace(/^export /, "")).join("\n");
  const calls: unknown[][] = [];
  let cancellations = 0;
  const request = Object.assign(Promise.resolve([]), { cancel: () => { cancellations++; } });
  const api = vm.runInNewContext(ts.transpileModule(`${production}\napi;`, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.None } }).outputText, {
    Call: { ByName: (...args: unknown[]) => { calls.push(args); return request; } },
  }) as { search: (query: string) => typeof request; listSkills: (page: number) => typeof request };
  for (const result of [api.search("design"), api.listSkills(1)]) {
    assert.equal(result, request);
    result.cancel();
    await result;
  }
  assert.equal(cancellations, 2);
  assert.deepEqual(calls, [["racomi/internal/desktop.Service.Search", "design"], ["racomi/internal/desktop.Service.ListSkills", 1]]);
});
