<script lang="ts">
  // Daily enrollment vs registered base, as two aligned panels sharing one
  // x-axis: bars for signups per UTC day (magnitude), a line for the
  // registered-users total derived backwards from today's count. Two panels —
  // never a dual axis — because the measures live on different scales.
  import type { EnrollmentWire } from '$lib/server/services';

  let { enrollment }: { enrollment: EnrollmentWire } = $props();

  const days = $derived(enrollment.days);
  const total = $derived(enrollment.stats.total_users);

  // Cumulative registered per day, anchored to the live total: walking back
  // from today, each prior day sheds that day's signups. Deletions make old
  // points approximate; the axis label says "est." for that reason.
  const cumulative = $derived.by(() => {
    const out = new Array<number>(days.length);
    let run = total;
    for (let i = days.length - 1; i >= 0; i--) {
      out[i] = run;
      run -= days[i].count;
    }
    return out;
  });

  const totalNew = $derived(days.reduce((sum, d) => sum + d.count, 0));
  const peak = $derived(days.reduce((best, d, i) => (d.count > days[best].count ? i : best), 0));

  // ── Geometry ───────────────────────────────────────────────────────────────
  const W = 680;
  const H = 318;
  const PAD_L = 46;
  const PAD_R = 14;
  const BAR_TOP = 20;
  const BAR_BOTTOM = 186;
  const LINE_TOP = 226;
  const LINE_BOTTOM = 288;
  const AXIS_Y = 306;

  const innerW = W - PAD_L - PAD_R;
  const band = $derived(innerW / Math.max(days.length, 1));

  // Nice ceiling (1/2/5 * 10^k) so gridline labels are round numbers.
  function niceCeil(v: number): number {
    if (v <= 4) return 4;
    const mag = 10 ** Math.floor(Math.log10(v));
    for (const m of [1, 2, 5, 10]) {
      if (v <= m * mag) return m * mag;
    }
    return 10 * mag;
  }

  const barMax = $derived(niceCeil(Math.max(...days.map((d) => d.count), 1)));
  const barY = (count: number) =>
    BAR_BOTTOM - (count / barMax) * (BAR_BOTTOM - BAR_TOP);

  const cumLo = $derived(Math.min(...cumulative));
  const cumHi = $derived(Math.max(...cumulative));
  const cumPad = $derived(Math.max((cumHi - cumLo) * 0.15, 1));
  const lineY = (v: number) =>
    LINE_BOTTOM - ((v - (cumLo - cumPad)) / (cumHi + cumPad - (cumLo - cumPad))) * (LINE_BOTTOM - LINE_TOP);

  const xMid = (i: number) => PAD_L + i * band + band / 2;

  // Bar path: 3px-rounded top corners (the data end), square base on the
  // baseline. Zero days draw nothing — an empty slot is honest.
  function barPath(i: number, count: number): string {
    if (count <= 0) return '';
    const w = Math.max(band - 2, 1.5);
    const x = PAD_L + i * band + (band - w) / 2;
    const y = barY(count);
    const r = Math.min(3, w / 2, BAR_BOTTOM - y);
    return `M${x},${BAR_BOTTOM} V${y + r} Q${x},${y} ${x + r},${y} H${x + w - r} Q${x + w},${y} ${x + w},${y + r} V${BAR_BOTTOM} Z`;
  }

  const linePoints = $derived(
    cumulative.map((v, i) => `${xMid(i).toFixed(1)},${lineY(v).toFixed(1)}`).join(' ')
  );

  // Date ticks: first, last, and roughly weekly in between.
  const tickEvery = $derived(Math.max(Math.round(days.length / 5), 1));
  const ticks = $derived(
    days.map((d, i) => ({ i, label: d.date.slice(5) })).filter(
      ({ i }) => i % tickEvery === 0 || i === days.length - 1
    )
  );

  // Days are UTC buckets; render them as UTC too, or the label drifts a day
  // for anyone west of Greenwich.
  function fmtDate(iso: string): string {
    const [y, m, d] = iso.split('-').map(Number);
    return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString(undefined, {
      month: 'short',
      day: 'numeric',
      timeZone: 'UTC'
    });
  }

  // ── Hover layer ────────────────────────────────────────────────────────────
  let hover = $state<number | null>(null);
  let plotEl = $state<SVGSVGElement | null>(null);

  function hoverAt(clientX: number) {
    if (!plotEl) return;
    const rect = plotEl.getBoundingClientRect();
    const x = ((clientX - rect.left) / rect.width) * W - PAD_L;
    const i = Math.floor(x / band);
    hover = i >= 0 && i < days.length ? i : null;
  }

  const tip = $derived(hover === null ? null : {
    date: fmtDate(days[hover].date),
    count: days[hover].count,
    registered: cumulative[hover],
    leftPct: (xMid(hover) / W) * 100,
    flip: hover > days.length * 0.62
  });
