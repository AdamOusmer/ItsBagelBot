<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // Responsive picker shell: an anchored dropdown on desktop and a modal
  // bottom sheet on mobile. Callers own the picker contents and selection.
  //
  // It exists because an in-flow dropdown cannot work there. The palette lives
  // inside InspectorSurface, which sets `overflow: hidden` to clip its own
  // scroller, so an absolutely-positioned panel is clipped at the editor's edge:
  // the menu opens "inside" the editor and the half that matters is
  // unreachable. Portalling to <body> and positioning fixed is the same escape
  // hatch InspectorSurface uses for its own mobile sheet.
  //
  // Below MOBILE_QUERY it stops pretending to be a dropdown at all: anchoring a
  // 300px panel to a chip on a 375px screen leaves it hanging off one edge or
  // covering the field you are editing. It becomes a bottom sheet instead, which
  // is the same shape the inspector already takes at that width.
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
    // full height stays on screen: a palette sits low in a long form, so the
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
  //
  // A scroll event whose target is INSIDE panelEl is ignored rather than
  // closing: the capture-phase listener sees every scroll in the document,
  // including a wheel over the panel's own scrolling content (a long list —
  // VariablePalette's "All variables" sheet is the case that surfaced this),
  // and that scroll does not move the anchor at all. Closing on it made the
  // panel impossible to read past its first screenful: mid-scroll, it tore
  // itself down. A scroll of the PAGE behind the panel — the actual "the
  // anchor moved" case this effect exists for — still closes it, since its
  // target is never inside panelEl.
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
  onkeydowncapture={(e) => {
    if (!open || e.key !== 'Escape') return;
    if (isSheet && !isTopmost(overlayId)) return;
    e.preventDefault();
    // CAPTURE, not bubble, and stopped immediately: the desktop dropdown
    // deliberately never joins the overlay stack (it is non-modal — see the
    // effect above), so an ancestor's own Escape handler (InspectorSurface,
    // Modal, anything using the same window-keydown pattern) sees the exact
    // same keypress with nothing telling it "a picker just consumed this".
    // A bubble-phase listener here raced that ancestor's OWN bubble-phase
    // window listener on registration order alone — whichever mounted first
    // won — and in practice the ancestor's ran first: opening a command's
    // editor, opening this panel from inside it, and pressing Escape closed
    // the picker AND the whole editor in one keystroke (VariablePalette's
    // "All variables" sheet is what surfaced this). Capture always runs
    // before any bubble-phase listener on any target, ancestor mount order
    // or not, so stopImmediatePropagation here reliably wins.
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
