<script lang="ts">
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';

  let {
    open = false,
    title,
    subtitle,
    closeDrawer,
    children
  }: {
    open: boolean;
    title?: string;
    subtitle?: string;
    closeDrawer: () => void;
    children: Snippet;
  } = $props();
</script>

{#if open}
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
  <div class="drawer-backdrop" role="button" tabindex="-1" aria-label="Close drawer" onclick={closeDrawer}></div>
  <div class="drawer open" role="dialog" aria-modal="true" aria-labelledby="drawer-title">
    <header class="drawer-head">
      <div class="drawer-id">
        {#if title}
          <h2 id="drawer-title">{title}</h2>
        {/if}
        {#if subtitle}
          <span class="drawer-sub">{subtitle}</span>
        {/if}
      </div>
      <button class="drawer-close" type="button" onclick={closeDrawer} aria-label="Close">
        <Icon name="x" size={16} />
      </button>
    </header>

    <div class="drawer-body" data-lenis-prevent>
      {@render children()}
    </div>
  </div>
{/if}

<style>
  .drawer-backdrop {
    position: fixed; inset: 0; z-index: 190;
    padding: 0; border: 0; cursor: pointer;
    background: rgba(0, 0, 0, 0.5);
    backdrop-filter: blur(2px); -webkit-backdrop-filter: blur(2px);
    animation: fade var(--bb-dur-fast, 160ms) var(--bb-ease-out-expo, ease) both;
  }
  @keyframes fade { from { opacity: 0; } to { opacity: 1; } }

  .drawer {
    position: fixed; top: 0; right: 0; z-index: 191;
    height: 100vh; width: min(620px, 92vw);
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
    cursor: pointer; padding: 6px; border-radius: var(--bb-radius-sm);
    display: flex; align-items: center; justify-content: center;
    transition: all var(--bb-dur-fast, 160ms) ease;
  }
  .drawer-close:hover { color: var(--bb-white); border-color: var(--bb-border-strong); background: rgba(255,255,255,0.04); }

  .drawer-body { flex: 1; overflow-y: auto; padding: 20px 22px 32px; }
</style>
