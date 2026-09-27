// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { filterSelectOptions, nextEnabledOption, normalizeSelectQuery, type SelectOption } from './select';
import { pushOverlay, removeOverlay, isTopmost, overlayIndex, trapFocus, registerOverlayAnchor, overlayContains } from './overlay-stack';

let sequence = 0;

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

/** Enhance once; the native select remains the form and change-event bridge. */
export function enhanceAstroSelect(root: HTMLElement): void {
  if (root.hasAttribute('data-select-enhanced')) return;
  const native = root.querySelector<HTMLSelectElement>('select');
  const fallback = root.querySelector<HTMLElement>('[data-select-fallback]');
  if (!native || !fallback) return;
  root.dataset.selectEnhanced = '';
  const id = native.id || `bb-astro-select-${++sequence}`;
  const labels = Array.from(native.labels);
  const label = root.dataset.label || 'Select an option';
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
  if (!trigger.hasAttribute('aria-labelledby') && !trigger.hasAttribute('aria-label') && labels.length) {
    trigger.setAttribute('aria-labelledby', labels.map((field, index) => {
      const text = field.querySelector<HTMLElement>('.bb-field__label') || field;
      text.id ||= `${id}-label-${index}`;
      return text.id;
    }).join(' '));
  }
  if (!trigger.hasAttribute('aria-label') && !trigger.hasAttribute('aria-labelledby')) trigger.setAttribute('aria-label', label);
  const valueLabel = document.createElement('span');
  valueLabel.className = 'bb-select__value';
  trigger.append(valueLabel);
  const chevron = fallback.querySelector('svg')?.cloneNode(true) as SVGElement | undefined;
  if (chevron) { chevron.setAttribute('class', 'bb-select__chevron'); trigger.append(chevron); }
  if (root.dataset.value !== undefined) native.value = root.dataset.value;
  native.id = `${id}-native`;
  native.classList.add('bb-select__native');
  native.tabIndex = -1;
  native.setAttribute('aria-hidden', 'true');
  root.append(native);
  fallback.replaceWith(trigger);

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

  function options(includeFallback = false): SelectOption[] {
    return Array.from(native!.options).filter((option) => includeFallback || !option.hasAttribute('data-select-fallback-option')).map((option) => ({
      value: option.value, label: option.label,
      disabled: option.disabled || (option.parentElement instanceof HTMLOptGroupElement && option.parentElement.disabled),
      group: option.dataset.group || (option.parentElement instanceof HTMLOptGroupElement ? option.parentElement.label : undefined),
      description: option.dataset.description, searchText: option.dataset.searchText, triggerLabel: option.dataset.triggerLabel,
    }));
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

  function activate(index: number) {
    active = index;
    if (!list) return;
    list.querySelectorAll<HTMLElement>('[role="option"]').forEach((item, i) => item.classList.toggle('active', i === active));
    const focus = input ?? list;
    if (active < 0) focus.removeAttribute('aria-activedescendant');
    else {
      const activeId = `${id}-option-${active}`;
      focus.setAttribute('aria-activedescendant', activeId);
      document.getElementById(activeId)?.scrollIntoView({ block: 'nearest' });
    }
  }

  function renderOptions() {
    if (!list) return;
    matches = filterSelectOptions(options(), input?.value ?? '');
    list.replaceChildren();
    let group: string | undefined;
    for (const [index, option] of matches.entries()) {
      if (option.group && option.group !== group) {
        const heading = document.createElement('li');
        heading.className = 'bb-select__group'; heading.setAttribute('role', 'presentation');
        heading.textContent = option.group; list.append(heading);
      }
      group = option.group;
      const item = document.createElement('li');
      item.id = `${id}-option-${index}`; item.className = 'bb-select__option';
      item.setAttribute('role', 'option'); item.setAttribute('aria-selected', String(option.value === native!.value));
      if (option.disabled) item.setAttribute('aria-disabled', 'true');
      const text = document.createElement('span');
      text.className = 'bb-select__label'; text.textContent = option.label; item.append(text);
      if (option.description) {
        const description = document.createElement('span');
        description.className = 'bb-select__description'; description.textContent = option.description; item.append(description);
      }
      item.addEventListener('pointerenter', () => { if (!option.disabled) activate(index); });
      item.addEventListener('click', () => pick(option)); list.append(item);
    }
    if (!matches.length) {
      const empty = document.createElement('li');
      empty.className = 'bb-select__empty'; empty.setAttribute('role', 'presentation');
      empty.textContent = root.dataset.emptyLabel ?? 'No matches'; list.append(empty);
    }
    if (clear) clear.hidden = !input?.value;
    const selected = matches.findIndex((option) => option.value === native!.value && !option.disabled);
    activate(selected >= 0 ? selected : nextEnabledOption(matches, -1, 'first'));
  }

  function keydown(event: KeyboardEvent) {
    // The search clear button keeps its native keyboard activation, including
    // Enter when the mobile focus trap tabs from the input to this button.
    if (event.target === clear && event.key !== 'Escape') return;
    if (event.key === 'Escape' && panel) {
      if (overlayId !== undefined && !isTopmost(overlayId)) return;
      event.preventDefault(); event.stopPropagation(); close();
    } else if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
      if (input && (event.key === 'Home' || event.key === 'End')) return;
      if (!panel) open();
      event.preventDefault();
      const direction = event.key === 'Home' ? 'first' : event.key === 'End' ? 'last' : event.key === 'ArrowDown' ? 1 : -1;
      activate(nextEnabledOption(matches, active, direction));
    } else if ((event.key === 'Enter' || (event.key === ' ' && !input)) && panel) {
      event.preventDefault(); if (matches[active]) pick(matches[active]);
    } else if (event.key === 'Tab' && panel && overlayId === undefined) close(true);
    else if (panel && !input && event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey) {
      event.preventDefault();
      const now = Date.now();
      typeahead = (now - typedAt > 700 ? '' : typeahead) + event.key;
      typedAt = now;
      const hits = matches.map((option, index) => ({ option, index })).filter(({ option }) => !option.disabled && normalizeSelectQuery(option.label).startsWith(normalizeSelectQuery(typeahead)));
      if (hits.length) activate(hits.find(({ index }) => index > active)?.index ?? hits[0].index);
    }
  }

  function outside(event: PointerEvent) {
    const target = event.target as Node | null;
    if (target && !(panel && overlayContains(panel, target)) && !trigger.contains(target)) close(false);
  }
  function moved(event: Event) {
    if (overlayId !== undefined || (panel && event.target instanceof Node && overlayContains(panel, event.target))) return;
    close(false);
  }

  function open() {
    if (trigger.disabled || panel) return;
    sync();
    if (trigger.disabled) return;
    panel = document.createElement('div'); panel.className = 'bb-picker-panel';
    panel.setAttribute('role', 'dialog'); panel.setAttribute('aria-label', label);
    panel.dataset.overlay = ''; panel.dataset.lenisPrevent = ''; panel.tabIndex = -1;
    panel.addEventListener('keydown', keydown);
    const mobile = window.matchMedia('(max-width: 639px)').matches;
    if (mobile) {
      shell = document.createElement('div'); shell.className = 'bb-picker-panel__shell'; shell.dataset.overlay = '';
      const scrim = document.createElement('button'); scrim.type = 'button';
      scrim.className = 'bb-picker-panel__scrim'; scrim.setAttribute('aria-label', label);
      scrim.addEventListener('click', () => close());
      panel.classList.add('bb-picker-panel--sheet'); panel.setAttribute('aria-modal', 'true');
      shell.append(scrim, panel); document.body.append(shell);
      overlayId = pushOverlay(); shell.style.zIndex = String(300 + overlayIndex(overlayId) * 10);
    } else {
      const rect = trigger.getBoundingClientRect();
      const width = Math.min(Math.max(rect.width, 260), window.innerWidth - 16);
      const height = Math.min(380, window.innerHeight - 16);
      panel.classList.add('bb-picker-panel--dropdown'); panel.style.width = `${width}px`; panel.style.maxHeight = `${height}px`;
      panel.style.left = `${Math.max(8, Math.min(rect.left, window.innerWidth - width - 8))}px`;
      panel.style.top = `${rect.bottom + 8 + height <= window.innerHeight ? rect.bottom + 8 : Math.max(8, rect.top - height - 8)}px`;
      document.body.append(panel);
    }
    unregisterAnchor = registerOverlayAnchor(shell ?? panel, trigger);
    if (searchable) {
      const search = document.createElement('label'); search.className = 'bb-search bb-input bb-input--fill';
      input = document.createElement('input'); input.type = 'search'; input.className = 'bb-search__input';
      input.placeholder = root.dataset.searchPlaceholder ?? 'Search…'; input.setAttribute('aria-label', input.placeholder);
      input.setAttribute('role', 'combobox'); input.setAttribute('aria-expanded', 'true'); input.setAttribute('aria-autocomplete', 'list');
      input.setAttribute('aria-controls', `${id}-list`); input.autocomplete = 'off'; input.addEventListener('input', renderOptions);
      clear = document.createElement('button'); clear.type = 'button'; clear.className = 'bb-search__clear';
      clear.setAttribute('aria-label', root.dataset.searchClearLabel ?? 'Clear search'); clear.append(searchGlyph(true));
      clear.addEventListener('click', () => { if (input) input.value = ''; renderOptions(); input?.focus(); });
      search.append(searchGlyph(), input, clear); panel.append(search);
    }
    list = document.createElement('ul'); list.id = `${id}-list`; list.className = 'bb-select__list';
    list.setAttribute('role', 'listbox'); list.setAttribute('aria-label', label); list.tabIndex = searchable ? -1 : 0;
    panel.append(list); renderOptions();
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
