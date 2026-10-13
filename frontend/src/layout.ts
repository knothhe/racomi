export type NavLayout = "side" | "top";
export type NavLayoutPreference = NavLayout | "auto";

const layoutKey = "racomi.navLayout";
const widthKey = "racomi.sideWidth";
const sideAt = window.matchMedia("(min-width: 1024px)");
const minWidth = 168;
const maxWidth = 320;
const collapseAt = 104;

function readPreference(): NavLayoutPreference {
  try {
    const saved = localStorage.getItem(layoutKey);
    if (saved === "side" || saved === "top") return saved;
  } catch { /* Use the window width when storage is unavailable. */ }
  return "auto";
}

function readWidth(): number | undefined {
  try {
    const saved = Number(localStorage.getItem(widthKey));
    if (Number.isFinite(saved) && saved > 0) return Math.round(saved);
  } catch { /* The default width still works without storage. */ }
  return undefined;
}

let preference = readPreference();
let savedWidth = readWidth();
let appliedLayout: NavLayout | undefined;
let appliedPreference: NavLayoutPreference | undefined;
let appliedWidth: number | undefined;
let installed = false;

interface SidebarDrag {
  edge: HTMLElement;
  pointerId: number;
  offset: number;
  width: number | undefined;
}

let drag: SidebarDrag | undefined;

function fitWidth(width: number): number {
  // As in Magpie, the sidebar keeps its minimum usable width even in a
  // window too narrow to fit that minimum within 36% of the viewport.
  return Math.round(Math.max(minWidth, Math.min(width, maxWidth, window.innerWidth * 0.36)));
}

function defaultWidth(): number { return window.innerWidth < 860 ? 184 : 212; }

export function getNavLayout(): NavLayout {
  return preference === "auto" ? (sideAt.matches ? "side" : "top") : preference;
}

export function getNavLayoutPreference(): NavLayoutPreference { return preference; }

function sidebarWidth(): number {
  const measured = document.querySelector<HTMLElement>("#racomi-sidebar")?.getBoundingClientRect().width;
  return measured && measured > 0 ? Math.round(measured) : fitWidth(savedWidth ?? defaultWidth());
}

function showWidth(width: number): void {
  const fitted = fitWidth(width);
  document.documentElement.style.setProperty("--side-w", `${fitted}px`);
  const edge = document.querySelector<HTMLElement>("[data-side-edge]");
  if (edge) {
    edge.setAttribute("role", "separator");
    edge.tabIndex = 0;
    edge.setAttribute("aria-orientation", "vertical");
    edge.setAttribute("aria-controls", "racomi-sidebar");
    edge.setAttribute("aria-valuemin", String(minWidth));
    edge.setAttribute("aria-valuemax", String(fitWidth(maxWidth)));
    edge.setAttribute("aria-valuenow", String(fitted));
  }
  if (fitted !== appliedWidth) {
    appliedWidth = fitted;
    document.dispatchEvent(new CustomEvent("racomi:sidebar-resize", { detail: { width: fitted } }));
  }
}

export function syncSidebarWidth(): void { showWidth(drag?.width ?? savedWidth ?? defaultWidth()); }

function keepWidth(width: number): void {
  savedWidth = fitWidth(width);
  try { localStorage.setItem(widthKey, String(savedWidth)); } catch { /* Keep the width for this session. */ }
  syncSidebarWidth();
}

function endDrag(commit: boolean): void {
  if (!drag) return;
  const ended = drag;
  drag = undefined;
  ended.edge.classList.remove("is-resizing");
  document.body?.classList.remove("side-drag");
  try {
    if (ended.edge.hasPointerCapture(ended.pointerId)) ended.edge.releasePointerCapture(ended.pointerId);
  } catch { /* A render may have removed the captured edge. */ }
  if (commit && ended.width !== undefined) keepWidth(ended.width);
  else syncSidebarWidth();
}

