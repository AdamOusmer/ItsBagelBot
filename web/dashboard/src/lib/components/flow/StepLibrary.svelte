<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { tick } from 'svelte';
  import Icon from './FlowIcon.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import IconTile from '@bagel/ui/svelte/IconTile.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import SegmentedControl from '@bagel/ui/svelte/SegmentedControl.svelte';
  import { getI18n } from '@bagel/kit';
  import type { RequirementTrigger } from '@bagel/kit/automations';
  import { groupLabel, requirementText, stepLabel, stepRequirementFor, stepSummary } from '#lib/flow/copy';
  import type { FlowKind, NodeSpec } from '#lib/flow/model';
  import { stepSections } from '#lib/flow/search';
  import { stepFocus } from '#lib/flow/keyboard';

  export type LibraryMode = FlowKind | 'search';

  let {
    mode,
    trigger,
    selectedLabel,
    focusToken = 0,
    onModeChange,
    onAdd,
    onClose
  }: {
    mode: LibraryMode;
    trigger: RequirementTrigger;
    selectedLabel?: string;
    focusToken?: number;
    onModeChange: (mode: FlowKind) => void;
    onAdd: (type: string) => void;
    onClose: () => void;
  } = $props();

  const { t } = getI18n();

  const KIND_COPY = {
    trigger: { label: 'flows.board.when', hint: 'flows.board.whenHint' },
    condition: { label: 'flows.board.onlyIf', hint: 'flows.board.onlyIfHint' },
    reply: { label: 'flows.board.do', hint: 'flows.board.doHint' }
  } as const;
  const KINDS: readonly FlowKind[] = ['trigger', 'condition', 'reply'];
  const segments = $derived(KINDS.map((value) => ({ value, label: t(KIND_COPY[value].label) })));
  const kind = $derived(mode === 'search' ? 'trigger' : mode);
  const chooseKind = (value: string) => {
    const next = KINDS.find((kind) => kind === value);
    if (next) onModeChange(next);
  };
  const TINTS: Record<FlowKind, string> = {
    trigger: 'var(--bb-green-glow)',
    condition: 'var(--bb-tan)',
    reply: 'var(--bb-muted-light)'
  };

  let query = $state('');
  let searchEl = $state<HTMLInputElement>();
  let root = $state<HTMLElement>();

  $effect(() => {
    if (mode === 'search') void tick().then(() => searchEl?.focus());
    query = '';
  });

  $effect(() => {
    if (focusToken > 0) void tick().then(() => (searchEl ?? root?.querySelector<HTMLElement>('.row'))?.focus());
  });

  function onKeyDown(event: KeyboardEvent) {
    if (event.defaultPrevented) return;
    const items = Array.from(root?.querySelectorAll<HTMLElement>('input, .row') ?? []);
    if (items.includes(event.target as HTMLElement)) stepFocus(event, items);
  }

  const searching = $derived(Boolean(query.trim()));
  const sections = $derived(stepSections(t, query, searching ? undefined : [kind]));

  function onSearchKeyDown(event: KeyboardEvent) {
    if (event.key !== 'Enter' || event.isComposing) return;
    const first = sections[0]?.specs[0];
    if (!first) return;
    event.preventDefault();
    event.stopPropagation();
    onAdd(first.type);
  }

  function onDragStart(e: DragEvent, spec: NodeSpec) {
    if (!e.dataTransfer) return;
    e.dataTransfer.setData('application/x-flow-node', spec.type);
    e.dataTransfer.effectAllowed = 'copy';
  }
</script>

