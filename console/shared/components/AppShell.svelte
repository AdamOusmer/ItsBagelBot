<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The stage layout: a thin call-sign strip on top, one centered reading
  // column, and exactly one nav system per width — the Sidebar rail at
  // ≥1024px, the floating command dock below it.
  import type { Snippet } from 'svelte';
  import Topbar from './Topbar.svelte';
  import Sidebar from './Sidebar.svelte';
  import Dock from './Dock.svelte';
  import { getI18n } from '../lib/i18n/context';
  import type { NavGroupDef, NavLink, DashboardLink } from '../lib/types';

  const { t } = getI18n();
  let {
    brandTitle = 'ItsBagelBot', brandSub, crumbRoot, crumb,
    accountName, accountRole, dashboards = [], groups, mobileItems,
    offset = false, logoSrc = '/logo.png', isPremium = false, banner, topActions, children,
    isDelegate = false, delegateExitHref = '', delegateExitLabel = ''
  }: {
    brandTitle?: string; brandSub: string; crumbRoot: string; crumb: string;
    accountName: string; accountRole: string; dashboards?: DashboardLink[];
    groups: NavGroupDef[]; mobileItems: NavLink[];
    offset?: boolean; logoSrc?: string; isPremium?: boolean; banner?: Snippet; topActions?: Snippet; children: Snippet;
    isDelegate?: boolean; delegateExitHref?: string; delegateExitLabel?: string;
  } = $props();

  // Flat apps (one group) get their curated mobileItems in the dock; apps with
  // several groups (admin) get the grouped dock, which collapses each group
  // into one button + popover so the bar never bloats.
  const dockItems = $derived(
    mobileItems.length ? mobileItems : groups.flatMap((g) => g.items)
  );

  // The reading column. The skip link and the Dock both point keyboard users
  // here; tabindex=-1 makes it a programmatic focus target without adding it to
  // the normal tab order.
  let mainEl = $state<HTMLElement | null>(null);
  function skipToMain(e: MouseEvent) {
    // Move focus explicitly (not just scroll) so the next Tab continues from the
    // content, regardless of how the client router treats the hash.
    e.preventDefault();
    mainEl?.focus();
    mainEl?.scrollIntoView();
  }
</script>

<!-- First focusable element in the whole shell: jump straight past the chrome. -->
<a class="skip-link" href="#main-content" onclick={skipToMain}>{t('common.skipToContent')}</a>

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
    {dashboards}
    {logoSrc}
    {isPremium}
    {isDelegate}
    {delegateExitHref}
    {delegateExitLabel}
  />
  <Sidebar {groups} />
  <main class="main" id="main-content" tabindex="-1" bind:this={mainEl}>
    <div class="canvas">{@render children()}</div>
  </main>
  <Dock items={dockItems} {groups} />
</div>

<style>
  .app { position: relative; z-index: 1; min-height: 100vh; display: flex; flex-direction: column; }
  .main { display: flex; flex-direction: column; min-width: 0; flex: 1; }
  /* main is a landmark skip-target, not a control: focus lands here from the
     skip link / dock so the next Tab starts in the content, but a full-width
     ring around the whole page reads as a bug. The link/dock that sent focus
     here already showed their own ring. */
  .main:focus { outline: none; }

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

  /* ≥1024px becomes a two-column stage: Sidebar owns column 1, the Topbar
     rules across both, and main centers its canvas in what remains. Grid, not
     a margin-left hack, because the rail must start BELOW the sticky topbar
     (not slide under it) and the canvas should center in the leftover space
     with no width arithmetic. DOM order (Topbar, Sidebar, main) drives
     auto-placement; .main pins column 2 so a future element can't silently
     reshuffle the stage. Below 1024 none of this applies and the dock shows. */
  @media (min-width: 1024px) {
    .app {
      display: grid;
      grid-template-columns: var(--sidebar-w, 248px) minmax(0, 1fr);
      grid-template-rows: auto 1fr;
      /* The rail's sticky pin line, consumed by Sidebar's top/height. 55px is
         measured, not derived: 9px padding + the 36px operator chip + the 1px
         rule at desktop gutters. The topbar has no height token of its own —
         if its padding or the chip grows, re-measure and move this. */
      --topbar-h: 55px;
    }
    /* The delegate/impersonation banner stacks 44px above the topbar, so the
       pin line drops by exactly that. */
    .app.offset { --topbar-h: 99px; }
    .app :global(.topbar) { grid-column: 1 / -1; }
    .main { grid-column: 2; }
    /* The dock's 110px bottom reserve is dead weight once the dock is hidden. */
    .canvas { padding-bottom: calc(var(--gutter) + 6px); }
  }
</style>