function applyLayout(): void {
  // Applying the new CSS can blur a sidebar control before the shell moves
  // the navigation. Preserve it so the shell can restore a visible focus.
  const previousFocus = document.activeElement;
  const layout = getNavLayout();
  if (layout === "top") endDrag(false);
  document.documentElement.dataset.navLayout = layout;
  document.documentElement.dataset.navLayoutPreference = preference;
  syncSidebarWidth();
  if (layout !== appliedLayout || preference !== appliedPreference) {
    appliedLayout = layout;
    appliedPreference = preference;
    document.dispatchEvent(new CustomEvent("racomi:nav-layout-change", { detail: { layout, preference, ...(previousFocus ? { previousFocus } : {}) } }));
  }
}

export function setNavLayoutPreference(next: NavLayoutPreference): void {
  preference = next;
  try {
    if (next === "auto") localStorage.removeItem(layoutKey);
    else localStorage.setItem(layoutKey, next);
  } catch { /* Keep the choice for this session. */ }
  applyLayout();
}

export function toggleNavLayout(): void { setNavLayoutPreference(getNavLayout() === "side" ? "top" : "side"); }

function eventEdge(event: Event): HTMLElement | null {
  return event.target instanceof Element ? event.target.closest<HTMLElement>("[data-side-edge]") : null;
}

// Delegation keeps the separator working when a page render replaces its DOM.
// Capturing the pointer makes a drag continue outside the narrow resize edge.
export function installSidebarResize(): void {
  if (installed) { syncSidebarWidth(); return; }
  installed = true;
  document.addEventListener("pointerdown", (event) => {
    const edge = eventEdge(event);
    if (!edge || event.button !== 0 || getNavLayout() !== "side" || drag) return;
    event.preventDefault();
    drag = { edge, pointerId: event.pointerId, offset: sidebarWidth() - event.clientX, width: undefined };
    edge.classList.add("is-resizing");
    try { edge.setPointerCapture(event.pointerId); } catch { /* Document listeners still finish the drag. */ }
    document.body?.classList.add("side-drag");
  });
  document.addEventListener("pointermove", (event) => {
    if (!drag || event.pointerId !== drag.pointerId) return;
    const wanted = drag.offset + event.clientX;
    if (wanted < collapseAt) {
      endDrag(false);
      setNavLayoutPreference("top");
      return;
    }
    drag.width = fitWidth(wanted);
    showWidth(drag.width);
  });
  document.addEventListener("pointerup", (event) => {
    if (event.pointerId === drag?.pointerId) endDrag(true);
  });
  document.addEventListener("pointercancel", (event) => {
    if (event.pointerId === drag?.pointerId) endDrag(false);
  });
  document.addEventListener("lostpointercapture", (event) => {
    if (event.pointerId === drag?.pointerId) endDrag(false);
  });
  document.addEventListener("dblclick", (event) => {
    if (!eventEdge(event) || getNavLayout() !== "side") return;
    event.preventDefault();
    endDrag(false);
    savedWidth = undefined;
    try { localStorage.removeItem(widthKey); } catch { /* Reset the width for this session. */ }
    syncSidebarWidth();
  });
  document.addEventListener("keydown", (event) => {
    if (event.key === "Escape" && drag) {
      event.preventDefault();
      endDrag(false);
      return;
    }
    if (!eventEdge(event) || getNavLayout() !== "side") return;
    const step = event.shiftKey ? 32 : 8;
    let width: number;
    switch (event.key) {
      case "ArrowLeft": width = sidebarWidth() - step; break;
      case "ArrowRight": width = sidebarWidth() + step; break;
      case "Home": width = minWidth; break;
      case "End": width = maxWidth; break;
      default: return;
    }
    event.preventDefault();
    endDrag(false);
    keepWidth(width);
  });
  syncSidebarWidth();
}

applyLayout();
sideAt.addEventListener("change", () => { if (preference === "auto") applyLayout(); });
window.addEventListener("resize", () => { endDrag(false); applyLayout(); });
window.addEventListener("storage", (event) => {
  if (event.key === layoutKey || event.key === widthKey || event.key === null) {
    endDrag(false);
    if (event.key === layoutKey || event.key === null) preference = readPreference();
    if (event.key === widthKey || event.key === null) savedWidth = readWidth();
    applyLayout();
  }
});
