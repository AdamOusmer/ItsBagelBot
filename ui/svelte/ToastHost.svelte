<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for the `.bb-toast` contract (../styles/elements/toast.css),
  // bound to the store in ./toast.svelte.ts. No Astro twin: a toast host with
  // nothing to subscribe to renders nothing, and a static one would be a
  // screenshot prop rather than an element.
  //
  // Mounted ONCE per app, in the root layout. Everything else calls `toast()`.
  //
  // The dismiss glyph is ./Icon.svelte at `name="x"`, the sibling adapter. It
  // was an inline copy of that path while the icon set still lived in the
  // console; the set is ../lib/icons.ts now and `icons.x` is byte-for-byte the
  // path this used to write, so the swap changed one class name and nothing
  // that draws.
  import '../styles/elements/toast.css';
  import Icon from './Icon.svelte';
  import { toasts, dismissToast, type ToastItem } from './toast.svelte';

  let {
    dismissLabel = 'Dismiss',
    undoLabel = 'Undo',
  }: {
    /** Accessible name for the per-toast close control. Localise at the call site. */
    dismissLabel?: string;
    /** Fallback label for the undo control when a toast supplies none. */
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
