<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.

  import type { Snippet } from 'svelte';
  import '../styles/elements/picker-panel.css';
  import { mediaQuery } from '../lib/motion-query';
  import { portal, pushOverlay, removeOverlay, isTopmost, overlayIndex, trapFocus } from '../lib/overlay-stack';

  let {
    open = false,
    anchor,
    label,
    width = 300,
    maxHeight = 340,
    onClose,
    children
  }: {
    open?: boolean;
    anchor?: HTMLElement;
    label: string;
    width?: number;
    maxHeight?: number;
    onClose: () => void;
    children: Snippet;
  } = $props();

  const MOBILE_QUERY = '(max-width: 639px)';
  const GAP_PX = 8;

  const sheetQuery = mediaQuery(MOBILE_QUERY);
  let isSheet = $state(sheetQuery.matches);
  let pos = $state({ top: 0, left: 0 });
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

  function place() {
    if (!anchor) return;
    const r = anchor.getBoundingClientRect();
    const left =
      r.right + GAP_PX + width <= window.innerWidth ? r.right + GAP_PX : Math.max(GAP_PX, r.left - GAP_PX - width);
    const top = Math.max(GAP_PX, Math.min(r.top, window.innerHeight - GAP_PX - maxHeight));
    pos = { top, left };
  }

  $effect(() => {
    if (open && !isSheet) place();
  });

  $effect(() => {
    if (!open || isSheet) return;
    const close = (e: Event) => {
      if (panelEl && e.target instanceof Node && panelEl.contains(e.target)) return;
      onClose();
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
      if (panelEl?.contains(t) || anchor?.contains(t)) return;
      onClose();
    };
    document.addEventListener('pointerdown', onDown, true);
    return () => document.removeEventListener('pointerdown', onDown, true);
  });
</script>

<svelte:window
  onkeydowncapture={(e) => {
    if (!open || e.key !== 'Escape') return;
    if (isSheet && !isTopmost(overlayId)) return;
    e.preventDefault();
    e.stopImmediatePropagation();
    onClose();
  }}
/>

{#if open}
  {#if isSheet}
    <div class="bb-picker-panel__shell" data-overlay style="z-index: {zIndex}" use:portal>
      <button class="bb-picker-panel__scrim" type="button" aria-label={label} onclick={onClose}></button>
      <div class="bb-picker-panel bb-picker-panel--sheet" role="dialog" aria-modal="true" aria-label={label} bind:this={panelEl} tabindex="-1" use:trapFocus>
        <span class="bb-picker-panel__grabber" aria-hidden="true"></span>
        {@render children()}
      </div>
    </div>
  {:else}
    <div
      class="bb-picker-panel bb-picker-panel--dropdown"
      data-overlay
      role="dialog"
      aria-label={label}
      style="top: {pos.top}px; left: {pos.left}px; width: {width}px; max-height: {maxHeight}px"
      bind:this={panelEl}
      use:portal
    >
      {@render children()}
    </div>
  {/if}
{/if}
