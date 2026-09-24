<script lang="ts">
  import { Button } from '@bagel/kit';
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Icon, ManagementRow, getI18n, type CounterDef, type CounterScope } from '@bagel/kit';
  import { formatCounterValue } from '@bagel/kit/validation';

  const { t } = getI18n();

  let {
    counter,
    index = undefined as number | undefined,
    expanded = false,
    onExpand,
    onDelete
  }: {
    counter: CounterDef;
    index?: number;
    expanded?: boolean;
    onExpand: () => void;
    onDelete: () => void;
  } = $props();

  const c = $derived(counter);
  const idx = $derived(index !== undefined ? String(index).padStart(2, '0') : '');
  const isChannel = $derived(c.scope === 'channel');

  const SCOPE_KEY: Record<CounterScope, string> = {
    channel: 'counters.tagChannel',
    viewer: 'counters.tagViewer',
    command: 'counters.tagCommand',
    viewer_command: 'counters.tagViewerCommand'
  };
  const scopeLabel = $derived(t(SCOPE_KEY[c.scope]));

  const perScopeNote = $derived(c.scope === 'command' ? t('counters.perCommandNote') : t('counters.perUserNote'));
</script>

<ManagementRow selected={expanded} {expanded} controls="counter-inspector" onselect={onExpand}>
  {#snippet primary()}
    <span class="prow">
      {#if idx}<span class="idx" aria-hidden="true">{idx}</span>{/if}
      <span class="name">
        <span class="c-name">{c.name}</span>
        <span class="c-tag bb-tag bb-tag--bare">{scopeLabel}</span>
      </span>
      <span class="meta">
        {#if isChannel}
          <span class="m-val">
            <span class="bb-sr-only">{t('counters.colValue')} </span>{formatCounterValue(c.value)}
          </span>
        {:else}
          <span class="m-note">{perScopeNote}</span>
        {/if}
      </span>
    </span>
  {/snippet}
  {#snippet actions()}
    <Button variant="icon" size="sm" class="delete-action" danger type="button" aria-label={t('counters.deleteAria', { name: c.name })} onclick={onDelete} ><Icon name="trash" size={15} /></Button>
  {/snippet}
</ManagementRow>

<style>
  .prow {
    display: grid;
    grid-template-columns: 28px minmax(0, 1fr) auto;
    align-items: center;
    gap: 14px;
  }
  .idx { font-family: var(--bb-font-mono); font-size: 10px; color: var(--bb-muted); opacity: 0.55; }

  .name { display: inline-flex; align-items: center; gap: 10px; min-width: 0; }
  .c-name {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 14px;
    color: var(--bb-white);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  .c-tag { flex: none; }

  .meta { display: inline-flex; align-items: center; justify-content: flex-end; }
  .m-val {
    font-family: var(--bb-font-mono);
    font-size: 13.5px;
    color: var(--bb-tan-light);
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }
  .m-note { font-family: var(--bb-font-body); font-size: 11px; color: var(--bb-muted); white-space: nowrap; }

  :global(.delete-action) { width: 32px; height: 32px; min-height: 32px; }

  @media (max-width: 760px) {
    .prow {
      grid-template-columns: minmax(0, 1fr);
      grid-template-areas:
        'name'
        'meta';
      row-gap: 4px;
    }
    .idx { display: none; }
    .name { grid-area: name; }
    .meta { grid-area: meta; justify-content: flex-start; }
    :global(.delete-action) { min-width: 44px; min-height: 44px; }
  }
</style>
