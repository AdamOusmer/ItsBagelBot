<script lang="ts">
  import { enhance } from '$app/forms';
  import { Icon, StatTile, Button } from '@bagel/shared';
  let { data, form } = $props();

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

  {#if form?.ok}
    <div class="perm-note" style="margin-bottom:18px">
      <Icon name="check" size={15} />
      <span>{form.action === 'restart' ? 'Restarting event delivery…' : 'Disconnected. Event delivery stopped.'}</span>
    </div>
  {/if}

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
      <form method="POST" action="?/restart" use:enhance>
        <Button variant="ghost" icon="activity" type="submit">Restart</Button>
      </form>
      <form method="POST" action="?/disconnect" use:enhance>
        <Button variant="primary" icon="power" type="submit">Disconnect</Button>
      </form>
    </div>
  </div>

  <div class="stat-grid">
    <StatTile icon="commands" label="Commands run" value="1,284" unit="today" delta="▲ 12% vs yesterday" />
    <StatTile icon="users" tan label="Chatters" value="312" unit="active" delta="▲ 28 this hour" />
    <StatTile icon="moderation" label="Mod actions" value="7" unit="today" delta="2 timeouts · 5 deletes" flat />
    <StatTile icon="pulse" tan label="Messages seen" value="48.2" unit="k" delta="▲ 6% vs yesterday" />
  </div>

  <div class="card" style="margin-top:var(--row-gap)">
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
</section>