</script>

<div class="chart">
  <div class="chart-summary" aria-hidden="true">
    <span><b>{totalNew.toLocaleString()}</b> signups in {days.length} days</span>
    <span class="mid">·</span>
    <span>peak <b>{days[peak].count}</b> on {fmtDate(days[peak].date)}</span>
    <span class="mid">·</span>
    <span><b>{total.toLocaleString()}</b> registered now</span>
  </div>

  <div class="plot-wrap">
    <svg
      bind:this={plotEl}
      viewBox="0 0 {W} {H}"
      role="img"
      aria-label="Daily signups and registered users over the last {days.length} days"
      onmousemove={(e) => hoverAt(e.clientX)}
      onmouseleave={() => (hover = null)}
    >
      <!-- panel titles: identity lives in text, not color alone -->
      <text class="panel-title" x={PAD_L} y={BAR_TOP - 7}>Signups / day</text>
      <text class="panel-title" x={PAD_L} y={LINE_TOP - 8}>Registered users (est. history)</text>

      <!-- recessive grid + round y labels, signups panel -->
      {#each [0.5, 1] as f (f)}
        <line class="grid" x1={PAD_L} x2={W - PAD_R} y1={barY(barMax * f)} y2={barY(barMax * f)} />
        <text class="tick" x={PAD_L - 6} y={barY(barMax * f) + 3} text-anchor="end">
          {Math.round(barMax * f)}
        </text>
      {/each}
      <line class="baseline" x1={PAD_L} x2={W - PAD_R} y1={BAR_BOTTOM} y2={BAR_BOTTOM} />

      <!-- bars -->
      {#each days as d, i (d.date)}
        <path class="bar" class:dim={hover !== null && hover !== i} d={barPath(i, d.count)} />
      {/each}
      <!-- selective direct label: the peak only -->
      {#if days[peak].count > 0 && hover === null}
        <text class="peak-label" x={xMid(peak)} y={barY(days[peak].count) - 5} text-anchor="middle">
          {days[peak].count}
        </text>
      {/if}

      <!-- registered panel: min/max ticks + 2px line -->
      <line class="grid" x1={PAD_L} x2={W - PAD_R} y1={LINE_TOP} y2={LINE_TOP} />
      <line class="grid" x1={PAD_L} x2={W - PAD_R} y1={LINE_BOTTOM} y2={LINE_BOTTOM} />
      <text class="tick" x={PAD_L - 6} y={LINE_TOP + 3} text-anchor="end">{cumHi.toLocaleString()}</text>
      <text class="tick" x={PAD_L - 6} y={LINE_BOTTOM + 3} text-anchor="end">{cumLo.toLocaleString()}</text>
      <polyline class="cumline" points={linePoints} />
      {#if hover === null}
        <circle class="cumdot" cx={xMid(days.length - 1)} cy={lineY(total)} r="3" />
      {/if}

      <!-- x-axis date ticks, shared by both panels -->
      {#each ticks as t (t.i)}
        <text class="tick" x={xMid(t.i)} y={AXIS_Y} text-anchor="middle">{t.label}</text>
      {/each}

      <!-- hover: full-height column band (hit target wider than the mark) -->
      {#if hover !== null}
        <line class="crosshair" x1={xMid(hover)} x2={xMid(hover)} y1={BAR_TOP} y2={LINE_BOTTOM} />
        <circle class="cumdot" cx={xMid(hover)} cy={lineY(cumulative[hover])} r="3.5" />
      {/if}
    </svg>

    {#if tip}
      <div class="tooltip" style="left:{tip.leftPct}%" class:flip={tip.flip}>
        <span class="tt-date">{tip.date}</span>
        <span class="tt-row"><i class="sw bar-sw"></i>{tip.count} signup{tip.count === 1 ? '' : 's'}</span>
        <span class="tt-row"><i class="sw line-sw"></i>{tip.registered.toLocaleString()} registered</span>
      </div>
    {/if}
  </div>

  <!-- data table for screen readers / no-hover access -->
  <table class="sr-only">
    <caption>Daily signups and estimated registered users</caption>
    <thead><tr><th>Date</th><th>Signups</th><th>Registered</th></tr></thead>
    <tbody>
      {#each days as d, i (d.date)}
        <tr><td>{d.date}</td><td>{d.count}</td><td>{cumulative[i]}</td></tr>
      {/each}
    </tbody>
  </table>
</div>

<style>
  .chart { display: flex; flex-direction: column; gap: 4px; }
  .chart-summary {
    display: flex; flex-wrap: wrap; gap: 8px; align-items: baseline;
    font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted);
    margin-bottom: 8px;
  }
  .chart-summary b { color: var(--bb-white); font-weight: 600; }
  .chart-summary .mid { color: var(--rule-strong); }

  .plot-wrap { position: relative; }
  svg { width: 100%; height: auto; display: block; }

  .panel-title {
    font-family: var(--bb-font-mono); font-size: 10px; letter-spacing: 0.1em;
    text-transform: uppercase; fill: var(--bb-muted);
  }
  .grid { stroke: rgba(240, 236, 228, 0.07); stroke-width: 1; }
  .baseline { stroke: rgba(240, 236, 228, 0.16); stroke-width: 1; }
  .tick { font-family: var(--bb-font-mono); font-size: 10px; fill: var(--bb-muted); }

  .bar { fill: var(--bb-green-glow); opacity: 0.92; transition: opacity 120ms ease; }
  .bar.dim { opacity: 0.35; }
  .peak-label { font-family: var(--bb-font-mono); font-size: 10px; fill: var(--bb-green-glow); }

  .cumline { fill: none; stroke: var(--bb-tan-light); stroke-width: 2; stroke-linejoin: round; }
  .cumdot { fill: var(--bb-tan-light); stroke: var(--bb-card-bg); stroke-width: 2; }

  .crosshair { stroke: rgba(240, 236, 228, 0.25); stroke-width: 1; stroke-dasharray: 3 3; }

  .tooltip {
    position: absolute; top: 4px; transform: translateX(10px);
    display: flex; flex-direction: column; gap: 3px;
    background: var(--bb-bg-1, #111110); border: 1px solid var(--bb-border-strong);
    border-radius: 8px; padding: 8px 11px; pointer-events: none; z-index: 5;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.45);
  }
  .tooltip.flip { transform: translateX(calc(-100% - 10px)); }
  .tt-date { font-family: var(--bb-font-mono); font-size: 10.5px; letter-spacing: 0.06em; color: var(--bb-muted); }
  .tt-row {
    display: inline-flex; align-items: center; gap: 7px;
    font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-white); white-space: nowrap;
  }
  .sw { width: 9px; height: 9px; border-radius: 3px; flex: none; }
  .bar-sw { background: var(--bb-green-glow); }
  .line-sw { background: var(--bb-tan-light); }
</style>
