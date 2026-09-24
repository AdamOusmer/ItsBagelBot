<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

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
    busy?: boolean;
    closeLabel?: string;
    ariaLabel?: string;
    class?: string;
    children?: Snippet;
    [key: string]: unknown;
  } = $props();

  const uid = $props.id();
  const titleId = `bb-modal-title-${uid}`;
  let overlayId = 0;
  let zIndex = $state(200);

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
