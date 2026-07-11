<script lang="ts">
  import type { Snippet } from 'svelte';
  import { pushOverlay, removeOverlay, isTopmost, overlayIndex, portal, trapFocus } from '../lib/overlay-stack';

  let {
    open = false,
    title,
    closeModal,
    busy = false,
    closeLabel = 'Close',
    children
  }: {
    open: boolean;
    title?: string;
    closeModal: () => void;
    // While busy (e.g. a save in flight) the surface is non-dismissible: Escape
    // and backdrop clicks are ignored so an action can't be abandoned mid-commit.
    busy?: boolean;
    // Accessible name for the backdrop dismiss control (localise at call sites).
    closeLabel?: string;
    children: Snippet;
  } = $props();

  // Unique per instance, so two stacked dialogs never collide on the id that
  // aria-labelledby points at.
  const uid = $props.id();
  const titleId = `modal-title-${uid}`;
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
  <div class="modal-shell" data-overlay style="z-index: {zIndex}" use:portal>
    <!-- Backdrop is a real button so dismissal is native (no static-role / key
         warnings); dialog semantics live on the card, its sibling. -->
    <button class="modal-backdrop" type="button" aria-label={closeLabel} onclick={tryClose}></button>
    <div
      class="modal-card"
      role="dialog"
      aria-modal="true"
      tabindex="-1"
      aria-labelledby={title ? titleId : undefined}
      data-lenis-prevent
      use:trapFocus
    >
      {#if title}
        <h3 id={titleId}>{title}</h3>
      {/if}
      {@render children()}
    </div>
  </div>
{/if}

<style>
  .modal-shell {
    position: fixed; inset: 0;
    display: flex; align-items: center; justify-content: center; padding: 16px;
  }
  .modal-backdrop {
    position: absolute; inset: 0;
    padding: 0; border: 0; cursor: pointer;
    background: rgba(0, 0, 0, 0.55);
    backdrop-filter: blur(4px); -webkit-backdrop-filter: blur(4px);
  }
  .modal-card {
    position: relative;
    background: var(--bb-bg-1, #111);
    border: 1px solid var(--glass-border);
    border-radius: 8px;
    backdrop-filter: blur(var(--glass-blur)); -webkit-backdrop-filter: blur(var(--glass-blur));
    padding: 28px 28px 24px; max-width: 420px; width: 100%;
    max-height: calc(100dvh - 32px); overflow-y: auto; overscroll-behavior: contain;
    -webkit-overflow-scrolling: touch;
  }
  .modal-card:focus { outline: none; }
  .modal-card h3 {
    font-family: var(--bb-font-display); font-weight: 700; font-size: 19px;
    color: var(--bb-white); margin: 0 0 12px; letter-spacing: -0.01em;
  }

  :global(.modal-body) {
    font-family: var(--bb-font-body); font-size: 14px; color: var(--bb-muted);
    line-height: 1.55; margin: 0 0 22px;
  }
  :global(.modal-actions) {
    display: flex; gap: .6rem; justify-content: flex-end; flex-wrap: wrap;
  }

  @media (max-width: 480px) {
    :global(.modal-actions) {
      flex-direction: column-reverse;
    }
    :global(.modal-actions .btn),
    :global(.modal-actions button) {
      width: 100%;
      justify-content: center;
      min-height: 44px;
    }
  }

  @media (max-width: 380px) {
    .modal-card { padding: 20px 16px 18px; }
  }
</style>
