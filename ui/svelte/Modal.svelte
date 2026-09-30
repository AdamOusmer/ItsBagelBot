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
  type Named = { title: string } | { title?: undefined; label: string };
  type Toolbar = { toolbar?: undefined } | { toolbar: Snippet; toolbarLabel: string };
  type Own = {
    open?: boolean;
    title?: string;
    label?: string;
    toolbarLabel?: string;
    toolbar?: Snippet;
    onClose?: () => void;
    onOpenChange?: (open: boolean) => void;
    busy?: boolean;
    closeLabel?: string;
    variant?: 'dialog' | 'viewer';
    role?: 'dialog' | 'alertdialog';
    describedBy?: string;
    headingLevel?: 1 | 2 | 3 | 4 | 5 | 6;
    class?: string;
    children?: Snippet;
    hint?: Snippet;
  };

  let {
    open = $bindable(false),
    title,
    onClose,
    onOpenChange,
    busy = false,
    closeLabel = i18n.t('action.close'),
    label,
    variant = 'dialog',
    role = 'dialog',
    describedBy,
    headingLevel = 3,
    toolbarLabel,
    class: className = '',
    children,
    toolbar,
    hint,
    ...rest
  }: Own & Named & Toolbar & Omit<SvelteHTMLElements['div'], keyof Own | 'title'> = $props();

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
    if (busy) return;
    onOpenChange?.(false);
    // onClose owns the state so it can veto; open is written only without one.
    if (onClose) onClose();
    else open = false;
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
      {role}
      aria-modal="true"
      tabindex="-1"
      aria-describedby={describedBy}
      aria-labelledby={title ? titleId : undefined}
      aria-label={title ? undefined : label}
      data-lenis-prevent
      use:trapFocus
    >{#if title}<svelte:element this={`h${headingLevel}`} class="bb-modal__title" id={titleId}>{title}</svelte:element>{/if}{#if viewer}<div class="bb-modal__stage"
          >{#if children}{@render children()}{/if}</div
        >{#if toolbar}<div class="bb-modal__toolbar" role="toolbar" aria-label={toolbarLabel}
            >{@render toolbar()}</div
          >{/if}{#if hint}<div class="bb-modal__hint" aria-hidden="true">{@render hint()}</div>{/if}{:else if children}{@render children()}{/if}</div
    ></div>
{/if}
