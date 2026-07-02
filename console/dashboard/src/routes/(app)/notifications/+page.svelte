<script lang="ts">
  import { enhance } from '$app/forms';
  import { Icon, PageHead, Card, EmptyState } from '@bagel/shared';
  import type { NotificationWire } from '$lib/server/services';
  let { data } = $props();

  const notifications = $derived((data.notifications ?? []) as NotificationWire[]);

  function levelLabel(l: string): string {
    return l.charAt(0).toUpperCase() + l.slice(1);
  }
</script>

<section class="screen active">
  <PageHead eyebrow="Account" description="Messages from the ItsBagelBot team.">Your <em>notifications</em></PageHead>

  <Card class="notif-card">
    {#if notifications.length === 0}
      <EmptyState icon="bell" title="No notifications" body="You're all caught up." />
    {:else}
      <div class="list">
        {#each notifications as n (n.id)}
          <div class="row-item" class:unread={!n.read}>
            <span class="level {n.level}">{levelLabel(n.level)}</span>
            <div class="text">
              <b>{n.title}</b>
              <p>{n.body}</p>
              <span class="meta">{n.created_by_login} · {new Date(n.created_at).toLocaleString()}</span>
            </div>
            {#if !n.read}
              <form method="POST" action="?/markRead" use:enhance>
                <input type="hidden" name="id" value={n.id} />
                <button type="submit" class="btn ghost sm"><Icon name="check" size={12} /> Mark read</button>
              </form>
            {/if}
          </div>
        {/each}
      </div>
    {/if}
  </Card>
</section>

<style>
  :global(.notif-card) { margin-top: 18px; }

  .list { display: flex; flex-direction: column; gap: 10px; }
  .row-item {
    display: flex; align-items: flex-start; gap: 14px;
    border: 1px solid var(--glass-border); border-radius: var(--bb-radius-md, 10px);
    padding: 12px 14px; background: rgba(255, 255, 255, 0.02);
  }
  .row-item.unread { border-color: rgba(201, 168, 124, 0.3); background: rgba(201, 168, 124, 0.04); }

  .text { flex: 1; min-width: 0; }
  .text b { font-size: 14px; color: var(--bb-white); }
  .text p { margin: 4px 0; font-size: 13px; color: var(--bb-muted); }
  .meta { font-family: var(--bb-font-mono); font-size: 11px; color: var(--bb-muted); opacity: 0.8; }

  .level {
    font-family: var(--bb-font-mono); font-size: 10px; letter-spacing: 0.08em; text-transform: uppercase;
    padding: 4px 10px; border-radius: var(--bb-radius-pill); border: 1px solid transparent; white-space: nowrap;
  }
  .level.info { background: rgba(255,255,255,0.04); color: var(--bb-muted); border-color: var(--glass-border); }
  .level.success { background: rgba(82,183,136,0.10); color: var(--bb-green-glow); border-color: rgba(82,183,136,0.28); }
  .level.warning { background: rgba(201,168,124,0.10); color: var(--bb-tan-light); border-color: rgba(201,168,124,0.28); }
  .level.critical { background: rgba(176,90,70,0.15); color: #cf8a78; border-color: rgba(176,90,70,0.4); }

  .btn.sm { padding: 4px 10px; font-size: 12px; }

  @media (max-width: 760px) {
    .row-item { flex-direction: column; }
  }
</style>
