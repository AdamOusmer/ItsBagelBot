<script lang="ts">
  import NavItem from './NavItem.svelte';
  import type { NavLink } from '../lib/types';
  let { label, items, startIndex = 1 }: { label?: string; items: NavLink[]; startIndex?: number } = $props();
</script>

{#if label}<div class="nav-group-label">{label}</div>{/if}
<nav class="nav">
  {#each items as it, i}
    <NavItem
      href={it.href}
      icon={it.icon}
      label={it.label}
      active={it.active}
      locked={it.locked}
      count={it.count}
      index={startIndex + i}
    />
  {/each}
</nav>

<style>
  .nav-group-label {
    font-family: var(--bb-font-mono); font-size: 9px; letter-spacing: 0.22em; text-transform: uppercase;
    color: var(--bb-tan); padding: 20px 12px 8px; display: flex; align-items: center; gap: 8px;
  }
  .nav-group-label::after { content: ""; flex: 1; height: 1px; background: var(--rule, rgba(240,236,228,0.1)); }
  .nav { display: flex; flex-direction: column; border-top: 1px solid var(--rule, rgba(240,236,228,0.1)); }
  .nav :global(.nav-item) { border-bottom: 1px solid var(--rule, rgba(240,236,228,0.06)); }
</style>
