<script lang="ts">
  // One ledger line in the quotes deck, rendered as a non-interactive <li> with
  // two SEPARATE controls, never nested: a disclosure button (opens the quote in
  // the page inspector) and a delete button. The disclosure's accessible name is
  // its visible content (number + full quote text + date), so the whole quote is
  // available to assistive tech even though the visible line is clamped.
  import { Icon, MiniButton, getI18n } from '@bagel/shared';
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
    const parts = iso.slice(0, 10).split('-').map(Number);
    if (parts.length !== 3 || parts.some((part) => !Number.isFinite(part))) return '';
    return new Date(parts[0], parts[1] - 1, parts[2]).toLocaleDateString();
  }
</script>

<li class="row-shell reveal {expanded ? 'selected' : ''}">
  <!-- Disclosure: the ONLY control that opens the inspector. Full quote text is
       inside it (visually clamped, but complete in the accessible name). -->
  <button
    class="disclosure"
    type="button"
    aria-expanded={expanded}
    aria-controls="quote-inspector"
    onclick={onExpand}
  >
    <span class="num">#{quote.number}</span>
    <span class="quote">
      <span class="swatch" aria-hidden="true"><Icon name="quote" size={11} /></span>
      <span class="quote-text">{quote.text}</span>
    </span>
    <span class="date">{formatDate(quote.created_at)}</span>
  </button>

  <div class="row-act">
    <MiniButton icon="trash" aria-label={`${t('quotes.deleteAria')}: #${quote.number}`} onclick={onDelete} />
  </div>
</li>

<style>
  .row-shell {
    list-style: none;
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    padding: 0 14px 0 0;
    border-bottom: 1px solid var(--rule, rgba(240, 236, 228, 0.08));
    transition: background var(--bb-dur-fast, 140ms) ease;
  }
  .row-shell.selected { background: rgba(201, 168, 124, 0.05); }

  .disclosure {
    display: grid;
    grid-template-columns: 48px minmax(0, 1fr) auto;
    align-items: center;
    gap: 14px;
    padding: 12px 0 12px 14px;
    min-width: 0;
    min-height: 44px;
    text-align: left;
    background: none;
    border: 0;
    color: inherit;
    font: inherit;
    cursor: pointer;
    user-select: none;
  }
  .disclosure:hover { background: rgba(201, 168, 124, 0.045); }
  .disclosure:focus-visible { outline: 1px solid var(--bb-tan, #c9a87c); outline-offset: -1px; }

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
    .disclosure {
      grid-template-columns: 48px minmax(0, 1fr);
      grid-template-areas:
        'quote quote'
        'num date';
      row-gap: 4px;
      padding: 8px 0 8px 12px;
    }
    .num { grid-area: num; }
    .quote { grid-area: quote; }
    .date { grid-area: date; justify-self: end; }
    /* Touch: keep the delete control at a >=44px hit target. */
    .row-act :global(.mini) { min-width: 44px; min-height: 44px; }
  }
</style>
