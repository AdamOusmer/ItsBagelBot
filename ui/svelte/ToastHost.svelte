<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/toast.css';
  import Icon from './Icon.svelte';
  import { toasts, dismissToast, type ToastItem } from './toast.svelte';

  let {
    dismissLabel = 'Dismiss',
    undoLabel = 'Undo',
  }: {
    dismissLabel?: string;
    undoLabel?: string;
  } = $props();

  function undo(t: ToastItem) {
    t.onUndo?.();
    dismissToast(t.id);
  }
</script>

{#if $toasts.length}
  <div class="bb-toast-stack" aria-live="polite">
    {#each $toasts as t (t.id)}
      <div class="bb-toast bb-toast--{t.kind}" role="status">
        <span class="bb-toast__text">{t.text}</span>
        {#if t.onUndo}
          <button class="bb-toast__undo" type="button" onclick={() => undo(t)}
            >{t.undoLabel ?? undoLabel}</button
          >
        {/if}
        <button
          class="bb-toast__close"
          type="button"
          aria-label={dismissLabel}
          onclick={() => dismissToast(t.id)}
        >
          <Icon name="x" size={12} />
        </button>
      </div>
    {/each}
  </div>
{/if}
