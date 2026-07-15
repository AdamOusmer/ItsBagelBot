<script lang="ts">
  import { enhance } from '$app/forms';
  import { onMount, untrack } from 'svelte';
  import type { SubmitFunction } from '@sveltejs/kit';
  import { Icon, PageHead, AlertBanner, Skeleton, toast } from '@bagel/shared';
  import type { ShardSnapshot } from '@bagel/shared';
  import {
    barWidth,
    eventsPerSecond,
    resolveCapacity,
    utilizationPct,
    utilizationTone
  } from '$lib/throughput';

  let { data } = $props();

  // ── Streamed snapshot -> local state + live poll ───────────────────────────
  let snap = $state<ShardSnapshot | null>(null);
  let degraded = $state(false);
  let live = $state(false);
  $effect(() => {
    let alive = true;
    data.bundle.then((b) => {
      if (!alive) return;
      // The poll may already have delivered something fresher than SSR.
      if (snap === null) snap = b.snapshot;
      degraded = b.degraded;
    });
    return () => {
      alive = false;
    };
  });

  // Poll: 2s while a recent action settles, else 8s; paused when hidden.
  let fastUntil = 0;
  async function pollSnapshot() {
    if (typeof document !== 'undefined' && document.hidden) return;
    try {
      const res = await fetch('/shards/snapshot');
      if (!res.ok) return;
      const body = (await res.json()) as { snapshot?: ShardSnapshot };
      if (body.snapshot) {
        snap = body.snapshot;
        degraded = false;
        live = true;
        sampleLoads(body.snapshot);
      } else {
        live = false; // endpoint answered but had no live snapshot — say so
      }
    } catch {
      live = false;
    }
  }

  onMount(() => {
    let timer: ReturnType<typeof setTimeout>;
    const tick = async () => {
      await pollSnapshot();
      timer = setTimeout(tick, Date.now() < fastUntil ? 2000 : 8000);
    };
    timer = setTimeout(tick, 1500);
    const onVis = () => {
      if (!document.hidden) pollSnapshot();
    };
    document.addEventListener('visibilitychange', onVis);
    return () => {
      clearTimeout(timer);
      document.removeEventListener('visibilitychange', onVis);
    };
  });

  // ── Per-shard load sparkline (client-side ring buffer over the poll) ──────
  const SPARK_LEN = 30;
  let sparks = $state<Record<number, number[]>>({});
  function sampleLoads(s: ShardSnapshot) {
    const next: Record<number, number[]> = { ...sparks };
    for (const sh of s.shards) {
      const arr = [...(next[sh.shard_id] ?? []), evRate(sh.load)];
      next[sh.shard_id] = arr.slice(-SPARK_LEN);
    }
    sparks = next;
  }
  function sparkPoints(vals: number[]): string {
    if (vals.length < 2) return '';
    const max = Math.max(...vals, 0.001);
    const step = 72 / (vals.length - 1);
    return vals
      .map((v, i) => `${(i * step).toFixed(1)},${(19 - (v / max) * 18).toFixed(1)}`)
      .join(' ');
  }

  // ── Derived views ──────────────────────────────────────────────────────────
  const cm = $derived(snap?.conduit_manager);
  const capacity = $derived(snap ? resolveCapacity(snap) : null);
  const minShards = $derived(snap?.min_shards ?? 1);
  const maxShards = $derived(snap?.max_shards ?? capacity?.websocket_autoscale_max_shards ?? 11);
  const autoscaleOn = $derived(snap?.autoscale ?? false);

  // Scale stepper: tracks user edits; resets when the authoritative base moves.
  const scaleBase = $derived(snap?.desired_count ?? snap?.shard_count ?? 1);
  let scaleOffset = $state(0);
  const scaleCount = $derived(scaleBase + scaleOffset);
  let lastScaleBase = $state<number | null>(null);
  $effect(() => {
    const nextBase = scaleBase;
    untrack(() => {
      if (lastScaleBase !== nextBase) {
        lastScaleBase = nextBase;
        if (scaleOffset !== 0) scaleOffset = 0;
      }
    });
  });

  function evRate(load?: number): number {
    return capacity ? eventsPerSecond(load, capacity.load_window_seconds) : 0;
  }
  function loadUtilization(load?: number): number {
    return capacity ? utilizationPct(evRate(load), capacity.websocket_rated_eps) : 0;
  }
  function loadTone(load?: number): string {
    if (load == null || !capacity) return 'muted';
    return utilizationTone(loadUtilization(load), capacity.target_utilization_pct);
  }
  function rateLabel(eps: number): string {
    if (eps <= 0) return '0 ev/s';
    if (eps < 1) return `${eps.toFixed(2)} ev/s`;
    return `${eps.toFixed(eps < 10 ? 1 : 0)} ev/s`;
  }
  function keepalive(ms?: number): string {
    if (!ms || ms <= 0) return '—';
    return `${Math.round(ms / 1000)}s window`;
  }
  function stateBadge(state: string): { label: string; tone: string } {
    if (state === 'connected') return { label: 'healthy', tone: 'green' };
    if (state === 'reconnecting' || state === 'connecting') return { label: 'restarting', tone: 'warn' };
    return { label: 'degraded', tone: 'err' };
  }
  function podIndex(raw?: string): string {
    if (!raw || !snap) return '';
    const index = snap.nodes.findIndex((node) => node === String(raw));
    return index >= 0 ? `pod${index + 1}` : '';
  }

  const aggregateEps = $derived(
    (snap?.shards ?? []).reduce((sum, s) => sum + evRate(s.load), 0)
  );
  const aggregateUtilization = $derived(
    capacity ? utilizationPct(aggregateEps, capacity.effective_rated_eps) : 0
  );

  // ── Actions: apply the echoed snapshot; autoscale flips optimistically ─────
  type ActionPayload = { action?: { ok: boolean; notice: string }; snapshot?: ShardSnapshot; error?: string };
  function payloadOf(result: unknown): ActionPayload | undefined {
    const r = result as { type: string; data?: ActionPayload };
    return r.type === 'success' || r.type === 'failure' ? r.data : undefined;
  }

  let busy = $state(false);

  const scaleSubmit: SubmitFunction = ({ formData }) => {
    formData.set('count', String(scaleCount));
    busy = true;
    return async ({ result, update }) => {
      busy = false;
      fastUntil = Date.now() + 30_000;
      const p = payloadOf(result);
      if (result.type === 'success' && p?.action?.ok) {
        if (p.snapshot) snap = p.snapshot;
        toast('ok', p.action.notice);
      } else {
        toast('err', p?.action?.notice ?? p?.error ?? 'scale failed');
      }
      await update({ reset: false });
    };
  };

  const autoscaleSubmit: SubmitFunction = () => {
    busy = true;
    const before = snap ? { ...snap } : null;
    if (snap) snap = { ...snap, autoscale: !snap.autoscale };
    return async ({ result, update }) => {
      busy = false;
      fastUntil = Date.now() + 30_000;
      const p = payloadOf(result);
      if (result.type === 'success' && p?.action?.ok) {
        if (p.snapshot) snap = p.snapshot;
        toast('ok', p.action.notice);
      } else {
        // Roll the flip back — the toggle must show what the fleet actually runs.
        if (before) snap = before;
        toast('err', p?.action?.notice ?? p?.error ?? 'autoscale change failed');
      }
      await update({ reset: false });
    };
  };
