import MarkdownIt, { type Token } from "markdown-it";

function inlineText(tokens: Token[]): string {
  return tokens.map((token) => token.children ? inlineText(token.children)
    : ["text", "code_inline"].includes(token.type) ? token.content
    : ["softbreak", "hardbreak"].includes(token.type) ? " " : "").join("");
}

function headingSlug(text: string): string {
  return text.normalize("NFC").toLowerCase().replace(/[^\p{L}\p{N}\p{M}_\-\s]/gu, "").trim().replace(/\s+/g, "-") || "section";
}

function relativeResource(url: string): boolean {
  return !/^(?:https?:|mailto:|\/\/)/i.test(url);
}

const markdown = new MarkdownIt({
  // Skill documents can come from untrusted repositories. Treat author HTML as
  // text, and let the parser escape code and all generated attributes.
  html: false,
  linkify: true,
  typographer: false,
});

const validateLink = markdown.validateLink.bind(markdown);
markdown.validateLink = (url: string): boolean => {
  if (!validateLink(url)) return false;
  // Keep Markdown's relative document links, but do not let documents invoke
  // desktop application protocols. Retain the parser's own URL validation too.
  const scheme = url.trim().match(/^([a-z][a-z\d+.-]*):/i)?.[1];
  return !scheme || /^(?:https?|mailto)$/i.test(scheme);
};

markdown.core.ruler.push("reader_document", (state) => {
  const headingIDs = new Set<string>();
  for (let index = 0; index < state.tokens.length; index++) {
    const token = state.tokens[index];
    if (token.type === "heading_open") {
      const base = headingSlug(inlineText(state.tokens[index + 1]?.children ?? []));
      let slug = base;
      for (let suffix = 1; headingIDs.has(slug); suffix++) slug = `${base}-${suffix}`;
      headingIDs.add(slug);
      token.attrSet("id", `markdown-${slug}`);
    }
    const links: (string | null)[] = [];
    for (const child of token.children ?? []) {
      if (child.type === "link_open") {
        const href = String(child.attrGet("href") ?? "");
        if (href.startsWith("#")) {
          let fragment = href.slice(1);
          try { fragment = decodeURIComponent(fragment); } catch { /* Preserve malformed fragments as readable text. */ }
          child.attrSet("href", `#markdown-${headingSlug(fragment)}`);
          links.push(null);
        } else if (relativeResource(href)) {
          child.tag = "span";
          child.attrs = [["class", "markdown-reference"]];
          links.push(markdown.normalizeLinkText(href));
        } else links.push(null);
      } else if (child.type === "link_close") {
        const reference = links.pop();
        if (reference != null) {
          child.tag = "span";
          child.meta = { reference };
        }
      }
    }
  }
});

markdown.renderer.rules.link_open = (tokens, index, options, _env, renderer) => {
  const href = String(tokens[index].attrGet("href") ?? "");
  if (tokens[index].tag === "a" && !href.startsWith("#")) {
    tokens[index].attrSet("target", "_blank");
    tokens[index].attrSet("rel", "noopener noreferrer");
  }
  return renderer.renderToken(tokens, index, options);
};

markdown.renderer.rules.link_close = (tokens, index, options, _env, renderer) => {
  const reference = tokens[index].meta?.reference;
  return `${reference == null ? "" : ` <code>${markdown.utils.escapeHtml(String(reference))}</code>`}${renderer.renderToken(tokens, index, options)}`;
};

const renderImage = markdown.renderer.rules.image!;
markdown.renderer.rules.image = (tokens, index, options, env, renderer) => {
  const token = tokens[index];
  const src = String(token.attrGet("src") ?? "");
  if (relativeResource(src)) {
    return `<span class="markdown-reference">${markdown.utils.escapeHtml(inlineText(token.children ?? []))} <code>${markdown.utils.escapeHtml(markdown.normalizeLinkText(src))}</code></span>`;
  }
  tokens[index].attrSet("loading", "lazy");
  tokens[index].attrSet("decoding", "async");
  tokens[index].attrSet("referrerpolicy", "no-referrer");
  return renderImage(tokens, index, options, env, renderer);
};

export type MarkdownDocumentOptions = {
  frontmatterLabel?: string;
};

/**
 * Render an HTML fragment for a `.markdown-body` container. The original input
 * is never modified, so callers can show the same content in their raw view.
 */
export function renderMarkdownDocument(content: string, options: MarkdownDocumentOptions = {}): string {
  // Recognize frontmatter only at the document start. Do not parse YAML: showing
  // it verbatim in a collapsed disclosure also supports unfamiliar metadata.
  const opening = content.match(/^\uFEFF?---[ \t]*\r?\n/);
  if (!opening) return markdown.render(content);
  const remainder = content.slice(opening[0].length);
  const closing = remainder.match(/^(?:---|\.\.\.)[ \t]*(?:\r?\n|$)/m);
  if (!closing) return markdown.render(content);

  const metadata = remainder.slice(0, closing.index).replace(/\r?\n$/, "");
  const label = markdown.utils.escapeHtml(options.frontmatterLabel ?? "Metadata");
  return `<details class="markdown-frontmatter"><summary>${label}</summary><pre><code>${markdown.utils.escapeHtml(metadata)}</code></pre></details>\n${markdown.render(remainder.slice(closing.index! + closing[0].length))}`;
}
