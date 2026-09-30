<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.

  import { getI18n, moduleDef, builtinDef } from '@bagel/kit';
  import { Chip, Heading, PickerOption, PickerPanel, SearchInput, Tag, Text, TextLink, rovingFocus } from '@bagel/ui/svelte';
  import { pinnedFor, sheetFor, type VariableChip, type VariableGroup, type VariableSurface } from '@bagel/kit/variables';
  import { webHref } from '@bagel/kit/site-links';
  import ModuleVariablePicker from '$lib/components/commands/ModuleVariablePicker.svelte';
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
  let btnEl = $state<HTMLElement>();
  let searchEl = $state<HTMLInputElement>();
  let searchValue = $state('');
  let query = $state('');
  let focusedId = $state<string | null>(null);
  const uid = $props.id();
  const descId = `${uid}-desc`;
  let hoverChip = $state<VariableChip | null>(null);
  let focusChip = $state<VariableChip | null>(null);

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

  function setAnchor(node: HTMLElement) {
    btnEl = node;
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

  const described = $derived(hoverChip ?? focusChip);
  const descriptionText = $derived(described ? (rowHint(described) ?? described.token) : '');

  const fullReferenceHref = $derived(`${webHref(locale, '/guides/variables')}${focusedId ? `#${focusedId}` : ''}`);
</script>

{#snippet rowTag(c: VariableChip)}
  {#if c.requires}
    <Tag tone="quiet">{t('commandEditor.requires', { module: requiresLabel(c.requires) })}</Tag>
    {#if moduleFlags[c.requires] === false}
      <Tag tone="danger">{t('commandEditor.off')}</Tag>
    {/if}
  {/if}
{/snippet}

{#snippet row(c: VariableChip)}
  <PickerOption as="li" onclick={() => pick(c)} onfocus={() => (focusedId = c.id ?? null)}>
    <span class="row">
      <Chip as="span" tone="muted">{c.token}</Chip>
      {#if rowHint(c)}<span class="row-hint"><Text as="span" size="xs" tone="muted">{rowHint(c)}</Text></span>{/if}
      {@render rowTag(c)}
    </span>
  </PickerOption>
{/snippet}

<div class="vp">
  <div class="vp-row" role="group" aria-label={t('commandEditor.insertVariable')}>
    <div class="vp-tokens">
      {#each pinnedChips as c (c.token)}
        <Chip
          tone="muted"
          aria-describedby={descId}
          onclick={() => insert(c.token)}
          onmouseenter={() => (hoverChip = c)}
          onmouseleave={() => (hoverChip = null)}
          onfocus={() => (focusChip = c)}
          onblur={() => (focusChip = null)}
        >
          <span class="vp-token">{c.token}</span>
        </Chip>
      {/each}
    </div>

    {#if isCustom || !sheetEmpty}
      <div class="vp-actions">
        {#if isCustom}
          <CounterPicker onInsert={insert} />
          <FetchSourcePicker defs={fetchDefs} keys={fetchKeys} onInsert={insert} onDefsChanged={onFetchDefsChanged} />
          <ModuleVariablePicker onInsert={insert} />
        {/if}

        {#if !sheetEmpty}
          <Chip tone="muted" aria-haspopup="dialog" aria-expanded={open} onclick={toggle} {@attach setAnchor}>
            {t('commandEditor.allVariables')}
          </Chip>
        {/if}
      </div>
    {/if}
    <div class="vp-desc"><Text size="xs" tone="muted" id={descId}>{descriptionText}</Text></div>
  </div>

  <PickerPanel {open} anchor={btnEl} label={t('commandEditor.allVariables')} width={380} maxHeight={480} onClose={closeSheet}>
    {#snippet children()}
      <div class="sheet" role="toolbar" tabindex="-1" aria-label={t('commandEditor.allVariables')} use:rovingFocus={{ selector: '.bb-picker-option__main' }}>
        <SearchInput
          fill
          debounceMs={120}
          bind:value={searchValue}
          bind:element={searchEl}
          onValueChange={(v) => (query = v)}
          placeholder={t('commandEditor.allVariables')}
        />

        <div class="sheet-scroll">
          {#if replyChips.length > 0}
            <section class="sec">
              <Heading level={3} variant="label">{t('commandEditor.thisReply')}</Heading>
              {#if filteredReply.length === 0}
                <Text size="xs" tone="muted">{t('commandEditor.noneHere')}</Text>
              {:else}
                <ul class="rows">{#each filteredReply as c (c.token)}{@render row(c)}{/each}</ul>
              {/if}
            </section>
          {/if}

          {#each groupRows as { group, chips } (group)}
            <section class="sec">
              <Heading level={3} variant="label">{t(`vars.group.${group}.label`)}</Heading>
              {#if chips.length === 0}
                <Text size="xs" tone="muted">{t('commandEditor.noneHere')}</Text>
              {:else}
                <ul class="rows">{#each chips as c (c.token)}{@render row(c)}{/each}</ul>
              {/if}
            </section>
          {/each}
        </div>

        <div class="sheet-foot">
          <Text size="xs" tone="muted">{t('commandEditor.fallbackHint')}</Text>
          <Text size="xs"><TextLink variant="inline" href={fullReferenceHref} external>{t('commandEditor.fullReference')}</TextLink></Text>
        </div>
      </div>
    {/snippet}
  </PickerPanel>
</div>

<style>
  .vp { display: contents; }

  .vp-row {
    display: grid;
    min-width: 0;
    gap: 6px;
    margin-top: 8px;
  }

  .vp-tokens,
  .vp-actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    min-width: 0;
    gap: 6px;
  }

  .vp-desc { min-height: 18px; }

  .vp-token { min-width: 0; overflow-wrap: anywhere; text-align: left; }

  .sheet { display: flex; flex-direction: column; gap: 10px; min-height: 0; }

  .sheet-scroll {
    display: flex;
    flex-direction: column;
    gap: 14px;
    overflow-y: auto;
    max-height: 340px;
    padding-right: 2px;
  }

  .sec { display: flex; flex-direction: column; gap: 6px; }

  .rows { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; }
  .row { flex: 1; min-width: 0; display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
  .row-hint { flex: 1; min-width: 80px; }

  .sheet-foot {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding-top: 8px;
    border-top: 1px solid var(--bb-border);
  }
</style>
