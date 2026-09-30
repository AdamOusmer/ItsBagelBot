<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import { getUiI18n } from './i18n';

  import '../styles/elements/card.css';
  import '../styles/elements/surface.css';
  import Icon from './Icon.svelte';
  import type { Snippet } from 'svelte';
  import { mediaQuery } from '../lib/motion-query';
  import {
    pushOverlay,
    removeOverlay,
    isTopmost,
    overlayIndex,
    hasOpenOverlay,
    portal,
    trapFocus,
  } from '../lib/overlay-stack';

  const i18n = getUiI18n();
  type Own = {
    open?: boolean;
    title: string;
    controls?: string;
    closeLabel?: string;
    class?: string;
    onClose?: () => void;
    onOpenChange?: (open: boolean) => void;
    children?: Snippet;
    idle?: Snippet;
  };

  let {
    open = $bindable(true),
    title,
    controls,
    closeLabel = i18n.t('action.close'),
    class: className = '',
    onClose,
    onOpenChange,
    children,
    idle,
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLElement>, keyof Own> = $props();

  const SHEET_QUERY = '(max-width: 1079px)';
  const sheetQuery = mediaQuery(SHEET_QUERY);

  let isSheet = $state(sheetQuery.matches);
  let overlayId = 0;
  let zIndex = $state(220);

  $effect(() => {
    const update = () => (isSheet = sheetQuery.matches);
    update();
    sheetQuery.addEventListener('change', update);
    return () => sheetQuery.removeEventListener('change', update);
  });

  let sheetEl = $state<HTMLDivElement>();

  $effect(() => {
    if (open && isSheet) {
      const id = pushOverlay();
      overlayId = id;
      zIndex = 220 + overlayIndex(id) * 10;
      return () => removeOverlay(id);
    }
  });

  const FOCUSABLE =
    'button:not([disabled]), a[href], input:not([disabled]), textarea:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])';
  $effect(() => {
    if (open && isSheet && sheetEl) {
      const first = sheetEl.querySelector<HTMLElement>(FOCUSABLE);
      (first ?? sheetEl).focus();
    }
  });

  function requestClose() {
    onOpenChange?.(false);
    // onClose owns the state so it can veto; open is written only without one.
    if (onClose) onClose();
    else open = false;
  }

  const dockedClasses = $derived(
    ['bb-surface', 'bb-card', 'bb-surface--docked', open ? null : 'bb-surface--idle', className || null]
      .filter(Boolean)
      .join(' '),
  );
  const sheetClasses = $derived(
    ['bb-surface', 'bb-surface--sheet', className || null].filter(Boolean).join(' '),
  );
</script>

<svelte:window
  onkeydown={(e) => {
    if (!open || e.key !== 'Escape') return;
    const canDismiss = isSheet ? isTopmost(overlayId) : !hasOpenOverlay();
    if (canDismiss) {
      e.preventDefault();
      requestClose();
    }
  }}
/>

{#snippet body(content: Snippet | undefined)}
  <div class="bb-surface__head">
    <span class="bb-surface__tag bb-tag bb-tag--bare">{title}</span>
    <button class="bb-surface__close" type="button" aria-label={closeLabel} onclick={requestClose}>
      <Icon name="x" size={14} />
    </button>
  </div>
  <div class="bb-surface__body" id={controls}>
    {#if content}{@render content()}{/if}
  </div>
{/snippet}

{#snippet idleBody()}
  <div class="bb-surface__idle">{#if idle}{@render idle()}{/if}</div>
{/snippet}

{#if open}
  {#if isSheet}
    <div class="bb-sheet-shell" data-overlay style="z-index: {zIndex}" use:portal>
      <button class="bb-sheet-shell__scrim" type="button" aria-label={closeLabel} onclick={requestClose}
      ></button>
      <div
        class={sheetClasses}
        role="dialog"
        aria-modal="true"
        tabindex="-1"
        aria-label={title}
        bind:this={sheetEl}
        use:trapFocus
        {...rest}
      >
        {@render body(children)}
      </div>
    </div>
  {:else}
    <aside class={dockedClasses} aria-label={title} {...rest}>
      {@render body(children)}
    </aside>
  {/if}
{:else if idle && !isSheet}
  <aside class={dockedClasses} aria-label={title} {...rest}>
    {@render body(idleBody)}
  </aside>
{/if}
