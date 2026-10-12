import { icon } from "./icons.ts";

export interface SourceFilterOption {
  id: string;
  name: string;
  description?: string;
}

export interface SourceFilterProps {
  name: "skills-source" | "preset-source";
  selected: string;
  options: SourceFilterOption[];
  label: string;
  searchLabel: string;
  emptyLabel: string;
  locale?: string;
}

const escapeHTML = (value: string) => value.replace(/[&<>"']/g, (character) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[character]!);
const matchesQuery = (text: string, query: string, locale?: string) => text.toLocaleLowerCase(locale).includes(query.trim().toLocaleLowerCase(locale));

export function filterSourceOptions(options: SourceFilterOption[], query: string, locale?: string): SourceFilterOption[] {
  return options.filter((option) => matchesQuery(`${option.name} ${option.description || ""}`, query, locale));
}

export function sourcePopupPosition(
  anchor: { left: number; right: number; top: number; bottom: number; width: number },
  viewport: { width: number; height: number; left?: number; top?: number },
  preferredHeight: number,
) {
  const margin = 12, gap = 5;
  const viewportLeft = viewport.left || 0, viewportTop = viewport.top || 0;
  const width = Math.min(Math.max(anchor.width, 280), Math.max(0, viewport.width - margin * 2));
  const availableHeight = Math.max(0, viewport.height - margin * 2);
  const below = Math.min(availableHeight, Math.max(0, viewportTop + viewport.height - margin - anchor.bottom - gap));
  const above = Math.min(availableHeight, Math.max(0, anchor.top - gap - viewportTop - margin));
  const placement = below < preferredHeight && above > below ? "above" : "below";
  const maxHeight = placement === "above" ? above : below;
  const height = Math.min(preferredHeight, maxHeight);
  return {
    width,
    maxHeight,
    placement,
    left: Math.max(viewportLeft + margin, Math.min(anchor.left, viewportLeft + viewport.width - margin - width)),
    top: Math.max(viewportTop + margin, Math.min(placement === "above" ? anchor.top - gap - height : anchor.bottom + gap, viewportTop + viewport.height - margin - height)),
  };
}

// The caller supplies the surrounding field and its visible label. Keeping this
// control out of a <label> lets the search box and trigger behave independently.
export function renderSourceFilter(props: SourceFilterProps): string {
  const { name, options, label, searchLabel, emptyLabel, locale = "zh-CN" } = props;
  const selected = options.find((option) => option.id === props.selected) || options[0];
  const prefix = `source-filter-${name}`;
  return `<div class="searchable-source-picker" data-source-picker="${name}" data-source-label="${escapeHTML(label)}" data-source-locale="${escapeHTML(locale)}"><input type="hidden" name="${name}" value="${escapeHTML(selected?.id || "")}"><button type="button" class="source-picker-trigger" aria-label="${escapeHTML(`${label}: ${selected?.name || ""}`)}" aria-haspopup="dialog" aria-expanded="false" aria-controls="${prefix}-popup" data-source-trigger><span data-source-selected-label>${escapeHTML(selected?.name || "")}</span>${icon("chevron-down", "source-picker-chevron")}</button><div class="source-picker-popup" id="${prefix}-popup" role="dialog" aria-label="${escapeHTML(label)}" hidden><div class="source-picker-search">${icon("search", "source-picker-search-icon")}<input type="text" class="source-picker-search-input" data-source-search role="combobox" aria-label="${escapeHTML(searchLabel)}" placeholder="${escapeHTML(searchLabel)}" aria-autocomplete="list" aria-expanded="false" aria-controls="${prefix}-options" autocomplete="off" spellcheck="false"></div><div class="source-picker-options" id="${prefix}-options" role="listbox" aria-label="${escapeHTML(label)}">${options.map((option, index) => `<div class="source-picker-option" id="${prefix}-option-${index}" role="option" tabindex="-1" aria-selected="${option.id === selected?.id}" data-source-option="${escapeHTML(option.id)}" data-source-option-name="${escapeHTML(option.name)}" data-source-search-text="${escapeHTML(`${option.name} ${option.description || ""}`)}"><span><strong>${escapeHTML(option.name)}</strong>${option.description ? `<small>${escapeHTML(option.description)}</small>` : ""}</span>${icon("check", "source-picker-check")}</div>`).join("")}</div><p class="source-picker-empty" role="status" hidden>${escapeHTML(emptyLabel)}</p></div></div>`;
}

const installedDocuments = new WeakSet<Document>();

export function installSourceFilters(root: Document = document): void {
  if (installedDocuments.has(root)) return;
  installedDocuments.add(root);

  const view = root.defaultView!;
  const popups = new WeakMap<HTMLElement, HTMLElement>();
  const popupOwners = new WeakMap<HTMLElement, HTMLElement>();
  const modalOwners = new WeakMap<HTMLElement, HTMLElement>();
  const expandedPickers = new Set<HTMLElement>();
  const popupFor = (picker: HTMLElement) => popups.get(picker) || picker.querySelector<HTMLElement>(".source-picker-popup")!;
  const pickerFor = (target: Element) => target.closest<HTMLElement>("[data-source-picker]") || popupOwners.get(target.closest<HTMLElement>(".source-picker-popup")!) || null;
  const triggerFor = (picker: HTMLElement) => picker.querySelector<HTMLButtonElement>("[data-source-trigger]")!;
  const searchFor = (picker: HTMLElement) => popupFor(picker).querySelector<HTMLInputElement>("[data-source-search]")!;
  const optionsFor = (picker: HTMLElement) => [...popupFor(picker).querySelectorAll<HTMLElement>("[data-source-option]")];
  const openPickers = () => [...expandedPickers];
  const close = (picker: HTMLElement, restoreFocus = false) => {
    expandedPickers.delete(picker);
    delete picker.dataset.open;
    const popup = popupFor(picker);
    popup.hidden = true;
    const modal = modalOwners.get(picker);
    if (modal) {
      const owned = (modal.getAttribute("aria-owns") || "").split(/\s+/).filter((id) => id && id !== popup.id);
      if (owned.length) modal.setAttribute("aria-owns", owned.join(" ")); else modal.removeAttribute("aria-owns");
      modalOwners.delete(picker);
    }
    if (picker.isConnected) picker.append(popup); else popup.remove();
    resizeObserver?.unobserve(triggerFor(picker));
    if (!expandedPickers.size) mutationObserver.disconnect();
    triggerFor(picker).setAttribute("aria-expanded", "false");
    const search = searchFor(picker);
    search.setAttribute("aria-expanded", "false");
    search.removeAttribute("aria-activedescendant");
    if (restoreFocus) triggerFor(picker).focus({ preventScroll: true });
  };
  const position = (picker: HTMLElement) => {
    if (!picker.isConnected) { close(picker); return; }
    const anchor = triggerFor(picker).getBoundingClientRect();
    const viewport = view.visualViewport;
    const bounds = { width: viewport?.width || view.innerWidth, height: viewport?.height || view.innerHeight, left: viewport?.offsetLeft || 0, top: viewport?.offsetTop || 0 };
    let visibleLeft = Math.max(anchor.left, bounds.left), visibleRight = Math.min(anchor.right, bounds.left + bounds.width);
    let visibleTop = Math.max(anchor.top, bounds.top), visibleBottom = Math.min(anchor.bottom, bounds.top + bounds.height);
    for (let ancestor = picker.parentElement; ancestor; ancestor = ancestor.parentElement) {
      const style = view.getComputedStyle(ancestor);
      const clipping = ancestor.getBoundingClientRect();
      if (/(auto|scroll|hidden|clip)/.test(style.overflowX)) { visibleLeft = Math.max(visibleLeft, clipping.left); visibleRight = Math.min(visibleRight, clipping.right); }
      if (/(auto|scroll|hidden|clip)/.test(style.overflowY)) { visibleTop = Math.max(visibleTop, clipping.top); visibleBottom = Math.min(visibleBottom, clipping.bottom); }
    }
    if (visibleRight <= visibleLeft || visibleBottom <= visibleTop) { close(picker); return; }
    const popup = popupFor(picker);
    const list = popup.querySelector<HTMLElement>(".source-picker-options")!;
    const scrollTop = list.scrollTop;
    // Measure the natural height after setting its width; wrapping source names
    // can change the height when a narrow window constrains the popup.
    popup.style.width = `${sourcePopupPosition(anchor, bounds, 0).width}px`;
    popup.style.maxHeight = "none";
    const layout = sourcePopupPosition(anchor, bounds, popup.getBoundingClientRect().height);
    popup.style.left = `${layout.left}px`;
    popup.style.top = `${layout.top}px`;
    popup.style.maxHeight = `${layout.maxHeight}px`;
    popup.dataset.placement = layout.placement;
    list.scrollTop = scrollTop;
  };
  let positionFrame = 0;
  const schedulePosition = () => {
    if (positionFrame || !expandedPickers.size) return;
    positionFrame = view.requestAnimationFrame(() => {
      positionFrame = 0;
      let moving = false;
      for (const picker of openPickers()) {
        position(picker);
        // A modal may still be animating when its trigger is opened. Track the
        // anchor until its ancestor animations settle as well as on scroll.
        for (let ancestor: HTMLElement | null = picker; ancestor; ancestor = ancestor.parentElement) {
          if (ancestor.getAnimations().some((animation) => animation.playState === "running")) moving = true;
        }
      }
      if (moving) schedulePosition();
    });
  };
  const resizeObserver = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(schedulePosition);
  const mutationObserver = new MutationObserver(schedulePosition);
  root.addEventListener("scroll", (event) => {
    if (!(event.target instanceof Element) || !event.target.closest(".source-picker-popup")) schedulePosition();
  }, true);
  root.addEventListener("animationstart", schedulePosition, true);
  root.addEventListener("transitionrun", schedulePosition, true);
  view.addEventListener("resize", schedulePosition);
  view.visualViewport?.addEventListener("resize", schedulePosition);
  view.visualViewport?.addEventListener("scroll", schedulePosition);
  const activate = (picker: HTMLElement, option?: HTMLElement, scroll = true) => {
    for (const item of optionsFor(picker)) item.classList.toggle("keyboard-active", item === option);
    const search = searchFor(picker);
    if (option) {
      search.setAttribute("aria-activedescendant", option.id);
      if (scroll) option.scrollIntoView({ block: "nearest" });
    } else search.removeAttribute("aria-activedescendant");
  };
  const filter = (picker: HTMLElement) => {
    const search = searchFor(picker);
    const options = optionsFor(picker);
    for (const option of options) option.hidden = !matchesQuery(option.dataset.sourceSearchText || "", search.value, picker.dataset.sourceLocale);
    const visible = options.filter((option) => !option.hidden);
    popupFor(picker).querySelector<HTMLElement>(".source-picker-empty")!.hidden = visible.length > 0;
    activate(picker, visible.find((option) => option.getAttribute("aria-selected") === "true") || visible[0], false);
    schedulePosition();
  };
  const open = (picker: HTMLElement, last = false) => {
    for (const other of openPickers()) if (other !== picker) close(other);
    const popup = popupFor(picker);
    popups.set(picker, popup);
    popupOwners.set(popup, picker);
    const modal = picker.closest<HTMLElement>('[role="dialog"][aria-modal="true"]');
    if (modal) {
      const owned = (modal.getAttribute("aria-owns") || "").split(/\s+/).filter(Boolean);
      if (!owned.includes(popup.id)) owned.push(popup.id);
      modal.setAttribute("aria-owns", owned.join(" "));
      modalOwners.set(picker, modal);
    }
    // Keep modal popups inside its focus boundary, but outside the scrolling
    // body, animated modal and filtered backdrop that would clip fixed content.
    (picker.closest("#modal-root") || root.body).append(popup);
    expandedPickers.add(picker);
    picker.dataset.open = "";
    popup.hidden = false;
    triggerFor(picker).setAttribute("aria-expanded", "true");
    const search = searchFor(picker);
    search.value = "";
    search.setAttribute("aria-expanded", "true");
    filter(picker);
    position(picker);
    resizeObserver?.observe(triggerFor(picker));
    mutationObserver.observe(root.body, { childList: true, subtree: true });
    search.focus({ preventScroll: true });
    const options = optionsFor(picker);
    activate(picker, last ? options.at(-1) : options.find((option) => option.classList.contains("keyboard-active")));
  };
  const choose = (picker: HTMLElement, option: HTMLElement) => {
    const value = option.dataset.sourceOption || "";
    const hidden = picker.querySelector<HTMLInputElement>('input[type="hidden"]')!;
    const changed = hidden.value !== value;
    hidden.value = value;
    const name = option.dataset.sourceOptionName || "";
    picker.querySelector<HTMLElement>("[data-source-selected-label]")!.textContent = name;
    triggerFor(picker).setAttribute("aria-label", `${picker.dataset.sourceLabel}: ${name}`);
    for (const item of optionsFor(picker)) item.setAttribute("aria-selected", String(item === option));
    close(picker, true);
    if (changed) {
      hidden.dispatchEvent(new Event("change", { bubbles: true }));
      // Consumers can redraw the whole page in response to the change.
      root.querySelector<HTMLButtonElement>(`[data-source-picker="${picker.dataset.sourcePicker}"] [data-source-trigger]`)?.focus({ preventScroll: true });
    }
  };

  root.addEventListener("click", (event) => {
    const target = event.target;
    if (!(target instanceof Element)) return;
    const picker = pickerFor(target);
    for (const other of openPickers()) if (other !== picker) close(other);
    if (!picker) return;
    if (target.closest("[data-source-trigger]")) {
      if (picker.hasAttribute("data-open")) close(picker, true); else open(picker);
    }
    const option = target.closest<HTMLElement>("[data-source-option]");
    if (option && !option.hidden && picker.hasAttribute("data-open")) choose(picker, option);
  });
  root.addEventListener("input", (event) => {
    const target = event.target;
    if (!(target instanceof HTMLInputElement) || !target.matches("[data-source-search]")) return;
    const picker = pickerFor(target);
    if (picker) filter(picker);
  });
  root.addEventListener("focusin", (event) => {
    for (const picker of openPickers()) if (event.target instanceof Node && !picker.contains(event.target) && !popupFor(picker).contains(event.target)) close(picker);
  });
  // Capture Escape before the containing preset dialog can close itself.
  root.addEventListener("keydown", (event) => {
    if (event.isComposing || event.keyCode === 229) return;
    if (!(event.target instanceof Element)) return;
    const picker = pickerFor(event.target);
    if (!picker) return;
    const expanded = picker.hasAttribute("data-open");
    if (event.key === "Escape" && expanded) {
      event.preventDefault(); event.stopPropagation(); close(picker, true); return;
    }
    if (event.key === "Tab" && expanded) {
      // Return to the trigger before native Tab traversal so the portalled
      // search field does not move keyboard focus to the end of the modal.
      close(picker, true);
      if (event.shiftKey) event.preventDefault();
      return;
    }
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      if (!expanded) { open(picker, event.key === "ArrowUp"); return; }
      const visible = optionsFor(picker).filter((option) => !option.hidden);
      if (!visible.length) return;
      const current = visible.findIndex((option) => option.classList.contains("keyboard-active"));
      const next = (current + (event.key === "ArrowDown" ? 1 : -1) + visible.length) % visible.length;
      activate(picker, visible[next]);
      searchFor(picker).focus({ preventScroll: true });
    }
    if (event.key === "Enter" && expanded && event.target.matches("[data-source-search], [data-source-option]")) {
      event.preventDefault();
      const option = optionsFor(picker).find((item) => !item.hidden && item.classList.contains("keyboard-active"));
      if (option) choose(picker, option);
    }
  }, true);
}