</script>

<section class="screen active">
  <div class="page-head">
    <span class="eyebrow">
      Twitch ingress
      {#if live}<span class="live-chip"><span class="live-dot"></span> live</span>{/if}
    </span>
    <h1>Shard <em>health</em></h1>
    {#if snap}
      <p>
        {snap.shards.filter((s) => s.state === 'connected').length}/{snap.shard_count || snap.shards.length}
        connected across {snap.nodes.length} nodes · reporter {snap.reporter}
      </p>
    {:else}
      <p>Waiting for the first fleet snapshot…</p>
    {/if}
  </div>

  {#if degraded}
    <AlertBanner>Live snapshot unavailable; showing sample data until the ingress answers.</AlertBanner>
  {/if}

  {#if snap === null}
    <div class="loading-stack">
      <Skeleton variant="block" height="88px" />
      <Skeleton variant="block" height="88px" />
      <Skeleton variant="block" height="160px" />
    </div>
  {:else}
    <div class="top-grid">
      <div class="card conduit-card">
        <div class="card-head">
          <h3>Conduit manager</h3>
          <span class="more">{snap.nodes.join(', ') || 'no nodes'}</span>
        </div>
        <div class="conduit-row">
          <div class="fi {cm?.state === 'leader' ? 'green' : ''}"><Icon name="overview" size={18} /></div>
          <div class="conduit-body">
            <div class="live-tag"><span class="dot"></span> {cm?.state ?? 'unknown'}</div>
            <div class="meta">
              <span>node {cm?.node ?? '—'}</span><span class="mid">·</span>
              <span>conduit {cm?.conduit_id ?? '—'}</span>
            </div>
          </div>
        </div>
      </div>

      {#if capacity}
        <div class="card conduit-card">
          <div class="card-head">
            <h3>Effective capacity</h3>
            <span class="more">limited by {capacity.bottleneck === 'nats' ? 'live NATS PubAck' : 'ingress compute'}</span>
          </div>
          <div class="load-row">
            <div class="load-bar-track">
              <div
                class="load-bar-fill {utilizationTone(aggregateUtilization, capacity.target_utilization_pct)}"
                style="width:{barWidth(aggregateUtilization)}%"
              ></div>
            </div>
            <span class="load-pct {utilizationTone(aggregateUtilization, capacity.target_utilization_pct)}">
              {rateLabel(aggregateEps)} · {aggregateUtilization.toFixed(1)}%
            </span>
          </div>
          <div class="shard-meta">
            <span>target {capacity.effective_target_eps.toLocaleString()} ev/s ({capacity.target_utilization_pct}%)</span>
            <span>rated {capacity.effective_rated_eps.toLocaleString()} ev/s</span>
            <span>NATS {capacity.nats_rated_eps.toLocaleString()} · compute {capacity.fleet_rated_eps.toLocaleString()} ev/s</span>
          </div>
        </div>
      {/if}
    </div>

    <!-- Scale control -->
    <div class="card control-card">
      <div class="card-head">
        <h3>Scale control</h3>
        <span class="badge {autoscaleOn ? 'badge-on' : 'badge-off'}">
          {autoscaleOn ? 'autoscale on' : 'manual'}
        </span>
      </div>

      <div class="ctrl-stats">
        <div class="ctrl-stat"><span class="ctrl-label">desired</span><span class="ctrl-val">{snap.desired_count ?? '—'}</span></div>
        <div class="ctrl-stat"><span class="ctrl-label">target</span><span class="ctrl-val">{snap.target ?? '—'}</span></div>
        <div class="ctrl-stat"><span class="ctrl-label">min</span><span class="ctrl-val">{snap.min_shards ?? '—'}</span></div>
        <div class="ctrl-stat"><span class="ctrl-label">max</span><span class="ctrl-val">{maxShards}</span></div>
      </div>

      <!-- Manual scale: input stays submittable while autoscale is on (visually
           muted) so an operator can pre-set the floor before disabling it. -->
      <form method="POST" action="?/scale" use:enhance={scaleSubmit} class="ctrl-form">
        <div class="stepper {autoscaleOn ? 'stepper-dim' : ''}">
          <button
            type="button"
            class="step-btn"
            aria-label="decrease shard count"
            disabled={scaleCount <= minShards}
            onclick={() => scaleOffset--}
          >−</button>
          <input
            type="number"
            name="count"
            class="step-input"
            min={minShards}
            max={maxShards}
            value={scaleCount}
            oninput={(e) => {
              const v = parseInt((e.target as HTMLInputElement).value, 10);
              if (!isNaN(v)) scaleOffset = v - scaleBase;
            }}
            aria-label="shard count"
          />
          <button type="button" class="step-btn" aria-label="increase shard count" disabled={scaleCount >= maxShards} onclick={() => scaleOffset++}>+</button>
        </div>
        <button type="submit" class="btn-apply" disabled={autoscaleOn || busy}>Apply</button>
        {#if autoscaleOn}<span class="ctrl-hint">disable autoscale to set manually</span>{/if}
      </form>

      <form method="POST" action="?/autoscale" use:enhance={autoscaleSubmit} class="autoscale-form">
        <input type="hidden" name="enabled" value={autoscaleOn ? 'false' : 'true'} />
        <button type="submit" class="btn-toggle {autoscaleOn ? 'btn-toggle-on' : 'btn-toggle-off'}" disabled={busy}>
          <span class="toggle-dot"></span>
          {autoscaleOn ? 'Disable autoscale' : 'Enable autoscale'}
        </button>
      </form>
    </div>

    <div class="shard-grid">
      {#each snap.shards as s (s.shard_id)}
        {@const sb = stateBadge(s.state)}
        {@const utilization = loadUtilization(s.load)}
        {@const ltone = loadTone(s.load)}
        <div class="card shard-card">
          <div class="shard-head">
            <span class="shard-id">shard {s.shard_id}</span>
            <span class="state-badge {sb.tone}">{sb.label}</span>
            <span class="shard-node">
              {s.host || 'unknown-host'}
              {#if podIndex(s.node)}<span class="pod">({podIndex(s.node)})</span>{/if}
            </span>
          </div>
          <div class="shard-meta">
            <span>{s.bound ? 'bound' : 'unbound'}</span>
            {#if s.handshake_in_flight}<span class="warn-tag">handshaking</span>{/if}
            <span>{keepalive(s.keepalive_ms)}</span>
            <span>{s.attempts ?? 0} att</span>
          </div>
          {#if s.load != null && capacity}
            <div class="load-row">
              <div class="load-bar-track">
                <div class="load-bar-fill {ltone}" style="width:{barWidth(utilization)}%"></div>
              </div>
              <span class="load-pct {ltone}">{rateLabel(evRate(s.load))} · {utilization.toFixed(1)}% of socket</span>
              {#if sparkPoints(sparks[s.shard_id] ?? [])}
                <svg class="spark {ltone}" viewBox="0 0 72 20" aria-hidden="true">
                  <polyline points={sparkPoints(sparks[s.shard_id] ?? [])} />
                </svg>
              {/if}
            </div>
          {/if}
          {#if capacity}
            <div class="shard-meta">
              <span>autoscale at {capacity.websocket_target_eps.toLocaleString()} ev/s ({capacity.target_utilization_pct}%)</span>
              <span>rated {capacity.websocket_rated_eps.toLocaleString()} ev/s</span>
            </div>
          {/if}
          <div class="shard-session">session {s.session_id ?? '—'}</div>
        </div>
      {/each}
    </div>
  {/if}
</section>

<style>
  .loading-stack { display: flex; flex-direction: column; gap: 14px; }

  .live-chip {
    display: inline-flex; align-items: center; gap: 5px; margin-left: 8px;
    color: var(--bb-green-glow); letter-spacing: 0.08em;
  }
  .live-dot {
    width: 6px; height: 6px; border-radius: 50%;
    background: var(--bb-green-glow); box-shadow: 0 0 8px var(--bb-green-glow);
    animation: live-pulse 2.4s ease-in-out infinite;
  }
  @keyframes live-pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.4; } }

  .top-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
  @media (max-width: 900px) { .top-grid { grid-template-columns: 1fr; } }

  .conduit-row { display: flex; align-items: center; gap: 14px; }
  .conduit-body .live-tag {
    display: inline-flex; align-items: center; gap: 8px;
    font-family: var(--bb-font-mono); font-size: 11px; letter-spacing: 0.14em;
    text-transform: uppercase; color: var(--bb-green-glow); margin-bottom: 6px;
  }
  .conduit-body .live-tag .dot {
    width: 7px; height: 7px; border-radius: 50%;
    background: var(--bb-green-glow); box-shadow: 0 0 8px var(--bb-green-glow);
    animation: live-pulse 2.4s ease-in-out infinite;
  }
  .conduit-body .meta { font-family: var(--bb-font-mono); font-size: 12px; color: var(--bb-muted); display: flex; gap: 8px; }
  .conduit-body .meta .mid { color: var(--bb-border-strong); }

  .fi {
    width: 36px; height: 36px; border-radius: 8px; flex-shrink: 0;
    display: flex; align-items: center; justify-content: center;
    background: rgba(201, 168, 124, 0.1); border: 1px solid rgba(201, 168, 124, 0.26);
  }
  .fi :global(svg) { width: 18px; height: 18px; stroke: var(--bb-tan-light); fill: none; stroke-width: 1.6; }
  .fi.green { background: rgba(82, 183, 136, 0.1); border-color: rgba(82, 183, 136, 0.28); }
  .fi.green :global(svg) { stroke: var(--bb-green-glow); }

  .control-card { margin-top: 16px; }
  .badge {
    font-family: var(--bb-font-mono); font-size: 10px; letter-spacing: 0.1em;
    text-transform: uppercase; padding: 2px 8px; border-radius: 8px;
  }
  .badge-on { color: var(--bb-green-glow); background: rgba(82, 183, 136, 0.12); border: 1px solid rgba(82, 183, 136, 0.32); }
  .badge-off { color: var(--bb-muted); background: rgba(255, 255, 255, 0.04); border: 1px solid var(--bb-border); }

  .ctrl-stats { display: flex; gap: 28px; margin-bottom: 18px; }
  .ctrl-stat { display: flex; flex-direction: column; gap: 3px; }
  .ctrl-label {
    font-family: var(--bb-font-mono); font-size: 10px; letter-spacing: 0.12em;
    text-transform: uppercase; color: var(--bb-muted);
  }
  .ctrl-val { font-family: var(--bb-font-mono); font-size: 22px; font-weight: 600; color: var(--bb-white); line-height: 1; }

  .ctrl-form { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; margin-bottom: 14px; }
  .stepper {
    display: flex; align-items: center;
    border: 1px solid var(--bb-border-strong); border-radius: 8px; overflow: hidden;
    transition: opacity 0.15s;
  }
  .stepper-dim { opacity: 0.45; }
  .step-btn {
    background: rgba(255, 255, 255, 0.04); border: none; color: var(--bb-white);
    font-family: var(--bb-font-mono); font-size: 18px; width: 36px; height: 36px;
    cursor: pointer; line-height: 1;
  }
  .step-btn:hover:not(:disabled) { background: rgba(255, 255, 255, 0.09); }
  .step-btn:disabled { opacity: 0.3; cursor: not-allowed; }
  .step-input {
    width: 52px; height: 36px; text-align: center; background: transparent; border: none;
    border-left: 1px solid var(--bb-border-strong); border-right: 1px solid var(--bb-border-strong);
    color: var(--bb-white); font-family: var(--bb-font-mono); font-size: 15px; font-weight: 600;
    -moz-appearance: textfield; appearance: textfield;
  }
  .step-input::-webkit-outer-spin-button,
  .step-input::-webkit-inner-spin-button { -webkit-appearance: none; margin: 0; }

  .btn-apply {
    height: 36px; padding: 0 18px;
    background: rgba(201, 168, 124, 0.12); border: 1px solid rgba(201, 168, 124, 0.38);
    border-radius: 8px; color: var(--bb-tan-light);
    font-family: var(--bb-font-mono); font-size: 12px; letter-spacing: 0.1em; text-transform: uppercase;
    cursor: pointer; transition: background 0.12s, opacity 0.12s;
  }
  .btn-apply:hover:not(:disabled) { background: rgba(201, 168, 124, 0.22); }
  .btn-apply:disabled { opacity: 0.3; cursor: not-allowed; }
  .ctrl-hint { font-family: var(--bb-font-mono); font-size: 11px; color: var(--bb-muted); opacity: 0.7; }

  .btn-toggle {
    display: inline-flex; align-items: center; gap: 10px; height: 34px; padding: 0 16px;
    border-radius: 8px; font-family: var(--bb-font-mono); font-size: 12px; letter-spacing: 0.08em;
    cursor: pointer; transition: background 0.12s;
  }
  .btn-toggle:disabled { opacity: 0.5; cursor: not-allowed; }
  .btn-toggle-off { background: rgba(82, 183, 136, 0.08); border: 1px solid rgba(82, 183, 136, 0.28); color: var(--bb-green-glow); }
  .btn-toggle-off:hover:not(:disabled) { background: rgba(82, 183, 136, 0.16); }
  .btn-toggle-on { background: rgba(176, 90, 70, 0.08); border: 1px solid rgba(176, 90, 70, 0.28); color: #cf8a78; }
  .btn-toggle-on:hover:not(:disabled) { background: rgba(176, 90, 70, 0.16); }
  .toggle-dot { width: 8px; height: 8px; border-radius: 50%; }
  .btn-toggle-off .toggle-dot { background: var(--bb-green-glow); box-shadow: 0 0 6px var(--bb-green-glow); }
  .btn-toggle-on .toggle-dot { background: #cf8a78; box-shadow: 0 0 6px #cf8a78; }

  .shard-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
    gap: 18px;
    margin-top: 22px;
  }
  .shard-card { display: flex; flex-direction: column; gap: 13px; min-height: 154px; padding: 22px; }
  .shard-head { display: grid; grid-template-columns: minmax(0, 1fr) auto auto; align-items: center; gap: 10px; }
  .shard-id { font-family: var(--bb-font-mono); font-size: 15px; font-weight: 600; color: var(--bb-white); min-width: 0; }
  .shard-node {
    display: inline-flex; align-items: center; justify-content: center; gap: 4px;
    font-family: var(--bb-font-mono); font-size: 11px; letter-spacing: 0.08em; text-transform: uppercase;
    color: var(--bb-tan-light); background: rgba(201, 168, 124, 0.1); border: 1px solid rgba(201, 168, 124, 0.28);
    border-radius: var(--bb-radius-pill); padding: 3px 8px; white-space: nowrap;
  }
  .shard-node .pod { opacity: 0.6; }

  .state-badge {
    font-family: var(--bb-font-mono); font-size: 10px; letter-spacing: 0.1em; text-transform: uppercase;
    padding: 2px 9px; border-radius: var(--bb-radius-pill); border: 1px solid transparent;
  }
  .state-badge.green { color: var(--bb-green-glow); background: rgba(82, 183, 136, 0.12); border-color: rgba(82, 183, 136, 0.32); }
  .state-badge.warn { color: var(--bb-tan-light); background: rgba(201, 168, 124, 0.12); border-color: rgba(201, 168, 124, 0.32); }
  .state-badge.err { color: #cf8a78; background: rgba(176, 90, 70, 0.12); border-color: rgba(176, 90, 70, 0.35); }

  .shard-meta { display: flex; flex-wrap: wrap; gap: 6px 12px; font-family: var(--bb-font-mono); font-size: 12px; color: var(--bb-muted); }
  .warn-tag {
    color: var(--bb-tan-light); background: rgba(201, 168, 124, 0.1);
    border: 1px solid rgba(201, 168, 124, 0.28); border-radius: 8px; padding: 1px 6px; font-size: 10px;
  }

  .load-row { display: flex; align-items: center; gap: 10px; }
  .load-bar-track { flex: 1; height: 7px; border-radius: 8px; background: rgba(255, 255, 255, 0.08); overflow: hidden; }
  .load-bar-fill { height: 100%; border-radius: 8px; transition: width 0.3s ease; }
  .load-bar-fill.green { background: var(--bb-green-glow); }
  .load-bar-fill.warn { background: var(--bb-tan); }
  .load-bar-fill.err { background: #cf8a78; }
  .load-bar-fill.muted { background: rgba(255, 255, 255, 0.18); }
  .load-pct { font-family: var(--bb-font-mono); font-size: 10px; min-width: 30px; text-align: right; }
  .load-pct.green { color: var(--bb-green-glow); }
  .load-pct.warn { color: var(--bb-tan-light); }
  .load-pct.err { color: #cf8a78; }
  .load-pct.muted { color: var(--bb-muted); }

  .spark { width: 72px; height: 20px; flex: none; }
  .spark polyline { fill: none; stroke-width: 1.5; stroke: rgba(255, 255, 255, 0.35); }
  .spark.green polyline { stroke: var(--bb-green-glow); }
  .spark.warn polyline { stroke: var(--bb-tan); }
  .spark.err polyline { stroke: #cf8a78; }

  .shard-session {
    font-family: var(--bb-font-mono); font-size: 12px; color: var(--bb-muted); opacity: 0.7;
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }

  @media (max-width: 760px) {
    .shard-grid { grid-template-columns: 1fr; }
    .conduit-card { padding: 16px; }
    .ctrl-stats { gap: 18px; }
  }
  @media (max-width: 380px) {
    .ctrl-form { flex-direction: column; align-items: flex-start; }
  }
</style>
