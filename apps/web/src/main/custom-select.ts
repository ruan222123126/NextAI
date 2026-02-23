export interface CustomSelectController {
  initCustomSelects: () => void;
  syncCustomSelect: (select: HTMLSelectElement) => void;
  syncAllCustomSelects: () => void;
  closeAllCustomSelects: (except?: HTMLSelectElement) => void;
}

interface CustomSelectInstance {
  container: HTMLDivElement;
  trigger: HTMLDivElement;
  selectedText: HTMLSpanElement;
  optionsList: HTMLDivElement;
  optionsBody: HTMLDivElement;
  searchInput: HTMLInputElement | null;
  isSearchEnabled: boolean;
}

interface CustomSelectControllerOptions {
  translate: (key: string) => string;
}

const CUSTOM_SELECT_OPTIONS_VERTICAL_GAP_PX = 6;
const CUSTOM_SELECT_VISIBLE_OPTIONS_COUNT = 3;
const CUSTOM_SELECT_OPTION_ROW_HEIGHT_PX = 38;
const CUSTOM_SELECT_OPTIONS_LIST_VERTICAL_PADDING_PX = 8;
const CUSTOM_SELECT_MAX_OPTIONS_HEIGHT_PX = CUSTOM_SELECT_VISIBLE_OPTIONS_COUNT * CUSTOM_SELECT_OPTION_ROW_HEIGHT_PX
  + CUSTOM_SELECT_OPTIONS_LIST_VERTICAL_PADDING_PX;
const CUSTOM_SELECT_SEARCH_FIELD_HEIGHT_PX = 40;
const CUSTOM_SELECT_OPTIONS_PANEL_VERTICAL_PADDING_PX = 12;
const CUSTOM_SELECT_MAX_PANEL_HEIGHT_PX = CUSTOM_SELECT_SEARCH_FIELD_HEIGHT_PX
  + CUSTOM_SELECT_OPTIONS_VERTICAL_GAP_PX
  + CUSTOM_SELECT_MAX_OPTIONS_HEIGHT_PX
  + CUSTOM_SELECT_OPTIONS_PANEL_VERTICAL_PADDING_PX;

