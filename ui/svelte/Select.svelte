<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import { getUiI18n } from './i18n';
  // The native control bridges forms, validation, reset and real change events.
  // After mounting, selection uses the same portal and mobile sheet as pickers.
  import { onMount, tick } from 'svelte';
  import PickerPanel from './PickerPanel.svelte';
  import SearchInput from './SearchInput.svelte';
  import Scroller from './Scroller.svelte';
  import { createTypeahead, filterSelectOptions, nextEnabledOption, optionIndexAt, SELECT_NAVIGATION_KEYS, type SelectDirection, type SelectOption } from '../lib/select';
  import { MOBILE_QUERY } from '../lib/overlay-stack';
  import '../styles/elements/field.css';
  import '../styles/elements/input.css';
  import '../styles/elements/select.css';

  const i18n = getUiI18n();
  type Own = {
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
  };

  let {
    value = $bindable(''), options, searchable = false,
    searchPlaceholder = i18n.t('search.placeholder'), searchClearLabel = i18n.t('search.clear'),
    emptyLabel = i18n.t('search.empty'), placeholder = i18n.t('select.placeholder'), label,
    filterOptions = filterSelectOptions, invalid = false, fill = false,
    id, name, form, disabled = false, required = false,
    class: className = '', onchange, oninput, ...rest
  }: Own & Omit<HTMLAttributes<HTMLElement>, keyof Own> = $props();

  const uid = $props.id();
  const controlId = $derived(id || uid);
  const panelLabel = $derived(label || String(rest['aria-label'] || i18n.t('select.label')));
  const classes = $derived(['bb-input', 'bb-input--select', 'bb-select__trigger', fill ? 'bb-input--fill' : '', className].filter(Boolean).join(' '));
  const selected = $derived(options.find((option) => option.value === value));

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
  const typeahead = createTypeahead();
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
  function choose(event: KeyboardEvent) {
    event.preventDefault();
    if (results[active]) void pick(results[active]);
  }
  const keyHandlers: Record<string, (event: KeyboardEvent) => void> = {
    Escape: (event) => { event.preventDefault(); event.stopPropagation(); close(true); },
    // Restore normal page tab order from the portalled desktop popup.
    Tab: () => { if (!window.matchMedia(MOBILE_QUERY).matches) close(true); },
    Enter: choose,
    ' ': (event) => { if (!searchable) choose(event); },
  };
  function navigate(event: KeyboardEvent, direction: SelectDirection) {
    if (searchable && typeof direction === 'string') return;
    event.preventDefault();
    active = nextEnabledOption(results, active, direction);
    scrollActive();
  }
  function typeTo(event: KeyboardEvent) {
    event.preventDefault();
    const index = typeahead.find(results, active, event.key);
    if (index >= 0) active = index;
    scrollActive();
  }
  function onKey(event: KeyboardEvent) {
    const direction = SELECT_NAVIGATION_KEYS[event.key];
    if (direction !== undefined) navigate(event, direction);
    else if (!searchable && typeahead.accepts(event)) typeTo(event);
    else keyHandlers[event.key]?.(event);
  }
  function onTriggerKey(event: KeyboardEvent) {
    const direction = SELECT_NAVIGATION_KEYS[event.key];
    if (direction !== undefined) {
      event.preventDefault();
      void show().then(() => {
        if (typeof direction === 'string') active = nextEnabledOption(results, -1, direction);
        scrollActive();
      });
    } else if (typeahead.accepts(event)) {
      event.preventDefault();
      const index = typeahead.find(options, options.findIndex((option) => option.value === value), event.key);
      if (index >= 0) void pick(options[index]);
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
      {#if !selected}<option value="" selected disabled hidden>{placeholder}</option>{/if}
      {#each options as option (option.value)}
        <option value={option.value} disabled={option.disabled}>{option.triggerLabel || option.label}</option>
      {/each}
    </select>
    <svg class="bb-input__chevron" viewBox="0 0 24 24" aria-hidden="true"><path d="m6 9 6 6 6-6"></path></svg>
  </span>
  <button id={enhanced ? controlId : undefined} type="button" class={classes} hidden={!enhanced} {disabled}
    data-invalid={invalid ? '' : undefined} data-placeholder={!selected?.value ? '' : undefined}
    aria-haspopup="dialog" aria-expanded={open} aria-controls={open ? `${controlId}-list` : undefined}
    aria-labelledby={rest['aria-labelledby'] ? String(rest['aria-labelledby']) : label ? undefined : labelledBy} aria-label={label}
    {...rest}
    onclick={() => open ? close(true) : void show()}
    onkeydown={onTriggerKey} bind:this={btnEl}>
    <span class="bb-select__value">{selected?.triggerLabel || selected?.label || placeholder}</span>
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
