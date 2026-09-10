<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for the `.bb-area` contract
  // (../styles/elements/area-series.css). Astro twin: ../astro/AreaSeries.astro.
  //
  // The chart chrome behind both consoles' volume curves: gridlines, the filled
  // area, the head-of-series dot and an optional gutter of event ticks. It owns
  // the geometry ONLY — the label, the rate readout, the legend and the
  // "unavailable" copy stay with the caller, because those differ per page
  // while the drawing never does.
  //
  // The arithmetic is ../lib/area-series.ts. It used to be in this file, with
  // a header saying there would never be an Astro twin because a second
  // implementation of the geometry in another language is the drift this
  // package exists to delete. That was right about the risk: the remedy is one
  // engine both adapters call, not one adapter.
  //
  // The path is computed rather than shipped as a fixed `d` string because the
  // point count changes with the window being charted: a 20-minute stream and
  // a 6-hour one both have to fill the same box. Everything is unitless viewBox
  // space and the SVG scales with its container, so no resize observer is
  // involved.
  import '../styles/elements/area-series.css';
  import {
    areaGeometry,
    AREA_W,
    AREA_H,
    AREA_PLOT_BOTTOM,
    AREA_TICK_TOP,
    AREA_TICK_BOTTOM,
  } from '../lib/area-series';

  let {
    values,
    ticks = [],
    ariaLabel,
    height = 178,
    uid,
    class: className = '',
    ...rest
  }: {
    /** One point per bucket, oldest first. Fewer than two draws nothing. */
    values: readonly number[];
    /** Indices into `values` to mark in the gutter below the plot. */
    ticks?: readonly number[];
    ariaLabel: string;
    /** Rendered height in CSS pixels; the viewBox is fixed, so this scales it. */
    height?: number;
    /**
     * Suffix for the gradient's id. Two charts on one page would otherwise
     * share (and fight over) one <defs> entry. Defaults to $props.id(), which
     * is stable across SSR and hydration unlike a module-level counter; the
     * Astro twin has no such generator and takes it as a required prop, which
     * is also what lets the parity suite render both from one fixed value.
     */
    uid?: string;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const fallbackId = $props.id();
  const gradientId = $derived(`bb-area-fill-${uid ?? fallbackId}`);
  const geo = $derived(areaGeometry(values, ticks));
  const classes = $derived(['bb-area', className || null].filter(Boolean).join(' '));
</script>

<svg
  class={classes}
  style="height: {height}px"
  viewBox="0 0 {AREA_W} {AREA_H}"
  preserveAspectRatio="none"
  role="img"
  aria-label={ariaLabel}
  {...rest}
>
  <defs>
    <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
      <stop offset="0%" stop-color="var(--bb-green-glow)" stop-opacity="0.3" />
      <stop offset="100%" stop-color="var(--bb-green-glow)" stop-opacity="0.02" />
    </linearGradient>
  </defs>
  <line x1="0" y1="44" x2={AREA_W} y2="44" class="bb-area__grid" />
  <line x1="0" y1="94" x2={AREA_W} y2="94" class="bb-area__grid" />
  <line
    x1="0"
    y1={AREA_PLOT_BOTTOM}
    x2={AREA_W}
    y2={AREA_PLOT_BOTTOM}
    class="bb-area__grid bb-area__grid--base"
  />
  <path d={geo.fill} fill="url(#{gradientId})" />
  <path d={geo.line} class="bb-area__line" />
  <g class="bb-area__ticks">
    {#each geo.marks as tx (tx)}
      <line x1={tx} y1={AREA_TICK_TOP} x2={tx} y2={AREA_TICK_BOTTOM} />
    {/each}
  </g>
  {#if geo.last}
    <circle cx={geo.last.x} cy={geo.last.y} r="3.5" class="bb-area__dot" />
  {/if}
</svg>