export function createCustomSelectController(options: CustomSelectControllerOptions): CustomSelectController {
  const { translate } = options;

  const customSelectInstances = new Map<HTMLSelectElement, CustomSelectInstance>();
  let customSelectGlobalEventsBound = false;

  function initCustomSelects(): void {
    document.body.classList.add("select-enhanced");
    const selects = Array.from(document.querySelectorAll<HTMLSelectElement>("select"));
    for (const select of selects) {
      if (customSelectInstances.has(select)) {
        continue;
      }
      enhanceSelectControl(select);
    }
    bindCustomSelectGlobalEvents();
    syncAllCustomSelects();
  }

  function enhanceSelectControl(select: HTMLSelectElement): void {
    const parent = select.parentElement;
    if (!parent) {
      return;
    }
    const isSearchEnabled = select.dataset.selectSearch !== "off";

    const container = document.createElement("div");
    container.className = "custom-select-container";
    container.classList.toggle("without-search", !isSearchEnabled);
    parent.insertBefore(container, select);
    container.appendChild(select);
    select.dataset.customSelectNative = "true";
    select.tabIndex = -1;

    const trigger = document.createElement("div");
    trigger.className = "select-trigger";
    trigger.tabIndex = 0;
    trigger.setAttribute("role", "button");
    trigger.setAttribute("aria-haspopup", "listbox");
    trigger.setAttribute("aria-expanded", "false");

    const selectedText = document.createElement("span");
    selectedText.className = "selected-text";

    const arrow = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    arrow.setAttribute("class", "arrow");
    arrow.setAttribute("viewBox", "0 0 20 20");
    arrow.setAttribute("fill", "currentColor");
    arrow.setAttribute("aria-hidden", "true");

    const arrowPath = document.createElementNS("http://www.w3.org/2000/svg", "path");
    arrowPath.setAttribute("fill-rule", "evenodd");
    arrowPath.setAttribute("d", "M5.293 7.293a1 1 0 011.414 0L10 10.586l3.293-3.293a1 1 0 111.414 1.414l-4 4a1 1 0 01-1.414 0l-4-4a1 1 0 010-1.414z");
    arrowPath.setAttribute("clip-rule", "evenodd");
    arrow.appendChild(arrowPath);

    trigger.append(selectedText, arrow);

    const optionsList = document.createElement("div");
    optionsList.className = "options-list";

    const optionsBody = document.createElement("div");
    optionsBody.className = "options-body";
    optionsBody.setAttribute("role", "listbox");

    let searchInput: HTMLInputElement | null = null;
    if (isSearchEnabled) {
      const searchField = document.createElement("div");
      searchField.className = "options-search";
      searchInput = document.createElement("input");
      searchInput.className = "options-search-input";
      searchInput.type = "search";
      searchInput.autocomplete = "off";
      searchInput.spellcheck = false;
      searchInput.placeholder = translate("tab.search");
      searchInput.setAttribute("aria-label", translate("search.inputLabel"));
      searchField.appendChild(searchInput);
      optionsList.append(searchField);
    }
    optionsList.append(optionsBody);

    container.append(trigger, optionsList);
    customSelectInstances.set(select, {
      container,
      trigger,
      selectedText,
      optionsList,
      optionsBody,
      searchInput,
      isSearchEnabled,
    });

    trigger.addEventListener("click", (event) => {
      event.stopPropagation();
      if (select.disabled || select.options.length === 0) {
        return;
      }
      toggleCustomSelect(select);
    });

    trigger.addEventListener("keydown", (event) => {
      handleCustomSelectTriggerKeydown(event, select);
    });

    optionsList.addEventListener("click", (event) => {
      const target = event.target;
      if (!(target instanceof Element)) {
        return;
      }
      const optionElement = target.closest<HTMLElement>(".option");
      if (!optionElement || optionElement.classList.contains("disabled") || optionElement.classList.contains("is-hidden")) {
        return;
      }
      const value = optionElement.dataset.value ?? "";
      selectCustomOption(select, value);
      closeCustomSelect(select);
      trigger.focus();
      event.stopPropagation();
    });

    optionsList.addEventListener(
      "wheel",
      (event) => {
        if (!(event instanceof WheelEvent)) {
          return;
        }
        if (optionsBody.scrollHeight <= optionsBody.clientHeight) {
          return;
        }
        optionsBody.scrollTop += event.deltaY;
        event.preventDefault();
      },
      { passive: false },
    );

    if (searchInput) {
      searchInput.addEventListener("input", () => {
        filterCustomSelectOptions(select, searchInput.value);
      });

      searchInput.addEventListener("keydown", (event) => {
        handleCustomSelectSearchKeydown(event, select);
      });
    }

    select.addEventListener("change", () => {
      syncCustomSelect(select);
    });
  }

  function bindCustomSelectGlobalEvents(): void {
    if (customSelectGlobalEventsBound) {
      return;
    }
    customSelectGlobalEventsBound = true;

    document.addEventListener("click", (event) => {
      const target = event.target;
      if (!(target instanceof Node)) {
        closeAllCustomSelects();
        return;
      }
      for (const instance of customSelectInstances.values()) {
        if (instance.container.contains(target)) {
          return;
        }
      }
      closeAllCustomSelects();
    });

    document.addEventListener("keydown", (event) => {
      if (event.key !== "Escape") {
        return;
      }
      closeAllCustomSelects();
    });
  }

  function handleCustomSelectTriggerKeydown(event: KeyboardEvent, select: HTMLSelectElement): void {
    if (select.disabled || select.options.length === 0) {
      return;
    }
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      toggleCustomSelect(select);
      return;
    }
    if (event.key === "Escape") {
      closeCustomSelect(select);
      return;
    }
    if (event.key !== "ArrowDown" && event.key !== "ArrowUp") {
      return;
    }
    event.preventDefault();
    const options = Array.from(select.options).filter((option) => !option.disabled);
    if (options.length === 0) {
      return;
    }
    const selectedIndex = options.findIndex((option) => option.value === select.value);
    const offset = event.key === "ArrowDown" ? 1 : -1;
    const nextIndex = selectedIndex === -1 ? 0 : (selectedIndex + offset + options.length) % options.length;
    const nextValue = options[nextIndex].value;
    selectCustomOption(select, nextValue);
    openCustomSelect(select);
  }

  function handleCustomSelectSearchKeydown(event: KeyboardEvent, select: HTMLSelectElement): void {
    if (event.key === "Escape") {
      event.preventDefault();
      closeCustomSelect(select);
      const instance = customSelectInstances.get(select);
      instance?.trigger.focus();
      return;
    }
    if (event.key !== "ArrowDown" && event.key !== "ArrowUp") {
      return;
    }
    event.preventDefault();
    const offset = event.key === "ArrowDown" ? 1 : -1;
    const options = getCustomSelectNavigableOptionElements(select);
    if (options.length === 0) {
      return;
    }
    const selectedIndex = options.findIndex((item) => item.classList.contains("selected"));
    const nextIndex = selectedIndex === -1
      ? (offset > 0 ? 0 : options.length - 1)
      : (selectedIndex + offset + options.length) % options.length;
    const nextOption = options[nextIndex];
    if (!nextOption) {
      return;
    }
    const nextValue = nextOption.dataset.value ?? "";
    if (nextValue === "") {
      return;
    }
    selectCustomOption(select, nextValue);
    if (typeof nextOption.scrollIntoView === "function") {
      nextOption.scrollIntoView({ block: "nearest" });
    }
  }

  function syncAllCustomSelects(): void {
    for (const select of customSelectInstances.keys()) {
      syncCustomSelect(select);
    }
  }

  function syncCustomSelect(select: HTMLSelectElement): void {
    const instance = customSelectInstances.get(select);
    if (!instance) {
      return;
    }

    instance.optionsBody.innerHTML = "";
    for (const option of Array.from(select.options)) {
      const optionElement = document.createElement("div");
      optionElement.className = "option";
      optionElement.dataset.value = option.value;
      const text = (option.textContent ?? "").trim();
      optionElement.textContent = text;
      optionElement.dataset.searchText = text.toLowerCase();
      optionElement.setAttribute("role", "option");
      optionElement.setAttribute("aria-selected", String(option.selected));
      if (option.disabled) {
        optionElement.classList.add("disabled");
        optionElement.setAttribute("aria-disabled", "true");
      }
      if (option.selected) {
        optionElement.classList.add("selected");
      }
      instance.optionsBody.appendChild(optionElement);
    }

    const selectedOption = Array.from(select.selectedOptions)[0] ?? select.options[select.selectedIndex] ?? select.options[0];
    instance.selectedText.textContent = selectedOption?.textContent?.trim() || "";
    if (instance.searchInput) {
      instance.searchInput.placeholder = translate("tab.search");
      instance.searchInput.setAttribute("aria-label", translate("search.inputLabel"));
    }
    filterCustomSelectOptions(select, instance.searchInput?.value ?? "");
    instance.container.classList.toggle("is-disabled", select.disabled);
    instance.trigger.setAttribute("aria-disabled", String(select.disabled));
    instance.trigger.tabIndex = select.disabled ? -1 : 0;

    if (select.disabled || select.options.length === 0) {
      closeCustomSelect(select);
    }
  }

  function selectCustomOption(select: HTMLSelectElement, value: string): void {
    const nextValue = value.trim();
    if (select.value === nextValue) {
      syncCustomSelect(select);
      return;
    }
    select.value = nextValue;
    select.dispatchEvent(new Event("change", { bubbles: true }));
  }

  function toggleCustomSelect(select: HTMLSelectElement): void {
    const instance = customSelectInstances.get(select);
    if (!instance) {
      return;
    }
    const nextOpen = !instance.container.classList.contains("open");
    if (nextOpen) {
      openCustomSelect(select);
      return;
    }
    closeCustomSelect(select);
  }

  function openCustomSelect(select: HTMLSelectElement): void {
    const instance = customSelectInstances.get(select);
    if (!instance) {
      return;
    }
    closeAllCustomSelects(select);
    if (instance.searchInput) {
      instance.searchInput.value = "";
    }
    filterCustomSelectOptions(select, "");
    applyCustomSelectOpenDirection(select);
    instance.container.classList.add("open");
    instance.trigger.setAttribute("aria-expanded", "true");
    if (instance.searchInput) {
      instance.searchInput.focus();
    }
  }

  function closeCustomSelect(select: HTMLSelectElement): void {
    const instance = customSelectInstances.get(select);
    if (!instance) {
      return;
    }
    instance.container.classList.remove("open");
    instance.trigger.setAttribute("aria-expanded", "false");
    if (instance.searchInput && instance.searchInput.value !== "") {
      instance.searchInput.value = "";
      filterCustomSelectOptions(select, "");
    }
  }

  function closeAllCustomSelects(except?: HTMLSelectElement): void {
    for (const [select] of customSelectInstances.entries()) {
      if (select === except) {
        continue;
      }
      closeCustomSelect(select);
    }
  }

  function applyCustomSelectOpenDirection(select: HTMLSelectElement): void {
    const instance = customSelectInstances.get(select);
    if (!instance) {
      return;
    }
    const optionCount = Math.max(instance.optionsBody.childElementCount, select.options.length, 1);
    const searchSectionHeight = instance.isSearchEnabled
      ? CUSTOM_SELECT_SEARCH_FIELD_HEIGHT_PX + CUSTOM_SELECT_OPTIONS_VERTICAL_GAP_PX
      : 0;
    const maxPanelHeight = searchSectionHeight
      + CUSTOM_SELECT_MAX_OPTIONS_HEIGHT_PX
      + CUSTOM_SELECT_OPTIONS_PANEL_VERTICAL_PADDING_PX;
    const estimatedOptionsHeight = Math.min(optionCount * CUSTOM_SELECT_OPTION_ROW_HEIGHT_PX, CUSTOM_SELECT_MAX_OPTIONS_HEIGHT_PX);
    const estimatedPanelHeight = Math.min(searchSectionHeight + estimatedOptionsHeight + CUSTOM_SELECT_OPTIONS_PANEL_VERTICAL_PADDING_PX, maxPanelHeight);
    const panelHeight = Math.min(
      Math.max(instance.optionsList.scrollHeight, estimatedPanelHeight),
      maxPanelHeight,
    );
    const requiredSpace = panelHeight + CUSTOM_SELECT_OPTIONS_VERTICAL_GAP_PX;
    const triggerRect = instance.trigger.getBoundingClientRect();
    const bounds = resolveCustomSelectVerticalBounds(instance.container);
    const availableBelow = Math.max(0, bounds.bottom - triggerRect.bottom);
    const availableAbove = Math.max(0, triggerRect.top - bounds.top);
    const shouldOpenUpward = availableBelow < requiredSpace && availableAbove > availableBelow;
    instance.container.classList.toggle("open-upward", shouldOpenUpward);
  }

  function getCustomSelectNavigableOptionElements(select: HTMLSelectElement): HTMLElement[] {
    const instance = customSelectInstances.get(select);
    if (!instance) {
      return [];
    }
    return Array.from(instance.optionsBody.querySelectorAll<HTMLElement>(".option"))
      .filter((option) => !option.classList.contains("disabled") && !option.classList.contains("is-hidden"));
  }

  function filterCustomSelectOptions(select: HTMLSelectElement, queryText: string): void {
    const instance = customSelectInstances.get(select);
    if (!instance) {
      return;
    }
    const query = queryText.trim().toLowerCase();
    for (const option of Array.from(instance.optionsBody.querySelectorAll<HTMLElement>(".option"))) {
      const label = option.dataset.searchText ?? "";
      const visible = query === "" || label.includes(query);
      option.classList.toggle("is-hidden", !visible);
      option.setAttribute("aria-hidden", String(!visible));
    }
  }

  function resolveCustomSelectVerticalBounds(container: HTMLElement): { top: number; bottom: number } {
    const viewportBottom = window.innerHeight || document.documentElement.clientHeight || 0;
    let top = 0;
    let bottom = viewportBottom;
    let current: HTMLElement | null = container.parentElement;
    while (current) {
      const computed = window.getComputedStyle(current);
      if (isClippingOverflowValue(computed.overflow) || isClippingOverflowValue(computed.overflowY)) {
        const rect = current.getBoundingClientRect();
        top = Math.max(top, rect.top);
        bottom = Math.min(bottom, rect.bottom);
      }
      current = current.parentElement;
    }
    if (bottom <= top) {
      return {
        top: 0,
        bottom: viewportBottom,
      };
    }
    return { top, bottom };
  }

  function isClippingOverflowValue(value: string): boolean {
    const normalized = value.trim().toLowerCase();
    return normalized.includes("hidden")
      || normalized.includes("auto")
      || normalized.includes("scroll")
      || normalized.includes("clip");
  }

  return {
    initCustomSelects,
    syncCustomSelect,
    syncAllCustomSelects,
    closeAllCustomSelects,
  };
}
