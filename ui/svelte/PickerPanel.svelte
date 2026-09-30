<script module lang="ts">
  // Capture listeners run in mount order; nested pickers dismiss newest first.
  const openPickerRoots = new Set<HTMLElement>();
</script>

<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.

  import type { Snippet } from 'svelte';
  import '../styles/elements/picker-panel.css';
  import { mediaQuery } from '../lib/motion-query';
  import { naturalHeight, placeDropdown, type DropdownPlacement } from '../lib/dropdown-placement';
  import { MOBILE_QUERY, portal, pushOverlay, removeOverlay, isTopmost, overlayIndex, trapFocus, registerOverlayAnchor, overlayContains, wirePopupAnchor, focusWithin, FOCUSABLE } from '../lib/overlay-stack';

  const uid = $props.id();

  let {
    open = $bindable(false),
    anchor,
    label,
    id = $bindable(`bb-picker-${uid}`),
    width = 300,
    maxHeight = 340,
    placement = 'beside',
    onClose,
    onOpenChange,
    children
  }: {
    open?: boolean;
    anchor?: HTMLElement;
    label: string;
    id?: string;
    width?: number;
    maxHeight?: number;
    placement?: 'beside' | 'below';
    onClose?: () => void;
    onOpenChange?: (open: boolean) => void;
    children: Snippet;
  } = $props();

  const GAP_PX = 8;

  let wasOpen = false;

  $effect.pre(() => {
    const closing = wasOpen && !open;
    wasOpen = open;
    if (closing && anchor && panelEl && overlayContains(panelEl, document.activeElement)) anchor.focus({ preventScroll: true });
  });

  $effect(() => {
    if (anchor) return wirePopupAnchor(anchor, open, id);
  });

  $effect(() => {
    if (open && !isSheet && panelEl) focusWithin(panelEl);
  });

  function leaveDropdown(e: KeyboardEvent) {
    if (e.key !== 'Tab' || !panelEl) return;
    const items = Array.from(panelEl.querySelectorAll<HTMLElement>(FOCUSABLE));
    const active = document.activeElement;
    const leaving = e.shiftKey ? active === panelEl || active === items[0] : active === (items.at(-1) ?? panelEl);
    if (!leaving) return;
    e.preventDefault();
    requestClose();
    anchor?.focus({ preventScroll: true });
  }

  function requestClose() {
    onOpenChange?.(false);
    // onClose owns the state so it can veto; open is written only without one.
    if (onClose) onClose();
    else open = false;
  }

  const sheetQuery = mediaQuery(MOBILE_QUERY);
  let isSheet = $state(sheetQuery.matches);
  let pos = $state<DropdownPlacement>({ top: 0, left: 0, width: 0, maxHeight: 0 });
  let panelEl = $state<HTMLDivElement>();
  let overlayId = 0;
  let zIndex = $state(300);

  $effect(() => {
    const mq = sheetQuery;
    const update = () => (isSheet = mq.matches);
    update();
    mq.addEventListener('change', update);
    return () => mq.removeEventListener('change', update);
  });

  $effect(() => {
    if (!open || !isSheet) return;
    const id = pushOverlay();
    overlayId = id;
    zIndex = 300 + overlayIndex(id) * 10;
    return () => removeOverlay(id);
  });

  $effect(() => {
    if (!open || !panelEl) return;
    const root = panelEl.closest<HTMLElement>('[data-overlay]') || panelEl;
    openPickerRoots.add(root);
    const unregister = anchor ? registerOverlayAnchor(root, anchor) : undefined;
    return () => {
      unregister?.();
      openPickerRoots.delete(root);
    };
  });

  function besideAnchor(r: DOMRect): DropdownPlacement {
    const left =
      r.right + GAP_PX + width <= window.innerWidth ? r.right + GAP_PX : Math.max(GAP_PX, r.left - GAP_PX - width);
    const top = Math.max(GAP_PX, Math.min(r.top, window.innerHeight - GAP_PX - maxHeight));
    return { top, left, width, maxHeight };
  }

  function place() {
    if (!anchor || !panelEl) return;
    const panel = panelEl;
    const r = anchor.getBoundingClientRect();
    const viewport = { width: window.innerWidth, height: window.innerHeight };
    pos = placement === 'below' ? placeDropdown(r, viewport, maxHeight, (w) => naturalHeight(panel, w)) : besideAnchor(r);
  }

  const px = (value: number | undefined) => (value === undefined ? undefined : `${value}px`);

  $effect(() => {
    if (open && !isSheet) place();
  });

  $effect(() => {
    if (!open || isSheet) return;
    const close = (e: Event) => {
      if (e.type === 'scroll' && panelEl && e.target instanceof Node && overlayContains(panelEl, e.target)) return;
      requestClose();
    };
    window.addEventListener('scroll', close, { capture: true, passive: true });
    window.addEventListener('resize', close, { passive: true });
    return () => {
      window.removeEventListener('scroll', close, { capture: true });
      window.removeEventListener('resize', close);
    };
  });

  $effect(() => {
    if (!open) return;
    const onDown = (e: PointerEvent) => {
      const t = e.target as Node | null;
      if (!t) return;
      if ((panelEl && overlayContains(panelEl, t)) || anchor?.contains(t)) return;
      requestClose();
    };
    document.addEventListener('pointerdown', onDown, true);
    return () => document.removeEventListener('pointerdown', onDown, true);
  });
</script>

<svelte:window
  onkeydowncapture={(e) => {
    if (!open || e.key !== 'Escape' || !panelEl) return;
    const root = panelEl.closest<HTMLElement>('[data-overlay]') || panelEl;
    if (Array.from(openPickerRoots).at(-1) !== root) return;
    // A child portal may use another adapter and handle Escape at its target.
    if (e.target instanceof Node && !root.contains(e.target) && overlayContains(root, e.target)) return;
    if (isSheet && !isTopmost(overlayId)) return;
    e.preventDefault();
    e.stopImmediatePropagation();
    requestClose();
  }}
/>

{#if open}
  {#if isSheet}
    <div class="bb-picker-panel__shell" data-overlay style="z-index: {zIndex}" use:portal>
      <button class="bb-picker-panel__scrim" type="button" tabindex="-1" aria-hidden="true" onclick={requestClose}></button>
      <div class="bb-picker-panel bb-picker-panel--sheet" data-lenis-prevent role="dialog" aria-modal="true" aria-label={label} {id} bind:this={panelEl} tabindex="-1" use:trapFocus>
        <span class="bb-picker-panel__grabber" aria-hidden="true"></span>
        {@render children()}
      </div>
    </div>
  {:else}
    <div
      class="bb-picker-panel bb-picker-panel--dropdown"
      data-overlay
      data-lenis-prevent
      role="dialog"
      aria-label={label}
      {id}
      tabindex="-1"
      onkeydown={leaveDropdown}
      style:top={px(pos.top)}
      style:bottom={px(pos.bottom)}
      style:left={px(pos.left)}
      style:width={px(pos.width)}
      style:max-height={px(pos.maxHeight)}
      bind:this={panelEl}
      use:portal
    >
      {@render children()}
    </div>
  {/if}
{/if}
