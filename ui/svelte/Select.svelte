<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  // The native control bridges forms, validation, reset and real change events.
  // After mounting, selection uses the same portal and mobile sheet as pickers.
  import { onMount, tick } from 'svelte';
  import PickerPanel from './PickerPanel.svelte';
  import SearchInput from './SearchInput.svelte';
  import Scroller from './Scroller.svelte';
  import { filterSelectOptions, nextEnabledOption, normalizeSelectQuery, optionIndexAt, type SelectOption } from '../lib/select';
  import '../styles/elements/field.css';
  import '../styles/elements/input.css';
  import '../styles/elements/select.css';

  let {
    value = $bindable(''), options, searchable = false,
    searchPlaceholder = 'Search…', searchClearLabel = 'Clear search',
    emptyLabel = 'No matches', placeholder = 'Select…', label,
    filterOptions = filterSelectOptions, invalid = false, fill = false,
    id, name, form, disabled = false, required = false,
    class: className = '', onchange, oninput, ...rest
  }: {
    value?: string;
    options: readonly SelectOption[];
    /** Show the search field. Small lists can omit it. */
    searchable?: boolean;
    searchPlaceholder?: string;
    searchClearLabel?: string;
    emptyLabel?: string;
    placeholder?: string;
    label?: string;
    /** Domain-specific ranking or accepted aliases. */
    filterOptions?: (options: readonly SelectOption[], query: string) => SelectOption[];
    invalid?: boolean;
    fill?: boolean;
    id?: string;
    name?: string;
    form?: string;
    disabled?: boolean;
    required?: boolean;
    class?: string;
    onchange?: (event: Event & { currentTarget: HTMLSelectElement }) => void;
    oninput?: (event: Event & { currentTarget: HTMLSelectElement }) => void;
    [key: string]: unknown;
  } = $props();

  const uid = $props.id();
  const controlId = $derived(id || uid);
  const panelLabel = $derived(label || String(rest['aria-label'] || 'Select an option'));
  const classes = $derived(['bb-input', 'bb-input--select', 'bb-select__trigger', fill ? 'bb-input--fill' : '', className].filter(Boolean).join(' '));
  const selected = $derived(options.find((option) => option.value === value));
  const nativeOptions = $derived(selected ? options : [{ value, label: value || placeholder }, ...options]);

  let enhanced = $state(false);
  let open = $state(false);
  let query = $state('');
  const results = $derived(filterOptions(options, searchable ? query : ''));
  let active = $state(-1);
  let nativeEl = $state<HTMLSelectElement>();
  let btnEl = $state<HTMLButtonElement>();
  let inputEl = $state<HTMLInputElement>();
  let listEl = $state<HTMLUListElement>();
  let labelledBy = $state<string>();
  let typeahead = '';
  let typedAt = 0;
  let pointer: { x: number; y: number } | undefined;

  onMount(() => {
    // Reuse Field's visible label, excluding the selected value from the name.
    const labels = Array.from(nativeEl?.labels || []);
    labelledBy = labels.map((field, i) => {
      const text = field.querySelector<HTMLElement>('.bb-field__label') || field;
      text.id ||= `${controlId}-label-${i}`;
      return text.id;
    }).join(' ') || undefined;
    enhanced = true;
  });

  $effect(() => {
    const index = results.findIndex((option) => option.value === value && !option.disabled);
    active = index >= 0 ? index : nextEnabledOption(results, -1, 'first');
  });
  $effect(() => { if (disabled) open = false; });

  function close(restore = false) {
    open = false;
    if (restore) {
      btnEl?.focus();
      // A mobile sheet keeps its trigger inert until overlay teardown. Retry
      // after the flush only if focus was lost, preserving outside-click focus.
      void tick().then(() => {
        if (!open && document.activeElement === document.body) btnEl?.focus();
      });
    }
  }
  async function show() {
    if (disabled) return;
    query = '';
    open = true;
    await tick();
    if (searchable) inputEl?.focus();
    else listEl?.focus();
    scrollActive();
  }
  function scrollActive() {
    pointer = undefined;
    document.getElementById(`${controlId}-option-${active}`)?.scrollIntoView({ block: 'nearest' });
  }
  function hoverAt(target: EventTarget | null) {
    const index = listEl ? optionIndexAt(listEl, target) : -1;
    if (index >= 0 && !results[index].disabled) active = index;
  }
  function trackPointer(event: PointerEvent) {
    pointer = event.pointerType === 'touch' ? undefined : { x: event.clientX, y: event.clientY };
    hoverAt(event.target);
  }
  function followPointer() {
    if (pointer) hoverAt(document.elementFromPoint(pointer.x, pointer.y));
  }
  async function pick(option: SelectOption) {
    if (disabled || option.disabled) return;
    const changed = value !== option.value;
    value = option.value;
    close(true);
    // Flush bindings BEFORE a consumer's change handler calls requestSubmit.
    await tick();
    if (changed && nativeEl) {
      nativeEl.dispatchEvent(new Event('input', { bubbles: true }));
      nativeEl.dispatchEvent(new Event('change', { bubbles: true }));
    }
  }
  function onKey(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.preventDefault(); event.stopPropagation(); close(true);
    } else if (event.key === 'Tab') {
      // Restore normal page tab order from the portalled desktop popup.
      if (!window.matchMedia('(max-width: 639px)').matches) close(true);
    } else if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
      if (searchable && (event.key === 'Home' || event.key === 'End')) return;
      event.preventDefault();
      active = nextEnabledOption(results, active, event.key === 'Home' ? 'first' : event.key === 'End' ? 'last' : event.key === 'ArrowDown' ? 1 : -1);
      scrollActive();
    } else if (event.key === 'Enter' || (!searchable && event.key === ' ')) {
      event.preventDefault();
      if (results[active]) void pick(results[active]);
    } else if (!searchable && event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey) {
      event.preventDefault();
      const now = Date.now();
      typeahead = (now - typedAt > 700 ? '' : typeahead) + event.key;
      typedAt = now;
      const matches = results.map((option, index) => ({ option, index })).filter(({ option }) => !option.disabled && normalizeSelectQuery(option.label).startsWith(normalizeSelectQuery(typeahead)));
      if (matches.length) active = matches.find(({ index }) => index > active)?.index ?? matches[0].index;
      scrollActive();
    }
  }
