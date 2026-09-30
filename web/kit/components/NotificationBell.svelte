<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.

  import '../styles/notifications.css';
  import type { ComponentProps } from 'svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import Badge from '@bagel/ui/svelte/Badge.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import { focusWithin, hasOpenOverlay } from '@bagel/ui/lib/overlay-stack';

  export interface BellNotification {
    id: number;
    title: string;
    body: string;
    level: 'info' | 'success' | 'warning' | 'critical';
    created_at: string;
    read?: boolean;
  }

  type Level = BellNotification['level'];
  type MarkRead = { onMarkRead: (id: number) => void; readLabel: string } | { onMarkRead?: undefined; readLabel?: string };

  let {
    notifications,
    unreadCount = 0,
    viewAllHref,
    onMarkRead,
    onOpen,
    emptyLabel,
    title,
    viewAllLabel,
    readLabel,
    unreadLabel,
    levelLabels,
    headingLevel = 4
  }: {
    notifications: BellNotification[];
    unreadCount?: number;
    viewAllHref: string;
    onOpen?: () => void;
    emptyLabel: string;
    title: string;
    viewAllLabel: string;
    unreadLabel: (count: number) => string;
    levelLabels: Record<Level, string>;
    headingLevel?: 1 | 2 | 3 | 4 | 5 | 6;
  } & MarkRead = $props();

  const uid = $props.id();
  const panelId = `bb-notifications-panel-${uid}`;
  const titleId = `bb-notifications-title-${uid}`;

  let open = $state(false);
  let peeked = $state(false);
  let wrap = $state<HTMLDivElement>();
  let trigger = $state<HTMLButtonElement>();
  let panel = $state<HTMLDivElement>();

  function toggle() {
    open = !open;
    if (open && onOpen && !peeked) {
      peeked = true;
      onOpen();
    }
  }

  function close(returnFocus = false) {
    open = false;
    if (returnFocus) trigger?.focus();
  }

  function onkeydown(event: KeyboardEvent) {
    if (open && event.key === 'Escape' && !hasOpenOverlay()) close(true);
  }

  function onfocusout(event: FocusEvent) {
    const next = event.relatedTarget;
    if (open && next instanceof Node && !wrap?.contains(next)) close();
  }

  $effect(() => {
    if (open && panel) focusWithin(panel);
  });

  const isBadgeSuppressed = $derived(Boolean(onOpen && peeked));
  const showBadge = $derived(unreadCount > 0 && !isBadgeSuppressed);
  const triggerName = $derived(showBadge ? `${title}, ${unreadLabel(unreadCount)}` : title);

  const LEVEL_TONE = {
    info: 'quiet',
    success: 'live',
    warning: 'pre',
    critical: 'alpha'
  } as const satisfies Record<string, ComponentProps<typeof Badge>['tone']>;
</script>

<svelte:window {onkeydown} />

<div class="bb-notifications__bell-wrap" bind:this={wrap} {onfocusout}>
  <button
    class="bb-notifications__icon-btn"
    class:bb-notifications__open={open}
    type="button"
    aria-label={triggerName}
    aria-expanded={open}
    aria-haspopup="dialog"
    aria-controls={open ? panelId : undefined}
    bind:this={trigger}
    onclick={toggle}
  >
    <Icon name="bell" size={16} />
    {#if showBadge}<Badge tone="bare" mark="solid" class="bb-notifications__badge">{unreadCount > 9 ? '9+' : unreadCount}</Badge>{/if}
  </button>

  {#if open}
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div class="bb-notifications__scrim" role="presentation" onclick={() => close()}></div>

    <div
      class="bb-notifications__dropdown"
      id={panelId}
      role="dialog"
      aria-modal="false"
      aria-labelledby={titleId}
      tabindex="-1"
      bind:this={panel}
    >
      <div class="bb-notifications__drop-head">
        <svelte:element this={`h${headingLevel}`} class="bb-notifications__title" id={titleId}>{title}</svelte:element>
        <a class="bb-notifications__view-all" href={viewAllHref} onclick={() => close()}>{viewAllLabel}</a>
      </div>
      {#if notifications.length === 0}
        <p class="bb-notifications__empty">{emptyLabel}</p>
      {:else}
        <div class="bb-notifications__items">
          {#each notifications as n (n.id)}
            <div class="bb-notifications__item" class:bb-notifications__unread={!n.read}>
              <Badge
                tone={LEVEL_TONE[n.level as keyof typeof LEVEL_TONE] ?? 'quiet'}
                class="bb-notifications__level bb-notifications__{n.level}">{levelLabels[n.level] ?? n.level}</Badge>
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
