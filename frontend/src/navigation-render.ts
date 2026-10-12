function nodeKey(node: Node): string {
  if (node.nodeType !== 1) return String(node.nodeType);
  const element = node as Element;
  return [element.tagName, ...["data-page", "data-nav-section", "data-action", "data-tab", "data-filter"].map((name) => element.getAttribute(name) || "")].join(":");
}

// Keep navigation controls mounted while changing selection, counts or locale.
// Replacing them restarts CSS effects and loses hover, focus and scroll state.
export function syncNavigationElement(current: Element, next: Element): void {
  if (current.isEqualNode(next)) return;
  for (const attribute of [...current.attributes]) {
    if (!next.hasAttribute(attribute.name)) current.removeAttribute(attribute.name);
  }
  for (const attribute of [...next.attributes]) {
    if (current.getAttribute(attribute.name) !== attribute.value) current.setAttribute(attribute.name, attribute.value);
  }
  const previous = [...current.childNodes];
  const used = new Set<Node>();
  for (const [index, child] of [...next.childNodes].entries()) {
    const existing = previous.find((node) => !used.has(node) && nodeKey(node) === nodeKey(child));
    if (!existing) {
      current.insertBefore(child.cloneNode(true), current.childNodes[index] || null);
      continue;
    }
    used.add(existing);
    if (current.childNodes[index] !== existing) current.insertBefore(existing, current.childNodes[index] || null);
    if (existing.nodeType === 1) syncNavigationElement(existing as Element, child as Element);
    else if (existing.nodeValue !== child.nodeValue) existing.nodeValue = child.nodeValue;
  }
  for (const node of previous) if (!used.has(node)) current.removeChild(node);
}

export function syncNavigationMarkup(current: Element, markup: string): void {
  const template = current.ownerDocument.createElement("template");
  template.innerHTML = markup;
  const next = template.content.firstElementChild;
  if (next) syncNavigationElement(current, next);
}
