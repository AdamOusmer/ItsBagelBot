<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Chat volume across the stream: one point per minute, plus tan ticks on the
  // minutes a command answered.
  //
  // The path is built here rather than shipped as a fixed `d` string because the
  // bucket count changes with stream length — a 20-minute stream and a 6-hour
  // one both have to fill the same box. Everything is unitless viewBox space and
  // the SVG scales with its container, so no resize observer is involved.
  import { getI18n } from '@bagel/shared/i18n/context';
  import type { ChatVolume } from '$lib/overview-live';

  const { t } = getI18n();

  let { volume }: { volume: ChatVolume } = $props();

  // viewBox geometry. Height splits into a plot band and a tick gutter so the
  // command marks never overlap the curve.
  const W = 640;
  const PLOT_TOP = 12;
  const PLOT_BOTTOM = 144;
  const TICK_TOP = 150;
  const TICK_BOTTOM = 160;
  const H = 178;

  type Geometry = { fill: string; line: string; ticks: number[]; last: { x: number; y: number } | null };

  const geo = $derived.by<Geometry>(() => {
    const b = volume.buckets;
    if (b.length < 2) return { fill: '', line: '', ticks: [], last: null };

    // Scale to the observed peak, never to zero: a flat stream would divide by
    // zero and a stream quieter than its own peak should still use the full box.
    const top = Math.max(...b, 1);
    const step = W / (b.length - 1);
    const x = (i: number) => Math.round(i * step * 10) / 10;
    const y = (v: number) =>
      Math.round((PLOT_BOTTOM - (v / top) * (PLOT_BOTTOM - PLOT_TOP)) * 10) / 10;

    const points = b.map((v, i) => `${x(i)},${y(v)}`);
    const line = `M${points.join(' L')}`;
    return {
      line,
      fill: `${line} L${W},${PLOT_BOTTOM} L0,${PLOT_BOTTOM} Z`,
      ticks: volume.commandTicks.filter((i) => i >= 0 && i < b.length).map(x),
      last: { x: x(b.length - 1), y: y(b[b.length - 1]) }
    };
  });

  const hasCurve = $derived(volume.ok && geo.line !== '');
</script>

<div class="ov-vol">
  <div class="ov-vol__head">
    <span class="ov-vol__label">{t('overview.chatVolume')}</span>
    {#if volume.ok}
      <span class="ov-vol__rate"
        >{t('overview.chatVolumeNowPeak', { now: volume.now, peak: volume.peak })}</span
      >
    {/if}
  </div>

  {#if hasCurve}
    <svg
      class="ov-vol__svg"
      viewBox="0 0 {W} {H}"
      preserveAspectRatio="none"
      role="img"
      aria-label={t('overview.chatVolumeChartLabel')}
    >
      <defs>
        <linearGradient id="ovFill" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stop-color="var(--bb-green-glow)" stop-opacity="0.3" />
          <stop offset="100%" stop-color="var(--bb-green-glow)" stop-opacity="0.02" />
        </linearGradient>
      </defs>
      <line x1="0" y1="44" x2={W} y2="44" class="ov-vol__grid" />
      <line x1="0" y1="94" x2={W} y2="94" class="ov-vol__grid" />
      <line x1="0" y1={PLOT_BOTTOM} x2={W} y2={PLOT_BOTTOM} class="ov-vol__grid ov-vol__grid--base" />
      <path d={geo.fill} fill="url(#ovFill)" />
      <path d={geo.line} class="ov-vol__line" />
      <g class="ov-vol__ticks">
        {#each geo.ticks as tx (tx)}
          <line x1={tx} y1={TICK_TOP} x2={tx} y2={TICK_BOTTOM} />
        {/each}
      </g>
      {#if geo.last}
        <circle cx={geo.last.x} cy={geo.last.y} r="3.5" class="ov-vol__dot" />
      {/if}
    </svg>
    <p class="ov-vol__legend">
      <span class="ov-vol__swatch" aria-hidden="true"></span>
      {t('overview.chatVolumeLegend')}
    </p>
  {:else}
    <p class="ov-vol__empty">{t('overview.chatVolumeUnavailable')}</p>
  {/if}
</div>

<style>
  .ov-vol {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .ov-vol__head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 6px;
  }
  .ov-vol__label,
  .ov-vol__rate {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .ov-vol__svg {
    width: 100%;
    height: 178px;
    display: block;
  }
  .ov-vol__grid {
    stroke: rgba(201, 168, 124, 0.1);
    stroke-width: 1;
  }
  .ov-vol__grid--base {
    stroke: rgba(201, 168, 124, 0.14);
  }
  .ov-vol__line {
    fill: none;
    stroke: var(--bb-green-glow);
    stroke-width: 1.8;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .ov-vol__ticks line {
    stroke: var(--bb-tan);
    stroke-width: 1.6;
    stroke-linecap: round;
    opacity: 0.7;
  }
  .ov-vol__dot {
    fill: var(--bb-green-glow);
  }
  .ov-vol__legend {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 12px 0 0;
    font-family: var(--bb-font-mono);
    font-size: 10px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .ov-vol__swatch {
    width: 10px;
    height: 2px;
    background: var(--bb-tan);
    display: inline-block;
    flex: none;
  }
  .ov-vol__empty {
    margin: 18px 0 0;
    font-family: var(--bb-font-body);
    font-size: 13px;
    color: var(--bb-muted);
  }
</style>
