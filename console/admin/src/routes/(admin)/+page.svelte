<script lang="ts">
  import { onMount } from 'svelte';
  import { Icon, StatTile } from '@bagel/shared';
  import type { ShardSnapshot } from '@bagel/shared';
  let { data } = $props();

  // Absolute URL of the bot-authorization route. The operator opens it in the
  // browser signed into the bot account; that browser gets the state cookie and
  // the callback validates it there, so the link works across the browser switch.
  let botLink = $state('');
  let copied = $state(false);
  onMount(() => {
    botLink = `${location.origin}/auth/bot/login`;
  });

  async function copyLink() {
    if (!botLink) return;
    try {
      await navigator.clipboard.writeText(botLink);
      copied = true;
      setTimeout(() => (copied = false), 1500);
    } catch {
      copied = false;
    }
  }

  // Shard rollup derived from a resolved snapshot (the overview bundle streams
  // in, so this is computed inside the {#await ... then} block per render).
  function shardSummary(snap: ShardSnapshot) {
    const connected = snap.shards.filter((s) => s.state === 'connected').length;
    const total = snap.shard_count || snap.shards.length;
    return { connected, total, health: total - connected <= 0 ? 'healthy' : 'degraded' };
  }
</script>

<section class="screen active">
  <div class="page-head">
    <span class="eyebrow">Operator overview</span>
    <h1>Live <em>control plane</em></h1>
    <p>Fleet, accounts, and bot status at a glance.</p>
  </div>

  {#await data.overview}
    <div class="stat-grid">
      <StatTile icon="users" label="Registered users" value="—" unit="total" delta="loading…" flat />
      <StatTile icon="pulse" tan label="Premium users" value="—" unit="premium" delta="loading…" flat />
      <StatTile icon="activity" label="Shards" value="—" unit="up" delta="loading…" flat />
      <StatTile icon="overview" tan label="Conduit" value="…" unit="" delta="loading…" flat />
    </div>
    <div class="grid-2">
      <div class="card"><div class="card-head"><h3>Shard summary</h3></div><div class="node-list muted-load">Loading live data…</div></div>
      <div class="card"><div class="card-head"><h3>Bot account</h3></div><div class="node-list muted-load">Loading live data…</div></div>
    </div>
  {:then o}
    {@const sum = shardSummary(o.snapshot)}
    {#if o.degraded}
      <p class="degraded-note"><em>Some live data is unavailable; showing last-known/sample values.</em></p>
    {/if}

    <div class="stat-grid">
      <StatTile icon="users" label="Registered users" value={o.stats.total_users.toLocaleString()} unit="total" delta={`${o.stats.active_users.toLocaleString()} active`} flat />
      <StatTile icon="pulse" tan label="Premium users" value={o.stats.premium_users.toLocaleString()} unit="premium" delta={`${o.stats.vip_users} VIP · ${o.stats.paid_users} paid`} flat />
      <StatTile icon="activity" label="Shards" value={`${sum.connected}/${sum.total}`} unit="up" delta={`${o.snapshot.nodes.length} nodes · ${sum.health}`} flat />
      <StatTile icon="overview" tan label="Conduit" value={o.snapshot.conduit_manager?.state ?? 'unknown'} unit="" delta={`node ${o.snapshot.conduit_manager?.node ?? '—'}`} flat />
    </div>

    <div class="grid-2">
      <div class="card">
        <div class="card-head"><h3>Shard summary</h3><a class="more" href="/shards">All shards →</a></div>
        <div class="node-list">
          {#each o.snapshot.shards as s (s.shard_id)}
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
              {o.botPresent ? 'Token stored' : 'No token stored'}
            </div>
            <h2 style="margin:4px 0">Twitch bot</h2>
            <div class="meta">
              <span>{o.botPresent ? 'Authorized · OAuth token present' : 'Awaiting authorization'}</span>
            </div>
            <div class="actions" style="margin-top:12px">
              <!--
                Bot authorization. The link below routes to /auth/bot/login,
                which sets the state cookie in whichever browser opens it and
                redirects to Twitch (dashboard app + chat scopes); the callback
                stores the token. The operator opens it in the browser signed
                into the bot account, hence the copyable URL.
              -->
              <a class="btn ghost" href="/auth/bot/login">
                <Icon name="link" size={14} />
                {o.botPresent ? 'Re-authorize' : 'Authorize'}
              </a>
            </div>

            {#if botLink}
              <div class="botlink">
                <p class="hint">Open this in the browser signed into the bot account:</p>
                <div class="botlink-row">
                  <input class="botlink-url" type="text" readonly value={botLink} />
                  <button class="btn ghost" type="button" onclick={copyLink}>
                    <Icon name="link" size={14} /> {copied ? 'Copied' : 'Copy'}
                  </button>
                </div>
              </div>
            {/if}
          </div>
        </div>
      </div>
    </div>
  {/await}
</section>

<style>
  .degraded-note { margin: 0 0 14px; font-size: 0.85rem; color: var(--bb-muted); }
  .botlink { margin-top: 12px; }
  .botlink .hint { margin: 0 0 6px; font-size: 0.8rem; color: var(--bb-muted); }
  .botlink-row { display: flex; gap: 8px; align-items: center; }
  .botlink-url {
    flex: 1;
    min-width: 0;
    padding: 7px 10px;
    font-family: var(--bb-font-mono, monospace);
    font-size: 12px;
    border: 1px solid var(--bb-border, #333);
    border-radius: 6px;
    background: var(--bb-surface, #1a1a1a);
    color: var(--bb-text, #eee);
  }
  .muted-load {
    padding: 18px 14px;
    font-family: var(--bb-font-body);
    font-size: 13px;
    color: var(--bb-muted);
    opacity: 0.7;
  }

  @media (max-width: 760px) {
    :global(.stat-grid) { grid-template-columns: 1fr 1fr; }
    /* status-hero in a card: stack vertically, let actions fill width */
    :global(.status-hero) { grid-template-columns: 1fr !important; gap: 14px !important; }
    :global(.status-hero .actions) { width: 100%; }
    :global(.status-hero .actions .btn) { width: 100%; justify-content: center; }
  }
</style>