</script>

<span class="bb-select" class:bb-select--fill={fill} data-enhanced={enhanced ? '' : undefined}>
  <span class="bb-input bb-input--select bb-select__fallback" class:bb-input--fill={fill} data-invalid={invalid ? '' : undefined}>
    <select id={enhanced ? `${controlId}-native` : controlId} {name} {form} {disabled} {required}
      bind:value bind:this={nativeEl} aria-hidden={enhanced ? 'true' : undefined} tabindex={enhanced ? -1 : undefined}
      {...rest} {onchange} {oninput}
      onfocus={() => { if (enhanced) btnEl?.focus(); }}
      oninvalid={(event) => { if (enhanced) { event.preventDefault(); btnEl?.focus(); void show(); } }}>
      {#each nativeOptions as option (option.value)}
        <option value={option.value} disabled={option.disabled}>{option.triggerLabel || option.label}</option>
      {/each}
    </select>
    <svg class="bb-input__chevron" viewBox="0 0 24 24" aria-hidden="true"><path d="m6 9 6 6 6-6"></path></svg>
  </span>
  <button id={enhanced ? controlId : undefined} type="button" class={classes} hidden={!enhanced} {disabled}
    data-invalid={invalid ? '' : undefined} data-placeholder={!value ? '' : undefined}
    aria-haspopup="dialog" aria-expanded={open} aria-controls={open ? `${controlId}-list` : undefined}
    aria-labelledby={rest['aria-labelledby'] ? String(rest['aria-labelledby']) : label ? undefined : labelledBy} aria-label={label}
    {...rest}
    onclick={() => open ? close(true) : void show()}
    onkeydown={(event) => {
      if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
        event.preventDefault();
        void show().then(() => {
          if (event.key === 'Home' || event.key === 'End') active = nextEnabledOption(results, -1, event.key === 'Home' ? 'first' : 'last');
          scrollActive();
        });
      }
    }} bind:this={btnEl}>
    <span class="bb-select__value">{selected?.triggerLabel || selected?.label || value || placeholder}</span>
    <svg class="bb-select__chevron" viewBox="0 0 24 24" aria-hidden="true"><path d="m6 9 6 6 6-6"></path></svg>
  </button>
</span>

<PickerPanel {open} anchor={btnEl} label={panelLabel} placement="below" maxHeight={380} onClose={() => close(true)}>
  {#if searchable}
    <SearchInput fill bind:value={query} bind:element={inputEl} placeholder={searchPlaceholder} clearLabel={searchClearLabel}
      aria-label={searchPlaceholder} role="combobox" aria-expanded="true" aria-controls="{controlId}-list"
      aria-activedescendant={active >= 0 ? `${controlId}-option-${active}` : undefined}
      aria-autocomplete="list" autocomplete="off" spellcheck="false" onkeydown={onKey} />
  {/if}
  <Scroller fill onscroll={followPointer}>
    <ul class="bb-select__list" id="{controlId}-list" role="listbox" aria-label={panelLabel}
      tabindex={searchable ? -1 : 0} aria-activedescendant={!searchable && active >= 0 ? `${controlId}-option-${active}` : undefined}
      onkeydown={onKey} onpointermove={trackPointer} onpointerleave={() => (pointer = undefined)} bind:this={listEl}>
      {#each results as option, i (option.value)}
        {#if option.group && (i === 0 || option.group !== results[i - 1].group)}
          <li class="bb-select__group" role="presentation">{option.group}</li>
        {/if}
        <li id="{controlId}-option-{i}" data-index={i} role="option" tabindex="-1" aria-selected={option.value === value}
          aria-disabled={option.disabled || undefined} class="bb-select__option" class:active={i === active}
          onclick={() => void pick(option)}
          onkeydown={(event) => { event.stopPropagation(); onKey(event); }}>
          <span class="bb-select__label">{option.label}</span>
          {#if option.description}<span class="bb-select__description">{option.description}</span>{/if}
        </li>
      {/each}
    </ul>
    {#if !results.length}<p class="bb-select__empty" role="status">{emptyLabel}</p>{/if}
  </Scroller>
</PickerPanel>
