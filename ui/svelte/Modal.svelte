<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import { getUiI18n } from './i18n';

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

  const i18n = getUiI18n();
  type Own = {
    open: boolean;
    title?: string;
    closeModal: () => void;
    busy?: boolean;
    closeLabel?: string;
    ariaLabel?: string;
    variant?: 'dialog' | 'viewer';
    toolbarLabel?: string;
    class?: string;
    children?: Snippet;
    toolbar?: Snippet;
    hint?: Snippet;
  };

  let {
    open = false,
    title,
    closeModal,
    busy = false,
    closeLabel = i18n.t('action.close'),
    ariaLabel,
    variant = 'dialog',
    toolbarLabel,
    class: className = '',
    children,
    toolbar,
    hint,
    ...rest
  }: Own & Omit<SvelteHTMLElements['div'], keyof Own> = $props();

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

  const viewer = $derived(variant === 'viewer');
  const classes = $derived(
    ['bb-modal', viewer ? 'bb-modal--viewer' : null, className || null].filter(Boolean).join(' '),
  );
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
    >{#if title}<h3 class="bb-modal__title" id={titleId}>{title}</h3>{/if}{#if viewer}<div class="bb-modal__stage"
          >{#if children}{@render children()}{/if}</div
        >{#if toolbar}<div class="bb-modal__toolbar" role="toolbar" aria-label={toolbarLabel}
            >{@render toolbar()}</div
          >{/if}{#if hint}<div class="bb-modal__hint" aria-hidden="true">{@render hint()}</div>{/if}{:else if children}{@render children()}{/if}</div
    ></div>
{/if}
