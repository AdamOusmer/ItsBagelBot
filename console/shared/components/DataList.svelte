<script lang="ts">
  // Responsive list: a labeled grid of rows on desktop that collapses into
  // stacked cards on mobile (each cell shows its column label inline). Callers
  // provide columns (label + fractional width) and a row snippet per item.
  import type { Snippet } from 'svelte';

  export interface Column {
    label: string;
    /** CSS grid track, e.g. '1fr', 'minmax(0,2fr)', 'auto'. Default '1fr'. */
    width?: string;
  }

  let {
    columns,
    items,
    key = undefined as ((item: unknown, index: number) => string | number) | undefined,
    row,
    empty = undefined as Snippet | undefined
  }: {
    columns: Column[];
    items: unknown[];
    key?: (item: unknown, index: number) => string | number;
    row: Snippet<[unknown, number]>;
    empty?: Snippet;
  } = $props();

  const template = $derived(columns.map((c) => c.width ?? '1fr').join(' '));
</script>

<div class="dl" style="--dl-cols: {template}">
  <div class="dl-head" aria-hidden="true">
    {#each columns as c (c.label)}<span>{c.label}</span>{/each}
  </div>
  {#each items as item, i (key ? key(item, i) : i)}
    <div class="dl-row">
      {@render row(item, i)}
    </div>
  {/each}
  {#if items.length === 0 && empty}
    {@render empty()}
  {/if}
</div>

<style>
  .dl { display: flex; flex-direction: column; }

  .dl-head {
    display: grid;
    grid-template-columns: var(--dl-cols);
    gap: 14px;
    padding: 8px 14px;
    font-family: var(--bb-font-mono);
    font-size: 10.5px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--bb-muted);
    border-bottom: 1px solid var(--glass-border);
  }

  .dl-row {
    display: grid;
    grid-template-columns: var(--dl-cols);
    gap: 14px;
    align-items: center;
    padding: 12px 14px;
    border-bottom: 1px solid var(--glass-border);
    font-family: var(--bb-font-body);
    font-size: 13px;
    color: var(--bb-white);
  }
  .dl-row:last-child { border-bottom: none; }

  @media (max-width: 760px) {
    .dl-head { display: none; }
    .dl-row {
      grid-template-columns: 1fr;
      gap: 6px;
      padding: 14px;
    }
  }
</style>
