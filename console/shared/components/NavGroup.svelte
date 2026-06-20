<script lang="ts">
  import NavItem from './NavItem.svelte';
  import type { NavLink } from '../lib/types';
  let { label, items }: { label?: string; items: NavLink[] } = $props();
</script>

{#if label}<div class="nav-group-label">{label}</div>{/if}
<nav class="nav">
  {#each items as it}
    <NavItem href={it.href} icon={it.icon} label={it.label} active={it.active} locked={it.locked} count={it.count} />
  {/each}
</nav>

<style>
  .nav-group-label { font-family: var(--bb-font-mono); font-size: 9.5px; letter-spacing: 0.18em; text-transform: uppercase; color: var(--bb-muted); padding: 18px 12px 8px; }
  .nav { display: flex; flex-direction: column; gap: 3px; }
</style>
