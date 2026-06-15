<script lang="ts">
  import { Icon, StatTile, Button } from '@bagel/shared';
  let { data } = $props();

  const nodes = [
    { warn: false, nm: 'chat-gateway', sv: 'streaming IRC · 3 replicas', pg: '38ms' },
    { warn: false, nm: 'command-engine', sv: 'healthy · 0 errors', pg: '11ms' },
    { warn: false, nm: 'moderation-svc', sv: 'automod active', pg: '9ms' },
    { warn: true, nm: 'analytics-worker', sv: 'backfilling queue', pg: 'qd 142' },
    { warn: false, nm: 'event-bus', sv: 'nominal', pg: '4ms' }
  ];

  const feed = [
    { icon: 'check', green: true, title: '<code>!uptime</code> triggered', sub: 'by viewer · ferret_king', w: '2m' },
    { icon: 'moderation', green: false, title: 'Timeout · 60s', sub: 'caps filter · LOUDGUY99', w: '8m' },
    { icon: 'check', green: false, title: 'New follower', sub: 'bagel_enjoyer just followed', w: '12m' },
    { icon: 'commands', green: true, title: '<code>!socials</code> added', sub: 'by ItsMavey', w: '31m' },
    { icon: 'send', green: false, title: 'Shoutout sent', sub: '!so @kettle', w: '44m' }
  ] as const;
</script>

<section class="screen active">
  <div class="page-head">
    <span class="eyebrow">Status</span>
    <h1>Good evening, <em>{data?.displayName ?? 'Mavey'}</em></h1>
    <p>Your bot is healthy and connected. Zero downtime, infinite bagels.</p>
  </div>

  <div class="card sheen status-hero">
    <div class="botmark"><img src="/logo.png" alt="" /></div>
    <div>
      <div class="live"><span class="dot"></span> {data.receiving ? 'Online · in chat' : 'Idle'}</div>
      <h2>#itsmavey</h2>
      <div class="meta">
        <span>Node eu-west-2</span><span class="mid">·</span>
        <span>Uptime 14d 06h</span><span class="mid">·</span>
        <span>Latency 38ms</span><span class="mid">·</span>
        <span>v2.4.1</span>
      </div>
    </div>
    <div class="actions">
      <Button variant="ghost" icon="activity">Restart</Button>
      <Button variant="primary" icon="power" data-only="streamer">Disconnect</Button>
    </div>
  </div>

  <div class="stat-grid">
    <StatTile icon="commands" label="Commands run" value="1,284" unit="today" delta="▲ 12% vs yesterday" />
    <StatTile icon="users" tan label="Chatters" value="312" unit="active" delta="▲ 28 this hour" />
    <StatTile icon="moderation" label="Mod actions" value="7" unit="today" delta="2 timeouts · 5 deletes" flat />
    <StatTile icon="pulse" tan label="Messages seen" value="48.2" unit="k" delta="▲ 6% vs yesterday" />
  </div>

  <div class="grid-2">
    <div class="card">
      <div class="card-head"><h3>Service health</h3><span class="more">All systems →</span></div>
      <div class="node-list">
        {#each nodes as n}
          <div class="node-row">
            <span class="nd {n.warn ? 'warn' : ''}"></span>
            <span class="nm">{n.nm}</span>
            <span class="sv">{n.sv}</span>
            <span class="pg">{n.pg}</span>
          </div>
        {/each}
      </div>
    </div>
    <div class="card">
      <div class="card-head"><h3>Recent activity</h3><span class="more">View all →</span></div>
      <div class="feed">
        {#each feed as f}
          <div class="feed-row">
            <div class="fi {f.green ? 'green' : ''}"><Icon name={f.icon} size={15} /></div>
            <div class="ft"><b>{@html f.title}</b><span>{f.sub}</span></div>
            <span class="fw">{f.w}</span>
          </div>
        {/each}
      </div>
    </div>
  </div>
</section>
