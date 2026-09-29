<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/toast.css';
  import Icon from './Icon.svelte';
  import { toasts, dismissToast, pauseToast, resumeToast, type ToastItem } from './toast.svelte';

  let {
    dismissLabel = 'Dismiss',
    undoLabel = 'Undo',
  }: {
    dismissLabel?: string;
    undoLabel?: string;
  } = $props();

  function leave(e: MouseEvent, id: number) {
    const el = e.currentTarget as HTMLElement;
    if (!el.contains(document.activeElement)) resumeToast(id);
  }

  function blur(e: FocusEvent, id: number) {
    const el = e.currentTarget as HTMLElement;
    if (!el.contains(e.relatedTarget as Node | null) && !el.matches(':hover')) resumeToast(id);
  }

  function undo(t: ToastItem) {
    t.onUndo?.();
    dismissToast(t.id);
  }
</script>

{#if $toasts.length}
  <div class="bb-toast-stack" aria-live="polite">
    {#each $toasts as t (t.id)}
      <div
        class="bb-toast bb-toast--{t.kind}"
        role="status"
        onmouseenter={() => pauseToast(t.id)}
        onmouseleave={(e) => leave(e, t.id)}
        onfocusin={() => pauseToast(t.id)}
        onfocusout={(e) => blur(e, t.id)}
      >
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
