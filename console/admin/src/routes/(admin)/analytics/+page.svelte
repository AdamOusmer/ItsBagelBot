<script lang="ts">
  import { goto } from '$app/navigation';
  import { onMount } from 'svelte';
  import { PageHead, PageToolbar, AlertBanner, Skeleton } from '@bagel/shared';
  import EnrollmentChart from '$lib/components/EnrollmentChart.svelte';
  import type { ServiceHealth } from '$lib/server/services';
  import type { AnalyticsBundle } from './+page.server';

  let { data } = $props();

  // A fresh uncached no-op request to every RPC service every 30 seconds makes
  // recovery visible without turning the Analytics page itself into a poller
  // while the tab is hidden.
  let liveHealth = $state<ServiceHealth[] | null>(null);
  async function pollHealth() {
    if (typeof document !== 'undefined' && document.hidden) return;
    try {
      const res = await fetch('/health');
      if (!res.ok) return;
      const body = (await res.json()) as { health?: ServiceHealth[] };
      if (body.health) liveHealth = body.health;
    } catch {
      /* transient: retain the last completed sample */
    }
  }
  onMount(() => {
    const timer = setInterval(pollHealth, 30_000);
    return () => clearInterval(timer);
  });

  let bundle = $state<AnalyticsBundle | null>(null);
  $effect(() => {
    let alive = true;
    bundle = null;
    data.bundle.then((b: AnalyticsBundle) => {
      if (alive) bundle = b;
    });
    return () => {
      alive = false;
    };
  });

  const WINDOWS = [7, 30, 90] as const;
  function applyWindow(days: number) {
    goto(days === 30 ? '/analytics' : `/analytics?window=${days}`, { keepFocus: true });
  }

  const enrollment = $derived(bundle?.enrollment ?? null);
  const stats = $derived(enrollment?.stats ?? null);

  // ── Base composition (current, not historical) ─────────────────────────────
  const tiers = $derived.by(() => {
    if (!stats) return [];
    const free = Math.max(stats.total_users - stats.premium_users, 0);
    return [
      { id: 'vip', label: 'VIP', count: stats.vip_users },
      { id: 'paid', label: 'Paid', count: stats.paid_users },
      { id: 'free', label: 'Free', count: free }
    ].filter((t) => t.count > 0);
  });

  const activity = $derived.by(() => {
    if (!stats) return [];
    const inactive = Math.max(stats.total_users - stats.active_users, 0);
    return [
      { id: 'active', label: 'Active', count: stats.active_users },
      { id: 'dormant', label: 'Dormant', count: inactive }
    ].filter((a) => a.count > 0);
  });

  function pct(count: number, total: number): number {
    return total > 0 ? (count / total) * 100 : 0;
  }

  // ── Signup patterns over the loaded window ─────────────────────────────────
  const WEEKDAYS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];

  const weekdayAvgs = $derived.by(() => {
    if (!enrollment) return [];
    const sums = new Array(7).fill(0);
    const counts = new Array(7).fill(0);
    for (const d of enrollment.days) {
      const [y, m, dd] = d.date.split('-').map(Number);
      const w = (new Date(Date.UTC(y, m - 1, dd)).getUTCDay() + 6) % 7; // Mon = 0
      sums[w] += d.count;
      counts[w]++;
    }
    return WEEKDAYS.map((label, i) => ({
      label,
      avg: counts[i] > 0 ? sums[i] / counts[i] : 0
    }));
  });
  const weekdayMax = $derived(Math.max(...weekdayAvgs.map((w) => w.avg), 0.001));

  const topDays = $derived.by(() => {
    if (!enrollment) return [];
    return [...enrollment.days]
      .filter((d) => d.count > 0)
      .sort((a, b) => b.count - a.count)
      .slice(0, 5);
  });

  function fmtDate(iso: string): string {
    const [y, m, d] = iso.split('-').map(Number);
    return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString(undefined, {
      month: 'short',
      day: 'numeric',
      timeZone: 'UTC'
    });
  }
</script>

