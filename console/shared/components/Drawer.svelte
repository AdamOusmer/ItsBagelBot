<script lang="ts">
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';
  import { pushOverlay, removeOverlay, isTopmost, overlayIndex, portal, trapFocus } from '../lib/overlay-stack';

  let {
    open = false,
    title,
    subtitle,
    busy = false,
    closeDrawer,
    children
  }: {
    open: boolean;
    title?: string;
    subtitle?: string;
    busy?: boolean;
    closeDrawer: () => void;
    children: Snippet;
  } = $props();

  const uid = $props.id();
  const titleId = `drawer-title-${uid}`;
  let overlayId = 0;
  let zIndex = $state(190);

  $effect(() => {
    if (!open) return;
    const id = pushOverlay();
    overlayId = id;
    zIndex = 190 + overlayIndex(id) * 10;
    return () => removeOverlay(id);
  });

  function tryClose() {
    if (!busy) closeDrawer();
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
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
  <div class="drawer-shell" data-overlay style="z-index: {zIndex}" use:portal>
    <button class="drawer-backdrop" type="button" aria-label="Close" onclick={tryClose}></button>
    <div class="drawer open" role="dialog" aria-modal="true" tabindex="-1" aria-labelledby={title ? titleId : undefined} use:trapFocus>
      <header class="drawer-head">
        <div class="drawer-id">
          {#if title}
            <h2 id={titleId}>{title}</h2>
          {/if}
          {#if subtitle}
            <span class="drawer-sub">{subtitle}</span>
          {/if}
        </div>
        <button class="drawer-close" type="button" onclick={tryClose} aria-label="Close">
          <Icon name="x" size={16} />
        </button>
      </header>

      <div class="drawer-body" data-lenis-prevent>
        {@render children()}
      </div>
    </div>
  </div>
{/if}

<style>
  .drawer-shell { position: fixed; inset: 0; }
  .drawer-backdrop {
    position: absolute; inset: 0;
    padding: 0; border: 0; cursor: pointer;
    background: rgba(0, 0, 0, 0.5);
    backdrop-filter: blur(2px); -webkit-backdrop-filter: blur(2px);
    animation: fade var(--bb-dur-fast, 160ms) var(--bb-ease-out-expo, ease) both;
  }
  @keyframes fade { from { opacity: 0; } to { opacity: 1; } }

  .drawer {
    position: absolute; top: 0; right: 0;
    height: 100dvh; width: min(620px, 92vw);
    display: flex; flex-direction: column;
    background:
      linear-gradient(var(--glass-fill), var(--glass-fill)),
      var(--bb-bg-1, #111);
    border-left: 1px solid var(--glass-border);
    backdrop-filter: blur(var(--glass-blur)); -webkit-backdrop-filter: blur(var(--glass-blur));
    box-shadow: -16px 0 48px rgba(0, 0, 0, 0.45);
    transform: translateX(100%);
    animation: slide-in var(--bb-dur-med, 320ms) var(--bb-ease-out-expo, cubic-bezier(.16,1,.3,1)) forwards;
  }
  @keyframes slide-in { to { transform: translateX(0); } }

  .drawer :global(:focus-visible),
  .drawer-close:focus-visible {
    outline: 2px solid var(--bb-green-glow, #52b788);
    outline-offset: 2px;
  }

  @media (prefers-reduced-motion: reduce) {
    .drawer-backdrop { animation: none; }
    .drawer { animation: none; transform: none; }
  }

  .drawer-head {
    display: flex; align-items: flex-start; justify-content: space-between;
    gap: 1rem; padding: 22px 22px 16px;
    border-bottom: 1px solid var(--glass-border);
  }
  .drawer-id { display: flex; flex-direction: column; gap: 4px; }
  .drawer-id h2 {
    font-family: var(--bb-font-display); font-weight: 700; font-size: 22px;
    color: var(--bb-white); margin: 0; letter-spacing: -0.01em;
  }
  .drawer-sub { font-family: var(--bb-font-mono); font-size: 12px; color: var(--bb-muted); }
  .drawer-close {
    background: none; border: 1px solid transparent; color: var(--bb-muted);
    cursor: pointer; padding: 6px; border-radius: 8px;
    display: flex; align-items: center; justify-content: center;
    transition: all var(--bb-dur-fast, 160ms) ease;
  }
  .drawer-close:hover { color: var(--bb-white); border-color: var(--bb-border-strong); background: rgba(255,255,255,0.04); }

  .drawer-body { flex: 1; overflow-y: auto; padding: 20px 22px 32px; }
</style>
