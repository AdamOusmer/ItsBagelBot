<script lang="ts">
  // The master-detail inspector shell. On wide screens it is an in-flow docked
  // panel next to the list; on narrow screens it becomes a modal bottom sheet
  // that portals to <body>, traps focus, makes the page inert, and dismisses on
  // scrim/Escape — reusing the shared overlay foundation. One component so every
  // route's inspector behaves identically instead of each re-styling an <aside>.
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';
  import { pushOverlay, removeOverlay, isTopmost, overlayIndex, hasOpenOverlay, portal, trapFocus } from '../lib/overlay-stack';

  let {
    open = false,
    title,
    controls,
    closeLabel = 'Close',
    onClose,
    children
  }: {
    open?: boolean;
    title: string;
    // id of the region the row's aria-controls points at.
    controls?: string;
    closeLabel?: string;
    onClose: () => void;
    children: Snippet;
  } = $props();

  const SHEET_QUERY = '(max-width: 1079px)';
  // Initialise synchronously so we render as sheet-or-docked in one pass; a
  // false->true swap on mount would tear down and re-mount the sheet, racing the
  // focus trap. The effect below still tracks live viewport changes.
  let isSheet = $state(typeof window !== 'undefined' && window.matchMedia(SHEET_QUERY).matches);
  let overlayId = 0;
  let zIndex = $state(220);

  $effect(() => {
    const mq = window.matchMedia(SHEET_QUERY);
    const update = () => (isSheet = mq.matches);
    update();
    mq.addEventListener('change', update);
    return () => mq.removeEventListener('change', update);
  });

  let sheetEl = $state<HTMLDivElement>();

  // Register with the overlay stack only in sheet mode (a docked desktop panel is
  // in-flow and must not lock scroll or inert the page).
  $effect(() => {
    if (open && isSheet) {
      const id = pushOverlay();
      overlayId = id;
      zIndex = 220 + overlayIndex(id) * 10;
      return () => removeOverlay(id);
    }
  });

  // Move focus into the sheet on open. Done in an effect (which runs after the
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
  <div class="surface-head">
    <span class="surface-tag">{title}</span>
    <button class="surface-close" type="button" aria-label={closeLabel} onclick={onClose}>
      <Icon name="x" size={14} />
    </button>
  </div>
  <div class="surface-body" id={controls}>
    {@render children()}
  </div>
{/snippet}

{#if open}
  {#if isSheet}
    <div class="sheet-shell" data-overlay style="z-index: {zIndex}" use:portal>
      <button class="sheet-scrim" type="button" aria-label={closeLabel} onclick={onClose}></button>
      <div class="surface sheet" role="dialog" aria-modal="true" tabindex="-1" aria-label={title} bind:this={sheetEl} use:trapFocus>
        {@render body()}
      </div>
    </div>
  {:else}
    <aside class="surface docked" aria-label={title}>
      {@render body()}
    </aside>
  {/if}
{/if}

<style>
  .surface {
    display: flex;
    flex-direction: column;
    min-height: 0;
    border: 1px solid var(--rule);
    border-top-color: var(--rule-strong);
    border-radius: 8px;
    background: linear-gradient(180deg, rgba(240, 236, 228, 0.03), rgba(240, 236, 228, 0.012));
    overflow: hidden;
  }
  .surface.docked {
    position: sticky;
    top: 62px;
    max-height: calc(100dvh - 62px - 108px);
  }

  .surface-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 12px 16px;
    border-bottom: 1px solid var(--rule);
    flex: none;
  }
  .surface-tag {
    font-family: var(--bb-font-display);
    font-weight: 700;
    font-size: 12px;
    letter-spacing: 0.02em;
    color: var(--bb-tan);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .surface-close {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    border: 1px solid transparent;
    border-radius: 8px;
    background: none;
    color: var(--bb-muted);
    cursor: pointer;
  }
  .surface-close:hover { color: var(--bb-white); border-color: var(--bb-border-strong); background: rgba(255, 255, 255, 0.04); }
  .surface-close:focus-visible { outline: 2px solid var(--bb-green-glow, #52b788); outline-offset: 2px; }

  .surface-body { display: flex; flex-direction: column; min-height: 0; flex: 1; }

  /* Mobile bottom sheet */
  .sheet-shell { position: fixed; inset: 0; }
  .sheet-scrim {
    position: absolute; inset: 0;
    border: 0; padding: 0;
    background: rgba(0, 0, 0, 0.55);
  }
  .surface.sheet {
    position: absolute;
    left: 0; right: 0; bottom: 0;
    max-height: 88dvh;
    border-radius: 8px 8px 0 0;
    background: var(--bb-bg-1, #111);
    padding-bottom: env(safe-area-inset-bottom, 0);
    animation: sheet-in var(--bb-dur-base, 320ms) var(--bb-ease-out-expo, cubic-bezier(.16, 1, .3, 1)) both;
  }
  @keyframes sheet-in { from { transform: translateY(100%); } to { transform: translateY(0); } }
  @media (prefers-reduced-motion: reduce) {
    .surface.sheet { animation: none; }
  }
</style>
