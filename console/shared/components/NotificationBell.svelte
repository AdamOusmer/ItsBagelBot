<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Topbar notification bell: unread badge + an anchored dropdown of recent
  // items (not a centered modal, notifications are a glance, not a task).
  // Presentational only: the host page owns fetching/caching the list and
  // wiring onMarkRead to its own form action (mark-read semantics differ
  // between the dashboard, which tracks per-user read state, and admin,
  // which has none and passes no onMarkRead at all).
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
    onOpen,
    emptyLabel = 'Nothing yet.',
    title = 'Notifications',
    viewAllLabel = 'View all →',
    readLabel = 'Read'
  }: {
    notifications: BellNotification[];
    unreadCount?: number;
    viewAllHref: string;
    onMarkRead?: (id: number) => void;
    // Fired the first time the dropdown opens. The host uses it to "peek"
    // (soft-acknowledge) every notification, so opening the bell counts as
    // seeing them. Only hosts that track per-user read state pass this.
    onOpen?: () => void;
    emptyLabel?: string;
    title?: string;
    viewAllLabel?: string;
    readLabel?: string;
  } = $props();

  let open = $state(false);
  // Once a peek-capable host has been notified of an open, clear the badge
  // optimistically so the count doesn't linger while the server round-trips.
  let peeked = $state(false);

  function toggle() {
    open = !open;
    if (open && onOpen && !peeked) {
      peeked = true;
      onOpen();
    }
  }

  // Suppress the badge only for peek-capable hosts (dashboard); the admin bell
  // has no per-user read state and keeps its badge behavior unchanged.
  const showBadge = $derived(unreadCount > 0 && !(onOpen && peeked));

  // Severity -> shared .bb-tag variant. Same map as the settings notification
  // list, so one level reads identically in both surfaces. No variant is red,
  // so critical takes --alpha plus the danger colour below.
  const LEVEL_TAG: Record<string, string> = {
    info: 'bb-tag--quiet',
    success: 'bb-tag--live',
    warning: 'bb-tag--pre',
    critical: 'bb-tag--alpha'
  };
</script>

<svelte:window onkeydown={(e) => { if (e.key === 'Escape') open = false; }} />

