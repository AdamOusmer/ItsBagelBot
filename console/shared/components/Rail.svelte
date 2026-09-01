<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The desktop rail: flat sections, always open, every page one click away.
  // Its whole character is one moving part — a single highlight that glides
  // between rows — so nothing else in here animates on navigation. The one
  // nested group (Modules, which fans out into the sections the /modules page
  // is divided into) collapses, because listing seven sub-rows permanently
  // pushed Billing/Settings below the fold on a 720px-tall laptop.
  import { slide } from 'svelte/transition';
  import Brand from './Brand.svelte';
  import RailItem from './RailItem.svelte';
  import AccountFoot from './AccountFoot.svelte';
  import type { DashboardLink, NavGroupDef, NavLink } from '../lib/types';

  let {
    brandTitle = 'ItsBagelBot', brandSub, groups, accountName, accountRole,
    dashboards = [], isDelegate = false, delegateExitHref = '', delegateExitLabel = ''
  }: {
    brandTitle?: string;
    brandSub: string;
    groups: NavGroupDef[];
    accountName: string;
    accountRole: string;
    dashboards?: DashboardLink[];
    isDelegate?: boolean;
    delegateExitHref?: string;
    delegateExitLabel?: string;
  } = $props();

  // Groups the reader collapsed or expanded by hand, keyed by the parent href.
  // Unset means "follow the page": the group whose own page is open starts
  // expanded, so landing on /modules never hides the section you came for.
  let manual = $state<Record<string, boolean>>({});
  const isOpen = (it: NavLink) => manual[it.href] ?? !!it.active;
  const toggle = (it: NavLink) => { manual[it.href] = !isOpen(it); };

  // The glide is measured, not per-row CSS, so one block travels across group
  // boundaries. offsetTop is relative to .rail (position: relative).
  let railEl = $state<HTMLElement | null>(null);
  let top = $state(0);
  let height = $state(0);
  let shown = $state(false);
  $effect(() => {
    // Re-measure when the nav changes or a group opens/closes under it.
    void groups;
    void manual;
    const el = railEl?.querySelector<HTMLElement>('.rail-item.active');
    shown = !!el;
    if (el) {
      top = el.offsetTop;
      height = el.offsetHeight;
    }
  });
</script>

<aside class="rail" bind:this={railEl}>
  <Brand title={brandTitle} sub={brandSub} />

  <span class="glide" class:shown style="top: {top}px; height: {height}px" aria-hidden="true">
    <span class="glide-edge"></span>
  </span>

  {#each groups as g (g.label ?? '')}
    {#if g.label}<div class="rail-label">{g.label}</div>{/if}
    <div class="rail-group">
      {#each g.items as it (it.href)}
        {#if it.children && it.children.length}
          <div class="rail-row">
            <RailItem
              href={it.href}
              icon={it.icon}
              label={it.label}
              active={it.active}
              locked={it.locked}
              count={it.count}
            />
            <button
              class="chev"
              class:open={isOpen(it)}
              type="button"
              aria-expanded={isOpen(it)}
              aria-label={it.label}
              onclick={() => toggle(it)}
            >
              <svg viewBox="0 0 24 24" width="14" height="14"><polyline points="9 6 15 12 9 18"></polyline></svg>
            </button>
          </div>
          {#if isOpen(it)}
            <div class="rail-subs" transition:slide={{ duration: 200 }}>
              {#each it.children as c (c.href)}
                <a class="rail-sub" href={c.href}>
                  <span class="lbl">{c.label}</span>
                  {#if c.count !== undefined}<span class="count">{c.count}</span>{/if}
                </a>
              {/each}
            </div>
          {/if}
        {:else}
          <RailItem
            href={it.href}
            icon={it.icon}
            label={it.label}
            active={it.active}
            locked={it.locked}
            count={it.count}
          />
        {/if}
      {/each}
    </div>
  {/each}

  <div class="rail-spacer"></div>
  <AccountFoot
    name={accountName}
    role={accountRole}
    {dashboards}
    {isDelegate}
    {delegateExitHref}
    {delegateExitLabel}
  />
</aside>

<style>
  .rail {
    position: sticky; top: 0; align-self: start; height: 100vh;
    box-sizing: border-box; width: 240px; flex: none;
    display: none; flex-direction: column;
    padding: 20px 14px 14px;
    background: rgba(10, 10, 10, 0.55);
    border-right: 1px solid var(--rule, rgba(240, 236, 228, 0.1));
    overflow: auto;
  }
  @media (min-width: 761px) { .rail { display: flex; } }

  /* The one moving part. It sits behind the rows (z-index below .rail-item). */
  .glide {
    position: absolute; left: 14px; right: 14px; pointer-events: none;
    border-radius: 10px; background: rgba(240, 236, 228, 0.05); opacity: 0;
    transition: top var(--bb-dur-slow) var(--bb-ease-out-expo),
                height var(--bb-dur-slow) var(--bb-ease-out-expo),
                opacity var(--bb-dur-fast) ease;
  }
  .glide.shown { opacity: 1; }
  .glide-edge {
    position: absolute; left: 0; top: 10px; bottom: 10px; width: 2px; border-radius: 2px;
    background: var(--bb-green-glow); box-shadow: 0 0 10px rgba(82, 183, 136, 0.5);
  }

  .rail-label {
    font-family: var(--bb-font-mono); font-size: 9px; letter-spacing: 0.22em;
    text-transform: uppercase; color: var(--bb-muted);
    padding: 0 8px 8px;
  }
  .rail-group { display: flex; flex-direction: column; gap: 1px; margin-bottom: 20px; }
  .rail-row { display: flex; align-items: center; }
  .rail-row :global(.rail-item) { flex: 1; min-width: 0; }

  .chev {
    position: relative; z-index: 1;
    flex: none; width: 26px; height: 26px; margin-right: 4px;
    display: flex; align-items: center; justify-content: center;
    background: none; border: none; cursor: pointer; color: var(--bb-muted);
    transition: color var(--bb-dur-fast) ease;
  }
  .chev svg { fill: none; stroke: currentColor; stroke-width: 1.8; stroke-linecap: round; stroke-linejoin: round; transition: transform var(--bb-dur-base) var(--bb-ease-out-expo); }
  .chev:hover { color: var(--bb-white); }
  .chev.open svg { transform: rotate(90deg); }

  .rail-subs {
    display: flex; flex-direction: column; gap: 2px;
    margin: 4px 0 6px 22px; padding-left: 12px;
    border-left: 1px solid rgba(201, 168, 124, 0.25);
  }
  .rail-sub {
    position: relative; z-index: 1;
    display: flex; align-items: center; gap: 8px;
    padding: 8px 10px; border-radius: 9px; text-decoration: none;
    font-family: var(--bb-font-sans); font-weight: 500; font-size: 12.5px;
    color: var(--bb-muted);
    transition: color var(--bb-dur-fast) ease;
  }
  .rail-sub:hover { color: var(--bb-white); }
  .rail-sub .lbl { flex: 1; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .rail-sub .count { font-family: var(--bb-font-mono); font-size: 10.5px; color: rgba(136, 128, 119, 0.8); }

  .rail-spacer { flex: 1; min-height: 18px; }

  @media (prefers-reduced-motion: reduce) {
    .glide, .chev svg { transition: none; }
  }
</style>
