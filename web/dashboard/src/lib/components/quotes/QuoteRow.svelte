<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import { getI18n } from '@bagel/kit';
  import RowDeleteButton from '$lib/components/shared/RowDeleteButton.svelte';
  import type { QuoteView } from '$lib/server/quotes-store';

  let {
    quote,
    expanded = false,
    onExpand,
    onDelete
  }: {
    quote: QuoteView;
    expanded?: boolean;
    onExpand: () => void;
    onDelete: () => void;
  } = $props();

  const { t, locale } = getI18n();

  function formatDate(iso: string): string {
    const parts = iso.slice(0, 10).split('-').map(Number);
    if (parts.length !== 3 || parts.some((part) => !Number.isFinite(part))) return '';
    return new Date(parts[0], parts[1] - 1, parts[2]).toLocaleDateString(locale);
  }
</script>

<ManagementRow as="li" class="reveal" selected={expanded} {expanded} controls="quote-inspector" onSelect={onExpand}>
  {#snippet primary()}
    <span class="prow">
      <span class="num" class:on={expanded}>#{quote.number}</span>
      <span class="quote-text">{quote.text}</span>
      <span class="date">{formatDate(quote.created_at)}</span>
    </span>
  {/snippet}
  {#snippet actions()}
    <RowDeleteButton label={t('quotes.deleteAriaNamed', { n: quote.number })} onclick={onDelete} />
  {/snippet}
</ManagementRow>

<style>
  .prow {
    display: grid;
    grid-template-columns: 48px minmax(0, 1fr) auto;
    grid-template-areas: 'num quote date';
    align-items: center;
    gap: 14px;
    min-height: 20px;
  }

  .num {
    grid-area: num;
    font-family: var(--bb-font-mono);
    font-size: var(--bb-text-xs);
    color: var(--bb-muted);
    font-variant-numeric: tabular-nums;
  }
  .num.on { color: var(--bb-tan); }

  .quote-text {
    grid-area: quote;
    min-width: 0;
    font-family: var(--bb-font-body);
    font-weight: 600;
    font-size: var(--bb-text-sm);
    color: var(--bb-white);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .date {
    grid-area: date;
    font-family: var(--bb-font-mono);
    font-size: var(--bb-text-xs);
    color: var(--bb-tan-light);
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }

  @media (max-width: 700px) {
    .prow {
      grid-template-columns: 48px minmax(0, 1fr);
      grid-template-areas:
        'quote quote'
        'num date';
      row-gap: 4px;
    }
    .date { justify-self: end; }
  }
</style>
