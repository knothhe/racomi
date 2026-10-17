import assert from "node:assert/strict";
import test from "node:test";
import { renderMarkdownDocument } from "../src/markdown.ts";

test("frontmatter wrap control preserves long metadata and escapes its label", () => {
  const text = "description: " + "A long authored line <&> ".repeat(40);
  const wrapped = renderMarkdownDocument(`---\n${text}\n---\n# Body`, { frontmatterWrap: { label: '软换行"<&>', enabled: true } });
  assert.match(wrapped, /data-wrap="true"/);
  assert.match(wrapped, /data-action="toggle-metadata-wrap"/);
  assert.match(wrapped, /aria-pressed="true"/);
  assert.match(wrapped, /aria-label="软换行&quot;&lt;&amp;&gt;"/);
  assert.match(wrapped, /A long authored line &lt;&amp;&gt;/);
  const unwrapped = renderMarkdownDocument(`---\n${text}\n---\n# Body`, { frontmatterWrap: { label: "Wrap", enabled: false } });
  assert.match(unwrapped, /data-wrap="false"/);
  assert.match(unwrapped, /aria-pressed="false"/);
  assert.equal(wrapped.split("<pre>")[1], unwrapped.split("<pre>")[1]);
});

test("skill Markdown renders headings, nested lists, code, tables, links and quotes", () => {
  const result = renderMarkdownDocument(`# Review 技能

Read **carefully** with \`git diff\`.

1. Inspect changes
   - Check examples
2. Report findings

> Keep the author's instructions.

\`\`\`typescript
const example = "<script>";
\`\`\`

| State | Meaning |
| --- | --- |
| Ready | Read locally |

[Documentation](https://example.com/docs?q=skills&lang=en)
`);

  assert.match(result, /<h1 id="markdown-review-技能">Review 技能<\/h1>/);
  assert.match(result, /<strong>carefully<\/strong>/);
  assert.match(result, /<code>git diff<\/code>/);
  assert.match(result, /<ol>[\s\S]*<ul>[\s\S]*<li>Check examples<\/li>[\s\S]*<\/ul>[\s\S]*<\/ol>/);
  assert.match(result, /<blockquote>\n<p>Keep the author's instructions\.<\/p>\n<\/blockquote>/);
  assert.match(result, /<pre><code class="language-typescript">const example = &quot;&lt;script&gt;&quot;;/);
  assert.match(result, /<table>[\s\S]*<th>State<\/th>[\s\S]*<td>Read locally<\/td>[\s\S]*<\/table>/);
  assert.match(result, /<a href="https:\/\/example.com\/docs\?q=skills&amp;lang=en" target="_blank" rel="noopener noreferrer">Documentation<\/a>/);
});

test("closed YAML frontmatter is a collapsed disclosure and preserves author text", () => {
  const source = '\uFEFF---\r\nname: review\r\ndescription: "<Review> & report"\r\n---\r\n# Original title\r\nRead before editing.\r\n';
  const result = renderMarkdownDocument(source, { frontmatterLabel: "元信息" });

  assert.match(result, /^<details class="markdown-frontmatter"><summary>元信息<\/summary>/);
  assert.doesNotMatch(result, /<details[^>]*\bopen\b/);
  assert.match(result, /name: review\r\ndescription: &quot;&lt;Review&gt; &amp; report&quot;/);
  assert.match(result, /<h1 id="markdown-original-title">Original title<\/h1>/);
  assert.doesNotMatch(result, /<h2>name:/);
});

test("frontmatter requires an opening delimiter at the beginning and a closing delimiter", () => {
  for (const source of ["---\nname: unfinished\n# Instructions", "# Instructions\n\n---\nname: ordinary text\n---\n"]) {
    const result = renderMarkdownDocument(source);
    assert.doesNotMatch(result, /markdown-frontmatter/);
    assert.match(result, /Instructions/);
    assert.match(result, /name:/);
  }
  assert.match(renderMarkdownDocument("---\n---\n# Instructions"), /<details[\s\S]*<h1 id="markdown-instructions">Instructions<\/h1>/);
  assert.match(renderMarkdownDocument("---\nname: review\n...\n# Instructions"), /<details[\s\S]*<h1 id="markdown-instructions">Instructions<\/h1>/);
});

test("author HTML, code content and frontmatter UI labels cannot inject markup", () => {
  const result = renderMarkdownDocument(`---
description: </code></pre><img src=x onerror=alert(1)>
---
<script>alert(1)</script>

<img src=x onerror=alert(1)>

\`\`\`html
</code></pre><script>alert(2)</script>
\`\`\`
`, { frontmatterLabel: '<img src=x onerror="alert(3)">' });

  assert.doesNotMatch(result, /<(?:script|img)\b/i);
  assert.match(result, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
  assert.match(result, /&lt;\/code&gt;&lt;\/pre&gt;&lt;script&gt;alert\(2\)&lt;\/script&gt;/);
  assert.match(result, /<summary>&lt;img src=x onerror=&quot;alert\(3\)&quot;&gt;<\/summary>/);
});

test("unsafe and desktop application URL schemes never become links or images", () => {
  for (const url of [
    "javascript:alert(1)", "JaVaScRiPt:alert(1)", "javascript&#58;alert(1)",
    "java&#x73;cript:alert(1)", "vbscript:msgbox(1)", "file:///etc/passwd",
    "data:text/html;base64,PHNjcmlwdD4=", "data:image/svg+xml;base64,PHN2Zz4=",
    "data:image/png;base64,iVBORw0KGgo=", "wails:invoke", "command:run", "vscode:open",
  ]) {
    const result = renderMarkdownDocument(`[unsafe](${url})\n\n![unsafe](${url})`);
    assert.doesNotMatch(result, /<(?:a|img)\b/i, url);
  }
});

test("safe external, relative and fragment links remain readable", () => {
  const result = renderMarkdownDocument("[Site](https://example.com) [Email](mailto:team@example.com) [Guide](references/guide.md) [Section](#instructions)\n\nhttps://example.com/docs\n\n![Diagram](https://example.com/diagram.png \"A diagram\")");
  assert.match(result, /href="mailto:team@example.com" target="_blank" rel="noopener noreferrer"/);
  assert.match(result, /<span class="markdown-reference">Guide <code>references\/guide.md<\/code><\/span>/);
  assert.match(result, /<a href="#markdown-instructions">Section<\/a>/);
  assert.match(result, /<a href="https:\/\/example.com\/docs" target="_blank" rel="noopener noreferrer">https:\/\/example.com\/docs<\/a>/);
  assert.match(result, /<img src="https:\/\/example.com\/diagram.png" alt="Diagram" title="A diagram" loading="lazy" decoding="async" referrerpolicy="no-referrer">/);
});

test("heading anchors are stable, unique, Unicode aware and isolated from application IDs", () => {
  const source = `# API
## **使用** \`技能\` &amp; café
## API
## API-1
## API
## <img src=x onerror=alert(1)>

[API](#api) [Unicode](#使用-技能-café) [Encoded](#%E4%BD%BF%E7%94%A8-%E6%8A%80%E8%83%BD-caf%C3%A9) [Repeat](#api-2)
`;
  const result = renderMarkdownDocument(source);
  const ids = [...result.matchAll(/\bid="([^"]+)"/g)].map((match) => match[1]);
  assert.deepEqual(ids, ["markdown-api", "markdown-使用-技能-café", "markdown-api-1", "markdown-api-1-1", "markdown-api-2", "markdown-img-srcx-onerroralert1"]);
  assert.equal(new Set(ids).size, ids.length);
  assert.match(result, /href="#markdown-api"/);
  assert.equal((result.match(/href="#markdown-使用-技能-café"/g) ?? []).length, 2);
  assert.match(result, /href="#markdown-api-2"/);
  assert.doesNotMatch(result, /<(?:img|script)\b|\bid="api"/);
  assert.equal(renderMarkdownDocument(source), result);
});

test("relative document and image references show escaped author text without requesting app paths", () => {
  const result = renderMarkdownDocument(`[Read **guide**](references/说明.md)

[Reference][ref]

![A **diagram**](assets/diagram.svg)

[Escaped](references/%3Cscript%3E.md)

![&lt;image&gt;](assets/%22%3E%3Cimg%3E.svg)

[Network](//example.com/docs)

![Network image](//example.com/image.png)

[ref]: ../SKILL.md
`);
  assert.match(result, /<span class="markdown-reference">Read <strong>guide<\/strong> <code>references\/说明.md<\/code><\/span>/);
  assert.match(result, /<span class="markdown-reference">Reference <code>\.\.\/SKILL.md<\/code><\/span>/);
  assert.match(result, /<span class="markdown-reference">A diagram <code>assets\/diagram.svg<\/code><\/span>/);
  assert.match(result, /<code>references\/&lt;script&gt;.md<\/code>/);
  assert.match(result, /&lt;image&gt; <code>assets\/&quot;&gt;&lt;img&gt;.svg<\/code>/);
  assert.doesNotMatch(result, /(?:href|src)="(?:references|assets|\.\.)|<script\b/);
  assert.match(result, /<a href="\/\/example.com\/docs" target="_blank" rel="noopener noreferrer">Network<\/a>/);
  assert.match(result, /<img src="\/\/example.com\/image.png"/);
});
