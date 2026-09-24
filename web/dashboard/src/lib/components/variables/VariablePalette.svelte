<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.

  import { getI18n, Chip, PickerPanel, SearchInput, Tag, moduleDef, builtinDef } from '@bagel/kit';
  import { pinnedFor, sheetFor, type VariableChip, type VariableGroup, type VariableSurface } from '@bagel/kit/variables';
  import { webHref } from '@bagel/kit/site-links';
  import CounterPicker from '$lib/components/counters/CounterPicker.svelte';
  import FetchSourcePicker, { type SourceDef } from '$lib/components/commands/fetches/FetchSourcePicker.svelte';

  const { t, locale } = getI18n();

  let {
    surface,
    moduleFlags = {},
    insert,
    fetchDefs = [],
    fetchKeys = [],
    onFetchDefsChanged
  }: {
    surface: VariableSurface;
    moduleFlags?: Record<string, boolean>;
    insert: (token: string) => void;
    fetchDefs?: SourceDef[];
    fetchKeys?: { label: string }[];
    onFetchDefsChanged?: (defs: SourceDef[]) => void;
  } = $props();

  const isCustom = $derived(surface === 'custom');

  const GROUPS: readonly VariableGroup[] = ['who', 'typed', 'stream', 'fun', 'data'];

  const pinnedChips = $derived(pinnedFor(surface));

  const sheetChips = $derived(sheetFor(surface));
  const replyChips = $derived(sheetChips.filter((c) => c.replyOnly));
  const manifestChips = $derived(sheetChips.filter((c) => !c.replyOnly));
  const structuralGroups = $derived(GROUPS.filter((g) => manifestChips.some((c) => c.group === g)));
  const sheetEmpty = $derived(sheetChips.length === 0);

  let open = $state(false);
  let btnEl = $state<HTMLButtonElement>();
  let listEl = $state<HTMLDivElement>();
  let searchEl = $state<HTMLInputElement>();
  let searchValue = $state('');
  let query = $state('');
  let focusedId = $state<string | null>(null);

  $effect(() => {
    if (!open) return;
    queueMicrotask(() => searchEl?.focus());
  });

  function toggle() {
    open = !open;
    if (open) {
      searchValue = '';
      query = '';
      focusedId = null;
    }
  }

  function closeSheet() {
    open = false;
    btnEl?.focus();
  }

  function pick(chip: VariableChip) {
    open = false;
    insert(chip.token);
  }

  function requiresLabel(id: string): string {
    return moduleDef(id)?.label ?? builtinDef(id)?.label ?? id;
  }

  function rowHint(c: VariableChip): string | undefined {
    if (c.hintKey) return t(c.hintKey);
    return c.sample ? `${c.token} → ${c.sample}` : undefined;
  }

  function matches(c: VariableChip, q: string): boolean {
    if (!q) return true;
    const needle = q.toLowerCase();
    if (c.token.toLowerCase().includes(needle)) return true;
    const hint = rowHint(c);
    return hint ? hint.toLowerCase().includes(needle) : false;
  }

  const filteredReply = $derived(replyChips.filter((c) => matches(c, query)));
  const groupRows = $derived(
    structuralGroups.map((group) => ({
      group,
      chips: manifestChips.filter((c) => c.group === group && matches(c, query))
    }))
  );

  function rowEls(): HTMLButtonElement[] {
    return Array.from(listEl?.querySelectorAll<HTMLButtonElement>('button.row') ?? []);
  }

  function focusRowAt(index: number) {
    const rows = rowEls();
    if (rows.length === 0) return;
    const i = index < 0 ? rows.length + index : index;
    rows[Math.max(0, Math.min(rows.length - 1, i))]?.focus();
  }

  function moveFocus(step: 1 | -1) {
    const rows = rowEls();
    if (rows.length === 0) return;
    const cur = rows.indexOf(document.activeElement as HTMLButtonElement);
    focusRowAt(cur === -1 ? 0 : Math.min(rows.length - 1, Math.max(0, cur + step)));
  }

  const ROW_KEYS: Readonly<Record<string, () => void>> = {
    ArrowDown: () => moveFocus(1),
    ArrowUp: () => moveFocus(-1),
    Home: () => focusRowAt(0),
    End: () => focusRowAt(-1)
  };

  function onSheetKeydown(e: KeyboardEvent) {
    if (document.activeElement instanceof HTMLInputElement) {
      if (e.key !== 'ArrowDown') return;
      e.preventDefault();
      focusRowAt(0);
      return;
    }
    const action = ROW_KEYS[e.key];
    if (!action) return;
    e.preventDefault();
    action();
  }

  const fullReferenceHref = $derived(`${webHref(locale, '/guides/variables')}${focusedId ? `#${focusedId}` : ''}`);
</script>

{#snippet rowTag(c: VariableChip)}
  {#if c.requires}
    <Tag tone="quiet">{t('commandEditor.requires', { module: requiresLabel(c.requires) })}</Tag>
    {#if moduleFlags[c.requires] === false}
      <Tag tone="error">{t('commandEditor.off')}</Tag>
    {/if}
  {/if}
{/snippet}

{#snippet row(c: VariableChip)}
  <li>
    <button
      type="button"
      class="row"
      onclick={() => pick(c)}
      onfocus={() => (focusedId = c.id ?? null)}
    >
      <span class="bb-chip bb-chip--muted row-token">{c.token}</span>
      {#if rowHint(c)}<span class="row-hint">{rowHint(c)}</span>{/if}
      {@render rowTag(c)}
    </button>
  </li>
{/snippet}

<div class="vp">
  <div class="vp-row">
    <div class="vp-scroll">
      {#each pinnedChips as c (c.token)}
        <Chip tone="muted" title={rowHint(c) ?? c.token} onclick={() => insert(c.token)}>{c.token}</Chip>
      {/each}

      {#if isCustom}
        <span class="vp-sep" aria-hidden="true"></span>
        <CounterPicker onInsert={insert} />
        <FetchSourcePicker defs={fetchDefs} keys={fetchKeys} onInsert={insert} onDefsChanged={onFetchDefsChanged} />
      {/if}
    </div>

    {#if !sheetEmpty}
      <button
        type="button"
        class="bb-chip bb-chip--muted vp-all"
        aria-haspopup="dialog"
        aria-expanded={open}
        onclick={toggle}
        bind:this={btnEl}
      >
        {t('commandEditor.allVariables')}
      </button>
    {/if}
  </div>

  <PickerPanel {open} anchor={btnEl} label={t('commandEditor.allVariables')} width={380} maxHeight={480} onClose={closeSheet}>
    {#snippet children()}
      <div class="sheet" role="toolbar" tabindex="-1" aria-label={t('commandEditor.allVariables')} onkeydown={onSheetKeydown}>
        <SearchInput
          fill
          debounceMs={120}
          bind:value={searchValue}
          bind:element={searchEl}
          oninput={(v) => (query = v)}
          placeholder={t('commandEditor.allVariables')}
        />

        <div class="sheet-scroll" bind:this={listEl}>
          {#if replyChips.length > 0}
            <section class="sec">
              <h3 class="sec-head">{t('commandEditor.thisReply')}</h3>
              {#if filteredReply.length === 0}
                <p class="sec-empty">{t('commandEditor.noneHere')}</p>
              {:else}
                <ul class="rows">{#each filteredReply as c (c.token)}{@render row(c)}{/each}</ul>
              {/if}
            </section>
          {/if}

          {#each groupRows as { group, chips } (group)}
            <section class="sec">
              <h3 class="sec-head">{t(`vars.group.${group}.label`)}</h3>
              {#if chips.length === 0}
                <p class="sec-empty">{t('commandEditor.noneHere')}</p>
              {:else}
                <ul class="rows">{#each chips as c (c.token)}{@render row(c)}{/each}</ul>
              {/if}
            </section>
          {/each}
        </div>

        <div class="sheet-foot">
          <p class="fallback">{t('commandEditor.fallbackHint')}</p>
          <a class="full-ref" href={fullReferenceHref} target="_blank" rel="noopener">{t('commandEditor.fullReference')}</a>
        </div>
      </div>
    {/snippet}
  </PickerPanel>
</div>

<style>
  .vp { display: contents; }

  .vp-row {
    display: flex;
    align-items: center;
    gap: 6px;
    min-height: calc(11.5px + 2 * 7px + 2 * 1px);
    margin-top: 8px;
  }

  .vp-scroll {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: 1 1 auto;
    min-width: 0;
    overflow-x: auto;
    overflow-y: hidden;
    scroll-snap-type: x proximity;
  }
  .vp-scroll :global(> *) {
    flex: none;
    scroll-snap-align: start;
  }
  .vp-scroll :global(.bb-chip) { white-space: nowrap; }

  .vp-all { flex: none; }

  .vp-sep {
    width: 1px;
    align-self: stretch;
    min-height: 16px;
    margin: 0 2px;
    background: var(--rule, var(--bb-border));
    flex: none;
  }

  .sheet { display: flex; flex-direction: column; gap: 10px; min-height: 0; }

  .sheet-scroll {
    display: flex;
    flex-direction: column;
    gap: 14px;
    overflow-y: auto;
    max-height: 340px;
    padding-right: 2px;
  }

  .sec-head {
    margin: 0 0 6px;
    font-family: var(--bb-font-body);
    font-size: 10.5px;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .sec-empty {
    margin: 0;
    font-family: var(--bb-font-body);
    font-size: 12px;
    font-style: italic;
    color: var(--bb-muted);
  }

  .rows { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; }
  .row {
    width: 100%;
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    padding: 6px 8px;
    background: transparent;
    border: none;
    border-radius: var(--bb-radius-sm);
    cursor: pointer;
    text-align: left;
  }
  .row:hover, .row:focus-visible { background: var(--glass-fill-2); }
  .row-token { pointer-events: none; }
  .row-hint {
    flex: 1;
    min-width: 80px;
    font-family: var(--bb-font-body);
    font-size: 11.5px;
    color: var(--bb-muted);
  }

  .sheet-foot {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding-top: 8px;
    border-top: 1px solid var(--rule, var(--bb-border));
  }
  .fallback { margin: 0; font-family: var(--bb-font-body); font-size: 11.5px; line-height: 1.4; color: var(--bb-muted); }
  .full-ref {
    align-self: flex-start;
    font-family: var(--bb-font-body);
    font-size: 11.5px;
    color: var(--bb-green-glow, #52b788);
    text-decoration: underline;
  }
</style>
