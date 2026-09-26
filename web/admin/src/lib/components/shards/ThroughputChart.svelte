<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit/i18n/context';
  import { rateLabel } from './shard-state';

  type Point = { at: number; production: number | null; trials: number | null };
  type Series = 'production' | 'trials';
  let { points, showTrials }: { points: Point[]; showTrials: boolean } = $props();
  const { t } = getI18n();
  let containerWidth = $state(800);
  let hoveredAt = $state<number | null>(null);
  const width = $derived(Math.max(280, containerWidth || 800));
  const height = $derived(width < 500 ? 220 : 260);
  const left = 55;
  const right = 18;
  const top = 24;
  const bottom = 34;
  const plotWidth = $derived(width - left - right);
  const plotHeight = $derived(height - top - bottom);
  const readings = $derived(points.filter((point) => Number.isFinite(point.at)).toSorted((a, b) => a.at - b.at));
  const firstAt = $derived(readings[0]?.at ?? 0);
  const lastAt = $derived(readings.at(-1)?.at ?? 0);
  const duration = $derived(Math.max(8000, lastAt - firstAt));
  const startAt = $derived(lastAt - duration);
  const latest = $derived(readings.at(-1));
  const ceiling = $derived(niceCeiling(Math.max(0, ...readings.flatMap((point) => [finiteRate(point.production) ?? 0, showTrials ? finiteRate(point.trials) ?? 0 : 0]))));
  const ticks = $derived(Array.from({ length: 5 }, (_, index) => ceiling * index / 4));
  const timeTickCount = $derived(width < 500 ? 3 : 5);
  const timeTicks = $derived(Array.from({ length: timeTickCount }, (_, index) => startAt + duration * index / (timeTickCount - 1)));
  const productionPath = $derived(seriesPath('production'));
  const trialPath = $derived(showTrials ? seriesPath('trials') : '');
  const sampling = $derived(readings.filter((point) => finiteRate(point.production) !== null || (showTrials && finiteRate(point.trials) !== null)).length < 2);
  const active = $derived(hoveredAt === null ? null : readings.reduce<Point | null>((closest, point) => !closest || Math.abs(point.at - hoveredAt!) < Math.abs(closest.at - hoveredAt!) ? point : closest, null));
  const tooltipX = $derived(active ? Math.min(width - 196, Math.max(left + 5, x(active.at) - 88)) : left);

  function finiteRate(value: number | null): number | null { return value !== null && Number.isFinite(value) && value >= 0 ? value : null; }
  function niceCeiling(value: number): number {
    if (value <= 0) return 1;
    const magnitude = 10 ** Math.floor(Math.log10(value));
    const normalized = value / magnitude;
    return (normalized <= 1 ? 1 : normalized <= 2 ? 2 : normalized <= 4 ? 4 : normalized <= 5 ? 5 : 10) * magnitude;
  }
  function x(at: number): number { return left + (at - startAt) / duration * plotWidth; }
  function y(rate: number): number { return top + plotHeight * (1 - rate / ceiling); }
  function seriesPath(series: Series): string {
    let previousValid = false;
    let previousAt = 0;
    return readings.map((point) => {
      const value = finiteRate(point[series]);
      if (value === null) { previousValid = false; return ''; }
      const command = previousValid && point.at - previousAt <= 30_000 ? 'L' : 'M';
      previousValid = true;
      previousAt = point.at;
      return `${command}${x(point.at).toFixed(2)},${y(value).toFixed(2)}`;
    }).join(' ');
  }
  function axisRate(rate: number): string {
    if (rate >= 1000) return `${(rate / 1000).toFixed(rate >= 10000 ? 0 : 1)}k`;
    if (rate === 0) return '0';
    if (rate < 1) return rate.toFixed(2);
    return rate.toFixed(rate < 10 ? 1 : 0);
  }
  function timeLabel(at: number): string { return new Date(at).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' }); }
  function label(value: number | null | undefined): string { return value == null || finiteRate(value) === null ? t('admin.shards.throughputUnavailable') : `${rateLabel(value)} ${t('admin.shards.eps')}`; }
  function inspect(event: PointerEvent) {
    const bounds = event.currentTarget instanceof SVGElement ? event.currentTarget.getBoundingClientRect() : null;
    if (!bounds || !readings.length) return;
    const local = (event.clientX - bounds.left) * width / bounds.width;
    hoveredAt = startAt + Math.min(1, Math.max(0, (local - left) / plotWidth)) * duration;
  }
</script>

<section class="chart" aria-labelledby="throughput-title">
  <header class="chart-head">
    <div><h2 class="chart-title" id="throughput-title">{t('admin.shards.throughputHistory')}</h2><p class="chart-hint">{t('admin.shards.throughputHistoryHint')}</p></div>
    <span class="window">{t('admin.shards.throughputWindow')}</span>
  </header>
  <div class="legend">
    <div class="series production"><span class="swatch" aria-hidden="true"></span><span>{t('admin.shards.throughputProduction')}</span><strong>{label(latest?.production)}</strong></div>
    {#if showTrials}<div class="series trials"><span class="swatch" aria-hidden="true"></span><span>{t('admin.shards.throughputTrials')}</span><strong>{label(latest?.trials)}</strong></div>{/if}
  </div>
  <div class="plot" bind:clientWidth={containerWidth}>
    <svg viewBox={`0 0 ${width} ${height}`} role="img" aria-label={t('admin.shards.throughputHistory')} onpointermove={inspect} onpointerleave={() => hoveredAt = null}>
      <title>{t('admin.shards.throughputHistory')}</title>
      <desc>{t('admin.shards.throughputHistoryHint')}</desc>
      <text class="unit" x={left} y="12">{t('admin.shards.eps')}</text>
      {#each ticks as tick}
        <line class="grid-line" x1={left} x2={width - right} y1={y(tick)} y2={y(tick)} />
        <text class="axis-label" x={left - 10} y={y(tick) + 3} text-anchor="end">{axisRate(tick)}</text>
      {/each}
      {#if readings.length}
        {#each timeTicks as at, index}
          <line class="time-tick" x1={x(at)} x2={x(at)} y1={height - bottom} y2={height - bottom + 5} />
          <text class="axis-label" x={x(at)} y={height - 10} text-anchor={index === 0 ? 'start' : index === timeTicks.length - 1 ? 'end' : 'middle'}>{timeLabel(at)}</text>
        {/each}
      {/if}
      <path class="series-line production-line" d={productionPath} />
      {#if showTrials}<path class="series-line trial-line" d={trialPath} />{/if}
      {#each readings as point}
        {#if finiteRate(point.production) !== null}<circle class="production-point" cx={x(point.at)} cy={y(point.production!)} r={readings.length < 3 ? 3.5 : 1.5} />{/if}
        {#if showTrials && finiteRate(point.trials) !== null}<circle class="trial-point" cx={x(point.at)} cy={y(point.trials!)} r={readings.length < 3 ? 3.5 : 1.5} />{/if}
      {/each}
      {#if active}
        <line class="crosshair" x1={x(active.at)} x2={x(active.at)} y1={top} y2={height - bottom} />
        {#if finiteRate(active.production) !== null}<circle class="active-point production-point" cx={x(active.at)} cy={y(active.production!)} r="4" />{/if}
        {#if showTrials && finiteRate(active.trials) !== null}<circle class="active-point trial-point" cx={x(active.at)} cy={y(active.trials!)} r="4" />{/if}
        <g class="tooltip" transform={`translate(${tooltipX}, ${top + 7})`}>
          <rect width="185" height={showTrials ? 72 : 52} rx="6" />
          <text x="10" y="16">{timeLabel(active.at)}</text>
          <text class="production-value" x="10" y="36">{t('admin.shards.throughputProduction')}: {label(active.production)}</text>
          {#if showTrials}<text class="trial-value" x="10" y="56">{t('admin.shards.throughputTrials')}: {label(active.trials)}</text>{/if}
        </g>
      {/if}
    </svg>
    {#if sampling}<div class="sampling">{t('admin.shards.throughputSampling')}</div>{/if}
  </div>
  {#if readings.length}
    <details class="readings">
      <summary>{t('admin.shards.throughputData')}</summary>
      <div class="table-scroll">
        <table>
          <caption class="sr-only">{t('admin.shards.throughputHistory')}</caption>
          <thead><tr><th scope="col">{t('admin.shards.throughputTime')}</th><th scope="col">{t('admin.shards.throughputProduction')}</th>{#if showTrials}<th scope="col">{t('admin.shards.throughputTrials')}</th>{/if}</tr></thead>
          <tbody>{#each readings.toReversed() as point}<tr><th scope="row">{timeLabel(point.at)}</th><td>{label(point.production)}</td>{#if showTrials}<td>{label(point.trials)}</td>{/if}</tr>{/each}</tbody>
        </table>
      </div>
    </details>
  {/if}
</section>

<style>
  .chart { margin-top: 24px; padding: 22px; border: 1px solid var(--bb-border); border-radius: 14px; background: var(--bb-card-bg); }
  .chart-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
  .chart-title { margin: 0; color: var(--bb-white); font-size: 17px; font-weight: 600; }
  .chart-hint { margin: 5px 0 0; color: var(--bb-muted); font-size: 12px; line-height: 1.6; }
  .window { color: var(--bb-muted); font-family: var(--bb-font-mono); font-size: 10px; white-space: nowrap; padding-top: 4px; }
  .legend { display: flex; flex-wrap: wrap; gap: 14px 28px; margin: 20px 0 18px; }
  .series { display: inline-flex; align-items: center; flex-wrap: wrap; gap: 8px; color: var(--bb-muted); font-family: var(--bb-font-mono); font-size: 11px; }
  .series strong { color: var(--bb-white); font-size: 12px; font-weight: 500; margin-left: 5px; }
  .swatch { width: 22px; border-top: 2px solid var(--bb-green-glow); }
  .trials .swatch { border-color: var(--bb-tan); border-top-style: dashed; }
  .plot { width: 100%; position: relative; }
  svg { display: block; width: 100%; height: auto; overflow: visible; }
  svg text { font-family: var(--bb-font-mono); font-size: 10px; fill: var(--bb-muted); }
  .grid-line { stroke: var(--bb-border); stroke-dasharray: 3 5; }
  .time-tick { stroke: var(--bb-border); }
  .series-line { fill: none; stroke-width: 2.5; stroke-linejoin: round; stroke-linecap: round; vector-effect: non-scaling-stroke; }
  .production-line { stroke: var(--bb-green-glow); }
  .trial-line { stroke: var(--bb-tan); stroke-dasharray: 6 4; }
  .production-point { fill: var(--bb-green-glow); }
  .trial-point { fill: var(--bb-tan); }
  .active-point { stroke: var(--bb-card-bg); stroke-width: 2; }
  .crosshair { stroke: var(--bb-muted); stroke-dasharray: 2 4; opacity: .6; }
  .tooltip { pointer-events: none; }
  .tooltip rect { fill: var(--bb-black); stroke: var(--bb-border-strong); }
  .tooltip .production-value { fill: var(--bb-green-glow); }
  .tooltip .trial-value { fill: var(--bb-tan); }
  .sampling { position: absolute; inset: 0; display: grid; place-items: center; pointer-events: none; color: var(--bb-muted); font-family: var(--bb-font-mono); font-size: 11px; text-align: center; padding: 25px 60px; }
  .readings { margin-top: 10px; color: var(--bb-muted); font-family: var(--bb-font-mono); font-size: 10px; }
  summary { cursor: pointer; width: fit-content; padding: 5px 0; }
  summary:focus-visible { outline: 2px solid var(--bb-green-glow); outline-offset: 4px; }
  .table-scroll { overflow: auto; max-height: 220px; margin-top: 10px; }
  table { width: 100%; border-collapse: collapse; text-align: left; }
  th, td { padding: 8px 12px; border-bottom: 1px solid var(--bb-border); font-weight: 400; white-space: nowrap; }
  thead th { color: var(--bb-white); }
  .sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0,0,0,0); white-space: nowrap; border: 0; }
  @media (max-width: 600px) { .chart { padding: 15px; } .chart-head { flex-wrap: wrap; gap: 6px; } .legend { gap: 12px; } .series { font-size: 10px; } }
</style>
