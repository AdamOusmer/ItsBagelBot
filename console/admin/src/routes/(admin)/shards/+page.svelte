<script lang="ts">
  import { enhance } from '$app/forms';
  import { onMount, untrack } from 'svelte';
  import { Icon } from '@bagel/shared';
  import type { ActionData } from './$types';

  let { data, form }: { data: any; form: ActionData } = $props();

  // Live snapshot override: null means "use data.snapshot from load()".
  // The page polls /shards/snapshot so shard state (connecting -> connected) and
  // scale/delete results show in near-real-time without a manual refresh.
  let snapOverride = $state<typeof data.snapshot | null>(null);
  const snap = $derived(snapOverride ?? data.snapshot);
  let live = $state(false);

  // Reflect the snapshot returned by a successful action, then poll faster for a
  // short window so spin-up / teardown transitions land quickly.
  let fastUntil = 0;
  function applyActionSnapshot(result: ActionData) {
    if (result && 'snapshot' in result && result.snapshot) {
      snapOverride = result.snapshot;
    }
    fastUntil = Date.now() + 30_000;
  }

  async function pollSnapshot() {
    if (typeof document !== 'undefined' && document.hidden) return;
    try {
      const res = await fetch('/shards/snapshot');
      if (!res.ok) return;
      const body = (await res.json()) as { snapshot?: typeof data.snapshot };
      if (body.snapshot) {
        snapOverride = body.snapshot;
        live = true;
      }
    } catch {
      /* transient; keep last good snapshot */
    }
  }

  onMount(() => {
    let timer: ReturnType<typeof setTimeout>;
    // Self-scheduling loop: 2s while a recent action is settling, else 8s.
    const tick = async () => {
      await pollSnapshot();
      timer = setTimeout(tick, Date.now() < fastUntil ? 2000 : 8000);
    };
    timer = setTimeout(tick, 2000);
    const onVis = () => {
      if (!document.hidden) pollSnapshot();
    };
    document.addEventListener('visibilitychange', onVis);
    return () => {
      clearTimeout(timer);
      document.removeEventListener('visibilitychange', onVis);
    };
  });

  const cm = $derived(snap.conduit_manager);

  // Scale stepper: tracks user edits; resets when snap changes.
  let scaleBase = $derived(snap.desired_count ?? snap.shard_count);
  let scaleOffset = $state(0);
  const scaleCount = $derived(scaleBase + scaleOffset);

  function stepDown() { if (scaleCount > (snap.min_shards ?? 1)) scaleOffset--; }
  function stepUp()   { scaleOffset++; }
  // Reset offset when the base changes (action result or navigation).
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

  const minShards = $derived(snap.min_shards ?? 1);
  const autoscaleOn = $derived(snap.autoscale ?? false);

  function keepalive(ms?: number): string {
    if (!ms || ms <= 0) return '—';
    return `${Math.round(ms / 1000)}s window`;
  }

  // Map raw shard state to an operator-facing health badge.
  function stateBadge(state: string): { label: string; tone: string } {
    if (state === 'connected') return { label: 'healthy', tone: 'green' };
    if (state === 'reconnecting' || state === 'connecting') return { label: 'restarting', tone: 'warn' };
    return { label: 'degraded', tone: 'err' };
  }

  // `s.load` from ingress is a RAW count of notifications in the last 60s
  // window per shard (ShardSession event_times length), not a 0..1 fraction.
  // Treating it as a fraction made any real traffic read as 100%/red. We
  // normalise against a generous nominal shard capacity for the bar, and show
  // the honest absolute throughput (events/sec) alongside it.
  //
  // LOAD_WINDOW_S mirrors ingress @load_window_ms (60s). SHARD_CAPACITY_EPS is
  // a display-only ceiling: a single shard handles far more than the
  // autoscaler's scale-up trigger (~0.83 ev/s), so we size the "full" bar at
  // 100 ev/s. This is for human gauge only; it does not drive autoscaling.
  const LOAD_WINDOW_S = 60;
  const SHARD_CAPACITY_EPS = 100; // events/sec considered a full bar

  function evRate(load?: number): number {
    if (load == null || load <= 0) return 0;
    return load / LOAD_WINDOW_S;
  }

  function loadBar(load?: number): number {
    const rate = evRate(load);
    if (rate <= 0) return 0;
    return Math.min(100, Math.round((rate / SHARD_CAPACITY_EPS) * 100));
  }

  function loadLabel(load?: number): string {
    const rate = evRate(load);
    if (rate <= 0) return '0 ev/s';
    if (rate < 1) return `${rate.toFixed(2)} ev/s`;
    return `${rate.toFixed(rate < 10 ? 1 : 0)} ev/s`;
  }

  function loadTone(load?: number): string {
    if (load == null) return 'muted';
    const frac = evRate(load) / SHARD_CAPACITY_EPS;
    if (frac >= 0.75) return 'err';
    if (frac >= 0.5) return 'warn';
    return 'green';
  }

  function podIndex(raw?: string): string {
    if (!raw) return '';
    const index = snap.nodes.findIndex((node: string) => node === String(raw));
    if (index >= 0) return `pod${index + 1}`;
    return '';
  }
