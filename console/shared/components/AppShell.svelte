<script lang="ts">
  // The stage layout: no sidebar. A thin call-sign strip on top, one centered
  // reading column for the page, and the floating command dock at the bottom —
  // the same navigation pattern at every breakpoint.
  import type { Snippet } from 'svelte';
  import Topbar from './Topbar.svelte';
  import Dock from './Dock.svelte';
  import type { NavGroupDef, NavLink } from '../lib/types';
  let {
    brandTitle = 'ItsBagelBot', brandSub, crumbRoot, crumb,
    accountName, accountRole, groups, mobileItems,
    offset = false, banner, topActions, children
  }: {
    brandTitle?: string; brandSub: string; crumbRoot: string; crumb: string;
    accountName: string; accountRole: string;
    groups: NavGroupDef[]; mobileItems: NavLink[];
    offset?: boolean; banner?: Snippet; topActions?: Snippet; children: Snippet;
  } = $props();

  // Flat apps (one group) get their curated mobileItems in the dock; apps with
  // several groups (admin) get the grouped dock, which collapses each group
  // into one button + popover so the bar never bloats.
  const dockItems = $derived(
    mobileItems.length ? mobileItems : groups.flatMap((g) => g.items)
  );
</script>

{#if banner}{@render banner()}{/if}

<div class="app" class:offset>
  <Topbar
    root={crumbRoot}
    {crumb}
    actions={topActions}
    {brandTitle}
    {brandSub}
    {accountName}
    {accountRole}
  />
  <main class="main">
    <div class="canvas">{@render children()}</div>
  </main>
  <Dock items={dockItems} {groups} />
</div>

<style>
  .app { position: relative; z-index: 1; min-height: 100vh; display: flex; flex-direction: column; }
  .main { display: flex; flex-direction: column; min-width: 0; flex: 1; }

  /* One centered reading column; the dock floats over the bottom padding. */
  .canvas {
    padding: calc(var(--gutter) + 6px) var(--gutter) calc(110px + env(safe-area-inset-bottom));
    max-width: 1160px;
    width: 100%;
    margin: 0 auto;
  }

  /* impersonation/delegate offset for the fixed banner */
  .app.offset { box-sizing: border-box; padding-top: 44px; min-height: 100vh; }
  .app.offset :global(.topbar) { top: 44px; }
</style>
