import { icon } from "./icons";

// Keep the native select as the form value and change-event source. Every
// single-value select uses the same compact trigger and floating menu.
export function installSelects(root: Document = document) {
  let opened: { select: HTMLSelectElement; trigger: HTMLButtonElement; popup: HTMLDivElement } | null = null;
  let serial = 0;
  let typed = "", lastTyped = 0;
  const labelFor = (select: HTMLSelectElement) => select.getAttribute("aria-label") || select.labels?.[0]?.querySelector(":scope > span")?.textContent?.trim() || select.name;
  function close(focus = false) {
    if (!opened) return;
    const { trigger, popup } = opened;
    opened = null;
    trigger.setAttribute("aria-expanded", "false");
    popup.remove();
    if (focus && trigger.isConnected) trigger.focus({ preventScroll: true });
  }
  function open(select: HTMLSelectElement, trigger: HTMLButtonElement) {
    if (opened?.select === select) { close(true); return; }
    close();
    if (select.disabled) return;
    typed = ""; lastTyped = 0;
    const popup = root.createElement("div");
    popup.id = trigger.getAttribute("aria-controls")!;
    popup.className = "select-popup";
    popup.setAttribute("role", "listbox");
    popup.setAttribute("aria-label", labelFor(select));
    let search: HTMLInputElement | null = null;
    if (select.dataset.selectSearch) {
      search = root.createElement("input");
      search.type = "search";
      search.className = "select-search";
      search.placeholder = select.dataset.selectSearch;
      search.setAttribute("aria-label", select.dataset.selectSearch);
      search.addEventListener("input", () => {
        const query = search!.value.trim().toLocaleLowerCase();
        for (const item of popup.querySelectorAll<HTMLButtonElement>(".select-option")) item.hidden = !item.textContent?.toLocaleLowerCase().includes(query);
      });
      popup.append(search);
    }
    for (const option of select.options) {
      if (option.hidden) continue;
      const item = root.createElement("button");
      item.type = "button"; item.className = "select-option";
      item.setAttribute("role", "option");
      item.setAttribute("aria-selected", String(option.selected));
      item.tabIndex = -1; item.disabled = option.disabled;
      const label = root.createElement("span"); label.textContent = option.text;
      item.append(label); item.insertAdjacentHTML("beforeend", icon("check"));
      item.addEventListener("click", () => {
        select.value = option.value;
        close(true);
        sync(select);
        select.dispatchEvent(new Event("change", { bubbles: true }));
        // A language change can replace the whole page. Keep keyboard focus.
        root.querySelector<HTMLButtonElement>(`[data-select-trigger="${CSS.escape(select.name)}"]`)?.focus({ preventScroll: true });
      });
      popup.append(item);
    }
    const owner = trigger.closest(".modal") || root.body;
    owner.append(popup);
    opened = { select, trigger, popup };
    trigger.setAttribute("aria-expanded", "true");
    const anchor = trigger.getBoundingClientRect();
    const scale = Number(getComputedStyle(root.documentElement).getPropertyValue("--browser-scale")) || 1;
    const width = Math.min(Math.max(anchor.width, 180 * scale), innerWidth - 24);
    popup.style.width = `${width / scale}px`;
    popup.style.maxHeight = `${Math.min(320, innerHeight - 24) / scale}px`;
    const height = popup.getBoundingClientRect().height;
    const below = innerHeight - anchor.bottom - 12;
    const above = anchor.top - 12;
    const showAbove = height > below && above > below;
    popup.style.maxHeight = `${Math.min(320 * scale, showAbove ? above : below) / scale}px`;
    popup.style.left = `${Math.max(12, Math.min(anchor.left, innerWidth - width - 12)) / scale}px`;
    popup.style.top = `${(showAbove ? Math.max(12, anchor.top - Math.min(height, above) - 5) : anchor.bottom + 5) / scale}px`;
    (search || popup.querySelector<HTMLButtonElement>('[aria-selected="true"]:not(:disabled)') || popup.querySelector<HTMLButtonElement>("button:not(:disabled)"))?.focus({ preventScroll: true });
  }
  function sync(select: HTMLSelectElement) {
    if (select.multiple || select.size > 1) return;
    let trigger = select.nextElementSibling as HTMLButtonElement | null;
    if (!trigger?.classList.contains("select-trigger")) {
      trigger = root.createElement("button"); trigger.type = "button";
      trigger.className = `select-trigger ${select.className}`;
      trigger.dataset.selectTrigger = select.name;
      trigger.setAttribute("aria-haspopup", "listbox");
      trigger.setAttribute("aria-expanded", "false");
      trigger.setAttribute("aria-controls", `select-menu-${++serial}`);
      trigger.innerHTML = `<span></span>${icon("chevron-down")}`;
      select.after(trigger);
      select.classList.add("enhanced-select");
      select.tabIndex = -1; select.setAttribute("aria-hidden", "true");
      const button = trigger;
      button.addEventListener("click", (event) => { event.preventDefault(); open(select, button); });
      button.addEventListener("keydown", (event) => {
        if (["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) { event.preventDefault(); open(select, button); }
      });
      select.addEventListener("change", () => sync(select));
    }
    trigger.disabled = select.disabled;
    trigger.querySelector("span")!.textContent = select.selectedOptions[0]?.text || "";
    const label = labelFor(select);
    trigger.setAttribute("aria-label", `${label}: ${select.selectedOptions[0]?.text || ""}`);
    if (opened?.select === select && select.disabled) close();
    if (root.activeElement === select && !select.disabled) trigger.focus({ preventScroll: true });
  }
  root.addEventListener("keydown", (event) => {
    if (!opened) return;
    const { popup } = opened;
    const items = [...popup.querySelectorAll<HTMLButtonElement>("button:not(:disabled):not([hidden])")];
    const index = items.indexOf(root.activeElement as HTMLButtonElement);
    if (event.key === "Escape") { event.preventDefault(); event.stopImmediatePropagation(); close(true); return; }
    if (event.key === "Tab") { close(true); return; }
    const searching = root.activeElement instanceof HTMLInputElement;
    if (searching && !["ArrowDown", "ArrowUp"].includes(event.key)) return;
    let next: HTMLButtonElement | undefined;
    if (event.key === "ArrowDown") next = items[(index + 1) % items.length];
    if (event.key === "ArrowUp") next = items[index < 0 ? items.length - 1 : (index - 1 + items.length) % items.length];
    if (event.key === "Home") next = items[0];
    if (event.key === "End") next = items.at(-1);
    if (event.key.length === 1 && !event.metaKey && !event.ctrlKey && event.key !== " ") {
      typed = Date.now() - lastTyped > 700 ? event.key : typed + event.key; lastTyped = Date.now();
      next = items.find((item) => item.textContent?.toLocaleLowerCase().startsWith(typed.toLocaleLowerCase()));
    }
    if (next) { event.preventDefault(); next.focus({ preventScroll: true }); next.scrollIntoView({ block: "nearest" }); }
  }, true);
  root.addEventListener("pointerdown", (event) => {
    if (opened && event.target instanceof Node && !opened.popup.contains(event.target) && !opened.trigger.contains(event.target)) close();
  }, true);
  root.addEventListener("focusin", (event) => {
    // Form validation and modal autofocus can target the retained native select.
    if (event.target instanceof HTMLSelectElement && event.target.classList.contains("enhanced-select")) {
      sync(event.target);
      return;
    }
    if (opened && event.target instanceof Node && !opened.popup.contains(event.target) && !opened.trigger.contains(event.target)) close();
  });
  root.addEventListener("scroll", (event) => { if (opened && !opened.popup.contains(event.target as Node)) close(); }, true);
  window.addEventListener("resize", () => close());
  const refresh = () => {
    if (opened && !opened.select.isConnected) close();
    for (const select of root.querySelectorAll<HTMLSelectElement>("select")) sync(select);
  };
  new MutationObserver((records) => {
    if (records.some((record) => record.type === "childList" && [...record.addedNodes].some((node) => node instanceof Element && (node.matches("select") || node.querySelector("select"))))) refresh();
    for (const record of records) if (record.target instanceof HTMLSelectElement) sync(record.target);
    if (opened && !opened.select.isConnected) close();
  }).observe(root.body, { childList: true, subtree: true, attributes: true, attributeFilter: ["disabled"] });
  refresh();
}