</script>

<section class="screen active">
  <div class="page-head">
    <span class="eyebrow">Twitch ingress {#if live}<span class="live-chip"><span class="live-dot"></span> live</span>{/if}</span>
    <h1>Shard <em>health</em></h1>
    <p>
      {snap.shards.filter((s: any) => s.state === 'connected').length}/{snap.shard_count || snap.shards.length}
      connected across {snap.nodes.length} nodes · reporter {snap.reporter}.{#if data.degraded}
        <em> Live snapshot unavailable; showing sample.</em>{/if}
    </p>
  </div>

  <div class="card conduit-card">
    <div class="card-head">
      <h3>Conduit manager</h3>
      <span class="more">{snap.nodes.join(', ') || 'no nodes'}</span>
    </div>
    <div class="conduit-row">
      <div class="fi {cm?.state === 'leader' ? 'green' : ''}"><Icon name="overview" size={18} /></div>
      <div class="conduit-body">
        <div class="live"><span class="dot"></span> {cm?.state ?? 'unknown'}</div>
        <div class="meta">
          <span>node {cm?.node ?? '—'}</span><span class="mid">·</span>
          <span>conduit {cm?.conduit_id ?? '—'}</span>
        </div>
      </div>
    </div>
  </div>

  <!-- Scale control panel -->
  <div class="card control-card">
    <div class="card-head">
      <h3>Scale control</h3>
      <span class="more">
        {#if autoscaleOn}
          <span class="badge badge-on">autoscale on</span>
        {:else}
          <span class="badge badge-off">manual</span>
        {/if}
      </span>
    </div>

    <div class="ctrl-stats">
      <div class="ctrl-stat">
        <span class="ctrl-label">desired</span>
        <span class="ctrl-val">{snap.desired_count ?? '—'}</span>
      </div>
      <div class="ctrl-stat">
        <span class="ctrl-label">target</span>
        <span class="ctrl-val">{snap.target ?? '—'}</span>
      </div>
      <div class="ctrl-stat">
        <span class="ctrl-label">min</span>
        <span class="ctrl-val">{snap.min_shards ?? '—'}</span>
      </div>
    </div>

    <!-- Manual scale form.
         Input is visually muted when autoscale is on, but still submittable —
         this lets an operator pre-set the floor before disabling autoscale. -->
    <form
      method="POST"
      action="?/scale"
      use:enhance={({ formData }) => {
        formData.set('count', String(scaleCount));
        return async ({ result, update }) => {
          if (result.type === 'success' && result.data) {
            applyActionSnapshot(result.data as ActionData);
          }
          await update({ reset: false });
        };
      }}
      class="ctrl-form"
    >
      <div class="stepper {autoscaleOn ? 'stepper-dim' : ''}">
        <button
          type="button"
          class="step-btn"
          aria-label="decrease shard count"
          disabled={scaleCount <= minShards}
          onclick={stepDown}
        >−</button>

        <input
          type="number"
          name="count"
          class="step-input"
          min={minShards}
          value={scaleCount}
          oninput={(e) => {
            const v = parseInt((e.target as HTMLInputElement).value, 10);
            if (!isNaN(v)) scaleOffset = v - scaleBase;
          }}
          aria-label="shard count"
        />

        <button
          type="button"
          class="step-btn"
          aria-label="increase shard count"
          onclick={stepUp}
        >+</button>
      </div>

      <button type="submit" class="btn-apply" disabled={autoscaleOn}>Apply</button>

      {#if autoscaleOn}
        <span class="ctrl-hint">disable autoscale to set manually</span>
      {/if}
    </form>

    <!-- Autoscale toggle -->
    <form
      method="POST"
      action="?/autoscale"
      use:enhance={() => {
        return async ({ result, update }) => {
          if (result.type === 'success' && result.data) {
            applyActionSnapshot(result.data as ActionData);
          }
          await update({ reset: false });
        };
      }}
      class="autoscale-form"
    >
      <input type="hidden" name="enabled" value={autoscaleOn ? 'false' : 'true'} />
      <button type="submit" class="btn-toggle {autoscaleOn ? 'btn-toggle-on' : 'btn-toggle-off'}">
        <span class="toggle-dot"></span>
        {autoscaleOn ? 'Disable autoscale' : 'Enable autoscale'}
      </button>
    </form>

    {#if form && 'action' in form && form.action}
      <div class="ctrl-notice {form.action.ok ? 'notice-ok' : 'notice-err'}">
        {form.action.notice}
      </div>
    {/if}
    {#if form && 'error' in form && form.error}
      <div class="ctrl-notice notice-err">{form.error}</div>
    {/if}
  </div>

  <div class="shard-grid">
    {#each snap.shards as s (s.shard_id)}
      {@const sb = stateBadge(s.state)}
      {@const pct = loadBar(s.load)}
      {@const ltone = loadTone(s.load)}
      <div class="card shard-card">
        <div class="shard-head">
          <span class="shard-id">shard {s.shard_id}</span>
          <span class="state-badge {sb.tone}">{sb.label}</span>
          <span class="shard-node">
            {s.host || 'unknown-host'}
            {#if podIndex(s.node)}
              <span style="opacity:0.6; margin-left:4px">({podIndex(s.node)})</span>
            {/if}
          </span>
        </div>
        <div class="shard-meta">
          <span>{s.bound ? 'bound' : 'unbound'}</span>
          {#if s.handshake_in_flight}<span class="warn-tag">handshaking</span>{/if}
          <span>{keepalive(s.keepalive_ms)}</span>
          <span>{s.attempts ?? 0} att</span>
        </div>
        {#if s.load != null}
          <div class="load-row">
            <div class="load-bar-track">
              <div class="load-bar-fill {ltone}" style="width:{pct}%"></div>
            </div>
            <span class="load-pct {ltone}">{loadLabel(s.load)} · {pct}%</span>
          </div>
        {/if}
        <div class="shard-session">session {s.session_id ?? '—'}</div>
      </div>
    {/each}
  </div>
</section>

<style>
  /* live auto-refresh chip in the eyebrow */
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

  /* conduit row: side by side icon + detail */
  .conduit-row {
    display: flex;
    align-items: center;
    gap: 14px;
  }
  .conduit-body .live {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: .14em;
    text-transform: uppercase;
    color: var(--bb-green-glow);
    margin-bottom: 6px;
  }
  .conduit-body .live .dot {
    width: 7px; height: 7px; border-radius: 50%;
    background: var(--bb-green-glow);
    box-shadow: 0 0 8px var(--bb-green-glow);
    animation: pulse 2.4s ease-in-out infinite;
  }
  @keyframes pulse { 0%,100%{ opacity:1; } 50%{ opacity:.45; } }
  .conduit-body .meta {
    font-family: var(--bb-font-mono);
    font-size: 12px;
    color: var(--bb-muted);
    display: flex;
    gap: 8px;
  }
  .conduit-body .meta .mid { color: var(--bb-border-strong); }

  .fi {
    width: 36px; height: 36px; border-radius: 9px; flex-shrink: 0;
    display: flex; align-items: center; justify-content: center;
    background: rgba(201,168,124,.10); border: 1px solid rgba(201,168,124,.26);
  }
  .fi :global(svg) { width: 18px; height: 18px; stroke: var(--bb-tan-light); fill: none; stroke-width: 1.6; }
  .fi.green { background: rgba(82,183,136,.10); border-color: rgba(82,183,136,.28); }
  .fi.green :global(svg) { stroke: var(--bb-green-glow); }

  /* ── control card ─────────────────────────────────────────────────────────── */
  .control-card {
    margin-top: 18px;
  }

  .badge {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    letter-spacing: .1em;
    text-transform: uppercase;
    padding: 2px 8px;
    border-radius: 4px;
  }
  .badge-on {
    color: var(--bb-green-glow);
    background: rgba(82,183,136,.12);
    border: 1px solid rgba(82,183,136,.32);
  }
  .badge-off {
    color: var(--bb-muted);
    background: rgba(255,255,255,.04);
    border: 1px solid var(--bb-border);
  }

  .ctrl-stats {
    display: flex;
    gap: 28px;
    margin-bottom: 18px;
  }
  .ctrl-stat {
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .ctrl-label {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    letter-spacing: .12em;
    text-transform: uppercase;
    color: var(--bb-muted);
  }
  .ctrl-val {
    font-family: var(--bb-font-mono);
    font-size: 22px;
    font-weight: 600;
    color: var(--bb-white);
    line-height: 1;
  }

  .ctrl-form {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
    margin-bottom: 14px;
  }

  .stepper {
    display: flex;
    align-items: center;
    gap: 0;
    border: 1px solid var(--bb-border-strong);
    border-radius: 7px;
    overflow: hidden;
    transition: opacity .15s;
  }
  .stepper-dim {
    opacity: .45;
  }

  .step-btn {
    background: rgba(255,255,255,.04);
    border: none;
    color: var(--bb-white);
    font-family: var(--bb-font-mono);
    font-size: 18px;
    width: 36px;
    height: 36px;
    cursor: pointer;
    transition: background .12s;
    line-height: 1;
  }
  .step-btn:hover:not(:disabled) { background: rgba(255,255,255,.09); }
  .step-btn:disabled { opacity: .3; cursor: not-allowed; }

  .step-input {
    width: 52px;
    height: 36px;
    text-align: center;
    background: transparent;
    border: none;
    border-left: 1px solid var(--bb-border-strong);
    border-right: 1px solid var(--bb-border-strong);
    color: var(--bb-white);
    font-family: var(--bb-font-mono);
    font-size: 15px;
    font-weight: 600;
    /* remove browser spinners */
    -moz-appearance: textfield;
    appearance: textfield;
  }
  .step-input::-webkit-outer-spin-button,
  .step-input::-webkit-inner-spin-button { -webkit-appearance: none; margin: 0; }

  .btn-apply {
    height: 36px;
    padding: 0 18px;
    background: rgba(201,168,124,.12);
    border: 1px solid rgba(201,168,124,.38);
    border-radius: 7px;
    color: var(--bb-tan-light);
    font-family: var(--bb-font-mono);
    font-size: 12px;
    letter-spacing: .1em;
    text-transform: uppercase;
    cursor: pointer;
    transition: background .12s, opacity .12s;
  }
  .btn-apply:hover:not(:disabled) { background: rgba(201,168,124,.22); }
  .btn-apply:disabled { opacity: .3; cursor: not-allowed; }

  .ctrl-hint {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
    opacity: .7;
  }

  .autoscale-form { margin-bottom: 4px; }

  .btn-toggle {
    display: inline-flex;
    align-items: center;
    gap: 10px;
    height: 34px;
    padding: 0 16px;
    border-radius: 7px;
    font-family: var(--bb-font-mono);
    font-size: 12px;
    letter-spacing: .08em;
    cursor: pointer;
    transition: background .12s;
  }
  .btn-toggle-off {
    background: rgba(82,183,136,.08);
    border: 1px solid rgba(82,183,136,.28);
    color: var(--bb-green-glow);
  }
  .btn-toggle-off:hover { background: rgba(82,183,136,.16); }
  .btn-toggle-on {
    background: rgba(176,90,70,.08);
    border: 1px solid rgba(176,90,70,.28);
    color: #cf8a78;
  }
  .btn-toggle-on:hover { background: rgba(176,90,70,.16); }

  .toggle-dot {
    width: 8px; height: 8px; border-radius: 50%;
  }
  .btn-toggle-off .toggle-dot {
    background: var(--bb-green-glow);
    box-shadow: 0 0 6px var(--bb-green-glow);
  }
  .btn-toggle-on .toggle-dot {
    background: #cf8a78;
    box-shadow: 0 0 6px #cf8a78;
  }

  .ctrl-notice {
    margin-top: 10px;
    font-family: var(--bb-font-mono);
    font-size: 12px;
    padding: 6px 10px;
    border-radius: 5px;
  }
  .notice-ok {
    color: var(--bb-green-glow);
    background: rgba(82,183,136,.08);
    border: 1px solid rgba(82,183,136,.22);
  }
  .notice-err {
    color: #cf8a78;
    background: rgba(176,90,70,.08);
    border: 1px solid rgba(176,90,70,.22);
  }

  /* ── shard card grid ─────────────────────────────────────────────────────── */
  .shard-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
    gap: 18px;
    margin-top: 22px;
  }

  .shard-card {
    display: flex;
    flex-direction: column;
    gap: 13px;
    min-height: 154px;
    padding: 22px;
  }

  .shard-head {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto auto;
    align-items: center;
    gap: 10px;
  }
  .shard-id {
    font-family: var(--bb-font-mono);
    font-size: 15px;
    font-weight: 600;
    color: var(--bb-white);
    min-width: 0;
  }
  .shard-node {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: .08em;
    text-transform: uppercase;
    color: var(--bb-tan-light);
    background: rgba(201,168,124,.10);
    border: 1px solid rgba(201,168,124,.28);
    border-radius: var(--bb-radius-pill);
    padding: 3px 8px;
    white-space: nowrap;
  }

  .state-badge {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    letter-spacing: .1em;
    text-transform: uppercase;
    padding: 2px 9px;
    border-radius: var(--bb-radius-pill);
    border: 1px solid transparent;
  }
  .state-badge.green { color: var(--bb-green-glow); background: rgba(82,183,136,.12); border-color: rgba(82,183,136,.32); }
  .state-badge.warn  { color: var(--bb-tan-light); background: rgba(201,168,124,.12); border-color: rgba(201,168,124,.32); }
  .state-badge.err   { color: #cf8a78; background: rgba(176,90,70,.12); border-color: rgba(176,90,70,.35); }

  .shard-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 6px 12px;
    font-family: var(--bb-font-mono);
    font-size: 12px;
    color: var(--bb-muted);
  }

  .warn-tag {
    color: var(--bb-tan-light);
    background: rgba(201,168,124,.10);
    border: 1px solid rgba(201,168,124,.28);
    border-radius: 4px;
    padding: 1px 6px;
    font-size: 10px;
  }

  /* load bar */
  .load-row {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .load-bar-track {
    flex: 1;
    height: 7px;
    border-radius: 4px;
    background: rgba(255,255,255,.08);
    overflow: hidden;
  }
  .load-bar-fill {
    height: 100%;
    border-radius: 4px;
    transition: width .3s ease;
  }
  .load-bar-fill.green { background: var(--bb-green-glow); }
  .load-bar-fill.warn  { background: var(--bb-tan); }
  .load-bar-fill.err   { background: #cf8a78; }
  .load-bar-fill.muted { background: rgba(255,255,255,.18); }

  .load-pct {
    font-family: var(--bb-font-mono);
    font-size: 10px;
    min-width: 30px;
    text-align: right;
  }
  .load-pct.green { color: var(--bb-green-glow); }
  .load-pct.warn  { color: var(--bb-tan-light); }
  .load-pct.err   { color: #cf8a78; }
  .load-pct.muted { color: var(--bb-muted); }

  .shard-session {
    font-family: var(--bb-font-mono);
    font-size: 12px;
    color: var(--bb-muted);
    opacity: .7;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  @media (max-width: 760px) {
    .shard-grid { grid-template-columns: 1fr; }
    .conduit-card { padding: 16px; }
    .ctrl-stats { gap: 18px; }
  }

  @media (max-width: 380px) {
    .shard-grid { grid-template-columns: 1fr; }
    .ctrl-form { flex-direction: column; align-items: flex-start; }
  }
</style>