<section class="screen active">
  <PageHead eyebrow="Analytics" description="Growth patterns and live NATS RPC round-trip timing across the fleet.">
    Growth <em>analytics</em>
  </PageHead>

  <PageToolbar>
    {#snippet lead()}
      <div class="seg" role="radiogroup" aria-label="Window">
        {#each WINDOWS as w (w)}
          <button
            type="button"
            class="chip"
            class:on={data.days === w}
            role="radio"
            aria-checked={data.days === w}
            onclick={() => applyWindow(w)}
          >
            {w}d
          </button>
        {/each}
      </div>
    {/snippet}
  </PageToolbar>

  <div class="card rpc-card">
    <div class="card-head">
      <h3>RPC round-trip latency</h3>
      <span class="more">uncached no-op over NATS · refreshes every 30s</span>
    </div>
    <p class="rpc-note">
      Measures the full admin → local leaf → service → reply path. A timeout means the route or responder is unavailable.
    </p>
    {#await data.health}
      <div class="health-grid">
        {#each Array.from({ length: 11 }) as _, i (i)}
          <Skeleton variant="block" height="42px" />
        {/each}
      </div>
    {:then initialHealth}
      {@const health = liveHealth ?? initialHealth}
      {#if health.length === 0}
        <p class="rpc-empty">RPC probes unavailable.</p>
      {:else}
        <div class="health-grid">
          {#each health as h (h.id)}
            <div
              class="health-cell"
              class:slow={h.ok && h.ms >= 250}
              class:down={!h.ok}
              title={h.error ?? `${h.ms} ms round trip`}
            >
              <span class="health-dot" class:warn={h.ok && h.ms >= 250} class:err={!h.ok}></span>
              <span class="health-name">{h.label}</span>
              <span class="health-ms">
                {h.ok ? `${h.ms} ms` : (h.error?.includes('timeout') ? 'timeout' : 'down')}
              </span>
            </div>
          {/each}
        </div>
      {/if}
    {/await}
  </div>

  {#if bundle === null}
    <div class="loading-stack">
      <Skeleton variant="block" height="320px" />
      <Skeleton variant="block" height="180px" />
    </div>
  {:else}
    {#if bundle.degraded}
      <AlertBanner>Users service unreachable; showing sample analytics, not live data.</AlertBanner>
    {/if}

    {#if enrollment}
      <div class="card">
        <div class="card-head"><h3>Enrollment — last {enrollment.days.length} days</h3></div>
        <EnrollmentChart {enrollment} />
      </div>

      <div class="grid-2 ana-grid">
        <div class="card">
          <div class="card-head"><h3>Base composition</h3></div>

          {#if stats}
            <div class="comp">
              <div class="comp-block">
                <span class="comp-label">Tier — {stats.total_users.toLocaleString()} users</span>
                <div class="comp-bar" role="img" aria-label="Tier split">
                  {#each tiers as t (t.id)}
                    <div class="comp-seg seg-{t.id}" style="width:{pct(t.count, stats.total_users)}%"></div>
                  {/each}
                </div>
                <div class="comp-legend">
                  {#each tiers as t (t.id)}
                    <span class="leg"><i class="sw seg-{t.id}"></i>{t.label}
                      <b>{t.count.toLocaleString()}</b>
                      <em>{pct(t.count, stats.total_users).toFixed(1)}%</em>
                    </span>
                  {/each}
                </div>
              </div>

              <div class="comp-block">
                <span class="comp-label">Activity</span>
                <div class="comp-bar" role="img" aria-label="Active share">
                  {#each activity as a (a.id)}
                    <div class="comp-seg seg-{a.id}" style="width:{pct(a.count, stats.total_users)}%"></div>
                  {/each}
                </div>
                <div class="comp-legend">
                  {#each activity as a (a.id)}
                    <span class="leg"><i class="sw seg-{a.id}"></i>{a.label}
                      <b>{a.count.toLocaleString()}</b>
                      <em>{pct(a.count, stats.total_users).toFixed(1)}%</em>
                    </span>
                  {/each}
                </div>
              </div>
            </div>
          {/if}
        </div>

        <div class="card">
          <div class="card-head"><h3>Signup patterns</h3></div>

          <div class="pattern-block">
            <span class="comp-label">Average signups per weekday</span>
            <div class="wk-row">
              {#each weekdayAvgs as w (w.label)}
                <div class="wk-col">
                  <span class="wk-val">{w.avg.toFixed(1)}</span>
                  <div class="wk-track">
                    <div class="wk-fill" style="height:{(w.avg / weekdayMax) * 100}%"></div>
                  </div>
                  <span class="wk-label">{w.label}</span>
                </div>
              {/each}
            </div>
          </div>

          <div class="pattern-block">
            <span class="comp-label">Biggest days in this window</span>
            {#if topDays.length === 0}
              <p class="pattern-empty">No signups in this window.</p>
            {:else}
              <ul class="top-list">
                {#each topDays as d, i (d.date)}
                  <li class="top-row">
                    <span class="top-rank">#{i + 1}</span>
                    <span class="top-date">{fmtDate(d.date)}</span>
                    <span class="top-count">{d.count} signup{d.count === 1 ? '' : 's'}</span>
                  </li>
                {/each}
              </ul>
            {/if}
          </div>
        </div>
      </div>
    {/if}
  {/if}
</section>

<style>
  .loading-stack { display: flex; flex-direction: column; gap: 14px; }
  .rpc-card { margin-bottom: var(--row-gap); }
  .rpc-note, .rpc-empty {
    margin: -2px 0 14px;
    font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted);
  }
  .rpc-empty { margin-bottom: 0; }
  .health-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
    gap: 10px;
  }
  .health-cell {
    display: flex; align-items: center; gap: 10px;
    padding: 12px 14px;
    border: 1px solid var(--rule); border-radius: 8px;
    background: rgba(240, 236, 228, 0.02);
  }
  .health-cell.slow { border-color: rgba(202, 167, 106, 0.35); background: rgba(202, 167, 106, 0.05); }
  .health-cell.down { border-color: rgba(176, 90, 70, 0.35); background: rgba(176, 90, 70, 0.05); }
  .health-dot {
    width: 8px; height: 8px; border-radius: 50%; flex: none;
    background: var(--bb-green-glow); box-shadow: 0 0 8px var(--bb-green-glow);
  }
  .health-dot.warn { background: var(--bb-tan-light); box-shadow: 0 0 8px rgba(202, 167, 106, 0.6); }
  .health-dot.err { background: #cf8a78; box-shadow: 0 0 8px rgba(176, 90, 70, 0.6); }
  .health-name { font-family: var(--bb-font-body); font-weight: 600; font-size: 13px; color: var(--bb-white); }
  .health-ms { margin-left: auto; font-family: var(--bb-font-mono); font-size: 11.5px; color: var(--bb-muted); }
  .health-cell.slow .health-ms { color: var(--bb-tan-light); }
  .health-cell.down .health-ms { color: #cf8a78; }
  .seg { display: flex; gap: 6px; flex-wrap: wrap; }
  .chip {
    font-family: var(--bb-font-mono); font-size: 11px; letter-spacing: 0.06em;
    padding: 8px 14px; border-radius: var(--bb-radius-pill); white-space: nowrap;
    background: rgba(255, 255, 255, 0.03); border: 1px solid var(--glass-border);
    color: var(--bb-muted); cursor: pointer;
  }
  .chip:hover { color: var(--bb-white); border-color: var(--bb-border-strong); }
  .chip.on { color: var(--bb-white); background: var(--ui-accent-soft); border-color: var(--bb-border-strong); }

  .ana-grid { align-items: start; }

  .comp { display: flex; flex-direction: column; gap: 22px; }
  .comp-block, .pattern-block { display: flex; flex-direction: column; gap: 10px; }
  .pattern-block + .pattern-block { margin-top: 20px; }
  .comp-label {
    font-family: var(--bb-font-mono); font-size: 10px; letter-spacing: 0.12em;
    text-transform: uppercase; color: var(--bb-muted);
  }

  /* Stacked composition bar: thin marks, 2px surface gaps between segments. */
  .comp-bar {
    display: flex; gap: 2px; height: 14px; border-radius: 7px; overflow: hidden;
  }
  .comp-seg { min-width: 3px; border-radius: 3px; }
  .seg-vip { background: #d9dee4; }
  .seg-paid { background: var(--bb-tan-light); }
  .seg-free { background: var(--bb-green-glow); }
  .seg-active { background: var(--bb-green-glow); }
  .seg-dormant { background: #8fa8bf; }

  .comp-legend { display: flex; gap: 16px; flex-wrap: wrap; }
  .leg {
    display: inline-flex; align-items: baseline; gap: 6px;
    font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted);
  }
  .leg b { color: var(--bb-white); font-weight: 600; }
  .leg em { font-style: normal; font-family: var(--bb-font-mono); font-size: 11px; }
  .sw { width: 9px; height: 9px; border-radius: 3px; align-self: center; flex: none; }

  .wk-row { display: flex; gap: 10px; align-items: flex-end; }
  .wk-col { display: flex; flex-direction: column; align-items: center; gap: 6px; flex: 1; }
  .wk-val { font-family: var(--bb-font-mono); font-size: 10px; color: var(--bb-muted); }
  .wk-track {
    width: 100%; max-width: 34px; height: 64px; border-radius: 4px;
    background: rgba(240, 236, 228, 0.05);
    display: flex; align-items: flex-end; overflow: hidden;
  }
  .wk-fill { width: 100%; background: var(--bb-green-glow); border-radius: 3px 3px 0 0; min-height: 2px; }
  .wk-label {
    font-family: var(--bb-font-mono); font-size: 10px; letter-spacing: 0.06em;
    text-transform: uppercase; color: var(--bb-muted);
  }

  .top-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; }
  .top-row {
    display: grid; grid-template-columns: 34px 1fr auto; gap: 10px; align-items: baseline;
    padding: 8px 2px; border-bottom: 1px solid var(--rule);
  }
  .top-row:last-child { border-bottom: none; }
  .top-rank { font-family: var(--bb-font-mono); font-size: 11px; color: var(--bb-tan); }
  .top-date { font-family: var(--bb-font-body); font-size: 13px; color: var(--bb-white); }
  .top-count { font-family: var(--bb-font-mono); font-size: 11.5px; color: var(--bb-muted); }

  .pattern-empty { font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); margin: 0; }

  @media (max-width: 900px) {
    .wk-track { height: 48px; }
  }
</style>
