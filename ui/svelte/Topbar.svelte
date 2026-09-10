<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for `.bb-topbar`: the call-sign strip. Its Astro twin is
  // ../astro/Topbar.astro; ../test/parity.test.ts diffs the two.
  //
  // This is the CHROME half of the component it replaces. The other half --
  // the operator chip, its menu, the avatar engine, the shared-board list and
  // a POST /auth/logout form -- was session data and app routing, and it now
  // lives in web/kit as its own wrapper, handed in through the `account`
  // snippet. The split is the plan's own rule: kit keeps what binds bot data,
  // ui keeps what is an element.
  //
  // The notification-bell fallback is gone with it. The strip used to render a
  // dead <button> when no `actions` snippet was given -- a bell that opened
  // nothing, on every page of the admin board. A missing slot now renders
  // nothing.
  import '../styles/elements/shell.css';
  import Brand from './Brand.svelte';
  import { mountClock } from '../lib/clock';
  import type { Snippet } from 'svelte';
  import type { UiBrand, UiCrumb } from '../lib/nav-types';

  let {
    brand,
    crumbs = [],
    crumbAriaLabel,
    clock = true,
    railed = false,
    actions,
    account,
    class: className = '',
    ...rest
  }: {
    brand: UiBrand;
    /** Ancestors first, current page last. The last one is not a link. */
    crumbs?: UiCrumb[];
    crumbAriaLabel?: string;
    /** The wall clock. Off for a surface that renders a static screenshot. */
    clock?: boolean;
    /** A railed board owns its account surface in the rail at desktop widths,
        so the chip here is a phone-only duplicate and is hidden there. */
    railed?: boolean;
    actions?: Snippet;
    account?: Snippet;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-topbar', className || null].filter(Boolean).join(' '));

  let clockEl = $state<HTMLElement | null>(null);
  $effect(() => {
    if (!clockEl || !clock) return;
    return mountClock(clockEl);
  });
</script>

<header class={classes} {...rest}
  ><Brand
    title={brand.title}
    sub={brand.sub}
    href={brand.href}
    logoSrc={brand.logoSrc}
    logoAlt={brand.logoAlt}
    size="sm"
    premium={brand.premium}
  />{#if crumbs.length}<nav class="bb-topbar__crumb" aria-label={crumbAriaLabel}
      ><ol
        >{#each crumbs as crumb, i (crumb.label)}<li
            data-here={i === crumbs.length - 1 ? '' : undefined}
            >{#if crumb.href}<a href={crumb.href}>{crumb.label}</a>{:else}<span
                aria-current="page">{crumb.label}</span
              >{/if}</li
          >{/each}</ol
      ></nav
    >{/if}<div class="bb-topbar__grow"></div>{#if clock}<span
      bind:this={clockEl}
      class="bb-topbar__clock"
      aria-hidden="true"
      data-clock=""
    ></span>{/if}{#if account}<div
      class="bb-topbar__account"
      data-railed={railed ? '' : undefined}>{@render account()}</div
    >{/if}{#if actions}{@render actions()}{/if}</header
>
