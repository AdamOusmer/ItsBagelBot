<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import '@bagel/ui/styles/elements/row-body.css';
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import { getI18n } from '@bagel/kit';
  import RowDeleteButton from '#lib/components/shared/RowDeleteButton.svelte';
  import type { QuoteView } from '#lib/server/quotes-store';

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
    <span class="prow bb-row-body">
      <span class="num" class:on={expanded}>#{quote.number}</span>
      <span class="quote-text bb-row-body__name">{quote.text}</span>
      <span class="date bb-row-body__val bb-row-body__val--xs">{formatDate(quote.created_at)}</span>
    </span>
  {/snippet}
  {#snippet actions()}
    <RowDeleteButton label={t('quotes.deleteAriaNamed', { n: quote.number })} onclick={onDelete} />
  {/snippet}
</ManagementRow>

<style>
  .prow {
    --row-cols: 48px minmax(0, 1fr) auto;
    grid-template-areas: 'num quote date';
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

  .quote-text { grid-area: quote; }
  .date { grid-area: date; }

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
