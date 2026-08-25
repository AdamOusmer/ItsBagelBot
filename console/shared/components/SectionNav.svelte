<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  // In-page section navigation. Real <a href="#id"> anchors (NOT ARIA tabs):
  // each one scrolls to and focuses a <section tabindex=-1> whose
  // scroll-margin-top lands it below the sticky topbar. Plain links keep
  // native semantics (open in new tab, right-click, "link" role).
  //
  // Layout is a container query, not a viewport hide. The modules index used
  // to `display:none` this rail below 980px and drive `is-current` with a rAF
  // scrollspy — a phone could not jump, and the highlight lied after search
  // dropped sections. Same markup everywhere: wrap as chips when the host is
  // wide, stack as a hairline rail when a parent column is ~10rem.
  import { onMount } from 'svelte';

  let {
    label,
    items
  }: {
    label: string;
    items: { href: string; label: string; count?: number }[];
  } = $props();

  // Hash only, not scroll position: :target lives on the section, so the
  // matching link is whatever the URL already says. hashchange covers an
  // in-page click that SvelteKit does not treat as a navigation.
  let hash = $state('');
  onMount(() => {
    const sync = () => {
      hash = window.location.hash;
    };
    sync();
    window.addEventListener('hashchange', sync);
    return () => window.removeEventListener('hashchange', sync);
  });
</script>

<div class="host">
  <nav class="section-nav" aria-label={label}>
    {#each items as item (item.href)}
      <a
        href={item.href}
        aria-current={hash === item.href ? 'location' : undefined}
      >{item.label}{#if item.count != null}<span class="n">{item.count}</span>{/if}</a>
    {/each}
  </nav>
</div>

<style>
  .host {
    container-type: inline-size;
    min-width: 0;
  }

  .section-nav {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin-bottom: 22px;
  }

  .section-nav a {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    min-height: 44px;
    padding: 8px 16px;
    border-radius: var(--bb-radius-pill, 100px);
    border: 1px solid var(--bb-border, rgba(201, 168, 124, 0.15));
    background: rgba(255, 255, 255, 0.03);
    color: var(--bb-tan-light);
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    text-decoration: none;
    transition:
      color var(--bb-dur-fast, 160ms) ease,
      background var(--bb-dur-fast, 160ms) ease,
      border-color var(--bb-dur-fast, 160ms) ease;
  }
  .section-nav a:hover {
    color: var(--bb-tan-pale, var(--bb-tan-light));
    background: rgba(201, 168, 124, 0.08);
    border-color: var(--bb-border-strong, rgba(201, 168, 124, 0.35));
  }
  .section-nav a[aria-current='location'] {
    color: var(--bb-tan-pale, var(--bb-tan-light));
    border-color: var(--bb-border-strong, rgba(201, 168, 124, 0.35));
    background: rgba(201, 168, 124, 0.1);
  }
  .n {
    font-size: 10px;
    letter-spacing: 0.04em;
    color: var(--bb-muted);
  }
  .section-nav a[aria-current='location'] .n { color: inherit; }

  /* Narrow host = sidebar column. Stack on the hairline; stick inside the
     stretched grid cell so the families column can scroll past. */
  @container (max-width: 220px) {
    .section-nav {
      flex-direction: column;
      flex-wrap: nowrap;
      gap: 2px;
      margin-bottom: 0;
      border-left: 1px solid var(--bb-border, rgba(201, 168, 124, 0.15));
      position: sticky;
      top: var(--section-nav-sticky-top, calc(58px + env(safe-area-inset-top, 0px) + 68px));
    }
    .section-nav a {
      display: flex;
      min-height: 0;
      padding: 8px 0 8px 14px;
      border: none;
      border-radius: 0;
      background: transparent;
      color: var(--bb-muted);
      position: relative;
    }
    .section-nav a::before {
      content: "";
      position: absolute;
      left: -1px;
      top: 20%;
      bottom: 20%;
      width: 1px;
      background: var(--bb-tan);
      transform: scaleY(0);
      transition: transform var(--bb-dur-base, 240ms) var(--bb-ease-out-expo, cubic-bezier(0.19, 1, 0.22, 1));
    }
    .section-nav a:hover {
      color: var(--bb-tan-light);
      background: transparent;
      border-color: transparent;
    }
    .section-nav a[aria-current='location'] {
      color: var(--bb-tan-light);
      background: transparent;
      padding-left: 18px;
    }
    .section-nav a[aria-current='location']::before { transform: scaleY(1); }
  }
</style>
