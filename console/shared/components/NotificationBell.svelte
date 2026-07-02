<script lang="ts">
  // Topbar notification bell: unread badge + a modal listing recent items.
  // Presentational only — the host page owns fetching/caching the list and
  // wiring onMarkRead to its own form action (mark-read semantics differ
  // between the dashboard, which tracks per-user read state, and admin,
  // which has none and passes no onMarkRead at all).
  import Modal from './Modal.svelte';
  import Icon from './Icon.svelte';

  export interface BellNotification {
    id: number;
    title: string;
    body: string;
    level: 'info' | 'success' | 'warning' | 'critical';
    created_at: string;
    read?: boolean;
  }

  let {
    notifications,
    unreadCount = 0,
    viewAllHref,
    onMarkRead,
    emptyLabel = 'Nothing yet.'
  }: {
    notifications: BellNotification[];
    unreadCount?: number;
    viewAllHref: string;
    onMarkRead?: (id: number) => void;
    emptyLabel?: string;
  } = $props();

  let open = $state(false);
</script>

<button class="icon-btn bell-btn" aria-label="Notifications" onclick={() => (open = true)}>
  <Icon name="bell" size={16} />
  {#if unreadCount > 0}<span class="badge">{unreadCount > 9 ? '9+' : unreadCount}</span>{/if}
</button>

<Modal {open} title="Notifications" closeModal={() => (open = false)}>
  {#if notifications.length === 0}
    <p class="empty">{emptyLabel}</p>
  {:else}
    <div class="items">
      {#each notifications as n (n.id)}
        <div class="item" class:unread={!n.read}>
          <span class="level {n.level}">{n.level}</span>
          <div class="text">
            <b>{n.title}</b>
            <p>{n.body}</p>
          </div>
          {#if onMarkRead && !n.read}
            <button type="button" class="btn ghost sm" onclick={() => onMarkRead?.(n.id)}>
              <Icon name="check" size={12} /> Read
            </button>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
  <a class="view-all" href={viewAllHref} onclick={() => (open = false)}>View all →</a>
</Modal>

<style>
  .icon-btn {
    width: 32px; height: 32px; border-radius: 2px; display: flex; align-items: center; justify-content: center;
    background: none; border: 1px solid var(--rule, rgba(240, 236, 228, 0.1)); color: var(--bb-tan-light); cursor: pointer;
    transition: all var(--bb-dur-base) var(--bb-ease-out-expo); flex: none; position: relative;
  }
  .icon-btn :global(svg) { width: 15px; height: 15px; stroke: currentColor; fill: none; stroke-width: 1.7; }
  .icon-btn:hover { border-color: var(--rule-tan, rgba(201, 168, 124, 0.45)); color: var(--bb-tan-pale); }

  .badge {
    position: absolute; top: -5px; right: -5px;
    min-width: 16px; height: 16px; padding: 0 4px;
    border-radius: 999px; background: var(--bb-tan, #c9a87c); color: #0a0a0a;
    font-family: var(--bb-font-mono); font-size: 9px; font-weight: 700; line-height: 16px; text-align: center;
    box-shadow: 0 0 0 2px rgba(10, 10, 10, 0.85);
  }

  .empty { color: var(--bb-muted); font-size: 13px; margin: 4px 0 16px; }

  .items { display: flex; flex-direction: column; gap: 10px; margin-bottom: 16px; max-height: 50vh; overflow-y: auto; }
  .item {
    display: flex; align-items: flex-start; gap: 10px;
    border: 1px solid var(--glass-border); border-radius: var(--bb-radius-md, 10px);
    padding: 10px 12px; background: rgba(255, 255, 255, 0.02);
  }
  .item.unread { border-color: rgba(201, 168, 124, 0.3); background: rgba(201, 168, 124, 0.04); }

  .text { flex: 1; min-width: 0; }
  .text b { font-size: 13.5px; color: var(--bb-white); }
  .text p { margin: 4px 0 0; font-size: 12.5px; color: var(--bb-muted); }

  .level {
    font-family: var(--bb-font-mono); font-size: 9px; letter-spacing: 0.08em; text-transform: uppercase;
    padding: 3px 8px; border-radius: var(--bb-radius-pill); border: 1px solid transparent; white-space: nowrap;
  }
  .level.info { background: rgba(255,255,255,0.04); color: var(--bb-muted); border-color: var(--glass-border); }
  .level.success { background: rgba(82,183,136,0.10); color: var(--bb-green-glow); border-color: rgba(82,183,136,0.28); }
  .level.warning { background: rgba(201,168,124,0.10); color: var(--bb-tan-light); border-color: rgba(201,168,124,0.28); }
  .level.critical { background: rgba(176,90,70,0.15); color: #cf8a78; border-color: rgba(176,90,70,0.4); }

  .btn.sm { padding: 4px 10px; font-size: 11px; white-space: nowrap; }

  .view-all {
    display: inline-block; font-family: var(--bb-font-mono); font-size: 11px; letter-spacing: 0.06em;
    color: var(--bb-tan); text-decoration: none;
  }
  .view-all:hover { color: var(--bb-tan-pale); }
</style>
