// Explicitly localize authored UI literals. Never walks rendered DOM or data.
// Usage: node scripts/localize-ui.mjs src/main.ts [--write] [--keys /tmp/keys.json]
import fs from "node:fs";
import ts from "typescript";

const args = process.argv.slice(2);
const file = args.find((arg) => !arg.startsWith("--")) || "src/main.ts";
const source = fs.readFileSync(file, "utf8");
const tree = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
const keys = new Set();
const hasChinese = (value) => /[\u3400-\u9fff]/.test(value);
const quote = (value) => JSON.stringify(value);
const templateEscape = (value) => value.replace(/\\/g, "\\\\").replace(/`/g, "\\`").replace(/\$\{/g, "\\${");
const isHTML = (value) => /<[a-z!/][^>]*|[a-z-]+="|<\//i.test(value);

function translate(value, vars = "") {
  keys.add(value);
  return `t(${quote(value)}${vars ? `, { ${vars} }` : ""})`;
}

function htmlChunk(value) {
  // HTML tags and quotes delimit text and authored attribute values. This
  // intentionally never touches expressions inserted from user content.
  let result = "", cursor = 0;
  for (const match of value.matchAll(/[^<>"'\n]+/g)) {
    if (!hasChinese(match[0])) continue;
    result += templateEscape(value.slice(cursor, match.index));
    result += "${" + translate(match[0]) + "}";
    cursor = match.index + match[0].length;
  }
  return result + templateEscape(value.slice(cursor));
}

function nested(node) {
  let output = "", cursor = node.getStart(tree);
  const children = [];
  ts.forEachChild(node, (child) => { children.push(child); });
  for (const child of children) {
    output += source.slice(cursor, child.getStart(tree)) + visit(child);
    cursor = child.end;
  }
  return output + source.slice(cursor, node.end);
}

function visit(node) {
  // Re-running the transform must not translate dictionary keys or variables.
  if (ts.isCallExpression(node) && ["t", "tMessage"].includes(node.expression.getText(tree))) {
    if (ts.isStringLiteral(node.arguments[0])) keys.add(node.arguments[0].text);
    return source.slice(node.getStart(tree), node.end);
  }
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) {
    const value = node.text;
    if (!hasChinese(value)) return source.slice(node.getStart(tree), node.end);
    for (let ancestor = node.parent; ancestor; ancestor = ancestor.parent) {
      if (ts.isVariableDeclaration(ancestor) && ["nav", "snapshotReasons"].includes(ancestor.name.getText(tree))) {
        keys.add(value);
        return source.slice(node.getStart(tree), node.end);
      }
    }
    // Task labels are also stable task IDs. Translate only their display in
    // status(), so changing language while a task runs cannot change its ID.
    if (value.startsWith("正在") && value.endsWith("…")) {
      keys.add(value);
      return source.slice(node.getStart(tree), node.end);
    }
    return isHTML(value) ? "`" + htmlChunk(value) + "`" : translate(value);
  }
  if (ts.isTemplateExpression(node)) {
    const chunks = [node.head.text, ...node.templateSpans.map((span) => span.literal.text)];
    if (!chunks.some(hasChinese)) return nested(node);
    const expressions = node.templateSpans.map((span) => visit(span.expression));
    if (!chunks.some(isHTML)) {
      const key = chunks.map((chunk, index) => chunk + (index < expressions.length ? `{${index}}` : "")).join("");
      return translate(key, expressions.map((expression, index) => `${quote(index)}: ${expression}`).join(", "));
    }
    return "`" + chunks.map((chunk, index) => htmlChunk(chunk) + (index < expressions.length ? "${" + expressions[index] + "}" : "")).join("") + "`";
  }
  return nested(node);
}

let transformed = nested(tree);
if (!/import\s*\{[^}]*\bt\b[^}]*\}\s*from\s*["']\.\/i18n["']/.test(transformed)) {
  transformed = `import { t } from "./i18n";\n${transformed}`;
}
if (args.includes("--write")) fs.writeFileSync(file, transformed);
if (args.includes("--check") && transformed !== source) {
  console.error(`${file} still has authored UI literals to localize.`);
  process.exitCode = 1;
}
const keysIndex = args.indexOf("--keys");
if (keysIndex >= 0) fs.writeFileSync(args[keysIndex + 1], JSON.stringify([...keys].sort(), null, 2) + "\n");
console.log(`${keys.size} authored UI messages${args.includes("--write") ? ` localized in ${file}` : " (dry run)"}.`);
