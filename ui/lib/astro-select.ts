// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { filterSelectOptions, nextEnabledOption, normalizeSelectQuery, optionIndexAt, type SelectOption } from './select';
import { naturalHeight, placeDropdown, type DropdownPlacement } from './dropdown-placement';
import { pushOverlay, removeOverlay, isTopmost, overlayIndex, trapFocus, registerOverlayAnchor, overlayContains } from './overlay-stack';

let sequence = 0;
const MAX_HEIGHT_PX = 380;

function searchGlyph(clear = false): SVGSVGElement {
  const namespace = 'http://www.w3.org/2000/svg';
  const svg = document.createElementNS(namespace, 'svg');
  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('aria-hidden', 'true');
  if (!clear) {
    const circle = document.createElementNS(namespace, 'circle');
    circle.setAttribute('cx', '11'); circle.setAttribute('cy', '11'); circle.setAttribute('r', '7');
    svg.append(circle);
  }
  const path = document.createElementNS(namespace, 'path');
  path.setAttribute('d', clear ? 'm6 6 12 12M18 6 6 18' : 'm20 20-3.5-3.5');
  svg.append(path);
  return svg;
}

type NavigationDirection = 1 | -1 | 'first' | 'last';
const NAVIGATION_KEYS: Record<string, NavigationDirection | undefined> = {
  ArrowDown: 1, ArrowUp: -1, Home: 'first', End: 'last',
};

function hasAccessibleName(element: HTMLElement): boolean {
  return element.hasAttribute('aria-labelledby') || element.hasAttribute('aria-label');
}

function labelIds(labels: HTMLLabelElement[], id: string): string {
  return labels.map((field, index) => {
    const text = field.querySelector<HTMLElement>('.bb-field__label') || field;
    text.id ||= `${id}-label-${index}`;
    return text.id;
  }).join(' ');
}

function createTrigger(native: HTMLSelectElement, fallback: HTMLElement, id: string, label: string): HTMLButtonElement {
  const trigger = document.createElement('button');
  trigger.type = 'button';
  trigger.id = id;
  trigger.className = `bb-select__trigger ${fallback.className}`;
  if (fallback.hasAttribute('data-invalid')) trigger.dataset.invalid = '';
  for (const attribute of Array.from(native.attributes)) {
    if (attribute.name.startsWith('aria-') || attribute.name === 'title') trigger.setAttribute(attribute.name, attribute.value);
  }
  trigger.setAttribute('aria-haspopup', 'dialog');
  trigger.setAttribute('aria-expanded', 'false');
  const labels = Array.from(native.labels);
  if (!hasAccessibleName(trigger) && labels.length) trigger.setAttribute('aria-labelledby', labelIds(labels, id));
  if (!hasAccessibleName(trigger)) trigger.setAttribute('aria-label', label);
  return trigger;
}

function appendChevron(trigger: HTMLButtonElement, fallback: HTMLElement): void {
  const chevron = fallback.querySelector('svg')?.cloneNode(true) as SVGElement | undefined;
  if (!chevron) return;
  chevron.setAttribute('class', 'bb-select__chevron');
  trigger.append(chevron);
}

function mountNativeBridge(root: HTMLElement, native: HTMLSelectElement, fallback: HTMLElement, trigger: HTMLButtonElement): void {
  if (root.dataset.value !== undefined) native.value = root.dataset.value;
  native.id = `${trigger.id}-native`;
  native.classList.add('bb-select__native');
  native.tabIndex = -1;
  native.setAttribute('aria-hidden', 'true');
  root.append(native);
  fallback.replaceWith(trigger);
}

function readOption(option: HTMLOptionElement): SelectOption {
  const group = option.parentElement instanceof HTMLOptGroupElement ? option.parentElement : undefined;
  return {
    value: option.value, label: option.label,
    disabled: option.disabled || group?.disabled,
    group: option.dataset.group || group?.label,
    description: option.dataset.description, searchText: option.dataset.searchText, triggerLabel: option.dataset.triggerLabel,
  };
}

function appendOptionText(item: HTMLLIElement, option: SelectOption): void {
  const text = document.createElement('span');
  text.className = 'bb-select__label'; text.textContent = option.label; item.append(text);
  if (!option.description) return;
  const description = document.createElement('span');
  description.className = 'bb-select__description'; description.textContent = option.description; item.append(description);
}

interface OptionRenderContext {
  id: string;
  selected: string;
  pick: (option: SelectOption) => void;
}

function createOptionItem(option: SelectOption, index: number, context: OptionRenderContext): HTMLLIElement {
  const { id, selected, pick } = context;
  const item = document.createElement('li');
  item.id = `${id}-option-${index}`; item.className = 'bb-select__option'; item.dataset.index = String(index);
  item.setAttribute('role', 'option'); item.setAttribute('aria-selected', String(option.value === selected));
  if (option.disabled) item.setAttribute('aria-disabled', 'true');
  appendOptionText(item, option);
  item.addEventListener('click', () => pick(option));
  return item;
}