<div class="library" role="presentation" bind:this={root} onkeydown={onKeyDown}>
  <header class="head">
    <div class="title-row">
      <h2 class="title bb-fit-box"><bb-fit>{t('flows.board.addStep')}</bb-fit></h2>
      <IconButton label={t('flows.board.closeLibrary')} size="sm" onclick={onClose}><Icon name="x" size={14} /></IconButton>
    </div>
    <div class="segments" class:faded={searching} inert={searching}>
      <SegmentedControl class="bb-tabs--even" options={segments} value={kind} onValueChange={chooseKind} label={t('flows.board.stepTools')} />
    </div>
    <p class="header-line bb-fit-box" class:faded={searching} aria-hidden={searching}><bb-fit>{t(KIND_COPY[kind].hint)}</bb-fit></p>
    <p class="header-line placement bb-fit-box" class:faded={!selectedLabel} aria-hidden={!selectedLabel}>
      <bb-fit>{selectedLabel ? t('flows.board.addsAfter', { step: selectedLabel }) : ''}</bb-fit>
    </p>
    <SearchInput bind:value={query} bind:element={searchEl} placeholder={t('flows.board.searchAll')} onkeydown={onSearchKeyDown} fill tall />
  </header>

  <Scroller fill padding="0 8px 16px" smooth>
    {#each sections as section (section.key)}
      <div class="group">
        {#if section.label}
          <h3 class="group-title bb-fit-box"><bb-fit>{section.label}</bb-fit></h3>
        {/if}
        {#each section.specs as spec (spec.type)}
          {@const requirement = stepRequirementFor(spec, trigger)}
          <button
            class="row"
            type="button"
            draggable="true"
            ondragstart={(e) => onDragStart(e, spec)}
            ondragend={onClose}
            onclick={() => onAdd(spec.type)}
          >
            <IconTile size="sm" style="--tile: 32px; color: {TINTS[spec.kind]}"><Icon name={spec.icon} size={16} /></IconTile>
            <span class="copy">
              <span class="name bb-fit-box"><bb-fit>{stepLabel(t, spec)}</bb-fit></span>
              <span class="details">
                <span class="summary bb-fit-box"><bb-fit>{stepSummary(t, spec)}</bb-fit></span>
                {#if searching}
                  <span class="group-tag bb-fit-box"><bb-fit>{groupLabel(t, spec.group)}</bb-fit></span>
                {/if}
              </span>
            </span>
            {#if requirement}
              <span class="requirement">
                <Chip as="span" tone="muted" class="bb-fit-box" style="display: flex; padding: 4px 6px"><bb-fit>{requirementText(t, requirement)}</bb-fit></Chip>
              </span>
            {/if}
          </button>
        {/each}
      </div>
    {:else}
      <p class="empty">{t('flows.board.noStepsMatchSearch')}</p>
    {/each}
  </Scroller>
</div>

<style>
  .library {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }
  .head {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 4px;
    flex: none;
    padding: 16px 12px 12px 16px;
  }
  .title-row {
    display: flex;
    align-items: center;
    gap: 8px;
    height: 44px;
  }
  .title {
    flex: 1;
    min-width: 0;
    margin: 0;
    font-family: var(--bb-font-body);
    font-size: 17px;
    font-weight: 600;
    color: var(--bb-white);
  }
  .segments,
  .header-line {
    transition: opacity 0.15s ease;
  }
  .segments {
    height: 44px;
    display: grid;
    align-items: center;
  }
  .header-line {
    height: 20px;
    line-height: 20px;
    margin: 0;
    font-size: 14px;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .placement,
  .group-tag {
    color: var(--bb-muted);
  }
  .faded { opacity: 0; }
  .group {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding-bottom: 12px;
  }
  .group-title {
    margin: 8px 8px 6px;
    height: 20px;
    font-family: var(--bb-font-body);
    font-size: 14px;
    font-weight: 600;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 12px;
    height: 60px;
    padding: 10px 8px;
    border: 0;
    border-radius: var(--bb-radius-sm);
    background: transparent;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: grab;
    transition: background 0.15s ease;
  }
  .row:hover,
  .row:focus-visible {
    background: rgba(var(--bb-white-rgb), 0.06);
  }
  .row:focus-visible {
    outline: 2px solid var(--bb-green-glow);
    outline-offset: -2px;
  }
  .row:active {
    cursor: grabbing;
  }
  .copy {
    display: flex;
    flex-direction: column;
    gap: 2px;
    flex: 1;
    min-width: 0;
  }
  .name {
    font-size: 15px;
    line-height: 19px;
    color: var(--bb-white);
  }
  .summary {
    min-width: 0;
    font-size: 14px;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .details {
    display: flex;
    gap: 6px;
    height: 18px;
    line-height: 18px;
  }
  .group-tag {
    flex: 0 0 36%;
    font-size: 12px;
  }
  .requirement { flex: 0 0 30%; }
  .empty {
    margin: 0;
    padding: 16px 8px;
    font-size: 14px;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
</style>
