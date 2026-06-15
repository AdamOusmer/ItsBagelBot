<script lang="ts">
  import { enhance } from '$app/forms';
  import { Button } from '@bagel/shared';
  let { data } = $props();

  const statusLabel = $derived({ free: 'Free', paid: 'Paid', vip: 'VIP' }[data.status] ?? 'Free');
  const paid = $derived(data.status !== 'free');
</script>

<section class="screen active">
  <div class="page-head">
    <span class="eyebrow">Status</span>
    <h1>Good evening, <em>{data?.displayName ?? 'there'}</em></h1>
    <p>Manage your bot connection, commands, and modules from here.</p>
  </div>

  <div class="card sheen status-hero">
    <div class="botmark"><img src="/logo.png" alt="" /></div>
    <div>
      <div class="live {data.receiving ? '' : 'off'}">
        <span class="dot"></span> {data.receiving ? 'Online · in chat' : data.enabled ? 'Connected · idle' : 'Not connected'}
      </div>
      <h2>#{data.login ?? 'itsmavey'}</h2>
      <div class="meta">
        <span class="status-tag {paid ? 'premium' : ''}">{statusLabel}</span>
      </div>
    </div>
    <div class="actions">
      {#if data.receiving}
        <form method="POST" action="?/restart" use:enhance>
          <Button variant="ghost" icon="activity" type="submit">Restart</Button>
        </form>
        <form method="POST" action="?/disconnect" use:enhance>
          <Button variant="tan" icon="power" type="submit">Disconnect</Button>
        </form>
      {:else}
        <form method="POST" action="?/enable" use:enhance>
          <Button variant="primary" icon="power" type="submit">Enable</Button>
        </form>
      {/if}
    </div>
  </div>
</section>

<style>
  .status-hero .live.off { color: var(--bb-muted); }
  .status-hero .live.off .dot { background: var(--bb-muted); box-shadow: none; animation: none; }
  .status-tag {
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    padding: 5px 12px;
    border-radius: var(--bb-radius-pill);
    background: rgba(255, 255, 255, 0.04);
    border: 1px solid var(--bb-border);
    color: var(--bb-muted);
  }
  .status-tag.premium {
    background: rgba(82, 183, 136, 0.12);
    border-color: rgba(82, 183, 136, 0.35);
    color: var(--bb-green-glow);
  }
</style>