<div class="bell-wrap">
  <button
    class="icon-btn bell-btn"
    class:open
    aria-label={title}
    aria-expanded={open}
    aria-haspopup="menu"
    onclick={toggle}
  >
    <Icon name="bell" size={16} />
    {#if showBadge}<span class="bb-tag bb-tag--bare badge"><i class="bb-mark" aria-hidden="true"></i>{unreadCount > 9 ? '9+' : unreadCount}</span>{/if}
  </button>

  {#if open}
    <!-- Click-away scrim; Escape (window handler above) is the keyboard path. -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="scrim"
      role="presentation"
      onclick={() => (open = false)}
      onkeydown={(e) => { if (e.key === 'Enter') open = false; }}
    ></div>

    <div class="dropdown" role="menu" aria-label="Recent notifications">
      <div class="drop-head">
        <h4>{title}</h4>
        <a class="view-all" href={viewAllHref} onclick={() => (open = false)}>{viewAllLabel}</a>
      </div>
      {#if notifications.length === 0}
        <p class="empty">{emptyLabel}</p>
      {:else}
        <div class="items">
          {#each notifications as n (n.id)}
            <div class="item" class:unread={!n.read}>
              <span class="bb-tag {LEVEL_TAG[n.level] ?? 'bb-tag--quiet'} level {n.level}">{n.level}</span>
              <div class="text">
                <b>{n.title}</b>
                <p>{n.body}</p>
              </div>
              {#if onMarkRead && !n.read}
                <button type="button" class="btn ghost sm" onclick={() => onMarkRead?.(n.id)}>
                  {readLabel}
                </button>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .bell-wrap { position: relative; display: inline-flex; }

  .icon-btn {
    width: 36px; height: 36px; border-radius: 50%; display: flex; align-items: center; justify-content: center;
    background: none; border: 1px solid var(--bb-border, rgba(201, 168, 124, 0.15)); color: var(--bb-tan-light); cursor: pointer;
    transition: all var(--bb-dur-base) var(--bb-ease-out-expo); flex: none; position: relative;
  }
  .icon-btn:hover, .icon-btn.open { border-color: var(--bb-border-strong, rgba(201, 168, 124, 0.35)); color: var(--bb-tan-pale); }
  .icon-btn.open { background: rgba(201, 168, 124, 0.1); }

  /* Was a filled 999px counter circle; the count is an indicator, not a
     control, so it is a bare .bb-tag with a .bb-mark beside the number. */
  .badge {
    position: absolute; top: -7px; right: -11px;
    gap: 4px; font-size: 9.5px; letter-spacing: 0.08em;
    color: var(--bb-tan, #c9a87c); pointer-events: none;
  }

  .scrim { position: fixed; inset: 0; z-index: 89; }

  /* anchored dropdown card, popping from the bell */
  .dropdown {
    position: absolute;
    top: calc(100% + 10px);
    right: 0;
    z-index: 90;
    width: min(380px, calc(100vw - 24px));
    background: var(--bb-card-bg, #111110);
    border: 1px solid var(--bb-border-strong, rgba(201, 168, 124, 0.35));
    border-radius: 8px 8px;
    box-shadow: 0 18px 50px rgba(0, 0, 0, 0.55);
    padding: 14px;
    transform-origin: top right;
    animation: drop-in 240ms var(--bb-ease-out-back, ease-out) both;
  }
  @keyframes drop-in {
    from { opacity: 0; transform: translateY(-6px) scale(0.97); }
    to { opacity: 1; transform: translateY(0) scale(1); }
  }

  .drop-head { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-bottom: 10px; }
  .drop-head h4 { font-family: var(--bb-font-display); font-weight: 700; font-size: 15px; letter-spacing: -0.01em; color: var(--bb-white); margin: 0; }

  .empty { color: var(--bb-muted); font-family: var(--bb-font-body); font-size: 13px; margin: 4px 0 4px; }

  .items { display: flex; flex-direction: column; gap: 8px; max-height: min(420px, 60vh); overflow-y: auto; }
  /* Level pill + Read share the top row; title/body take the full card width
     below, so the text never gets squeezed into a sliver between them. */
  .item {
    display: grid; grid-template-columns: 1fr auto; align-items: center; gap: 8px 10px;
    border: 1px solid var(--bb-border, rgba(201, 168, 124, 0.15)); border-radius: 8px 8px;
    padding: 10px 12px; background: rgba(255, 255, 255, 0.02);
  }
  .item .level { justify-self: start; grid-row: 1; }
  .item .text { grid-column: 1 / -1; }
  .item .btn { grid-column: 2; grid-row: 1; justify-self: end; }
  .item.unread { border-color: rgba(201, 168, 124, 0.3); background: rgba(201, 168, 124, 0.05); }

  .text { flex: 1; min-width: 0; }
  .text b { font-family: var(--bb-font-body); font-size: 13.5px; color: var(--bb-white); }
  .text p { margin: 4px 0 0; font-family: var(--bb-font-body); font-size: 12.5px; color: var(--bb-muted); line-height: 1.45; }

  /* Pill triad retired: the .bb-tag variants above carry info/success/warning.
     Only the layout and critical's red survive as scoped rules. */
  .level { font-size: 10px; white-space: nowrap; }
  .level.critical { color: #d98a8a; border-bottom-color: rgba(217, 138, 138, 0.45); }

  .btn.sm { padding: 4px 10px; font-size: 11px; white-space: nowrap; }

  .view-all {
    font-family: var(--bb-font-body); font-weight: 600; font-size: 12px;
    color: var(--bb-tan); text-decoration: none;
  }
  .view-all:hover { color: var(--bb-tan-pale); }

  @media (prefers-reduced-motion: reduce) {
    .dropdown { animation: none; }
  }
</style>
