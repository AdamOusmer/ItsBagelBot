<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for the `.bb-area` contract
  // (../styles/elements/area-series.css). No Astro twin: the geometry below is
  // the element, and a second implementation of it in another language is
  // exactly the drift this package exists to delete. A static surface that
  // needs a curve should render this one server-side.
  //
  // The chart chrome behind both consoles' volume curves: gridlines, the filled
  // area, the head-of-series dot and an optional gutter of event ticks. It owns
  // the geometry ONLY — the label, the rate readout, the legend and the
  // "unavailable" copy stay with the caller, because those differ per page
  // while the drawing never does.
  //
  // The path is built here rather than shipped as a fixed `d` string because
  // the point count changes with the window being charted: a 20-minute stream
  // and a 6-hour one both have to fill the same box. Everything is unitless
  // viewBox space and the SVG scales with its container, so no resize observer
  // is involved.
  import '../styles/elements/area-series.css';

  let {
    values,
    ticks = [],
    ariaLabel,
    height = 178,
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
    class?: string;
    [key: string]: unknown;
  } = $props();

  // viewBox geometry. The height splits into a plot band and a tick gutter so
  // the event marks never overlap the curve.
  const W = 640;
  const PLOT_TOP = 12;
  const PLOT_BOTTOM = 144;
  const TICK_TOP = 150;
  const TICK_BOTTOM = 160;
  const H = 178;

  // The gradient is referenced by id, so two charts on one page would otherwise
  // share (and fight over) one <defs> entry. $props.id() is stable across SSR
  // and hydration, unlike a module-level counter.
  const uid = $props.id();

  type Geometry = {
    fill: string;
    line: string;
    marks: number[];
    last: { x: number; y: number } | null;
  };

  const geo = $derived.by<Geometry>(() => {
    const b = values;
    if (b.length < 2) return { fill: '', line: '', marks: [], last: null };

    // Scale to the observed peak, never to zero: a flat series would divide by
    // zero, and a series quieter than its own peak should still use the full
    // box.
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
      marks: ticks.filter((i) => i >= 0 && i < b.length).map(x),
      last: { x: x(b.length - 1), y: y(b[b.length - 1]) },
    };
  });

  const classes = $derived(['bb-area', className || null].filter(Boolean).join(' '));
</script>

<svg
  class={classes}
  style="height: {height}px"
  viewBox="0 0 {W} {H}"
  preserveAspectRatio="none"
  role="img"
  aria-label={ariaLabel}
  {...rest}
>
  <defs>
    <linearGradient id="bb-area-fill-{uid}" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0%" stop-color="var(--bb-green-glow)" stop-opacity="0.3" />
      <stop offset="100%" stop-color="var(--bb-green-glow)" stop-opacity="0.02" />
    </linearGradient>
  </defs>
  <line x1="0" y1="44" x2={W} y2="44" class="bb-area__grid" />
  <line x1="0" y1="94" x2={W} y2="94" class="bb-area__grid" />
  <line x1="0" y1={PLOT_BOTTOM} x2={W} y2={PLOT_BOTTOM} class="bb-area__grid bb-area__grid--base" />
  <path d={geo.fill} fill="url(#bb-area-fill-{uid})" />
  <path d={geo.line} class="bb-area__line" />
  <g class="bb-area__ticks">
    {#each geo.marks as tx (tx)}
      <line x1={tx} y1={TICK_TOP} x2={tx} y2={TICK_BOTTOM} />
    {/each}
  </g>
  {#if geo.last}
    <circle cx={geo.last.x} cy={geo.last.y} r="3.5" class="bb-area__dot" />
  {/if}
</svg>