function appendGroupHeading(list: HTMLUListElement, group: string | undefined, previous: string | undefined): void {
  if (!group || group === previous) return;
  const heading = document.createElement('li');
  heading.className = 'bb-select__group'; heading.setAttribute('role', 'presentation');
  heading.textContent = group; list.append(heading);
}

function appendEmptyMessage(list: HTMLUListElement, label: string): void {
  const empty = document.createElement('li');
  empty.className = 'bb-select__empty'; empty.setAttribute('role', 'presentation');
  empty.textContent = label; list.append(empty);
}

function isTypeaheadKey(event: KeyboardEvent): boolean {
  if (event.key.length !== 1) return false;
  return ![event.ctrlKey, event.metaKey, event.altKey].some(Boolean);
}

function typeaheadIndices(options: readonly SelectOption[], query: string): number[] {
  const prefix = normalizeSelectQuery(query);
  return options.flatMap((option, index) => {
    if (option.disabled) return [];
    return normalizeSelectQuery(option.label).startsWith(prefix) ? [index] : [];
  });
}

function createPanel(label: string, keydown: (event: KeyboardEvent) => void): HTMLDivElement {
  const panel = document.createElement('div'); panel.className = 'bb-picker-panel';
  panel.setAttribute('role', 'dialog'); panel.setAttribute('aria-label', label);
  panel.dataset.overlay = ''; panel.dataset.lenisPrevent = ''; panel.tabIndex = -1;
  panel.addEventListener('keydown', keydown);
  return panel;
}

function mountMobilePanel(panel: HTMLDivElement, label: string, close: () => void): { shell: HTMLDivElement; overlayId: number } {
  const shell = document.createElement('div'); shell.className = 'bb-picker-panel__shell'; shell.dataset.overlay = '';
  const scrim = document.createElement('button'); scrim.type = 'button';
  scrim.className = 'bb-picker-panel__scrim'; scrim.setAttribute('aria-label', label);
  scrim.addEventListener('click', close);
  panel.classList.add('bb-picker-panel--sheet'); panel.setAttribute('aria-modal', 'true');
  shell.append(scrim, panel); document.body.append(shell);
  const overlayId = pushOverlay(); shell.style.zIndex = String(300 + overlayIndex(overlayId) * 10);
  return { shell, overlayId };
}

function applyPlacement(panel: HTMLElement, { left, width, maxHeight, top, bottom }: DropdownPlacement): void {
  Object.assign(panel.style, {
    left: `${left}px`, width: `${width}px`, maxHeight: `${maxHeight}px`,
    top: top === undefined ? '' : `${top}px`, bottom: bottom === undefined ? '' : `${bottom}px`,
  });
}

function mountDesktopPanel(panel: HTMLDivElement, trigger: HTMLButtonElement): void {
  panel.classList.add('bb-picker-panel--dropdown');
  document.body.append(panel);
  const viewport = { width: window.innerWidth, height: window.innerHeight };
  applyPlacement(panel, placeDropdown(trigger.getBoundingClientRect(), viewport, MAX_HEIGHT_PX, (width) => naturalHeight(panel, width)));
}

function createList(id: string, label: string, searchable: boolean): HTMLUListElement {
  const list = document.createElement('ul'); list.id = `${id}-list`; list.className = 'bb-select__list';
  list.setAttribute('role', 'listbox'); list.setAttribute('aria-label', label); list.tabIndex = searchable ? -1 : 0;
  return list;
}

function createSearch(root: HTMLElement, panel: HTMLDivElement, id: string, render: () => void): { input: HTMLInputElement; clear: HTMLButtonElement } {
  const search = document.createElement('label'); search.className = 'bb-search bb-input bb-input--fill';
  const input = document.createElement('input'); input.type = 'search'; input.className = 'bb-search__input';
  input.placeholder = root.dataset.searchPlaceholder ?? 'Search…'; input.setAttribute('aria-label', input.placeholder);
  input.setAttribute('role', 'combobox'); input.setAttribute('aria-expanded', 'true'); input.setAttribute('aria-autocomplete', 'list');
  input.setAttribute('aria-controls', `${id}-list`); input.autocomplete = 'off'; input.addEventListener('input', render);
  const clear = document.createElement('button'); clear.type = 'button'; clear.className = 'bb-search__clear';
  clear.setAttribute('aria-label', root.dataset.searchClearLabel ?? 'Clear search'); clear.append(searchGlyph(true));
  clear.addEventListener('click', () => { input.value = ''; render(); input.focus(); });
  search.append(searchGlyph(), input, clear); panel.append(search);
  return { input, clear };
}

