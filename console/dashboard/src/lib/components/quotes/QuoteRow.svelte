<script lang="ts">
  import { Icon, getI18n } from '@bagel/shared';
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

  const { t } = getI18n();

  function formatDate(iso: string): string {
    const day = iso.slice(0, 10);
    const parts = day.split('-').map(Number);
    if (parts.length !== 3 || parts.some((part) => !Number.isFinite(part))) return '';
    return new Date(parts[0], parts[1] - 1, parts[2]).toLocaleDateString();
  }

  function rowKey(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      onExpand();
    }
  }
</script>

<div class="row-shell reveal {expanded ? 'selected' : ''}">
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="qrow" role="button" tabindex="0" aria-pressed={expanded} onclick={onExpand} onkeydown={rowKey}>
    <span class="num">#{quote.number}</span>
    <span class="quote">
      <span class="swatch"><Icon name="quote" size={11} /></span>
      <span class="quote-text">{quote.text}</span>
    </span>
    <span class="date">{formatDate(quote.created_at)}</span>

    <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
    <span class="row-act" onclick={(e) => e.stopPropagation()}>
      <button class="mini" type="button" aria-label={t('quotes.deleteAria')} onclick={onDelete}>
        <Icon name="trash" size={15} />
      </button>
    </span>
  </div>
</div>

<style>
  .row-shell {
    border-bottom: 1px solid var(--rule, rgba(240, 236, 228, 0.08));
    transition: background var(--bb-dur-fast, 140ms) ease;
  }
  .row-shell.selected { background: rgba(201, 168, 124, 0.05); }

  .qrow {
    display: grid;
    grid-template-columns: 48px minmax(0, 1fr) auto auto;
    align-items: center;
    gap: 14px;
    padding: 12px 14px;
    cursor: pointer;
    user-select: none;
  }
  .qrow:hover { background: rgba(201, 168, 124, 0.045); }
  .qrow:focus-visible { outline: 1px solid var(--bb-tan, #c9a87c); outline-offset: -1px; }

  .num {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
    font-variant-numeric: tabular-nums;
  }
  .row-shell.selected .num { color: var(--bb-tan); }

  .quote { display: inline-flex; align-items: center; gap: 8px; min-width: 0; }
  .quote-text {
    font-family: var(--bb-font-body);
    font-weight: 600;
    font-size: 13.5px;
    color: var(--bb-white);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .swatch {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 20px;
    height: 20px;
    flex: none;
    border-radius: 6px;
    background: color-mix(in srgb, var(--bb-tan, #c9a87c) 24%, transparent);
    border: 1px solid color-mix(in srgb, var(--bb-tan, #c9a87c) 55%, transparent);
    color: color-mix(in srgb, var(--bb-tan, #c9a87c) 75%, white);
  }

  .date {
    font-family: var(--bb-font-mono);
    font-size: 12px;
    color: var(--bb-tan-light);
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }
  .row-act { display: inline-flex; align-items: center; }

  @media (max-width: 700px) {
    .qrow {
      grid-template-columns: 48px minmax(0, 1fr) auto;
      grid-template-areas:
        'quote quote act'
        'num date act';
      row-gap: 4px;
    }
    .num { grid-area: num; }
    .quote { grid-area: quote; }
    .date { grid-area: date; justify-self: start; }
    .row-act { grid-area: act; }
    .mini { min-width: 44px; min-height: 44px; }
  }
</style>
