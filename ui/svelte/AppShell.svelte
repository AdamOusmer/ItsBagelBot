<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/shell.css';
  import SkipLink from './SkipLink.svelte';
  import Topbar from './Topbar.svelte';
  import Dock from './Dock.svelte';
  import Rail from './Rail.svelte';
  import type { Snippet } from 'svelte';
  import type { UiBrand, UiCrumb, UiNavGroup, UiNavLink } from '../lib/nav-types';

  type Own = {
    brand: UiBrand;
    crumbs?: UiCrumb[];
    groups?: UiNavGroup[];
    dockItems?: UiNavLink[];
    rail?: boolean;
    offset?: boolean;
    stacked?: boolean;
    skipLabel?: string;
    crumbLabel?: string;
    dockLabel?: string;
    railLabel?: string;
    clock?: boolean;
    banner?: Snippet;
    topActions?: Snippet;
    account?: Snippet;
    railFooter?: Snippet;
    children: Snippet;
    class?: string;
  };

  let {
    brand,
    crumbs = [],
    groups = [],
    dockItems = [],
    rail = false,
    offset = false,
    stacked = false,
    skipLabel,
    crumbLabel,
    dockLabel,
    railLabel,
    clock = true,
    banner,
    topActions,
    account,
    railFooter,
    children,
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['div'], keyof Own> = $props();

  const classes = $derived(
    [
      'bb-shell',
      offset ? 'bb-shell--offset' : null,
      stacked ? 'bb-shell--stacked' : null,
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

<SkipLink href="#main-content" label={skipLabel} class="bb-shell__skip" onclick={skipToMain} />

{#if banner}{@render banner()}{/if}

<div class={classes} {...rest}>
  {#if rail}
    <Rail {brand} {groups} footer={railFooter} label={railLabel} />
  {/if}
  <div class="bb-shell__stage">
    <Topbar
      {brand}
      {crumbs}
      {crumbLabel}
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
    <Dock {items} {groups} label={dockLabel} />
  </div>
</div>
