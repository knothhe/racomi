const layouts = [
  { key: "skills", selector: ".skills-layout", minLeft: 260, minRight: 300 },
  { key: "sources", selector: ".sources-layout", minLeft: 260, minRight: 300 },
  { key: "presets", selector: ".presets-layout", minLeft: 220, minRight: 300 },
  { key: "projects", selector: ".projects-layout", minLeft: 180, minRight: 320 },
  { key: "discover", selector: ".discover-content.has-preview", minLeft: 300, minRight: 300 },
] as const;
type SplitLayout = typeof layouts[number];
const storagePrefix = "racomi.workspaceSplit.";
const ratios = new Map<string, number | undefined>();
const observed = new Set<Element>();
let separatorLabel = "";
let installed = false;
let observer: ResizeObserver | undefined;
let frame: number | undefined;
let drag: { layout: HTMLElement; edge: HTMLElement; key: string; pointerId: number; offset: number; ratio: number } | undefined;

export function splitBounds(width: number, minLeft: number, minRight: number) {
  const available = Math.max(0, width);
  const scale = Math.min(1, available / (minLeft + minRight));
  return { min: minLeft * scale, max: Math.max(minLeft * scale, available - minRight * scale) };
}

function readRatio(key: string): number | undefined {
  try {
    const value = Number(localStorage.getItem(storagePrefix + key));
    if (Number.isFinite(value) && value > 0 && value < 1) return value;
  } catch { /* Keep resizing available when storage is blocked. */ }
  return undefined;
}

function keepRatio(key: string, value: number | undefined) {
  ratios.set(key, value);
  try {
    if (value === undefined) localStorage.removeItem(storagePrefix + key);
    else localStorage.setItem(storagePrefix + key, String(value));
  } catch { /* Preserve the width for this session. */ }
}

function measure(layout: HTMLElement, config: SplitLayout) {
  // Read the original responsive grid before applying a saved width. A stacked
  // layout keeps its own row sizing and never exposes a horizontal separator.
  layout.removeAttribute("data-workspace-split");
  const style = getComputedStyle(layout);
  const columns = style.gridTemplateColumns.trim().split(/\s+/);
  const gap = Number.parseFloat(style.columnGap) || 0;
  const padding = Number.parseFloat(style.paddingLeft) || 0;
  const width = layout.clientWidth - padding - (Number.parseFloat(style.paddingRight) || 0) - gap;
  const horizontal = columns.length === 2 && width > 0;
  return { horizontal, width, gap, padding, ...splitBounds(width, config.minLeft, config.minRight), initial: Number.parseFloat(columns[0]) || width / 2 };
}

function apply(layout: HTMLElement, config: SplitLayout) {
  let edge = layout.querySelector<HTMLElement>("[data-workspace-split-edge]");
  if (!edge) {
    edge = document.createElement("div");
    edge.className = "workspace-split-resizer";
    edge.dataset.workspaceSplitEdge = config.key;
    edge.dataset.preserveScroll = `workspace-split:${config.key}`;
    edge.setAttribute("role", "separator");
    edge.setAttribute("aria-orientation", "vertical");
    const firstPane = layout.firstElementChild;
    if (firstPane) {
      if (!firstPane.id) firstPane.id = `workspace-pane-${config.key}`;
      edge.setAttribute("aria-controls", firstPane.id);
    }
    layout.append(edge);
  }
  if (!ratios.has(config.key)) ratios.set(config.key, readRatio(config.key));
  const bounds = measure(layout, config);
  edge.hidden = !bounds.horizontal;
  edge.tabIndex = bounds.horizontal ? 0 : -1;
  edge.title = separatorLabel;
  edge.setAttribute("aria-label", separatorLabel);
  if (!bounds.horizontal) return;
  const ratio = drag?.layout === layout ? drag.ratio : ratios.get(config.key);
  const left = Math.max(bounds.min, Math.min(bounds.max, ratio === undefined ? bounds.initial : bounds.width * ratio));
  layout.style.setProperty("--workspace-columns", `${left}px minmax(0, 1fr)`);
  edge.style.left = `${bounds.padding + left + bounds.gap / 2}px`;
  edge.setAttribute("aria-valuemin", String(Math.round(bounds.min / bounds.width * 100)));
  edge.setAttribute("aria-valuemax", String(Math.round(bounds.max / bounds.width * 100)));
  edge.setAttribute("aria-valuenow", String(Math.round(left / bounds.width * 100)));
  edge.setAttribute("aria-valuetext", `${Math.round(left)} px`);
  layout.dataset.workspaceSplit = config.key;
}

export function syncWorkspaceResize(label?: string) {
  if (label !== undefined) separatorLabel = label;
  if (drag && !drag.layout.isConnected) endDrag(false);
  const current = new Set<Element>();
  for (const config of layouts) {
    for (const layout of document.querySelectorAll<HTMLElement>(config.selector)) {
      current.add(layout);
      apply(layout, config);
      if (observer && !observed.has(layout)) observer.observe(layout);
    }
  }
  for (const layout of observed) if (!current.has(layout)) observer?.unobserve(layout);
  observed.clear();
  for (const layout of current) observed.add(layout);
  if (drag?.edge.hidden) endDrag(false);
}

