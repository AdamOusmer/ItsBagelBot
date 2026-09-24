<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

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
    crumbs?: UiCrumb[];
    crumbAriaLabel?: string;
    clock?: boolean;
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
