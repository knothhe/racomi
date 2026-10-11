import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
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
