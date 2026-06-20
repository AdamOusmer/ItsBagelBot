<script lang="ts">
  import Icon from './Icon.svelte';
  import type { NavLink } from '../lib/types';
  let { items, logout = true }: { items: NavLink[]; logout?: boolean } = $props();
</script>

<nav class="mobile-nav" aria-label="Main navigation">
  {#each items as it}
    <a href={it.href} class={it.active ? 'active' : ''}>
      <Icon name={it.icon} size={20} /><span>{it.label}</span>
    </a>
  {/each}
  {#if logout}
    <form method="POST" action="/auth/logout"><button type="submit"><Icon name="power" size={20} /><span>Log out</span></button></form>
  {/if}
</nav>

<style>
  .mobile-nav { display: none; }
  @media (max-width: 760px) {
    .mobile-nav {
      display: flex;
      position: fixed; left: 0; right: 0; bottom: 0; z-index: 50;
      padding: 8px max(12px, env(safe-area-inset-left)) calc(8px + env(safe-area-inset-bottom));
      gap: 4px;
      background: rgba(10, 10, 10, 0.7);
      border-top: 1px solid var(--glass-border);
      backdrop-filter: blur(var(--glass-blur)) saturate(150%);
      -webkit-backdrop-filter: blur(var(--glass-blur)) saturate(150%);
    }
    .mobile-nav form { flex: 1; display: flex; }
    .mobile-nav a, .mobile-nav button {
      flex: 1; display: flex; flex-direction: column; align-items: center; gap: 4px;
      padding: 6px 4px; border: 0; background: none; cursor: pointer;
      color: var(--bb-muted); text-decoration: none;
      font-family: var(--bb-font-mono); font-size: 9px; letter-spacing: 0.1em; text-transform: uppercase;
    }
    .mobile-nav a :global(svg), .mobile-nav button :global(svg) { width: 20px; height: 20px; stroke: currentColor; fill: none; stroke-width: 1.7; stroke-linecap: round; stroke-linejoin: round; }
    .mobile-nav a.active { color: var(--bb-green-glow); }
  }
  @media (max-width: 400px) { .mobile-nav a span { display: none; } }
</style>
