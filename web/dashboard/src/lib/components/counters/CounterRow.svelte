<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { Icon, IconButton, ManagementRow, Tag, Text, getI18n, type CounterDef, type CounterScope } from '@bagel/kit';
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
        <Tag tone="bare" class="c-tag">{scopeLabel}</Tag>
      </span>
      <span class="meta">
        {#if isChannel}
          <span class="m-val">
            <span class="bb-sr-only">{t('counters.colValue')} </span>{formatCounterValue(c.value)}
          </span>
        {:else}
          <Text as="span" size="xs" tone="muted">{perScopeNote}</Text>
        {/if}
      </span>
    </span>
  {/snippet}
  {#snippet actions()}
    <span class="del">
      <IconButton size="sm" danger label={t('counters.deleteAria', { name: c.name })} onclick={onDelete}><Icon name="trash" size={15} /></IconButton>
    </span>
  {/snippet}
</ManagementRow>

<style>
  .prow {
    display: grid;
    grid-template-columns: 28px minmax(0, 1fr) auto;
    align-items: center;
    gap: 14px;
  }
  .idx { font-family: var(--bb-font-mono); font-size: var(--bb-text-xs); color: var(--bb-muted); opacity: 0.55; }

  .name { display: inline-flex; align-items: center; gap: 10px; min-width: 0; }
  .c-name {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: var(--bb-text-sm);
    color: var(--bb-white);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  .name :global(.c-tag) { flex: none; }

  .meta { display: inline-flex; align-items: center; justify-content: flex-end; }
  .m-val {
    font-family: var(--bb-font-mono);
    font-size: var(--bb-text-sm);
    color: var(--bb-tan-light);
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }

  .del { display: inline-flex; --btn-icon-min-size: 32px; }

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
    .del { --btn-icon-min-size: 44px; }
  }
</style>
