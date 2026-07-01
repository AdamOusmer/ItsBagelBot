<script lang="ts">
  // The command dock: navigation lives in a floating strip at the bottom of the
  // screen — the same pattern on desktop and mobile (phone-dock muscle memory),
  // which is what frees the layout from the classic sidebar dashboard skeleton.
  import Icon from './Icon.svelte';
  import type { NavLink } from '../lib/types';
  let { items, logout = true }: { items: NavLink[]; logout?: boolean } = $props();
</script>

<nav class="dock" aria-label="Main navigation">
  <div class="dock-inner">
    {#each items as it (it.href)}
      <a href={it.href} class="dock-item {it.active ? 'active' : ''}" aria-current={it.active ? 'page' : undefined}>
        <Icon name={it.icon} size={18} />
        <span class="lbl">{it.label}</span>
      </a>
    {/each}
    {#if logout}
      <span class="dock-rule" aria-hidden="true"></span>
      <form method="POST" action="/auth/logout" class="dock-form">
        <button type="submit" class="dock-item" title="Log out">
          <Icon name="power" size={18} />
          <span class="lbl">Exit</span>
        </button>
      </form>
    {/if}
  </div>
</nav>

<style>
  .dock {
    position: fixed;
    left: 0; right: 0; bottom: 0;
    z-index: 60;
    display: flex;
    justify-content: center;
    padding: 0 12px calc(14px + env(safe-area-inset-bottom));
    pointer-events: none;
  }
  .dock-inner {
    pointer-events: auto;
    display: flex;
    align-items: stretch;
    gap: 2px;
    padding: 6px;
    background: rgba(10, 10, 10, 0.88);
    border: 1px solid var(--rule, rgba(240, 236, 228, 0.12));
    border-top-color: var(--rule-strong, rgba(240, 236, 228, 0.22));
    border-radius: var(--bb-radius-lg, 3px);
    box-shadow: 0 18px 50px rgba(0, 0, 0, 0.55);
    animation: dock-in 520ms var(--bb-ease-out-expo) 120ms both;
    max-width: calc(100vw - 24px);
    overflow-x: auto;
    scrollbar-width: none;
  }
  .dock-inner::-webkit-scrollbar { display: none; }
  @keyframes dock-in {
    from { opacity: 0; transform: translateY(14px); }
    to { opacity: 1; transform: translateY(0); }
  }

  .dock-item {
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 4px;
    min-width: 64px;
    padding: 8px 12px 7px;
    border-radius: var(--bb-radius-sm, 2px);
    color: var(--bb-muted);
    text-decoration: none;
    cursor: pointer;
    font-family: var(--bb-font-mono);
    font-size: 9px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    transition: color var(--bb-dur-fast, 180ms) ease, background var(--bb-dur-fast, 180ms) ease, transform 200ms var(--bb-ease-out-back, ease);
    white-space: nowrap;
  }
  .dock-item :global(svg) { width: 18px; height: 18px; stroke: currentColor; fill: none; stroke-width: 1.6; stroke-linecap: round; stroke-linejoin: round; }
  .dock-item:hover { color: var(--bb-white); transform: translateY(-2px); }
  .dock-item:active { transform: translateY(0); }

  .dock-item.active { color: var(--bb-tan-light); background: rgba(201, 168, 124, 0.07); }
  .dock-item.active::after {
    content: "";
    position: absolute;
    bottom: 2px;
    width: 14px;
    height: 1px;
    background: var(--bb-tan);
  }
  .dock-item.active::before {
    content: "";
    position: absolute;
    top: 4px;
    right: 6px;
    width: 4px;
    height: 4px;
    background: var(--bb-green-glow);
    box-shadow: 0 0 6px var(--bb-green-glow);
  }

  .dock-rule { width: 1px; background: var(--rule, rgba(240, 236, 228, 0.12)); margin: 6px 4px; }
  .dock-form { display: flex; }

  @media (max-width: 760px) {
    .dock { padding: 0 8px calc(8px + env(safe-area-inset-bottom)); }
    .dock-inner { width: 100%; justify-content: space-around; }
    .dock-item { min-width: 0; flex: 1; padding: 8px 6px 7px; }
  }
  @media (max-width: 400px) {
    .dock-item .lbl { display: none; }
  }
  @media (prefers-reduced-motion: reduce) {
    .dock-inner { animation: none; }
    .dock-item:hover { transform: none; }
  }
</style>
