<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import { getUiI18n } from './i18n';
  import '../styles/elements/line-series.css';
  import {
    clockTime,
    hoverTime,
    lineChart,
    lineX,
    lineY,
    nearestPoint,
    plainValue,
    plottable,
    seriesClass,
    tooltipBox,
    LINE_DEFAULT_WIDTH,
    type LinePoint,
    type LineSeriesSpec,
  } from '../lib/line-series';

  const i18n = getUiI18n();
  type Own = {
    points: readonly LinePoint[];
    series: readonly LineSeriesSpec[];
    label: string;
    description?: string;
    unit?: string;
    emptyLabel?: string;
    formatValue?: (value: number | null | undefined) => string;
    formatTime?: (at: number) => string;
    class?: string;
  };

  let {
    points,
    series,
    label,
    description = '',
    unit = '',
    emptyLabel = i18n.t('data.empty'),
    formatValue = plainValue,
    formatTime = clockTime,
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['div'], keyof Own> = $props();

  let containerWidth = $state(LINE_DEFAULT_WIDTH);
  let hoveredAt = $state<number | null>(null);

  const keys = $derived(series.map((s) => s.key));
  const chart = $derived(lineChart(points, keys, containerWidth));
  const active = $derived(hoveredAt === null ? null : nearestPoint(chart.readings, hoveredAt));
  const tip = $derived(active ? tooltipBox(chart, lineX(chart, active.at), series.length) : null);
  const classes = $derived(['bb-line-series', className || null].filter(Boolean).join(' '));

  function inspect(event: PointerEvent) {
    const bounds = event.currentTarget instanceof SVGElement ? event.currentTarget.getBoundingClientRect() : null;
    if (!bounds || !chart.readings.length) return;
    hoveredAt = hoverTime(chart, ((event.clientX - bounds.left) * chart.width) / bounds.width);
  }
</script>

<div class={classes} {...rest}>
  <div class="bb-line-series__legend">
    {#each series as spec (spec.key)}
      <div class={seriesClass(spec, 'key')}>
        <span class="bb-line-series__swatch" aria-hidden="true"></span>
        <span>{spec.label}</span>
        <strong>{formatValue(chart.latest?.[spec.key])}</strong>
      </div>
    {/each}
  </div>
  <div class="bb-line-series__plot" bind:clientWidth={containerWidth}>
    <svg
      viewBox="0 0 {chart.width} {chart.height}"
      role="img"
      aria-label={label}
      onpointermove={inspect}
      onpointerleave={() => (hoveredAt = null)}
    >
      <title>{label}</title>
      {#if description}<desc>{description}</desc>{/if}
      {#if unit}<text class="bb-line-series__unit" x={chart.left} y="12">{unit}</text>{/if}
      {#each chart.valueTicks as tick (tick.value)}
        <line class="bb-line-series__grid" x1={chart.left} x2={chart.width - chart.right} y1={tick.y} y2={tick.y} />
        <text x={chart.left - 10} y={tick.y + 3} text-anchor="end">{tick.label}</text>
      {/each}
      {#each chart.timeTicks as tick (tick.at)}
        <line class="bb-line-series__tick" x1={tick.x} x2={tick.x} y1={chart.height - chart.bottom} y2={chart.height - chart.bottom + 5} />
        <text x={tick.x} y={chart.height - 10} text-anchor={tick.anchor}>{formatTime(tick.at)}</text>
      {/each}
      {#each series as spec, i (spec.key)}
        <path class={seriesClass(spec, 'line')} d={chart.paths[i]} />
      {/each}
      {#each chart.dots as dot, i (i)}
        <circle class={seriesClass(series[dot.series], 'point')} cx={dot.x} cy={dot.y} r={chart.dotRadius} />
      {/each}
      {#if active && tip}
        <line class="bb-line-series__crosshair" x1={lineX(chart, active.at)} x2={lineX(chart, active.at)} y1={chart.top} y2={chart.height - chart.bottom} />
        {#each series as spec (spec.key)}
          {@const value = plottable(active[spec.key])}
          {#if value !== null}
            <circle class="{seriesClass(spec, 'point')} bb-line-series__point--active" cx={lineX(chart, active.at)} cy={lineY(chart, value)} r="4" />
          {/if}
        {/each}
        <g class="bb-line-series__tooltip" transform="translate({tip.x}, {tip.y})">
          <rect width={tip.width} height={tip.height} rx="6" />
          <text x="10" y="16">{formatTime(active.at)}</text>
          {#each series as spec, i (spec.key)}
            <text class={seriesClass(spec, 'value')} x="10" y={36 + i * 20}>{spec.label}: {formatValue(active[spec.key])}</text>
          {/each}
        </g>
      {/if}
    </svg>
    {#if chart.sampling && emptyLabel}<div class="bb-line-series__empty">{emptyLabel}</div>{/if}
  </div>
</div>
