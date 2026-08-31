<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // The stage layout: a thin call-sign strip on top, one centered reading
  // column for the page, and the floating command dock at the bottom.
  //
  // `rail` opts a board into the desktop sidebar: the dock alone could never
  // show a page's sub-pages, so the dashboard nests the /modules sections under
  // Modules there. It is a prop, not the default, because the admin board's
  // several groups are what the grouped dock was built for -- flipping it on
  // everywhere would redesign a surface nobody asked about.
  import type { Snippet } from 'svelte';
  import Topbar from './Topbar.svelte';
  import Dock from './Dock.svelte';
  import Rail from './Rail.svelte';
  import { getI18n } from '../lib/i18n/context';
  import type { NavGroupDef, NavLink, DashboardLink } from '../lib/types';

  const { t } = getI18n();
  let {
    brandTitle = 'ItsBagelBot', brandSub, crumbRoot, crumb,
    accountName, accountRole, dashboards = [], groups, mobileItems, rail = false,
    offset = false, logoSrc = '/logo.png', isPremium = false, banner, topActions, children,
    isDelegate = false, delegateExitHref = '', delegateExitLabel = ''
  }: {
    brandTitle?: string; brandSub: string; crumbRoot: string; crumb: string;
    accountName: string; accountRole: string; dashboards?: DashboardLink[];
    groups: NavGroupDef[]; mobileItems: NavLink[]; rail?: boolean;
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

<div class="app" class:offset class:railed={rail}>
  {#if rail}
    <Rail {brandTitle} {brandSub} {groups} {accountName} {accountRole} />
  {/if}
  <div class="stage">
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
  <main class="main" id="main-content" tabindex="-1" bind:this={mainEl}>
    <div class="canvas">{@render children()}</div>
  </main>
  </div>
  <div class="dock-slot">
    <Dock items={dockItems} {groups} />
  </div>
</div>

<style>
  .app { position: relative; z-index: 1; min-height: 100vh; display: flex; flex-direction: column; }
  .stage { display: flex; flex-direction: column; flex: 1; min-width: 0; }
  /* display: contents keeps the fixed dock positioned against the viewport as
     before; the wrapper exists only so the rail can take the dock's place at
     desktop widths without Dock growing a breakpoint of its own. */
  .dock-slot { display: contents; }
  @media (min-width: 761px) {
    .app.railed { flex-direction: row; }
    .app.railed .dock-slot { display: none; }
    /* No dock to clear: give the column back the bottom it was reserving. */
    .app.railed .canvas { padding-bottom: calc(var(--gutter) + 24px); }
  }
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

  /* impersonation/delegate offset for the fixed banner; the pill topbar keeps
     its own 10px float below the banner. */
  .app.offset { box-sizing: border-box; padding-top: 44px; min-height: 100vh; }
  .app.offset :global(.topbar) { top: calc(54px + env(safe-area-inset-top, 0px)); }
</style>
