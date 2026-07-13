<script lang="ts">
  import { onMount } from 'svelte';
  import { Icon, StatTile, PageHead, CardHead, Card, Button, Skeleton, AlertBanner } from '@bagel/shared';
  import type { ShardSnapshot } from '@bagel/shared';
  import EnrollmentChart from '$lib/components/EnrollmentChart.svelte';
  import type { AuditEntry } from '$lib/server/services';

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

  function shardSummary(snap: ShardSnapshot) {
    const connected = snap.shards.filter((s) => s.state === 'connected').length;
    const total = snap.shard_count || snap.shards.length;
    return { connected, total, healthy: total - connected <= 0 };
  }

  function ago(iso: string): string {
    const mins = Math.max(Math.round((Date.now() - new Date(iso).getTime()) / 60e3), 0);
    if (mins < 1) return 'now';
    if (mins < 60) return `${mins}m ago`;
    const hours = Math.round(mins / 60);
    if (hours < 48) return `${hours}h ago`;
    return `${Math.round(hours / 24)}d ago`;
  }

  function auditLine(e: AuditEntry): string {
    const target = e.target ? ` → ${e.target}` : '';
    return `${e.action}${target}`;
  }
</script>

<section class="screen active">
  <PageHead eyebrow="Operator overview" description="Growth, fleet, and bot status at a glance.">
    Live <em>control plane</em>
  </PageHead>

  {#await data.overview}
    <div class="stat-grid">
      <StatTile icon="users" label="Registered users" value="—" unit="total" delta="loading…" flat />
      <StatTile icon="pulse" tan label="Premium users" value="—" unit="premium" delta="loading…" flat />
      <StatTile icon="server" label="Shards" value="—" unit="up" delta="loading…" flat />
      <StatTile icon="overview" tan label="Conduit" value="…" unit="" delta="loading…" flat />
    </div>
    <div class="growth-card card">
      <div class="card-head"><h3>Enrollment</h3></div>
      <Skeleton variant="block" height="300px" />
    </div>
    <div class="grid-2">
      <Card>
        <CardHead title="Fleet" />
        <div class="skeleton-list">
          {#each [0, 1, 2, 3] as i (i)}<Skeleton variant="block" height="34px" />{/each}
        </div>
      </Card>
      <Card>
        <CardHead title="Bot account" />
        <div class="skeleton-list">
          <Skeleton variant="pill" />
          <Skeleton variant="text" lines={2} width="70%" />
          <Skeleton variant="block" height="40px" />
        </div>
      </Card>
    </div>
  {:then o}
    {@const sum = shardSummary(o.snapshot)}
    {@const stats = o.enrollment.stats}
    {#if o.degraded}
      <AlertBanner>Some live data is unavailable; affected panels show last-known or sample values.</AlertBanner>
    {/if}

    <div class="stat-grid">
      <StatTile
        icon="users"
        label="Registered users"
        value={stats.total_users.toLocaleString()}
        unit="total"
        delta={`${stats.active_users.toLocaleString()} active`}
        flat
      />
      <StatTile
        icon="pulse"
        tan
        label="Premium users"
        value={stats.premium_users.toLocaleString()}
        unit="premium"
        delta={`${stats.vip_users} VIP · ${stats.paid_users} paid`}
        flat
      />
      <StatTile
        icon="server"
        label="Shards"
        value={`${sum.connected}/${sum.total}`}
        unit="up"
        delta={`${o.snapshot.nodes.length} nodes · ${sum.healthy ? 'healthy' : 'degraded'}`}
        flat={sum.healthy}
      />
      <StatTile
        icon="overview"
        tan
        label="Conduit"
        value={o.snapshot.conduit_manager?.state ?? 'unknown'}
        unit=""
        delta={`node ${o.snapshot.conduit_manager?.node ?? '—'}`}
        flat
      />
    </div>

    <div class="growth-card card">
      <div class="card-head">
        <h3>Enrollment — last {o.enrollment.days.length} days</h3>
        <a class="more" href="/users">All users →</a>
      </div>
      <EnrollmentChart enrollment={o.enrollment} />
    </div>

    <div class="grid-2">
      <Card>
        <CardHead title="Fleet">
          {#snippet action()}<a class="more" href="/shards">All shards →</a>{/snippet}
        </CardHead>
        <div class="node-list">
          {#each o.snapshot.shards as s (s.shard_id)}
            <div class="node-row">
              <span class="nd {s.state === 'connected' ? '' : 'warn'}"></span>
              <span class="nm">shard {s.shard_id}</span>
              <span class="sv">{s.state} · {s.host || 'unknown-host'}</span>
              <span class="pg">{s.attempts ?? 0} att</span>
            </div>
          {/each}
        </div>
      </Card>

      <Card>
        <CardHead title="Bot account" />
        <div class="bot-body">
          <div class="bot-row">
            <div class="botmark"><img src="/logo.png" alt="" /></div>
            <div>
              <div class="live" class:off={!o.botPresent}>
                <span class="dot"></span>
                {o.botPresent ? 'Token stored' : 'No token stored'}
              </div>
              <div class="bot-meta">
                {o.botPresent ? 'Authorized · OAuth token present' : 'Awaiting authorization'}
              </div>
            </div>
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
                <Button variant="ghost" type="button" onclick={copyLink}>
                  <Icon name="link" size={14} /> {copied ? 'Copied' : 'Copy'}
                </Button>
              </div>
            </div>
          {/if}
        </div>
      </Card>
    </div>

    {#if data.isManager}
      <div class="card audit-card">
        <div class="card-head">
          <h3>Recent operator actions</h3>
          <a class="more" href="/audit">Full audit →</a>
        </div>
        {#if o.recentAudit.length === 0}
          <p class="audit-empty">No actions recorded yet.</p>
        {:else}
          <div class="node-list">
            {#each o.recentAudit as e (e.id)}
              <div class="node-row">
                <span class="nd {e.ok ? '' : 'err'}"></span>
                <span class="nm">@{e.actor_login}</span>
                <span class="sv mono">{auditLine(e)}</span>
                {#if !e.ok}<span class="audit-err">{e.error || 'failed'}</span>{/if}
                <span class="pg">{ago(e.created_at)}</span>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    {/if}
  {/await}
</section>

<style>
  .growth-card { margin-top: var(--row-gap); }

  .skeleton-list { display: flex; flex-direction: column; gap: 10px; padding: 14px; }

  .node-row .sv.mono { font-family: var(--bb-font-mono); font-size: 12px; }
  .node-row .nd.err { background: #cf8a78; box-shadow: 0 0 8px rgba(176, 90, 70, 0.6); }

  .audit-card { margin-top: var(--row-gap); }
  .audit-empty { font-family: var(--bb-font-body); font-size: 13px; color: var(--bb-muted); margin: 0; }
  .audit-err {
    font-family: var(--bb-font-mono); font-size: 10.5px; color: #cf8a78;
    background: rgba(176, 90, 70, 0.1); border: 1px solid rgba(176, 90, 70, 0.28);
    border-radius: var(--bb-radius-pill); padding: 2px 8px;
    max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }

  .bot-body { display: flex; flex-direction: column; gap: 14px; }
  .bot-row { display: flex; align-items: center; gap: 14px; }
  .bot-row .botmark {
    width: 48px; height: 48px; border-radius: 50%; flex: none;
    background: rgba(82, 183, 136, 0.07); border: 1px solid rgba(82, 183, 136, 0.3);
    display: flex; align-items: center; justify-content: center;
  }
  .bot-row .botmark img { width: 32px; height: 32px; border-radius: 50%; }
  .bot-row .live {
    display: inline-flex; align-items: center; gap: 8px;
    font-family: var(--bb-font-mono); font-size: 11px; letter-spacing: 0.12em;
    text-transform: uppercase; color: var(--bb-green-glow); margin-bottom: 4px;
  }
  .bot-row .live .dot {
    width: 7px; height: 7px; border-radius: 50%;
    background: var(--bb-green-glow); box-shadow: 0 0 8px var(--bb-green-glow);
  }
  .bot-row .live.off { color: var(--bb-tan-light); }
  .bot-row .live.off .dot { background: var(--bb-tan); box-shadow: 0 0 8px var(--bb-tan); }
  .bot-row .bot-meta { font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); }
  .bot-row .btn { margin-left: auto; white-space: nowrap; }

  .botlink .hint { margin: 0 0 6px; font-size: 0.8rem; color: var(--bb-muted); font-family: var(--bb-font-body); }
  .botlink-row { display: flex; gap: 8px; align-items: center; }
  .botlink-url {
    flex: 1; min-width: 0; padding: 7px 10px;
    font-family: var(--bb-font-mono, monospace); font-size: 12px;
    border: 1px solid var(--bb-border, #333); border-radius: 8px;
    background: var(--bb-bg-1, #1a1a1a); color: var(--bb-white, #eee);
  }

  @media (max-width: 760px) {
    :global(.stat-grid) { grid-template-columns: 1fr 1fr; }
    .bot-row { flex-wrap: wrap; }
    .bot-row .btn { margin-left: 0; width: 100%; justify-content: center; }
    .audit-err { display: none; }
  }
</style>
