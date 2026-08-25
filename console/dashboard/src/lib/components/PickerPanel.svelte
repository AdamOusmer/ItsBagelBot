<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The dropdown shell shared by the command editor's palette menus (counters,
  // data sources).
  //
  // It exists because an in-flow dropdown cannot work there. The palette lives
  // inside InspectorSurface, which sets `overflow: hidden` to clip its own
  // scroller, so an absolutely-positioned panel is clipped at the editor's edge
  // — the menu opens "inside" the editor and the half that matters is
  // unreachable. Portalling to <body> and positioning fixed is the same escape
  // hatch InspectorSurface uses for its own mobile sheet.
  //
  // Below MOBILE_QUERY it stops pretending to be a dropdown at all: anchoring a
  // 300px panel to a chip on a 375px screen leaves it hanging off one edge or
  // covering the field you are editing. It becomes a bottom sheet instead, which
  // is the same shape the inspector already takes at that width.
  import type { Snippet } from 'svelte';
  import { portal, pushOverlay, removeOverlay, isTopmost, overlayIndex } from '@bagel/shared';

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
    /** Trigger element; desktop placement is measured from its rect. */
    anchor?: HTMLElement;
    label: string;
    width?: number;
    maxHeight?: number;
    onClose: () => void;
    children: Snippet;
  } = $props();

  const MOBILE_QUERY = '(max-width: 639px)';
  const GAP = 8;

  // Initialised synchronously so the first render is already the right shape; a
  // false->true swap on mount would tear the panel down and rebuild it.
  let isSheet = $state(typeof window !== 'undefined' && window.matchMedia(MOBILE_QUERY).matches);
  let pos = $state({ top: 0, left: 0 });
  let panelEl = $state<HTMLDivElement>();
  let overlayId = 0;
  let zIndex = $state(300);

  $effect(() => {
    const mq = window.matchMedia(MOBILE_QUERY);
    const update = () => (isSheet = mq.matches);
    update();
    mq.addEventListener('change', update);
    return () => mq.removeEventListener('change', update);
  });

  // Only the sheet joins the overlay stack: it is modal (scrim, scroll lock,
  // page inert). The desktop dropdown is non-modal and must not lock anything.
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
    // Prefer the right of the trigger, fall back to its left, then clamp so the
    // full height stays on screen — a palette sits low in a long form, so the
    // naive "below the trigger" placement runs off the bottom exactly when the
    // form is longest.
    const left =
      r.right + GAP + width <= window.innerWidth ? r.right + GAP : Math.max(GAP, r.left - GAP - width);
    const top = Math.max(GAP, Math.min(r.top, window.innerHeight - GAP - maxHeight));
    pos = { top, left };
  }

  // Re-measure whenever it opens (or the viewport class flips while open).
  $effect(() => {
    if (open && !isSheet) place();
  });

  // Desktop coords are a snapshot, so movement invalidates them: close rather
  // than chase, since a drifting dropdown reads as a bug. The sheet is fixed to
  // the viewport and does not care.
  $effect(() => {
    if (!open || isSheet) return;
    const close = () => onClose();
    window.addEventListener('scroll', close, { capture: true, passive: true });
    window.addEventListener('resize', close, { passive: true });
    return () => {
      window.removeEventListener('scroll', close, { capture: true });
      window.removeEventListener('resize', close);
    };
  });

  // Dismiss on a click outside the panel and its trigger. Pointerdown rather
  // than click so it fires before a button inside the panel re-renders away.
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
  onkeydown={(e) => {
    if (!open || e.key !== 'Escape') return;
    if (isSheet && !isTopmost(overlayId)) return;
    e.preventDefault();
    onClose();
  }}
/>

{#if open}
  {#if isSheet}
    <div class="sheet-shell" data-overlay style="z-index: {zIndex}" use:portal>
      <button class="sheet-scrim" type="button" aria-label={label} onclick={onClose}></button>
      <div class="panel sheet" role="dialog" aria-modal="true" aria-label={label} bind:this={panelEl}>
        <span class="grabber" aria-hidden="true"></span>
        {@render children()}
      </div>
    </div>
  {:else}
    <div
      class="panel drop"
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

<style>
  .panel {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px;
    background: var(--bb-bg-1, #111);
    border: 1px solid var(--bb-border);
    overscroll-behavior: contain;
  }

  .drop {
    position: fixed;
    z-index: 300;
    overflow-y: auto;
    border-radius: 10px;
    box-shadow: 0 12px 32px rgba(0, 0, 0, 0.45);
  }
  :global(:root[data-theme='light']) .drop { box-shadow: 0 12px 32px rgba(20, 17, 12, 0.15); }

  /* Mobile: bottom sheet, same shape the inspector takes at this width. */
  .sheet-shell { position: fixed; inset: 0; }
  .sheet-scrim {
    position: absolute;
    inset: 0;
    padding: 0;
    border: 0;
    background: rgba(0, 0, 0, 0.55);
  }
  .sheet {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    /* Leaves the top of the screen visible so the sheet reads as covering the
       page rather than being the whole page. */
    max-height: 82dvh;
    overflow-y: auto;
    border-radius: 12px 12px 0 0;
    padding: 8px 16px calc(16px + env(safe-area-inset-bottom, 0px));
    /* No fill mode on purpose: the element's resting state is then the visible
       one, and the slide-in is decoration layered over it. With `both` the
       resting state before the animation starts is translateY(100%) — fully
       off-screen — so anything that keeps the animation from starting leaves
       the sheet permanently invisible.

       This does not cover a suspended mid-run animation (a pane that is not
       painting holds currentTime at 0, and the `from` keyframe applies during
       the active phase whatever the fill), but that resolves on its own the
       moment the compositor runs. Reduced motion drops the animation entirely
       below, which is the case that would otherwise strand it. */
    animation: sheet-in var(--bb-dur-base, 320ms) var(--bb-ease-out-expo, cubic-bezier(0.16, 1, 0.3, 1));
  }
  .grabber {
    align-self: center;
    width: 36px;
    height: 4px;
    flex: none;
    margin-bottom: 4px;
    border-radius: 999px;
    background: var(--rule, rgba(255, 255, 255, 0.18));
  }
  @keyframes sheet-in {
    from { transform: translateY(100%); }
    to { transform: translateY(0); }
  }
  @media (prefers-reduced-motion: reduce) {
    .sheet { animation: none; }
  }
</style>
