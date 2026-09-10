<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for the `.bb-modal` contract (../styles/elements/modal.css).
  // Its Astro twin is ../astro/Modal.astro, which draws the surface WITHOUT the
  // overlay behaviour (a guide screenshot has nothing to trap focus in);
  // ../test/parity.test.ts holds the open, focus-managed markup of the two to
  // the same shape.
  //
  // Behaviour lives in ../lib/overlay-stack.ts and is shared with
  // InspectorSurface: a ref-counted scroll lock, background `inert`,
  // topmost-only Escape, z-order for nested overlays, portal to <body> and a
  // focus trap. Each of those was decentralised once — every overlay owned its
  // own body.overflow write (nested overlays fought over it), every open
  // overlay listened for Escape on window (one keypress closed two surfaces),
  // dialog semantics sat on the backdrop, and focus was never trapped or
  // restored.
  //
  // THE BACKDROP IS A REAL BUTTON. Dismissal is then native — no static-element
  // click handler, no role or keyboard warnings, Enter and Space work with no
  // keydown listener. Dialog semantics live on the card, its sibling, so the
  // dialog's accessible name is not the word "Close".
  //
  // `data-lenis-prevent` on the card: the smooth-scroll engine otherwise
  // intercepts a wheel inside the dialog and scrolls the (frozen) page.
  //
  // `data-overlay=""` and not a bare `data-overlay`: this element also carries
  // a `{...rest}` spread, and Svelte compiles every attribute on a spread
  // element through one object, where a valueless attribute becomes `true` and
  // serialises as `data-overlay="true"`. The overlay stack's inert sweep uses
  // `[data-overlay]`, so both spellings work — but the Astro adapter emits the
  // bare form and the parity diff is the only place that difference is ever
  // visible.
  import '../styles/elements/modal.css';
  import type { Snippet } from 'svelte';
  import {
    pushOverlay,
    removeOverlay,
    isTopmost,
    overlayIndex,
    portal,
    trapFocus,
  } from '../lib/overlay-stack';

  let {
    open = false,
    title,
    closeModal,
    busy = false,
    closeLabel = 'Close',
    ariaLabel,
    class: className = '',
    children,
    ...rest
  }: {
    open: boolean;
    title?: string;
    closeModal: () => void;
    /**
     * While busy (a save in flight) the surface is non-dismissible: Escape and
     * backdrop clicks are ignored so an action cannot be abandoned mid-commit.
     */
    busy?: boolean;
    /** Accessible name for the backdrop dismiss control. Localise at the call site. */
    closeLabel?: string;
    /**
     * Accessible name for the dialog itself when there is no visible `title`
     * (a purely visual celebration modal). Pass one whenever title is omitted.
     */
    ariaLabel?: string;
    class?: string;
    children?: Snippet;
    [key: string]: unknown;
  } = $props();

  // Unique per instance, so two stacked dialogs never collide on the id that
  // aria-labelledby points at.
  const uid = $props.id();
  const titleId = `bb-modal-title-${uid}`;
  let overlayId = 0;
  let zIndex = $state(200);

  // Register with the shared stack while open: ref-counted scroll lock + inert,
  // topmost-only Escape, and z-order for nested overlays.
  $effect(() => {
    if (!open) return;
    const id = pushOverlay();
    overlayId = id;
    zIndex = 200 + overlayIndex(id) * 10;
    return () => removeOverlay(id);
  });

  function tryClose() {
    if (!busy) closeModal();
  }

  const classes = $derived(['bb-modal', className || null].filter(Boolean).join(' '));
</script>

<svelte:window
  onkeydown={(e) => {
    if (open && e.key === 'Escape' && isTopmost(overlayId)) {
      e.preventDefault();
      tryClose();
    }
  }}
/>

{#if open}
  <div class={classes} data-overlay="" style="z-index: {zIndex}" use:portal {...rest}><button
      class="bb-modal__backdrop"
      type="button"
      aria-label={closeLabel}
      data-cursor="quiet"
      onclick={tryClose}
    ></button><div
      class="bb-modal__card"
      role="dialog"
      aria-modal="true"
      tabindex="-1"
      aria-labelledby={title ? titleId : undefined}
      aria-label={title ? undefined : ariaLabel}
      data-lenis-prevent
      use:trapFocus
    >{#if title}<h3 class="bb-modal__title" id={titleId}>{title}</h3>{/if}{#if children}{@render children()}{/if}</div
    ></div>
{/if}
