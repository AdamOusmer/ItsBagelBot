<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for the `.bb-surface` contract
  // (../styles/elements/surface.css). Its Astro twin is
  // ../astro/InspectorSurface.astro, which draws the DOCKED form only — a
  // static page has no viewport to react to and nothing to trap focus in.
  //
  // The master-detail inspector shell. On a wide screen it is an in-flow panel
  // docked next to the list; below 1080px it becomes a modal bottom sheet that
  // portals to <body>, traps focus, makes the page inert and dismisses on scrim
  // or Escape, reusing the same ../lib/overlay-stack.ts as Modal. One component
  // so every route's inspector behaves identically instead of each re-styling
  // an <aside>.
  //
  // The close glyph is ./Icon.svelte at `name="x"`, matching ./ToastHost.svelte
  // and ../astro/InspectorSurface.astro. It was an inline copy of that path
  // while the icon set still lived in the console.
  // Both stylesheets, in this order: the docked form composes `.bb-card`, and
  // surface.css must come second so its flex column beats the card's block.
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

  let {
    open = false,
    title,
    controls,
    closeLabel = 'Close',
    class: className = '',
    onClose,
    children,
    ...rest
  }: {
    open?: boolean;
    title: string;
    /** id of the region the row's aria-controls points at. */
    controls?: string;
    closeLabel?: string;
    class?: string;
    onClose: () => void;
    children?: Snippet;
    [key: string]: unknown;
  } = $props();

  // 1080px is where the docked panel stops fitting beside the widest deck (the
  // commands list) without its rows truncating — measured, not a round number.
  const SHEET_QUERY = '(max-width: 1079px)';
  const sheetQuery = mediaQuery(SHEET_QUERY);

  // Initialised synchronously so the first pass renders sheet-or-docked
  // directly. A false -> true swap on mount tears down and re-mounts the sheet,
  // racing its focus trap; the effect below still tracks live changes.
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

  // Register with the overlay stack only in sheet mode: a docked desktop panel
  // is in-flow and must not lock scroll or inert the page behind it.
  $effect(() => {
    if (open && isSheet) {
      const id = pushOverlay();
      overlayId = id;
      zIndex = 220 + overlayIndex(id) * 10;
      return () => removeOverlay(id);
    }
  });

  // Move focus into the sheet on open. In an effect (which runs after the
  // portal move) rather than via requestAnimationFrame, so initial focus lands
  // even where rAF is throttled; trapFocus still contains Tab thereafter.
  const FOCUSABLE =
    'button:not([disabled]), a[href], input:not([disabled]), textarea:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])';
  $effect(() => {
    if (open && isSheet && sheetEl) {
      const first = sheetEl.querySelector<HTMLElement>(FOCUSABLE);
      (first ?? sheetEl).focus();
    }
  });

  // Built here and mirrored in the Astro adapter. Joined rather than
  // interpolated: a template that pastes an empty `className` into the class
  // attribute emits a trailing space inside it, which the parity diff sees and
  // Astro never produces.
  const dockedClasses = $derived(
    ['bb-surface', 'bb-card', 'bb-surface--docked', className || null]
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
    // Sheet: only when frontmost on the overlay stack. Docked (non-modal): only
    // when no modal (e.g. a discard confirmation) is stacked on top of it.
    if (isSheet ? isTopmost(overlayId) : !hasOpenOverlay()) {
      e.preventDefault();
      onClose();
    }
  }}
/>

{#snippet body()}
  <div class="bb-surface__head">
    <span class="bb-surface__tag bb-tag bb-tag--bare">{title}</span>
    <button class="bb-surface__close" type="button" aria-label={closeLabel} onclick={onClose}>
      <Icon name="x" size={14} />
    </button>
  </div>
  <div class="bb-surface__body" id={controls}>
    {#if children}{@render children()}{/if}
  </div>
{/snippet}

{#if open}
  {#if isSheet}
    <div class="bb-sheet-shell" data-overlay style="z-index: {zIndex}" use:portal>
      <button class="bb-sheet-shell__scrim" type="button" aria-label={closeLabel} onclick={onClose}
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
        {@render body()}
      </div>
    </div>
  {:else}
    <aside class={dockedClasses} aria-label={title} {...rest}>
      {@render body()}
    </aside>
  {/if}
{/if}
