<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
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
  /* The rule BETWEEN rows belongs to the group, not to an entry: an entry that
     drew its own would double it against the group's top border. This is the
     one place @bagel/ui's `--bb-nav-link-rule` is deliberately left alone and
     the border set directly -- the contract's hairline is transparent by
     default precisely so a container like this one can own it.

     The row padding is the rail's, not the contract's: 11px/12px is the density
     the console's 240px rail was laid out against, and the contract's 6px/2px
     is a bar's, where links sit side by side. */
  .nav :global(.nav-item) {
    --bb-nav-link-pad: 11px 10px 11px 12px;
    --bb-nav-link-size: 12px;
    --bb-nav-link-tracking: 0.08em;
    border-bottom: 1px solid var(--rule, rgba(240,236,228,0.06));
  }
</style>