/** Enhance once; the native select remains the form and change-event bridge. */
export function enhanceAstroSelect(root: HTMLElement): void {
  if (root.hasAttribute('data-select-enhanced')) return;
  const native = root.querySelector<HTMLSelectElement>('select');
  const fallback = root.querySelector<HTMLElement>('[data-select-fallback]');
  if (!native || !fallback) return;
  root.dataset.selectEnhanced = '';
  const id = native.id || `bb-astro-select-${++sequence}`;
  const label = root.dataset.label || 'Select an option';
  const trigger = createTrigger(native, fallback, id, label);
  const valueLabel = document.createElement('span');
  valueLabel.className = 'bb-select__value';
  trigger.append(valueLabel);
  appendChevron(trigger, fallback);
  mountNativeBridge(root, native, fallback, trigger);

  let panel: HTMLDivElement | undefined;
  let shell: HTMLDivElement | undefined;
  let input: HTMLInputElement | undefined;
  let clear: HTMLButtonElement | undefined;
  let list: HTMLUListElement | undefined;
  let matches: readonly SelectOption[] = [];
  let active = -1;
  let overlayId: number | undefined;
  let focusTrap: ReturnType<typeof trapFocus> | undefined;
  let unregisterAnchor: (() => void) | undefined;
  const searchable = root.hasAttribute('data-searchable');
  let typeahead = '';
  let typedAt = 0;
  let pointer: { x: number; y: number } | undefined;

  function options(includeFallback = false): SelectOption[] {
    return Array.from(native!.options)
      .filter((option) => includeFallback || !option.hasAttribute('data-select-fallback-option'))
      .map(readOption);
  }

  function sync() {
    const selected = options(true).find((option) => option.value === native!.value);
    valueLabel.textContent = selected?.triggerLabel ?? selected?.label ?? root.dataset.placeholder ?? 'Select…';
    trigger.disabled = native!.disabled;
    trigger.toggleAttribute('data-placeholder', !native!.value);
    if (trigger.disabled && panel) close(false);
  }

  function close(restore = true) {
    if (!panel) return;
    focusTrap?.destroy();
    focusTrap = undefined;
    unregisterAnchor?.();
    unregisterAnchor = undefined;
    if (overlayId !== undefined) removeOverlay(overlayId);
    overlayId = undefined;
    (shell ?? panel).remove();
    shell = panel = undefined;
    input = undefined; clear = undefined; list = undefined;
    trigger.setAttribute('aria-expanded', 'false');
    trigger.removeAttribute('aria-controls');
    document.removeEventListener('pointerdown', outside, true);
    window.removeEventListener('scroll', moved, true);
    window.removeEventListener('resize', moved);
    if (restore) trigger.focus();
  }

  function pick(option: SelectOption) {
    if (native!.disabled || option.disabled) return;
    const changed = native!.value !== option.value;
    native!.value = option.value;
    sync(); close();
    if (changed) {
      native!.dispatchEvent(new Event('input', { bubbles: true }));
      native!.dispatchEvent(new Event('change', { bubbles: true }));
    }
  }

  function activate(index: number, reveal = true) {
    active = index;
    if (!list) return;
    list.querySelectorAll<HTMLElement>('[role="option"]').forEach((item, i) => item.classList.toggle('active', i === active));
    const focus = input ?? list;
    if (active < 0) {
      focus.removeAttribute('aria-activedescendant');
      return;
    }
    const activeId = `${id}-option-${active}`;
    focus.setAttribute('aria-activedescendant', activeId);
    if (reveal) revealOption(activeId);
  }

  function revealOption(optionId: string) {
    pointer = undefined;
    document.getElementById(optionId)?.scrollIntoView({ block: 'nearest' });
  }

  function hoverAt(target: EventTarget | null) {
    const index = list ? optionIndexAt(list, target) : -1;
    if (index >= 0 && !matches[index].disabled) activate(index, false);
  }

  function trackPointer(event: PointerEvent) {
    pointer = event.pointerType === 'touch' ? undefined : { x: event.clientX, y: event.clientY };
    hoverAt(event.target);
  }

  function followPointer() {
    if (pointer) hoverAt(document.elementFromPoint(pointer.x, pointer.y));
  }

  function followHover(scroller: HTMLElement, options: HTMLUListElement) {
    options.addEventListener('pointermove', trackPointer);
    options.addEventListener('pointerleave', () => { pointer = undefined; });
    scroller.addEventListener('scroll', followPointer, { passive: true });
  }

  function appendChoices(target: HTMLUListElement) {
    const context: OptionRenderContext = { id, selected: native!.value, pick };
    let group: string | undefined;
    for (const [index, option] of matches.entries()) {
      appendGroupHeading(target, option.group, group);
      group = option.group;
      target.append(createOptionItem(option, index, context));
    }
    if (!matches.length) appendEmptyMessage(target, root.dataset.emptyLabel ?? 'No matches');
  }

  function resetActive() {
    const selected = matches.findIndex((option) => option.value === native!.value && !option.disabled);
    activate(selected >= 0 ? selected : nextEnabledOption(matches, -1, 'first'));
  }

  function renderOptions() {
    if (!list) return;
    matches = filterSelectOptions(options(), input?.value ?? '');
    list.replaceChildren();
    appendChoices(list);
    if (clear) clear.hidden = !input?.value;
    resetActive();
  }

  function handleEscape(event: KeyboardEvent) {
    if (!panel) return;
    if (overlayId !== undefined && !isTopmost(overlayId)) return;
    event.preventDefault(); event.stopPropagation(); close();
  }

  function handleNavigation(event: KeyboardEvent, direction: NavigationDirection) {
    // Home and End keep their editing meaning in a searchable picker.
    if (input && typeof direction === 'string') return;
    if (!panel) open();
    event.preventDefault();
    activate(nextEnabledOption(matches, active, direction));
  }

  function handleSelection(event: KeyboardEvent) {
    if (!panel) return;
    event.preventDefault();
    if (matches[active]) pick(matches[active]);
  }

  function handleSpace(event: KeyboardEvent) {
    if (!input) handleSelection(event);
  }

  function handleTab() {
    if (overlayId !== undefined) return;
    if (panel) close(true);
  }

  function handleTypeahead(event: KeyboardEvent) {
    if (!panel) return;
    if (input) return;
    if (!isTypeaheadKey(event)) return;
    event.preventDefault();
    const now = Date.now();
    typeahead = (now - typedAt > 700 ? '' : typeahead) + event.key;
    typedAt = now;
    const hits = typeaheadIndices(matches, typeahead);
    if (hits.length) activate(hits.find((index) => index > active) ?? hits[0]);
  }

  function keydown(event: KeyboardEvent) {
    // The clear button keeps native activation, including Enter after mobile Tab.
    if (event.target === clear && event.key !== 'Escape') return;
    const direction = NAVIGATION_KEYS[event.key];
    if (direction !== undefined) {
      handleNavigation(event, direction);
      return;
    }
    const handlers: Record<string, (event: KeyboardEvent) => void> = {
      Escape: handleEscape, Enter: handleSelection, ' ': handleSpace, Tab: handleTab,
    };
    (handlers[event.key] ?? handleTypeahead)(event);
  }

  function outside(event: PointerEvent) {
    const target = event.target as Node | null;
    if (!target) return;
    if (trigger.contains(target)) return;
    if (panel && overlayContains(panel, target)) return;
    close(false);
  }
  function moved(event: Event) {
    if (overlayId !== undefined) return;
    if (!(event.target instanceof Node)) { close(false); return; }
    if (panel && overlayContains(panel, event.target)) return;
    close(false);
  }

  function open() {
    if (trigger.disabled || panel) return;
    sync();
    if (trigger.disabled) return;
    panel = createPanel(label, keydown);
    if (searchable) ({ input, clear } = createSearch(root, panel, id, renderOptions));
    list = createList(id, label, searchable);
    panel.append(list); renderOptions();
    const mobile = window.matchMedia('(max-width: 639px)').matches;
    if (mobile) ({ shell, overlayId } = mountMobilePanel(panel, label, () => close()));
    else mountDesktopPanel(panel, trigger);
    unregisterAnchor = registerOverlayAnchor(shell ?? panel, trigger);
    followHover(panel, list);
    activate(active);
    trigger.setAttribute('aria-expanded', 'true'); trigger.setAttribute('aria-controls', list.id);
    if (mobile) focusTrap = trapFocus(panel);
    (input ?? list).focus();
    document.addEventListener('pointerdown', outside, true);
    window.addEventListener('scroll', moved, { capture: true, passive: true }); window.addEventListener('resize', moved, { passive: true });
  }

  trigger.addEventListener('click', () => { if (panel) close(); else open(); });
  trigger.addEventListener('keydown', keydown);
  native.addEventListener('change', sync);
  native.addEventListener('invalid', (event) => { event.preventDefault(); trigger.focus(); open(); });
  // Browser default reset runs after event handlers and their microtasks.
  // Read the native value in the next task so the custom label reflects it.
  native.form?.addEventListener('reset', () => { close(false); setTimeout(sync, 0); });
  const observer = new MutationObserver(() => { sync(); if (panel) renderOptions(); });
  observer.observe(native, { childList: true, subtree: true, attributes: true, attributeFilter: ['disabled', 'label', 'selected', 'value'] });
  document.addEventListener('astro:before-swap', () => { close(false); observer.disconnect(); }, { once: true });
  sync();
}
