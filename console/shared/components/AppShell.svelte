<script lang="ts">
  import type { Snippet } from 'svelte';
  import Sidebar from './Sidebar.svelte';
  import Topbar from './Topbar.svelte';
  import MobileNav from './MobileNav.svelte';
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
</script>

{#if banner}{@render banner()}{/if}

<div class="app" class:offset>
  <Sidebar {brandTitle} {brandSub} {groups} {accountName} {accountRole} />
  <main class="main">
    <Topbar root={crumbRoot} {crumb} actions={topActions} />
    <div class="canvas">{@render children()}</div>
  </main>
  <MobileNav items={mobileItems} />
</div>

<style>
  .app {
    position: relative; z-index: 1;
    display: grid; grid-template-columns: 1fr; min-height: 100vh;
  }
  @media (min-width: 761px) { .app { grid-template-columns: var(--sidebar-w) 1fr; } }
  .main { display: flex; flex-direction: column; min-width: 0; }
  .canvas { padding: var(--gutter); max-width: 1180px; width: 100%; padding-bottom: calc(72px + var(--gutter)); }
  @media (min-width: 761px) { .canvas { padding-bottom: var(--gutter); } }

  /* impersonation/delegate offset for the fixed banner */
  .app.offset { box-sizing: border-box; padding-top: 44px; min-height: 100vh; }
  .app.offset :global(.sidebar) { height: calc(100vh - 44px); top: 44px; }
</style>
