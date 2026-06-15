<script lang="ts">
  import { Icon, StatTile, Button } from '@bagel/shared';
  let { data } = $props();

  const snap = $derived(data.snapshot);
  const connected = $derived(snap.shards.filter((s) => s.state === 'connected').length);
  const total = $derived(snap.shard_count || snap.shards.length);
  const degradedShards = $derived(Math.max(0, total - connected));
  const health = $derived(degradedShards === 0 ? 'healthy' : 'degraded');
</script>

<section class="screen active">
  <div class="page-head">
    <span class="eyebrow">Operator overview</span>
    <h1>Live <em>control plane</em></h1>
    <p>
      Fleet, accounts, and bot status at a glance.{#if data.degraded}
        <em> Some live data is unavailable; showing last-known/sample values.</em>{/if}
    </p>
  </div>

  <div class="stat-grid">
    <StatTile icon="users" label="Registered users" value={data.stats.total_users.toLocaleString()} unit="total" delta={`${data.stats.active_users.toLocaleString()} active`} flat />
    <StatTile icon="pulse" tan label="Premium users" value={data.stats.premium_users.toLocaleString()} unit="premium" delta={`${data.stats.vip_users} VIP · ${data.stats.paid_users} paid`} flat />
    <StatTile icon="activity" label="Shards" value={`${connected}/${total}`} unit="up" delta={`${snap.nodes.length} nodes · ${health}`} flat />
    <StatTile icon="overview" tan label="Conduit" value={snap.conduit_manager?.state ?? 'unknown'} unit="" delta={`node ${snap.conduit_manager?.node ?? '—'}`} flat />
  </div>

  <div class="grid-2">
    <div class="card">
      <div class="card-head"><h3>Shard summary</h3><a class="more" href="/shards">All shards →</a></div>
      <div class="node-list">
        {#each snap.shards as s (s.shard_id)}
          <div class="node-row">
            <span class="nd {s.state === 'connected' ? '' : 'warn'}"></span>
            <span class="nm">shard {s.shard_id}</span>
            <span class="sv">{s.state} · {s.node}</span>
            <span class="pg">{s.attempts ?? 0} att</span>
          </div>
        {/each}
      </div>
    </div>

    <div class="card">
      <div class="card-head"><h3>Bot account</h3></div>
      <div class="status-hero" style="grid-template-columns:auto 1fr;gap:14px">
        <div class="botmark"><img src="/logo.png" alt="" /></div>
        <div>
          <div class="live">
            <span class="dot"></span>
            {data.botPresent ? 'Token stored' : 'No token stored'}
          </div>
          <h2 style="margin:4px 0">Twitch bot</h2>
          <div class="meta">
            <span>{data.botPresent ? 'Authorized · OAuth token present' : 'Awaiting authorization'}</span>
          </div>
          <div class="actions" style="margin-top:12px">
            <Button variant="ghost" icon="link">Re-authorize</Button>
          </div>
        </div>
      </div>
    </div>
  </div>
</section>
