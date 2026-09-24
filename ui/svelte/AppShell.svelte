<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

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
    dockItems?: UiNavLink[];
    rail?: boolean;
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

  let mainEl = $state<HTMLElement | null>(null);
  function skipToMain(event: MouseEvent) {
    event.preventDefault();
    mainEl?.focus();
    mainEl?.scrollIntoView();
  }
</script>

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