function scheduleSync() {
  if (frame !== undefined) return;
  frame = requestAnimationFrame(() => { frame = undefined; syncWorkspaceResize(); });
}

function eventEdge(event: Event) {
  return event.target instanceof Element ? event.target.closest<HTMLElement>("[data-workspace-split-edge]") : null;
}

function context(edge: HTMLElement) {
  const layout = edge.parentElement;
  const config = layouts.find((item) => item.key === edge.dataset.workspaceSplitEdge);
  if (!layout || !config || edge.hidden) return;
  return { layout, config, bounds: measure(layout, config) };
}

function endDrag(commit: boolean) {
  if (!drag) return;
  const ended = drag;
  drag = undefined;
  ended.edge.classList.remove("is-resizing");
  document.body.classList.remove("workspace-split-drag");
  if (commit) keepRatio(ended.key, ended.ratio);
  try {
    if (ended.edge.hasPointerCapture(ended.pointerId)) ended.edge.releasePointerCapture(ended.pointerId);
  } catch { /* A page render may have replaced the captured separator. */ }
  syncWorkspaceResize();
}

export function installWorkspaceResize() {
  if (installed) return;
  installed = true;
  if (typeof ResizeObserver !== "undefined") observer = new ResizeObserver(scheduleSync);
  document.addEventListener("pointerdown", (event) => {
    const edge = eventEdge(event);
    if (!edge || event.button !== 0 || drag) return;
    const item = context(edge);
    if (!item?.bounds.horizontal) return;
    const left = Math.max(item.bounds.min, Math.min(item.bounds.max, ratios.get(item.config.key) === undefined ? item.bounds.initial : item.bounds.width * ratios.get(item.config.key)!));
    drag = { layout: item.layout, edge, key: item.config.key, pointerId: event.pointerId, offset: left - event.clientX + item.layout.getBoundingClientRect().left + item.bounds.padding, ratio: left / item.bounds.width };
    event.preventDefault();
    edge.classList.add("is-resizing");
    edge.focus({ preventScroll: true });
    try { edge.setPointerCapture(event.pointerId); } catch { /* Document listeners still finish the drag. */ }
    document.body.classList.add("workspace-split-drag");
    apply(item.layout, item.config);
  });
  document.addEventListener("pointermove", (event) => {
    if (!drag || event.pointerId !== drag.pointerId) return;
    const item = context(drag.edge);
    if (!item?.bounds.horizontal) { endDrag(false); return; }
    const left = Math.max(item.bounds.min, Math.min(item.bounds.max, event.clientX - item.layout.getBoundingClientRect().left - item.bounds.padding + drag.offset));
    drag.ratio = left / item.bounds.width;
    apply(item.layout, item.config);
  });
  document.addEventListener("pointerup", (event) => { if (event.pointerId === drag?.pointerId) endDrag(true); });
  document.addEventListener("pointercancel", (event) => { if (event.pointerId === drag?.pointerId) endDrag(false); });
  document.addEventListener("lostpointercapture", (event) => { if (event.pointerId === drag?.pointerId) endDrag(false); });
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape" && drag) { event.preventDefault(); endDrag(false); return; }
    const edge = eventEdge(event);
    if (!edge || !["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
    const item = context(edge);
    if (!item?.bounds.horizontal) return;
    event.preventDefault();
    endDrag(false);
    const saved = ratios.get(item.config.key);
    const current = Math.max(item.bounds.min, Math.min(item.bounds.max, saved === undefined ? item.bounds.initial : item.bounds.width * saved));
    const step = event.shiftKey ? 32 : 8;
    const wanted = event.key === "Home" ? item.bounds.min : event.key === "End" ? item.bounds.max : current + (event.key === "ArrowLeft" ? -step : step);
    keepRatio(item.config.key, Math.max(item.bounds.min, Math.min(item.bounds.max, wanted)) / item.bounds.width);
    apply(item.layout, item.config);
  });
  document.addEventListener("dblclick", (event) => {
    const edge = eventEdge(event);
    if (!edge || edge.hidden) return;
    event.preventDefault();
    endDrag(false);
    keepRatio(edge.dataset.workspaceSplitEdge!, undefined);
    syncWorkspaceResize();
  });
  window.addEventListener("resize", scheduleSync);
  document.addEventListener("racomi:sidebar-resize", scheduleSync);
  document.addEventListener("racomi:nav-layout-change", scheduleSync);
  window.addEventListener("storage", (event) => {
    if (event.key !== null && !event.key.startsWith(storagePrefix)) return;
    endDrag(false);
    ratios.clear();
    syncWorkspaceResize();
  });
}
