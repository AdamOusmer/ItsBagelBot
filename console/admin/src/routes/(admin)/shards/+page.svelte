<script lang="ts">
  import { Icon } from '@bagel/shared';
  let { data } = $props();

  const snap = $derived(data.snapshot);
  const cm = $derived(snap.conduit_manager);

  function keepalive(ms?: number): string {
    if (!ms || ms <= 0) return '—';
    return `${Math.round(ms / 1000)}s window`;
  }

  function stateTone(state: string): string {
    if (state === 'connected') return 'green';
    if (state === 'reconnecting' || state === 'connecting') return 'warn';
    return 'err';
  }

  function stateLabel(state: string): string {
    return state;
  }
</script>

<section class="screen active">
  <div class="page-head">
    <span class="eyebrow">Twitch ingress</span>
    <h1>Shard <em>health</em></h1>
    <p>
      {snap.shards.filter((s) => s.state === 'connected').length}/{snap.shard_count || snap.shards.length}
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

  <div class="shard-grid">
    {#each snap.shards as s (s.shard_id)}
      {@const tone = stateTone(s.state)}
      <div class="card shard-card {tone}">
        <div class="shard-head">
          <div class="shard-dot {tone}"></div>
          <span class="shard-id">shard {s.shard_id}</span>
          <span class="shard-node">{s.node}</span>
        </div>
        <div class="shard-state {tone}">{stateLabel(s.state)}</div>
        <div class="shard-meta">
          <span>{s.bound ? 'bound' : 'unbound'}</span>
          {#if s.handshake_in_flight}<span class="warn-tag">handshaking</span>{/if}
          <span>{keepalive(s.keepalive_ms)}</span>
          <span>{s.attempts ?? 0} att</span>
        </div>
        <div class="shard-session">session {s.session_id ?? '—'}</div>
      </div>
    {/each}
  </div>
</section>

<style>
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

  /* shard card grid */
  .shard-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
    gap: 14px;
    margin-top: 18px;
  }

  .shard-card {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 18px;
    border-left-width: 3px !important;
  }
  .shard-card.green { border-left-color: rgba(82,183,136,.6) !important; }
  .shard-card.warn  { border-left-color: rgba(201,168,124,.6) !important; }
  .shard-card.err   { border-left-color: rgba(176,90,70,.5)   !important; }

  .shard-head {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .shard-dot {
    width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0;
  }
  .shard-dot.green { background: var(--bb-green-glow); box-shadow: 0 0 8px var(--bb-green-glow); }
  .shard-dot.warn  { background: var(--bb-tan);        box-shadow: 0 0 8px var(--bb-tan); }
  .shard-dot.err   { background: #cf8a78;               box-shadow: 0 0 8px #cf8a78; }

  .shard-id {
    font-family: var(--bb-font-mono);
    font-size: 13px;
    font-weight: 600;
    color: var(--bb-white);
  }
  .shard-node {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
    margin-left: auto;
  }

  .shard-state {
    font-family: var(--bb-font-mono);
    font-size: 12px;
    letter-spacing: .08em;
    text-transform: uppercase;
  }
  .shard-state.green { color: var(--bb-green-glow); }
  .shard-state.warn  { color: var(--bb-tan-light); }
  .shard-state.err   { color: #cf8a78; }

  .shard-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 10px;
    font-family: var(--bb-font-mono);
    font-size: 11px;
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

  .shard-session {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    color: var(--bb-muted);
    opacity: .55;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  @media (max-width: 760px) {
    .shard-grid { grid-template-columns: 1fr; }
    .conduit-card { padding: 16px; }
  }

  @media (max-width: 380px) {
    .shard-grid { grid-template-columns: 1fr; }
  }
</style>
