<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // The stage layout: a thin call-sign strip on top, one centred reading
  // column, and the floating dock at the bottom. Svelte only -- a static
  // surface draws the pieces it wants rather than the whole shell.
  //
  // `rail` opts a board into the desktop sidebar. It is a prop and not the
  // default because the dock alone cannot show a page's sub-pages, and the
  // grouped dock is what a board with several groups was built for: turning
  // the rail on everywhere would redesign a surface nobody asked about.
  //
  // Every string it needs arrives as a prop, including the skip link's. That
  // one used to be a `t('common.skipToContent')` call inside the component,
  // which is the single line that kept this layout from being shareable.
  import '../styles/elements/shell.css';
  import Topbar from './Topbar.svelte';
  import Dock from './Dock.svelte';
  import Rail from './Rail.svelte';
  import type { Snippet } from 'svelte';
  import type { UiBrand, UiCrumb, UiNavGroup, UiNavLink } from '../lib/nav-types';

  let {
    brand,
    crumbs = [],
    groups = [],
    dockItems = [],
    rail = false,
    offset = false,
    skipLabel,
    crumbAriaLabel,
    dockAriaLabel,
    railAriaLabel,
    clock = true,
    banner,
    topActions,
    account,
    railFoot,
    children,
    class: className = '',
    ...rest
  }: {
    brand: UiBrand;
    crumbs?: UiCrumb[];
    groups?: UiNavGroup[];
    /** The dock's flat items; empty falls back to every group's items. */
    dockItems?: UiNavLink[];
    rail?: boolean;
    /** Reserve room above for a fixed banner. */
    offset?: boolean;
    skipLabel: string;
    crumbAriaLabel?: string;
    dockAriaLabel?: string;
    railAriaLabel?: string;
    clock?: boolean;
    banner?: Snippet;
    topActions?: Snippet;
    account?: Snippet;
    railFoot?: Snippet;
    children: Snippet;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-shell',
      offset ? 'bb-shell--offset' : null,
      rail ? 'bb-shell--railed' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );

  const items = $derived(
    dockItems.length ? dockItems : groups.flatMap((group) => group.items),
  );

  // The reading column. The skip link points here; tabindex=-1 makes it a
  // programmatic focus target without adding it to the tab order.
  let mainEl = $state<HTMLElement | null>(null);
  function skipToMain(event: MouseEvent) {
    // Move focus explicitly, not just scroll, so the next Tab continues from
    // the content regardless of how a client router treats the hash.
    event.preventDefault();
    mainEl?.focus();
    mainEl?.scrollIntoView();
  }
</script>

<!-- The first focusable element in the whole shell: jump past the chrome. -->
<a class="bb-shell__skip" href="#main-content" onclick={skipToMain}>{skipLabel}</a>

{#if banner}{@render banner()}{/if}

<div class={classes} {...rest}>
  {#if rail}
    <Rail {brand} {groups} foot={railFoot} ariaLabel={railAriaLabel} />
  {/if}
  <div class="bb-shell__stage">
    <Topbar
      {brand}
      {crumbs}
      {crumbAriaLabel}
      {clock}
      railed={rail}
      actions={topActions}
      {account}
    />
    <main class="bb-shell__main" id="main-content" tabindex="-1" bind:this={mainEl}>
      <div class="bb-shell__canvas">{@render children()}</div>
    </main>
  </div>
  <div class="bb-shell__dock-slot">
    <Dock {items} {groups} ariaLabel={dockAriaLabel} />
  </div>
</div>
