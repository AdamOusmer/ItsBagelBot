<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Per-command answer counts for the CURRENT stream — deliberately not the
  // lifetime "top commands" strip, which lives lower on the page and answers a
  // different question ("what do people use") than this one ("what happened
  // tonight"). Same data source, different scope; keeping both is the point.
  //
  // Bars are proportional to the busiest command rather than to a fixed ceiling,
  // so a quiet stream still reads as a shape instead of five slivers.
  import { getI18n } from '@bagel/shared/i18n/context';
  import type { AnsweredTonight } from '$lib/overview-live';

  const { t } = getI18n();

  let { answered }: { answered: AnsweredTonight } = $props();

  type Bar = { name: string; count: number; pct: number; tone: 'a' | 'b' | 'c' };

  const bars = $derived.by<Bar[]>(() => {
    const rows = answered.commands;
    if (!rows.length) return [];
    const top = Math.max(...rows.map((r) => r.count), 1);
    return rows.map((r, i) => ({
      name: r.name,
      count: r.count,
      pct: Math.max(4, Math.round((r.count / top) * 100)),
      // Three steps of green, brightest at the top, so rank reads without
      // relying on bar length alone.
      tone: i === 0 ? 'a' : i < 3 ? 'b' : 'c'
    }));
  });
</script>

<section class="ov-ans" aria-labelledby="ov-ans-h">
  <div class="ov-ans__head">
    <h2 id="ov-ans-h" class="ov-ans__h">{t('overview.answeredTonight')}</h2>
    <a class="ov-ans__all" href="/commands">{t('overview.answeredAll')} →</a>
  </div>

  {#if !answered.ok}
    <p class="ov-ans__empty">{t('overview.answeredUnavailable')}</p>
  {:else if !bars.length}
    <p class="ov-ans__empty">{t('overview.answeredEmpty')}</p>
  {:else}
    <ul class="ov-ans__list">
      {#each bars as bar (bar.name)}
        <li class="ov-ans__row">
          <div class="ov-ans__line">
            <b class="ov-ans__name">{bar.name}</b>
            <span class="ov-ans__count">{bar.count.toLocaleString()}</span>
          </div>
          <span class="ov-ans__track">
            <span class="ov-ans__fill ov-ans__fill--{bar.tone}" style:width="{bar.pct}%"></span>
          </span>
        </li>
      {/each}
    </ul>
  {/if}
</section>

<style>
  .ov-ans {
    background: var(--bb-card-bg);
    border: 1px solid var(--bb-border);
    border-radius: 8px;
    overflow: hidden;
  }
  .ov-ans__head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 17px 20px 14px;
    border-bottom: 1px solid var(--bb-border);
  }
  .ov-ans__h {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 16px;
    letter-spacing: -0.01em;
    color: var(--bb-white);
    margin: 0;
  }
  .ov-ans__all {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--bb-tan);
    text-decoration: none;
  }
  .ov-ans__all:hover {
    color: var(--bb-tan-pale);
  }
  .ov-ans__list {
    list-style: none;
    margin: 0;
    padding: 16px 20px 18px;
    display: flex;
    flex-direction: column;
    gap: 13px;
  }
  .ov-ans__row {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .ov-ans__line {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 10px;
  }
  .ov-ans__name {
    font-family: var(--bb-font-mono);
    font-weight: 500;
    font-size: 12.5px;
    color: var(--bb-white);
  }
  .ov-ans__count {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
    font-variant-numeric: tabular-nums;
  }
  .ov-ans__track {
    height: 5px;
    border-radius: 999px;
    background: rgba(201, 168, 124, 0.1);
    display: block;
    overflow: hidden;
  }
  .ov-ans__fill {
    display: block;
    height: 5px;
    border-radius: 999px;
    background: var(--bb-green-glow);
  }
  .ov-ans__fill--b {
    background: var(--bb-green-light);
  }
  .ov-ans__fill--c {
    background: var(--bb-green);
  }
  .ov-ans__empty {
    margin: 0;
    padding: 16px 20px 20px;
    font-family: var(--bb-font-body);
    font-size: 13px;
    color: var(--bb-muted);
  }
</style>
