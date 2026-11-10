<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/area-series.css';
  import {
    areaGeometry,
    isAreaKey,
    nearestIndex,
    percent,
    plainValue,
    pointAt,
    stepIndex,
    tipRow,
    tipSide,
    valueText,
    AREA_W,
    AREA_H,
    AREA_PLOT_TOP,
    AREA_PLOT_BOTTOM,
    AREA_TICK_TOP,
    AREA_TICK_BOTTOM,
  } from '../lib/area-series';

  type Own = {
    values: readonly number[];
    ticks?: readonly number[];
    labels?: readonly string[];
    formatValue?: (value: number) => string;
    unit?: string;
    label: string;
    height?: number;
    uid?: string;
    class?: string;
  };

  let {
    values,
    ticks = [],
    labels = [],
    formatValue = plainValue,
    unit = '',
    label,
    height = 178,
    uid,
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['div'], keyof Own> = $props();

  const fallbackId = $props.id();
  const gradientId = $derived(`bb-area-fill-${uid ?? fallbackId}`);
  const geo = $derived(areaGeometry(values, ticks));
  const classes = $derived(['bb-area', className || null].filter(Boolean).join(' '));
  const interactive = $derived(values.length > 1);
  const last = $derived(pointAt(values, values.length - 1));

  let hovered = $state<number | null>(null);
  const index = $derived(hovered !== null && hovered < values.length ? hovered : null);
  const spot = $derived(index === null ? null : pointAt(values, index));
  const current = $derived(index ?? values.length - 1);
  const reading = $derived(interactive ? valueText(values[current], formatValue, unit) : '');
  const caption = $derived(labels[current] ?? '');
  const spoken = $derived(caption ? `${caption}, ${reading}` : reading);

  function inspect(event: PointerEvent) {
    const bounds = event.currentTarget instanceof HTMLElement ? event.currentTarget.getBoundingClientRect() : null;
    if (!bounds || !bounds.width) return;
    hovered = nearestIndex((event.clientX - bounds.left) / bounds.width, values.length);
  }

  function step(event: KeyboardEvent) {
    if (!isAreaKey(event.key)) return;
    event.preventDefault();
    hovered = stepIndex(index, event.key, values.length);
  }

  function clear() {
    hovered = null;
  }
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<div
  class={classes}
  style="height: {height}px"
  role={interactive ? 'slider' : 'img'}
  aria-label={label}
  tabindex={interactive ? 0 : undefined}
  aria-orientation={interactive ? 'horizontal' : undefined}
  aria-readonly={interactive ? 'true' : undefined}
  aria-valuemin={interactive ? 0 : undefined}
  aria-valuemax={interactive ? values.length - 1 : undefined}
  aria-valuenow={interactive ? current : undefined}
  aria-valuetext={interactive ? (spoken || undefined) : undefined}
  onpointermove={interactive ? inspect : undefined}
  onpointerdown={interactive ? inspect : undefined}
  onpointerleave={clear}
  onpointercancel={clear}
  onkeydown={interactive ? step : undefined}
  onblur={clear}
  {...rest}
>
  <svg
    class="bb-area__svg"
    viewBox="0 0 {AREA_W} {AREA_H}"
    preserveAspectRatio="none"
    aria-hidden="true"
    focusable="false"
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
  </svg>
  {#if last}
    <span class="bb-area__dot" style="left: {percent(last.x)}; top: {percent(last.y)}"></span>
  {/if}
  {#if spot}
    <span
      class="bb-area__guide"
      style="left: {percent(spot.x)}; top: {percent(AREA_PLOT_TOP / AREA_H)}; height: {percent((AREA_PLOT_BOTTOM - AREA_PLOT_TOP) / AREA_H)}"
    ></span>
    <span class="bb-area__dot bb-area__dot--active" style="left: {percent(spot.x)}; top: {percent(spot.y)}"></span>
    <span class="bb-area__tip bb-area__tip--{tipSide(spot.x)} bb-area__tip--{tipRow(spot.y)}" style="left: {percent(spot.x)}">
      {#if caption}<span class="bb-area__tip-label">{caption}</span>{/if}
      <strong class="bb-area__tip-value">{reading}</strong>
    </span>
  {/if}
</div>
