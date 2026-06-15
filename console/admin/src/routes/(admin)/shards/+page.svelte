<script lang="ts">
  import { Icon } from '@bagel/shared';
  let { data } = $props();

  const snap = $derived(data.snapshot);
  const cm = $derived(snap.conduit_manager);

  function keepalive(ms?: number): string {
    if (!ms || ms <= 0) return '—';
    return `${Math.round(ms / 1000)}s window`;
  }
  function tone(state: string): string {
    return state === 'connected' ? 'green' : '';
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

  <div class="card">
    <div class="card-head"><h3>Conduit manager</h3><span class="more">{snap.nodes.join(', ') || 'no nodes'}</span></div>
    <div class="status-hero" style="grid-template-columns:auto 1fr;gap:14px">
      <div class="fi {cm?.state === 'leader' ? 'green' : ''}"><Icon name="overview" size={18} /></div>
      <div>
        <div class="live"><span class="dot"></span> {cm?.state ?? 'unknown'}</div>
        <div class="meta">
          <span>node {cm?.node ?? '—'}</span><span class="mid">·</span>
          <span>conduit {cm?.conduit_id ?? '—'}</span>
        </div>
      </div>
    </div>
  </div>

  <div class="stat-grid">
    {#each snap.shards as s (s.shard_id)}
      <div class="card stat">
        <div class="ico {tone(s.state)}"><Icon name="pulse" size={18} /></div>
        <div class="label">shard {s.shard_id} · {s.node}</div>
        <div class="value">{s.state}</div>
        <div class="delta flat">
          {s.bound ? 'bound' : 'unbound'}
          {#if s.handshake_in_flight}· handshake{/if}
          · {keepalive(s.keepalive_ms)}
          · {s.attempts ?? 0} att
        </div>
        <div class="delta flat" style="opacity:.6">session {s.session_id ?? '—'}</div>
      </div>
    {/each}
  </div>
</section>
