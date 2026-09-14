<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Topbar notification bell: unread badge + an anchored dropdown of recent
  // items (not a centered modal, notifications are a glance, not a task).
  // Presentational only: the host page owns fetching/caching the list and
  // wiring onMarkRead to its own form action (mark-read semantics differ
  // between the dashboard, which tracks per-user read state, and admin,
  // which has none and passes no onMarkRead at all).
  import '../styles/elements/notifications.css';
  import type { ComponentProps } from 'svelte';
  import Icon from './Icon.svelte';
  import Badge from './Badge.svelte';
  import Button from './Button.svelte';

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

  // Severity -> Badge tone. Same map as the settings notification list, so one
  // level reads identically in both surfaces. No tone is red, so critical takes
  // `alpha` plus the danger colour below.
  const LEVEL_TONE = {
    info: 'quiet',
    success: 'live',
    warning: 'pre',
    critical: 'alpha'
  } as const satisfies Record<string, ComponentProps<typeof Badge>['tone']>;
</script>

<svelte:window onkeydown={(e) => { if (e.key === 'Escape') open = false; }} />

<div class="bb-notifications__bell-wrap">
  <button
    class="bb-notifications__icon-btn"
    class:bb-notifications__open={open}
    aria-label={title}
    aria-expanded={open}
    aria-haspopup="menu"
    onclick={toggle}
  >
    <Icon name="bell" size={16} />
    {#if showBadge}<Badge tone="bare" mark="solid" class="bb-notifications__badge">{unreadCount > 9 ? '9+' : unreadCount}</Badge>{/if}
  </button>

  {#if open}
    <!-- Click-away scrim; Escape (window handler above) is the keyboard path. -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="bb-notifications__scrim"
      role="presentation"
      onclick={() => (open = false)}
      onkeydown={(e) => { if (e.key === 'Enter') open = false; }}
    ></div>

    <div class="bb-notifications__dropdown" role="menu" aria-label="Recent notifications">
      <div class="bb-notifications__drop-head">
        <h4>{title}</h4>
        <a class="bb-notifications__view-all" href={viewAllHref} onclick={() => (open = false)}>{viewAllLabel}</a>
      </div>
      {#if notifications.length === 0}
        <p class="bb-notifications__empty">{emptyLabel}</p>
      {:else}
        <div class="bb-notifications__items">
          {#each notifications as n (n.id)}
            <div class="bb-notifications__item" class:bb-notifications__unread={!n.read}>
              <Badge
                tone={LEVEL_TONE[n.level as keyof typeof LEVEL_TONE] ?? 'quiet'}
                class="bb-notifications__level bb-notifications__{n.level}">{n.level}</Badge>
              <div class="bb-notifications__text">
                <b>{n.title}</b>
                <p>{n.body}</p>
              </div>
              {#if onMarkRead && !n.read}
                <Button variant="ghost" size="sm" class="bb-notifications__mark-read" onclick={() => onMarkRead?.(n.id)}
                  >{readLabel}</Button
                >
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
</div>
