<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import '@bagel/ui/styles/elements/row-body.css';
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n, type CounterDef, type CounterScope } from '@bagel/kit';
  import { formatCounterValue } from '@bagel/kit/validation';
  import RowDeleteButton from '#lib/components/shared/RowDeleteButton.svelte';

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

<ManagementRow selected={expanded} {expanded} controls="counter-inspector" onSelect={onExpand}>
  {#snippet primary()}
    <span class="prow bb-row-body bb-row-body--stack">
      {#if idx}<span class="bb-row-body__idx" aria-hidden="true">{idx}</span>{/if}
      <span class="name">
        <span class="bb-row-body__name bb-row-body__name--display">{c.name}</span>
        <Tag tone="bare" class="c-tag">{scopeLabel}</Tag>
      </span>
      <span class="bb-row-body__meta">
        {#if isChannel}
          <span class="bb-row-body__val">
            <span class="bb-sr-only">{t('counters.colValue')} </span>{formatCounterValue(c.value)}
          </span>
        {:else}
          <Text as="span" size="xs" tone="muted">{perScopeNote}</Text>
        {/if}
      </span>
    </span>
  {/snippet}
  {#snippet actions()}
    <RowDeleteButton label={t('counters.deleteAria', { name: c.name })} onclick={onDelete} />
  {/snippet}
</ManagementRow>

<style>
  .prow { --row-cols: 28px minmax(0, 1fr) auto; }

  .name { display: inline-flex; align-items: center; gap: 10px; min-width: 0; }
  .name :global(.c-tag) { flex: none; }
</style>
