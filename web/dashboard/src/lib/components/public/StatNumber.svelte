<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import VisuallyHidden from '@bagel/ui/svelte/VisuallyHidden.svelte';
  import { statGlyphs } from '#lib/stats-digits';

  let { value, locale, compact = false, decimals = 0, reduced = false, rate = Infinity }: {
    value: number | bigint;
    locale: string;
    compact?: boolean;
    decimals?: number;
    reduced?: boolean;
    rate?: number;
  } = $props();

  const formatter = $derived(new Intl.NumberFormat(locale, {
    notation: compact ? 'compact' : 'standard',
    roundingMode: 'floor',
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals
  }));
  const numerals = $derived(Array.from({ length: 10 }, (_, i) =>
    new Intl.NumberFormat(locale, { useGrouping: false }).format(i)));
  const label = $derived(formatter.format(value));
  const parts = $derived(formatter.formatToParts(value));
  const glyphs = $derived(typeof value === 'number' ? statGlyphs(value, formatter, numerals, rate) : []);
</script>

<span class="stat-number">
  <VisuallyHidden class="stat-number__label">{label}</VisuallyHidden>
  <span class="stat-number__visual" aria-hidden="true">
    {#if reduced || typeof value === 'bigint'}
      {#each parts as part, i (i)}<span class:stat-number__unit={part.type === 'compact'}>{part.value}</span>{/each}
    {:else}
      {#each glyphs as glyph (glyph.key)}
        {#if glyph.phase === null}
          <span class:stat-number__unit={glyph.key.startsWith('compact-')}>{glyph.text}</span>
        {:else}
          <span class="stat-number__digit">
            <span class="stat-number__wheel" style:transform={`translateY(${-glyph.phase}em)`}>
              {#each [...numerals, numerals[0]] as numeral, i (i)}<span>{numeral}</span>{/each}
            </span>
          </span>
        {/if}
      {/each}
    {/if}
  </span>
</span>

<style>
  .stat-number { display: inline-flex; font-variant-numeric: tabular-nums; }
  .stat-number__visual { display: inline-flex; align-items: baseline; white-space: pre; }
  .stat-number__digit { display: inline-block; width: 1ch; height: 1em; overflow: clip; }
  .stat-number__wheel { display: flex; flex-direction: column; line-height: 1; text-align: center; }
  .stat-number__wheel > span { display: block; flex: none; height: 1em; }
  .stat-number__unit { margin-inline-start: 0.1em; }
</style>
