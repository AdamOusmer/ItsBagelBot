<script lang="ts">
  import type { Snippet } from 'svelte';

  let {
    open = false,
    title,
    closeModal,
    children
  }: {
    open: boolean;
    title?: string;
    closeModal: () => void;
    children: Snippet;
  } = $props();

  $effect(() => {
    const lenis = (window as any).__lenis;
    if (open) {
      document.body.style.overflow = 'hidden';
      if (lenis) lenis.stop();
    } else {
      document.body.style.overflow = '';
      if (lenis) lenis.start();
    }
    return () => {
      document.body.style.overflow = '';
      if (lenis) lenis.start();
    };
  });
</script>

<svelte:window onkeydown={(e) => { if (open && e.key === 'Escape') closeModal(); }} />

{#if open}
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_noninteractive_element_interactions a11y_no_static_element_interactions -->
  <div class="modal-backdrop" onclick={closeModal} role="dialog" aria-modal="true" aria-labelledby="modal-title" tabindex="-1">
    <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_noninteractive_element_interactions a11y_no_static_element_interactions -->
    <div class="modal-card" role="presentation" data-lenis-prevent onclick={(e) => e.stopPropagation()}>
      {#if title}
        <h3 id="modal-title">{title}</h3>
      {/if}
      {@render children()}
    </div>
  </div>
{/if}

<style>
  .modal-backdrop {
    position: fixed; inset: 0; z-index: 200;
    background: rgba(0, 0, 0, 0.55);
    backdrop-filter: blur(4px); -webkit-backdrop-filter: blur(4px);
    display: flex; align-items: center; justify-content: center; padding: 16px;
  }
  .modal-card {
    background: var(--bb-bg-1, #111);
    border: 1px solid var(--glass-border);
    border-radius: 8px;
    backdrop-filter: blur(var(--glass-blur)); -webkit-backdrop-filter: blur(var(--glass-blur));
    padding: 28px 28px 24px; max-width: 420px; width: 100%;
    max-height: calc(100vh - 32px); overflow-y: auto; overscroll-behavior: contain;
    -webkit-overflow-scrolling: touch;
  }
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
